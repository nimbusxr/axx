package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/runner"
)

func TestPausesOffTheStepsAreTold(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	feature := `Feature: Track parcels

  Scenario: A registered parcel can be tracked
    # the parcel of the seed
    Given the parcel "AX-1001" is registered
    Then the parcel "AX-1001" can be tracked
`
	if err := os.MkdirAll(filepath.Join(dir, "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "features", "tracking.feature"), []byte(feature), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, _ := run(t, "run", "--dry-run", "features/tracking.feature",
		"--pause-at", "features/tracking.feature:4", "--pause-at", "features/tracking.feature:5", "--pause-at", "features/other.feature:3")
	for _, want := range []string{
		"axx: not pausing at features/tracking.feature:4: no step of this run is on that line",
		"axx: not pausing at features/other.feature:3: no step of this run is on that line",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "tracking.feature:5:") {
		t.Errorf("warned about a step's line:\n%s", stderr)
	}
}

func TestRerunLocationsKeepFilesOutsideTheProject(t *testing.T) {
	project, elsewhere := t.TempDir(), t.TempDir()
	t.Chdir(project)
	cfg := &config.Config{Dir: project}
	outside := filepath.Join(elsewhere, "probe.feature")
	for uri, want := range map[string]string{
		"features/tracking.feature": "features/tracking.feature:2",
		filepath.ToSlash(outside):   outside + ":2",
	} {
		r := &runner.ScenarioResult{Pickle: &feature.Pickle{Doc: &feature.Document{URI: uri}, Line: 2}}
		if got := scenarioLocation(cfg, r); got != want {
			t.Errorf("the location of %s is %s, want %s", uri, got, want)
		}
	}
}
