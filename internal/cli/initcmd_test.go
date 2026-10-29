package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A compose file is often the stack its developers run every day: axx's runs
// apart from it, so that its cleanup never removes that stack's volumes.
func TestInitStartsAComposeFileAsAStackOfItsOwn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Parcels_API")
	writeFiles(t, dir, map[string]string{"compose.yaml": "services: {}\n"})
	t.Chdir(dir)
	if out, stderr, code := run(t, "init", "--no-ci", "--no-agents"); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, "axx.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"command: docker compose -f compose.yaml -p parcels-api-axx up --build\n",
		"cleanup: docker compose -f compose.yaml -p parcels-api-axx down -v --remove-orphans\n",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("axx.yaml lacks %q:\n%s", want, b)
		}
	}
	if out, stderr, code := run(t, "validate"); code != 0 {
		t.Errorf("the files init wrote do not validate: exit %d: %s%s", code, out, stderr)
	}
}

func TestInitNamesTheStackAsComposeAllows(t *testing.T) {
	for dir, want := range map[string]string{"/src/depot": "depot-axx", "/src/_api": "api-axx", "/src/__": "axx"} {
		if got := detect(dir).Stack; got != want {
			t.Errorf("detect(%q).Stack = %q, want %q", dir, got, want)
		}
	}
}
