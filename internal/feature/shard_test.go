package feature

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
)

// The shards of a run split its scenarios evenly, each once, the same however the files were
// loaded: every nth in their files' and lines' order, kept in the order the run has them.
func TestShard(t *testing.T) {
	a, b := &Document{URI: "features/a.feature"}, &Document{URI: "features/b.feature"}
	all := []*Pickle{{Doc: b, Line: 3}, {Doc: a, Line: 9}, {Doc: a, Line: 4}, {Doc: b, Line: 12}, {Doc: a, Line: 20}}
	at := func(ps []*Pickle) []string {
		var out []string
		for _, p := range ps {
			out = append(out, fmt.Sprintf("%s:%d", p.Doc.URI[len("features/"):len("features/")+1], p.Line))
		}
		return out
	}
	// In their files' and lines' order: a:4, a:9, a:20, b:3, b:12.
	for _, c := range []struct {
		spec string
		want []string
	}{
		{"1/2", []string{"a:4", "b:12", "a:20"}},
		{"2/2", []string{"b:3", "a:9"}},
		{"3/5", []string{"a:20"}},
		{"1/1", []string{"b:3", "a:9", "a:4", "b:12", "a:20"}},
		{"", []string{"b:3", "a:9", "a:4", "b:12", "a:20"}},
	} {
		got, err := Shard(all, c.spec)
		if err != nil {
			t.Fatalf("%q: %v", c.spec, err)
		}
		if !slices.Equal(at(got), c.want) {
			t.Errorf("--shard %q runs %v, not %v", c.spec, at(got), c.want)
		}
	}
	seen := map[*Pickle]int{}
	for _, spec := range []string{"1/3", "2/3", "3/3"} {
		got, err := Shard(all, spec)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range got {
			seen[p]++
		}
	}
	for _, p := range all {
		if seen[p] != 1 {
			t.Errorf("%s:%d runs in %d of three shards, not one", p.Doc.URI, p.Line, seen[p])
		}
	}
	for _, bad := range []string{"0/3", "4/3", "1", "a/b", "1/0", "-1/2"} {
		_, err := Shard(all, bad)
		var e *axxerr.Error
		if !errors.As(err, &e) || e.Code != CodeShard {
			t.Errorf("--shard %q: %v, not %s", bad, err, CodeShard)
		}
	}
}
