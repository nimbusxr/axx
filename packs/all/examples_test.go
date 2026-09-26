package all_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/packs/all"
)

// TestStepExamples checks every step of every pack: it is documented, it has
// examples that start with its keyword, and each example matches that step
// and no other, across all the packs (an ambiguous step fails at run time).
// The docs and the skills show the examples, so they must be real.
func TestStepExamples(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	keywords := []string{"Given ", "When ", "Then ", "And ", "But "}
	for _, d := range e.Registry.Defs() {
		s := d.Step
		if strings.TrimSpace(s.Doc) == "" {
			t.Errorf("%s (%s): no Doc", s.ID, d.Pack)
		}
		if len(s.Examples) == 0 {
			t.Errorf("%s (%s): no Examples", s.ID, d.Pack)
		}
		if s.Arg == core.ArgTable && (s.Table == nil || len(s.Table.Columns) == 0) {
			t.Errorf("%s: takes a table, and says nothing of its columns (Table)", s.ID)
		}
		tableRows := 0
		for _, ex := range s.Examples {
			line, rest, _ := strings.Cut(ex, "\n")
			if s.Keyword != "" && !strings.HasPrefix(line, s.Keyword+" ") {
				t.Errorf("%s: example %q should start with %s", s.ID, line, s.Keyword)
			}
			for _, row := range strings.Split(rest, "\n") {
				row = strings.TrimSpace(row)
				if !strings.HasPrefix(row, "|") {
					continue
				}
				tableRows++
				if s.Table != nil && len(s.Table.Rows) > 0 && !knownRow(s.Table.Rows, strings.TrimSpace(strings.Split(row, "|")[1])) {
					t.Errorf("%s: example row %q is none of the rows its table knows", s.ID, row)
				}
			}
			text := line
			for _, kw := range keywords {
				text = strings.TrimPrefix(text, kw)
			}
			ms := e.Registry.Match(text)
			switch {
			case len(ms) != 1:
				t.Errorf("%s: example %q matches %d steps", s.ID, ex, len(ms))
			case ms[0].Def().Step.ID != s.ID:
				t.Errorf("%s: example %q matches %s", s.ID, ex, ms[0].Def().Step.ID)
			}
		}
		if s.Arg == core.ArgTable && tableRows == 0 {
			t.Errorf("%s: takes a table, and no example shows one", s.ID)
		}
	}
	for _, p := range e.Registry.Params() {
		if p.Pack != "cucumber" && len(p.Type.Examples) == 0 {
			t.Errorf("{%s} (%s): no Examples", p.Type.Name, p.Pack)
		}
	}
}

// knownRow reports whether a table knows a row: by its name, or by a name
// with a placeholder, like "header.<name>" or "<client>.acks", that stands
// for any text there.
func knownRow(rows []core.TableRow, name string) bool {
	for _, r := range rows {
		if r.Name == name || placeholders(r.Name).MatchString(name) {
			return true
		}
	}
	return false
}

var placeholder = regexp.MustCompile(`<[^<>]+>`)

func placeholders(row string) *regexp.Regexp {
	parts := placeholder.Split(row, -1)
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".+") + "$")
}
