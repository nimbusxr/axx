package rest

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServiceRegistration(t *testing.T) {
	h := newHarness(t)
	h.fails("the request is executed", "No service set")
	h.fails("the api service with the following properties:", `Property "url" is required`, []string{"openapi", "spec.yaml"})
	h.ok("the api service with the following properties:", []string{"url", "http://${env:HOST}:1"}, []string{"openapi", "space30.yaml"})
	h.fails("the api service with the following properties:", `Service "api" already set`, []string{"url", "http://x"})
	h.ok("the other service with the following properties:", []string{"url", "http://y"})
	h.fails("a GET request to /x on nope", `Service "nope" not set`)
	st := stateKey.Of(h.sc)
	svc, _ := st.services.Default()
	if svc.Name != "api" || svc.URL != "http://127.0.0.1:1" || svc.OpenAPI != "space30.yaml" {
		t.Fatalf("default service %+v", svc)
	}
}

// TestOrderedRequests ports the request-index rules of the original models:
// requests are append-only, the default request is the first one, method
// and path are set once, and a request executes once.
func TestOrderedRequests(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.fails("the request is executed", "No request set for service api")
	h.fails("a 2nd ordered GET request to /x", "Cannot add request at index 1 for service api because the current number of requests is 0")
	h.fails("a 0th ordered GET request to /x", "Cannot add request at index -1 for service api because index cannot be negative")
	h.ok("a 1st ordered GET request to /echo?n=1")
	h.fails("a GET request to /other", "Method already set")
	h.ok("a 2nd ordered post request to /echo?n=2")
	h.ok("a 3rd ordered GET request to /echo?n=3 on api")
	h.fails("a 5th ordered GET request to /x", "because the current number of requests is 3")
	h.fails("the 4th ordered request is executed", "request 4 is not set for service api; the scenario added 3 request(s)")
	h.fails("the response status code is 200", "Response not set")

	h.ok("the 2nd ordered request is executed")
	if got := a.last(); got.Method != http.MethodPost || got.URL != "/echo?n=2" {
		t.Fatalf("sent %+v", got)
	}
	h.ok("the 2nd ordered response status code is 200")
	h.fails("the response status code is 200", "Response not set")
	h.ok("the request is executed on api")
	h.ok("the 3rd ordered request is executed on api")
	h.ok("the response status code is 200 on api")
	h.ok("the 3rd ordered response status code is 200 on api")
	h.ok("the response body contains 'n=3' for 3rd ordered response")
	h.ok("the response body contains 'n=1' for response on api")
	h.ok("the response body contains 'n=1' for 1st ordered response on api")
	n := a.count()
	h.fails("the request is executed", "Response already set")
	if a.count() != n {
		t.Fatal("a request executes only once; the second execution must not be sent")
	}
	h.assertionFails("the response status code is 201", "Expected status code <201> but was <200>.")
}

func TestRequestHeaders(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a GET request to /echo")
	h.ok("the request header Content-Type is 'text/plain'")
	h.ok("the request headers are:",
		[]string{"Content-Type", "application/json"}, // replaces
		[]string{"X-Multi", "a"},
		[]string{"X-Multi", "b"}, // repeats
		[]string{"Accept", "text/html"},
	)
	h.ok("the request header accept is 'application/json'") // replaces, case-insensitive
	h.ok("the request is executed")
	got := a.last().Header
	if got.Get("Content-Type") != "application/json" || len(got.Values("Content-Type")) != 1 {
		t.Errorf("Content-Type %v", got.Values("Content-Type"))
	}
	if v := got.Values("Accept"); len(v) != 1 || v[0] != "application/json" {
		t.Errorf("Accept %v", v)
	}
	if v := got.Values("X-Multi"); len(v) != 2 {
		t.Errorf("X-Multi %v", v)
	}
	if !strings.HasPrefix(got.Get("User-Agent"), "axx") {
		t.Errorf("User-Agent %q", got.Get("User-Agent"))
	}
	h.fails("the request headers for 1st ordered request are:", "row 1 has 1 cells", []string{"X-Only-Name"})

	// Defaults: Accept */*, and the payload's media type without a Content-Type header.
	h2 := newHarness(t)
	h2.service("api", srv.URL, "")
	h2.ok("a POST request to /echo")
	h2.ok("a request payload using an application/json empty content template")
	h2.ok("the request is executed")
	got = a.last().Header
	if got.Get("Accept") != "*/*" || got.Get("Content-Type") != "application/json" {
		t.Errorf("defaults: %v", got)
	}
}

func TestPayloadProperties(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.fails("the request payload property name is 'x'", "Request payload is not set. Use a payload step")
	h.ok("a request payload using an application/json empty content template")
	h.fails("a request payload using an application/json empty content template", "MIME type already set")
	steps := []struct {
		text string
		rows [][]string
		want string
	}{
		{"the request payload property name is 'John Doe'", nil, `{"name":"John Doe"}`},
		{"the request payload property age is '30'", nil, `{"name":"John Doe","age":30}`},
		{"the request payload property age is '31'", nil, `{"name":"John Doe","age":31}`},
		{`the request payload property age is '"thirty"'`, nil, `{"name":"John Doe","age":"thirty"}`},
		{"the request payload property active is 'TRUE'", nil, `"active":true`},
		{"the request payload property active is 'yes'", nil, `"active":false`}, // Boolean.parseBoolean
		{"the request payload property price is '19.99'", nil, `"price":19.99`},
		{"the request payload property big is '3000000000'", nil, `"big":3000000000`},
		{"the request payload property address is '{\"city\": \"LA\"}'", nil, `"address":{"city":"LA"}`},
		{"the request payload property address.city is 'NY'", nil, `"address":{"city":"NY"}`},
		{"the request payload property tags is '[\"b\", \"c\"]'", nil, `"tags":["b","c"]`},
		{"the request payload property launchDate is 'soon'", nil, `"launchDate":"soon"`},
		{"the request payload property launchDate is null", nil, `"launchDate":null`},
		{"the request payload properties are:", [][]string{
			{"launchDate", "2024-01-01"}, // was null: axx infers the type
			{"name", "undefined"},
			{"extra", `"null"`},
			{"gone", "NULL"},
		}, ""},
	}
	for _, s := range steps {
		if s.text == "the request payload properties are:" {
			h.fails(s.text, "Property not found in payload", s.rows...) // gone does not exist
			s.rows = s.rows[:3]
		}
		h.ok(s.text, s.rows...)
		if s.want == "" {
			continue
		}
		svc, _ := stateKey.Of(h.sc).services.Default()
		if p := *svc.requests[0].Payload; !strings.Contains(p, s.want) {
			t.Fatalf("%s: payload %s does not contain %s", s.text, p, s.want)
		}
	}
	h.fails("the request payload property price is 'cheap'", `Invalid value "cheap" for property "price"`)
	h.fails("the request payload property nothing is null", "Property not found in payload")
	h.ok("the request is executed")
	body := a.last().Body
	for _, want := range []string{`"launchDate":"2024-01-01"`, `"extra":"null"`, `"tags":["b","c"]`} {
		if !strings.Contains(body, want) {
			t.Errorf("sent %s, missing %s", body, want)
		}
	}
	if strings.Contains(body, `"name"`) {
		t.Errorf("undefined must remove the property: %s", body)
	}
}

func TestPayloadPropertyNullValueFix(t *testing.T) {
	_, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.ok("a request payload using an application/json empty content template")
	h.ok("the request payload property n is '1'")
	h.ok("the request payload property n is null")
	h.ok("the request payload property n is '42'") // was null
	svc, _ := stateKey.Of(h.sc).services.Default()
	if p := *svc.requests[0].Payload; p != `{"n":42}` {
		t.Fatalf("payload %s", p)
	}
}

func TestFormEncoding(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.ok("a request payload using an application/x-www-form-urlencoded empty content template")
	h.ok("the request payload properties are:",
		[]string{"grant_type", "client_credentials"},
		[]string{"redirect_uri", "https://example.com/cb"},
		[]string{"metadata", `"{"priority": "example", "budget": 0}"`},
		[]string{"nested", `{"a": 1, "b": [1, 2]}`},
	)
	h.ok("the request is executed")
	got := a.last()
	want := "grant_type=client_credentials&redirect_uri=https%3A%2F%2Fexample.com%2Fcb" +
		"&metadata=%7B%22priority%22%3A+%22example%22%2C+%22budget%22%3A+0%7D" +
		"&nested=%7B%22a%22%3A1%2C%22b%22%3A%5B1%2C2%5D%7D"
	if got.Body != want {
		t.Fatalf("form body\n got %s\nwant %s", got.Body, want)
	}
	if got.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type %q", got.Header.Get("Content-Type"))
	}

	h2 := newHarness(t)
	h2.service("api", srv.URL, "")
	h2.ok("a POST request to /echo")
	h2.ok("a request payload using an application/problem+json empty content template")
	h2.fails("the request payload property a is '1'", "Request content type is not supported for payload property validation: application/problem+json")

	// An empty template is sent as an empty form.
	h3 := newHarness(t)
	h3.service("api", srv.URL, "")
	h3.ok("a POST request to /echo")
	h3.ok("a request payload using an application/x-www-form-urlencoded empty content template")
	h3.ok("the request is executed")
	if got := a.last(); got.Body != "" || got.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("empty form: %+v", got)
	}
}

func TestResponseAssertions(t *testing.T) {
	_, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a GET request to /headers")
	h.ok("the request is executed")

	// Headers.
	h.ok("the response header x-request-id is 'req-123'")
	h.ok("the response header Access-Control-Allow-Origin is 'test.example.com'")
	h.ok("the response header Content-Type matches ^application/json.*$")
	h.ok("the response header X-Custom is missing")
	h.ok("the response headers are:", []string{"Access-Control-Allow-Origin", "*"}, []string{"Access-Control-Allow-Origin", "test.example.com"})
	h.ok("the response headers match:", []string{"X-Request-Id", `req-\d+`})
	h.ok("the response headers are missing:", []string{"X-Custom"}, []string{"X-Other"})
	h.assertionFails("the response header X-Request-Id is 'nope'", "Response header X-Request-Id does not have the expected value")
	h.assertionFails("the response header X-Request-Id matches req", "does not match req") // full match
	h.assertionFails("the response header X-Request-Id is missing", "is present")
	ae := h.assertion("the response headers are:", "2 expectations failed",
		[]string{"X-Request-Id", "a"}, []string{"X-Absent", "b"}, []string{"Access-Control-Allow-Origin", "*"})
	if !strings.Contains(ae.Message, "<missing>") {
		t.Errorf("message %s", ae.Message)
	}
	h.fails("the response header X matches [", "invalid regular expression")

	// Payload properties: charset in the Content-Type is ignored.
	h.ok("the response payload property name is 'x'")
	h.ok("the response payload properties are:",
		[]string{"count", "3"},
		[]string{"ratio", "1.5"},
		[]string{"ok", "true"},
		[]string{"none", "null"},
		[]string{"missing", "UNDEFINED"},
		[]string{"list", "[1,2]"},
		[]string{"obj", `{"a":"b"}`},
		[]string{"name", `"x"`},
	)
	h.ok("the response payload property none is null")
	h.ok("the response payload property nope is undefined")
	h.ok("the response payload property name matches ^x$")
	h.ok("the response payload properties match:", []string{"name", "[a-z]"}, []string{"obj.a", "b"})
	ae = h.assertion("the response payload property count is '3.0'", "Response payload property count is not 3.0")
	if ae.Expected != jsonScalar("3.0") || ae.Actual != jsonScalar("3") {
		t.Errorf("expected/actual %v / %v", ae.Expected, ae.Actual)
	}
	h.assertionFails("the response payload property count is '\"3\"'", "is not")
	h.assertionFails("the response payload property name is null", "is not null")
	h.assertionFails("the response payload property name is undefined", "is not undefined")
	h.assertionFails("the response payload property count matches 3", "does not match") // not a string
	ae = h.assertion("the response payload properties are:", "2 expectations failed",
		[]string{"count", "4"}, []string{"name", "x"}, []string{"none", "x"})
	if !strings.Contains(ae.Message, "count") || !strings.Contains(ae.Message, "none") {
		t.Errorf("message %s", ae.Message)
	}
	h.fails("the response payload property [ is 'x'", "InvalidPath")

	// Body.
	h.ok("the response body contains '\"ratio\":1.5'")
	h.assertionFails("the response body contains 'absent'", "doesn't contain")
}

func TestResponseContentTypes(t *testing.T) {
	_, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a GET request to /vnd")
	h.ok("a 2nd ordered GET request to /text")
	h.ok("a 3rd ordered POST request to /api/launches?fail=1")
	h.ok("a request payload using an application/json empty content template for 3rd ordered request")
	for _, s := range []string{"the request is executed", "the 2nd ordered request is executed", "the 3rd ordered request is executed"} {
		h.ok(s)
	}
	h.ok("the response payload property data.id is '\"7\"'") // +json
	h.fails("the response payload property a is 'b' for 2nd ordered response", "Response content type is not supported for payload property validation: text/plain")
	h.ok("the response payload property title is 'Bad Request' for 3rd ordered response")
	h.ok("the response payload property status is '400' for 3rd ordered response")
	// The other property checks never looked at the content type.
	h.ok("the response payload property a is undefined for 2nd ordered response")
}

func TestDescribe(t *testing.T) {
	_, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /api/launches")
	h.ok("the request header Content-Type is 'application/json'")
	h.ok("a request payload using an application/json empty content template")
	h.ok("the request payload property name is 'Falcon'")
	if h.sc.Descriptions() != nil {
		t.Fatal("nothing to describe before execution")
	}
	h.ok("the request is executed")

	d := h.sc.Descriptions()["rest"].(map[string]any)
	if d["service"] != "api" || d["response"].(map[string]any)["status"] != 201 ||
		d["request"].(map[string]any)["body"] != `{"name":"Falcon"}` {
		t.Fatalf("describe %v", d)
	}
}

func TestExamplePayloads(t *testing.T) {
	a, srv := newAPI(t)
	cases := []struct {
		spec, step, want string
	}{
		// First example in document order ("Zeta" precedes "Alpha").
		{"space30.yaml", "a request payload using an application/json content example", `{"name":"Zeta","flight_number":7,"fuel_ratio":1.0,"approved":true,"crew":[{"first_name":"Jane","last_name":"Speed"}]}`},
		{"space30.yaml", "a request payload using an application/json content example named 'Alpha Launch'", `{"name":"Alpha","flight_number":1,"fuel_ratio":0.5,"approved":false}`},
		{"space30.yaml", "a request payload using an application/json content example named 'External Launch'", `{"name":"External","flight_number":3,"fuel_ratio":2.0}`},
		{"space31.json", "a request payload using an application/json content example", `{"name":"Second","flight_number":2,"tags":["b"],"big":12345678901234567890}`},
	}
	for _, c := range cases {
		h := newHarness(t)
		h.service("api", srv.URL, c.spec)
		h.ok("a POST request to /api/launches")
		h.ok(c.step)
		h.ok("the request header Content-Type is 'application/json'")
		h.ok("the request is executed")
		if got := strings.TrimSpace(a.last().Body); got != c.want {
			t.Errorf("%s: sent\n %s\nwant\n %s", c.step, got, c.want)
		}
	}

	h := newHarness(t)
	h.service("api", srv.URL, "space30.yaml")
	h.ok("a POST request to /api/launches")
	h.fails("a request payload using an application/json content example named 'Nope'", `No example with name "Nope" found for request content`)
	h.fails("a request payload using a text/json content example", "No content type found for request to get example")
	h.fails("a request payload using a text/xml content example", "Unsupported content type: text/xml")
}

func TestExampleLookupErrors(t *testing.T) {
	_, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "space30.yaml")
	h.service("plain", srv.URL, "")
	for _, c := range []struct{ request, want string }{
		{"a GET request to /nowhere", "No path found for request to get example"},
		{"a 2nd ordered DELETE request to /api/launches", "no DELETE operation found"},
		{"a 3rd ordered GET request to /api/launches", "No request body found"},
		{"a 4th ordered POST request to /api/launches/search", "No content type found"},
	} {
		h.ok(c.request)
	}
	h.fails("a request payload using an application/json content example", "No path found for request to get example")
	h.fails("a request payload using an application/json content example for 2nd ordered request", "no DELETE operation found")
	h.fails("a request payload using an application/json content example for 3rd ordered request", "No request body found")
	h.fails("a request payload using an application/json content example for 4th ordered request", "No content type found")
	h.ok("a POST request to /api/launches on plain")
	h.fails("a request payload using an application/json content example for request on plain", "has no openapi property")

	h2 := newHarness(t)
	h2.service("api", srv.URL, "missing.yaml")
	h2.ok("a POST request to /api/launches")
	h2.fails("a request payload using an application/json content example", "OpenAPI specification missing.yaml of service api")
}

func TestSpecFromURLAndExternalExample(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/api-docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"openapi":"3.0.1","info":{"title":"t","version":"1"},"paths":{"/api/things":{"post":{
			"requestBody":{"content":{"application/json":{"examples":{"Ext":{"externalValue":"../openapi/thing.json"}}}}},
			"responses":{"201":{"description":"ok"}}}}}}`))
	})
	mux.HandleFunc("/openapi/thing.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"thing":1}`)) })
	var got string
	mux.HandleFunc("/api/things", func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 64)
		n, _ := r.Body.Read(b)
		got = string(b[:n])
		w.WriteHeader(http.StatusCreated)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	h := newHarness(t)
	h.service("api", srv.URL, srv.URL+"/v3/api-docs")
	h.ok("a POST request to /api/things")
	h.ok("a request payload using an application/json content example")
	h.ok("the request is executed")
	if got != `{"thing":1}` {
		t.Fatalf("sent %q", got)
	}
}

func TestTLSVerification(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the rejected handshake is expected
	srv.StartTLS()
	defer srv.Close()
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a GET request to /")
	h.ok("the request is executed") // self-signed: verification is off by default
	h.ok("the response status code is 204")

	h2 := newHarness(t, withPackConfig("rest", `{"tls":{"verify":true}}`))
	h2.service("api", srv.URL, "")
	h2.ok("a GET request to /")
	h2.fails("the request is executed", "certificate")
}

func TestTimeoutAndRedirects(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a GET request to /slow")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	h.sc.SetContext(ctx)
	start := time.Now()
	h.fails("the request is executed", "deadline exceeded")
	if time.Since(start) > 2*time.Second {
		t.Fatal("the step context must bound the request")
	}
	h.sc.SetContext(context.Background())

	h.ok("a 2nd ordered GET request to /redirect")
	h.ok("a 3rd ordered POST request to /redirect")
	h.ok("the 2nd ordered request is executed")
	h.ok("the 2nd ordered response status code is 200") // GET follows
	if a.last().URL != "/api/launches" {
		t.Fatalf("last %v", a.last())
	}
	h.ok("the 3rd ordered request is executed")
	h.ok("the 3rd ordered response status code is 302") // POST does not
}

func TestResolveURL(t *testing.T) {
	for _, c := range []struct{ base, path, want, validation string }{
		{"http://h:1", "/api/x?a=1", "http://h:1/api/x?a=1", "http://h:1/api/x?a=1"},
		{"http://h:1/", "/api/x", "http://h:1/api/x", "http://h:1/api/x"},
		{"http://h:1/ctx", "/api/x", "http://h:1/ctx/api/x", "http://h:1/api/x"},
		{"http://h:1/ctx/", "api/x", "http://h:1/ctx/api/x", "http://h:1/api/x"},
		{"http://h:1", "http://other:2/y", "http://other:2/y", "http://other:2/y"},
	} {
		got, err := resolveURL(c.base, c.path)
		if err != nil || got != c.want {
			t.Errorf("resolveURL(%s, %s) = %s, %v", c.base, c.path, got, err)
		}
		if v := validationPath(got, c.path); v != c.validation {
			t.Errorf("validationPath(%s, %s) = %s, want %s", got, c.path, v, c.validation)
		}
	}
	if _, err := resolveURL("localhost:8080", "/x"); err == nil {
		t.Error("a service URL must be absolute")
	}
}

func TestMediaTypes(t *testing.T) {
	for ct, want := range map[string]bool{
		"application/json": true, "application/json;charset=UTF-8": true, "Application/JSON; charset=utf-8": true,
		"text/json": true, "application/problem+json": true, "application/vnd.api+json": true,
		"text/plain": false, "": false, "application/x-www-form-urlencoded": false, "application/jsonx": false,
	} {
		if got := isJSONMediaType(mediaTypeOf(ct)); got != want {
			t.Errorf("%q: %v", ct, got)
		}
	}
}

func TestPackConfigErrors(t *testing.T) {
	h := newHarness(t, withPackConfig("rest", `{"tls":{"verify":"yes"}}`))
	if err := (pack{}).Init(context.Background(), h.suite); err == nil || !strings.Contains(err.Error(), "packs.rest") {
		t.Fatalf("Init: %v", err)
	}
	m := pack{}.Manifest()
	if !json.Valid(m.ConfigSchema) {
		t.Fatal("config schema is not JSON")
	}
}
