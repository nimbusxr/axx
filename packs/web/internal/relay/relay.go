// Package relay carries the network of a browser the axx host started
// through the run that drives it, so the browser reaches what the run's
// sandbox lets the run reach, and nothing else.
//
// The browser's proxy is the host's side ([Listen]): it reads nothing of
// what the browser sends, and hands each of the browser's connections to a
// connection the run opened to it. The run's side ([Serve]) is the proxy:
// it opens the connections the browser asks for from where the run is,
// through the proxy its environment names (HTTP_PROXY, HTTPS_PROXY,
// ALL_PROXY, NO_PROXY), as the run's other requests go.
//
// The run opens the connections, never the host: a sandbox lets a run
// reach the host's loopback port, and may keep it from listening.
package relay

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// keyLength is the length of a relay's key, in hexadecimal digits.
const keyLength = 64

// waitForRun is how long a connection of the browser waits for one of the
// run's.
const waitForRun = 30 * time.Second

// Host is the host's side of a relay: the proxy of one browser.
type Host struct {
	proxy, tunnel net.Listener
	key           string
	spares        chan net.Conn
	done          chan struct{}
	once          sync.Once
	wg            sync.WaitGroup
}

// Listen starts the host's side on two loopback ports the system picks: one
// for the browser (Proxy), one for the run (Tunnel, with Key).
func Listen() (*Host, error) {
	b := make([]byte, keyLength/2)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	var lc net.ListenConfig
	proxy, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tunnel, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		_ = proxy.Close()
		return nil, err
	}
	h := &Host{proxy: proxy, tunnel: tunnel, key: hex.EncodeToString(b), spares: make(chan net.Conn, 256), done: make(chan struct{})}
	h.wg.Go(h.acceptRun)
	h.wg.Go(h.acceptBrowser)
	return h, nil
}

// Proxy is the address the browser has as its HTTP proxy.
func (h *Host) Proxy() string { return h.proxy.Addr().String() }

// Tunnel is the address the run's side connects to, with Key.
func (h *Host) Tunnel() string { return h.tunnel.Addr().String() }

// Key is what the run's side gives with each connection: only the run the
// host told it carries the browser's network.
func (h *Host) Key() string { return h.key }

// Close ends the relay and every connection through it.
func (h *Host) Close() {
	h.once.Do(func() {
		close(h.done)
		_ = h.proxy.Close()
		_ = h.tunnel.Close()
	})
	h.wg.Wait()
	for {
		select {
		case c := <-h.spares:
			_ = c.Close()
		default:
			return
		}
	}
}

// acceptRun keeps the connections the run opens, each once it gave the key.
func (h *Host) acceptRun() {
	for {
		c, err := h.tunnel.Accept()
		if err != nil {
			return
		}
		h.wg.Go(func() {
			_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
			got := make([]byte, keyLength+1)
			if _, err := io.ReadFull(c, got); err != nil || got[keyLength] != '\n' ||
				subtle.ConstantTimeCompare(got[:keyLength], []byte(h.key)) != 1 {
				_ = c.Close()
				return
			}
			_ = c.SetReadDeadline(time.Time{})
			select {
			case h.spares <- c:
			case <-h.done:
				_ = c.Close()
			}
		})
	}
}

// acceptBrowser joins each of the browser's connections to one of the run's.
func (h *Host) acceptBrowser() {
	for {
		c, err := h.proxy.Accept()
		if err != nil {
			return
		}
		h.wg.Go(func() {
			defer c.Close()
			wait := time.NewTimer(waitForRun)
			defer wait.Stop()
			select {
			case run := <-h.spares:
				defer run.Close()
				stop := make(chan struct{})
				defer close(stop)
				go func() {
					select {
					case <-h.done:
						_ = c.Close()
						_ = run.Close()
					case <-stop:
					}
				}()
				join(c, run)
			case <-wait.C:
			case <-h.done:
			}
		})
	}
}

// join copies each way between two connections until both ways ended.
func join(a, b net.Conn) {
	var wg sync.WaitGroup
	oneWay := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		// The other end hears that this way ended, and may still answer.
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	wg.Go(func() { oneWay(a, b) })
	wg.Go(func() { oneWay(b, a) })
	wg.Wait()
}

// readAhead is a connection whose first bytes a reader already holds.
type readAhead struct {
	net.Conn
	r *bufio.Reader
}

func (c *readAhead) Read(p []byte) (int, error) { return c.r.Read(p) }

// CloseWrite ends the connection's writing, when it can.
func (c *readAhead) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Close()
}

var errClosed = errors.New("relay: closed")

// waitContext is ctx, or done when stop is closed.
func waitContext(ctx context.Context, stop <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
