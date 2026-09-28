package stream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func events(t *testing.T, stream string) []Event {
	t.Helper()
	var got []Event
	if err := ReadEvents(strings.NewReader(stream), func(e Event) { got = append(got, e) }); err != nil {
		t.Fatal(err)
	}
	return got
}

// The event stream format, as the HTML standard reads it.
func TestReadEvents(t *testing.T) {
	for _, c := range []struct {
		name, stream string
		want         []Event
	}{
		{
			"a typed event with an id", "event: scan\nid: 7\ndata: {\"status\":\"IN_TRANSIT\"}\n\n",
			[]Event{{Type: "scan", ID: "7", Data: `{"status":"IN_TRANSIT"}`}},
		},
		{"no type is message", "data: hello\n\n", []Event{{Type: "message", Data: "hello"}}},
		{"data lines join", "data: first\ndata: second\n\n", []Event{{Type: "message", Data: "first\nsecond"}}},
		{"CR and CRLF end lines", "event: scan\r\ndata: a\rdata: b\r\n\r\n", []Event{{Type: "scan", Data: "a\nb"}}},
		{"a byte order mark is dropped", "\uFEFFdata: x\n\n", []Event{{Type: "message", Data: "x"}}},
		{"comments are ignored", ": keep-alive\ndata: x\n\n: another\n", []Event{{Type: "message", Data: "x"}}},
		{"no space after the colon", "data:x\n\n", []Event{{Type: "message", Data: "x"}}},
		{"only one leading space is dropped", "data:  x\n\n", []Event{{Type: "message", Data: " x"}}},
		{"a field without a colon", "data\n\n", []Event{{Type: "message", Data: ""}}},
		{"the last id carries over", "id: 1\ndata: a\n\ndata: b\n\n", []Event{{Type: "message", ID: "1", Data: "a"}, {Type: "message", ID: "1", Data: "b"}}},
		{"an event without data is not sent", "event: scan\n\ndata: x\n\n", []Event{{Type: "message", Data: "x"}}},
		{"an unfinished event is discarded", "data: a\n\ndata: b\n", []Event{{Type: "message", Data: "a"}}},
		{"unknown fields and retry are ignored", "retry: 3000\nfoo: bar\ndata: x\n\n", []Event{{Type: "message", Data: "x"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := events(t, c.stream)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestOpenEvents(t *testing.T) {
	var header http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		fl := w.(http.Flusher)
		fmt.Fprint(w, "event: scan\nid: 1\ndata: {\"status\":\"IN_TRANSIT\"}\n\n")
		fl.Flush()
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "event: delivered\nid: 2\ndata: {\"status\":\"DELIVERED\"}\n\n")
		fl.Flush()
	}))
	defer srv.Close()
	var mu sync.Mutex
	var got []Event
	ended := make(chan error, 1)
	s, err := OpenEvents(srv.URL, http.Header{"Authorization": {"Bearer shop-token"}},
		func(e Event) { mu.Lock(); got = append(got, e); mu.Unlock() }, func(err error) { ended <- err })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	select {
	case err := <-ended:
		if err != nil {
			t.Errorf("ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[1].Type != "delivered" || got[1].ID != "2" {
		t.Errorf("events: %+v", got)
	}
	if header.Get("Accept") != "text/event-stream" || header.Get("Authorization") != "Bearer shop-token" {
		t.Errorf("request header: %v", header)
	}
}

func TestOpenEventsRefusesWhatIsNotAStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"title":"no parcel PX-LIV-5799"}`)
	}))
	defer srv.Close()
	_, err := OpenEvents(srv.URL, nil, func(Event) {}, func(error) {})
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "no parcel PX-LIV-5799") {
		t.Errorf("err: %v", err)
	}
}

// A refused WebSocket says what the server answered instead.
func TestDialWebSocketSaysWhyItWasRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"title":"sign in to follow PX-LIV-5799"}`)
	}))
	defer srv.Close()
	_, err := DialWebSocket("ws"+strings.TrimPrefix(srv.URL, "http"), nil, nil, func(Received) {}, func(int, string) {})
	want := `the server answered 401 with "application/problem+json", not a websocket: {"title":"sign in to follow PX-LIV-5799"}`
	if err == nil || err.Error() != want {
		t.Errorf("err: %v", err)
	}
}

// A stream outlives the context of whatever opened it, until Close.
func TestAStreamLivesUntilClosed(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		select {
		case <-release:
			fmt.Fprint(w, "data: later\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	got := make(chan Event, 1)
	s, err := OpenEvents(srv.URL, nil, func(e Event) { got <- e }, func(error) {})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	close(release)
	select {
	case e := <-got:
		if e.Data != "later" {
			t.Errorf("event: %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
	s.Close()
}

func TestWebSocket(t *testing.T) {
	var protocol, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"parcels.v1"}})
		if err != nil {
			return
		}
		protocol = c.Subprotocol()
		ctx := context.Background()
		_, msg, err := c.Read(ctx)
		if err != nil {
			return
		}
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"echo":`+string(msg)+`}`))
		_ = c.Write(ctx, websocket.MessageText, []byte(strings.Repeat("x", 100<<10)))
		_ = c.Close(websocket.StatusPolicyViolation, "no parcel to follow")
	}))
	defer srv.Close()
	var mu sync.Mutex
	var got []Received
	closed := make(chan string, 1)
	w, err := DialWebSocket("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Authorization": {"Bearer shop-token"}}, []string{"parcels.v1"},
		func(r Received) { mu.Lock(); got = append(got, r); mu.Unlock() },
		func(code int, reason string) { closed <- fmt.Sprintf("%d %s", code, reason) })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Send(context.Background(), []byte(`{"follow":"PX-LIV-5701"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-closed:
		if c != "1008 no parcel to follow" {
			t.Errorf("closed: %s", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not closed")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || string(got[0].Data) != `{"echo":{"follow":"PX-LIV-5701"}}` || len(got[1].Data) != 100<<10 {
		t.Errorf("received %d messages", len(got))
	}
	if code, reason, ok := w.Closed(); !ok || code != 1008 || reason != "no parcel to follow" {
		t.Errorf("Closed: %d %q %v", code, reason, ok)
	}
	if protocol != "parcels.v1" || auth != "Bearer shop-token" {
		t.Errorf("handshake: %q %q", protocol, auth)
	}
}
