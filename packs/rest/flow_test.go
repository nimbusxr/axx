package rest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// TestLaunchFlow follows the example suite's "post a space launch" scenario.
func TestLaunchFlow(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("space-explorer", srv.URL, "space30.yaml")
	h.ok("a POST request to /api/launches?query=falcon")
	h.ok("the request headers are:", []string{"Content-Type", "application/json"}, []string{"Accept", "application/json"})
	h.ok("a request payload using an application/json content example")
	h.ok("the request payload properties are:",
		[]string{"name", "super duper"},
		[]string{"flight_number", "1000"},
		[]string{"crew", `[{"first_name": "John", "last_name": "Doe"}, {"first_name": "x"}]`},
		[]string{"crew[1]", `{"first_name": "John", "last_name": "Wick"}`},
	)
	h.ok("the request is executed")
	if got := a.last(); got.URL != "/api/launches?query=falcon" || !strings.Contains(got.Body, `"name":"super duper"`) ||
		!strings.Contains(got.Body, `"fuel_ratio":1.0`) {
		t.Fatalf("sent %+v", got)
	}
	h.ok("the response status code is 201")
	h.ok("the response header Content-Length is '174'") // chunked responses have none
	h.ok("the response header Content-Type is 'application/json'")
	h.ok("the response payload properties are:",
		[]string{"name", "super duper"},
		[]string{"flight_number", "1000"},
		[]string{"crew[0]", `{"first_name": "John", "last_name": "Doe"}`},
		[]string{"crew[1]", `{"first_name": "John", "last_name": "Wick"}`},
		[]string{"crew[?(@.last_name=='Wick')].first_name", `["John"]`},
		[]string{"crew[?(@.first_name=='John' && @.last_name=='Doe')]", `[{"first_name":"John","last_name":"Doe"}]`},
		[]string{"fuel_ratio", "1.0"},
	)
	if logs := strings.Join(h.sink.logs, "\n"); !strings.Contains(logs, "POST "+srv.URL+"/api/launches?query=falcon -> 201 Created") {
		t.Errorf("logs: %s", logs)
	}
	if len(h.sink.attachments) != 2 {
		t.Errorf("attachments: %v", h.sink.attachments)
	}
}

// TestConcurrentScenarios runs scenarios in parallel on one suite, sharing
// the HTTP client and the parsed specification, as the runner does.
func TestConcurrentScenarios(t *testing.T) {
	_, srv := newAPI(t)
	base := newHarness(t)
	steps := []string{
		"the space service with the following properties:",
		"a POST request to /api/launches",
		"the request header Content-Type is 'application/json'",
		"a request payload using an application/json content example named 'Alpha Launch'",
		"the request payload property name is 'Parallel'",
		"the request is executed",
		"the response status code is 201",
		"the response payload property name is 'Parallel'",
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc := core.NewScenario(context.Background(), core.ScenarioInfo{Name: fmt.Sprint("s", i)}, base.suite, &sink{})
			for _, text := range steps {
				ms := base.reg.Match(text)
				if len(ms) != 1 {
					errs <- fmt.Errorf("%s: %d matches", text, len(ms))
					return
				}
				var tbl *core.Table
				if strings.HasSuffix(text, "properties:") {
					tbl = &core.Table{Rows: [][]string{{"url", srv.URL}, {"openapi", "space30.yaml"}}}
				}
				args, err := base.reg.Resolve(sc, ms[0], text, tbl, nil)
				if err == nil {
					err = ms[0].Def().Step.Run(sc, args)
				}
				if err != nil {
					errs <- fmt.Errorf("scenario %d: %s: %w", i, text, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
