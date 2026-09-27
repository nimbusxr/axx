//go:build integration

package packbuild

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/packset"
)

// TestBuildWithLocalPack builds axx (from this checkout) with one of axx's
// packs and a custom pack with a dependency of its own, and runs a feature
// that uses them. It builds with
// the installed go; internal/gotool tests the toolchain axx downloads.
func TestBuildWithLocalPack(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on PATH")
	}
	t.Setenv("AXX_GO", goBin)
	src, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	proj := localPackProject(t, nil)
	f, _, err := packset.Load(proj)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := f.Entries()
	res, err := Ensure(context.Background(), Options{
		ProjectDir: proj, Entries: entries, AxxVersion: "v0.0.0", AxxSource: src, CacheDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(res.Binary, "run", "--format", "progress")
	cmd.Dir = proj
	cmd.Env = append(os.Environ(), "AXX_PACK_BUILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "1 passed") {
		t.Fatalf("run: %v\n%s", err, out)
	}
	// The second call reuses the cached build.
	again, err := Ensure(context.Background(), Options{
		ProjectDir: proj, Entries: entries, AxxVersion: "v0.0.0", AxxSource: src, CacheDir: filepath.Dir(filepath.Dir(res.Binary)),
	})
	if err != nil || !again.Cached || again.Binary != res.Binary {
		t.Fatalf("cache: %+v %v", again, err)
	}
}

// localPackProject writes a project with one of axx's packs (rest) and a
// custom pack with a dependency of its own, a feature that uses both, and
// the extra files given.
func localPackProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	proj := t.TempDir()
	for _, dir := range []string{"features", "steps"} {
		if err := os.MkdirAll(filepath.Join(proj, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"axx.yaml":           "version: 1\n",
		"axx-packs.yaml":     "packs: [rest, ./steps]\n",
		"steps/go.mod":       "module axx.local/steps\n\ngo 1.27\n\nrequire (\n\tgithub.com/nimbusxr/axx v0.0.0\n\trsc.io/quote/v3 v3.1.0\n)\n",
		"features/f.feature": "Feature: f\n  Scenario: s\n    Given the parcels service with the following properties:\n      | url | http://localhost:8080 |\n    Then the custom step runs\n",
		"steps/pack.go": `package steps

import (
	"github.com/nimbusxr/axx/core"
	"rsc.io/quote/v3"
)

func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: "steps", Steps: []core.StepDef{{
		ID: "steps.runs", Expr: "the custom step runs",
		Run: func(sc *core.Scenario, _ core.Args) error { sc.Log(quote.GoV3()); return nil },
	}}}
}
`,
	}
	maps.Copy(files, extra)
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(proj, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return proj
}

// TestUpRunDownWithPacks runs axx, as people install it (the core, from
// this checkout), in a project with packs: every command switches to the
// project's build, up's supervisor too, so the apps it keeps running and
// the runs that reuse them see the same packs.
func TestUpRunDownWithPacks(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on PATH")
	}
	src, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	axx := filepath.Join(t.TempDir(), "axx")
	build := exec.Command(goBin, "build", "-o", axx, "./cmd/axx")
	build.Dir = src
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building axx: %v\n%s", err, out)
	}
	proj := localPackProject(t, map[string]string{
		"axx.yaml": `version: 1
apps:
  api:
    command: [sh, -c, "echo api ready; exec sleep 300"]
    ready: {log: "api ready", timeout: 30s}
`,
	})
	cache := t.TempDir()
	axxIn := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(axx, args...)
		cmd.Dir = proj
		// The build of the project's packs comes from this checkout, with the go on PATH,
		// in a cache of the test's own on Linux.
		cmd.Env = append(os.Environ(), "AXX_GO="+goBin, "AXX_SOURCE_DIR="+src, "XDG_CACHE_HOME="+cache, "AXX_PACK_BUILD=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("axx %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	axxIn("up")
	t.Cleanup(func() {
		cmd := exec.Command(axx, "down")
		cmd.Dir = proj
		_ = cmd.Run()
	})
	if out := axxIn("run", "--format", "progress"); !strings.Contains(out, "1 passed") {
		t.Fatalf("run:\n%s", out)
	}
	axxIn("down")
}
