package relay

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// browser is an HTTP client that has the host's side as its proxy, as the
// browser the host starts has.
func browser(h *Host) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(&url.URL{Scheme: "http", Host: h.Proxy()}),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // a test server's certificate
		},
	}
}

// relayTo starts a relay whose run side reaches names as reach maps them
// (a name only the run's side knows shows the request went through it).
func relayTo(t *testing.T, opts Options) (*Host, *Run) {
	t.Helper()
	h, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	r, err := Serve(h.Tunnel(), h.Key(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return h, r
}

func noProxy(*url.URL) (*url.URL, error) { return nil, nil }

// onlyFromTheRun reaches the depot's board by a name only the run's side
// resolves, and refuses every other name.
func onlyFromTheRun(board string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if host != "board.depot.test" {
			return nil, fmt.Errorf("the sandbox refuses %s", addr)
		}
		var d net.Dialer
		return d.DialContext(ctx, network, board)
	}
}

func TestABrowsersRequestsGoThroughTheRun(t *testing.T) {
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Connection") != "" || r.Header.Get("X-Forwarded-For") != "" {
			t.Errorf("the site got the proxy's headers: %v", r.Header)
		}
		fmt.Fprintf(w, "held parcels of %s: PX-HLD-1001 (%s)", r.Host, r.URL.RequestURI())
	}))
	defer board.Close()
	h, run := relayTo(t, Options{Dial: onlyFromTheRun(board.Listener.Addr().String()), Proxy: noProxy})

	resp, err := browser(h).Get("http://board.depot.test/held?depot=leeds")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if got, want := string(body), "held parcels of board.depot.test: PX-HLD-1001 (/held?depot=leeds)"; got != want {
		t.Errorf("the page: %q, want %q", got, want)
	}
	if run.Carried() == 0 {
		t.Error("the run's side carried no request")
	}
}

func TestAnHTTPSSiteIsReachedFromTheRun(t *testing.T) {
	board := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "PX-HLD-1001")
	}))
	defer board.Close()
	h, _ := relayTo(t, Options{Dial: onlyFromTheRun(board.Listener.Addr().String()), Proxy: noProxy})

	resp, err := browser(h).Get("https://board.depot.test/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "PX-HLD-1001" {
		t.Errorf("the page: %q", body)
	}
}

func TestWhatTheRunCannotReachTheBrowserCannot(t *testing.T) {
	board := httptest.NewServer(http.NotFoundHandler())
	defer board.Close()
	h, _ := relayTo(t, Options{Dial: onlyFromTheRun(board.Listener.Addr().String()), Proxy: noProxy})

	// The test server answers on this machine, and the host could reach it:
	// the run's side cannot, so the browser cannot.
	direct := "http://" + board.Listener.Addr().String() + "/"
	resp, err := browser(h).Get(direct)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "the sandbox refuses") {
		t.Errorf("an address the run cannot reach: %d %q, want a 502 that says why", resp.StatusCode, body)
	}
	if resp, err := browser(h).Get("https://elsewhere.example/"); err == nil {
		_ = resp.Body.Close()
		t.Error("an https site the run cannot reach was reached")
	}
}

// TestTheRunsProxyIsUsed: a sandbox that lets its processes out only through
// a proxy (HTTPS_PROXY) lets the browser out through it too.
func TestTheRunsProxyIsUsed(t *testing.T) {
	board := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "PX-HLD-1001")
	}))
	defer board.Close()
	var asked atomic.Value
	sandboxProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		asked.Store(r.Host + " " + r.Header.Get("Proxy-Authorization"))
		if r.Host != "board.depot.test:443" {
			http.Error(w, "not an allowed site", http.StatusForbidden)
			return
		}
		dst, err := net.Dial("tcp", board.Listener.Addr().String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer dst.Close()
		src, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer src.Close()
		_, _ = io.WriteString(src, "HTTP/1.1 200 OK\r\n\r\n")
		join(src, dst)
	}))
	defer sandboxProxy.Close()
	via, _ := url.Parse(sandboxProxy.URL)
	via.User = url.UserPassword("run", "s3cret")
	h, _ := relayTo(t, Options{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("no direct connections")
		},
		Proxy: func(*url.URL) (*url.URL, error) { return via, nil },
	})

	resp, err := browser(h).Get("https://board.depot.test/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "PX-HLD-1001" {
		t.Errorf("the page: %q", body)
	}
	if got, want := asked.Load(), "board.depot.test:443 Basic cnVuOnMzY3JldA=="; got != want {
		t.Errorf("the sandbox's proxy was asked %q, want %q", got, want)
	}
	if resp, err := browser(h).Get("https://elsewhere.example/"); err == nil {
		_ = resp.Body.Close()
		t.Error("a site the sandbox's proxy refuses was reached")
	}
}

func TestOnlyTheRunWithTheKeyCarriesTheBrowsersNetwork(t *testing.T) {
	h, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	// Another process on the machine that found the port: without the key,
	// the host ends its connection, and gives it none of the browser's.
	c, err := net.Dial("tcp", h.Tunnel())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = io.WriteString(c, strings.Repeat("0", keyLength)+"\n")
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Error("a connection without the key was kept")
	}
	select {
	case <-h.spares:
		t.Error("a connection without the key waits for the browser's")
	default:
	}
}

func TestManyConnectionsAtOnce(t *testing.T) {
	var live, most atomic.Int64
	release := make(chan struct{})
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := live.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		<-release
		live.Add(-1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer board.Close()
	h, _ := relayTo(t, Options{Dial: onlyFromTheRun(board.Listener.Addr().String()), Proxy: noProxy})

	// More at once than the run keeps idle: each connection taken is replaced.
	const pages = 3 * idle
	var wg sync.WaitGroup
	errs := make(chan error, pages)
	for range pages {
		wg.Go(func() {
			c := browser(h)
			c.Transport.(*http.Transport).DisableKeepAlives = true
			resp, err := c.Get("http://board.depot.test/")
			if err != nil {
				errs <- err
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		})
	}
	deadline := time.Now().Add(10 * time.Second)
	for most.Load() < pages && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if most.Load() != pages {
		t.Errorf("%d requests were at the site at once, want %d", most.Load(), pages)
	}
}

func TestClosingEndsTheConnections(t *testing.T) {
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer board.Close()
	h, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	run, err := Serve(h.Tunnel(), h.Key(), Options{Dial: onlyFromTheRun(board.Listener.Addr().String()), Proxy: noProxy})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := browser(h).Get("http://board.depot.test/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	done := make(chan struct{})
	go func() {
		run.Close()
		h.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("closing waits for a connection the browser still has")
	}
	if _, err := Serve(h.Tunnel(), h.Key(), Options{}); err == nil {
		t.Error("a run's side connected to a relay that closed")
	}
}
