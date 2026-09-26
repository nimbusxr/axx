package webcore

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
)

// pageTable is a table as the page shows it: its column headers and rows.
type pageTable struct {
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// tablesJS reads the page's visible tables. The headers are the cells of
// the last header row (in <thead>, or a first row of <th> cells).
const tablesJS = `() => [...document.querySelectorAll('table')]
  .filter((t) => t.offsetWidth || t.offsetHeight || t.getClientRects().length)
  .map((t) => {
    const rows = [...t.rows];
    let header = null;
    if (t.tHead && t.tHead.rows.length) header = t.tHead.rows[t.tHead.rows.length - 1];
    else if (rows.length && [...rows[0].cells].every((c) => c.tagName === 'TH')) header = rows[0];
    const cells = (r) => [...r.cells].map((c) => c.innerText.replace(/\s+/g, ' ').trim());
    return {
      headers: header ? cells(header) : [],
      rows: rows.filter((r) => r !== header && !(t.tHead && t.tHead.contains(r))).map(cells),
    };
  })`

// tables are the visible tables of every frame of the page.
func tables(pg playwright.Page) []pageTable {
	var out []pageTable
	for _, f := range pg.Frames() {
		v, err := f.Evaluate(tablesJS)
		if err != nil {
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		var ts []pageTable
		if json.Unmarshal(b, &ts) == nil {
			out = append(out, ts...)
		}
	}
	return out
}

// matchRow reports whether a row of one of the tables has, in each column
// named in want, the value want gives (compared as the page shows the text,
// spaces collapsed). When none does, it explains what the tables have.
func matchRow(ts []pageTable, want []core.Pair) (bool, string) {
	n, why := countRows(ts, want)
	return n > 0, why
}

// countRows counts the rows of the tables that have want's values in its
// columns, and explains what the tables have.
func countRows(ts []pageTable, want []core.Pair) (int, string) {
	columns := make([]string, len(want))
	for i, p := range want {
		columns[i] = p.Key
	}
	var seen []string
	withColumns, matches := 0, 0
	for _, t := range ts {
		idx := make([]int, len(want))
		ok := true
		for i, p := range want {
			idx[i] = slices.Index(t.Headers, p.Key)
			if idx[i] < 0 {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		withColumns++
		for _, r := range t.Rows {
			match := true
			parts := make([]string, len(want))
			for i, p := range want {
				cell := ""
				if idx[i] < len(r) {
					cell = r[idx[i]]
				}
				parts[i] = fmt.Sprintf("%s=%s", p.Key, cell)
				if cell != collapse(p.Value) {
					match = false
				}
			}
			if match {
				matches++
			}
			seen = append(seen, strings.Join(parts, ", "))
		}
	}
	if withColumns == 0 {
		var hs []string
		for _, t := range ts {
			hs = append(hs, fmt.Sprintf("%q", strings.Join(t.Headers, " | ")))
		}
		if len(hs) == 0 {
			return 0, "the page shows no table"
		}
		return 0, fmt.Sprintf("no table on the page has the columns %s; the tables have: %s",
			strings.Join(columns, ", "), strings.Join(hs, "; "))
	}
	if len(seen) == 0 {
		return 0, "the tables with those columns have no rows"
	}
	if len(seen) > 10 {
		seen = append(seen[:10], fmt.Sprintf("… and %d more", len(seen)-10))
	}
	return matches, "the rows are:\n  " + strings.Join(seen, "\n  ")
}
