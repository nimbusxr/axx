//go:build integration

package coveragepython

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
	"github.com/nimbusxr/axx/packs/coverage/internal/coveragetest"
)

// What the service in the container runs: the desk, and a server's
// workers, one it spawns, as uvicorn does, and one it forks that ends with
// os._exit, as gunicorn's do; then it waits until it is stopped, and stops
// on SIGTERM as a service's server does.
const deskServer = `import multiprocessing
import os
import signal
import sys
import time

from parcels import desk


def worker():
    desk.close()


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    spawned = multiprocessing.get_context("spawn").Process(target=worker)
    spawned.start()
    spawned.join()
    pid = os.fork()
    if pid == 0:
        desk.ship(0)
        os._exit(0)
    os.waitpid(pid, 0)
    print("ready", flush=True)
    time.sleep(60)
`

// A Python service in a container, under its own coverage.py, as a user of
// its own and as its PID 1, writes its data into the folder of this
// machine it mounts as compose stops it; the run reads it, its files named
// as they are in the project.
func TestAPythonServiceInAContainer(t *testing.T) {
	cached, err := library(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The service's own coverage.py is installed in its image, with the
	// file that starts it in the processes the service starts. Here it is a
	// copy of axx's in the user's site-packages (PYTHONUSERBASE), readable
	// by the container's user: axx's own is its user's only.
	userBase := filepath.Join(tmp, "pyuser")
	site := filepath.Join(userBase, "lib", "python3.13", "site-packages")
	if err := os.MkdirAll(filepath.Dir(site), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(site, os.DirFS(cached)); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(tmp, "app")
	if err := os.MkdirAll(filepath.Join(app, "parcels"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"desk.py": desk, "server.py": deskServer, "__init__.py": ""} {
		if err := os.WriteFile(filepath.Join(app, "parcels", name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(tmp, ".axx", "coverage", "raw", "r1", "claims")
	s := core.CoverageService{Name: "claims", Runtime: "python", Dir: dir, Mount: "/axx-coverage", WorkDir: app}
	env, err := coveragecore.Pack().(core.CoverageLauncher).LaunchCoverage(context.Background(), func(string, any) error { return nil }, s)
	if err != nil {
		t.Fatal(err)
	}
	code, out := coveragetest.Run(t, coveragetest.Service{
		Image: "python:3.13-alpine", Cmd: []string{"python", "-m", "coverage", "run", "-m", "parcels.server"},
		Env:   append(env, "PYTHONPATH=/app", "PYTHONUSERBASE=/opt/pyuser", "PYTHONDONTWRITEBYTECODE=1"),
		Binds: []string{app + ":/app:ro", userBase + ":/opt/pyuser:ro", dir + ":/axx-coverage"},
		Ready: "ready",
	})
	if code != 0 {
		t.Errorf("exit code %d, want 0 (it exits on SIGTERM):\n%s", code, out)
	}
	files, m, err := pyRuntime{}.Convert(context.Background(), coveragecore.ConvertCall{
		End:      core.CoverageEnd{CoverageService: s, Status: "exited"},
		Settings: func(string, any) error { return nil },
		Path:     func(p string) string { return strings.Replace(p, "/app", app, 1) },
	})
	if err != nil {
		t.Fatalf("%v (exit code %d):\n%s", err, code, out)
	}
	var deskFile *istanbul.FileCoverage
	for _, f := range files {
		if f.Path == filepath.Join(app, "parcels", "desk.py") {
			deskFile = f
		}
	}
	if deskFile == nil || !m.Branches || !m.Functions {
		t.Fatalf("files %+v, measured %+v (exit code %d):\n%s", files, m, code, out)
	}
	// The workers ran close and ship's other branch: every line counts.
	if s := istanbul.Total([]*istanbul.FileCoverage{deskFile}); s.Lines.Covered != 10 || s.Lines.Total != 10 || s.Branches.Covered != 2 || s.Functions.Covered != 3 {
		t.Errorf("lines %+v, branches %+v, functions %+v: the workers' coverage is missing", s.Lines, s.Branches, s.Functions)
	}
}
