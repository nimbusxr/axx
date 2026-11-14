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

	"golang.org/x/net/http/httpproxy"
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

// sandboxWithoutLocalNames is how a run reaches things in a sandbox like the
// one that showed this: every process has a proxy for what is not on the
// bypass list, which says localhost and not its subdomains, and DNS there has
// no name under localhost. Loopback addresses themselves connect.
func sandboxWithoutLocalNames(t *testing.T) Options {
	t.Helper()
	env := &httpproxy.Config{
		HTTPProxy:  "http://run:s3cret@127.0.0.1:1",
		HTTPSProxy: "http://run:s3cret@127.0.0.1:1",
		NoProxy:    "localhost,127.0.0.1,::1,169.254.0.0/16,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
	}
	return Options{
		Proxy: env.ProxyFunc(),
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			if net.ParseIP(host) == nil {
				return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
			}
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
}

// TestANameUnderLocalhostIsThisMachine: a page at app.localhost loads from
// the machine's own port, as it does in a browser with no proxy, where the
// run's DNS has no such name and its proxy could not reach one.
func TestANameUnderLocalhostIsThisMachine(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "the depot's board, asked for as %s", r.Host)
	})
	listen := func(t *testing.T, address string, tls bool) (*httptest.Server, string) {
		t.Helper()
		l, err := net.Listen("tcp", address)
		if err != nil {
			t.Skipf("this machine has no %s: %v", address, err)
		}
		srv := httptest.NewUnstartedServer(page)
		_ = srv.Listener.Close()
		srv.Listener = l
		if tls {
			srv.StartTLS()
		} else {
			srv.Start()
		}
		t.Cleanup(srv.Close)
		_, port, _ := net.SplitHostPort(l.Addr().String())
		return srv, port
	}
	get := func(t *testing.T, address string) string {
		t.Helper()
		h, _ := relayTo(t, sandboxWithoutLocalNames(t))
		resp, err := browser(h).Get(address)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return fmt.Sprintf("%d %s", resp.StatusCode, body)
	}

	t.Run("on IPv4", func(t *testing.T) {
		_, port := listen(t, "127.0.0.1:0", false)
		for _, name := range []string{"board.localhost", "Board.Depot.LOCALHOST", "localhost"} {
			host := net.JoinHostPort(name, port)
			if got, want := get(t, "http://"+host+"/"), "200 the depot's board, asked for as "+host; got != want {
				t.Errorf("%s: %q, want %q", name, got, want)
			}
		}
	})
	t.Run("on IPv6", func(t *testing.T) {
		_, port := listen(t, "[::1]:0", false)
		host := net.JoinHostPort("board.localhost", port)
		if got, want := get(t, "http://"+host+"/"), "200 the depot's board, asked for as "+host; got != want {
			t.Errorf("%q, want %q", got, want)
		}
	})
	t.Run("over https", func(t *testing.T) {
		_, port := listen(t, "127.0.0.1:0", true)
		host := net.JoinHostPort("board.localhost", port)
		if got, want := get(t, "https://"+host+"/"), "200 the depot's board, asked for as "+host; got != want {
			t.Errorf("%q, want %q", got, want)
		}
	})
	t.Run("nothing on the port", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, port, _ := net.SplitHostPort(l.Addr().String())
		_ = l.Close()
		got := get(t, "http://"+net.JoinHostPort("board.localhost", port)+"/")
		if !strings.HasPrefix(got, "502 ") || !strings.Contains(got, "nothing answers on this machine's port "+port) {
			t.Errorf("%q, want a 502 that says nothing answers on the port", got)
		}
	})
	t.Run("another name still asks DNS", func(t *testing.T) {
		_, port := listen(t, "127.0.0.1:0", false)
		for _, name := range []string{"board.depot.test", "notlocalhost", "localhost.example"} {
			got := get(t, "http://"+net.JoinHostPort(name, port)+"/")
			if !strings.HasPrefix(got, "502 ") {
				t.Errorf("%s: %q, want a 502: only localhost and the names under it are this machine", name, got)
			}
		}
	})
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
