package sql

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

// A step that registers a database says that the scenario has it, a seed
// what it wrote, and a selection what it asked and the rows that came back.
// The steps that check a selection say nothing.
func TestSeedsAndSelectionsSayTheirExchanges(t *testing.T) {
	h := sqliteDB(t)
	h.File("late.yaml", "pickups:\n  - reference: PX-LATE-1\n    postcode: \"10117\"\n  - reference: PX-LATE-2\n    postcode: \"10117\"\n")
	h.OK("a late.yaml db seed")
	h.OK(selectRows, [][]string{{"postcode", `"10117"`}, {"grams", "null"}})
	h.OK("the selection has 2 rows")
	h.OK("the 1st row reference column of the selection is 'PX-LATE-1'")
	h.OK("within 5s a 2nd selection of at least 1 row is retrieved from the pickups table on parcels-db where:", [][]string{{"reference", "PX-1"}})
	h.OK(selectRows, [][]string{{"reference", "PX-NONE"}})
	h.OK("within 1s the pickups table has no rows where:", [][]string{{"reference", "PX-NONE"}})

	got := h.SC.Exchanges()
	if len(got) != 6 {
		t.Fatalf("exchanges: %+v", got)
	}
	for i, x := range got {
		if x.Peer != "parcels-db" || x.Kind != "database" || x.From != "" || x.At.IsZero() {
			t.Errorf("exchange %d: %+v", i, x)
		}
	}
	if reg := got[0]; reg.Sent != nil || reg.Seen != nil {
		t.Errorf("the registration: %+v", reg)
	}
	if seed := got[1]; seed.Sent == nil || seed.Sent.Title != "seed pickups: 2 rows" || seed.Sent.Body != "" || seed.Seen != nil {
		t.Errorf("the seed: %+v", seed.Sent)
	}

	sel := got[2]
	if sel.Sent.Title != `select pickups where postcode = "10117" and grams = null` || sel.Seen.Title != "2 rows" || sel.Seen.MediaType != "application/json" {
		t.Errorf("the selection: %+v, %+v", sel.Sent, sel.Seen)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(sel.Seen.Body), &rows); err != nil || len(rows) != 2 {
		t.Fatalf("the selection's rows: %s: %v", sel.Seen.Body, err)
	}
	if rows[0]["reference"] != "PX-LATE-1" || rows[0]["postcode"] != "10117" || rows[0]["grams"] != nil {
		t.Errorf("the first row: %v", rows[0])
	}
	if !strings.HasPrefix(sel.Seen.Body, `[{"reference":"PX-LATE-1","postcode":"10117","grams":null,`) {
		t.Errorf("a row's columns should be in the table's order: %s", sel.Seen.Body)
	}

	poll := got[3]
	if poll.Sent.Title != "select pickups where reference = PX-1" || poll.Seen.Title != "1 row" ||
		!strings.Contains(poll.Seen.Body, `"grams":850,"kilos":12.5,`) || !strings.Contains(poll.Seen.Body, `"signed":1,"day":"2026-09-24T00:00:00Z"`) {
		t.Errorf("the selection that waits: %+v, %+v", poll.Sent, poll.Seen)
	}
	for i, none := range got[4:] {
		if none.Sent.Title != "select pickups where reference = PX-NONE" || none.Seen.Title != "0 rows" || none.Seen.Body != "" || none.Seen.MediaType != "" {
			t.Errorf("selection %d of no rows: %+v, %+v", i, none.Sent, none.Seen)
		}
	}
}

// A selection that fails for its rows said what it found; a step that never
// reached the database says nothing.
func TestAFailedSelectionSaysWhatItFound(t *testing.T) {
	h := sqliteDB(t)
	before := len(h.SC.Exchanges())
	_ = h.Fails(selectRows, `postcode is text in pickups (TEXT): write "10115"`, [][]string{{"postcode", "10115"}})
	_ = h.Fails("a missing.yaml db seed", "Could not perform seed")
	if got := h.SC.Exchanges(); len(got) != before {
		t.Errorf("steps that sent nothing said: %+v", got[before:])
	}
	_ = h.Fails("within 1s the pickups table has no rows where:", "but 1 still does", [][]string{{"reference", "PX-2"}})
	got := h.SC.Exchanges()
	if len(got) != before+1 || got[before].Seen.Title != "1 row" || !strings.Contains(got[before].Seen.Body, `"reference":"PX-2"`) {
		t.Errorf("the rows that still match: %+v", got[before:])
	}
}

// An exchange has the first rows of a selection, long texts cut, and a
// seed of many tables says how many.
func TestAnExchangeHasTheFirstRows(t *testing.T) {
	sel := &Selection{Table: "parcels.notes", Columns: []string{"id", "note", "at", "rate", "raw"}}
	at := time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC)
	for i := range 8 {
		sel.Rows = append(sel.Rows, []any{int64(i), strings.Repeat("é", 200), at, math.NaN(), float32(1.5)})
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(rowsJSON(sel)), &rows); err != nil || len(rows) != exchangeRows {
		t.Fatalf("%d rows: %v", len(rows), err)
	}
	note, _ := rows[4]["note"].(string)
	if rows[4]["id"] != 4.0 || note != strings.Repeat("é", exchangeText/2)+"…" || rows[4]["at"] != "2026-09-24T07:40:00Z" || rows[4]["rate"] != "NaN" || rows[4]["raw"] != 1.5 {
		t.Errorf("row 5: %v", rows[4])
	}

	h := sqliteDB(t)
	svc, err := Context(h.SC).Service("parcels-db")
	if err != nil {
		t.Fatal(err)
	}
	one := []DatasetRow{{Columns: []string{"id"}, Values: []any{1}}}
	svc.seeded(h.SC, &Dataset{Tables: []DatasetTable{{Name: "parcels.parcels", Rows: append(one, one...)}, {Name: "parcels.holds", Rows: one}}})
	svc.seeded(h.SC, &Dataset{Tables: []DatasetTable{{Name: "a", Rows: one}, {Name: "b", Rows: one}, {Name: "c", Rows: one}, {Name: "d", Rows: one}}})
	got := h.SC.Exchanges()
	if n := len(got); n != 3 || got[n-2].Sent.Title != "seed parcels.parcels: 2 rows, parcels.holds: 1 row" || got[n-1].Sent.Title != "seed 4 tables: 4 rows" {
		t.Errorf("seeds: %+v", got)
	}
	if c := (containment{column: "details"}).asked(&core.Table{Rows: [][]string{{"manifestId", "M-KESTREL-0412"}, {"source", "manifest"}}}); c != "details contains manifestId = M-KESTREL-0412 and source = manifest" {
		t.Errorf("a containment's conditions: %s", c)
	}
}
