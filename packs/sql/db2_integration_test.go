//go:build integration

package sql

import (
	"context"
	dbsql "database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/packs/sql/internal/db2"
)

// db2Image is IBM's Db2 Community Edition, whose license its LICENSE setting
// accepts. It is built for amd64 only: a Mac with Apple silicon emulates it.
const db2Image = "icr.io/db2_community/db2:12.1.5.0@sha256:2de8151713c261843868c5c3411b57be6ae79d99d70a5b3022337836776bfda6"

const (
	db2User     = "db2inst1"
	db2Password = "Parcels-Secret-1"
	// db2Startup is how long Db2's first start may take: a minute or two on
	// the processor it is built for, and more where it is emulated.
	db2Startup = 8 * time.Minute
)

var (
	db2Once      sync.Once
	db2URL       string
	db2Container testcontainers.Container
	// db2Failed is why the package's Db2 did not start, and db2Missing why
	// there is no Docker to start it with.
	db2Failed, db2Missing string
)

// db2Server is a Db2 with a parcels database, and how to log in to it: the
// one AXX_DB2_TEST_URL names, with AXX_DB2_TEST_USER and the password in the
// file AXX_DB2_TEST_PASSWORD_FILE names, or one container for the package,
// which takes a minute or two to start. The image's first start archives
// logs and tunes Db2 to the machine unless it is told not to (ARCHIVE_LOGS,
// AUTOCONFIG), which a test's database has no use for.
func db2Server(t *testing.T) (url, user, password string) {
	t.Helper()
	if url := os.Getenv("AXX_DB2_TEST_URL"); url != "" {
		pw, err := os.ReadFile(os.Getenv("AXX_DB2_TEST_PASSWORD_FILE"))
		if err != nil {
			t.Fatal(err)
		}
		return url, os.Getenv("AXX_DB2_TEST_USER"), strings.TrimSpace(string(pw))
	}
	db2Once.Do(func() {
		ctx := context.Background()
		c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image: db2Image, ExposedPorts: []string{"50000/tcp"}, Privileged: true,
				Env: map[string]string{
					"LICENSE": "accept", "DB2INST1_PASSWORD": db2Password, "DBNAME": "parcels",
					"AUTOCONFIG": "false", "ARCHIVE_LOGS": "false", "TZ": databaseZone,
				},
				// Each wait has a minute of its own unless it is given more.
				WaitingFor: wait.ForAll(
					wait.ForLog("Setup has completed").WithStartupTimeout(db2Startup),
					wait.ForListeningPort("50000/tcp").WithStartupTimeout(db2Startup),
				).WithDeadline(db2Startup),
			},
			Started: true,
		})
		switch {
		case err != nil && c != nil:
			// Not a machine without Docker: a container that started and did
			// not get ready, or stopped. Skipped, the tests would prove nothing.
			said := "no log"
			if logs, lerr := c.Logs(ctx); lerr == nil {
				b, _ := io.ReadAll(logs)
				_ = logs.Close()
				if len(b) > 3000 {
					b = b[len(b)-3000:]
				}
				said = string(b)
			}
			_ = c.Terminate(ctx)
			db2Failed = fmt.Sprintf("Db2 did not get ready: %v\nthe end of its log:\n%s", err, said)
		case err != nil:
			db2Missing = fmt.Sprintf("%s unavailable: %v", db2Image, err)
		default:
			host, herr := c.Host(ctx)
			port, perr := c.MappedPort(ctx, "50000/tcp")
			if herr != nil || perr != nil {
				db2Failed = fmt.Sprintf("Db2's address: %v, %v", herr, perr)
				return
			}
			db2Container, db2URL = c, fmt.Sprintf("jdbc:db2://%s:%s/parcels", host, port.Port())
		}
	})
	if db2Failed != "" {
		t.Fatal(db2Failed)
	}
	if db2Missing != "" {
		t.Skip(db2Missing)
	}
	return db2URL, db2User, db2Password
}

func db2ForTest(t *testing.T, url, user, password string) *dbsql.DB {
	t.Helper()
	cfg, err := db2.ParseURL(url, user, password)
	if err != nil {
		t.Fatal(err)
	}
	db := dbsql.OpenDB(db2.NewConnector(cfg))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestDb2(t *testing.T) {
	server, user, password := db2Server(t)
	// A schema of this run's own, which the URL makes the current one, and
	// another beside it.
	schema := fmt.Sprintf("AXX%d", time.Now().UnixNano()%1e9)
	depot := schema + "D"
	admin := db2ForTest(t, server, user, password)
	for _, s := range []string{"CREATE SCHEMA " + schema, "CREATE SCHEMA " + depot} {
		if _, err := admin.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	url := server + ":currentSchema=" + schema + ";"
	if strings.Contains(server, "/parcels:") {
		url = server + "currentSchema=" + schema + ";" // a URL with properties of its own
	}
	db := db2ForTest(t, url, user, password)
	t.Cleanup(func() {
		for _, table := range []string{"pickups", "seeded", "always_id", "trigger_parcels", depot + ".shelves"} {
			_, _ = db.Exec("DROP TABLE " + table)
		}
		_, _ = admin.Exec("DROP SCHEMA " + schema + " RESTRICT")
		_, _ = admin.Exec("DROP SCHEMA " + depot + " RESTRICT")
	})
	for _, s := range []string{
		`CREATE TABLE pickups (
			reference VARCHAR(40) NOT NULL PRIMARY KEY, postcode VARCHAR(10), code CHAR(5), note CLOB(1M), grams INT, label BIGINT, tries SMALLINT,
			kilos DECIMAL(10,2), rate DOUBLE, ratio REAL, thin REAL, exact DECFLOAT, signed_for BOOLEAN, due DATE, opens TIME, planned_at TIMESTAMP,
			details VARCHAR(200), photo BLOB(1M), stamp VARCHAR(8) FOR BIT DATA, seal VARBINARY(8), title VARGRAPHIC(20), manifest XML)`,
		`INSERT INTO pickups VALUES ('PX-TYPES-1', '10115', 'H91E2', 'ring twice', 850, 3000000000, 2, 12.50, 1.5, 0.5, 0.1, 1.20, TRUE,
			'2026-09-24', '07:40:00', '2026-09-24 07:40:00', '{"zone":"DE-1","count":3}', BX'78AB', BX'00FF', BX'CAFE',
			'Zoë 日本', '<parcel><grams>850</grams></parcel>')`,
		`INSERT INTO pickups (reference, grams) VALUES ('ABC-OTHER', 1)`,
		`CREATE TABLE seeded (id BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, reference VARCHAR(40), kilos DECIMAL(10,2), flagged BOOLEAN, due DATE,
			planned_at TIMESTAMP, created_at TIMESTAMP, details CLOB(1M), photo BLOB(1M), stamp VARCHAR(8) FOR BIT DATA)`,
		`CREATE TABLE always_id (id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY, reference VARCHAR(40))`,
		`CREATE TABLE ` + depot + `.shelves (code VARCHAR(10) NOT NULL PRIMARY KEY, parcels INT)`,
		`CREATE TABLE trigger_parcels (id BIGINT GENERATED ALWAYS AS IDENTITY, reference VARCHAR(40) NOT NULL PRIMARY KEY, status VARCHAR(20), kilos DECIMAL(10,2))`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	for _, s := range triggerRows("trigger_parcels") {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	h := newHarness(t)
	h.ok("a parcels-db database with the following properties:", []string{"url", url}, []string{"user", user}, []string{"password", password})

	t.Run("values are held to their columns", func(t *testing.T) {
		columnCases{
			table: "pickups",
			fails: [][3]string{
				{"reference", "0", `reference is text in pickups (VARCHAR): write "0"`},
				{"postcode", "10115", `postcode is text in pickups (VARCHAR): write "10115"`},
				{"code", "12345", `code is text in pickups (CHAR): write "12345"`},
				{"note", "true", `note is text in pickups (CLOB): write "true"`},
				{"title", "1", `title is text in pickups (VARGRAPHIC): write "1"`},
				{"grams", `"850"`, `grams is an integer in pickups (INTEGER): write 850`},
				{"grams", "850.0", `grams is an integer in pickups (INTEGER): write 850`},
				{"label", "3000000000.0", `label is an integer in pickups (BIGINT): write 3000000000`},
				{"tries", "true", `tries is an integer in pickups (SMALLINT)`},
				{"kilos", "12", `kilos is a decimal in pickups (DECIMAL): write 12.0`},
				{"rate", "2", `rate is a decimal in pickups (DOUBLE)`},
				{"ratio", "1", `ratio is a decimal in pickups (REAL)`},
				{"exact", "1", `exact is a decimal in pickups (DECFLOAT)`},
				{"signed_for", "1", `signed_for is a boolean in pickups (BOOLEAN)`},
				{"signed_for", `"true"`, `signed_for is a boolean in pickups (BOOLEAN)`},
				{"due", "2026", `due is written as a string in pickups (DATE): write "2026"`},
				{"planned_at", `"2026-09-24T07:40:00+02:00"`, `planned_at has no time zone in pickups (TIMESTAMP)`},
			},
			finds: [][2]string{
				{"reference", "PX-TYPES-1"},
				{"postcode", `"10115"`},
				{"code", "H91E2"},
				{"note", "ring twice"},
				{"title", "Zoë 日本"},
				{"grams", "850"},
				{"label", "3000000000"},
				{"tries", "2"},
				{"kilos", "12.5"},
				{"kilos", "12.50"},
				{"rate", "1.5"},
				{"ratio", "0.5"},
				{"thin", "0.1"},
				{"exact", "1.2"},
				{"signed_for", "true"},
				{"due", `"2026-09-24"`},
				{"opens", `"07:40:00"`},
				{"planned_at", `"2026-09-24 07:40:00"`},
				{"planned_at", `"2026-09-24T07:40:00"`},
				{"details", `{"zone":"DE-1","count":3}`},
				{"photo", "78AB"},
				{"photo", "78ab"},
				{"stamp", "00FF"},
				{"seal", "CAFE"},
			},
			misses: [][2]string{
				{"reference", "PX-NONE"},
				{"grams", "1000"},
				{"kilos", "3.0"},
				{"postcode", `"99999"`},
				{"signed_for", "false"},
				{"details", `{"count":3,"zone":"DE-1"}`},
				{"photo", "78AC"},
			},
		}.run(t, h, true)
	})

	t.Run("a column's value", func(t *testing.T) {
		cellCases{
			table: "pickups",
			passes: [][2]string{
				{"reference", "PX-TYPES-1"},
				{"postcode", `"10115"`},
				{"code", "H91E2"},
				{"note", "ring twice"},
				{"title", "Zoë 日本"},
				{"grams", "850"},
				{"label", "3000000000"},
				{"tries", "2"},
				{"kilos", "12.5"},
				{"kilos", "12.50"},
				{"rate", "1.5"},
				{"ratio", "0.5"},
				{"thin", "0.1"},
				{"exact", "1.20"},
				{"signed_for", "true"},
				{"due", "2026-09-24"},
				{"opens", "07:40:00"},
				{"planned_at", "2026-09-24 07:40:00"},
				{"details", `"{"zone":"DE-1","count":3}"`},
				{"photo", "78AB"},
				{"stamp", "00FF"},
				{"seal", "CAFE"},
				{"manifest", "<parcel><grams>850</grams></parcel>"},
			},
			fails: [][3]string{
				{"postcode", "10115", `"10115"`},
				{"grams", `"850"`, "850"},
				{"kilos", "12", "differs"},
				{"due", "2026-09-25", "differs"},
				{"signed_for", "1", "differs"},
				{"planned_at", "2026-09-24T07:40:00Z", "has no time zone"},
			},
		}.run(t, h)
	})

	t.Run("a column's JSON", func(t *testing.T) {
		h.ok("a selection of rows is retrieved from the pickups table where:", []string{"reference", "PX-TYPES-1"})
		n := nth(len(mustService(t, h).Selections()))
		h.ok(fmt.Sprintf("the 1st row details property for the %s selection json properties are:", n),
			[]string{"zone", "DE-1"}, []string{"count", "3"})
		if err := h.step(fmt.Sprintf("the 1st row details property for the %s selection json properties are:", n), []string{"count", `"3"`}); err == nil {
			t.Error(`"3" matched the number 3`)
		}
	})

	t.Run("seeds", func(t *testing.T) {
		h.file("seed.yaml", `seeded:
  - id: 7001
    reference: PX-DB2-1
    kilos: 2.5
    flagged: true
    due: "2026-09-24"
    planned_at: "2026-09-24 07:40:00.123"
    created_at: "[DAY,PLUS,1]"
    details: {zone: DE-1, stops: [1, 2]}
    photo: CAFE
    stamp: 00FF
  - reference: PX-DB2-2
    kilos: "[null]"
    flagged: false
    created_at: "[HOUR,MINUS,2]"
    planned_at: "2026-09-24T07:40:00"
`+depot+`.shelves:
  - code: A-1
    parcels: 3
`)
		h.ok("a seed.yaml db seed")
		h.ok("a selection of rows is retrieved from the seeded table where:",
			[]string{"id", "7001"}, []string{"reference", "PX-DB2-1"}, []string{"kilos", "2.5"}, []string{"flagged", "true"}, []string{"due", `"2026-09-24"`},
			[]string{"planned_at", `"2026-09-24 07:40:00.123"`}, []string{"details", `{"stops":[1,2],"zone":"DE-1"}`}, []string{"photo", "CAFE"}, []string{"stamp", "00FF"})
		svc := mustService(t, h)
		last := func() *Selection {
			all := svc.Selections()
			return all[len(all)-1]
		}
		if len(last().Rows) != 1 {
			t.Fatalf("seeded row: %v", last().Rows)
		}
		// A day from now by the database's clock, and two hours ago to the minute.
		var days, minutes int
		if err := db.QueryRow("SELECT DAYS(created_at) - DAYS(CURRENT DATE) FROM seeded WHERE id = 7001").Scan(&days); err != nil {
			t.Fatal(err)
		}
		if days != 1 {
			t.Errorf("created_at is %d days from the database's clock, want 1", days)
		}
		if err := db.QueryRow("SELECT TIMESTAMPDIFF(4, CHAR(CURRENT TIMESTAMP - created_at)) FROM seeded WHERE reference = 'PX-DB2-2'").Scan(&minutes); err != nil {
			t.Fatal(err)
		}
		if minutes < 119 || minutes > 121 {
			t.Errorf("created_at is %d minutes ago, want 120", minutes)
		}
		h.ok("a selection of rows is retrieved from the seeded table where:",
			[]string{"reference", "PX-DB2-2"}, []string{"kilos", "null"}, []string{"flagged", "false"})
		if len(last().Rows) != 1 {
			t.Errorf("a row with a null and a false: %v", last().Rows)
		}
		h.ok("a selection of rows is retrieved from the "+depot+".shelves table where:", []string{"code", "A-1"}, []string{"parcels", "3"})
		if len(last().Rows) != 1 {
			t.Errorf("a row of another schema's table: %v", last().Rows)
		}
	})

	t.Run("a seed's mistakes say what to write", func(t *testing.T) {
		for _, c := range []struct{ seed, want string }{
			{"seeded:\n  - id: 7001\n    reference: PX-DB2-1\n", "a row with that key is already there"},
			{"seeded:\n  - no_such_column: PX-DB2-3\n", "seeded has the columns ID, REFERENCE"},
			{"seedd:\n  - reference: PX-DB2-3\n", "has the tables ALWAYS_ID, PICKUPS, SEEDED, TRIGGER_PARCELS"},
			{depot + ".shelfs:\n  - code: A-2\n", "the schema " + depot + " has the tables SHELVES"},
			{"always_id:\n  - id: 41\n    reference: PX-DB2-ALWAYS\n", "Db2 lets no row set a GENERATED ALWAYS column"},
		} {
			h.file("bad.yaml", c.seed)
			if err := h.step("a bad.yaml db seed"); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%q: %v, want %s", c.seed, err, c.want)
			}
		}
		// Left out, the column is Db2's to give.
		h.file("generated.yaml", "always_id:\n  - reference: PX-DB2-ALWAYS\n")
		h.ok("a generated.yaml db seed")
	})

	t.Run("row locks", func(t *testing.T) {
		h.ok("the rows in the pickups table are locked where:", []string{"reference", "PX-TYPES-1"})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := db.ExecContext(ctx, "UPDATE pickups SET grams = 851 WHERE reference = 'PX-TYPES-1'")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("the row is not locked: %v", err)
		}
		h.ok("the row locks are released")
		if _, err := db.Exec("UPDATE pickups SET grams = 850 WHERE reference = 'PX-TYPES-1'"); err != nil {
			t.Errorf("after releasing: %v", err)
		}
	})

	t.Run("a row someone else holds is waited for, ten seconds", func(t *testing.T) {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.Exec("UPDATE pickups SET grams = 2 WHERE reference = 'ABC-OTHER'"); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		err = h.step("the rows in the pickups table are locked where:", []string{"reference", "ABC-OTHER"})
		took := time.Since(start)
		if err == nil || !strings.Contains(err.Error(), "Could not acquire row lock") || took < 9*time.Second || took > 20*time.Second {
			t.Errorf("after %s: %v", took, err)
		}
		// Released, the row is the step's to lock.
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		h.ok("the row locks are released")
		h.ok("the rows in the pickups table are locked where:", []string{"reference", "ABC-OTHER"})
		h.ok("the row locks are released")
	})

	t.Run("a statement that runs past its time is stopped", func(t *testing.T) {
		one := db2ForTest(t, url, user, password)
		one.SetMaxOpenConns(1)
		handle := func() (h int64) {
			t.Helper()
			if err := one.QueryRow("VALUES MON_GET_APPLICATION_HANDLE()").Scan(&h); err != nil {
				t.Fatal(err)
			}
			return h
		}
		before := handle()
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		start := time.Now()
		// Minutes of counting, were it left to.
		_, err := one.ExecContext(ctx, "SELECT COUNT(*) FROM syscat.columns a, syscat.columns b, syscat.columns c")
		took := time.Since(start)
		if !errors.Is(err, context.DeadlineExceeded) || took < 400*time.Millisecond || took > 10*time.Second {
			t.Errorf("after %s: %v", took, err)
		}
		// The server stopped it, and the connection is the one it was.
		if after := handle(); after != before {
			t.Errorf("the statement after it ran on connection %d, not on %d, which the stopped one ran on", after, before)
		}
	})

	t.Run("the database's clock", func(t *testing.T) {
		svc := mustService(t, h)
		now, err := svc.databaseNow(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if d := time.Since(now); d < -time.Minute || d > time.Minute {
			t.Errorf("the database's clock reads %s, %s from this machine's", now, d)
		}
		var hhmmss int
		if err := db.QueryRow("VALUES INT(CURRENT TIMEZONE)").Scan(&hhmmss); err != nil {
			t.Fatal(err)
		}
		_, offset := now.Zone()
		if want := hhmmss/10000*3600 + hhmmss/100%100*60; offset != want {
			t.Errorf("the clock's zone is %d seconds from UTC, and Db2's %d", offset, want)
		}
	})

	t.Run("a user and its password", func(t *testing.T) {
		wrong := newHarness(t)
		wrong.ok("a parcels-db database with the following properties:", []string{"url", url}, []string{"user", user}, []string{"password", "not-the-password"})
		err := wrong.step("a selection of rows is retrieved from the pickups table where:", []string{"reference", "PX-TYPES-1"})
		if err == nil || !strings.Contains(err.Error(), "the password is not the user's") {
			t.Errorf("a wrong password: %v", err)
		}
	})

	t.Run("triggers", func(t *testing.T) {
		insert := func(ref string) error {
			_, err := db.Exec("INSERT INTO trigger_parcels (reference, status, kilos) VALUES ('" + ref + "', 'REGISTERED', 1.5)")
			return err
		}
		stored := func(ref string) int {
			t.Helper()
			var n int
			if err := db.QueryRow("SELECT COUNT(*) FROM trigger_parcels WHERE reference = '" + ref + "'").Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		// Db2 gives the state the trigger raised, with its own code for an
		// error a trigger raises.
		raised := func(what string, err error, state string) {
			t.Helper()
			var e *db2.Error
			if !errors.As(err, &e) || e.SQLState != state || e.Code != -438 || !strings.Contains(e.Message, "axx simulated exception (sqlState: "+state+")") {
				t.Errorf("%s: %v, want SQL0438N with SQLSTATE %s", what, err, state)
			}
		}

		h.ok("a before insert trigger on the trigger_parcels table will raise a 40001 exception 1 time where:", []string{"reference", "PX-DB2-T1"})
		raised("the first insert", insert("PX-DB2-T1"), "40001")
		if err := insert("PX-DB2-T1"); err != nil {
			t.Errorf("the second insert: %v", err)
		}
		if err := insert("PX-DB2-OTHER"); err != nil {
			t.Errorf("a row it does not match: %v", err)
		}
		h.ok("the before insert trigger on the trigger_parcels table was raised 1 time")
		if err := h.step("the before insert trigger on the trigger_parcels table was raised 2 times"); err == nil {
			t.Error("a wrong count passed")
		}

		// A state with letters, in lower case as a feature may write it, and
		// every matching insert.
		h.ok("a 2nd ordered before insert trigger on the trigger_parcels table will raise a 57p01 exception where:",
			[]string{"reference", "PX-DB2-T2"}, []string{"status", "REGISTERED"})
		for range 2 {
			raised("an insert it always refuses", insert("PX-DB2-T2"), "57P01")
		}
		if n := stored("PX-DB2-T2"); n != 0 {
			t.Errorf("the failed insert stored %d rows", n)
		}
		h.ok("the 2nd ordered before insert trigger on the trigger_parcels table was raised 2 times")

		// The row stays, and the insert fails: a write that succeeded and reported a failure.
		h.ok("a 3rd ordered before insert trigger on the trigger_parcels table will insert and raise a 08006 exception where:", []string{"reference", "PX-DB2-T3"})
		raised("insert and raise", insert("PX-DB2-T3"), "08006")
		if n := stored("PX-DB2-T3"); n != 1 {
			t.Errorf("insert and raise stored %d rows, want 1", n)
		}
		// Again: a key the row has already is no other failure.
		raised("insert and raise, once more", insert("PX-DB2-T3"), "08006")
		h.ok("the 3rd ordered before insert trigger on the trigger_parcels table was raised 2 times")

		// Only that many are kept and refused; the one after them is an insert like any other.
		h.ok("a 4th ordered before insert trigger on the trigger_parcels table will insert and raise a 40001 exception 1 time where:", []string{"reference", "PX-DB2-T4"})
		raised("the first, kept", insert("PX-DB2-T4"), "40001")
		if n := stored("PX-DB2-T4"); n != 1 {
			t.Errorf("the first stored %d rows, want 1", n)
		}
		if err := insert("PX-DB2-T4"); err == nil || db2Code(err) != -803 {
			t.Errorf("the second, of a key the table has: %v", err)
		}
		h.ok("the 4th ordered before insert trigger on the trigger_parcels table was raised 1 time")

		for _, c := range []struct {
			code string
			rows []string
			want string
		}{
			{"ORA-20001", []string{"reference", "PX-DB2-T5"}, "is an Oracle error"},
			{"01000", []string{"reference", "PX-DB2-T5"}, "fails nothing on Db2"},
			{"00000", []string{"reference", "PX-DB2-T5"}, "fails nothing on Db2"},
			{"40001", []string{"kilos", "2"}, "write 2.0"},
			{"40001", []string{"no_such_column", "1"}, "before insert trigger"},
		} {
			err := h.step(fmt.Sprintf("a before insert trigger on the trigger_parcels table will raise a %s exception where:", c.code), c.rows)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s %v: %v, want %s", c.code, c.rows, err, c.want)
			}
		}

		if err := mustService(t, h).dropTriggers(context.Background()); err != nil {
			t.Fatal(err)
		}
		if left := db2Leftovers(t, db); left != 0 {
			t.Errorf("%d of the triggers' objects left", left)
		}
		if err := insert("PX-DB2-T5"); err != nil {
			t.Errorf("after the triggers: %v", err)
		}
	})

	t.Run("update and delete triggers", func(t *testing.T) {
		updateAndDeleteTriggers(t, h, db, "trigger_parcels", "40001")
		if left := db2Leftovers(t, db); left != 0 {
			t.Errorf("%d of the triggers' objects left", left)
		}
	})

	t.Run("JSONB is another database's", func(t *testing.T) {
		err := h.step("a selection of rows is retrieved from the pickups table where the details jsonb column contains:", []string{"zone", "DE-1"})
		if err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
			t.Errorf("JSONB containment on Db2: %v", err)
		}
	})
}

// db2Leftovers counts what triggers' steps make, in the test's schema:
// triggers, procedures, variables and sequences.
func db2Leftovers(t *testing.T, db *dbsql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM syscat.triggers WHERE trigschema = CURRENT SCHEMA AND trigname LIKE 'AXX\_%' ESCAPE '\') +
		(SELECT COUNT(*) FROM syscat.routines WHERE routineschema = CURRENT SCHEMA AND routinename LIKE 'AXX\_%' ESCAPE '\') +
		(SELECT COUNT(*) FROM syscat.variables WHERE varschema = CURRENT SCHEMA AND varname LIKE 'AXX\_%' ESCAPE '\') +
		(SELECT COUNT(*) FROM syscat.sequences WHERE seqschema = CURRENT SCHEMA AND seqname LIKE 'AXX\_%' ESCAPE '\')
		FROM sysibm.sysdummy1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A Db2 that takes no password as it is (AUTHENTICATION SERVER_ENCRYPT) is
// logged in to with the user and the password encrypted, whether the URL
// names the way or leaves it to axx, and so is one that takes them encrypted
// with AES only (ALTERNATE_AUTH_ENC AES_ONLY).
func TestDb2EncryptedPasswords(t *testing.T) {
	server, user, password := db2Server(t)
	if db2Container == nil {
		t.Skip("this changes how the server takes a user: it needs the Db2 the tests start, not one of AXX_DB2_TEST_URL")
	}
	who := func(url, password string) (string, error) {
		t.Helper()
		var name string
		err := db2ForTest(t, url, user, password).QueryRow("VALUES CURRENT USER").Scan(&name)
		return strings.TrimSpace(name), err
	}
	// A Db2 that takes a password as it is takes it no other way, and says
	// which ways it does.
	if name, err := who(server+":securityMechanism=3;", password); err != nil || name != "DB2INST1" {
		t.Errorf("securityMechanism=3 on a Db2 that takes a password as it is: %q, %v", name, err)
	}
	for _, mechanism := range []string{"7", "9"} {
		_, err := who(server+":securityMechanism="+mechanism+";", password)
		if err == nil || !strings.Contains(err.Error(), "the server does not take security mechanism "+mechanism+" (it offers 3") {
			t.Errorf("securityMechanism=%s on a Db2 that takes a password as it is: %v", mechanism, err)
		}
	}

	ctx := context.Background()
	code, out, err := db2Container.Exec(ctx, []string{
		"su", "-", db2User, "-c",
		"db2 update dbm cfg using AUTHENTICATION SERVER_ENCRYPT && db2stop force && db2start && db2 activate db parcels",
	})
	if err != nil || code != 0 {
		said, _ := io.ReadAll(out)
		t.Fatalf("setting AUTHENTICATION SERVER_ENCRYPT: exit %d, %v\n%s", code, err, said)
	}

	// The steps, with the URL a feature would have: axx finds the way itself.
	h := newHarness(t)
	h.ok("a parcels-db database with the following properties:", []string{"url", server}, []string{"user", user}, []string{"password", password})
	h.ok("a selection of rows is retrieved from the sysibm.sysdummy1 table where:", []string{"ibmreqd", "Y"})
	if rows := len(mustService(t, h).Selections()[0].Rows); rows != 1 {
		t.Errorf("%d rows selected, want 1", rows)
	}
	for _, mechanism := range []string{"7", "9"} {
		if name, err := who(server+":securityMechanism="+mechanism+";", password); err != nil || name != "DB2INST1" {
			t.Errorf("securityMechanism=%s: %q, %v", mechanism, name, err)
		}
	}
	if _, err := who(server+":securityMechanism=3;", password); err == nil || !strings.Contains(err.Error(), "does not take a user and a password as they are") {
		t.Errorf("securityMechanism=3 on a Db2 that takes no password as it is: %v", err)
	}
	if _, err := who(server, "not-the-password"); err == nil || !strings.Contains(err.Error(), "the password is not the user's") {
		t.Errorf("a wrong password, encrypted: %v", err)
	}
	// Such a Db2 takes either cipher: AES, which axx asks for first, and
	// DES when the URL names it.
	for _, cipher := range []string{"1", "2"} {
		if name, err := who(server+":encryptionAlgorithm="+cipher+";", password); err != nil || name != "DB2INST1" {
			t.Errorf("encryptionAlgorithm=%s: %q, %v", cipher, name, err)
		}
	}

	// A Db2 that takes a password encrypted with AES only.
	code, out, err = db2Container.Exec(ctx, []string{
		"su", "-", db2User, "-c",
		"db2 update dbm cfg using ALTERNATE_AUTH_ENC AES_ONLY && db2stop force && db2start && db2 activate db parcels",
	})
	if err != nil || code != 0 {
		said, _ := io.ReadAll(out)
		t.Fatalf("setting ALTERNATE_AUTH_ENC AES_ONLY: exit %d, %v\n%s", code, err, said)
	}
	aes := newHarness(t)
	aes.ok("a parcels-db database with the following properties:", []string{"url", server}, []string{"user", user}, []string{"password", password})
	aes.ok("a selection of rows is retrieved from the sysibm.sysdummy1 table where:", []string{"ibmreqd", "Y"})
	for _, url := range []string{server + ":securityMechanism=7;", server + ":securityMechanism=9;encryptionAlgorithm=2;"} {
		if name, err := who(url, password); err != nil || name != "DB2INST1" {
			t.Errorf("%s on a Db2 of AES only: %q, %v", url, name, err)
		}
	}
	if _, err := who(server+":encryptionAlgorithm=1;", password); err == nil || !strings.Contains(err.Error(), "not with DES") {
		t.Errorf("DES on a Db2 of AES only: %v", err)
	}
	if _, err := who(server, "not-the-password"); err == nil || !strings.Contains(err.Error(), "the password is not the user's") {
		t.Errorf("a wrong password, encrypted with AES: %v", err)
	}
}
