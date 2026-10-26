package core

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// What a scenario puts in a folder is as open as the folder: a service of
// another user takes it from a folder its deployment shares (0777), and a
// folder of the machine's user alone keeps it so (0755).
func TestAFileWrittenToAFolderIsAsOpenAsTheFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX permissions")
	}
	for _, c := range []struct{ folder, sub, file fs.FileMode }{
		{0o777, 0o777, 0o666},
		{0o755, 0o755, 0o644},
		{0o700, 0o700, 0o600},
	} {
		dir := filepath.Join(t.TempDir(), "uploads")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, c.folder); err != nil {
			t.Fatal(err)
		}
		if err := LocalFiles(dir).Write(context.Background(), "PX-5103/photos/porch.png", []byte("png")); err != nil {
			t.Fatal(err)
		}
		for path, want := range map[string]fs.FileMode{"PX-5103": c.sub, "PX-5103/photos": c.sub, "PX-5103/photos/porch.png": c.file} {
			fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != want {
				t.Errorf("a folder of %v: %s is %v, want %v", c.folder, path, fi.Mode().Perm(), want)
			}
		}
	}
}
