package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ws "github.com/coder/websocket"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/secrets"
)

// tracker follows a parcel as the parcels portal does: the client says
// which parcel it follows, and gets its latest scan; a client that names no
// parcel is closed with 1008.
type tracker struct {
	open atomic.Int32
	auth atomic.Value
}

func (tr *tracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tr.auth.Store(r.Header.Get("Authorization"))
	c, err := ws.Accept(w, r, nil)
	if err != nil {
		return
	}
	tr.open.Add(1)
	defer tr.open.Add(-1)
	ctx := r.Context()
	for {
		_, msg, err := c.Read(ctx)
		if err != nil {
			return
		}
		var in struct{ Follow string }
		_ = json.Unmarshal(msg, &in)
		if in.Follow == "" {
			_ = c.Close(ws.StatusPolicyViolation, "no parcel to follow")
			return
		}
		scan := `{"reference":"` + in.Follow + `","status":"OUT_FOR_DELIVERY","location":"Leipzig"}`
		_ = c.Write(ctx, ws.MessageText, []byte(scan))
	}
}

func harness(t *testing.T) (*cloudtest.Harness, *tracker, string) {
	t.Helper()
	tr := &tracker{}
	srv := httptest.NewServer(tr)
	t.Cleanup(srv.Close)
	return cloudtest.New(t, Pack()), tr, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestSendAndReceive(t *testing.T) {
	h, tr, url := harness(t)
	h.File("tracking/follow.json", `{"follow": "PX-LIV-5703"}`)
	h.OK("the tracking websocket with the following properties:", [][]string{{"url", url + "/live"}, {"header.Authorization", "Bearer shop-token"}})
	h.OK("a message is sent to the tracking websocket:", `{"follow": "PX-LIV-5701"}`)
	h.OK("the tracking websocket received a message where:", [][]string{{"reference", "PX-LIV-5701"}, {"status", "OUT_FOR_DELIVERY"}, {"recipient", "undefined"}})
	h.OK("the tracking/follow.json message is sent to the tracking websocket")
	h.OK("within 5s the tracking websocket received a message containing 'PX-LIV-5703'")
	_ = h.Fails("within 1s the tracking websocket received a message where:", "No message on the tracking websocket met the conditions within 1s",
		[][]string{{"status", "DELIVERED"}})
	if tr.auth.Load() != "Bearer shop-token" {
		t.Errorf("header: %v", tr.auth.Load())
	}
}

func TestACloseCode(t *testing.T) {
	h, _, url := harness(t)
	h.OK("the tracking websocket with the following properties:", [][]string{{"url", url}})
	h.OK("a message is sent to the tracking websocket:", `{}`)
	h.OK("the tracking websocket was closed with code 1008")
	_ = h.Fails("the tracking websocket was closed with code 1000", "The tracking websocket was closed with code 1008 (no parcel to follow), not 1000")
	// A check still waiting fails at once, saying why.
	start := time.Now()
	_ = h.Fails("within 1m the tracking websocket received a message containing 'PX'", "the server closed the connection: 1008 no parcel to follow")
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the check waited %s", d)
	}
}

func TestAnOpenConnectionIsNotClosed(t *testing.T) {
	h, _, url := harness(t)
	h.OK("the tracking websocket with the following properties:", [][]string{{"url", url}})
	_ = h.Fails("within 1s the tracking websocket was closed with code 1000", "The tracking websocket is still open after 1s")
}

// A connection belongs to its scenario: another scenario has none of it,
// and it closes when its scenario ends.
func TestConnectionsBelongToTheirScenario(t *testing.T) {
	h, tr, url := harness(t)
	h.OK("the tracking websocket with the following properties:", [][]string{{"url", url}})
	waitFor(t, func() bool { return tr.open.Load() == 1 })
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return tr.open.Load() == 0 })
	h.NewScenario()
	_ = h.Fails("a message is sent to the tracking websocket:", `no websocket named "tracking" in this scenario`, `{"follow": "PX-LIV-5701"}`)
}

func TestSecretsAreMasked(t *testing.T) {
	const token = "live-token-live-token"
	t.Setenv("TRACKING_TOKEN", token)
	h := cloudtest.New(t, Pack())
	err := h.Fails("the tracking websocket with the following properties:", "could not open the tracking websocket",
		[][]string{{"url", "ws://127.0.0.1:1/live?token=${env:TRACKING_TOKEN}"}})
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), secrets.Masked) {
		t.Errorf("failure: %v", err)
	}
}

func TestProperties(t *testing.T) {
	h := cloudtest.New(t, Pack())
	_ = h.Fails("the tracking websocket with the following properties:", `the websocket property "url" is required`, [][]string{{"subprotocol", "parcels.v1"}})
	_ = h.Fails("the tracking websocket with the following properties:", `unknown websocket property "origin"`, [][]string{{"url", "ws://localhost"}, {"origin", "x"}})
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
