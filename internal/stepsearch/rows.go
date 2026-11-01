package stepsearch

import "github.com/nimbusxr/axx/core"

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
