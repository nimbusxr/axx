// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/selfupdate"
)

// machineCache gives the test a cache of its own, and returns axx's folder
// in it, with: a build nothing used for two months, one used yesterday, a
// release nothing used for two months, and tools.
func machineCache(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("LocalAppData", filepath.Join(home, "AppData", "Local"))
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "axx")
	writeFiles(t, root, map[string]string{
		"builds/old/axx":         "an old build",
		"builds/old.lock":        "",
		"builds/fresh/axx":       "a build in use",
		"builds/fresh/used":      "",
		"versions/0.4.2/axx":     "an old release",
		"versions/0.4.2/used":    "",
		"models-0a1b2c/runtime":  "a model runtime",
		"models-real/model.onnx": "a model",
		"go/path/mod":            "modules",
		"upgrade-reminder.json":  "{}",
	})
	long := time.Now().Add(-60 * 24 * time.Hour)
	for _, f := range []string{"builds/old/axx", "builds/old.lock", "versions/0.4.2/axx", "versions/0.4.2/used"} {
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(f)), long, long); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCacheListsWhatIsKept(t *testing.T) {
	root := machineCache(t)
	out, stderr, code := run(t, "cache")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	for _, want := range []string{root, "models", "builds", "versions", "go", "2 binaries of axx", "went unused for 30 days", "axx cache clean"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	out, _, _ = run(t, "cache", "--json")
	data := envelope(t, out).Data.(map[string]any)
	if data["path"] != root || len(data["groups"].([]any)) != 4 || data["keepDays"].(float64) != 30 || data["unused"].(map[string]any)["binaries"].(float64) != 2 {
		t.Errorf("json: %s", out)
	}
}

func TestCacheCleanRemovesTheBinariesNothingUsed(t *testing.T) {
	root := machineCache(t)
	out, _, code := run(t, "cache", "clean", "--dry-run")
	if code != int(exitcode.OK) || !strings.Contains(out, "would remove 2 binaries") {
		t.Fatalf("dry run, exit %d: %s", code, out)
	}
	if _, gone := present(root, "builds/old", "versions/0.4.2"); len(gone) > 0 {
		t.Fatalf("a dry run removed %v", gone)
	}
	out, stderr, code := run(t, "cache", "clean")
	if code != int(exitcode.OK) || !strings.Contains(out, "removed 2 binaries") {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	if there, _ := present(root, "builds/old", "builds/old.lock", "versions/0.4.2"); len(there) > 0 {
		t.Errorf("still there: %v", there)
	}
	if _, gone := present(root, "builds/fresh/axx", "models-0a1b2c", "models-real", "go", "upgrade-reminder.json"); len(gone) > 0 {
		t.Errorf("removed what is in use, or is no binary: %v", gone)
	}
	if out, _, _ := run(t, "cache", "clean"); !strings.Contains(out, "nothing to remove") {
		t.Errorf("a second clean: %s", out)
	}
}

func TestCacheCleanRemovesThePartsNamed(t *testing.T) {
	root := machineCache(t)
	out, _, code := run(t, "cache", "clean", "models", "--json")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, out)
	}
	if got := len(envelope(t, out).Data.(map[string]any)["removed"].([]any)); got != 2 {
		t.Errorf("removed %d folders of models, want 2: %s", got, out)
	}
	if there, _ := present(root, "models-0a1b2c", "models-real"); len(there) > 0 {
		t.Errorf("still there: %v", there)
	}
	if _, gone := present(root, "builds/old", "builds/fresh", "go", "versions/0.4.2"); len(gone) > 0 {
		t.Errorf("removed a part that was not named: %v", gone)
	}

	_, stderr, code := run(t, "cache", "clean", "browsers")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, `no part "browsers"`) || !strings.Contains(stderr, "builds, go, versions") {
		t.Errorf("a part the cache has not, exit %d:\n%s", code, stderr)
	}
	if _, stderr, code := run(t, "cache", "clean", "--all", "go"); code != int(exitcode.Usage) || !strings.Contains(stderr, "not both") {
		t.Errorf("--all with a name, exit %d:\n%s", code, stderr)
	}
}

func TestCacheCleanAll(t *testing.T) {
	root := machineCache(t)
	out, stderr, code := run(t, "cache", "clean", "--all")
	if code != int(exitcode.OK) || !strings.Contains(out, "removed") {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	if there, _ := present(root, "builds", "versions", "models-0a1b2c", "models-real", "go"); len(there) > 0 {
		t.Errorf("still there: %v", there)
	}
	if out, _, _ := run(t, "cache"); !strings.Contains(out, "is empty") {
		t.Errorf("the cache after: %s", out)
	}
}

func TestCacheKeepDays(t *testing.T) {
	machineCache(t)
	// A week: the old ones are past it too.
	if out, _, code := runEnv(t, map[string]string{envCacheKeepDays: "7"}, "cache"); code != int(exitcode.OK) || !strings.Contains(out, "went unused for 7 days") {
		t.Errorf("7 days, exit %d: %s", code, out)
	}
	// None by itself: the command still removes on request, by 30 days.
	if out, _, code := runEnv(t, map[string]string{envCacheKeepDays: "0"}, "cache"); code != int(exitcode.OK) || !strings.Contains(out, "=0 keeps axx from doing so by itself") {
		t.Errorf("0 days, exit %d: %s", code, out)
	}
	if _, stderr, code := runEnv(t, map[string]string{envCacheKeepDays: "soon"}, "cache"); code != int(exitcode.Usage) || !strings.Contains(stderr, "not a number of days") {
		t.Errorf("no number, exit %d:\n%s", code, stderr)
	}
}

// Before axx hands over to the release a project names, it marks that
// release as used and removes the binaries nothing used for long: by
// itself, at most once a day.
func TestHandingOverTidiesTheCache(t *testing.T) {
	for _, tc := range []struct {
		name             string
		env              map[string]string
		lastLooked       time.Duration
		gone, stillThere []string
	}{
		{
			"a day after the last look", nil, 48 * time.Hour,
			[]string{"builds/old", "versions/0.4.2"},
			[]string{"builds/fresh", "versions/0.5.0"},
		},
		{
			"the same day as the last look", nil, time.Hour,
			nil,
			[]string{"builds/old", "versions/0.4.2", "builds/fresh", "versions/0.5.0"},
		},
		{
			"releases put there by hand stay",
			map[string]string{envAutoDownload: "0"},
			48 * time.Hour,
			[]string{"builds/old"},
			[]string{"versions/0.4.2", "builds/fresh", "versions/0.5.0"},
		},
		{
			"told to keep everything",
			map[string]string{envCacheKeepDays: "0"},
			48 * time.Hour,
			nil,
			[]string{"builds/old", "versions/0.4.2", "builds/fresh", "versions/0.5.0"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, cache := pinnedProject(t, "version: 1\naxx: 0.5.0\n")
			stamp(t, "0.9.0")
			to := watchHandOver(t)
			root := filepath.Join(cache, "axx")
			writeFiles(t, root, map[string]string{
				"versions/0.5.0/" + selfupdate.BinaryName(runtime.GOOS): "the release the project names",
				"versions/0.4.2/axx":  "a release nothing names any more",
				"versions/0.4.2/used": "",
				"builds/old/axx":      "an old build",
				"builds/fresh/axx":    "a build in use",
				"builds/fresh/used":   "",
				".tidied":             "",
			})
			long := time.Now().Add(-60 * 24 * time.Hour)
			for _, f := range []string{"versions/0.4.2/axx", "versions/0.4.2/used", "builds/old/axx"} {
				if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(f)), long, long); err != nil {
					t.Fatal(err)
				}
			}
			looked := time.Now().Add(-tc.lastLooked)
			if err := os.Chtimes(filepath.Join(root, ".tidied"), looked, looked); err != nil {
				t.Fatal(err)
			}

			_, stderr, _ := runEnv(t, tc.env, "validate")
			if want := versionBinary(cache, "0.5.0"); *to != want {
				t.Fatalf("handed over to %q, want %q; %s", *to, want, stderr)
			}
			if there, _ := present(root, tc.gone...); len(there) > 0 {
				t.Errorf("still there: %v", there)
			}
			if _, gone := present(root, append(tc.stillThere, "versions/0.5.0/used")...); len(gone) > 0 {
				t.Errorf("gone, or not marked as used: %v", gone)
			}
			if strings.Contains(stderr, "removing") {
				t.Errorf("it said so, though it was quick: %s", stderr)
			}
		})
	}
}
