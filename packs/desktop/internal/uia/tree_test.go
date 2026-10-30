package uia

import (
	"slices"
	"testing"
)

// A process's descendants end, whatever cycle numbers that Windows reused
// make, and leave out an older process that only looks like a child.
func TestDescendants(t *testing.T) {
	kids := map[int][]int{
		10: {11, 12},
		11: {13},
		13: {10, 14}, // 10 reused: it looks like 13's child
		12: {15},
	}
	born := map[int]int64{10: 100, 11: 110, 12: 120, 13: 130, 14: 140, 15: 50}
	at := func(p int) (int64, bool) { b, ok := born[p]; return b, ok }
	got := descendants(kids, 10, 0, at)
	slices.Sort(got)
	if want := []int{11, 12, 13, 14}; !slices.Equal(got, want) {
		t.Errorf("descendants of 10: %v, want %v (15 started before 12)", got, want)
	}
	if got := descendants(map[int][]int{1: {2}, 2: {1}}, 1, 0, func(int) (int64, bool) { return 100, true }); !slices.Equal(got, []int{2}) {
		t.Errorf("a cycle ends: %v", got)
	}
}

// An app that has ended keeps its start, as its launch knew it: a process that
// started before it, whose parent's number it got (a CI runner's, started days
// ago by a process long gone), is no child of it, and neither is a process
// whose start is not known.
func TestDescendantsOfAnAppThatEnded(t *testing.T) {
	kids := map[int][]int{10: {11, 20, 21}, 20: {22}}
	born := map[int]int64{11: 110, 20: 50, 22: 60} // 10 has ended; 21's start is not known
	at := func(p int) (int64, bool) { b, ok := born[p]; return b, ok }
	if got := descendants(kids, 10, 100, at); !slices.Equal(got, []int{11}) {
		t.Errorf("descendants of 10, which started at 100 and has ended: %v, want [11]", got)
	}
	if got := descendants(kids, 10, 0, at); got != nil {
		t.Errorf("descendants of 10, whose start is not known: %v, want none", got)
	}
}
