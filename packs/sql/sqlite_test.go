package sql

import (
	"context"
	dbsql "database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// sqliteService is the connection of the service under test to the harness's
// database (sqliteDB): one of its own, as another program's is, that waits for
// the file while a step has it, as a program that shares an SQLite file must.
func sqliteService(t *testing.T, h *cloudtest.Harness) *dbsql.DB {
	t.Helper()
	svc, err := Context(h.SC).Service("parcels-db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := dbsql.Open("sqlite", svc.Target.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// An SQLite URL's connections wait for a file another connection has locked,
// unless the URL says how long they wait.
func TestAnSQLiteURLWaitsForTheFile(t *testing.T) {
	const waits = "_pragma=busy_timeout(10000)"
	for _, c := range []struct{ url, dsn string }{
		{"sqlite:/data/parcels.db", "/data/parcels.db?" + waits},
		{"jdbc:sqlite:/data/parcels.db", "/data/parcels.db?" + waits},
		{`sqlite:C:\data\parcels.db`, `C:\data\parcels.db?` + waits},
		{"sqlite::memory:", ":memory:?" + waits},
		{"file:/data/parcels.db?mode=ro", "file:/data/parcels.db?mode=ro&" + waits},
		{"sqlite:/data/parcels.db?_pragma=foreign_keys(1)", "/data/parcels.db?_pragma=foreign_keys(1)&" + waits},
		{"sqlite:/data/parcels.db?_pragma=busy_timeout(250)", "/data/parcels.db?_pragma=busy_timeout(250)"},
		{"sqlite:/data/parcels.db?_pragma=Busy_Timeout%3D250", "/data/parcels.db?_pragma=Busy_Timeout%3D250"},
		{"sqlite:/data/parcels.db?_busy_timeout=250", "/data/parcels.db?_busy_timeout=250"},
		{"sqlite:/data/parcels.db?_timeout=250", "/data/parcels.db?_timeout=250"},
		{"sqlite:", ""},
	} {
		tgt, err := Resolve(c.url, "u", "p")
		if err != nil {
			t.Errorf("%s: %v", c.url, err)
			continue
		}
		if tgt.DSN != c.dsn {
			t.Errorf("%s: %q, want %q", c.url, tgt.DSN, c.dsn)
		}
	}

	// What the driver makes of them: the wait of each connection, in milliseconds.
	for tail, want := range map[string]int{"": 10000, "?_pragma=busy_timeout(250)": 250, "?_pragma=foreign_keys(1)": 10000} {
		h := cloudtest.New(t, Pack())
		h.OK("a parcels-db database with the following properties:",
			[][]string{{"url", "sqlite:" + filepath.Join(t.TempDir(), "parcels.db") + tail}, {"user", "u"}, {"password", "p"}})
		svc, err := Context(h.SC).Service("parcels-db")
		if err != nil {
			t.Fatal(err)
		}
		var got int
		if err := svc.DB.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("a URL ending in %q: its connections wait %d ms, want %d", tail, got, want)
		}
	}
}

// A step that finds the file locked waits for it. SQLite locks the whole file,
// and with no wait a step fails at once ("database is locked") whenever the
// service under test is in the middle of a write.
func TestAStepWaitsForAnSQLiteFileItsServiceHasLocked(t *testing.T) {
	h := sqliteDB(t)
	service := sqliteService(t, h)
	h.File("late.yaml", "pickups:\n  - reference: PX-LATE\n    postcode: \"10117\"\n")
	ctx := context.Background()
	for _, c := range []struct {
		begin string
		step  func()
	}{
		// A write transaction keeps every other writer out until it ends.
		{"BEGIN IMMEDIATE", func() { h.OK("a late.yaml db seed") }},
		// One that asks for the file to itself keeps readers out too.
		{"BEGIN EXCLUSIVE", func() { h.OK(selectRows, [][]string{{"postcode", `"10115"`}}) }},
	} {
		conn, err := service.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range []string{c.begin, "INSERT INTO pickups (reference) VALUES ('" + c.begin + "')"} {
			if _, err := conn.ExecContext(ctx, q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		// The file is locked before the step starts, so with no wait the step fails
		// however long the service takes. With it, the step ends once the service has
		// committed, whenever that is.
		committed := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, err := conn.ExecContext(ctx, "COMMIT")
			committed <- err
		}()
		c.step()
		if err := <-committed; err != nil {
			t.Errorf("%s: the service's commit: %v", c.begin, err)
		}
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}
}

// Scenarios that run at once share the file too: each one's steps wait for the
// others', where with no wait most of them failed ("database is locked").
func TestScenariosAtOnceShareAnSQLiteFile(t *testing.T) {
	h := sqliteDB(t)
	svc, err := Context(h.SC).Service("parcels-db")
	if err != nil {
		t.Fatal(err)
	}
	database := [][]string{{"url", "sqlite:" + svc.Target.DSN}, {"user", "u"}, {"password", "p"}}
	const scenarios = 8
	errs := make(chan error, scenarios)
	for i := range scenarios {
		reference := fmt.Sprintf("PX-AT-ONCE-%d", i)
		h.File(reference+".yaml", "pickups:\n  - reference: "+reference+"\n    postcode: \"10117\"\n")
		in := h.In(h.NewScenario())
		mine := [][]string{{"reference", `"` + reference + `"`}}
		go func() {
			errs <- func() error {
				if err := in.Step("a parcels-db database with the following properties:", database); err != nil {
					return err
				}
				// Reads on every side of the write, so that the scenarios' steps meet.
				for range 20 {
					if err := in.Step(selectRows, mine); err != nil {
						return err
					}
				}
				if err := in.Step("a " + reference + ".yaml db seed"); err != nil {
					return err
				}
				for range 20 {
					if err := in.Step(selectRows, mine); err != nil {
						return err
					}
				}
				return in.Step("the 40th selection has 1 row")
			}()
		}()
	}
	for range scenarios {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

// A file that stays locked for longer than a step waits is not a table with no
// column types. The step says that it could not read them, where it once went
// on, and selected by a value of any type when the lock had gone by then.
func TestALockedSQLiteFileIsNotATableWithNoTypes(t *testing.T) {
	h := sqliteDB(t)
	service := sqliteService(t, h)
	svc, err := Context(h.SC).Service("parcels-db")
	if err != nil {
		t.Fatal(err)
	}
	// The same file, for steps that do not wait: the lock outlasts them at once.
	path, _, _ := strings.Cut(svc.Target.DSN, "?")
	h.OK("a nowait-db database with the following properties:",
		[][]string{{"url", "sqlite:" + path + "?_pragma=busy_timeout(0)"}, {"user", "u"}, {"password", "p"}})
	const selectNow = "a selection of rows is retrieved from the pickups table on nowait-db where:"
	ofAnotherType := [][]string{{"postcode", "10115"}}
	// Its connection has read the schema, as one a scenario has used has: what it
	// then needs of the file is the table itself.
	h.OK(selectNow, [][]string{{"postcode", `"10115"`}})

	ctx := context.Background()
	conn, err := service.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	_ = h.Fails(selectNow, "could not read the table's column types: database is locked", ofAnotherType)
	_ = h.Fails("within 1s a selection of at least 1 row is retrieved from the pickups table on nowait-db where:",
		"could not read the table's column types: database is locked", ofAnotherType)
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}
	_ = h.Fails(selectNow, `postcode is text in pickups (TEXT): write "10115"`, ofAnotherType)
}
