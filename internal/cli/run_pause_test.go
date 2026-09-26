package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
