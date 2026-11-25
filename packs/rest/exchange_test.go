package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// parcelsAPI answers a POST with 201 and the parcel, a GET of /api/labels
// with a body longer than an exchange shows, and a GET of /api/label.png
// with one that is no text.
func parcelsAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		switch {
		case r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Set-Cookie", "session=abc")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"reference":"PX-1","token":"s3cret-value"}`))
		case r.URL.Path == "/api/labels":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(strings.Repeat("x", describeBodyLimit+100)))
		case r.URL.Path == "/api/label.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', 0xff, 0xfe})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A step that registers a service says that the scenario talks to it, and an
// executed request what went to it and what came back: the method and the
// path, the status, and both bodies, with no header and no secret; and where
// the service was reached, without the user and password its URL may carry.
func TestAnExecutedRequestSaysItsExchange(t *testing.T) {
	srv := parcelsAPI(t)
	h := newHarness(t)
	secrets.Keep(h.sc, "s3cret-value")
	h.service("parcels", strings.Replace(srv.URL, "http://", "http://depot:pa55word@", 1), "")
	h.ok("a POST request to /api/parcels?source=web")
	h.ok("the request header Authorization is 'Bearer s3cret-value'")
	h.ok("a request payload using an application/json empty content template")
	h.ok("the request payload properties are:", []string{"reference", "PX-1"})
	h.ok("the request is executed")
	h.ok("the response status code is 201")

	got := h.sc.Exchanges()
	if len(got) != 2 {
		t.Fatalf("exchanges: %+v, want the registration and the request", got)
	}
	if reg := got[0]; reg.Peer != "parcels" || reg.Kind != "service" || reg.Sent != nil || reg.Seen != nil || reg.At.IsZero() {
		t.Errorf("the registration: %+v", reg)
	}
	x := got[1]
	if x.Peer != "parcels" || x.Kind != "service" || x.From != "" || x.Sent == nil || x.Seen == nil {
		t.Fatalf("the request: %+v", x)
	}
	if x.Address != srv.URL {
		t.Errorf("where the service was reached: %q, want %q", x.Address, srv.URL)
	}
	if x.Sent.Title != "POST /api/parcels?source=web" || x.Sent.MediaType != "application/json" || x.Sent.Body != `{"reference":"PX-1"}` {
		t.Errorf("sent: %+v", x.Sent)
	}
	if x.Seen.Title != "201 Created" || x.Seen.MediaType != "application/json" ||
		x.Seen.Body != `{"reference":"PX-1","token":"`+secrets.Masked+`"}` {
		t.Errorf("seen: %+v", x.Seen)
	}
	all, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{"s3cret-value", "Bearer", "Authorization", "session=abc", "depot", "pa55word"} {
		if strings.Contains(string(all), kept) {
			t.Errorf("the exchanges should not have %q: %s", kept, all)
		}
	}
}

// A long body is cut where the failure description cuts it, a body that is
// no text is left out, and a request with no body has none.
func TestAnExchangesBodyIsCutAndIsText(t *testing.T) {
	srv := parcelsAPI(t)
	h := newHarness(t)
	h.service("parcels", srv.URL, "")
	h.ok("a GET request to /api/labels")
	h.ok("a 2nd ordered GET request to /api/label.png")
	h.ok("a 3rd ordered DELETE request to /api/parcels/PX-1")
	h.ok("the request is executed")
	h.ok("the 2nd ordered request is executed")
	h.ok("the 3rd ordered request is executed")

	got := h.sc.Exchanges()
	if len(got) != 4 {
		t.Fatalf("exchanges: %+v", got)
	}
	long, image, deleted := got[1], got[2], got[3]
	if long.Sent.Title != "GET /api/labels" || long.Sent.Body != "" || long.Sent.MediaType != "" {
		t.Errorf("a request with no body: %+v", long.Sent)
	}
	if want := strings.Repeat("x", describeBodyLimit) + "... (100 more bytes)"; long.Seen.Title != "200 OK" || long.Seen.Body != want {
		t.Errorf("a long body: %q (%d bytes)", long.Seen.Title, len(long.Seen.Body))
	}
	if image.Seen.MediaType != "image/png" || image.Seen.Body != "" {
		t.Errorf("a body that is no text: %+v", image.Seen)
	}
	if deleted.Sent.Title != "DELETE /api/parcels/PX-1" || deleted.Seen.Title != "204 No Content" || deleted.Seen.Body != "" {
		t.Errorf("a response with no body: %+v, %+v", deleted.Sent, deleted.Seen)
	}
}

// A request that never reached the service says nothing.
func TestARequestThatFailsSaysNoExchange(t *testing.T) {
	srv := parcelsAPI(t)
	url := srv.URL
	srv.Close()
	h := newHarness(t)
	h.service("parcels", url, "")
	h.ok("a GET request to /api/parcels/PX-1")
	if err := h.step("the request is executed"); err == nil {
		t.Fatal("the request should fail: nothing listens")
	}
	if got := h.sc.Exchanges(); len(got) != 1 || got[0] != (core.Exchange{Peer: "parcels", Kind: "service", At: got[0].At}) {
		t.Errorf("exchanges: %+v, want the registration only", got)
	}
}
