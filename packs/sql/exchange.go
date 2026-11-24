package sql

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// What the steps say they sent to a database and saw of it (core.Exchange),
// for a pack that shows the run: a seed's tables, and a selection's
// conditions with the rows that came back.
const (
	// exchangeRows is how many of a selection's rows an exchange has: as many
	// as the failure description.
	exchangeRows = 5
	// exchangeText is where a long text in a row is cut.
	exchangeText = 256
	// exchangeTables is how many of a seed's tables its title names.
	exchangeTables = 3
)

// seeded says that the dataset's rows went to the database: each table and
// how many rows it got, or how many tables and rows when there are many.
func (svc *Service) seeded(sc *core.Scenario, ds *Dataset) {
	var parts []string
	rows := 0
	for _, t := range ds.Tables {
		rows += len(t.Rows)
		parts = append(parts, t.Name+": "+plural(len(t.Rows), "row"))
	}
	title := "seed " + strings.Join(parts, ", ")
	if len(parts) > exchangeTables {
		title = fmt.Sprintf("seed %d tables: %s", len(parts), plural(rows, "row"))
	}
	sc.Exchanged(core.Exchange{Peer: svc.Name, Kind: "database", Sent: &core.Message{Title: title}})
}

// selected says a selection: what the step asked of the table, as its own
// table of conditions says it, and the rows that came back, the first of
// them as JSON.
func (svc *Service) selected(sc *core.Scenario, sel *Selection, where string) {
	seen := &core.Message{Title: plural(len(sel.Rows), "row")}
	if len(sel.Rows) > 0 {
		seen.MediaType, seen.Body = core.MimeJSON, rowsJSON(sel)
	}
	sc.Exchanged(core.Exchange{
		Peer: svc.Name, Kind: "database",
		Sent: &core.Message{Title: "select " + sel.Table + " where " + where},
		Seen: seen,
	})
}

// asked is a step's table of conditions in a line, as the step wrote
// them: "reference = PX-WEB-5302 and day = Friday".
func asked(t *core.Table) string {
	pairs, err := t.Pairs()
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, p.Key+" = "+p.Value)
	}
	return strings.Join(parts, " and ")
}

// asked is the same for a selection by what a JSONB column contains:
// "details contains manifestId = M-KESTREL-0412 and source = manifest".
func (c containment) asked(t *core.Table) string {
	return c.column + " contains " + asked(t)
}

// rowsJSON is the first rows of a selection as a JSON array of objects, each
// with the selection's columns in their order.
func rowsJSON(sel *Selection) string {
	rows := make([]*orderedObject, 0, exchangeRows)
	for _, r := range sel.Rows[:min(len(sel.Rows), exchangeRows)] {
		o := &orderedObject{}
		for i, c := range sel.Columns {
			if i < len(r) {
				o.set(c, shown(r[i]))
			}
		}
		rows = append(rows, o)
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return ""
	}
	return string(b)
}

// shown is a row's value as JSON has it: a number, a boolean and null as
// they are, and anything else (a text, a date, a time) as the text the
// messages show, cut where it is long.
func shown(v any) any {
	switch x := v.(type) {
	case nil, bool, int, int32, int64:
		return x
	case float32, float64:
		// With the digits the messages show; NaN and the infinities are no JSON numbers.
		if s := text(x); json.Valid([]byte(s)) {
			return json.RawMessage(s)
		}
	}
	s := text(v)
	if len(s) > exchangeText {
		s = strings.ToValidUTF8(s[:exchangeText], "") + "…"
	}
	return s
}

func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}
