package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

type journaled struct {
	Method  string
	URL     string
	Headers map[string]string
	// Record is what the axx WireMock extension recorded, if anything.
	Record any
}

// fakeWireMock implements the admin endpoints axx uses.
type fakeWireMock struct {
	mu      sync.Mutex
	journal []journaled
	counts  int // number of count calls
}

func (f *fakeWireMock) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/__admin/requests/count", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Method  string                   `json:"method"`
			URL     string                   `json:"url"`
			Headers map[string]headerMatcher `json:"headers"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.counts++
		n := 0
		for _, j := range f.journal {
			if (p.Method == "ANY" || p.Method == j.Method) && p.URL == j.URL && headersMatch(p.Headers, j.Headers) {
				n++
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"count": n})
	})
	mux.HandleFunc("GET /__admin/requests", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		reqs := []map[string]any{}
		for i := len(f.journal) - 1; i >= 0; i-- { // newest first, like WireMock
			j := f.journal[i]
			r := map[string]any{"id": fmt.Sprintf("id-%d", i), "request": map[string]any{"method": j.Method, "url": j.URL}}
			if j.Record != nil {
				r["subEvents"] = []map[string]any{{"type": "openapi-validation", "data": j.Record}}
			}
			reqs = append(reqs, r)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"requests": reqs})
	})
	mux.HandleFunc("/__admin/near-misses/request-pattern", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var misses []map[string]any
		for _, j := range f.journal {
			misses = append(misses, map[string]any{"request": map[string]any{"method": j.Method, "url": j.URL}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"nearMisses": misses})
	})
	return mux
}

func headersMatch(want map[string]headerMatcher, got map[string]string) bool {
	for k, m := range want {
		v, ok := got[k]
		switch {
		case m.Absent:
			if ok {
				return false
			}
		case m.EqualTo != "":
			if !ok || v != m.EqualTo {
				return false
			}
		case m.Matches != "":
			if !ok || !regexp.MustCompile(`^(?:`+m.Matches+`)$`).MatchString(v) {
				return false
			}
		}
	}
	return true
}

func setup(t *testing.T) (*match.Registry, *core.Scenario, *fakeWireMock, string) {
	t.Helper()
	fw := &fakeWireMock{journal: []journaled{
		{Method: "GET", URL: "/launches", Headers: map[string]string{"Accept": "application/json"}},
		{Method: "GET", URL: "/launches", Headers: map[string]string{"Accept": "application/json"}},
		{Method: "POST", URL: "/crew/validate", Headers: map[string]string{"Content-Type": "application/json"}},
	}}
	srv := httptest.NewServer(fw.handler())
	t.Cleanup(srv.Close)
	reg := match.NewRegistry()
	for _, p := range []struct {
		n string
		p core.Pack
	}{{"core", core.ParamsPack()}, {"mock", Pack()}} {
		if err := reg.AddPack(p.n, p.p.Manifest()); err != nil {
			t.Fatal(err)
		}
	}
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{}, nil, nil)
	return reg, sc, fw, srv.URL
}

func step(t *testing.T, reg *match.Registry, sc *core.Scenario, text string, table [][]string) error {
	t.Helper()
	ms := reg.Match(text)
	if len(ms) != 1 {
		t.Fatalf("%q matched %d definitions", text, len(ms))
	}
	var tbl *core.Table
	if table != nil {
		tbl = &core.Table{Rows: table}
	}
	args, err := reg.Resolve(sc, ms[0], text, tbl, nil)
	if err != nil {
		return err
	}
	return ms[0].Def().Step.Run(sc, args)
}

func TestVerificationFlow(t *testing.T) {
	reg, sc, _, url := setup(t)
	ok := func(text string, table ...[]string) {
		t.Helper()
		if err := step(t, reg, sc, text, table); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
	fail := func(text string, table ...[]string) error {
		t.Helper()
		err := step(t, reg, sc, text, table)
		if err == nil {
			t.Fatalf("%s: expected failure", text)
		}
		return err
	}
	if err := step(t, reg, sc, "the mocked spacex service with the following properties:", [][]string{{"url", url}}); err != nil {
		t.Fatal(err)
	}
	ok("the mocked GET request to /launches named get-launches was received by spacex")
	ok("the mocked request named get-launches was received exactly 2 times")
	ok("the mocked request named get-launches on spacex was received at least 1 time")
	ok("the mocked request named get-launches was received at most 2 times")
	err := fail("the mocked request named get-launches was received exactly 3 times")
	if !core.IsAssertion(err) || !strings.Contains(err.Error(), "received 2") || !strings.Contains(err.Error(), "GET /launches") {
		t.Errorf("failure should explain count and near misses: %v", err)
	}
	ok("the mocked DELETE request to /launches named delete-launches was not received")
	_ = fail("the mocked GET request to /launches named again on spacex was not received")
	ok("the header Accept for mocked request named get-launches is 'application/json'")
	ok("the header Accept for mocked request named get-launches on spacex matches ^.*/json$")
	ok("the header Accept-Charset for mocked request named get-launches on spacex is missing")
	ok("the headers for mocked request named get-launches on spacex are missing:", []string{"X-Nope"}, []string{"X-Other"})
	ok("the headers for mocked request named get-launches on spacex are:", []string{"Accept", "application/json"})
	_ = fail("the headers for mocked request named get-launches on spacex match:", []string{"Accept", "^text/.*$"})
	if d := sc.Descriptions(); d == nil || d["mock"] == nil {
		t.Errorf("failure context should describe the last verification: %v", d)
	}
}

func TestErrors(t *testing.T) {
	reg, sc, _, url := setup(t)
	if err := step(t, reg, sc, "the mocked request named x was received exactly 1 time", nil); err == nil || !strings.Contains(err.Error(), "No mocked service set") {
		t.Errorf("no service: %v", err)
	}
	if err := step(t, reg, sc, "the mocked spacex service with the following properties:", [][]string{{"uri", url}}); err == nil || !strings.Contains(err.Error(), `Property "url" is required`) {
		t.Errorf("missing url: %v", err)
	}
	if err := step(t, reg, sc, "the mocked spacex service with the following properties:", [][]string{{"url", url}}); err != nil {
		t.Fatal(err)
	}
	if err := step(t, reg, sc, "the mocked spacex service with the following properties:", [][]string{{"url", url}}); err == nil || !strings.Contains(err.Error(), `Mocked service "spacex" already set`) {
		t.Errorf("duplicate: %v", err)
	}
	if err := step(t, reg, sc, "the mocked request named never was received exactly 1 time", nil); err == nil || !strings.Contains(err.Error(), "register it first") {
		t.Errorf("unregistered name: %v", err)
	}
	if err := step(t, reg, sc, "the mocked TRACE request to /x named t was received by spacex", nil); err == nil || !strings.Contains(err.Error(), "Unsupported method: TRACE") {
		t.Errorf("method: %v", err)
	}
	if err := step(t, reg, sc, "the mocked request named x on nowhere was received exactly 1 time", nil); err == nil || !strings.Contains(err.Error(), `Mocked service "nowhere" not set`) {
		t.Errorf("unknown service: %v", err)
	}
}

func TestInterpolatedURLAndPathPrefix(t *testing.T) {
	fw := &fakeWireMock{}
	mux := http.NewServeMux()
	mux.Handle("/wm/", http.StripPrefix("/wm", fw.handler()))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := newClient(srv.URL+"/wm/", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.count(context.Background(), &pattern{Method: "GET", URL: "/"}); err != nil {
		t.Fatalf("path prefix not honored: %v", err)
	}
	if fw.counts != 1 {
		t.Fatalf("count calls = %d", fw.counts)
	}
}
