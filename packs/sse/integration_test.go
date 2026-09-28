//go:build integration

package sse

import (
	"net/http"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// TestAgainstWireMock opens an event stream WireMock serves in chunks over
// time, as a service sends events as they happen.
func TestAgainstWireMock(t *testing.T) {
	addr := cloudtest.Emulator(t, "wiremock/wiremock:3.13.2", "8080", "/__admin/health", nil)
	stub := `{
  "request": {"method": "GET", "urlPath": "/api/parcels/PX-LIV-5702/events"},
  "response": {
    "status": 200,
    "headers": {"Content-Type": "text/event-stream"},
    "body": ": connected\n\nevent: scan\nid: 1\ndata: {\"reference\":\"PX-LIV-5702\",\"status\":\"OUT_FOR_DELIVERY\"}\n\nevent: delivered\nid: 2\ndata: {\"reference\":\"PX-LIV-5702\",\"status\":\"DELIVERED\"}\n\n",
    "chunkedDribbleDelay": {"numberOfChunks": 4, "totalDuration": 800}
  }
}`
	res, err := http.Post("http://"+addr+"/__admin/mappings", "application/json", strings.NewReader(stub))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("stub: %d", res.StatusCode)
	}
	h := cloudtest.New(t, Pack())
	h.OK("the tracking event stream with the following properties:", [][]string{{"url", "http://" + addr + "/api/parcels/PX-LIV-5702/events"}})
	h.OK("within 10s the tracking event stream has an event where:", [][]string{{"event type", "scan"}, {"status", "OUT_FOR_DELIVERY"}})
	h.OK("within 10s the tracking event stream has an event where:", [][]string{{"event type", "delivered"}, {"event id", "2"}, {"status", "DELIVERED"}})
	_ = h.Fails("within 10s the tracking event stream has an event containing 'RETURNED'", "the server ended the stream")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
