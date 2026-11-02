package stepsearch

import (
	"strings"

	"github.com/nimbusxr/axx/core"
)

// compactRows is the most text a step's table rows take, with what each
// takes, in what agents read; a table of more (a Kafka client's Java
// properties) comes as its rows' names.
const compactRows = 2500

// BriefRows returns a table's rows as agents read them: whole, or, past
// compactRows, without what each takes and the values it takes.
func BriefRows(rows []core.TableRow) (brief []core.TableRow, whole bool) {
	n := 0
	for _, r := range rows {
		n += len(r.Name) + len(r.Takes) + len(r.Default)
		for _, v := range r.Values {
			n += len(v) + 2
		}
	}
	if n <= compactRows {
		return rows, true
	}
	brief = make([]core.TableRow, len(rows))
	for i, r := range rows {
		brief[i] = core.TableRow{Name: r.Name, Default: r.Default, Required: r.Required}
	}
	return brief, false
}

// BriefDoc is a step's documentation as an agent first reads it: what the step
// does, without its list of details. An agent looks up a dozen steps and carries
// what it reads through every later turn, and the details were half of it; the
// full form of a lookup has them.
func BriefDoc(doc string) string {
	doc = strings.TrimSpace(doc)
	if i := strings.Index(doc, "\n- "); i >= 0 {
		return strings.TrimSpace(doc[:i])
	}
	return doc
}

// BriefNote is a table's note, with the sentence on how its values read left
// out once seen has it: it is the same on every step of a kind, and comes once
// a lookup, as a parameter's documentation does.
func BriefNote(note string, seen map[string]bool) string {
	for _, kind := range []core.ValueKind{core.JSONValues, core.TextValues} {
		for _, undefined := range []bool{true, false} {
			doc := core.ValueDoc(kind, undefined)
			if !strings.HasSuffix(note, doc) {
				continue
			}
			if seen[doc] {
				return strings.TrimSpace(strings.TrimSuffix(note, doc))
			}
			seen[doc] = true
			return note
		}
	}
	return note
}
