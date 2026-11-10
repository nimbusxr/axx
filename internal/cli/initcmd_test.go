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

// The CI workflow init writes checks the features' layout before it runs
// them, and what init writes passes that check.
func TestInitsWorkflowChecksTheFeaturesAreFormatted(t *testing.T) {
	for _, files := range []map[string]string{
		{"README.md": "# Parcels\n"},
		// The smoke feature's table gets a row for the OpenAPI file.
		{"README.md": "# Parcels\n", "api/openapi.yaml": "openapi: 3.0.3\ninfo: {title: Parcels, version: '1'}\npaths: {}\n"},
	} {
		initFormatted(t, files)
	}
}

func initFormatted(t *testing.T, files map[string]string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "parcels")
	writeFiles(t, dir, files)
	t.Chdir(dir)
	if out, stderr, code := run(t, "init", "--no-agents"); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".github/workflows/acceptance.yml"))
	if err != nil {
		t.Fatal(err)
	}
	check, runs := strings.Index(string(b), "- run: axx fmt --check\n"), strings.Index(string(b), "- run: axx run ")
	if check < 0 || runs < 0 || check > runs {
		t.Errorf("the workflow does not check the layout before the run:\n%s", b)
	}
	if out, stderr, code := run(t, "fmt", "--check"); code != 0 {
		t.Errorf("the features init wrote are not formatted: exit %d: %s%s", code, out, stderr)
	}
	if _, ok := files["api/openapi.yaml"]; ok {
		smoke, _ := os.ReadFile(filepath.Join(dir, "features/smoke/smoke.feature"))
		if !strings.Contains(string(smoke), "      | openapi | api/openapi.yaml") {
			t.Errorf("the smoke feature lacks the OpenAPI file's row:\n%s", smoke)
		}
	}
}
