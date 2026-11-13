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
)

// A JVM service with JaCoCo's agent writes what ran as it exits, and the
// pack reads it against its classes: testdata/Desk.java, compiled here.
func TestAJVMServicesCoverage(t *testing.T) {
	java, err := javaProgram()
	if err != nil {
		t.Skip(err)
	}
	javac := filepath.Join(filepath.Dir(java), "javac")
	if _, err := os.Stat(javac); err != nil {
		t.Skip("no javac beside", java)
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
	if out, err := exec.Command(javac, "-d", classes, filepath.Join(src, "Desk.java")).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	dir := filepath.Join(project, ".axx", "coverage", "raw", "r1", "desk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_TOOL_OPTIONS", "")
	s := core.CoverageService{Name: "desk", Runtime: "jvm", Dir: dir, WorkDir: work}
	env, err := runtime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: s})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(java, "-cp", classes, "parcels.Desk")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	files, m, err := runtime{}.Convert(context.Background(), coveragecore.ConvertCall{
		End:      core.CoverageEnd{CoverageService: s, Status: "exited with code 0"},
		Settings: func(string, any) error { return nil },
		Project:  project,
		Path:     func(p string) string { return filepath.Join(work, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(src, "Desk.java") || !m.Branches || !m.Functions {
		t.Fatalf("files %+v, measured %+v", files, m)
	}
	if s := istanbul.Total(files); s.Lines.Covered != 4 || s.Lines.Total != 7 || s.Branches.Covered != 1 || s.Functions.Covered != 2 {
		t.Errorf("lines %+v, branches %+v, functions %+v", s.Lines, s.Branches, s.Functions)
	}
}
