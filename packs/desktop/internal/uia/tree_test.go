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
	got := descendants(kids, 10, func(p int) (int64, bool) { b, ok := born[p]; return b, ok })
	slices.Sort(got)
	if want := []int{11, 12, 13, 14}; !slices.Equal(got, want) {
		t.Errorf("descendants of 10: %v, want %v (15 started before 12)", got, want)
	}
	if got := descendants(map[int][]int{1: {2}, 2: {1}}, 1, func(int) (int64, bool) { return 0, false }); !slices.Equal(got, []int{2}) {
		t.Errorf("a cycle ends: %v", got)
	}
}
