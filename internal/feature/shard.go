package feature

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Shard returns the share of the pickles that shard i of n runs ("i/n", from 1): every nth of
// them, in the order of their files and lines, from the ith, so that the shards of a run split
// its scenarios evenly, the same way on every machine, and together run each one once. The
// pickles keep their order. An empty spec is all of them.
func Shard(pickles []*Pickle, spec string) ([]*Pickle, error) {
	if spec == "" {
		return pickles, nil
	}
	i, n, err := parseShard(spec)
	if err != nil {
		return nil, err
	}
	ordered := slices.Clone(pickles)
	slices.SortStableFunc(ordered, func(a, b *Pickle) int {
		return cmp.Or(strings.Compare(a.Doc.URI, b.Doc.URI), cmp.Compare(a.Line, b.Line))
	})
	in := map[*Pickle]bool{}
	for k, p := range ordered {
		if k%n == i-1 {
			in[p] = true
		}
	}
	var out []*Pickle
	for _, p := range pickles {
		if in[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

// parseShard reads "i/n": shard i of n, 1 <= i <= n.
func parseShard(spec string) (int, int, error) {
	a, b, ok := strings.Cut(spec, "/")
	i, errI := strconv.Atoi(strings.TrimSpace(a))
	n, errN := strconv.Atoi(strings.TrimSpace(b))
	if !ok || errI != nil || errN != nil || n < 1 || i < 1 || i > n {
		return 0, 0, axxerr.New(CodeShard, exitcode.Usage, "invalid --shard %q", spec).
			WithHint("give the shard and how many there are, like 1/3: shard 1 of 3, from 1 to 3")
	}
	return i, n, nil
}
