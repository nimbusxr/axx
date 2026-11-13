//go:build integration

package coveragepython

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
	"github.com/nimbusxr/axx/packs/coverage/internal/coveragetest"
)

const desk = `def ship(n):
    if n > 0:
        return "shipped"
    return "held"


def close():
    return 1


class Desk:
    def open(self):
        return ship(1)


print(Desk().open())
`

// A Python service under coverage.py, with axx's settings, writes its data
// into its folder; axx reads it with its own coverage.py, the paths the
// service named mapped to the project's.
func TestAPythonServicesCoverage(t *testing.T) {
	py, err := python(context.Background())
	if err != nil {
		coveragetest.Missing(t, err)
	}
	lib, err := library(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// coverage.py names files by their real paths (macOS's /private/var).
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(tmp, "app")
	if err := os.MkdirAll(filepath.Join(app, "parcels"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "parcels", "desk.py"), []byte(desk), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "claims")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := core.CoverageService{Name: "claims", Runtime: "python", Dir: dir, WorkDir: app}
	env, err := pyRuntime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: s})
	if err != nil {
		t.Fatal(err)
	}
	// The service's own coverage.py: here, the one axx downloaded.
	cmd := exec.Command(py, "-m", "coverage", "run", "-m", "parcels.desk")
	cmd.Dir, cmd.Env = app, append(os.Environ(), append(env, "PYTHONPATH="+lib)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	// As if the service ran in a container whose /app is the project's app.
	moved := filepath.Join(tmp, "project", "app")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(app, moved); err != nil {
		t.Fatal(err)
	}
	files, m, err := pyRuntime{}.Convert(context.Background(), coveragecore.ConvertCall{
		End:      core.CoverageEnd{CoverageService: s, Status: "exited with code 0"},
		Settings: func(string, any) error { return nil },
		Path:     func(p string) string { return strings.Replace(p, app, moved, 1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(moved, "parcels", "desk.py") || !m.Branches || !m.Functions {
		t.Fatalf("files %+v (%s), measured %+v", files, files[0].Path, m)
	}
	if s := istanbul.Total(files); s.Lines.Covered != 8 || s.Lines.Total != 10 || s.Branches.Covered != 1 || s.Branches.Total != 2 || s.Functions.Covered != 2 || s.Functions.Total != 3 {
		t.Errorf("lines %+v, branches %+v, functions %+v", s.Lines, s.Branches, s.Functions)
	}
}
