package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The SDK decodes a result's structured content to plain Go values, whose
// numbers are float64s: 6.0 comes out as 6, and 9007199254740993 as
// 9007199254740992. A check reads a value with its type and its exact digits
// (ADR 0014), so a tool call also keeps its result's JSON as the server wrote
// it. The SDK offers no way to it: its middleware sees the decoded result, so
// the JSON is kept where the answer passes on its way to the SDK. That is
// exactConn for stdio, and exactBodies for streamable HTTP, whose connection
// cannot be wrapped: the SDK tells it the session's protocol version through
// an unexported method a wrapper would hide.

// rawResult is where a call's transport leaves the JSON of its result. A call
// that takes several rounds leaves the last one's.
type rawResult struct {
	mu   sync.Mutex
	json json.RawMessage
}

func (r *rawResult) keep(raw json.RawMessage) {
	r.mu.Lock()
	r.json = raw
	r.mu.Unlock()
}

func (r *rawResult) get() json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.json
}

type rawResultKey struct{}

// withRawResult makes a context whose call leaves its result's JSON in the
// rawResult. The context reaches the transports with the call's request.
func withRawResult(ctx context.Context) (context.Context, *rawResult) {
	r := &rawResult{}
	return context.WithValue(ctx, rawResultKey{}, r), r
}

func rawResultOf(ctx context.Context) *rawResult {
	r, _ := ctx.Value(rawResultKey{}).(*rawResult)
	return r
}

// exactTransport keeps the results of a transport whose connection can be
// wrapped.
type exactTransport struct{ sdk.Transport }

func (t exactTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	c, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &exactConn{Connection: c, calls: map[jsonrpc.ID]*rawResult{}}, nil
}

// exactConn gives each response's result to the call that sent its request.
type exactConn struct {
	sdk.Connection
	mu    sync.Mutex
	calls map[jsonrpc.ID]*rawResult
}

func (c *exactConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	if r := rawResultOf(ctx); r != nil {
		if req, ok := msg.(*jsonrpc.Request); ok && req.IsCall() {
			c.mu.Lock()
			c.calls[req.ID] = r
			c.mu.Unlock()
		}
	}
	return c.Connection.Write(ctx, msg)
}

func (c *exactConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if res, ok := msg.(*jsonrpc.Response); ok {
		c.mu.Lock()
		r := c.calls[res.ID]
		delete(c.calls, res.ID)
		c.mu.Unlock()
		if r != nil && res.Error == nil {
			r.keep(res.Result)
		}
	}
	return msg, err
}

// exactBodies keeps the result in the response to a call's request: its JSON,
// or a message of its event stream.
type exactBodies struct{ next http.RoundTripper }

func (t exactBodies) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.next.RoundTrip(req)
	if r := rawResultOf(req.Context()); err == nil && r != nil {
		res.Body = &resultBody{
			ReadCloser: res.Body,
			result:     r,
			events:     strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream"),
		}
	}
	return res, err
}

// resultBody reads a response's messages as the SDK reads the response: the
// result is kept before the SDK has the bytes of its message.
type resultBody struct {
	io.ReadCloser
	result *rawResult
	events bool
	unread []byte // what is not read as a line yet, or the whole of a JSON body
	data   []byte // the data lines so far of an event
}

func (b *resultBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.unread = append(b.unread, p[:n]...)
	if b.events {
		b.lines()
	}
	if err != nil {
		b.ended()
	}
	return n, err
}

// lines reads the event stream's whole lines.
func (b *resultBody) lines() {
	for {
		i := bytes.IndexByte(b.unread, '\n')
		if i < 0 {
			return
		}
		b.line(bytes.TrimSuffix(b.unread[:i], []byte("\r")))
		b.unread = b.unread[i+1:]
	}
}

func (b *resultBody) line(line []byte) {
	if len(line) == 0 {
		b.message(b.data)
		b.data = nil
		return
	}
	if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
		if b.data != nil {
			b.data = append(b.data, '\n')
		}
		b.data = append(b.data, bytes.TrimPrefix(data, []byte(" "))...)
	}
}

func (b *resultBody) ended() {
	if b.events {
		if len(b.unread) > 0 {
			b.line(b.unread)
		}
		b.message(b.data)
	} else {
		b.message(b.unread)
	}
	b.unread, b.data = nil, nil
}

func (b *resultBody) message(data []byte) {
	if len(data) == 0 {
		return
	}
	msg, err := jsonrpc.DecodeMessage(data)
	if err != nil {
		return // the SDK says so
	}
	if res, ok := msg.(*jsonrpc.Response); ok && res.Error == nil {
		b.result.keep(res.Result)
	}
}
