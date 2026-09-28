package sse

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/packs/asyncapi"
)

// tracking streams a parcel's tracking as the parcels service does: a scan
// event, then a delivered event that ends the stream.
type tracking struct{ open atomic.Int32 }

func (e *tracking) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/PX-LIV-5702/events") {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"title":"no such parcel"}`)
		return
	}
	e.open.Add(1)
	defer e.open.Add(-1)
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	fmt.Fprint(w, ": keep-alive\n\nevent: scan\nid: 1\ndata: {\"reference\":\"PX-LIV-5702\",\"status\":\"OUT_FOR_DELIVERY\"}\n\n")
	fl.Flush()
	if r.URL.Query().Get("hold") != "" {
		<-r.Context().Done()
		return
	}
	time.Sleep(200 * time.Millisecond)
	fmt.Fprint(w, "event: delivered\nid: 2\ndata: {\"reference\":\"PX-LIV-5702\",\"status\":\"DELIVERED\"}\n\n")
	fl.Flush()
}

func harness(t *testing.T) (*cloudtest.Harness, *tracking, string) {
	t.Helper()
	e := &tracking{}
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return cloudtest.New(t, Pack()), e, srv.URL
}

func TestEvents(t *testing.T) {
	h, _, url := harness(t)
	h.OK("the tracking event stream with the following properties:", [][]string{{"url", url + "/api/parcels/PX-LIV-5702/events"}})
	h.OK("the tracking event stream has an event where:", [][]string{{"event type", "scan"}, {"event id", "1"}, {"status", "OUT_FOR_DELIVERY"}})
	h.OK("within 5s the tracking event stream has an event where:", [][]string{{"event type", "delivered"}, {"reference", "PX-LIV-5702"}, {"status", "DELIVERED"}})
	h.OK("the tracking event stream has an event containing 'DELIVERED'")
	// The server ended the stream: a check that finds nothing fails at once.
	start := time.Now()
	_ = h.Fails("within 1m the tracking event stream has an event where:", "No event on the tracking event stream met the conditions: the server ended the stream",
		[][]string{{"event type", "returned"}})
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the check waited %s", d)
	}
}

func TestAStreamThatIsNotOne(t *testing.T) {
	h, _, url := harness(t)
	_ = h.Fails("the tracking event stream with the following properties:", `answered 404 with "application/problem+json", not an event stream: {"title":"no such parcel"}`,
		[][]string{{"url", url + "/api/parcels/PX-LIV-5799/events"}})
}

// A stream belongs to its scenario, which closes it when it ends.
func TestStreamsBelongToTheirScenario(t *testing.T) {
	h, e, url := harness(t)
	h.OK("the tracking event stream with the following properties:", [][]string{{"url", url + "/api/parcels/PX-LIV-5702/events?hold=1"}})
	waitFor(t, func() bool { return e.open.Load() == 1 })
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return e.open.Load() == 0 })
	h.NewScenario()
	_ = h.Fails("the tracking event stream has an event containing 'PX'", `no event stream named "tracking" in this scenario`)
}

func TestSecretsAreMasked(t *testing.T) {
	const token = "feed-token-feed-token"
	t.Setenv("TRACKING_TOKEN", token)
	h := cloudtest.New(t, Pack())
	err := h.Fails("the tracking event stream with the following properties:", "could not open the tracking event stream",
		[][]string{{"url", "http://127.0.0.1:1/events?token=${env:TRACKING_TOKEN}"}})
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), secrets.Masked) {
		t.Errorf("failure: %v", err)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for !ok() {
		select {
		case <-ctx.Done():
			t.Fatal("timed out")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// trackingContract is the tracking stream's AsyncAPI document: an event's
// type names its message.
const trackingContract = `asyncapi: 3.0.0
info: {title: Parcel tracking, version: 1.0.0}
channels:
  tracking:
    address: /api/parcels/{reference}/events
    messages:
      scan:
        name: scan
        payload:
          type: object
          required: [reference, status]
      delivered:
        name: delivered
        payload:
          type: object
          required: [reference, status, signedBy]
`

func TestContract(t *testing.T) {
	e := &tracking{}
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	h := cloudtest.New(t, asyncapi.Pack(), Pack())
	h.Start(h.Plan())
	h.File("asyncapi.yaml", trackingContract)
	h.OK("the tracking event stream with the following properties:",
		[][]string{{"url", srv.URL + "/api/parcels/PX-LIV-5702/events"}, {"asyncapi", "asyncapi.yaml"}})
	h.OK("the tracking event stream has an event where:", [][]string{{"event type", "scan"}, {"status", "OUT_FOR_DELIVERY"}})
	_ = h.Fails("within 5s the tracking event stream has an event containing 'DELIVERED'",
		"- validation.message.payload.schema.required: payload $: missing property 'signedBy' (the delivered message of the tracking channel)")
}
