package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

// sink collects a scenario's logs and attachments.
type sink struct {
	mu          sync.Mutex
	logs        []string
	attachments []string
}

func (s *sink) Log(_ *core.Scenario, msg string) {
	s.mu.Lock()
	s.logs = append(s.logs, msg)
	s.mu.Unlock()
}

func (s *sink) Attach(_ *core.Scenario, mediaType string, body []byte, name string) {
	s.mu.Lock()
	s.attachments = append(s.attachments, name+" ("+mediaType+"): "+string(body))
	s.mu.Unlock()
}

// harness runs step text through a registry holding the core and rest
// packs, like the runner does.
type harness struct {
	t     *testing.T
	reg   *match.Registry
	suite *core.Suite
	sc    *core.Scenario
	sink  *sink
}

type harnessOpt func(*core.SuiteOptions)

func withPackConfig(pack, jsonText string) harnessOpt {
	return func(o *core.SuiteOptions) {
		if o.PackConfig == nil {
			o.PackConfig = map[string]json.RawMessage{}
		}
		o.PackConfig[pack] = json.RawMessage(jsonText)
	}
}

func newHarness(t *testing.T, opts ...harnessOpt) *harness {
	t.Helper()
	reg := match.NewRegistry()
	if err := reg.AddParams("core", core.ParamsPack().Manifest().Params); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddPack("rest", Pack().Manifest()); err != nil {
		t.Fatal(err)
	}
	so := core.SuiteOptions{
		ResolvePath: func(p string) (string, error) {
			cand := filepath.Join("testdata", filepath.FromSlash(p))
			if _, err := os.Stat(cand); err != nil {
				return "", err
			}
			return cand, nil
		},
		Interpolate: func(s string) string { return strings.ReplaceAll(s, "${env:HOST}", "127.0.0.1") },
	}
	for _, o := range opts {
		o(&so)
	}
	suite := core.NewSuite(so)
	t.Cleanup(func() { _ = suite.Close(context.Background()) })
	sk := &sink{}
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{Name: t.Name()}, suite, sk)
	return &harness{t: t, reg: reg, suite: suite, sc: sc, sink: sk}
}

// step runs one step line; rows, if any, form its data table.
func (h *harness) step(text string, rows ...[]string) error {
	h.t.Helper()
	ms := h.reg.Match(text)
	if len(ms) != 1 {
		h.t.Fatalf("%q matched %d step definitions", text, len(ms))
	}
	var tbl *core.Table
	if rows != nil {
		tbl = &core.Table{Rows: rows}
	}
	args, err := h.reg.Resolve(h.sc, ms[0], text, tbl, nil)
	if err != nil {
		return err
	}
	return ms[0].Def().Step.Run(h.sc, args)
}

func (h *harness) ok(text string, rows ...[]string) {
	h.t.Helper()
	if err := h.step(text, rows...); err != nil {
		h.t.Fatalf("%s: %v", text, err)
	}
}

// failure runs a step that must fail with an error containing want, and
// returns the error for further checks.
func (h *harness) failure(text, want string, rows ...[]string) error {
	h.t.Helper()
	err := h.step(text, rows...)
	if err == nil {
		h.t.Fatalf("%s: expected an error containing %q", text, want)
	}
	if !strings.Contains(err.Error(), want) {
		h.t.Fatalf("%s: error %q does not contain %q", text, err, want)
	}
	return err
}

// fails runs a step that must fail with an error containing want.
func (h *harness) fails(text, want string, rows ...[]string) {
	h.t.Helper()
	_ = h.failure(text, want, rows...)
}

// assertion runs a step that must fail with an assertion failure
// containing want, and returns it.
func (h *harness) assertion(text, want string, rows ...[]string) *core.AssertionError {
	h.t.Helper()
	err := h.failure(text, want, rows...)
	var ae *core.AssertionError
	if !errors.As(err, &ae) {
		h.t.Fatalf("%s: %T is not an assertion failure: %v", text, err, err)
	}
	return ae
}

// assertionFails runs a step that must fail with an assertion failure.
func (h *harness) assertionFails(text, want string, rows ...[]string) {
	h.t.Helper()
	_ = h.assertion(text, want, rows...)
}

// service registers the default service at url (and spec, if any).
func (h *harness) service(name, u, spec string) {
	h.t.Helper()
	rows := [][]string{{"url", u}}
	if spec != "" {
		rows = append(rows, []string{"openapi", spec})
	}
	h.ok("the "+name+" service with the following properties:", rows...)
}

// ---- fake API ----

type seen struct {
	Method string
	URL    string
	Header http.Header
	Body   string
}

// api is a small fake of the Space Explorer API.
type api struct {
	mu   sync.Mutex
	reqs []seen
}

func (a *api) last() seen {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.reqs) == 0 {
		return seen{}
	}
	return a.reqs[len(a.reqs)-1]
}

func (a *api) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.reqs)
}

func respond(w http.ResponseWriter, status int, ct, body string) {
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	a.mu.Lock()
	a.reqs = append(a.reqs, seen{r.Method, r.URL.String(), r.Header.Clone(), string(body)})
	a.mu.Unlock()
	const js = "application/json"
	switch {
	case r.URL.Path == "/api/launches" && r.Method == http.MethodGet:
		respond(w, 200, js, `[{"id":1,"name":"FalconSat","flight_number":1,"date_utc":"2006-03-24T22:30:00.000Z"},`+
			`{"id":2,"name":"DemoSat","flight_number":2,"fuel_ratio":1.0,"crew":null}]`)
	case r.URL.Path == "/api/launches" && r.Method == http.MethodPost:
		if r.URL.Query().Get("fail") != "" {
			respond(w, 400, "application/problem+json", `{"title":"Bad Request","status":400}`)
			return
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			respond(w, 400, "application/problem+json", `{"title":"Bad Request","status":400}`)
			return
		}
		out := `{"id":42`
		if len(body) > 2 {
			out += "," + strings.TrimSpace(string(body))[1:]
		} else {
			out += "}"
		}
		w.Header().Set("Location", "/api/launches/42")
		respond(w, 201, js, out)
	case r.URL.Path == "/api/launches/search":
		form, _ := url.ParseQuery(string(body))
		b, _ := json.Marshal(form)
		respond(w, 200, js, string(b))
	case strings.HasPrefix(r.URL.Path, "/api/launches/"):
		switch strings.TrimPrefix(r.URL.Path, "/api/launches/") {
		case "1":
			w.Header().Set("X-Rate-Limit", "10")
			respond(w, 200, js, `{"id":1,"name":"FalconSat","flight_number":1}`)
		case "2": // missing the required X-Rate-Limit header
			respond(w, 200, js, `{"id":2,"name":"DemoSat","flight_number":2}`)
		case "3": // wrong types
			w.Header().Set("X-Rate-Limit", "10")
			respond(w, 200, js, `{"id":"three","name":"Trailblazer","flight_number":3,"approved":"yes"}`)
		case "4": // undocumented content type
			w.Header().Set("X-Rate-Limit", "10")
			respond(w, 200, "text/csv", "id,name\n4,x\n")
		case "418":
			respond(w, 418, js, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	case r.URL.Path == "/api/missions" || r.URL.Path == "/api/secure":
		respond(w, 200, js, `[]`)
	case r.URL.Path == "/headers":
		w.Header().Add("Access-Control-Allow-Origin", "*")
		w.Header().Add("Access-Control-Allow-Origin", "test.example.com")
		w.Header().Set("X-Request-Id", "req-123")
		respond(w, 200, "application/json; charset=UTF-8", `{"name":"x","count":3,"ratio":1.5,"ok":true,"none":null,"list":[1,2],"obj":{"a":"b"}}`)
	case r.URL.Path == "/vnd":
		respond(w, 200, "application/vnd.api+json", `{"data":{"id":"7"}}`)
	case r.URL.Path == "/text":
		respond(w, 200, "text/plain", "plain text body")
	case r.URL.Path == "/redirect":
		http.Redirect(w, r, "/api/launches", http.StatusFound)
	case r.URL.Path == "/slow":
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/echo":
		b, _ := json.Marshal(map[string]any{"method": r.Method, "query": r.URL.RawQuery, "headers": r.Header, "body": string(body)})
		respond(w, 200, js, string(b))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newAPI(t *testing.T) (*api, *httptest.Server) {
	t.Helper()
	a := &api{}
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	return a, srv
}
