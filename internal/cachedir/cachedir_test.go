// SPDX-License-Identifier: Apache-2.0

package cachedir

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/filelock"
)

const day = 24 * time.Hour

// build puts a build in the cache at root that was made ago before now and
// never marked as used; release names the axx it is of, when not "".
func build(t *testing.T, root, key string, ago time.Duration, release string) string {
	t.Helper()
	dir := filepath.Join(Builds(root), key)
	write(t, filepath.Join(dir, "axx"), "binary")
	write(t, filepath.Join(dir, "resolved.json"), "{}")
	if release != "" {
		write(t, filepath.Join(dir, "src", "go.mod"), "module axx.local/build\n\ngo 1.27\n\nrequire "+axxModule+" v"+release+"\n")
	}
	write(t, dir+".lock", "")
	at := time.Now().Add(-ago)
	for _, f := range []string{"axx", "resolved.json"} {
		age(t, filepath.Join(dir, f), at)
	}
	age(t, dir+".lock", at)
	return dir
}

// release puts a release of axx in the cache at root.
func release(t *testing.T, root, v string) string {
	t.Helper()
	dir := filepath.Join(Versions(root), v)
	write(t, filepath.Join(dir, "axx"), "binary")
	return dir
}

// used marks dir as used ago before now.
func used(t *testing.T, dir string, ago time.Duration) {
	t.Helper()
	p := filepath.Join(dir, usedName)
	write(t, p, "")
	age(t, p, time.Now().Add(-ago))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func age(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func names(bins []Binary) []string {
	var out []string
	for _, b := range bins {
		out = append(out, b.Kind+" "+b.Name)
	}
	slices.Sort(out)
	return out
}

func TestUseMarksABinaryAsUsedNow(t *testing.T) {
	dir := build(t, t.TempDir(), "k", 60*day, "")
	if UsedLately(dir) {
		t.Fatal("a build nothing marked was used lately")
	}
	Use(dir)
	if !UsedLately(dir) {
		t.Fatal("a build marked now was not used lately")
	}
	// An old mark is moved to now.
	used(t, dir, 3*day)
	if UsedLately(dir) {
		t.Fatal("a mark of three days ago is lately")
	}
	Use(dir)
	if at, _ := usedAt(dir); time.Since(at) > time.Minute {
		t.Errorf("the mark is still of %s", at)
	}
}

// A folder this process cannot write keeps the time it had, without a word.
func TestUseOfWhatCannotBeWrittenDoesNothing(t *testing.T) {
	Use(filepath.Join(t.TempDir(), "not", "there"))
}

func TestBinariesSayWhenEachWasUsed(t *testing.T) {
	root := t.TempDir()
	marked := build(t, root, "marked", 60*day, "0.9.0")
	used(t, marked, 2*day)
	build(t, root, "unmarked", 60*day, "")
	// A build an earlier release made has no mark: it is as used as its
	// release, which the axx that hands over to it marks.
	build(t, root, "of-a-release-in-use", 60*day, "0.5.0")
	used(t, release(t, root, "0.5.0"), 3*day)
	build(t, root, "of-a-release-out-of-use", 60*day, "0.4.2")
	used(t, release(t, root, "0.4.2"), 45*day)
	// A release nothing marked yet is in use from now.
	unmarked := release(t, root, "0.6.0")

	got := map[string]time.Duration{}
	for _, b := range Binaries(root) {
		got[b.Name] = time.Since(b.Used).Round(day)
		if b.Bytes == 0 {
			t.Errorf("%s has no size", b.Name)
		}
	}
	want := map[string]time.Duration{
		"marked": 2 * day, "unmarked": 60 * day, "of-a-release-in-use": 3 * day, "of-a-release-out-of-use": 45 * day,
		"0.5.0": 3 * day, "0.4.2": 45 * day, "0.6.0": 0,
	}
	for name, ago := range want {
		if got[name] != ago {
			t.Errorf("%s was used %s ago, want %s", name, got[name], ago)
		}
	}
	if len(got) != len(want) {
		t.Errorf("binaries: %v", got)
	}
	if !UsedLately(unmarked) {
		t.Error("the release nothing had marked got no mark")
	}
}

func TestStaleLeavesWhatIsUsedAndWhatRuns(t *testing.T) {
	root := t.TempDir()
	build(t, root, "old", 60*day, "")
	running := build(t, root, "running", 60*day, "")
	fresh := build(t, root, "fresh", 60*day, "")
	used(t, fresh, day)
	got := Stale(Binaries(root), time.Now(), Keep, running)
	if want := []string{"build old"}; !slices.Equal(names(got), want) {
		t.Errorf("stale: %v, want %v", names(got), want)
	}
}

func TestRemoveLeavesABinaryThatIsBeingMade(t *testing.T) {
	root := t.TempDir()
	dir := build(t, root, "k", 60*day, "")
	b := Binary{Kind: "build", Name: "k", Path: dir}
	unlock, ok, err := filelock.TryLock(dir + ".lock")
	if err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	if Remove(b, nil) || !exists(dir) {
		t.Fatal("a build whose lock is held was removed")
	}
	unlock()
	if Remove(b, func() bool { return false }) || !exists(dir) {
		t.Fatal("a build that was used meanwhile was removed")
	}
	if !Remove(b, nil) || exists(dir) || exists(dir+".lock") {
		t.Fatal("the build, or its lock, is still there")
	}
}

func TestTidyRemovesWhatNothingUsedAtMostOnceADay(t *testing.T) {
	root := t.TempDir()
	old := build(t, root, "old", 60*day, "")
	fresh := build(t, root, "fresh", 60*day, "")
	used(t, fresh, day)
	oldRelease := release(t, root, "0.4.2")
	used(t, oldRelease, 45*day)

	// The first look removes nothing: it only notes when it looked.
	if got := Tidy(root, Keep, true); len(got) != 0 || !exists(old) {
		t.Fatalf("the first look removed %v", names(got))
	}
	// Nor does a look within a day of the last.
	if got := Tidy(root, Keep, true); len(got) != 0 || !exists(old) {
		t.Fatalf("a second look the same day removed %v", names(got))
	}
	age(t, filepath.Join(root, tidiedName), time.Now().Add(-2*day))
	got := Tidy(root, Keep, false)
	if want := []string{"build old"}; !slices.Equal(names(got), want) {
		t.Fatalf("removed %v, want %v", names(got), want)
	}
	if exists(old) || exists(old+".lock") || !exists(fresh) || !exists(oldRelease) {
		t.Error("the old build is there, or one that should stay is not")
	}
	// A release goes too where axx may download it again.
	age(t, filepath.Join(root, tidiedName), time.Now().Add(-2*day))
	if got := Tidy(root, Keep, true); !slices.Equal(names(got), []string{"version 0.4.2"}) || exists(oldRelease) {
		t.Errorf("removed %v, want the release", names(got))
	}
}

// A cache a CI job restored, or an image has, is as old as the day it was
// saved: what was in use then stays, though its marks are older than Keep.
func TestTidyJudgesARestoredCacheAsOfWhenItWasSaved(t *testing.T) {
	root := t.TempDir()
	inUseThen := build(t, root, "in-use-then", 100*day, "")
	used(t, inUseThen, 70*day)
	unusedThen := build(t, root, "unused-then", 200*day, "")
	used(t, unusedThen, 100*day)
	write(t, filepath.Join(root, tidiedName), "")
	age(t, filepath.Join(root, tidiedName), time.Now().Add(-60*day))

	got := Tidy(root, Keep, true)
	if want := []string{"build unused-then"}; !slices.Equal(names(got), want) {
		t.Fatalf("removed %v, want %v", names(got), want)
	}
	if !exists(inUseThen) {
		t.Error("the build that was in use when the cache was saved is gone")
	}
}

func TestTidyDoesNothingWhenToldToKeepEverything(t *testing.T) {
	root := t.TempDir()
	old := build(t, root, "old", 60*day, "")
	if got := Tidy(root, 0, true); len(got) != 0 || !exists(old) || exists(filepath.Join(root, tidiedName)) {
		t.Errorf("removed %v", names(got))
	}
}

func TestSweepRemovesWhatWasLeftBeside(t *testing.T) {
	root := t.TempDir()
	kept := build(t, root, "kept", 60*day, "")
	orphan := filepath.Join(Builds(root), "gone.lock")
	write(t, orphan, "")
	age(t, orphan, time.Now().Add(-60*day))
	recent := filepath.Join(Builds(root), "being-made.lock")
	write(t, recent, "")
	download := filepath.Join(Versions(root), ".download-1")
	write(t, filepath.Join(download, "axx.tar.gz"), "half")
	age(t, download, time.Now().Add(-60*day))

	Sweep(root, time.Now().Add(-Keep))
	if exists(orphan) || exists(download) {
		t.Error("the lock of a build that is gone, or a download that never finished, is still there")
	}
	if !exists(kept+".lock") || !exists(recent) || !exists(kept) {
		t.Error("the lock of a build that is there, or a new one, is gone")
	}
}

func TestGroupsFoldAFoldersHashIntoItsName(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "models-0a1b2c", "runtime"), "12345678")
	write(t, filepath.Join(root, "models-real", "model.onnx"), "1234")
	write(t, filepath.Join(root, "go", "path", "mod"), "12")
	write(t, filepath.Join(root, "upgrade-reminder.json"), "{}")
	build(t, root, "k", day, "")

	groups, err := Groups(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, g := range groups {
		got = append(got, g.Name)
	}
	if want := []string{"models", "builds", "go"}; !slices.Equal(got, want) {
		t.Fatalf("groups, the largest first: %v, want %v", got, want)
	}
	if m := groups[0]; m.Bytes != 12 || len(m.Paths) != 2 || m.What == "" {
		t.Errorf("models: %+v", m)
	}
	if none, err := Groups(filepath.Join(root, "not-there")); err != nil || len(none) != 0 {
		t.Errorf("a cache that is not there: %v %v", none, err)
	}
}

func TestSizeReadsAsAPersonWould(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 999: "999 B", 1500: "1.5 kB", 73_400_000: "73 MB", 1_230_000_000: "1.2 GB", 69_000_000_000: "69 GB"} {
		if got := Size(n); got != want {
			t.Errorf("Size(%d) = %q, want %q", n, got, want)
		}
	}
}
