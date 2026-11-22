package appium

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A server that crashes under a session drops its connection: the session revives it, becomes
// the new server's session, and sends the command again, once.
func TestSessionRevivesItsServer(t *testing.T) {
	crashed := false
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if !crashed {
			crashed = true
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close() // as a server whose process died
			return
		}
		_, _ = w.Write([]byte(`{"value": "<hierarchy/>"}`))
	}))
	defer srv.Close()
	// Each request on a connection of its own: Go's client sends a GET again by itself when a
	// kept-alive connection drops, which a crashed server's refused connections never are.
	c := &Client{URL: srv.URL, HTTP: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}
	s := &Session{c: c, ID: "first"}
	revived := 0
	s.Revive = func(context.Context) error {
		revived++
		s.Renew("second")
		return nil
	}
	src, err := s.Source(context.Background())
	if err != nil || src != "<hierarchy/>" {
		t.Fatalf("source: %q, %v", src, err)
	}
	if revived != 1 || strings.Join(paths, " ") != "/session/first/source /session/second/source" {
		t.Errorf("revived %d times; requests %v", revived, paths)
	}

	// Without a way to revive it, the session says the server is down.
	crashed = false
	s.Revive = nil
	if _, err := s.Source(context.Background()); !IsDown(err) {
		t.Errorf("a dead server: %v", err)
	}
}

// A connection that is taken and closed at once, as adb's forward does for a device whose server
// is gone, is a server that is down, however Go's client comes to know it: as the end of the
// connection where an answer was due, or, when it sees the connection closed before it sent the
// request, as a "server closed idle connection", which it has no error value for.
func TestAConnectionClosedAtOnceIsAServerDown(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	c := &Client{URL: "http://" + l.Addr().String(), HTTP: &http.Client{Transport: &http.Transport{}}}
	s := &Session{c: c, ID: "first"}
	seen := map[string]int{}
	for range 500 {
		_, err := s.Source(context.Background())
		if err == nil {
			t.Fatal("a source from a server that answers nothing")
		}
		if !IsDown(err) {
			t.Fatalf("not taken for a server that is down: %v", err)
		}
		text := err.Error()
		seen[text[strings.LastIndex(text, ": ")+2:]]++
	}
	t.Logf("as Go's client reported them: %v", seen)
}
