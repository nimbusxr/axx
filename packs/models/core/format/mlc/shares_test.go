package mlc

import (
	"cmp"
	"slices"
	"testing"
)

// shares deals n shares of tasks of the seconds given: each task, the
// longest first, to the share of the least time so far. A share lists its
// tasks' indexes in order.
func shares(seconds []int, n int) [][]int {
	order := make([]int, len(seconds))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(seconds[b], seconds[a]) })
	out, total := make([][]int, n), make([]int, n)
	for _, i := range order {
		least := 0
		for s := range n {
			if total[s] < total[least] {
				least = s
			}
		}
		out[least] = append(out[least], i)
		total[least] += seconds[i]
	}
	for _, s := range out {
		slices.Sort(s)
	}
	return out
}

// Every task is in one share, and the shares are of about the same time.
func TestShares(t *testing.T) {
	got := shares([]int{3, 5, 3, 4, 3}, 2)
	if want := [][]int{{1, 2}, {0, 3, 4}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("shares %v, not %v", got, want)
	}
	if got := shares([]int{1}, 3); len(got) != 3 || !slices.Equal(got[0], []int{0}) || got[1] != nil || got[2] != nil {
		t.Errorf("one task in three shares: %v", got)
	}
}
