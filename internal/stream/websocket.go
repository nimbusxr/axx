package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// maxMessage bounds one WebSocket message. The library's default, 32 KiB,
// would drop the connection on bigger ones.
const maxMessage = 16 << 20

// handshakeTimeout bounds opening a connection.
const handshakeTimeout = 10 * time.Second

// WebSocket is an open WebSocket connection.
type WebSocket struct {
	conn   *websocket.Conn
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	code   int
	reason string
	closed bool
}

// Received is a message a WebSocket received.
type Received struct {
	Data   []byte
	Binary bool
}

// DialWebSocket opens a WebSocket connection and reads it until it closes,
// calling receive for each message and closed once, with the close code
// and reason the server sent (-1 when the connection broke without one). The
// connection lives until Close, whatever happens to the context of the step
// that opened it.
func DialWebSocket(url string, header http.Header, subprotocols []string, receive func(Received), closed func(code int, reason string)) (*WebSocket, error) {
	dial, cancelDial := context.WithTimeout(context.Background(), handshakeTimeout)
	defer cancelDial()
	conn, res, err := websocket.Dial(dial, url, &websocket.DialOptions{HTTPHeader: header, Subprotocols: subprotocols})
	var body []byte
	if res != nil && res.Body != nil {
		// On a refusal, the library keeps the start of what the server answered.
		body, _ = io.ReadAll(io.LimitReader(res.Body, 2<<10))
		_ = res.Body.Close()
	}
	if err != nil {
		if res != nil && res.StatusCode != http.StatusSwitchingProtocols {
			return nil, fmt.Errorf("the server answered %d with %q, not a websocket: %s",
				res.StatusCode, res.Header.Get("Content-Type"), strings.TrimSpace(string(body)))
		}
		return nil, err
	}
	conn.SetReadLimit(maxMessage)
	ctx, cancel := context.WithCancel(context.Background())
	w := &WebSocket{conn: conn, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return // closed by us
				}
				code, reason := -1, err.Error()
				var ce websocket.CloseError
				if errors.As(err, &ce) {
					code, reason = int(ce.Code), ce.Reason
				}
				w.mu.Lock()
				w.code, w.reason, w.closed = code, reason, true
				w.mu.Unlock()
				closed(code, reason)
				return
			}
			receive(Received{Data: data, Binary: typ == websocket.MessageBinary})
		}
	}()
	return w, nil
}

// Send sends a text message.
func (w *WebSocket) Send(ctx context.Context, data []byte) error {
	return w.conn.Write(ctx, websocket.MessageText, data)
}

// Closed reports the code and reason the server closed the connection with,
// if it did.
func (w *WebSocket) Closed() (code int, reason string, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.code, w.reason, w.closed
}

// Close closes the connection normally and waits for its reader.
func (w *WebSocket) Close() {
	_ = w.conn.Close(websocket.StatusNormalClosure, "")
	w.cancel()
	<-w.done
}
