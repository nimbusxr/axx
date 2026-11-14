package relay

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/proxy"
)

// idle is how many connections the run keeps open to the host, waiting for
// the browser's: a page opens several at once.
const idle = 8

// Options are how the run's side reaches what the browser asks for. The
// zero value reaches it as the run's environment says.
type Options struct {
	// Dial opens a connection from where the run is (default: direct, or
	// through the SOCKS proxy ALL_PROXY names).
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Proxy is the HTTP proxy a request goes through, nil for none
	// (default: HTTP_PROXY, HTTPS_PROXY and NO_PROXY).
	Proxy func(*url.URL) (*url.URL, error)
	// Logger hears what the browser could not reach. Nil discards.
	Logger *slog.Logger
}

// Run is the run's side of a relay: the proxy the browser's connections
// reach through the host.
type Run struct {
	tunnel, key string
	opts        Options
	srv         *http.Server
	conns       chan net.Conn
	done        chan struct{}
	once        sync.Once
	wg          sync.WaitGroup
	carried     atomic.Int64
}

// Serve connects the run's side to the host's (its Tunnel and Key), and
// serves the browser's connections until Close.
func Serve(tunnel, key string, opts Options) (*Run, error) {
	if opts.Dial == nil {
		d := proxy.FromEnvironmentUsing(&net.Dialer{Timeout: 30 * time.Second})
		opts.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if cd, ok := d.(proxy.ContextDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return d.Dial(network, addr)
		}
	}
	if opts.Proxy == nil {
		opts.Proxy = httpproxy.FromEnvironment().ProxyFunc()
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	opts.Dial, opts.Proxy = thisMachine(opts.Dial), noProxyForThisMachine(opts.Proxy)
	r := &Run{tunnel: tunnel, key: key, opts: opts, conns: make(chan net.Conn), done: make(chan struct{})}
	// The first connection says whether the host's side is there.
	first, err := r.open()
	if err != nil {
		return nil, fmt.Errorf("cannot reach the browser's network relay in the axx host (%s): %w", tunnel, err)
	}
	forward := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL, pr.Out.Host = pr.In.URL, pr.In.Host
		},
		Transport: &http.Transport{
			Proxy:       func(req *http.Request) (*url.URL, error) { return opts.Proxy(req.URL) },
			DialContext: opts.Dial,
			// What the browser sent and what the site answers, as they are.
			DisableCompression: true,
			IdleConnTimeout:    30 * time.Second,
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			r.unreachable(req.URL.Host, err)
			http.Error(w, "axx: the run cannot reach "+req.URL.Host+": "+err.Error(), http.StatusBadGateway)
		},
	}
	r.srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			r.carried.Add(1)
			switch {
			case req.Method == http.MethodConnect:
				r.connect(w, req)
			case req.URL.IsAbs() && (req.URL.Scheme == "http" || req.URL.Scheme == "https"):
				forward.ServeHTTP(w, req)
			case req.URL.IsAbs() && (req.URL.Scheme == "ws" || req.URL.Scheme == "wss"):
				// WebKit asks its proxy for a WebSocket by its own address;
				// the request is the same as one for http that upgrades.
				req.URL.Scheme = map[string]string{"ws": "http", "wss": "https"}[req.URL.Scheme]
				forward.ServeHTTP(w, req)
			default:
				http.Error(w, "axx: this is the proxy of a browser the axx host started", http.StatusBadRequest)
			}
		}),
		ErrorLog: slog.NewLogLogger(opts.Logger.Handler(), slog.LevelDebug),
	}
	r.wg.Go(func() { _ = r.srv.Serve(listener{r}) })
	r.wg.Go(func() { r.wait(first) })
	for range idle - 1 {
		r.wg.Go(func() { r.wait(nil) })
	}
	return r, nil
}

// Carried is how many requests of the browser went through the run.
func (r *Run) Carried() int64 { return r.carried.Load() }

// Close ends the run's side and the connections through it.
func (r *Run) Close() {
	r.once.Do(func() {
		close(r.done)
		_ = r.srv.Close()
	})
	r.wg.Wait()
}

// open opens a connection to the host's side, and gives the key.
func (r *Run) open() (net.Conn, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.Dial("tcp", r.tunnel)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(c, r.key+"\n"); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// wait holds a connection open until the host joins a browser's connection
// to it (its first bytes arrive), hands it to the proxy and, as it is no
// longer idle, opens the next.
func (r *Run) wait(c net.Conn) {
	for failures := 0; ; {
		if c == nil {
			var err error
			if c, err = r.open(); err != nil {
				// The host's side is gone, or busy: the browser is, or waits.
				if failures++; failures > 20 {
					return
				}
				select {
				case <-time.After(time.Duration(failures) * 50 * time.Millisecond):
					c = nil
					continue
				case <-r.done:
					return
				}
			}
		}
		failures = 0
		stop := make(chan struct{})
		go func(c net.Conn) {
			select {
			case <-r.done:
				_ = c.Close()
			case <-stop:
			}
		}(c)
		br := bufio.NewReader(c)
		_, err := br.Peek(1)
		close(stop)
		if err != nil {
			_ = c.Close()
			select {
			case <-r.done:
				return
			default:
			}
			// The host ended it: its side is closing, which the next open
			// finds out.
			c = nil
			failures++
			if failures > 20 {
				return
			}
			continue
		}
		select {
		case r.conns <- &readAhead{Conn: c, r: br}:
		case <-r.done:
			_ = c.Close()
			return
		}
		c = nil
	}
}

// listener gives the proxy the connections the host joined a browser's to.
type listener struct{ r *Run }

func (l listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.r.conns:
		return c, nil
	case <-l.r.done:
		return nil, errClosed
	}
}
func (l listener) Close() error   { return nil }
func (l listener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// connect opens the connection a CONNECT asks for (an https site, a
// WebSocket) from where the run is, and joins the browser's to it.
func (r *Run) connect(w http.ResponseWriter, req *http.Request) {
	ctx, cancel := waitContext(req.Context(), r.done)
	defer cancel()
	dst, err := r.dial(ctx, req.Host)
	if err != nil {
		r.unreachable(req.Host, err)
		http.Error(w, "axx: the run cannot reach "+req.Host+": "+err.Error(), http.StatusBadGateway)
		return
	}
	defer dst.Close()
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "axx: cannot take the connection over", http.StatusInternalServerError)
		return
	}
	src, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer src.Close()
	if _, err := io.WriteString(src, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-r.done:
			_ = src.Close()
			_ = dst.Close()
		case <-stop:
		}
	}()
	// What the browser sent after its CONNECT is the site's.
	join(&readAhead{Conn: src, r: rw.Reader}, dst)
}

// dial opens a connection to addr (host:port) as the run's environment
// says: through the HTTP proxy HTTPS_PROXY names, with a CONNECT of its
// own, or as Options.Dial does.
func (r *Run) dial(ctx context.Context, addr string) (net.Conn, error) {
	via, err := r.opts.Proxy(&url.URL{Scheme: "https", Host: addr})
	if err != nil {
		return nil, err
	}
	if via == nil || (via.Scheme != "http" && via.Scheme != "https") {
		return r.opts.Dial(ctx, "tcp", addr)
	}
	proxyAddr := via.Host
	if via.Port() == "" {
		proxyAddr = net.JoinHostPort(via.Hostname(), map[string]string{"http": "80", "https": "443"}[via.Scheme])
	}
	c, err := (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("the proxy %s: %w", via.Host, err)
	}
	if via.Scheme == "https" {
		c = tls.Client(c, &tls.Config{ServerName: via.Hostname(), MinVersion: tls.VersionTLS12})
	}
	ask := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: http.Header{}}
	if u := via.User; u != nil {
		pass, _ := u.Password()
		ask.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.Username()+":"+pass)))
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	if err := ask.Write(c); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("the proxy %s: %w", via.Host, err)
	}
	br := bufio.NewReader(c)
	// What follows the proxy's answer on the connection is the site's, not
	// a body to read and close.
	resp, err := http.ReadResponse(br, ask) //nolint:bodyclose // see above
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("the proxy %s: %w", via.Host, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = c.Close()
		return nil, fmt.Errorf("the proxy %s refused it: %s", via.Host, resp.Status)
	}
	return &readAhead{Conn: c, r: br}, nil
}

// isThisMachine reports whether host is localhost or a name under it
// (app.localhost): the machine itself, by definition (RFC 6761), which a
// browser connects to without asking DNS or a proxy.
func isThisMachine(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// thisMachine is dial, with localhost and the names under it connected to the
// machine's loopback addresses, IPv6's and then IPv4's, as a browser does.
// The browser the host started leaves its names to the run, and where the run
// is, DNS need not have them: a sandbox's has no app.localhost.
func thisMachine(dial func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || !isThisMachine(host) {
			return dial(ctx, network, addr)
		}
		c, err6 := dial(ctx, network, net.JoinHostPort("::1", port))
		if err6 == nil {
			return c, nil
		}
		c, err4 := dial(ctx, network, net.JoinHostPort("127.0.0.1", port))
		if err4 == nil {
			return c, nil
		}
		return nil, fmt.Errorf("nothing answers on this machine's port %s: %w", port, errors.Join(err4, err6))
	}
}

// noProxyForThisMachine is proxy, with no proxy for localhost and the names
// under it: a proxy would look for them on its own machine.
func noProxyForThisMachine(proxy func(*url.URL) (*url.URL, error)) func(*url.URL) (*url.URL, error) {
	return func(u *url.URL) (*url.URL, error) {
		if isThisMachine(u.Hostname()) {
			return nil, nil
		}
		return proxy(u)
	}
}

// unreachable says, for someone who looks at the run's log, what the
// browser asked for and the run could not reach. A browser asks for much
// of its own (its updates, its accounts), so it is no warning.
func (r *Run) unreachable(host string, err error) {
	r.opts.Logger.Debug(fmt.Sprintf("the browser asked for %s, which this run cannot reach: %v", host, err))
}
