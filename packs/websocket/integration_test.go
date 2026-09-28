//go:build integration

package websocket

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// TestAgainstAnEchoServer opens a connection to a real WebSocket server,
// which greets each connection and echoes what it is sent.
func TestAgainstAnEchoServer(t *testing.T) {
	addr := cloudtest.Emulator(t, "jmalloc/echo-server:v0.3.7", "8080", "/", nil)
	h := cloudtest.New(t, Pack())
	h.OK("the echo websocket with the following properties:", [][]string{{"url", "ws://" + addr + "/.ws"}})
	h.OK("within 10s the echo websocket received a message containing 'Request served by'")
	h.OK("a message is sent to the echo websocket:", `{"reference": "PX-LIV-5701", "status": "IN_TRANSIT", "location": "Leipzig"}`)
	h.OK("within 10s the echo websocket received a message where:",
		[][]string{{"reference", "PX-LIV-5701"}, {"status", "IN_TRANSIT"}, {"recipient", "undefined"}})
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
