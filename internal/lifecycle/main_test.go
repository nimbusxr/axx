package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
)

// helperEnv makes the test binary act as a fake app (see runHelper).
const helperEnv = "AXX_LIFECYCLE_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(runHelper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// runHelper implements the fake apps. Modes:
//
//	sleep                       run until killed
//	print <line>...             print lines ("err:" prefix: to stderr), then sleep
//	delayed-print <d> <line>    sleep d, print line, sleep
//	exit <code> <line>...       print lines, exit with code
//	print-exit <d> <code> <l>   print l, sleep d, exit with code
//	http <addr> <d>             after d, serve 200 on addr
//	tcp <addr> <d>              after d, accept connections on addr
//	touch <file> <d>            after d, create file, sleep
//	exists <file>               exit 0 if file exists, else 1
//	append <file> <text>        append a line to file, exit 0
//	record <file> <name>        append "<name> started", print "started"; on
//	                            SIGTERM/SIGINT append "<name> <SIG>" and exit 0
//	stubborn <file>             print "ready"; record and ignore SIGTERM/SIGINT
//	grandchild <pidfile> <file> spawn a stubborn child, write its pid, print "spawned"
//	pwd                         print the working directory, sleep
func runHelper(args []string) int {
	if len(args) == 0 {
		return 2
	}
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	dur := func(i int) time.Duration {
		d, _ := time.ParseDuration(arg(i))
		return d
	}
	forever := func() int {
		time.Sleep(time.Hour)
		return 0
	}
	switch args[0] {
	case "sleep":
		return forever()
	case "print":
		for _, l := range args[1:] {
			if rest, ok := strings.CutPrefix(l, "err:"); ok {
				fmt.Fprintln(os.Stderr, rest)
			} else {
				fmt.Println(l)
			}
		}
		return forever()
	case "delayed-print":
		time.Sleep(dur(1))
		fmt.Println(arg(2))
		return forever()
	case "exit":
		for _, l := range args[2:] {
			fmt.Println(l)
		}
		code, _ := strconv.Atoi(arg(1))
		return code
	case "print-exit":
		fmt.Println(arg(3))
		time.Sleep(dur(1))
		code, _ := strconv.Atoi(arg(2))
		return code
	case "http":
		time.Sleep(dur(2))
		srv := &http.Server{
			Addr:              arg(1),
			ReadHeaderTimeout: time.Second,
			Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		}
		fmt.Println("serving", arg(1))
		_ = srv.ListenAndServe()
		return 1
	case "tcp":
		time.Sleep(dur(2))
		ln, err := net.Listen("tcp", arg(1))
		if err != nil {
			return 1
		}
		for {
			c, err := ln.Accept()
			if err != nil {
				return 1
			}
			_ = c.Close()
		}
	case "touch":
		time.Sleep(dur(2))
		if err := os.WriteFile(arg(1), nil, 0o600); err != nil {
			return 1
		}
		return forever()
	case "exists":
		if _, err := os.Stat(arg(1)); err != nil {
			return 1
		}
		return 0
	case "append":
		appendLine(arg(1), strings.Join(args[2:], " "))
		return 0
	case "record":
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
		appendLine(arg(1), arg(2)+" started")
		fmt.Println("started")
		sig := <-sigs
		appendLine(arg(1), arg(2)+" "+signalName(sig))
		return 0
	case "stubborn":
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
		fmt.Println("ready")
		for sig := range sigs {
			appendLine(arg(1), signalName(sig))
		}
		return 0
	case "grandchild":
		exe, _ := os.Executable()
		child := exec.Command(exe, "stubborn", arg(2))
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			return 1
		}
		if err := os.WriteFile(arg(1), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			return 1
		}
		time.Sleep(100 * time.Millisecond) // let the child install its handler
		fmt.Println("spawned")
		return forever()
	case "pwd":
		wd, _ := os.Getwd()
		fmt.Println("pwd=" + wd)
		return forever()
	}
	return 2
}

func signalName(sig os.Signal) string {
	switch sig {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGINT:
		return "SIGINT"
	}
	return sig.String()
}

var appendMu sync.Mutex

func appendLine(file, line string) {
	appendMu.Lock()
	defer appendMu.Unlock()
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintln(f, line)
}

// --- test utilities ---

// helper returns the command running this test binary in a helper mode.
func helper(t *testing.T, mode string, args ...string) config.Command {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return config.Command{Argv: append([]string{exe, mode}, args...)}
}

// helperApp is an app running the test binary in a helper mode.
func helperApp(t *testing.T, name, mode string, args ...string) config.App {
	t.Helper()
	return config.App{
		Name:    name,
		Command: helper(t, mode, args...),
		Env:     helperEnvVars(),
	}
}

// helperEnvVars makes a process a helper. Under -race, helpers would
// otherwise sleep a second before exiting (GORACE atexit_sleep_ms).
func helperEnvVars() map[string]string {
	return map[string]string{helperEnv: "1", "GORACE": "atexit_sleep_ms=0"}
}

// logReady makes app ready once a line matches re.
func logReady(app config.App, re string) config.App {
	app.Ready = &config.Ready{
		Log:      re,
		Timeout:  config.Duration(10 * time.Second),
		Interval: config.Duration(20 * time.Millisecond),
	}
	return app
}

// newTestLogger logs everything as text into w.
func newTestLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// buffer is a goroutine-safe bytes.Buffer.
type buffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// harness is a Manager under test with captured output.
type harness struct {
	*Manager
	stdout, stderr, logs *buffer
	dir                  string
}

// newHarness creates a Manager whose ConfigDir is a temp dir, capturing
// output and logs, and stops it when the test ends.
func newHarness(t *testing.T, apps config.Apps, opts Options) *harness {
	t.Helper()
	h := &harness{stdout: &buffer{}, stderr: &buffer{}, logs: &buffer{}, dir: t.TempDir()}
	if opts.ConfigDir == "" {
		opts.ConfigDir = h.dir
	}
	opts.Stdout, opts.Stderr = h.stdout, h.stderr
	opts.Logger = newTestLogger(h.logs)
	m, err := New(apps, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.Manager = m
	t.Cleanup(func() {
		if err := m.Stop(context.Background()); err != nil {
			t.Logf("cleanup Stop: %v", err)
		}
		if t.Failed() {
			t.Logf("stdout:\n%s\nstderr:\n%s\nlogs:\n%s", h.stdout, h.stderr, h.logs)
		}
	})
	return h
}

// readLines returns the lines of file (nil if it does not exist).
func readLines(t *testing.T, file string) []string {
	t.Helper()
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// wantCode fails unless err is an *axxerr.Error with the code.
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	_ = mustCode(t, err, code)
}

// mustCode is wantCode returning the error for further checks.
func mustCode(t *testing.T, err error, code string) *axxerr.Error {
	t.Helper()
	var ae *axxerr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("error = %v (%T), want an *axxerr.Error with code %s", err, err, code)
	}
	if ae.Code != code {
		t.Fatalf("error code = %s, want %s (error: %v)", ae.Code, code, err)
	}
	return ae
}

// handedOut remembers the addresses freeAddr returned: the kernel happily
// hands a just-freed port to the next bind, and parallel tests must not
// share one.
var handedOut sync.Map

// freeAddr returns a loopback address with a currently free port that no
// other test got.
func freeAddr(t *testing.T) string {
	t.Helper()
	for {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		if _, dup := handedOut.LoadOrStore(addr, true); !dup {
			return addr
		}
	}
}

// listenAddr listens on a loopback port (closed when the test ends) and
// returns its address.
func listenAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().String()
	handedOut.Store(addr, true)
	return addr
}

// eventually polls cond until it holds or d elapses.
func eventually(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", d, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals: " + why)
	}
}

// pidOf returns the pid of a started app.
func (h *harness) pidOf(t *testing.T, name string) int {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	a := h.apps[name]
	if a.proc == nil {
		t.Fatalf("app %s was not launched", name)
	}
	return a.proc.cmd.Process.Pid
}
