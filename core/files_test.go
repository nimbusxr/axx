package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScenarioFilesAreFoundNextToTheFeature(t *testing.T) {
	project := t.TempDir()
	feature := filepath.Join(project, "features", "home")
	write := func(rel string) {
		t.Helper()
		p := filepath.Join(project, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("features/home/seeds/wen.yaml")
	write("screenshots/old.ios.png")
	write("features/home/screenshots/both.ios.png")
	write("screenshots/both.ios.png")

	suite := NewSuite(SuiteOptions{ProjectDir: project, ResolvePath: func(p string) (string, error) {
		return filepath.Join(project, p), nil
	}})
	sc := NewScenario(context.Background(), ScenarioInfo{Dir: feature}, suite, nil)

	if got, err := sc.ResolvePath("seeds/wen.yaml"); err != nil || got != filepath.Join(feature, "seeds", "wen.yaml") {
		t.Errorf("a file next to the feature: %s %v", got, err)
	}
	if got, _ := sc.ResolvePath("seeds/ana.yaml"); got != filepath.Join(project, "seeds", "ana.yaml") {
		t.Errorf("a file not next to it is resolved as the suite resolves it: %s", got)
	}
	for _, c := range []struct{ name, want string }{
		{"new.ios.png", filepath.Join(feature, "screenshots", "new.ios.png")},   // a new one is kept next to the feature
		{"old.ios.png", filepath.Join(project, "screenshots", "old.ios.png")},   // one kept in the project's folder stays found
		{"both.ios.png", filepath.Join(feature, "screenshots", "both.ios.png")}, // the feature's own comes first
	} {
		if got := sc.KeptFile("screenshots", c.name); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// A scenario with no feature file on disk keeps its files in the project.
	bare := NewScenario(context.Background(), ScenarioInfo{}, suite, nil)
	if got := bare.KeptFile("screenshots", "new.ios.png"); got != filepath.Join(project, "screenshots", "new.ios.png") {
		t.Errorf("no feature directory: %s", got)
	}
}
