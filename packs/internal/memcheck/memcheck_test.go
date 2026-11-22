// SPDX-License-Identifier: Apache-2.0

package memcheck

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
	for _, name := range []string{"reports", "a folder", `a "quoted" folder`, "the checker's folder"} {
		dir := filepath.Join(t.TempDir(), name)
		t.Setenv(Env, dir)
		path := filepath.Join(dir, "report")
		// As the checker reads its options: the path whole, in quotes it has none of.
		want := "log_path='" + path + "' abort_on_error=1"
		if strings.Contains(path, "'") {
			want = `log_path="` + path + `" abort_on_error=1`
		}
		if got := options(); got != want {
			t.Errorf("options() = %s, want %s", got, want)
		}
		// And as C reads the source: a function that returns them, as a string literal.
		if got, c := string(Source()), strconv.Quote(want); !strings.Contains(got, "__asan_default_options") || !strings.Contains(got, c) {
			t.Errorf("Source() = %s, want the checker's options as %s", got, c)
		}
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

func TestTheLibrariesBuiltCheckedAreListed(t *testing.T) {
	t.Setenv(Env, "")
	if got, err := Record("/cache/axx-ar.dylib", nil); got != "/cache/axx-ar.dylib" || err != nil {
		t.Fatalf("Record() = %q, %v, want what it was given", got, err)
	}
	if listed, err := Libraries(); listed != nil || err != nil {
		t.Fatalf("nothing is checked, and Libraries() = %v, %v", listed, err)
	}

	// The folder is made when the first library is noted.
	dir := filepath.Join(t.TempDir(), "reports")
	t.Setenv(Env, dir)
	if listed, err := Libraries(); listed != nil || err != nil {
		t.Fatalf("before any is noted, Libraries() = %v, %v", listed, err)
	}
	for _, library := range []string{"/cache/axx-ar.dylib", "/cache/axx-simulator.dylib", "/cache/axx-ar.dylib"} {
		if got, err := Record(library, nil); got != library || err != nil {
			t.Fatalf("Record(%s) = %q, %v", library, got, err)
		}
	}
	// A library that did not build is none.
	failed := errors.New("clang: no such file")
	if _, err := Record("/cache/axx-models.dylib", failed); !errors.Is(err, failed) {
		t.Fatalf("Record() of a build that failed: %v", err)
	}
	listed, err := Libraries()
	if err != nil || strings.Join(listed, " ") != "/cache/axx-ar.dylib /cache/axx-simulator.dylib" {
		t.Errorf("Libraries() = %v, %v, want each once, in the order noted", listed, err)
	}
	// Reports and the list do not take each other for one another.
	if reports, err := Reports(); len(reports) != 0 || err != nil {
		t.Errorf("Reports() = %v, %v, want none", reports, err)
	}
}
