//go:build integration

package mock

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestAgainstRealWireMock verifies the admin-API client against WireMock 3.
func TestAgainstRealWireMock(t *testing.T) {
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

	// Stub + traffic.
	stub := `{"request":{"method":"ANY","urlPattern":"/.*"},"response":{"status":200,"body":"ok"}}`
	resp, err := http.Post(base+"/__admin/mappings", "application/json", strings.NewReader(stub))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, base+"/launches?limit=1", nil)
		req.Header.Set("Accept", "application/json")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
	}

	// A JSON body and a form, for the payload and form checks.
	for _, r := range []struct{ path, contentType, body string }{
		{"/v1/collections", "application/json", `{"reference": "PX-REG-1401", "weightGrams": 800, "deliverTo": {"postcode": "10115", "country": "DE"}}`},
		{"/v1/collections", "application/json", `{"reference": "PX-REG-1402", "weightGrams": 650, "recipient": {"name": "Ada Lovelace"}}`},
		{"/v1/pickups", "application/x-www-form-urlencoded", "reference=PX-WEB-5401&day=Friday"},
	} {
		r, err := http.Post(base+r.path, r.contentType, strings.NewReader(r.body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
	}

	reg, sc, _, _ := setup(t)
	run := func(text string, table ...[]string) error {
		t.Helper()
		return step(t, reg, sc, text, table)
	}
	if err := run("the mocked spacex service with the following properties:", []string{"url", base}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"the mocked GET request to /launches?limit=1 named launches was received by spacex",
		"the mocked request named launches was received exactly 2 times",
		"the header Accept for mocked request named launches on spacex matches ^application/.*$",
		"the header X-Missing for mocked request named launches on spacex is missing",
		"the mocked POST request to /launches named post-launches was not received",
	} {
		if err := run(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if err := run("the mocked POST request to /v1/collections named collection was received by spacex"); err != nil {
		t.Fatal(err)
	}
	if err := run("the mocked request named collection was received exactly 2 times"); err != nil {
		t.Fatal(err)
	}
	// The body's properties narrow what the named request counts.
	if err := run("the payload properties for mocked request named collection on spacex are:",
		[]string{"reference", "PX-REG-1401"}, []string{"weightGrams", "800"},
		[]string{"deliverTo.postcode", `"10115"`}, []string{"recipient", "undefined"}); err != nil {
		t.Fatal(err)
	}
	if err := run("the mocked request named collection was received exactly 1 time"); err != nil {
		t.Fatal(err)
	}
	if err := run("the payload properties for mocked request named collection are:", []string{"weightGrams", "900"}); err == nil {
		t.Error("a payload property with another value passed")
	}
	if err := run("the mocked POST request to /v1/pickups named pickup-notice was received by spacex"); err != nil {
		t.Fatal(err)
	}
	if err := run("the form fields for mocked request named pickup-notice are:",
		[]string{"reference", "PX-WEB-5401"}, []string{"day", "Friday"}, []string{"shop", "undefined"}); err != nil {
		t.Fatal(err)
	}
	if err := run("the form fields for mocked request named pickup-notice on spacex are:", []string{"day", "Tomorrow"}); err == nil {
		t.Error("a form field with another value passed")
	}

	err = run("the mocked request named launches was received exactly 5 times")
	if err == nil || !strings.Contains(err.Error(), "received 2") {
		t.Fatalf("expected count failure with near misses, got %v", err)
	}
}
