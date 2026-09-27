package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

// In a project, the reference is the project's packs'.
func TestDocsExportDocumentsTheProjectsPacks(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest]\n"})
	out := filepath.Join(t.TempDir(), "reference")
	if _, stderr, code := run(t, "docs", "export", "--out", out); code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "packs", "rest.md")); err != nil {
		t.Errorf("the project's pack is not documented: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "packs", "sql.md")); err == nil {
		t.Error("a pack the project does not list is documented")
	}
}

func TestDocsExportSaysWhenAProjectListsNoPacks(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\n"})
	_, stderr, code := run(t, "docs", "export", "--out", filepath.Join(t.TempDir(), "reference"))
	if code != int(exitcode.Usage) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"AXX-E0014", "the project lists none", "axx pack add"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the error lacks %q:\n%s", want, stderr)
		}
	}
}
