package webcore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/web/internal/driver"
	"github.com/nimbusxr/axx/packs/web/internal/guard"
	"github.com/nimbusxr/axx/packs/web/internal/relay"
)

// A run in an agent's sandbox cannot start a browser there (on macOS, the
// sandbox refuses what Chromium asks of the system as it starts). When an
// axx host serves the project, the pack's side in the host starts the
// browser, on the person's machine, and the run drives it from the sandbox.
//
// What the run then has is a browser, and nothing more of the machine:
//   - the host starts it, Playwright's own or the Chrome or Edge installed,
//     with the host's options: the run names an engine, and no program,
//     argument or folder;
//   - its network goes through the run (relay), so it reaches what the
//     sandbox lets the run reach;
//   - Playwright's server gets the commands the web packs' steps use and no
//     other (guard): none that names a file of the machine, opens one as a
//     page, or talks to the browser past Playwright;
//   - what a scenario uploads, the run reads and sends; what it downloads,
//     records or traces, the run takes and writes;
//   - a page gets none of the machine's devices: the browser's own clipboard
//     (a headless browser's), and stand-ins for a camera and a microphone.

// notHosted says why the host on a system starts no browser of an engine
// for a run, "" when it does. A hosted browser's network must go through
// the run, all of it: macOS sends a WebKit's requests to the machine's own
// addresses itself, whatever proxy the browser has, so a WebKit there
// would reach what the run's sandbox may keep from the run.
func notHosted(engine, goos string) string {
	if engine == "webkit" && goos == "darwin" {
		return "macOS sends WebKit's requests to this machine's own addresses itself, past the run's sandbox, so the axx host starts no WebKit on a Mac"
	}
	return ""
}

// hostedRequest is what a run asks the pack's side in the host for.
type hostedRequest struct {
	// Engine is the browser: one of engines.
	Engine string `json:"engine"`
}

// hostedBrowser is the host's answer: where the run's Playwright connects,
// and where its side of the browser's network does. Error says why there is
// no browser.
type hostedBrowser struct {
	Endpoint string `json:"endpoint,omitempty"`
	Tunnel   string `json:"tunnel,omitempty"`
	Key      string `json:"key,omitempty"`
	Error    string `json:"error,omitempty"`
}

// HostServes says what the pack does in an axx host (core.HostServer).
func (pack) HostServes() string {
	return "the browsers of web scenarios, for runs in a sandbox that keeps a browser from starting"
}

// ServeHost starts the browser a run asks for and serves it until the run
// closes the connection (core.HostServer).
func (pack) ServeHost(ctx context.Context, run core.HostRun, conn net.Conn) error {
	defer conn.Close()
	log := run.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	answer := func(b hostedBrowser) error {
		line, _ := json.Marshal(b)
		_, err := conn.Write(append(line, '\n'))
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	in := bufio.NewReaderSize(conn, 4<<10)
	line, err := in.ReadSlice('\n')
	if err != nil {
		return fmt.Errorf("the run asked for no browser: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	var req hostedRequest
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || !slices.Contains(engines, req.Engine) {
		_ = answer(hostedBrowser{Error: "the axx host starts the browsers " + strings.Join(engines, ", ") + ", and the run asked for another"})
		return errors.New("the run asked for a browser the pack has none of")
	}
	if why := notHosted(req.Engine, runtime.GOOS); why != "" {
		_ = answer(hostedBrowser{Error: why})
		return errors.New(why)
	}
	// A run asks for a browser an engine, and a host serves a few runs: more
	// at once than that is no run's need, and this machine's memory.
	select {
	case hostedBrowsers <- struct{}{}:
		defer func() { <-hostedBrowsers }()
	default:
		why := fmt.Sprintf("the axx host already has %d browsers started for runs, and starts no more until one of those runs ends", cap(hostedBrowsers))
		_ = answer(hostedBrowser{Error: why})
		return errors.New(why)
	}
	b, err := startHosted(ctx, req.Engine, log, run.Tell)
	if err != nil {
		_ = answer(hostedBrowser{Error: err.Error()})
		return err
	}
	defer b.stop()
	if err := answer(hostedBrowser{Endpoint: b.endpoint, Tunnel: b.net.Tunnel(), Key: b.net.Key()}); err != nil {
		return err
	}
	run.Tell("started " + req.Engine)
	// The connection is the browser's lifetime: the run closes it, or ends.
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		_, _ = io.Copy(io.Discard, in)
	}()
	select {
	case <-ended:
	case <-ctx.Done():
	case <-b.exited:
		log.Warn("the browser the axx host started for a run ended before the run did", "engine", req.Engine, "run", run.Command)
	}
	return nil
}

// hosted is a browser the host started for a run, with what stands between
// the two: the guard, where the run's Playwright connects, and the relay,
// its network.
type hosted struct {
	endpoint string
	net      *relay.Host
	exited   chan struct{}
	stop     func()
}

// hostedBrowsers holds a place for each browser the host has started.
var hostedBrowsers = make(chan struct{}, 16)

// hostDriver prepares the driver and Playwright's browsers once per host.
var hostDriver struct {
	mu        sync.Mutex
	dir       string
	installed map[string]bool
}

// startHosted starts an engine's browser, headless, with a folder of its
// own that goes when it stops.
func startHosted(ctx context.Context, engine string, log *slog.Logger, tell func(string)) (_ *hosted, err error) {
	d, err := hostedDriver(engine, log, tell)
	if err != nil {
		return nil, err
	}
	var undo []func()
	stop := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	defer func() {
		if err != nil {
			stop()
		}
	}()
	dir, err := os.MkdirTemp("", "axx-host-browser-")
	if err != nil {
		return nil, err
	}
	undo = append(undo, func() { _ = os.RemoveAll(dir) })
	network, err := relay.Listen()
	if err != nil {
		return nil, err
	}
	undo = append(undo, network.Close)

	secret, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	name, config := hostedLaunch(engine, dir, network.Proxy(), secret)
	data, _ := json.Marshal(config)
	file, stdin, read, err := launchConfig(dir, data)
	if err != nil {
		return nil, err
	}
	defer read()
	cmd := d.Command("launch-server", "--browser", name, "--config", file)
	cmd.Dir = dir
	cmd.Stdin = stdin
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var said tail
	cmd.Stderr = &said
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start the %s browser: %w", engine, err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	undo = append(undo, func() { endProcess(cmd, exited) })
	// Playwright's server prints where it listens once the browser started.
	listening := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if l := strings.TrimSpace(sc.Text()); strings.HasPrefix(l, "ws://") {
				select {
				case listening <- l:
				default:
				}
			}
		}
	}()
	var server string
	select {
	case server = <-listening:
	case <-exited:
		return nil, launchError(&App{Engine: engine}, errors.New(whyEnded(said.String())))
	case <-time.After(launchTimeout + 30*time.Second):
		return nil, fmt.Errorf("cannot start the %s browser: it did not start in %s", engine, launchTimeout+30*time.Second)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// The run's Playwright connects here, never to Playwright's server.
	key, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	serving, cancel := context.WithCancel(context.WithoutCancel(ctx))
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.URL.Path, "/")), []byte(key)) != 1 {
				http.NotFound(w, r)
				return
			}
			fromRun, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = fromRun.CloseNow() }()
			toServer, resp, err := websocket.Dial(serving, server, &websocket.DialOptions{HTTPHeader: http.Header{"x-playwright-browser": {name}}})
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if err != nil {
				_ = fromRun.Close(websocket.StatusInternalError, "the browser's server does not answer")
				return
			}
			defer func() { _ = toServer.CloseNow() }()
			if err := guard.ForRun(log).Relay(serving, fromRun, toServer); err != nil {
				log.Debug("a run's connection to its browser ended", "error", err)
			}
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	undo = append(undo, func() {
		cancel()
		_ = srv.Close()
	})
	return &hosted{
		endpoint: fmt.Sprintf("ws://%s/%s", ln.Addr(), key),
		net:      network, exited: exited, stop: sync.OnceFunc(stop),
	}, nil
}

// hostedLaunch is the browser of Playwright's that runs an engine (chromium
// for the Chrome and Edge installed), and the options Playwright's server
// starts it with: the host's own, whatever the run.
func hostedLaunch(engine, dir, proxy, secret string) (name string, config map[string]any) {
	config = map[string]any{
		"headless": true,
		"host":     "127.0.0.1",
		"port":     0,
		"wsPath":   secret,
		"timeout":  float64(launchTimeout.Milliseconds()),
		// Every request of the browser, to this machine's own addresses too,
		// goes to the relay, and through it to the run.
		"proxy": map[string]any{"server": "http://" + proxy},
		// What Playwright's server keeps for the run to take.
		"artifactsDir":  filepath.Join(dir, "artifacts"),
		"tracesDir":     filepath.Join(dir, "traces"),
		"downloadsPath": filepath.Join(dir, "downloads"),
	}
	name = engine
	switch {
	case branded(engine):
		name, config["channel"] = "chromium", engine
	case engine == "chromium":
		// The whole of Chromium, as a run outside a sandbox starts.
		config["channel"] = "chromium"
	}
	switch name {
	case "chromium":
		config["args"] = []string{
			"--disable-lcd-text", "--enable-unsafe-webgpu",
			// No connection past the proxy: WebRTC's would go from this
			// machine, not from the run.
			"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
			// A page that asks for a camera or a microphone gets the
			// browser's own stand-ins, never this machine's.
			"--use-fake-device-for-media-stream",
		}
		if env := chromiumEnv(); env != nil {
			config["env"] = env
		}
		// Outside a sandbox, Chromium keeps its pages in its own.
		if runtime.GOOS != "linux" {
			config["chromiumSandbox"] = true
		}
	case "firefox":
		config["firefoxUserPrefs"] = map[string]any{
			"media.peerconnection.ice.proxy_only": true,
			// As for Chromium: stand-ins for a camera and a microphone.
			"media.navigator.streams.fake": true,
		}
	}
	return name, config
}

// hostedDriver prepares the driver, and Playwright's browser of an engine,
// once per host.
func hostedDriver(engine string, log *slog.Logger, tell func(string)) (*playwright.PlaywrightDriver, error) {
	hostDriver.mu.Lock()
	defer hostDriver.mu.Unlock()
	if hostDriver.dir == "" {
		dir, err := driver.Ensure(context.Background(), driver.Options{})
		if err != nil {
			return nil, fmt.Errorf("cannot prepare the browser driver: %w", err)
		}
		if err := playwright.Install(&playwright.RunOptions{DriverDirectory: dir, SkipInstallBrowsers: true, Logger: log}); err != nil {
			return nil, fmt.Errorf("cannot prepare the browser driver: %w", err)
		}
		hostDriver.dir, hostDriver.installed = dir, map[string]bool{}
	}
	d, err := playwright.NewDriver(&playwright.RunOptions{DriverDirectory: hostDriver.dir, SkipInstallBrowsers: true, Logger: log})
	if err != nil {
		return nil, err
	}
	if branded(engine) || hostDriver.installed[engine] {
		return d, nil
	}
	// Playwright's installer does nothing for a browser installed whole, and
	// waits for another's to end.
	var out bytes.Buffer
	cmd := d.Command("install", "--no-shell", engine)
	cmd.Stdout, cmd.Stderr = &out, &out
	started := time.Now()
	if err := cmd.Run(); err != nil {
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		return nil, fmt.Errorf("cannot download the %s browser of Playwright %s: %s", engine, PlaywrightVersion, strings.Join(lines[max(0, len(lines)-5):], "\n  "))
	}
	if time.Since(started) > 5*time.Second {
		tell(fmt.Sprintf("downloaded the %s browser of Playwright %s", engine, PlaywrightVersion))
	}
	hostDriver.installed[engine] = true
	return d, nil
}

// endProcess asks Playwright's server to end, which closes its browser, and
// ends it if it has not after a while.
func endProcess(cmd *exec.Cmd, exited <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	interrupt(cmd.Process)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// whyEnded is, of what Playwright's server said as it ended, what says why:
// its error, and what the browser itself said, on one line.
func whyEnded(said string) string {
	var why, browser []string
	for line := range strings.SplitSeq(said, "\n") {
		line = strings.TrimSpace(line)
		switch _, said, isBrowser := strings.Cut(line, "[err] "); {
		case isBrowser && !slices.Contains(browser, said) && !strings.HasPrefix(said, "***"):
			browser = append(browser, said)
		case strings.HasPrefix(line, "Error: ") && len(why) == 0:
			why = append(why, strings.TrimSuffix(strings.TrimPrefix(line, "Error: "), "."))
		}
	}
	why = append(why, browser[:min(len(browser), 3)]...)
	if len(why) == 0 {
		return "Playwright's server ended as it started"
	}
	return strings.Join(why, ": ")
}

// tail keeps the end of what a process says, for an error.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 8<<10 {
		t.buf = t.buf[len(t.buf)-8<<10:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// connectHosted has the axx host that serves the project start an engine's
// browser, and connects the run to it: its Playwright through the host's
// guard, and its side of the browser's network.
func connectHosted(s *core.Suite, pw *playwright.Playwright, a *App) (playwright.Browser, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, err := s.HostDial(ctx, Name)
	if err != nil {
		return nil, noHostedBrowser{err}
	}
	closeConn := true
	defer func() {
		if closeConn {
			_ = conn.Close()
		}
	}()
	ask, _ := json.Marshal(hostedRequest{Engine: a.Engine})
	if _, err := conn.Write(append(ask, '\n')); err != nil {
		return nil, err
	}
	// The host may download the browser first.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	line, err := bufio.NewReaderSize(conn, 4<<10).ReadSlice('\n')
	if err != nil {
		return nil, fmt.Errorf("the axx host did not say where its browser is: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	var got hostedBrowser
	if err := json.Unmarshal(line, &got); err != nil {
		return nil, fmt.Errorf("the axx host's answer is not one: %w", err)
	}
	if got.Error != "" {
		return nil, errors.New(got.Error)
	}
	network, err := relay.Serve(got.Tunnel, got.Key, hostedNetwork(s))
	if err != nil {
		return nil, err
	}
	bt := map[string]playwright.BrowserType{"chromium": pw.Chromium, "firefox": pw.Firefox, "webkit": pw.WebKit}[a.Engine]
	if bt == nil {
		bt = pw.Chromium // the Chrome or Edge installed
	}
	b, err := bt.Connect(got.Endpoint, playwright.BrowserTypeConnectOptions{Timeout: playwright.Float(float64(launchTimeout.Milliseconds()))})
	if err != nil {
		network.Close()
		return nil, fmt.Errorf("cannot connect to the browser the axx host started: %s", firstLine(err))
	}
	closeConn = false
	hostedOf(s).Store(a.key(), true)
	s.OnClose(func(context.Context) error {
		err := b.Close()
		network.Close()
		_ = conn.Close()
		return err
	})
	return b, nil
}

// hostedOf are the run's browsers the axx host started, by browser key.
func hostedOf(s *core.Suite) *sync.Map {
	m, _ := core.Cached(s, Name+"/hosted", func() (*sync.Map, error) { return &sync.Map{}, nil })
	return m
}

// isHosted reports whether an app's browser is one the axx host started.
func isHosted(s *core.Suite, a *App) bool {
	_, hosted := hostedOf(s).Load(a.key())
	return hosted
}

// hostedNetwork is how the run's side reaches what a hosted browser asks
// for: as the run's environment says. A test reaches it its own way.
var hostedNetwork = func(s *core.Suite) relay.Options { return relay.Options{Logger: s.Logger()} }

// noHostedBrowser is why the run could not ask the host for a browser (it
// is gone, or runs without the pack): the run starts its own then.
type noHostedBrowser struct{ error }
