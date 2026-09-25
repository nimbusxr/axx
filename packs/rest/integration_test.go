//go:build integration

package rest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/packs/all"
)

// stub registers a WireMock stub mapping.
func stub(t *testing.T, base string, mapping map[string]any) {
	t.Helper()
	b, _ := json.Marshal(mapping)
	resp, err := http.Post(base+"/__admin/mappings", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("stub: %d %s", resp.StatusCode, body)
	}
}

func jsonResponse(status int, body string, headers map[string]string) map[string]any {
	h := map[string]any{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	return map[string]any{"status": status, "body": body, "headers": h}
}

// TestRealisticFlowAgainstWireMock drives the REST pack through the real
// engine (every pack registered, steps invoked by text) against a
// WireMock server that also serves the OpenAPI specification, and checks
// the traffic with the mock pack.
func TestRealisticFlowAgainstWireMock(t *testing.T) {
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "wiremock/wiremock:3.13.2",
			ExposedPorts: []string{"8080/tcp"},
			WaitingFor:   wait.ForHTTP("/__admin/health").WithPort("8080/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("wiremock container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "8080/tcp")
	base := fmt.Sprintf("http://%s:%s", host, port.Port())

	spec, err := os.ReadFile("testdata/space30.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ext, _ := os.ReadFile("testdata/examples/launch-ext.json")
	stub(t, base, map[string]any{
		"request":  map[string]any{"method": "GET", "url": "/openapi/space30.yaml"},
		"response": map[string]any{"status": 200, "body": string(spec), "headers": map[string]string{"Content-Type": "application/yaml"}},
	})
	stub(t, base, map[string]any{
		"request":  map[string]any{"method": "GET", "url": "/openapi/examples/launch-ext.json"},
		"response": jsonResponse(200, string(ext), nil),
	})
	stub(t, base, map[string]any{
		"request": map[string]any{
			"method": "POST", "url": "/api/launches",
			"bodyPatterns": []any{map[string]any{"matchesJsonPath": "$[?(@.name == 'Falcon Heavy')]"}},
		},
		"response": jsonResponse(201, `{"id":42,"name":"Falcon Heavy","flight_number":2,"crew":null}`, map[string]string{"Location": "/api/launches/42"}),
	})
	stub(t, base, map[string]any{
		"request":  map[string]any{"method": "GET", "url": "/api/launches/42"},
		"response": jsonResponse(200, `{"id":42,"name":"Falcon Heavy","flight_number":"two"}`, map[string]string{"X-Rate-Limit": "5"}),
	})

	e, err := engine.New(engine.Options{Config: &config.Config{Dir: t.TempDir()}, Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close(ctx) })
	sc := core.NewScenario(ctx, core.ScenarioInfo{Name: "realistic flow"}, e.Suite, nil)
	run := func(text string, rows ...[]string) error {
		t.Helper()
		var tbl *core.Table
		if rows != nil {
			tbl = &core.Table{Rows: rows}
		}
		return e.Invoke(sc, text, tbl, nil)
	}
	ok := func(text string, rows ...[]string) {
		t.Helper()
		if err := run(text, rows...); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}

	ok("Given the space-explorer service with the following properties:", []string{"url", base}, []string{"openapi", base + "/openapi/space30.yaml"})
	ok("And the mocked spacex service with the following properties:", []string{"url", base})

	// A valid POST built from the example named in the specification.
	ok("Given a POST request to /api/launches")
	ok("And the request headers are:", []string{"Content-Type", "application/json"}, []string{"Accept", "application/json"})
	ok("And a request payload using an application/json content example named 'Alpha Launch'")
	ok("And the request payload properties are:", []string{"name", "Falcon Heavy"}, []string{"flight_number", "2"}, []string{"approved", "undefined"})
	ok("When the request is executed")
	ok("Then the response status code is 201")
	ok("And the response header Location is '/api/launches/42'")
	ok("And the response payload properties are:", []string{"id", "42"}, []string{"name", "Falcon Heavy"}, []string{"crew", "null"}, []string{"approved", "undefined"})
	ok("And the mocked POST request to /api/launches named create was received by spacex")
	ok("And the header Content-Type for mocked request named create on spacex is 'application/json'")

	// The second request's response violates the specification.
	ok("Given a 2nd ordered GET request to /api/launches/42")
	err = run("When the 2nd ordered request is executed")
	if !core.IsAssertion(err) || !strings.Contains(err.Error(), "validation.response.body.schema.type") {
		t.Fatalf("expected an OpenAPI failure, got %v", err)
	}
	ok("Then the 2nd ordered response status code is 200")

	// The same violation relaxed for the scenario.
	ok("Given the OpenAPI validation levels on space-explorer are:", []string{"validation.response.body.schema", "WARN"})
	ok("Given a 3rd ordered GET request to /api/launches/42 on space-explorer")
	ok("When the 3rd ordered request is executed on space-explorer")
	ok("Then the response payload property flight_number is '\"two\"' for 3rd ordered response on space-explorer")

	// The external example is read relative to the specification's URL.
	ok("Given a 4th ordered POST request to /api/launches")
	ok("And a request payload using an application/json content example named 'External Launch' for 4th ordered request")
	ok("And the request payload property name is 'Falcon Heavy' for 4th ordered request")
	ok("And the request header Content-Type is 'application/json' for 4th ordered request")
	ok("When the 4th ordered request is executed")
	ok("Then the 4th ordered response status code is 201")
	ok("And the mocked request named create was received exactly 2 times")

	d := sc.Descriptions()["rest"].(map[string]any)
	if d["response"].(map[string]any)["status"] != 201 {
		t.Fatalf("describe %v", d)
	}
}
