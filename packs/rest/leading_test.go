package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The property steps' forms with the ordinal first are the steps they say:
// the same request or response, read the same way, on the default service or
// a named one.
func TestOrdinalFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/first":
			_, _ = w.Write([]byte(`{"reference": "PX-1"}`))
		default:
			_, _ = w.Write([]byte(`{"reference": "PX-2", "note": null, "zip": "53111", "count": 3}`))
		}
	}))
	t.Cleanup(srv.Close)
	for _, on := range []string{"", " on api"} {
		h := newHarness(t)
		h.service("api", srv.URL, "")
		h.ok("a GET request to /first")
		h.ok("a 2nd ordered GET request to /second")
		h.ok("the request is executed")
		h.ok("the 2nd ordered request is executed")

		h.ok("the 2nd ordered response payload property reference is 'PX-2'" + on)
		h.ok("the 2nd ordered response payload property note is null" + on)
		h.ok("the 2nd ordered response payload property gone is undefined" + on)
		h.ok("the 2nd ordered response payload property reference matches ^PX-[0-9]$" + on)
		h.ok("the 2nd ordered response payload properties"+on+" are:", []string{"reference", "PX-2"}, []string{"zip", `"53111"`}, []string{"count", "3"})
		h.ok("the 2nd ordered response payload properties"+on+" match:", []string{"reference", "^PX-2$"})
		h.ok("the 1st ordered response payload property reference is 'PX-1'" + on)

		// It is the second response these read, not the first.
		h.fails("the 2nd ordered response payload property reference is 'PX-1'"+on, "reference")
		h.fails("the 2nd ordered response payload properties"+on+" are:", "zip", []string{"zip", "53111"})
		h.fails("the 3rd ordered response payload property reference is 'PX-3'"+on, "")
	}
}

func TestOrdinalFirstSetsTheRequest(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		if r.URL.Path == "/second" {
			got = string(b[:n])
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /first")
	h.ok("a 2nd ordered POST request to /second")
	h.ok("a request payload using an application/json empty content template for 1st ordered request")
	h.ok("a request payload using an application/json empty content template for 2nd ordered request")
	h.ok("the 2nd ordered request payload property reference is 'PX-2'")
	h.ok("the 2nd ordered request payload properties are:", []string{"count", "3"}, []string{"zip", `"53111"`}, []string{"note", "x"})
	h.ok("the 2nd ordered request payload property note is null on api")
	h.ok("the request is executed")
	h.ok("the 2nd ordered request is executed")
	want := `{"reference":"PX-2","count":3,"zip":"53111","note":null}`
	if got != want {
		t.Errorf("the second request's body\n got %s\nwant %s", got, want)
	}
}

// An integer without quotes says what the same integer in the step's own
// quotes says, and `'"…"'` is the string (ADR 0014).
func TestBareInteger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"weightGrams": 2500, "zip": "2500", "negative": -3}`))
	}))
	t.Cleanup(srv.Close)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a GET request to /parcel")
	h.ok("the request is executed")
	for _, ok := range []string{
		"the response payload property weightGrams is 2500",
		"the response payload property weightGrams is '2500'",
		"the response payload property negative is -3",
		"the response payload property weightGrams is 2500 for 1st ordered response",
		"the response payload property weightGrams is 2500 for response on api",
		"the 1st ordered response payload property weightGrams is 2500",
		"the 1st ordered response payload property weightGrams is 2500 on api",
		`the response payload property zip is '"2500"'`,
	} {
		h.ok(ok)
	}
	// A hint says what to write in the step's own text, whose quotes are the
	// step's: `is "2500"` is the integer again.
	h.fails("the response payload property zip is 2500", `zip is the string "2500": write '"2500"'`)
	h.fails(`the response payload property zip is "2500"`, `zip is the string "2500": write '"2500"'`)
	h.fails(`the response payload property weightGrams is '"2500"'`, "weightGrams is the integer 2500: write '2500'")
}

func TestABareIntegerSetsARequestProperty(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 256)
		n, _ := r.Body.Read(b)
		got = string(b[:n])
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /parcel")
	h.ok("a request payload using an application/json empty content template")
	h.ok("the request payload property weightGrams is 2500")
	h.ok("the 1st ordered request payload property count is 3 on api")
	h.ok("the request is executed")
	if want := `{"weightGrams":2500,"count":3}`; got != want {
		t.Errorf("body %s, want %s", got, want)
	}
}
