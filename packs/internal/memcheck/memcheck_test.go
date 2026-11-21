// SPDX-License-Identifier: Apache-2.0

package memcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOffByDefault(t *testing.T) {
	t.Setenv(Env, "")
	if Enabled() || Dir() != "" || Key() != nil || Clang() != nil || Swift() != nil {
		t.Fatalf("with %s unset: Enabled=%v Dir=%q Key=%q Clang=%v Swift=%v, want nothing checked", Env, Enabled(), Dir(), Key(), Clang(), Swift())
	}
	if found, err := Reports(); err != nil || found != nil {
		t.Fatalf("Reports() = %v, %v, want none", found, err)
	}
}

func TestCheckedBuild(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(Env, dir)
	if !Enabled() || Dir() != dir {
		t.Fatalf("Enabled=%v Dir=%q, want %q", Enabled(), Dir(), dir)
	}
	if got := strings.Join(Clang(), " "); !strings.Contains(got, "-fsanitize=address") {
		t.Errorf("Clang() = %q, want AddressSanitizer", got)
	}
	if got := strings.Join(Swift(), " "); got != "-sanitize=address" {
		t.Errorf("Swift() = %q", got)
	}
	// The usual build and the checked one, and two that report elsewhere, keep apart in the cache.
	here := string(Key())
	t.Setenv(Env, t.TempDir())
	if elsewhere := string(Key()); here == "" || here == elsewhere {
		t.Errorf("Key() = %q for one folder and %q for another, want two keys", here, elsewhere)
	}
}

func TestSourceNamesTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), `a "quoted" folder`)
	t.Setenv(Env, dir)
	got := string(Source())
	want := `log_path=` + strings.ReplaceAll(filepath.Join(dir, "report"), `"`, `\"`)
	if !strings.Contains(got, "__asan_default_options") || !strings.Contains(got, want) {
		t.Errorf("Source() = %s, want the checker's options with %s", got, want)
	}
}

func TestReports(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(Env, dir)
	for _, name := range []string{"report.812", "report.77", "other.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	found, err := Reports()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "report.77"), filepath.Join(dir, "report.812")}
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Errorf("Reports() = %v, want %v", found, want)
	}
}
