package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/exitcode"
	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
	"github.com/nimbusxr/axx/packs/coverage/core/istanbul"
)

// keptRuntime is a runtime read while its services run, as JVM's and
// Node's are: a run resets it as it starts and takes it as it ends.
type keptRuntime struct{ resets, takes atomic.Int32 }

var keptTest = &keptRuntime{}

func init() { coveragecore.Register(keptTest) }

func (*keptRuntime) Name() string                { return "kept-test" }
func (*keptRuntime) Measured() istanbul.Measured { return istanbul.Measured{} }
func (*keptRuntime) Launch(context.Context, coveragecore.LaunchCall) ([]string, error) {
	return nil, nil
}

func (*keptRuntime) Convert(_ context.Context, call coveragecore.ConvertCall) ([]*istanbul.FileCoverage, istanbul.Measured, error) {
	f, err := os.Open(filepath.Join(call.End.Dir, "lcov.info"))
	if err != nil {
		return nil, istanbul.Measured{}, nil
	}
	defer f.Close()
	files, m, err := istanbul.ReadLcov(f)
	for _, fc := range files {
		fc.Path = call.Path(fc.Path)
	}
	return files, m, err
}

func (k *keptRuntime) Reset(context.Context, coveragecore.LaunchCall) error {
	k.resets.Add(1)
	return nil
}

func (k *keptRuntime) Take(_ context.Context, _ coveragecore.LaunchCall, into string) error {
	k.takes.Add(1)
	return os.WriteFile(filepath.Join(into, "lcov.info"), []byte("TN:\nSF:src/api.go\nDA:1,4\nDA:2,0\nend_of_record\n"), 0o644)
}

// keptProject is a project whose services axx up keeps, by this process:
// api, of a runtime read while it runs, and batch, which writes its
// coverage only as it exits. coverage is what up.json says it measures.
func keptProject(t *testing.T, coverage map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(helperEnv, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	writeFiles(t, dir, map[string]string{
		"axx.yaml": "services:\n" +
			"  api:\n    command: ./never-started\n    coverage:\n      runtime: kept-test\n" +
			"  batch:\n    command: ./never-started\n    coverage:\n      runtime: report\n",
		"axx-packs.yaml": "packs: [cli, coverage-core]\n",
		"features/kept.feature": "Feature: Kept\n\n  Scenario: a command\n" +
			"    Given the echo command with the following properties:\n      | command | " + q(exe) + " append " + q(filepath.Join(dir, "out")) + " x |\n" +
			"    When the echo command is run\n    Then the echo command's exit code is 0\n",
	})
	st := upState{PID: os.Getpid(), Services: []string{"api", "batch"}, Started: time.Now(), Coverage: coverage}
	b, _ := json.Marshal(st)
	writeFiles(t, dir, map[string]string{".axx/run/up.json": string(b)})
	return dir
}

// A run with coverage against the services axx up keeps reads api while it
// runs, reset as the run starts; batch writes its coverage only as axx down
// stops it, which the run says.
func TestARunWithCoverageOfKeptServices(t *testing.T) {
	dir := keptProject(t, map[string]string{"api": "kept-test", "batch": "report"})
	resets, takes := keptTest.resets.Load(), keptTest.takes.Load()
	rep, ok, stderr, code := runReport(t, "run", "--coverage")
	if code != int(exitcode.OK) || !ok {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	if keptTest.resets.Load()-resets != 1 || keptTest.takes.Load()-takes != 1 {
		t.Errorf("resets %d, takes %d", keptTest.resets.Load()-resets, keptTest.takes.Load()-takes)
	}
	parts := map[string]string{}
	for _, p := range rep.Coverage.Parts {
		lines := ""
		if p.Totals.Lines != nil {
			lines = strconv.Itoa(p.Totals.Lines.Covered) + "/" + strconv.Itoa(p.Totals.Lines.Total)
		}
		parts[p.Name] = lines + p.Missing
	}
	if parts["api"] != "1/2" || !strings.Contains(parts["batch"], "axx down writes it") {
		t.Errorf("parts: %v", parts)
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx", "run", "coverage.lock")); !os.IsNotExist(err) {
		t.Errorf("the lock outlived the run: %v", err)
	}

	// Another run that measures them holds the lock: this one says so.
	writeFiles(t, dir, map[string]string{".axx/run/coverage.lock": strconv.Itoa(os.Getpid()) + "\n"})
	if _, stderr, code := run(t, "run", "--coverage"); code != int(exitcode.Environment) || !strings.Contains(stderr, "another run") {
		t.Errorf("a held lock: exit %d:\n%s", code, stderr)
	}
}

// Services axx up keeps without coverage cannot be measured by a run.
func TestARunWithCoverageOfServicesKeptWithout(t *testing.T) {
	keptProject(t, nil)
	if _, stderr, code := run(t, "run", "--coverage"); code != int(exitcode.Usage) || !strings.Contains(stderr, codeKeptUnmeasured) || !strings.Contains(stderr, "axx up --coverage") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
}
