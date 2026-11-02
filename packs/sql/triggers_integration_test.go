//go:build integration

package sql

import (
	dbsql "database/sql"
	"fmt"
	"strings"
	"testing"
)

// updateAndDeleteTriggers holds before update and before delete triggers to what they
// say, on a table of reference, status and kilos, with rows PX-TRG-1 to PX-TRG-9 stored
// as REGISTERED, 1.5. code is the error the triggers raise, as the step writes it.
func updateAndDeleteTriggers(t *testing.T, h *harness, db *dbsql.DB, table, code string) {
	t.Helper()
	update := func(reference, status string) error {
		_, err := db.Exec("UPDATE " + table + " SET status = '" + status + "' WHERE reference = '" + reference + "'")
		return err
	}
	remove := func(reference string) error {
		_, err := db.Exec("DELETE FROM " + table + " WHERE reference = '" + reference + "'")
		return err
	}
	insert := func(reference string) error {
		_, err := db.Exec("INSERT INTO " + table + " (reference, status, kilos) VALUES ('" + reference + "', 'REGISTERED', 1.5)")
		return err
	}
	status := func(reference string) string {
		t.Helper()
		var s string
		if err := db.QueryRow("SELECT status FROM " + table + " WHERE reference = '" + reference + "'").Scan(&s); err != nil {
			t.Fatalf("%s: %v", reference, err)
		}
		return s
	}
	refused := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), strings.TrimPrefix(code, "ORA-")) {
			t.Errorf("%s: %v, want %s", what, err, code)
		}
	}
	allowed := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Errorf("%s: %v", what, err)
		}
	}
	step := func(text string, rows ...[]string) {
		t.Helper()
		if err := h.step(text, rows...); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
	on := " trigger on the " + table + " table"
	drop := func() {
		t.Helper()
		if err := mustService(t, h).dropTriggers(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	// An update's conditions are on the row as it would leave it.
	step("a before update"+on+" will raise a "+code+" exception where:", []string{"reference", "PX-TRG-1"}, []string{"status", "DELIVERED"})
	refused("the update it names", update("PX-TRG-1", "DELIVERED"))
	if got := status("PX-TRG-1"); got != "REGISTERED" {
		t.Errorf("the refused update left %s", got)
	}
	allowed("to another status", update("PX-TRG-1", "PICKED_UP"))
	allowed("of another row", update("PX-TRG-2", "DELIVERED"))
	step("the before update" + on + " was raised 1 time")
	if err := h.step("the before update" + on + " was raised 2 times"); err == nil || !strings.Contains(err.Error(), "before update trigger") {
		t.Errorf("a wrong count: %v", err)
	}
	drop()

	// A column the update does not change has the value it had, and the value it had
	// is not what a changed one is held to.
	step("a before update"+on+" will raise a "+code+" exception where:", []string{"kilos", "1.5"})
	refused("a column it does not change", update("PX-TRG-3", "PICKED_UP"))
	drop()
	step("a before update"+on+" will raise a "+code+" exception where:", []string{"reference", "PX-TRG-4"}, []string{"status", "REGISTERED"})
	allowed("the status the update takes away", update("PX-TRG-4", "PICKED_UP"))
	drop()

	// Only that many.
	step("a before update"+on+" will raise a "+code+" exception 1 time where:", []string{"reference", "PX-TRG-5"})
	refused("the first", update("PX-TRG-5", "PICKED_UP"))
	allowed("the second", update("PX-TRG-5", "PICKED_UP"))
	step("the before update" + on + " was raised 1 time")
	drop()

	// A delete's are on the row it would take away, which stays.
	step("a before delete"+on+" will raise a "+code+" exception where:", []string{"reference", "PX-TRG-6"}, []string{"status", "REGISTERED"})
	refused("the delete it names", remove("PX-TRG-6"))
	if got := status("PX-TRG-6"); got != "REGISTERED" {
		t.Errorf("the refused delete left %s", got)
	}
	allowed("of another row", remove("PX-TRG-7"))
	step("the before delete" + on + " was raised 1 time")
	if err := h.step("the before delete" + on + " was raised 2 times"); err == nil || !strings.Contains(err.Error(), "before delete trigger") {
		t.Errorf("a wrong count: %v", err)
	}
	drop()

	step("a before delete"+on+" will raise a "+code+" exception 1 time where:", []string{"reference", "PX-TRG-8"})
	refused("the first", remove("PX-TRG-8"))
	allowed("the second", remove("PX-TRG-8"))
	step("the before delete" + on + " was raised 1 time")
	drop()

	// Each goes before its own write, and each kind is numbered on its own.
	step("a before insert"+on+" will raise a "+code+" exception where:", []string{"reference", "PX-TRG-KIND"})
	step("a before update"+on+" will raise a "+code+" exception where:", []string{"reference", "PX-TRG-9"})
	step("a before delete"+on+" will raise a "+code+" exception where:", []string{"reference", "PX-TRG-9"})
	refused("an insert", insert("PX-TRG-KIND"))
	refused("an update", update("PX-TRG-9", "PICKED_UP"))
	refused("a delete", remove("PX-TRG-9"))
	refused("a second delete", remove("PX-TRG-9"))
	step("the before insert" + on + " was raised 1 time")
	step("the before update" + on + " was raised 1 time")
	step("the before delete" + on + " was raised 2 times")
	if err := h.step("the 2nd ordered before update" + on + " was raised 1 time"); err == nil || !strings.Contains(err.Error(), "No before update trigger found") {
		t.Errorf("a trigger of that kind there is not: %v", err)
	}
	drop()

	// Its conditions are held to their columns, as an insert's are.
	for _, kind := range []string{"update", "delete"} {
		err := h.step("a before "+kind+on+" will raise a "+code+" exception where:", []string{"kilos", "2"})
		if err == nil || !strings.Contains(err.Error(), "write 2.0") {
			t.Errorf("a before %s trigger on 2: %v, want write 2.0", kind, err)
		}
		err = h.step("a before "+kind+on+" will raise a "+code+" exception where:", []string{"no_such_column", "1"})
		if err == nil || !strings.Contains(err.Error(), "before "+kind+" trigger") {
			t.Errorf("a before %s trigger on no column: %v", kind, err)
		}
	}
	if got := len(mustService(t, h).Triggers()); got != 0 {
		t.Errorf("%d triggers were created", got)
	}

	// Dropped, they refuse nothing.
	allowed("an update after them", update("PX-TRG-9", "DELIVERED"))
	allowed("a delete after them", remove("PX-TRG-9"))
}

// triggerRows are the rows updateAndDeleteTriggers works on.
func triggerRows(table string) []string {
	rows := make([]string, 0, 9)
	for i := 1; i <= 9; i++ {
		rows = append(rows, fmt.Sprintf("INSERT INTO %s (reference, status, kilos) VALUES ('PX-TRG-%d', 'REGISTERED', 1.5)", table, i))
	}
	return rows
}

func TestUpdateAndDeleteTriggersPostgres(t *testing.T) {
	url := postgresURL(t)
	tgt, err := Resolve(url, "space", "secret")
	if err != nil {
		t.Fatal(err)
	}
	const table = "space.trigger_parcels"
	exec(t, "pgx", tgt.DSN, "CREATE TABLE "+table+" (reference text PRIMARY KEY, status text, kilos numeric(10,2))")
	exec(t, "pgx", tgt.DSN, triggerRows(table)...)
	db, err := dbsql.Open("pgx", tgt.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := newHarness(t)
	h.db(url)
	updateAndDeleteTriggers(t, h, db, table, "40001")

	var left int
	if err := db.QueryRow("SELECT count(*) FROM pg_trigger WHERE tgname LIKE 'axx_trigger_%'").Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d triggers left", left)
	}
}

// Only PostgreSQL and Oracle have them, and the others say so.
func TestUpdateAndDeleteTriggersOnlyWhereThereAreSome(t *testing.T) {
	path := t.TempDir() + "/parcels.db"
	exec(t, "sqlite", path, "CREATE TABLE parcels (reference TEXT PRIMARY KEY)")
	h := newHarness(t)
	h.db("sqlite:" + path)
	for _, kind := range []string{"update", "delete"} {
		err := h.step("a before "+kind+" trigger on the parcels table will raise a 40001 exception where:", []string{"reference", "PX-1"})
		if err == nil || !strings.Contains(err.Error(), "Only PostgreSQL and Oracle") {
			t.Errorf("a before %s trigger on SQLite: %v", kind, err)
		}
	}
}
