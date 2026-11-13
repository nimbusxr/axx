//go:build integration

package coveragejvm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/core"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
	"github.com/nimbusxr/axx/packs/coverage/internal/coveragetest"
)

// A JVM service in a container, as a user of its own and as its PID 1,
// writes its coverage into the folder of this machine it mounts as compose
// stops it, through the agent in that folder; the run reads it against the
// classes it ran.
func TestAJVMServiceInAContainer(t *testing.T) {
	java, err := javaProgram()
	if err != nil {
		coveragetest.Missing(t, err)
	}
	javac := filepath.Join(filepath.Dir(java), "javac")
	if _, err := os.Stat(javac); err != nil {
		coveragetest.Missing(t, "no javac beside", java)
	}
	project := t.TempDir()
	work := filepath.Join(project, "desk")
	src := filepath.Join(work, "src", "main", "java", "parcels")
	classes := filepath.Join(work, "build", "classes", "java", "main")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("testdata", "Desk.java"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Desk.java"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Server.java"), []byte(server), 0o644); err != nil {
		t.Fatal(err)
	}
	// For the container's Java 21, whatever this machine's.
	if out, err := exec.Command(javac, "--release", "17", "-d", classes, filepath.Join(src, "Desk.java"), filepath.Join(src, "Server.java")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	dir := filepath.Join(project, ".axx", "coverage", "raw", "r1", "desk")
	t.Setenv("JAVA_TOOL_OPTIONS", "")
	s := core.CoverageService{Name: "desk", Runtime: "jvm", Dir: dir, Mount: "/axx-coverage", WorkDir: work}
	env, err := coveragecore.Pack().(core.CoverageLauncher).LaunchCoverage(context.Background(), func(string, any) error { return nil }, s)
	if err != nil {
		t.Fatal(err)
	}
	code, out := coveragetest.Run(t, coveragetest.Service{
		Image: "eclipse-temurin:21-jre-alpine", Cmd: []string{"java", "-cp", "/app", "parcels.Server"}, Env: env, Ready: "ready",
		Binds: []string{classes + ":/app:ro", dir + ":/axx-coverage"},
	})
	if code != 143 {
		t.Errorf("exit code %d, want SIGTERM's:\n%s", code, out)
	}
	files, _, err := runtime{}.Convert(context.Background(), coveragecore.ConvertCall{
		End:      core.CoverageEnd{CoverageService: s, Status: "exited with code 143"},
		Settings: func(string, any) error { return nil },
		Project:  project,
		Path:     func(p string) string { return filepath.Join(work, p) },
	})
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	var desk *istanbul.FileCoverage
	for _, f := range files {
		if f.Path == filepath.Join(src, "Desk.java") {
			desk = f
		}
	}
	if desk == nil {
		t.Fatalf("files %+v:\n%s", files, out)
	}
	if s := istanbul.Total([]*istanbul.FileCoverage{desk}); s.Lines.Covered == 0 || s.Lines.Covered == s.Lines.Total {
		t.Errorf("lines %+v", s.Lines)
	}
}
