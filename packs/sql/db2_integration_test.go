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

// db2Server is a Db2 with a parcels database, and how to log in to it: the
// one AXX_DB2_TEST_URL names, with AXX_DB2_TEST_USER and the password in the
// file AXX_DB2_TEST_PASSWORD_FILE names, or one this starts, which takes a
// minute or two. The image's first start archives logs and tunes Db2 to the
// machine unless it is told not to (ARCHIVE_LOGS, AUTOCONFIG), and each takes
// minutes a test's database has no use for.
func db2Server(t *testing.T) (url, user, password string) {
	t.Helper()
	if url := os.Getenv("AXX_DB2_TEST_URL"); url != "" {
		pw, err := os.ReadFile(os.Getenv("AXX_DB2_TEST_PASSWORD_FILE"))
		if err != nil {
			t.Fatal(err)
		}
		return url, os.Getenv("AXX_DB2_TEST_USER"), strings.TrimSpace(string(pw))
	}
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
	if c != nil {
		t.Cleanup(func() { _ = c.Terminate(ctx) })
	}
	if err != nil && c != nil {
		// Not a machine without Docker: a container that started and did not
		// get ready, or stopped. Skipped, the test would prove nothing.
		said := "no log"
		if logs, lerr := c.Logs(ctx); lerr == nil {
			b, _ := io.ReadAll(logs)
			_ = logs.Close()
			if len(b) > 3000 {
				b = b[len(b)-3000:]
			}
			said = string(b)
		}
		t.Fatalf("Db2 did not get ready: %v\nthe end of its log:\n%s", err, said)
	}
	if err != nil {
		t.Skipf("%s unavailable: %v", db2Image, err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := c.MappedPort(ctx, "50000/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("jdbc:db2://%s:%s/parcels", host, port.Port()), db2User, db2Password
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
		for _, table := range []string{"pickups", "seeded", "always_id", depot + ".shelves"} {
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
	} {
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
			{"seedd:\n  - reference: PX-DB2-3\n", "has the tables ALWAYS_ID, PICKUPS, SEEDED"},
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

	t.Run("triggers are another database's", func(t *testing.T) {
		err := h.step("a before insert trigger on the pickups table will raise an 23505 exception 1 time where:", []string{"reference", "PX-DB2-T1"})
		if err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
			t.Errorf("a trigger on Db2: %v", err)
		}
	})
}
