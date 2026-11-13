package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
)

// A run with coverage asks the host to measure the services: what they get
// for their runtime is a definition of its own, approved once, the folder
// in it the same from run to run; their coverage goes to the run's folder
// in the project, and the run learns how they ended.
func TestTheHostMeasuresTheServicesOfARunWithCoverage(t *testing.T) {
	dir := realPath(t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got := filepath.Join(dir, "env")
	writeConfig(t, dir, "  api:\n"+
		"    command: ["+quote(exe)+", env-serve, "+quote(got)+", GOCOVERDIR, AGENT]\n"+
		"    ready:\n      log: ready\n      timeout: 20s\n"+
		"    coverage:\n      runtime: go\n")
	f := &fixture{dir: dir, cleanups: filepath.Join(dir, "cleanups"), approvals: filepath.Join(t.TempDir(), "approvals.json"), approver: &fakeApprover{}, events: &events{}}
	f.launch = func(_ context.Context, _ *config.Config, s core.CoverageService) ([]string, error) {
		if s.Name != "api" || s.Runtime != "go" || s.WorkDir != dir {
			t.Errorf("readied: %+v", s)
		}
		return []string{"GOCOVERDIR=" + s.Dir, "AGENT=" + filepath.Join(s.Dir, "agent.jar")}, nil
	}
	f.start(t)
	f.approver.answer = func(Question) bool { return true }

	for i, run := range []string{"r1", "r2"} {
		folder := filepath.Join(dir, ".axx", "coverage", "raw", run, "api")
		ls, err := f.open(t, Request{Command: "axx run --coverage", Dir: dir, Coverage: true, CoverageDir: ".axx/coverage/raw/" + run}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := ls.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(got)
		if want := "GOCOVERDIR=" + folder + "\nAGENT=" + filepath.Join(folder, "agent.jar"); !strings.Contains(string(b), want) {
			t.Errorf("run %d: the service got\n%s\nwant\n%s", i+1, b, want)
		}
		exits := ls.Exits()
		if len(exits) != 1 || !exits[0].Measured || exits[0].CoverageDir != folder || exits[0].Status == "" {
			t.Errorf("run %d: exits %+v", i+1, exits)
		}
	}
	qs := f.approver.asked()
	if len(qs) != 1 {
		t.Fatalf("asked %d times: a run with coverage is one definition, whatever its folder", len(qs))
	}
	if !strings.Contains(qs[0].Proposed.Text, "  GOCOVERDIR=${AXX_COVERAGE_DIR}\n  AGENT=${AXX_COVERAGE_DIR}/agent.jar") {
		t.Errorf("what the person approves:\n%s", qs[0].Proposed.Text)
	}

	// Its coverage goes to the project's raw folder, nowhere else.
	var ae *axxerr.Error
	for name, req := range map[string]Request{
		"outside the raw folder": {Coverage: true, CoverageDir: "../elsewhere"},
		"the raw folder itself":  {Coverage: true, CoverageDir: ".axx/coverage/raw"},
	} {
		if _, err := f.open(t, req, nil, nil); !errors.As(err, &ae) || ae.Code != CodeRefused {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A host whose axx has no coverage pack refuses a run with coverage,
// instead of starting the services unmeasured.
func TestAHostWithoutCoveragePacksRefusesARunWithCoverage(t *testing.T) {
	f := newFixture(t, "")
	f.approver.answer = func(Question) bool { return true }
	_, err := f.open(t, Request{Coverage: true, CoverageDir: ".axx/coverage/raw/r1"}, nil, nil)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeRefused || !strings.Contains(err.Error(), "coverage packs") {
		t.Fatalf("got %v", err)
	}
}
