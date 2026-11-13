package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/config"
)

// A service with a coverage section runs its coverage command and env, and
// what its runtime adds, when the run measures it; debug mode wins. Its
// folder is named on every run.
func TestCoverage(t *testing.T) {
	t.Parallel()
	vars := []string{CoverageEnv, "COV_FLAG", "AGENT"}
	for _, c := range []struct {
		name       string
		opts       Options
		noSection  bool
		want       string // the line the service wrote
		measured   bool
		unmeasured string
	}{
		{
			name: "measured", opts: Options{Coverage: map[string]bool{"api": true}, CoverageEnv: map[string][]string{"api": {"AGENT=jacoco"}}},
			want: "coverage AXX_COVERAGE_DIR=<run>/api COV_FLAG=on AGENT=jacoco", measured: true,
		},
		{
			name: "not measured", opts: Options{CoverageEnv: map[string][]string{"api": {"AGENT=jacoco"}}},
			want: "normal AXX_COVERAGE_DIR=<unmeasured>/api COV_FLAG= AGENT=",
		},
		{
			name: "debug wins", opts: Options{Coverage: map[string]bool{"api": true}, Debug: map[string]bool{"api": true}},
			want: "debug AXX_COVERAGE_DIR=<unmeasured>/api COV_FLAG= AGENT=", unmeasured: "debug mode",
		},
		{
			name: "no coverage section", opts: Options{Coverage: map[string]bool{"api": true}}, noSection: true,
			want: "normal AXX_COVERAGE_DIR= COV_FLAG= AGENT=",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "env")
			app := logReady(helperApp(t, "api", "getenvs", append([]string{file, "normal"}, vars...)...), "started")
			app.Debug = &config.Debug{Command: helper(t, "getenvs", append([]string{file, "debug"}, vars...)...)}
			if !c.noSection {
				app.Coverage = &config.Coverage{
					Runtime: "jvm", Command: helper(t, "getenvs", append([]string{file, "coverage"}, vars...)...),
					Env: map[string]string{"COV_FLAG": "on"},
				}
			}
			c.opts.CoverageDir = filepath.Join(t.TempDir(), "run")
			h := newHarness(t, config.Services{app}, c.opts)
			if err := h.Start(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if err := h.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.NewReplacer(c.opts.CoverageDir, "<run>", filepath.Join(h.dir, ".axx", "coverage", "raw", "unmeasured"), "<unmeasured>").Replace(strings.TrimSpace(string(b)))
			if got != c.want {
				t.Errorf("the service got\n  %s\nwant\n  %s", got, c.want)
			}
			exits := h.Exits()
			if len(exits) != 1 {
				t.Fatalf("exits: %+v", exits)
			}
			e := exits[0]
			if e.Measured != c.measured || !strings.Contains(e.Unmeasured, c.unmeasured) || (c.unmeasured == "") != (e.Unmeasured == "") {
				t.Errorf("exit: %+v", e)
			}
			if c.measured && e.CoverageDir != filepath.Join(c.opts.CoverageDir, "api") {
				t.Errorf("its coverage folder: %s", e.CoverageDir)
			}
			if e.Killed || e.OnItsOwn || e.Status == "" {
				t.Errorf("it stopped when asked: %+v", e)
			}
		})
	}
}

// A service axx had to kill after its grace wrote nothing as it exited: the
// run says so instead of reading an empty coverage.
func TestExitKilledAfterItsGrace(t *testing.T) {
	t.Parallel()
	skipOnWindows(t, "stop signals")
	app := logReady(helperApp(t, "api", "stubborn", filepath.Join(t.TempDir(), "events")), "ready")
	app.Stop.Grace = config.Duration(100 * time.Millisecond)
	app.Coverage = &config.Coverage{Runtime: "go"}
	h := newHarness(t, config.Services{app}, Options{Coverage: map[string]bool{"api": true}})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e := h.Exits(); len(e) != 1 || !e[0].Killed || !e[0].Measured || !strings.Contains(e[0].Status, "killed") {
		t.Errorf("exits: %+v", e)
	}
}
