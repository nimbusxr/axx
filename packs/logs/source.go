package logs

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// A log's url says where its lines are:
//
//	file:///var/log/app.log, file://logs/app.log   a file something appends to
//	                                              (relative to axx.yaml)
//	udp://0.0.0.0:5140                            axx listens; one datagram, one or more lines
//	tcp://0.0.0.0:5150                            axx listens; newline-delimited or
//	                                              octet-counted (RFC 6587) messages
//	http://0.0.0.0:5160/logs, https://...         axx listens; the body of each POST or PUT
//
// Everything axx receives on a listener is appended to a file under
// .axx/logs/listen, so reading a log is always reading a file. Listeners are
// opened before the apps start (Prepare), because services connect or send
// when they start; one opened by `axx up` is reused by later runs.
type source struct {
	scheme string // file, udp, tcp, http, https
	path   string // file: the file; http(s): the URL path
	addr   string // host:port for listeners
}

func (s source) network() bool { return s.scheme != "file" }

// key identifies a listener: one per scheme and port.
func (s source) key() string { return s.scheme + "-" + portOf(s.addr) }

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return port
}

func parseSource(projectDir, raw string) (source, error) {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return source{}, fmt.Errorf("invalid log url %q: use file://, udp://, tcp://, http:// or https://", raw)
	}
	switch scheme {
	case "file":
		if rest == "" {
			return source{}, fmt.Errorf("invalid log url %q: no file path", raw)
		}
		p := filepath.FromSlash(rest)
		// file:///C:/logs/app.log on Windows.
		if len(rest) > 3 && rest[0] == '/' && rest[2] == ':' {
			p = filepath.FromSlash(rest[1:])
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(projectDir, p)
		}
		return source{scheme: scheme, path: p}, nil
	case "udp", "tcp", "http", "https":
		host, urlPath, _ := strings.Cut(rest, "/")
		if _, port, err := net.SplitHostPort(host); err != nil || port == "" {
			return source{}, fmt.Errorf("invalid log url %q: %s needs host:port, e.g. %s://0.0.0.0:5140", raw, scheme, scheme)
		}
		s := source{scheme: scheme, addr: host}
		if scheme == "http" || scheme == "https" {
			s.path = "/" + urlPath
		} else if urlPath != "" {
			return source{}, fmt.Errorf("invalid log url %q: %s addresses have no path", raw, scheme)
		}
		return s, nil
	}
	return source{}, fmt.Errorf("invalid log url %q: unknown scheme %q (use file, udp, tcp, http or https)", raw, scheme)
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// file is the file axx reads for the log.
func (s source) file(projectDir string) string {
	if !s.network() {
		return s.path
	}
	name := s.key()
	if s.path != "" && s.path != "/" {
		name += "-" + strings.Trim(unsafeName.ReplaceAllString(s.path, "_"), "_")
	}
	return filepath.Join(listenDir(projectDir), name+".log")
}

func listenDir(projectDir string) string { return filepath.Join(projectDir, ".axx", "logs", "listen") }

// owner is written next to the files of a listener, so another axx process
// (a run while `axx up` holds the listener) reuses it instead of binding.
type owner struct {
	PID   int      `json:"pid"`
	Paths []string `json:"paths,omitempty"`
}

func ownerFile(projectDir string, s source) string {
	return filepath.Join(listenDir(projectDir), s.key()+".owner")
}

// listeners are this process's listeners, per run (suite).
type listeners struct {
	projectDir string

	mu   sync.Mutex
	open map[string]*listener // by source key
}

type listener struct {
	src    source
	remote bool // held by another axx process
	paths  map[string]*sink
	close  func() error
}

// sink appends received lines to a file.
type sink struct {
	mu sync.Mutex
	f  *os.File
}

func (k *sink) write(b []byte) {
	if len(b) == 0 {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	_, _ = k.f.Write(b)
}

func newSink(path string) (*sink, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// A new listener starts a new file; readers only look at what arrives
	// after their scenario started anyway.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &sink{f: f}, nil
}

func runListeners(s *core.Suite) *listeners {
	l, _ := core.Cached(s, "logs.listeners", func() (*listeners, error) {
		l := &listeners{projectDir: s.ProjectDir(), open: map[string]*listener{}}
		s.OnClose(l.closeAll)
		return l, nil
	})
	return l
}

// ensure makes sure something listens for src: this process, or the axx
// process that owns the listener.
func (l *listeners) ensure(src source) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if cur, ok := l.open[src.key()]; ok {
		return l.addPath(cur, src)
	}
	if o, ok := l.liveOwner(src); ok {
		if src.path != "" && src.path != "/" && !contains(o.Paths, src.path) {
			return fmt.Errorf("axx process %d (axx up) listens on %s://%s but not for %s; restart it with `axx down` and `axx up`",
				o.PID, src.scheme, src.addr, src.path)
		}
		l.open[src.key()] = &listener{src: src, remote: true}
		return nil
	}
	lis, err := l.listen(src)
	if err != nil {
		return err
	}
	l.open[src.key()] = lis
	return l.writeOwner(lis)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (l *listeners) liveOwner(src source) (owner, bool) {
	var o owner
	b, err := os.ReadFile(ownerFile(l.projectDir, src))
	if err != nil || json.Unmarshal(b, &o) != nil {
		return o, false
	}
	return o, o.PID != os.Getpid() && lifecycle.ProcessAlive(o.PID)
}

func (l *listeners) writeOwner(lis *listener) error {
	o := owner{PID: os.Getpid()}
	for p := range lis.paths {
		if p != "" {
			o.Paths = append(o.Paths, p)
		}
	}
	sort.Strings(o.Paths)
	b, _ := json.Marshal(o)
	return os.WriteFile(ownerFile(l.projectDir, lis.src), b, 0o644)
}

func (l *listeners) addPath(lis *listener, src source) error {
	if lis.src.scheme != src.scheme {
		return fmt.Errorf("%s://%s and %s://%s use the same port", lis.src.scheme, lis.src.addr, src.scheme, src.addr)
	}
	if lis.remote || src.path == "" {
		return nil
	}
	if _, ok := lis.paths[src.path]; ok {
		return nil
	}
	k, err := newSink(src.file(l.projectDir))
	if err != nil {
		return err
	}
	lis.paths[src.path] = k
	return l.writeOwner(lis)
}

func (l *listeners) closeAll(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var errs []error
	for _, lis := range l.open {
		if lis.remote {
			continue
		}
		errs = append(errs, lis.close())
		_ = os.Remove(ownerFile(l.projectDir, lis.src))
		for _, k := range lis.paths {
			_ = k.f.Close()
		}
	}
	l.open = map[string]*listener{}
	return errors.Join(errs...)
}

// listen opens a listener for src.
func (l *listeners) listen(src source) (*listener, error) {
	lis := &listener{src: src, paths: map[string]*sink{}}
	k, err := newSink(src.file(l.projectDir))
	if err != nil {
		return nil, err
	}
	lis.paths[src.path] = k
	switch src.scheme {
	case "udp":
		pc, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", src.addr)
		if err != nil {
			return nil, listenError(src, err)
		}
		lis.close = pc.Close
		go func() {
			buf := make([]byte, 64*1024)
			for {
				n, _, err := pc.ReadFrom(buf)
				if err != nil {
					return
				}
				k.write(append([]byte(nil), buf[:n]...))
			}
		}()
	case "tcp":
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", src.addr)
		if err != nil {
			return nil, listenError(src, err)
		}
		lis.close = ln.Close
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go readStream(conn, k)
			}
		}()
	case "http", "https":
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", src.addr)
		if err != nil {
			return nil, listenError(src, err)
		}
		srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l.mu.Lock()
			k := lis.paths[r.URL.Path]
			l.mu.Unlock()
			switch {
			case k == nil:
				http.NotFound(w, r)
			case r.Method != http.MethodPost && r.Method != http.MethodPut:
				w.Header().Set("Allow", "POST, PUT")
				w.WriteHeader(http.StatusMethodNotAllowed)
			default:
				body, err := readBody(r)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				k.write(body)
				w.WriteHeader(http.StatusNoContent)
			}
		})}
		if src.scheme == "https" {
			cert, err := selfSigned()
			if err != nil {
				_ = ln.Close()
				return nil, err
			}
			ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		}
		lis.close = func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return srv.Shutdown(ctx)
		}
		go func() { _ = srv.Serve(ln) }()
	}
	return lis, nil
}

func listenError(src source, err error) error {
	return fmt.Errorf("cannot listen for the log on %s://%s: %w", src.scheme, src.addr, err)
}

// readStream appends the messages of a TCP connection: octet-counted syslog
// frames ("123 <14>1 ..."), or newline-delimited lines.
func readStream(conn net.Conn, k *sink) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, 64*1024)
	for {
		if n, ok := octetCount(br); ok {
			msg := make([]byte, n)
			if _, err := io.ReadFull(br, msg); err != nil {
				return
			}
			k.write(msg)
			continue
		}
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			k.write([]byte(strings.TrimRight(string(line), "\r\n")))
		}
		if err != nil {
			return
		}
	}
}

// octetCount reads the "MSG-LEN SP" prefix of an octet-counted frame when
// the next bytes are one ("123 <").
func octetCount(br *bufio.Reader) (int, bool) {
	peek, _ := br.Peek(12)
	i := 0
	for i < len(peek) && peek[i] >= '0' && peek[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(peek) || peek[i] != ' ' || peek[i+1] != '<' {
		return 0, false
	}
	n, err := strconv.Atoi(string(peek[:i]))
	if err != nil || n <= 0 {
		return 0, false
	}
	_, _ = br.Discard(i + 1)
	return n, true
}

func readBody(r *http.Request) ([]byte, error) {
	var body io.Reader = http.MaxBytesReader(nil, r.Body, 32<<20)
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		body = gz
	}
	return io.ReadAll(body)
}

// selfSigned makes a certificate for https listeners; senders must not
// verify it (they are test-only log endpoints).
func selfSigned() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "axx log listener"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		DNSNames:     []string{"localhost", "host.docker.internal"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
