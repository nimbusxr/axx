package mock

import (
	"context"
	"encoding/base64"
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
	// Body is the request's body, and Response the body of its answer.
	Body, Response string
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
	mux.HandleFunc("/__admin/requests/find", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Method  string                   `json:"method"`
			URL     string                   `json:"url"`
			URLPath string                   `json:"urlPath"`
			Headers map[string]headerMatcher `json:"headers"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.mu.Lock()
		defer f.mu.Unlock()
		reqs := []map[string]any{}
		for _, j := range f.journal {
			path, _, _ := strings.Cut(j.URL, "?")
			if p.Method != j.Method || (p.URL != j.URL && p.URLPath != path) || !headersMatch(p.Headers, j.Headers) {
				continue
			}
			reqs = append(reqs, map[string]any{"method": j.Method, "url": j.URL, "bodyAsBase64": base64.StdEncoding.EncodeToString([]byte(j.Body))})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"requests": reqs})
	})
	mux.HandleFunc("GET /__admin/requests", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		reqs := []map[string]any{}
		for i := len(f.journal) - 1; i >= 0; i-- { // newest first, like WireMock
			j := f.journal[i]
			r := map[string]any{
				"id":       fmt.Sprintf("id-%d", i),
				"request":  map[string]any{"method": j.Method, "url": j.URL, "bodyAsBase64": base64.StdEncoding.EncodeToString([]byte(j.Body))},
				"response": map[string]any{"bodyAsBase64": base64.StdEncoding.EncodeToString([]byte(j.Response))},
			}
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
	if err := step(t, reg, sc, "the mocked spacex service with the following properties:", [][]string{}); err == nil || !strings.Contains(err.Error(), `Property "url" is required`) {
		t.Errorf("missing url: %v", err)
	}
	// An unknown property fails wherever it is.
	for _, table := range [][][]string{{{"uri", url}}, {{"url", url}, {"uri", url}}} {
		if err := step(t, reg, sc, "the mocked spacex service with the following properties:", table); err == nil ||
			!strings.Contains(err.Error(), `unknown mocked service property "uri" (supported: url)`) {
			t.Errorf("unknown property: %v", err)
		}
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
	// A path is a path: its method is checked, and a query string belongs in the table.
	for _, c := range []struct{ text, want string }{
		{"the mocked TRACE request to path /x named t was not received", "Unsupported method: TRACE"},
		{"the mocked GET request to path /x?a=1 named t was not received", "has a query string"},
		{"none of the mocked TRACE requests to path /x have the query parameters:", "Unsupported method: TRACE"},
		{"none of the mocked GET requests to path /x?a=1 have the query parameters:", "has a query string"},
	} {
		if err := step(t, reg, sc, c.text, [][]string{{"a", "1"}}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.text, err)
		}
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

// rows adds a table's rows to p, as a step does.
func rows(t *testing.T, p *pattern, add rowAdder, table ...[]string) {
	t.Helper()
	if err := addRows(p, &core.Table{Rows: table}, add); err != nil {
		t.Fatal(err)
	}
}

func TestBodyAndFormPatterns(t *testing.T) {
	p := &pattern{Method: "POST", URL: "/v1/collections"}
	rows(t, p, addProperty, []string{"reference", "PX-REG-1401"}, []string{"$.deliverTo.postcode", `"10115"`},
		[]string{"recipient", "undefined"}, []string{"weightGrams", "800"})
	rows(t, p, addProperty, []string{"reference", "PX-REG-1402"}) // a later check of a property replaces the earlier one
	rows(t, p, addField, []string{"day", "Friday"}, []string{"shop", "undefined"}, []string{"note", `""`})
	// WireMock matches the request; axx checks the body's properties, with
	// their types (ADR 0014), and failures show them.
	got := string(mustJSON(p))
	want := `{"method":"POST","url":"/v1/collections",` +
		`"formParameters":{"day":{"equalTo":"Friday"},"shop":{"absent":true},"note":{"equalTo":""}}}`
	if got != want {
		t.Errorf("pattern\n got %s\nwant %s", got, want)
	}
	if got, want := string(p.shown()), strings.TrimSuffix(want, "}")+`,"payloadProperties":{"$.reference":"PX-REG-1402",`+
		`"$.deliverTo.postcode":"10115","$.recipient":"<absent>","$.weightGrams":800}}`; got != want {
		t.Errorf("shown\n got %s\nwant %s", got, want)
	}
	for body, want := range map[string]string{
		`{"reference":"PX-REG-1402","deliverTo":{"postcode":"10115"},"weightGrams":800}`:                         "",
		`{"reference":"PX-REG-1402","deliverTo":{"postcode":10115},"weightGrams":800}`:                           `$.deliverTo.postcode is the integer 10115: write 10115`,
		`{"reference":"PX-REG-1402","deliverTo":{"postcode":"10115"},"weightGrams":"800"}`:                       `$.weightGrams is the string "800": write "800"`,
		`{"reference":"PX-REG-1402","deliverTo":{"postcode":"10115"},"weightGrams":800.0}`:                       `$.weightGrams is the decimal 800.0: write 800.0`,
		`{"reference":"PX-REG-1401","deliverTo":{"postcode":"10115"},"weightGrams":800}`:                         `$.reference is "PX-REG-1401", not "PX-REG-1402"`,
		`{"reference":"PX-REG-1402","deliverTo":{"postcode":"10115"},"weightGrams":800,"recipient":{"n":"Ada"}}`: `it has $.recipient: {"n":"Ada"}`,
		`{"reference":"PX-REG-1402"}`: "it has no $.deliverTo.postcode",
		"reference=PX-REG-1402":       "its body is not JSON",
		"":                            "its body is not JSON",
	} {
		if got, err := p.mismatch([]byte(body)); err != nil || got != want {
			t.Errorf("%s: %q, %v; want %q", body, got, err, want)
		}
	}
}

func TestPathAndQueryPatterns(t *testing.T) {
	p := &pattern{Method: "POST", URL: "/v1/collections", PathOnly: true}
	rows(t, p, addQuery, []string{"slot", "same-day"}, []string{"reference", "undefined"}, []string{"page", `"2"`})
	got := string(mustJSON(p))
	want := `{"method":"POST","urlPath":"/v1/collections","queryParameters":{"slot":{"equalTo":"same-day"},"reference":{"absent":true},"page":{"equalTo":"2"}}}`
	if got != want {
		t.Errorf("pattern\n got %s\nwant %s", got, want)
	}
}

// A table's value the rule cannot read fails the step, and adds none of
// its rows (ADR 0014): query parameters, form fields and headers are text,
// and a payload property's value is typed.
func TestValuesTheRuleCannotRead(t *testing.T) {
	for _, c := range []struct {
		add   rowAdder
		table [][]string
		want  string
	}{
		{addQuery, [][]string{{"slot", "same-day"}, {"page", "2"}}, `page: 2 reads as an integer, and only text fits here: write "2" for the text`},
		{addField, [][]string{{"express", "true"}}, `express: true reads as a boolean, and only text fits here: write "true" for the text`},
		{addField, [][]string{{"day", ""}}, `day: the cell is empty: write "" for the empty text, or undefined for none`},
		{addHeader, [][]string{{"X-Count", "3"}}, `X-Count: 3 reads as an integer`},
		{addHeader, [][]string{{"X-Count", "undefined"}}, `X-Count: undefined reads as undefined (no such property)`},
		{addProperty, [][]string{{"reference", "PX-REG-1401"}, {"postcode", "01067"}}, `postcode: 01067 could be the number 1067 or the text 01067`},
		{addProperty, [][]string{{"weightGrams", ""}}, `weightGrams: the cell is empty: write "" for the empty string, null for null, or undefined for no such property`},
		{addProperty, [][]string{{"[", "x"}}, `[:`},
	} {
		p := &pattern{Method: "POST", URL: "/v1/collections"}
		err := addRows(p, &core.Table{Rows: c.table}, c.add)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.table, err, c.want)
		}
		if len(p.body)+len(p.query)+len(p.form)+len(p.headers) > 0 {
			t.Errorf("%v: rows were added: %+v", c.table, p)
		}
	}
}

// The payload properties of a named request are checked with their types,
// and narrow what its counts count; a failure says how the requests to it
// differ.
func TestPayloadPropertiesFlow(t *testing.T) {
	reg, sc, fw, url := setup(t)
	fw.journal = append(fw.journal,
		journaled{Method: "POST", URL: "/v1/collections", Body: `{"reference":"PX-REG-1401","weightGrams":800,"deliverTo":{"postcode":"10115"}}`},
		journaled{Method: "POST", URL: "/v1/collections", Body: `{"reference":"PX-REG-1402","weightGrams":"650","recipient":{"name":"Ada"}}`},
	)
	run := func(text string, table ...[]string) error {
		t.Helper()
		return step(t, reg, sc, text, table)
	}
	ok := func(text string, table ...[]string) {
		t.Helper()
		if err := run(text, table...); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
	ok("the mocked courier service with the following properties:", []string{"url", url})
	ok("the mocked POST request to /v1/collections named collection was received by courier")
	ok("the mocked request named collection was received exactly 2 times")
	ok("the payload properties for mocked request named collection are:", []string{"reference", "PX-REG-1401"},
		[]string{"weightGrams", "800"}, []string{"deliverTo.postcode", `"10115"`}, []string{"recipient", "undefined"})
	ok("the mocked request named collection was received exactly 1 time")

	ok("the mocked POST request to /v1/collections named other was received by courier")
	err := run("the payload properties for mocked request named other on courier are:", []string{"weightGrams", "650"})
	if !core.IsAssertion(err) || !strings.Contains(err.Error(), "Requests received that match but for their payload:\n"+
		"  POST /v1/collections: $.weightGrams is 800, not 650\n"+
		`  POST /v1/collections: $.weightGrams is the string "650": write "650"`) {
		t.Errorf("a value of another type: %v", err)
	}
	if err := run("the payload properties for mocked request named collection are:", []string{"weightGrams", ""}); err == nil ||
		core.IsAssertion(err) || !strings.Contains(err.Error(), "weightGrams: the cell is empty") {
		t.Errorf("an empty cell: %v", err)
	}
	ok("the mocked request named collection was received exactly 1 time") // the failed step added nothing

	ok("none of the mocked POST requests to path /v1/collections have the payload properties:",
		[]string{"reference", "PX-REG-1402"}, []string{"weightGrams", "650"})
	if err := run("none of the mocked POST requests to path /v1/collections on courier have the payload properties:",
		[]string{"reference", "PX-REG-1402"}, []string{"weightGrams", `"650"`}); !core.IsAssertion(err) ||
		!strings.Contains(err.Error(), "1 POST request(s) to path /v1/collections on courier have the payload properties of the table:\n  POST /v1/collections") {
		t.Errorf("none with the string: %v", err)
	}
	d := sc.Descriptions()["mock"].(map[string]any)
	if b, _ := json.Marshal(d["pattern"]); !strings.Contains(string(b), `"payloadProperties":{"$.reference":"PX-REG-1402","$.weightGrams":"650"}`) {
		t.Errorf("the failure's pattern shows the properties: %s", b)
	}
}
