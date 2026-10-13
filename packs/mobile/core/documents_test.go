package mobilecore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// A registration's documents are found next to the feature file first; the phone has them in one
// folder, by their names, so two of one name are refused, as is a folder.
func TestDocuments(t *testing.T) {
	feature := t.TempDir()
	for _, f := range []string{"documents/safe-place-PX-MOB-9401.pdf", "scans/pod-PX-MOB-9402.jpg", "other/safe-place-PX-MOB-9401.pdf"} {
		path := filepath.Join(feature, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sc := core.NewScenario(t.Context(), core.ScenarioInfo{Dir: feature}, core.NewSuite(core.SuiteOptions{}), nil)

	got, err := Documents(sc, "documents/safe-place-PX-MOB-9401.pdf, scans/pod-PX-MOB-9402.jpg,")
	want := []string{filepath.Join(feature, "documents", "safe-place-PX-MOB-9401.pdf"), filepath.Join(feature, "scans", "pod-PX-MOB-9402.jpg")}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("documents %v, %v; want %v", got, err, want)
	}
	for value, why := range map[string]string{
		"documents/safe-place-PX-MOB-9401.pdf, other/safe-place-PX-MOB-9401.pdf": "both safe-place-PX-MOB-9401.pdf",
		"documents":                      "is a folder",
		"documents/no-such-document.pdf": "no-such-document.pdf",
	} {
		if _, err := Documents(sc, value); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%s: %v, want an error saying %q", value, err, why)
		}
	}
}
