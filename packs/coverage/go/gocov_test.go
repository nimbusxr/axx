package coveragego

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
)

const shipments = `package main

import "os"

// ship ships a parcel, or holds it.
func ship(n int) string {
	if n > 0 {
		return "shipped"
	}
	return "held"
}

type desk struct{}

func (*desk) close() int {

	return 1
}

func main() {
	_ = ship(len(os.Args))
}
`

// A service built with go build -cover writes its coverage as it exits:
// axx converts it with Go's own tool, and names the module's files.
func TestAGoServicesCoverage(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on PATH")
	}
	t.Setenv("AXX_GO", goBin) // axx's Go is this one: nothing to download
	project := t.TempDir()
	mod := filepath.Join(project, "shipments")
	write(t, filepath.Join(mod, "go.mod"), "module example.com/shipments\n\ngo 1.21\n")
	write(t, filepath.Join(mod, "main.go"), shipments)
	bin := filepath.Join(t.TempDir(), "shipments")
	build := exec.Command(goBin, "build", "-cover", "-o", bin, ".")
	build.Dir, build.Env = mod, append(os.Environ(), "GOFLAGS=", "GOWORK=off", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	dir := filepath.Join(project, ".axx", "coverage", "raw", "r1", "shipments")
	s := core.CoverageService{Name: "shipments", Runtime: "go", Dir: dir, WorkDir: mod}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	env, err := runtime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: s})
	if err != nil || len(env) != 1 || env[0] != "GOCOVERDIR="+dir {
		t.Fatalf("launch: %v %v", env, err)
	}
	run := exec.Command(bin)
	run.Env = append(os.Environ(), env...)
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	call := coveragecore.ConvertCall{
		End:      core.CoverageEnd{CoverageService: s, Status: "exited with code 0"},
		Settings: func(string, any) error { return nil },
		Project:  project,
		Path:     func(p string) string { return p },
	}
	files, m, err := runtime{}.Convert(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.Join(mod, "main.go") || m.Branches || !m.Functions {
		t.Fatalf("files %+v, measured %+v", files, m)
	}
	lines, hits := files[0].LineCoverage()
	got := map[int]int{}
	for _, l := range lines {
		got[l] = min(hits[l], 1)
	}
	// ship's if and its first return ran, the second did not; the method's
	// blank line is no line; main's call ran.
	for line, want := range map[int]int{7: 1, 8: 1, 10: 0, 17: 0, 21: 1} {
		if h, ok := got[line]; !ok || h != want {
			t.Errorf("line %d: %d (%v), want %d; lines %v", line, h, ok, want, got)
		}
	}
	if _, ok := got[16]; ok {
		t.Error("the blank line counts")
	}
	fns := map[string]int{}
	for i, fn := range files[0].FnMap {
		fns[fn.Name] = files[0].F[i]
	}
	if fns["ship"] == 0 || fns["desk.close"] != 0 || fns["main"] == 0 || len(fns) != 3 {
		t.Errorf("functions: %v", fns)
	}
	if s := istanbul.Total(files); s.Lines.Total != 5 || s.Lines.Covered != 3 {
		t.Errorf("lines: %+v", s.Lines)
	}
}

// A service without a -cover build wrote nothing Go reads: no files, and
// coverage-core says why.
func TestNoCoverBuildIsNoCoverage(t *testing.T) {
	files, _, err := runtime{}.Convert(context.Background(), coveragecore.ConvertCall{End: core.CoverageEnd{CoverageService: core.CoverageService{Dir: t.TempDir()}}})
	if err != nil || files != nil {
		t.Errorf("%v %v", files, err)
	}
}

// In a container, Go writes into the mount.
func TestLaunchInAContainer(t *testing.T) {
	env, _ := runtime{}.Launch(context.Background(), coveragecore.LaunchCall{Service: core.CoverageService{Dir: "/host/.axx/coverage/raw/r1/api", Mount: "/axx-coverage"}})
	if strings.Join(env, " ") != "GOCOVERDIR=/axx-coverage" {
		t.Errorf("%v", env)
	}
}

func TestModulesAndFiles(t *testing.T) {
	project := t.TempDir()
	write(t, filepath.Join(project, "shipments", "go.mod"), "module example.com/shipments\n")
	write(t, filepath.Join(project, "shipments", "label", "go.mod"), "module example.com/shipments/label\n")
	s := core.CoverageService{Name: "shipments", WorkDir: filepath.Join(project, "shipments", "cmd")}
	if err := os.MkdirAll(s.WorkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := modulesOf(s, nil, project)
	if err != nil || len(found) != 1 || found[0].path != "example.com/shipments" {
		t.Fatalf("the service's module: %+v %v", found, err)
	}
	both, err := modulesOf(s, []string{"shipments", "shipments/label"}, project)
	if err != nil {
		t.Fatal(err)
	}
	for imp, want := range map[string]string{
		"example.com/shipments/cmd/main.go":   filepath.Join(project, "shipments", "cmd", "main.go"),
		"example.com/shipments/label/zpl.go":  filepath.Join(project, "shipments", "label", "zpl.go"),
		"github.com/acme/parcels/shipping.go": "",
	} {
		if got, _ := fileOf(both, imp); got != want {
			t.Errorf("%s: %s, want %s", imp, got, want)
		}
	}
	if _, err := modulesOf(core.CoverageService{Name: "x", WorkDir: t.TempDir()}, nil, project); err == nil || !strings.Contains(err.Error(), "modules") {
		t.Errorf("no go.mod: %v", err)
	}
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
