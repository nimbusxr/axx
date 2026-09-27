package rest

import (
	"net/http"
	"slices"
	"testing"
)

// TestRequestHeaderChanges changes a request's headers the ways a custom
// step can, and checks that the server gets them.
func TestRequestHeaderChanges(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a GET request to /echo")
	h.ok("the request headers are:", []string{"X-Shop", "kestrel-books"}, []string{"X-Draft", "yes"})
	svc, err := Context(h.sc).Service()
	if err != nil {
		t.Fatal(err)
	}
	req, err := svc.Request(0)
	if err != nil {
		t.Fatal(err)
	}
	req.Header().Set("Authorization", "Bearer shop-token")
	req.Header().Add("X-Shop", "lark-ceramics")
	req.Header().Del("X-Draft")
	req.SetHeader("Accept", "application/json") // replaces the default
	req.SetHeader("X-Trace", "a")
	req.SetHeader("X-Trace", "b") // adds
	h.ok("the request is executed")

	got := a.last().Header
	for name, want := range map[string][]string{
		"Authorization": {"Bearer shop-token"},
		"X-Shop":        {"kestrel-books", "lark-ceramics"},
		"X-Draft":       nil,
		"Accept":        {"application/json"},
		"X-Trace":       {"a", "b"},
	} {
		if v := got.Values(name); !slices.Equal(v, want) {
			t.Errorf("%s: the server got %q, want %q", name, v, want)
		}
	}
	// The headers added when the request is sent are the exchange's.
	if ua := req.Header().Get("User-Agent"); ua != "" {
		t.Errorf("the request's headers have User-Agent %q", ua)
	}
	if ua := req.Exchange().RequestHeader.Get("User-Agent"); ua == "" {
		t.Error("the exchange's request headers have no User-Agent")
	}

	// A request a custom step adds, and a request built by hand.
	r2 := svc.AddRequest(http.MethodPost, "/echo")
	r2.Header().Set("X-Parcels-Signature", "t=1,v1=5f2b")
	h.ok("the 2nd ordered request is executed")
	if v := a.last().Header.Get("X-Parcels-Signature"); v != "t=1,v1=5f2b" {
		t.Errorf("X-Parcels-Signature: the server got %q", v)
	}
	var r3 Request
	r3.Header().Set("X-Shop", "kestrel-books")
	if v := r3.Header().Get("X-Shop"); v != "kestrel-books" {
		t.Errorf("a Request's zero value keeps no headers: %q", v)
	}
}
