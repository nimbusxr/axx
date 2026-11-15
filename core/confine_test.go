package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A suite the axx host makes for an agent's run reads the project's files
// only: a file a step names elsewhere is refused, a link that leads out of
// the project too.
func TestAConfinedSuiteReadsItsProjectOnly(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project, outside := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, f := range []string{"project/labels/label.png", "project/features/round/photo.jpg", "home/.ssh/id"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, ".ssh"), filepath.Join(project, "keys")); err != nil {
		t.Skip("no symbolic links here:", err)
	}
	if err := os.Symlink(filepath.Join(project, "labels"), filepath.Join(project, "same")); err != nil {
		t.Fatal(err)
	}
	s := NewSuite(SuiteOptions{ProjectDir: project, ResolvePath: func(p string) (string, error) {
		if filepath.IsAbs(p) {
			return p, nil
		}
		return filepath.Join(project, filepath.FromSlash(p)), nil
	}})
	// Not confined: a suite reads what its steps name.
	if _, err := s.ResolvePath(filepath.Join(outside, ".ssh", "id")); err != nil {
		t.Fatalf("a suite that is not confined: %v", err)
	}
	s.Confine(project)
	for _, ok := range []string{"labels/label.png", "same/label.png", "labels/not-there-yet.png", "made/later/label.png"} {
		if _, err := s.ResolvePath(ok); err != nil {
			t.Errorf("%s is in the project: %v", ok, err)
		}
	}
	for _, out := range []string{filepath.Join(outside, ".ssh", "id"), "../home/.ssh/id", "keys/id", "keys/not-there", systemFile()} {
		_, err := s.ResolvePath(out)
		if err == nil || !strings.Contains(err.Error(), "is not in the project") {
			t.Errorf("%s is not in the project: %v", out, err)
		}
	}
	// A file found next to the feature file is held to the same.
	feature := filepath.Join(project, "features", "round")
	if got, err := s.ResolvePathFrom(feature, "photo.jpg"); err != nil || got != filepath.Join(feature, "photo.jpg") {
		t.Errorf("next to the feature: %q %v", got, err)
	}
	if err := os.Symlink(filepath.Join(outside, ".ssh", "id"), filepath.Join(feature, "id.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolvePathFrom(feature, "id.jpg"); err == nil {
		t.Error("a link next to the feature that leads out of the project was read")
	}
	// A project whose own folder is reached through a link is the same project.
	if err := os.Symlink(project, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	linked := NewSuite(SuiteOptions{})
	linked.Confine(filepath.Join(root, "link"))
	if _, err := linked.ResolvePath(filepath.Join(project, "labels", "label.png")); err != nil {
		t.Errorf("the project by its real path: %v", err)
	}
}

// systemFile is a file of the system's, by its whole path. (On Windows, /etc/hosts is no whole
// path: it has no drive, and is read from the project's folder, where it is in the project.)
func systemFile() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\drivers\etc\hosts`
	}
	return "/etc/hosts"
}
