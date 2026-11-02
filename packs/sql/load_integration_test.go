//go:build integration

package sql

import (
	"context"
	dbsql "database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// databaseZone is the zone these tests' databases run in, so that a clock that is not
// the database's shows on any machine: no machine running them is half an hour off UTC
// without someone noticing, and CI's are in UTC.
const databaseZone = "Asia/Kolkata"

// assertDatabaseZone fails a test that would prove nothing: q is the database's offset
// in minutes.
func assertDatabaseZone(t *testing.T, db *dbsql.DB, q string) {
	t.Helper()
	var minutes int
	if err := db.QueryRow(q).Scan(&minutes); err != nil {
		t.Fatal(err)
	}
	if minutes != 330 {
		t.Fatalf("the database is %d minutes from UTC, not in %s", minutes, databaseZone)
	}
}

// relativeTimes seeds a row whose columns are all two hours ago, and asks the database,
// by its own clock, how many minutes ago each is: age is a query of one row with one
// number for each column. [UNIX_TIMESTAMP] goes in the table's epoch column.
func relativeTimes(t *testing.T, h *harness, db *dbsql.DB, table string, columns []string, age string) {
	t.Helper()
	var seed strings.Builder
	seed.WriteString(table + ":\n  - reference: PX-CLOCK\n    epoch: \"[UNIX_TIMESTAMP]\"\n")
	for _, c := range columns {
		seed.WriteString("    " + c + ": \"[HOUR,MINUS,2]\"\n")
	}
	h.file("clock.yaml", seed.String())
	if err := h.step("a clock.yaml db seed"); err != nil {
		t.Fatal(err)
	}
	ages := make([]int, len(columns))
	dest := make([]any, len(columns))
	for i := range ages {
		dest[i] = &ages[i]
	}
	if err := db.QueryRow(age).Scan(dest...); err != nil {
		t.Fatal(err)
	}
	for i, c := range columns {
		if ages[i] < 119 || ages[i] > 121 {
			t.Errorf("%s is %d minutes ago by the database's clock, want 120", c, ages[i])
		}
	}
	var epoch string
	if err := db.QueryRow("SELECT epoch FROM " + table + " WHERE reference = 'PX-CLOCK'").Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	seconds, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		t.Fatalf("epoch: %v", err)
	}
	if off := time.Now().Unix() - seconds; off < -60 || off > 60 {
		t.Errorf("[UNIX_TIMESTAMP] is %d seconds off", off)
	}
}

// manyAtOnce runs twice as many seeds, selections and checks at once as the pool has
// connections: a step that held a connection and waited for another would wait forever
// (the context's deadline here), and a connection closed for being over the idle limit
// is what makes Oracle's listener refuse them (ORA-12516). table has a reference, kilos
// and at_time, and its seeds name idColumn too when there is one.
func manyAtOnce(t *testing.T, h *harness, table, idColumn string) {
	t.Helper()
	svc := mustService(t, h)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	before := svc.DB.Stats()
	const scenarios = maxConnections * 2
	errs := make([]error, scenarios)
	var wg sync.WaitGroup
	for i := range scenarios {
		id := ""
		if idColumn != "" {
			id = fmt.Sprintf("    %s: %d\n", idColumn, 9000+i)
		}
		h.file(fmt.Sprintf("load/%d.yaml", i), fmt.Sprintf("%s:\n  - reference: PX-LOAD-%d\n%s    kilos: 1.5\n    at_time: \"[MIN,MINUS,5]\"\n", table, i, id))
		ds, err := LoadDataset(filepath.Join(h.dir, "load", strconv.Itoa(i)+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			now, err := svc.databaseNow(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			if err := svc.insert(ctx, ds, now); err != nil {
				errs[i] = err
				return
			}
			sel, err := svc.query(ctx, table, fmt.Sprintf("SELECT * FROM %s WHERE reference = 'PX-LOAD-%d'", table, i))
			if err != nil {
				errs[i] = err
				return
			}
			if len(sel.Rows) != 1 {
				errs[i] = fmt.Errorf("%d rows", len(sel.Rows))
			}
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("scenario %d: %v", i, err)
		}
	}
	stats := svc.DB.Stats()
	if closed := stats.MaxIdleClosed - before.MaxIdleClosed; closed != 0 {
		t.Errorf("%d connections were closed for being over the idle limit", closed)
	}
	if stats.MaxOpenConnections != maxConnections {
		t.Errorf("the pool opens %d connections, want %d", stats.MaxOpenConnections, maxConnections)
	}
}

// oneConnectionAtATime runs the steps on a pool of one connection, so that a step that
// holds one and asks for another waits forever at once, and not only when a run has as
// many scenarios at once as the pool has connections, which how fast a database is
// decides. row is the rest of a seed's row of the table, naming an identity column
// where it has one; triggers says the database has them.
func oneConnectionAtATime(t *testing.T, h *harness, table, row string, triggers bool) {
	t.Helper()
	svc := mustService(t, h)
	svc.DB.SetMaxOpenConns(1)
	defer func() { // lowering the first lowered the second
		svc.DB.SetMaxOpenConns(maxConnections)
		svc.DB.SetMaxIdleConns(maxConnections)
	}()
	h.file("one.yaml", table+":\n  - reference: PX-ONE\n"+row)
	done := make(chan error, 1)
	go func() {
		done <- func() error {
			if err := h.step("a one.yaml db seed"); err != nil {
				return fmt.Errorf("a seed: %w", err)
			}
			if err := h.step("a one.yaml db seed"); err == nil {
				return fmt.Errorf("the same row twice was stored")
			}
			// A row the database refuses for a column it has not got: the hint asks it
			// which it has.
			h.file("no-column.yaml", table+":\n  - no_such_column: PX-ONE\n")
			if err := h.step("a no-column.yaml db seed"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reference") {
				return fmt.Errorf("a column it has not got: %w", err)
			}
			sel := "a selection of rows is retrieved from the " + table + " table where:"
			if err := h.step(sel, []string{"reference", "PX-ONE"}, []string{"kilos", "1.5"}); err != nil {
				return fmt.Errorf("a selection: %w", err)
			}
			n := nth(len(svc.Selections()))
			if err := h.step("the 1st row kilos column of the " + n + " selection is '1.5'"); err != nil {
				return fmt.Errorf("a column check: %w", err)
			}
			if err := h.step("within 5s a selection of at least 1 row is retrieved from the "+table+" table where:", []string{"reference", "PX-ONE"}); err != nil {
				return fmt.Errorf("a poll: %w", err)
			}
			if !triggers {
				return nil
			}
			code := "23505"
			if svc.Target.Dialect.Oracle {
				code = "20001"
			}
			if err := h.step("a before insert trigger on the "+table+" table will raise a "+code+" exception where:", []string{"reference", "PX-NEVER"}); err != nil {
				return fmt.Errorf("a trigger: %w", err)
			}
			return svc.dropTriggers(t.Context())
		}()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("a step holds a connection and waits for another")
	}
}

// A database that cannot be reached is the step's failure: its types are not nothing to
// hold a value to, as they would be for a table it has not got.
func unreachable(t *testing.T, h *harness, table string) {
	t.Helper()
	const lookup = "could not read the table's column types"
	sel := "a selection of rows is retrieved from the " + table + " table where:"
	if err := h.step(sel, []string{"reference", "PX-TYPES-1"}); err != nil {
		t.Fatal(err)
	}
	n := nth(len(mustService(t, h).Selections()))
	svc := mustService(t, h)
	if err := svc.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.step(sel, []string{"reference", "PX-TYPES-1"}); err == nil || !strings.Contains(err.Error(), lookup) {
		t.Errorf("a selection: %v, want %s", err, lookup)
	}
	// A poll waits it out, as a connection may come back.
	start := time.Now()
	err := h.step("within 1s a selection of at least 1 row is retrieved from the "+table+" table where:", []string{"reference", "PX-TYPES-1"})
	if err == nil || !strings.Contains(err.Error(), lookup) {
		t.Errorf("a poll: %v, want %s", err, lookup)
	}
	if took := time.Since(start); took < 400*time.Millisecond { // it asks every 500 ms
		t.Errorf("a poll gave up after %s", took)
	}
	// What was read stays readable: its checks ask nothing of the database.
	if err := h.step("the 1st row reference column of the " + n + " selection is 'PX-TYPES-1'"); err != nil {
		t.Errorf("a column check of what was read: %v", err)
	}
	h.file("unreachable.yaml", table+":\n  - reference: PX-GONE\n")
	if err := h.step("a unreachable.yaml db seed"); err == nil || !strings.Contains(err.Error(), "clock") {
		t.Errorf("a seed: %v, want that it could not read the clock", err)
	}
}

// A table a database has not got still fails in that database's words, in a step that
// holds nothing to it.
func TestAMissingTableIsTheDatabasesToReport(t *testing.T) {
	h := newHarness(t)
	h.db(postgresURL(t))
	err := h.step("a selection of rows is retrieved from the space.no_such_table table where:", []string{"id", "1"})
	if err == nil || strings.Contains(err.Error(), "column types") || !strings.Contains(err.Error(), "no_such_table") {
		t.Errorf("a table that is not there: %v", err)
	}
}

func TestPostgresClockAndLoad(t *testing.T) {
	url := postgresURL(t)
	separator := "?"
	if strings.Contains(url, "?") {
		separator = "&"
	}
	url += separator + "timezone=" + databaseZone
	tgt, err := Resolve(url, "space", "secret")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "pgx", tgt.DSN,
		`CREATE TABLE clock_times (reference text PRIMARY KEY, day_at timestamp, zoned_at timestamptz, epoch text)`,
		`CREATE TABLE load_parcels (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, reference text UNIQUE, kilos numeric(10,2), at_time timestamp)`,
	)
	db, err := dbsql.Open("pgx", tgt.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertDatabaseZone(t, db, "SELECT (EXTRACT(TIMEZONE FROM now()) / 60)::int")
	h := newHarness(t)
	h.db(url)
	relativeTimes(t, h, db, "clock_times", []string{"day_at", "zoned_at"},
		`SELECT round(extract(epoch FROM (now()::timestamp - day_at)) / 60)::int, round(extract(epoch FROM (now() - zoned_at)) / 60)::int
			FROM clock_times WHERE reference = 'PX-CLOCK'`)
	// Each names the identity column, whose lookup takes place inside the transaction.
	oneConnectionAtATime(t, h, "load_parcels", "    id: 8000\n    kilos: 1.5\n    at_time: \"[MIN,MINUS,5]\"\n", true)
	manyAtOnce(t, h, "load_parcels", "id")

	exec(t, "pgx", tgt.DSN, `CREATE TABLE gone (reference text PRIMARY KEY)`, `INSERT INTO gone VALUES ('PX-TYPES-1')`)
	gone := newHarness(t)
	gone.db(url)
	unreachable(t, gone, "gone")
}
