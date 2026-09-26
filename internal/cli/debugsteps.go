package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/gotool"
	"github.com/nimbusxr/axx/internal/packbuild"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/version"
)

const (
	// envDebuggee marks the axx that runs under Delve for --debug-steps.
	envDebuggee = "AXX_STEPS_DEBUGGEE"
	// envExitFile names a file where axx writes its exit code; Delve does
	// not pass the code of the program it runs on.
	envExitFile = "AXX_EXIT_FILE"

	codeNoDelve    = "AXX-E0305"
	codeDelveStart = "AXX-E0306"

	// stepsDebugger names the debugger request, and the "Debugger: axx-steps"
	// run configuration the IntelliJ plugin starts for it.
	stepsDebugger = "axx-steps"
	// defaultStepsPort is Delve's customary port.
	defaultStepsPort = "2345"

	// noTimeout replaces step and hook timeouts under a debugger.
	noTimeout = 100 * 365 * 24 * time.Hour
)

// debugSteps runs this `axx run` under Delve so an IDE can stop at
// breakpoints in step code: the project's packs and axx's own code. It
// builds axx with debug information (once; cached), starts it headless and
// waiting, and prints the request the IntelliJ plugin answers by attaching.
func (a *App) debugSteps(ctx context.Context, f *runFlags) error {
	port, err := strconv.Atoi(f.debugSteps)
	if err != nil || port <= 0 || port > 65535 {
		return axxerr.New("AXX-E0001", exitcode.Usage, "invalid usage: --debug-steps takes a port number, got %q", f.debugSteps).
			WithHint("use --debug-steps (port %s) or --debug-steps=<port>", defaultStepsPort)
	}
	dir, err := config.ProjectDir(a.Config)
	if err != nil {
		return err
	}
	var entries []packset.Entry
	if pf, found, err := packset.Load(dir); err != nil {
		return err
	} else if found {
		if entries, err = pf.Entries(); err != nil {
			return err
		}
	}
	info := version.Get()
	source := axxSource(info)
	if source == "" && info.Channel == "dev" {
		return axxerr.New(packbuild.CodeBuild, exitcode.Usage, "this development build of axx cannot build itself for debugging").
			WithHint("use a released axx, or set AXX_SOURCE_DIR to your axx checkout")
	}
	lock, err := packset.LoadLock(dir)
	if err != nil {
		return err
	}
	res, err := packbuild.Ensure(ctx, packbuild.Options{
		ProjectDir: dir, Entries: entries, Lock: lock, AxxVersion: info.Version, AxxSource: source, Log: a.Stderr, Debug: true,
	})
	if err != nil {
		return err
	}
	dlv, err := a.findDelve(ctx)
	if err != nil {
		return err
	}
	exitFile, err := os.CreateTemp("", "axx-exit-*")
	if err != nil {
		return err
	}
	_ = exitFile.Close()
	defer func() { _ = os.Remove(exitFile.Name()) }()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	args := append([]string{"exec", res.Binary, "--headless", "--listen=" + addr, "--api-version=2", "--accept-multiclient", "--"},
		withoutFlag(os.Args[1:], "--debug-steps")...)
	cmd := exec.Command(dlv, args...) //nolint:noctx // stopped below, gracefully, when ctx ends
	cmd.Env = append(os.Environ(), envPackBuild+"=1", envDebuggee+"=1", envExitFile+"="+exitFile.Name())
	cmd.Stdin, cmd.Stderr = os.Stdin, a.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return axxerr.Wrap(err, codeDelveStart, exitcode.Environment, "cannot start Delve")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// Delve's first line says it is listening; everything after it is the
	// run's own output.
	listening := make(chan struct{})
	var early bytes.Buffer
	go func() {
		sc := bufio.NewScanner(out)
		announced := false
		for sc.Scan() {
			line := sc.Text()
			if !announced {
				if strings.HasPrefix(line, "API server listening at:") {
					announced = true
					close(listening)
					continue
				}
				early.WriteString(line + "\n")
			}
			fmt.Fprintln(a.Stdout, line)
		}
		_, _ = io.Copy(a.Stdout, out)
	}()
	select {
	case <-listening:
	case err := <-done:
		return axxerr.New(codeDelveStart, exitcode.Environment, "Delve exited before it was ready: %v\n%s", err, strings.TrimSpace(early.String()))
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		return axxerr.New(codeDelveStart, exitcode.Environment, "Delve did not start listening on %s within 30s", addr)
	}
	fmt.Fprintf(a.Stderr, "axx: waiting for a Go debugger on %s; the run starts when one attaches (breakpoints in step code, custom packs included)\n", addr)
	fmt.Fprintf(a.Stdout, "[AXX-IDE] debug-attach-request name=%s type=go host=127.0.0.1 port=%d\n", stepsDebugger, port)

	// Delve keeps serving after the run ends when a debugger stays attached,
	// so the run's exit file, not Delve's exit, ends the session.
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return a.stepsExit(exitFile.Name())
		case <-ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			return ctx.Err()
		case <-tick.C:
			if code, ok := readExit(exitFile.Name()); ok {
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					_ = cmd.Process.Kill()
					<-done
				}
				return silentExit{code: exitcode.Code(code)}
			}
		}
	}
}

func (a *App) stepsExit(file string) error {
	if code, ok := readExit(file); ok {
		return silentExit{code: exitcode.Code(code)}
	}
	return axxerr.New(codeDelveStart, exitcode.Environment, "the run under Delve ended without an exit code (the debugger stopped it)")
}

func readExit(file string) (int, bool) {
	b, err := os.ReadFile(file)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return code, err == nil
}

// withoutFlag removes a flag (as --flag or --flag=value) from arguments.
func withoutFlag(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			continue
		}
		out = append(out, a)
	}
	return out
}

// delveVersion is the Delve axx prepares for --debug-steps: one that
// supports the Go release axx builds with.
const delveVersion = "v1.27.2"

// findDelve returns the Delve to run the debug build under: $AXX_DLV, else
// axx's own, built once with the Go axx builds with (so that it supports
// that Go) and kept in axx's cache, else a dlv installed here.
func (a *App) findDelve(ctx context.Context) (string, error) {
	if p := os.Getenv("AXX_DLV"); p != "" {
		return p, nil
	}
	own, err := prepareDelve(ctx, a.Stderr)
	if err == nil {
		return own, nil
	}
	if p, ok := installedDelve(ctx); ok {
		return p, nil
	}
	return "", axxerr.Wrap(err, codeNoDelve, exitcode.Environment, "debugging step code needs Delve, and axx could not prepare it").
		WithHint("axx builds Delve once from the Go module proxy: check the network, or install it with `go install github.com/go-delve/delve/cmd/dlv@latest`, or set AXX_DLV to a dlv")
}

// prepareDelve builds axx's own Delve, once, with the Go axx builds with,
// in axx's cache.
func prepareDelve(ctx context.Context, log io.Writer) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "axx", "delve", delveVersion)
	bin := filepath.Join(dir, "dlv")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if st, err := os.Stat(bin); err == nil && !st.IsDir() {
		return bin, nil
	}
	tc, err := gotool.Ensure(ctx, gotool.Options{Log: log})
	if err != nil {
		return "", err
	}
	fmt.Fprintf(log, "axx: preparing Delve %s, Go's debugger (once; cached for later runs)\n", delveVersion)
	cmd := exec.CommandContext(ctx, tc.Go, "install", "github.com/go-delve/delve/cmd/dlv@"+delveVersion)
	cmd.Env = append(slices.Clip(tc.Env), "GOBIN="+dir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go install dlv@%s: %w\n%s", delveVersion, err, strings.TrimSpace(out.String()))
	}
	return bin, nil
}

// installedDelve finds a dlv on PATH or in Go's bin directories.
func installedDelve(ctx context.Context) (string, bool) {
	name := "dlv"
	if runtime.GOOS == "windows" {
		name = "dlv.exe"
	}
	if p, err := exec.LookPath("dlv"); err == nil {
		return p, true
	}
	var dirs []string
	if goBin, err := exec.LookPath("go"); err == nil {
		if out, err := exec.CommandContext(ctx, goBin, "env", "GOBIN", "GOPATH").Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
				dirs = append(dirs, strings.TrimSpace(lines[0]))
			}
			if len(lines) > 1 {
				for _, p := range filepath.SplitList(strings.TrimSpace(lines[1])) {
					dirs = append(dirs, filepath.Join(p, "bin"))
				}
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// axxSource is the axx source to build from: $AXX_SOURCE_DIR, or for a
// development build, the checkout it was built from when it is still there.
func axxSource(info version.Info) string {
	if s := os.Getenv("AXX_SOURCE_DIR"); s != "" {
		return s
	}
	if info.Channel != "dev" {
		return ""
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		return ""
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file))) // internal/cli/debugsteps.go
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !bytes.Contains(data, []byte("module "+packbuild.AxxModule+"\n")) {
		return ""
	}
	return root
}
