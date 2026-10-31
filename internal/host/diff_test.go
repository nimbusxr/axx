package host

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDiff(t *testing.T) {
	var a []string
	for i := 1; i <= 20; i++ {
		a = append(a, "line "+string(rune('a'+i-1)))
	}
	b := append([]string(nil), a...)
	b[1] = "changed b"
	b = append(b[:15], append([]string{"inserted"}, b[15:]...)...)
	got := Diff(strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n")
	want := `--- approved
+++ proposed
@@ -1,5 +1,5 @@
 line a
-line b
+changed b
 line c
 line d
 line e
@@ -13,6 +13,7 @@
 line m
 line n
 line o
+inserted
 line p
 line q
 line r
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := Diff("", "services:\n  api: {}\n"); got != "--- approved\n+++ proposed\n@@ -1,0 +1,2 @@\n+services:\n+  api: {}\n" {
		t.Errorf("from nothing:\n%s", got)
	}
	if got := Diff("same\n", "same\n"); got != "--- approved\n+++ proposed\n" {
		t.Errorf("no change:\n%s", got)
	}
}

func TestApprovals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host", "approvals.json")
	a := loadApprovals(path, "/work/shop")
	if a.has("one") {
		t.Fatal("nothing is approved yet")
	}
	for _, step := range []struct{ key, text string }{{"axx.yaml", "one"}, {"axx.yaml, profile ci", "ci"}, {"axx.yaml", "two"}} {
		if err := a.add(step.key, step.text); err != nil {
			t.Fatal(err)
		}
	}
	b := loadApprovals(path, "/work/shop")
	if !b.has("one") || !b.has("two") || !b.has("ci") {
		t.Errorf("kept: %+v", b.list)
	}
	if last, ok := b.last("axx.yaml"); !ok || last != "two" {
		t.Errorf("last of axx.yaml: %q %v", last, ok)
	}
	if _, ok := b.last("axx.yaml, profile local"); ok {
		t.Error("a key never approved has no last")
	}
	for i := range 2 * keptApprovals {
		_ = b.add("axx.yaml", strings.Repeat("x", i+1))
	}
	if len(b.list) != keptApprovals || b.has("one") {
		t.Errorf("keeps the last %d: %d kept", keptApprovals, len(b.list))
	}
}
