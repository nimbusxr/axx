package a2a

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// The SDK decodes a data part to plain Go values, whose numbers are float64s:
// 6.0 comes out as 6, and 9007199254740993 as 9007199254740992. It holds what
// it decoded in a task and its events, with no way to the JSON behind them, and
// a check reads a value with its type and its exact digits (ADR 0014). So
// exactData hands it each data part's JSON as text, which it keeps as it is,
// for artifactView to read back. gRPC has no such JSON: a data part is a
// protobuf Struct there, whose numbers are all doubles.

// heldData starts the text that holds a data part's JSON.
const heldData = "\x00axx-json:"

// exactData is a JSON transport's round trips, with the data parts of what the
// agent answers held as text: in a JSON body, and in each event of a stream.
type exactData struct{ next http.RoundTripper }

func (t exactData) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.next.RoundTrip(req)
	if err != nil || res.StatusCode != http.StatusOK {
		return res, err
	}
	kind := res.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(kind, "text/event-stream"):
		res.Body = &heldEvents{Closer: res.Body, from: bufio.NewReader(res.Body)}
	case strings.Contains(kind, "json"):
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		if held, ok := holdData(body); ok {
			body = held
		}
		res.Body = io.NopCloser(bytes.NewReader(body))
		res.ContentLength = -1
		res.Header.Del("Content-Length")
	}
	return res, nil
}

// holdData is a JSON message with its parts' data held as text, or false for
// what is not JSON, which is left to the SDK to say.
func holdData(body []byte) ([]byte, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil, false
	}
	holdParts(v)
	out, err := json.Marshal(v)
	return out, err == nil
}

// holdParts holds the data of every list of parts: an artifact's, and a
// message's. What a part or its owner calls metadata is the agent's own.
func holdParts(v any) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			holdParts(e)
		}
	case map[string]any:
		for k, e := range x {
			if k == "metadata" {
				continue
			}
			if parts, ok := e.([]any); ok && k == "parts" {
				for _, p := range parts {
					holdPart(p)
				}
				continue
			}
			holdParts(e)
		}
	}
}

func holdPart(p any) {
	part, ok := p.(map[string]any)
	if !ok || part["data"] == nil {
		return
	}
	if b, err := json.Marshal(part["data"]); err == nil {
		part["data"] = heldData + string(b)
	}
}

// exactly is a part's data as the agent wrote it, when it was held.
func exactly(data any) any {
	if s, ok := data.(string); ok {
		if raw, ok := strings.CutPrefix(s, heldData); ok {
			return json.RawMessage(raw)
		}
	}
	return data
}

// heldEvents is an event stream with its events' data parts held as text.
type heldEvents struct {
	io.Closer
	from *bufio.Reader
	data [][]byte     // the data lines so far of an event
	out  bytes.Buffer // what is ready to read
	err  error
}

func (e *heldEvents) Read(p []byte) (int, error) {
	for e.out.Len() == 0 {
		if e.err != nil {
			return 0, e.err
		}
		e.line()
	}
	return e.out.Read(p)
}

// line reads a line of the stream: an event's data waits for the blank line
// that ends the event, and any other line passes.
func (e *heldEvents) line() {
	line, err := e.from.ReadBytes('\n')
	text := bytes.TrimRight(line, "\r\n")
	if data, ok := bytes.CutPrefix(text, []byte("data:")); ok {
		e.data = append(e.data, bytes.TrimPrefix(data, []byte(" ")))
	} else {
		if len(text) == 0 {
			e.event()
		}
		e.out.Write(line)
	}
	if err != nil {
		e.event()
		e.err = err
	}
}

func (e *heldEvents) event() {
	if len(e.data) == 0 {
		return
	}
	if held, ok := holdData(bytes.Join(e.data, []byte("\n"))); ok {
		e.data = [][]byte{held}
	}
	for _, d := range e.data {
		e.out.WriteString("data: ")
		e.out.Write(d)
		e.out.WriteByte('\n')
	}
	e.data = nil
}
