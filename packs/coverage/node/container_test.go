//go:build integration

package coveragenode

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

// A Node service in a container, as a user of its own and as its PID 1,
// writes its coverage into the folder of this machine it mounts as compose
// stops it; the run reads it, its files named as they are in the project.
func TestANodeServiceInAContainer(t *testing.T) {
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
	dir := filepath.Join(tmp, ".axx", "coverage", "raw", "r1", "api")
	s := core.CoverageService{Name: "api", Runtime: "node", Dir: dir, Mount: "/axx-coverage", WorkDir: app}
	t.Setenv("NODE_OPTIONS", "")
	env, err := coveragecore.Pack().(core.CoverageLauncher).LaunchCoverage(context.Background(), func(string, any) error { return nil }, s)
	if err != nil {
		t.Fatal(err)
	}
	code, out := coveragetest.Run(t, coveragetest.Service{
		Image: "node:24-alpine", Cmd: []string{"node", "/app/server.js"}, Env: env, Ready: "ready",
		Binds: []string{app + ":/app:ro", dir + ":/axx-coverage"},
	})
	if code != 143 {
		t.Errorf("exit code %d, want SIGTERM's:\n%s", code, out)
	}
	files, _, err := nodeRuntime{}.Convert(context.Background(), coveragecore.ConvertCall{
		End:  core.CoverageEnd{CoverageService: s, Status: "exited with code 143"},
		Path: func(p string) string { return strings.Replace(p, "/app", app, 1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(app, "server.js") {
		t.Fatalf("files %+v:\n%s", files, out)
	}
	if s := istanbul.Total(files); s.Functions.Covered == 0 || s.Lines.Covered == s.Lines.Total {
		t.Errorf("functions %+v, lines %+v", s.Functions, s.Lines)
	}
}
