// Package stream holds the clients of connections a scenario keeps open and
// reads as things arrive: server-sent event streams and WebSockets. They
// report what arrives through callbacks, so that any pack can collect it.
package stream

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

// Event is one server-sent event.
type Event struct {
	// Type is the event's type: its event: field, or "message".
	Type string
	// ID is the last event ID the stream set, as of this event.
	ID string
	// Data is its data: lines, joined with line breaks.
	Data string
}

// maxLine bounds one line of an event stream.
const maxLine = 16 << 20

// ReadEvents reads an event stream from r as the HTML standard does and
// calls emit for each event, until r ends. Lines end with CR, LF or CRLF;
// a leading byte order mark is dropped; comment lines (":…") are ignored;
// an event without data is not dispatched; an event the stream did not
// finish with a blank line is discarded.
func ReadEvents(r io.Reader, emit func(Event)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	sc.Split(splitLines)
	var data strings.Builder
	var typ, lastID string
	hasData, first := false, true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		if line == "" {
			if hasData {
				ev := Event{Type: typ, ID: lastID, Data: strings.TrimSuffix(data.String(), "\n")}
				if ev.Type == "" {
					ev.Type = "message"
				}
				emit(ev)
			}
			data.Reset()
			typ, hasData = "", false
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}
		switch name {
		case "event":
			typ = value
		case "data":
			data.WriteString(value)
			data.WriteByte('\n')
			hasData = true
		case "id":
			if !strings.ContainsRune(value, 0) {
				lastID = value
			}
		}
	}
	return sc.Err()
}

// splitLines splits at CR, LF and CRLF.
func splitLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\n' {
			return i + 1, data[:i], nil
		}
		if i+1 < len(data) {
			if data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
		if atEOF {
			return i + 1, data[:i], nil
		}
		return 0, nil, nil // a CR at the end: wait for what follows it
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// EventStream is an open event stream.
type EventStream struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// OpenEvents requests the event stream at url and reads it until it ends
// or is closed, calling emit for each event and ended once, with the error
// that ended it (nil when the server finished the stream). The stream lives
// until Close, whatever happens to the context of the step that opened it.
// It fails, with the body, on a status other than 200 or a content type
// other than text/event-stream.
func OpenEvents(url string, header http.Header, emit func(Event), ended func(error)) (*EventStream, error) {
	return openEvents(http.MethodGet, url, header, nil, emit, ended)
}

// PostEvents is OpenEvents for a stream that answers a POST of body, as
// GraphQL over server-sent events has it.
func PostEvents(url string, header http.Header, body []byte, emit func(Event), ended func(error)) (*EventStream, error) {
	return openEvents(http.MethodPost, url, header, body, emit, ended)
}

func openEvents(method, url string, header http.Header, body []byte, emit func(Event), ended func(error)) (*EventStream, error) {
	ctx, cancel := context.WithCancel(context.Background())
	var content io.Reader
	if body != nil {
		content = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, content)
	if err != nil {
		cancel()
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	client := &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ResponseHeaderTimeout: 10 * time.Second,
		DisableCompression:    true, // compression buffers events
	}}
	res, err := client.Do(req) //nolint:bodyclose // closed below, or by the goroutine reading the stream
	if err != nil {
		cancel()
		return nil, err
	}
	media, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if res.StatusCode != http.StatusOK || media != "text/event-stream" {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<10))
		_ = res.Body.Close()
		cancel()
		return nil, fmt.Errorf("the event stream answered %d with %q, not an event stream: %s",
			res.StatusCode, res.Header.Get("Content-Type"), strings.TrimSpace(string(body)))
	}
	s := &EventStream{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer func() { _ = res.Body.Close() }()
		err := ReadEvents(res.Body, emit)
		if ctx.Err() != nil {
			return // closed
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			err = nil
		}
		ended(err)
	}()
	return s, nil
}

// Close ends the stream and waits for its reader.
func (s *EventStream) Close() {
	s.cancel()
	<-s.done
}
