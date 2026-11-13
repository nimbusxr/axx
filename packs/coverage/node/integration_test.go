//go:build integration

package coveragenode

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
	"github.com/nimbusxr/axx/packs/coverage/internal/coveragetest"
)

const server = `function ship(n) {
  if (n > 0) {
    return 'shipped';
  }
  return 'held';
}

function close() {
  return 1;
}

console.log(ship(1));
console.log('ready');
setInterval(() => {}, 1000);
`

// A Node service that does not handle SIGTERM writes its coverage as axx
// stops it, through the preload; the run reads it, and a take mid-run
// gives what ran until then.
func TestANodeServicesCoverage(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		coveragetest.Missing(t, "no node on PATH")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(tmp, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "server.js"), []byte(server), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "raw", "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NODE_OPTIONS", "")
	s := core.CoverageService{Name: "api", Runtime: "node", Dir: dir, WorkDir: app}
	env, err := nodeRuntime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: s})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "server.js")
	cmd.Dir, cmd.Env = app, append(os.Environ(), env...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(out)
	for sc.Scan() && sc.Text() != "ready" {
	}

	// A take: V8 writes what ran so far.
	if err := os.WriteFile(filepath.Join(dir, "take"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	taken := filepath.Join(dir, "taken-"+strconv.Itoa(cmd.Process.Pid))
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(taken); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the preload did not take the coverage")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	if ee, ok := errors.AsType[*exec.ExitError](err); !ok || ee.ExitCode() != 143 {
		t.Errorf("it exits as SIGTERM would have it: %v", err)
	}
	written, _ := filepath.Glob(filepath.Join(dir, "v8", "coverage-*.json"))
	if len(written) != 2 {
		t.Errorf("%d coverage files, want the take's and the exit's", len(written))
	}

	files, m, err := nodeRuntime{}.Convert(context.Background(), coveragecore.ConvertCall{
		End:  core.CoverageEnd{CoverageService: s, Status: "exited with code 143"},
		Path: func(p string) string { return p },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(app, "server.js") || !m.Branches || !m.Functions {
		var paths []string
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		t.Fatalf("files %s, measured %+v", strings.Join(paths, ", "), m)
	}
	s2 := istanbul.Total(files)
	fns := map[string]int{}
	for i, fn := range files[0].FnMap {
		fns[fn.Name] = files[0].F[i]
	}
	if fns["ship"] == 0 || fns["close"] != 0 || s2.Lines.Covered == s2.Lines.Total {
		t.Errorf("functions %v, lines %+v", fns, s2.Lines)
	}
}

// A Node service axx up keeps is read while it runs: reset as a run
// starts, taken as it ends, into the run's folder.
func TestANodeServiceKeptUp(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		coveragetest.Missing(t, "no node on PATH")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(tmp, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "server.js"), []byte(server), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "raw", "up", "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NODE_OPTIONS", "")
	s := core.CoverageService{Name: "api", Runtime: "node", Dir: dir, WorkDir: app, Kept: true}
	env, err := nodeRuntime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: s})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "server.js")
	cmd.Dir, cmd.Env = app, append(os.Environ(), env...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	sc := bufio.NewScanner(out)
	for sc.Scan() && sc.Text() != "ready" {
	}

	call := coveragecore.LaunchCall{Service: s}
	if err := (nodeRuntime{}).Reset(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "v8", "coverage-*.json")); len(left) != 0 {
		t.Errorf("the reset left %d files", len(left))
	}
	into := filepath.Join(tmp, "raw", "run1", "api")
	if err := (nodeRuntime{}).Take(context.Background(), call, into); err != nil {
		t.Fatal(err)
	}
	if taken, _ := filepath.Glob(filepath.Join(into, "v8", "coverage-*.json")); len(taken) != 2 {
		t.Fatalf("%d files taken, want the take and the reset's base", len(taken))
	}
	end := core.CoverageEnd{CoverageService: s}
	end.Dir = into
	files, _, err := nodeRuntime{}.Convert(context.Background(), coveragecore.ConvertCall{End: end, Path: func(p string) string { return p }})
	if err != nil || len(files) != 1 {
		t.Fatalf("%v %v", files, err)
	}
	// Every named function counts (ship, close), from the reset's base; ship ran before it,
	// not since: the run's counts start there.
	fns := map[string]int{}
	for i, fn := range files[0].FnMap {
		fns[fn.Name] = files[0].F[i]
	}
	if s := istanbul.Total(files); s.Functions.Total != 2 || fns["ship"] != 0 {
		t.Errorf("functions %v, totals %+v", fns, s.Functions)
	}
}
