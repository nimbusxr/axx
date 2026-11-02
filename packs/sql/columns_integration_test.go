//go:build integration

package sql

import (
	"context"
	dbsql "database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/core"
)

// columnCases are a dialect's real columns: what its driver names them, what a
// value of another type is told to write, and the values that select the row.
type columnCases struct {
	table  string
	fails  [][3]string // column, cell, message
	finds  [][2]string // column, cell
	misses [][2]string // column, cell: of the type, and of no row
}

// A value is held to its column's type, on each database, on what selects,
// polls and locks rows.
func (c columnCases) run(t *testing.T, h *harness, lockable bool) {
	t.Helper()
	sel := fmt.Sprintf("a selection of rows is retrieved from the %s table where:", c.table)
	poll := fmt.Sprintf("within 30s a selection of at least 1 row is retrieved from the %s table where:", c.table)
	lock := fmt.Sprintf("the rows in the %s table are locked where:", c.table)
	for _, f := range c.fails {
		err := h.step(sel, []string{f[0], f[1]})
		var ve *core.ValueError
		if !errors.As(err, &ve) || !strings.Contains(err.Error(), f[2]) {
			t.Errorf("%s %s: %v, want %s", f[0], f[1], err, f[2])
		}
		start := time.Now()
		if err := h.step(poll, []string{f[0], f[1]}); err == nil || !strings.Contains(err.Error(), f[2]) {
			t.Errorf("polling %s %s: %v, want %s", f[0], f[1], err, f[2])
		}
		if took := time.Since(start); took > 15*time.Second {
			t.Errorf("polling %s %s took %s: it should fail at once", f[0], f[1], took)
		}
		if lockable {
			if err := h.step(lock, []string{f[0], f[1]}); err == nil || !strings.Contains(err.Error(), f[2]) {
				t.Errorf("locking %s %s: %v, want %s", f[0], f[1], err, f[2])
			}
		}
	}
	svc, err := Context(h.sc).Service()
	if err != nil {
		t.Fatal(err)
	}
	selected := func() int {
		all := svc.Selections()
		return len(all[len(all)-1].Rows)
	}
	for _, f := range c.finds {
		if err := h.step(sel, []string{f[0], f[1]}); err != nil {
			t.Errorf("%s %s: %v", f[0], f[1], err)
			continue
		}
		if got := selected(); got != 1 {
			t.Errorf("%s %s selected %d rows, not 1", f[0], f[1], got)
		}
	}
	for _, f := range c.misses {
		if err := h.step(sel, []string{f[0], f[1]}); err != nil {
			t.Errorf("%s %s: %v", f[0], f[1], err)
			continue
		}
		if got := selected(); got != 0 {
			t.Errorf("%s %s selected %d rows, not 0", f[0], f[1], got)
		}
	}
	if lockable {
		h.ok(lock, []string{"reference", "PX-TYPES-1"})
		h.ok("the row locks are released")
	}
	// A column the table does not have is the database's to report.
	if err := h.step(sel, []string{"no_such_column", "1"}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "no_such_column") {
		t.Errorf("an unknown column should keep the database's error: %v", err)
	}
}

func TestColumnTypesPostgres(t *testing.T) {
	url := postgresURL(t)
	tgt, _ := Resolve(url, "space", "secret")
	db, err := dbsql.Open("pgx", tgt.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE EXTENSION IF NOT EXISTS citext;
		CREATE SCHEMA parcels;
		CREATE TYPE parcels.pickup_status AS ENUM ('PLANNED', 'DONE');
		CREATE TABLE parcels.pickups (
			reference varchar(40) primary key, postcode text, code char(5), shop citext, grams integer, label bigint, tries smallint,
			kilos numeric(10,2), rate float8, ratio real, signed boolean, day date, planned_at timestamptz, token uuid,
			status parcels.pickup_status, fee money, details jsonb, photo bytea, thin real);
		INSERT INTO parcels.pickups VALUES ('PX-TYPES-1', '10115', 'H91E2', 'Willow', 850, 3000000000, 2, 12.50, 1.5, 0.5, true, '2026-09-24',
			'2026-09-24T07:40:00Z', '6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11', 'PLANNED', 12.50, '{"zone":"DE-1","pieces":3,"kilos":3.0}', 'x', 0.1);
	`); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.db(url)
	const tb = "parcels.pickups"
	columnCases{
		table: tb,
		fails: [][3]string{
			{"postcode", "10115", `postcode is text in parcels.pickups (TEXT): write "10115"`},
			{"reference", "0", `reference is text in parcels.pickups (VARCHAR): write "0"`},
			{"code", "12345", `code is text in parcels.pickups (BPCHAR): write "12345"`},
			{"shop", "true", `shop is text in parcels.pickups (CITEXT): write "true"`},
			{"grams", `"850"`, `grams is an integer in parcels.pickups (INT4): write 850`},
			{"grams", "850.0", `grams is an integer in parcels.pickups (INT4): write 850`},
			{"grams", "850.5", `grams is an integer in parcels.pickups (INT4): write an integer, not 850.5`},
			{"label", "3000000000.0", `label is an integer in parcels.pickups (INT8): write 3000000000`},
			{"tries", "true", `tries is an integer in parcels.pickups (INT2): write an integer, not true`},
			{"kilos", "12", `kilos is a decimal in parcels.pickups (NUMERIC): write 12.0`},
			{"kilos", `"12.50"`, `kilos is a decimal in parcels.pickups (NUMERIC): write 12.50`},
			{"rate", "2", `rate is a decimal in parcels.pickups (FLOAT8): write 2.0`},
			{"ratio", "1", `ratio is a decimal in parcels.pickups (FLOAT4): write 1.0`},
			{"signed", "1", `signed is a boolean in parcels.pickups (BOOL): write true`},
			{"signed", `"true"`, `signed is a boolean in parcels.pickups (BOOL): write true`},
			{"day", "2026", `day is written as a string in parcels.pickups (DATE): write "2026"`},
			{"day", "2026-09-24T00:00:00+02:00", `day has no time zone in parcels.pickups (DATE), so an offset would be dropped: write "2026-09-24 00:00:00"`},
			{"planned_at", "2026-09-24 07:40:00", `planned_at holds a moment in parcels.pickups (TIMESTAMPTZ), which a time with no offset does not say: write its offset, like "2026-09-24T07:40:00Z"`},
			{"planned_at", "2026-09-24", `planned_at holds a moment in parcels.pickups (TIMESTAMPTZ)`},
			{"planned_at", "1790244000", `planned_at is written as a string in parcels.pickups (TIMESTAMPTZ): write "1790244000"`},
			{"token", "7", `token is written as a string in parcels.pickups (UUID): write "7"`},
			{"status", "1", `status is written as a string in parcels.pickups (PICKUP_STATUS): write "1"`},
			{"fee", "12.50", `fee is written as a string in parcels.pickups (MONEY): write "12.50"`},
			{"details", "3", `details is written as a string in parcels.pickups (JSONB): write "3"`},
		},
		finds: [][2]string{
			{"reference", "PX-TYPES-1"},
			{"postcode", `"10115"`},
			{"code", "H91E2"},
			{"shop", "willow"},
			{"grams", "850"},
			{"label", "3000000000"},
			{"label", "3000000000L"},
			{"kilos", "12.5"},
			{"kilos", "12.50"},
			{"rate", "1.5"},
			{"ratio", "0.5"},
			{"thin", "0.1"},
			{"signed", "true"},
			{"day", "2026-09-24"},
			{"planned_at", "2026-09-24T07:40:00Z"},
			{"token", "6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11"},
			{"status", "PLANNED"},
			{"fee", `"12.50"`},
		},
		misses: [][2]string{{"grams", "851"}, {"signed", "false"}, {"kilos", "12.0"}},
	}.run(t, h, true)
	h.ok("the row locks are released")

	// A trigger's condition runs on the service's inserts, so it is held
	// before it is created.
	for _, c := range [][3]string{
		{"postcode", "10115", `postcode is text in parcels.pickups (TEXT): write "10115"`},
		{"grams", "850.0", `grams is an integer in parcels.pickups (INT4): write 850`},
	} {
		err := h.step("a before insert trigger on the parcels.pickups table will raise a 23505 exception where:", []string{c[0], c[1]})
		var ve *core.ValueError
		if !errors.As(err, &ve) || !strings.Contains(err.Error(), c[2]) {
			t.Errorf("a trigger on %s %s: %v", c[0], c[1], err)
		}
	}
	if got := len(mustService(t, h).Triggers()); got != 0 {
		t.Errorf("%d triggers were created", got)
	}
	h.ok("a before insert trigger on the parcels.pickups table will raise a 23505 exception where:", []string{"grams", "851"})

	// A float of 32 bits in a trigger's condition is the same as in a selection's.
	h.ok("a 2nd ordered before insert trigger on the parcels.pickups table will raise a 23514 exception where:", []string{"thin", "0.2"})
	if _, err := db.Exec("INSERT INTO parcels.pickups (reference, thin) VALUES ('PX-THIN', 0.2)"); err == nil || !strings.Contains(err.Error(), "23514") {
		t.Errorf("0.2 in a real: %v, want 23514", err)
	}
	if _, err := db.Exec("INSERT INTO parcels.pickups (reference, thin) VALUES ('PX-THICK', 0.3)"); err != nil {
		t.Errorf("0.3 in a real, which it does not match: %v", err)
	}

	// JSONB containment compares numbers by value: what it selects is held to
	// a check's rule.
	jsonb := "a selection of rows is retrieved from the parcels.pickups table where the details jsonb column contains:"
	count := func() int {
		all := mustService(t, h).Selections()
		return len(all[len(all)-1].Rows)
	}
	for _, c := range []struct {
		rows [][]string
		want int
	}{
		{[][]string{{"pieces", "3"}}, 1},
		{[][]string{{"kilos", "3.0"}, {"zone", "DE-1"}}, 1},
		{[][]string{{"pieces", "3.0"}}, 0},
		{[][]string{{"kilos", "3"}}, 0},
		{[][]string{{"pieces", `"3"`}}, 0},
	} {
		h.ok(jsonb, c.rows...)
		if got := count(); got != c.want {
			t.Errorf("containing %v: %d rows, not %d", c.rows, got, c.want)
		}
	}
	h.ok(jsonb, []string{"pieces", "3.0"})
	n := len(mustService(t, h).Selections())
	if err := h.step(fmt.Sprintf("the %s selection has 1 row", nth(n))); !core.IsAssertion(err) || !strings.Contains(err.Error(), "pieces is the integer 3: write 3") {
		t.Errorf("a row dropped for a number's type should say so: %v", err)
	}
}

// A column's type is the database's each time, though pgx keeps what it has
// prepared on each connection: a service may change its schema as a scenario runs.
func TestAColumnsTypeChangesWithItsSchema(t *testing.T) {
	url := postgresURL(t)
	tgt, _ := Resolve(url, "space", "secret")
	db, err := dbsql.Open("pgx", tgt.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE space.migrated (reference text primary key, postcode text)`); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.db(url)
	const sel = "a selection of rows is retrieved from the space.migrated table where:"
	for range 40 { // on as many of the pool's connections as it opens
		h.ok(sel, []string{"postcode", `"10115"`})
	}
	if _, err := db.Exec(`ALTER TABLE space.migrated ALTER COLUMN postcode TYPE integer USING postcode::integer`); err != nil {
		t.Fatal(err)
	}
	const want = "postcode is an integer in space.migrated (INT4): write 10115"
	if err := h.step(sel, []string{"postcode", `"10115"`}); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("after the change: %v, want %s", err, want)
	}
	h.ok(sel, []string{"postcode", "10115"})
}

func mustService(t *testing.T, h *harness) *Service {
	t.Helper()
	svc, err := Context(h.sc).Service()
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func nth(n int) string {
	switch {
	case n%100 >= 11 && n%100 <= 13:
		return fmt.Sprintf("%dth", n)
	case n%10 == 1:
		return fmt.Sprintf("%dst", n)
	case n%10 == 2:
		return fmt.Sprintf("%dnd", n)
	case n%10 == 3:
		return fmt.Sprintf("%drd", n)
	}
	return fmt.Sprintf("%dth", n)
}

// container starts a database image and returns its host and mapped port.
func container(t *testing.T, image, port string, env map[string]string, ready string, occurrence int) (string, string) {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: image, ExposedPorts: []string{port + "/tcp"}, Env: env,
			WaitingFor: wait.ForLog(ready).WithOccurrence(occurrence).WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("%s unavailable: %v", image, err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := c.MappedPort(ctx, port+"/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return host, mapped.Port()
}

func exec(t *testing.T, driverName, dsn string, statements ...string) {
	t.Helper()
	db, err := dbsql.Open(driverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadline := time.Now().Add(2 * time.Minute)
	for db.Ping() != nil && time.Now().Before(deadline) {
		time.Sleep(time.Second)
	}
	for _, s := range statements {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

var mysqlSchema = []string{
	`CREATE TABLE pickups (
		reference VARCHAR(40) PRIMARY KEY, postcode TEXT, code CHAR(5), note LONGTEXT, grams INT, label BIGINT UNSIGNED, tries SMALLINT, built YEAR,
		kilos DECIMAL(10,2), rate DOUBLE, ratio FLOAT, signed_for BOOLEAN, flags TINYINT UNSIGNED, seal BIT(1), day DATE, planned_at DATETIME,
		status ENUM('PLANNED','DONE'), details JSON, photo BLOB, returned BOOLEAN, seal_off BIT(1), thin FLOAT)`,
	`INSERT INTO pickups VALUES ('PX-TYPES-1', '10115', 'H91E2', 'ring twice', 850, 3000000000, 2, 2026, 12.50, 1.5, 0.5, TRUE, 3, b'1',
		'2026-09-24', '2026-09-24 07:40:00', 'PLANNED', '{"zone":"DE-1"}', 'x', FALSE, b'0', 0.1)`,
	`INSERT INTO pickups (reference, grams) VALUES ('ABC-OTHER', 1)`,
}

// mysqlCases are MySQL's and MariaDB's: the wire protocol calls TEXT and LONGTEXT
// alike TEXT.
func mysqlCases() columnCases {
	return columnCases{
		table: "pickups",
		fails: [][3]string{
			// MySQL would compare this with every reference as a number, and
			// each that is no number as 0.
			{"reference", "0", `reference is text in pickups (VARCHAR): write "0"`},
			{"postcode", "10115", `postcode is text in pickups (TEXT): write "10115"`},
			{"code", "12345", `code is text in pickups (CHAR): write "12345"`},
			{"note", "true", `note is text in pickups (TEXT): write "true"`},
			{"grams", `"850"`, `grams is an integer in pickups (INT): write 850`},
			{"grams", "850.0", `grams is an integer in pickups (INT): write 850`},
			{"grams", "850.5", `grams is an integer in pickups (INT): write an integer, not 850.5`},
			{"label", "3000000000.0", `label is an integer in pickups (UNSIGNED BIGINT): write 3000000000`},
			{"built", `"2026"`, `built is an integer in pickups (YEAR): write 2026`},
			{"tries", "true", `tries is an integer in pickups (SMALLINT): write an integer, not true`},
			{"kilos", "12", `kilos is a decimal in pickups (DECIMAL): write 12.0`},
			{"rate", `"1.5"`, `rate is a decimal in pickups (DOUBLE): write 1.5`},
			{"ratio", "1", `ratio is a decimal in pickups (FLOAT): write 1.0`},
			{"signed_for", `"true"`, `signed_for is an integer or a boolean in pickups (TINYINT): write true`},
			{"signed_for", "1.0", `signed_for is an integer or a boolean in pickups (TINYINT): write 1`},
			{"flags", "3.0", `flags is an integer or a boolean in pickups (UNSIGNED TINYINT): write 3`},
			{"day", "2026", `day is written as a string in pickups (DATE): write "2026"`},
			{"planned_at", "20260924", `planned_at is written as a string in pickups (DATETIME): write "20260924"`},
			{"planned_at", "2026-09-24T07:40:00+02:00", `planned_at has no time zone in pickups (DATETIME), so an offset would be dropped: write "2026-09-24 07:40:00"`},
			{"status", "1", `status is written as a string in pickups (ENUM): write "1"`},
			{"photo", "7", `written as a string in pickups (BLOB): write "7"`},
		},
		finds: [][2]string{
			{"reference", "PX-TYPES-1"},
			{"postcode", `"10115"`},
			{"code", "H91E2"},
			{"grams", "850"},
			{"label", "3000000000"},
			{"built", "2026"},
			{"kilos", "12.5"},
			{"rate", "1.5"},
			{"ratio", "0.5"},
			{"signed_for", "true"},
			{"signed_for", "1"},
			{"flags", "3"},
			{"seal", "true"},
			{"seal", "1"},
			{"day", "2026-09-24"},
			{"planned_at", "2026-09-24 07:40:00"},
			{"status", "PLANNED"},
			{"returned", "false"},
			{"returned", "0"},
			{"seal_off", "false"},
			{"seal_off", "0"},
			{"thin", "0.1"},
		},
		misses: [][2]string{{"grams", "851"}, {"signed_for", "false"}, {"reference", `"0"`}, {"returned", "true"}, {"seal_off", "1"}},
	}
}

func TestColumnTypesMySQL(t *testing.T) {
	host, port := container(t, "mysql:8.4", "3306",
		map[string]string{"MYSQL_ROOT_PASSWORD": "secret", "MYSQL_DATABASE": "parcels", "MYSQL_USER": "space", "MYSQL_PASSWORD": "secret"},
		"port: 3306  MySQL Community Server", 1)
	url := fmt.Sprintf("jdbc:mysql://%s:%s/parcels", host, port)
	tgt, _ := Resolve(url, "space", "secret")
	exec(t, "mysql", tgt.DSN, mysqlSchema...)
	h := newHarness(t)
	h.db(url)
	mysqlCases().run(t, h, true)
}

func TestColumnTypesMariaDB(t *testing.T) {
	host, port := container(t, "mariadb:11.8", "3306",
		map[string]string{"MARIADB_ROOT_PASSWORD": "secret", "MARIADB_DATABASE": "parcels", "MARIADB_USER": "space", "MARIADB_PASSWORD": "secret", "TZ": databaseZone},
		"port: 3306  mariadb.org binary distribution", 1)
	url := fmt.Sprintf("jdbc:mariadb://%s:%s/parcels", host, port)
	tgt, _ := Resolve(url, "space", "secret")
	exec(t, "mysql", tgt.DSN, mysqlSchema...)
	h := newHarness(t)
	h.db(url)
	mysqlCases().run(t, h, true)
	// MariaDB's JSON is its LONGTEXT, so the column holds text and is written as text.
	cells := mysqlCells()
	cells.passes = append(cells.passes, [2]string{"details", `"{"zone":"DE-1"}"`})
	cells.fails = append(cells.fails, [3]string{"details", `{"zone":"DE-1"}`, `write '"{"zone":"DE-1"}"'`})
	cells.run(t, h)
	mysqlClockAndLoad(t, h, tgt)
}

func TestColumnTypesSQLServer(t *testing.T) {
	host, port := container(t, "mcr.microsoft.com/azure-sql-edge:latest", "1433",
		map[string]string{"ACCEPT_EULA": "Y", "MSSQL_SA_PASSWORD": "Parcels-Secret-1"},
		"SQL Server is now ready for client connections", 1)
	master := fmt.Sprintf("sqlserver://sa:Parcels-Secret-1@%s:%s?encrypt=disable", host, port)
	exec(t, "sqlserver", master, "CREATE DATABASE parcels")
	url := fmt.Sprintf("sqlserver://%s:%s?database=parcels&encrypt=disable", host, port)
	tgt, err := Resolve(url, "sa", "Parcels-Secret-1")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "sqlserver", tgt.DSN,
		`CREATE TABLE dbo.pickups (
			reference VARCHAR(40) PRIMARY KEY, postcode NVARCHAR(10), code CHAR(5), shop NCHAR(10), grams INT, label BIGINT, tries SMALLINT, age TINYINT,
			kilos DECIMAL(10,2), rate FLOAT, ratio REAL, fee MONEY, signed_for BIT, day DATE, planned_at DATETIME2, token UNIQUEIDENTIFIER, thin REAL)`,
		`INSERT INTO dbo.pickups VALUES ('PX-TYPES-1', N'10115', 'H91E2', N'willow', 850, 3000000000, 2, 7, 12.50, 1.5, 0.5, 12.50, 1, '2026-09-24',
			'2026-09-24T07:40:00', '6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11', 0.1)`,
	)
	h := newHarness(t)
	h.ok("a space-db database with the following properties:", []string{"url", url}, []string{"user", "sa"}, []string{"password", "Parcels-Secret-1"})
	columnCases{
		table: "dbo.pickups",
		fails: [][3]string{
			{"reference", "0", `reference is text in dbo.pickups (VARCHAR): write "0"`},
			{"postcode", "10115", `postcode is text in dbo.pickups (NVARCHAR): write "10115"`},
			{"code", "12345", `code is text in dbo.pickups (CHAR): write "12345"`},
			{"shop", "true", `shop is text in dbo.pickups (NCHAR): write "true"`},
			{"grams", `"850"`, `grams is an integer in dbo.pickups (INT): write 850`},
			{"grams", "850.0", `grams is an integer in dbo.pickups (INT): write 850`},
			{"label", "3000000000.0", `label is an integer in dbo.pickups (BIGINT): write 3000000000`},
			{"tries", "true", `tries is an integer in dbo.pickups (SMALLINT): write an integer, not true`},
			{"age", "7.0", `age is an integer in dbo.pickups (TINYINT): write 7`},
			{"kilos", "12", `kilos is a decimal in dbo.pickups (DECIMAL): write 12.0`},
			{"rate", "2", `rate is a decimal in dbo.pickups (FLOAT): write 2.0`},
			{"ratio", "1", `ratio is a decimal in dbo.pickups (REAL): write 1.0`},
			{"fee", "12", `fee is a decimal in dbo.pickups (MONEY): write 12.0`},
			{"signed_for", "1", `signed_for is a boolean in dbo.pickups (BIT): write true`},
			{"signed_for", `"true"`, `signed_for is a boolean in dbo.pickups (BIT): write true`},
			{"day", "2026", `day is written as a string in dbo.pickups (DATE): write "2026"`},
			{"planned_at", "2026-09-24T07:40:00+02:00", `planned_at has no time zone in dbo.pickups (DATETIME2), so an offset would be dropped: write "2026-09-24 07:40:00"`},
			{"token", "7", `token is written as a string in dbo.pickups (UNIQUEIDENTIFIER): write "7"`},
		},
		finds: [][2]string{
			{"reference", "PX-TYPES-1"},
			{"postcode", `"10115"`},
			{"code", "H91E2"},
			{"grams", "850"},
			{"label", "3000000000"},
			{"age", "7"},
			{"kilos", "12.5"},
			{"rate", "1.5"},
			{"ratio", "0.5"},
			{"thin", "0.1"},
			{"fee", "12.50"},
			{"signed_for", "true"},
			{"day", "2026-09-24"},
			{"token", "6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11"},
		},
		misses: [][2]string{{"grams", "851"}, {"signed_for", "false"}},
	}.run(t, h, true)
}

// identitySeed is a seed whose rows name the identity column, and one that does not.
const identitySeed = `parcels:
  - id: 7001
    reference: PX-ID-7001
  - id: 7002
    reference: PX-ID-7002
  - reference: PX-ID-GENERATED
`

// A seed's row sets an identity column, whatever the database calls it, and a row
// of the same seed that leaves it out has it generated.
func seedsIdentity(t *testing.T, h *harness) {
	t.Helper()
	h.file("seed.yaml", identitySeed)
	h.ok("a seed.yaml db seed")
	h.ok("a selection of rows is retrieved from the parcels table where:", []string{"id", "7002"}, []string{"reference", "PX-ID-7002"})
	h.ok("the selection has 1 row")
	h.ok("a 2nd selection of rows is retrieved from the parcels table where:", []string{"reference", "PX-ID-GENERATED"})
	h.ok("the 2nd selection has 1 row")
}

func TestSeedSetsIdentityPostgres(t *testing.T) {
	url := postgresURL(t)
	tgt, err := Resolve(url, "space", "secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, generated := range []string{"ALWAYS", "BY DEFAULT"} {
		t.Run(generated, func(t *testing.T) {
			exec(t, "pgx", tgt.DSN,
				"DROP TABLE IF EXISTS parcels",
				"CREATE TABLE parcels (id bigint GENERATED "+generated+" AS IDENTITY PRIMARY KEY, reference text)")
			h := newHarness(t)
			h.db(url)
			seedsIdentity(t, h)
		})
	}
	exec(t, "pgx", tgt.DSN, "DROP TABLE parcels")
}

func TestSeedSetsIdentitySQLServer(t *testing.T) {
	host, port := container(t, "mcr.microsoft.com/azure-sql-edge:latest", "1433",
		map[string]string{"ACCEPT_EULA": "Y", "MSSQL_SA_PASSWORD": "Parcels-Secret-1"},
		"SQL Server is now ready for client connections", 1)
	exec(t, "sqlserver", fmt.Sprintf("sqlserver://sa:Parcels-Secret-1@%s:%s?encrypt=disable", host, port), "CREATE DATABASE parcels")
	url := fmt.Sprintf("sqlserver://%s:%s?database=parcels&encrypt=disable", host, port)
	tgt, err := Resolve(url, "sa", "Parcels-Secret-1")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "sqlserver", tgt.DSN,
		"CREATE TABLE parcels (id BIGINT IDENTITY(1,1) PRIMARY KEY, reference NVARCHAR(40))",
		"CREATE TABLE depots (id INT IDENTITY(1,1) PRIMARY KEY, code NVARCHAR(10))")
	h := newHarness(t)
	h.ok("a space-db database with the following properties:", []string{"url", url}, []string{"user", "sa"}, []string{"password", "Parcels-Secret-1"})
	seedsIdentity(t, h)

	// A second table's identity in the same seed, as a session has it on for one
	// table at a time; a row refused leaves it off on the connection.
	h.file("two.yaml", "depots:\n  - id: 41\n    code: BER-1\nparcels:\n  - id: 7100\n    reference: PX-ID-7100\n")
	h.ok("a two.yaml db seed")
	h.file("dup.yaml", "parcels:\n  - id: 7100\n    reference: PX-ID-7100\n")
	if err := h.step("a dup.yaml db seed"); err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("a duplicate id: %v", err)
	}
	h.file("generated.yaml", "parcels:\n  - reference: PX-ID-AFTER\n")
	for range 40 { // through every connection of the pool
		h.ok("a generated.yaml db seed")
	}
}

func TestSeedSetsAutoIncrementMySQL(t *testing.T) {
	host, port := container(t, "mysql:8.4", "3306",
		map[string]string{"MYSQL_ROOT_PASSWORD": "secret", "MYSQL_DATABASE": "parcels", "MYSQL_USER": "space", "MYSQL_PASSWORD": "secret"},
		"port: 3306  MySQL Community Server", 1)
	url := fmt.Sprintf("jdbc:mysql://%s:%s/parcels", host, port)
	tgt, _ := Resolve(url, "space", "secret")
	exec(t, "mysql", tgt.DSN, "CREATE TABLE parcels (id BIGINT AUTO_INCREMENT PRIMARY KEY, reference VARCHAR(40))")
	h := newHarness(t)
	h.db(url)
	seedsIdentity(t, h)
}

func TestSeedSetsAutoIncrementSQLite(t *testing.T) {
	path := t.TempDir() + "/parcels.db"
	exec(t, "sqlite", path, "CREATE TABLE parcels (id INTEGER PRIMARY KEY AUTOINCREMENT, reference TEXT)")
	h := newHarness(t)
	h.db("sqlite:" + path)
	seedsIdentity(t, h)
}

// cellCases are a row's values, of a table with a PX-TYPES-1: what a check of a column
// passes with, and what it fails with and says.
type cellCases struct {
	table  string
	passes [][2]string // column, value
	fails  [][3]string // column, value, what the failure says
}

func (c cellCases) run(t *testing.T, h *harness) {
	t.Helper()
	h.ok(fmt.Sprintf("a selection of rows is retrieved from the %s table where:", c.table), []string{"reference", "PX-TYPES-1"})
	selection := nth(len(mustService(t, h).Selections()))
	check := func(column, value string) error {
		quote := "'"
		if strings.Contains(value, "'") {
			quote = `"`
		}
		return h.step(fmt.Sprintf("the 1st row %s column of the %s selection is %s%s%s", column, selection, quote, value, quote))
	}
	for _, p := range c.passes {
		if err := check(p[0], p[1]); err != nil {
			t.Errorf("%s is %s: %v", p[0], p[1], err)
		}
	}
	for _, f := range c.fails {
		err := check(f[0], f[1])
		if err == nil || !strings.Contains(err.Error(), f[2]) {
			t.Errorf("%s is %s: %v, want a failure with %s", f[0], f[1], err, f[2])
		}
	}
	if err := check("no_such_column", "1"); err == nil || !strings.Contains(err.Error(), "columns:") {
		t.Errorf("a column the selection does not have: %v", err)
	}
}

func TestColumnChecksPostgres(t *testing.T) {
	url := postgresURL(t)
	tgt, _ := Resolve(url, "space", "secret")
	exec(t, "pgx", tgt.DSN,
		"CREATE EXTENSION IF NOT EXISTS citext",
		`CREATE TABLE cells (
			reference varchar(40) primary key, postcode text, code char(8), shop citext, grams integer, label bigint,
			kilos numeric(10,2), rate float8, signed boolean, day date, planned_at timestamptz, plain_at timestamp, token uuid, details jsonb, missing text,
			returned boolean, ratio real, fee money, photo bytea, tags text[])`,
		`INSERT INTO cells VALUES ('PX-TYPES-1', '10115', 'H91E2', 'Willow', 850, 3000000000, 12.50, 2, true, '2026-09-24',
			'2026-09-24T07:40:00Z', '2026-09-24 07:40:00', '6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11', '{"zone":"DE-1","pieces":3}', null,
			false, 0.1, 12.50, 'x', '{a,b}')`)
	h := newHarness(t)
	h.db(url)
	cellCases{
		table: "cells",
		passes: [][2]string{
			{"reference", "PX-TYPES-1"},
			{"postcode", `"10115"`},
			{"code", "H91E2"},
			{"shop", "Willow"},
			{"grams", "850"},
			{"label", "3000000000"},
			{"kilos", "12.5"},
			{"kilos", "12.50"},
			{"rate", "2.0"},
			{"signed", "true"},
			{"day", "2026-09-24"},
			{"planned_at", "2026-09-24T07:40:00Z"},
			{"planned_at", "2026-09-24T09:40:00+02:00"},
			{"plain_at", "2026-09-24 07:40:00"},
			{"token", "6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11"},
			{"details", `{"pieces":3,"zone":"DE-1"}`},
			{"missing", "null"},
			{"REFERENCE", "PX-TYPES-1"},
			{"returned", "false"},
			{"ratio", "0.1"},
			{"fee", "$12.50"},
			{"photo", "x"},
			{"tags", `"{a,b}"`},
		},
		fails: [][3]string{
			{"postcode", "10115", `postcode is the string "10115": write '"10115"'`},
			{"grams", `"850"`, `grams is the integer 850: write '850'`},
			{"grams", "851", "differs"},
			{"kilos", "12", "differs"},
			{"rate", "2", "differs"},
			{"signed", "false", "differs"},
			{"signed", `"true"`, "differs"},
			{"day", "2026-09-25", "differs"},
			{"missing", "x", "differs"},
			{"reference", "null", "differs"},
			{"returned", "true", "differs"},
			{"day", "soon", "differs"},
			{"fee", "12.50", "differs"},
			{"plain_at", "2026-09-24T07:40:00+02:00", `plain_at has no time zone (TIMESTAMP), so an offset would be dropped: write '2026-09-24 07:40:00'`},
			{"planned_at", "2026-09-24 07:40:00", `planned_at holds a moment (TIMESTAMPTZ), which a time with no offset does not say: write its offset, like '2026-09-24T07:40:00Z'`},
		},
	}.run(t, h)
	exec(t, "pgx", tgt.DSN, "DROP TABLE cells")

	// With its table gone, a selection's values are what the driver gave.
	n := nth(len(mustService(t, h).Selections()))
	for column, value := range map[string]string{"signed": "true", "returned": "false", "grams": "850", "reference": "PX-TYPES-1"} {
		if err := h.step("the 1st row " + column + " column of the " + n + " selection is '" + value + "'"); err != nil {
			t.Errorf("%s of a table since dropped: %v", column, err)
		}
	}
}

// mysqlCells are MySQL's and MariaDB's.
func mysqlCells() cellCases {
	return cellCases{
		table: "pickups",
		passes: [][2]string{
			{"reference", "PX-TYPES-1"},
			{"postcode", `"10115"`},
			{"code", "H91E2"},
			{"note", "ring twice"},
			{"grams", "850"},
			{"label", "3000000000"},
			{"tries", "2"},
			{"built", "2026"},
			{"kilos", "12.5"},
			{"rate", "1.5"},
			{"ratio", "0.5"},
			{"thin", "0.1"},
			{"signed_for", "1"},
			{"signed_for", "true"},
			{"flags", "3"},
			{"seal", "1"},
			{"seal", "true"},
			{"returned", "0"},
			{"returned", "false"},
			{"seal_off", "0"},
			{"seal_off", "false"},
			{"day", "2026-09-24"},
			{"planned_at", "2026-09-24 07:40:00"},
			{"status", "PLANNED"},
			{"photo", "x"},
		},
		fails: [][3]string{
			{"grams", `"850"`, "850"},
			{"kilos", "12", "differs"},
			{"signed_for", "false", "differs"},
			{"returned", "true", "differs"},
			{"returned", "1", "differs"},
			{"seal_off", "true", "differs"},
			{"day", "2026-09-25", "differs"},
			{"day", "soon", "differs"},
			{"planned_at", "2026-09-24T07:40:00Z", `planned_at has no time zone (DATETIME)`},
		},
	}
}

// mysqlClockAndLoad holds MySQL's and MariaDB's relative times to the database's clock,
// in databaseZone, and runs more at once than the pool has connections.
func mysqlClockAndLoad(t *testing.T, h *harness, tgt Target) {
	t.Helper()
	exec(t, "mysql", tgt.DSN,
		`CREATE TABLE clock_times (reference VARCHAR(40) PRIMARY KEY, plain_at DATETIME, moment TIMESTAMP NULL, epoch VARCHAR(20))`,
		`CREATE TABLE load_parcels (reference VARCHAR(40) PRIMARY KEY, kilos DECIMAL(10,2), at_time DATETIME)`,
	)
	db, err := dbsql.Open("mysql", tgt.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertDatabaseZone(t, db, "SELECT TIMESTAMPDIFF(MINUTE, UTC_TIMESTAMP(6), NOW(6))")
	relativeTimes(t, h, db, "clock_times", []string{"plain_at", "moment"},
		"SELECT TIMESTAMPDIFF(MINUTE, plain_at, NOW()), TIMESTAMPDIFF(MINUTE, moment, NOW()) FROM clock_times WHERE reference = 'PX-CLOCK'")
	err = h.step("a selection of rows is retrieved from the clock_times table where:", []string{"moment", "2026-09-24T07:40:00+02:00"})
	if err == nil || !strings.Contains(err.Error(), "moment has no time zone in clock_times (TIMESTAMP)") {
		t.Errorf("an offset on a TIMESTAMP: %v", err)
	}
	oneConnectionAtATime(t, h, "load_parcels", "    kilos: 1.5\n    at_time: \"[MIN,MINUS,5]\"\n", false)
	manyAtOnce(t, h, "load_parcels", "")
}

func TestColumnChecksMySQL(t *testing.T) {
	host, port := container(t, "mysql:8.4", "3306",
		map[string]string{"MYSQL_ROOT_PASSWORD": "secret", "MYSQL_DATABASE": "parcels", "MYSQL_USER": "space", "MYSQL_PASSWORD": "secret", "TZ": databaseZone},
		"port: 3306  MySQL Community Server", 1)
	url := fmt.Sprintf("jdbc:mysql://%s:%s/parcels", host, port)
	tgt, _ := Resolve(url, "space", "secret")
	exec(t, "mysql", tgt.DSN, mysqlSchema...)
	h := newHarness(t)
	h.db(url)
	cells := mysqlCells()
	cells.passes = append(cells.passes, [2]string{"details", `{"zone":"DE-1"}`})
	cells.run(t, h)
	mysqlClockAndLoad(t, h, tgt)
}

func TestColumnChecksSQLServer(t *testing.T) {
	host, port := container(t, "mcr.microsoft.com/azure-sql-edge:latest", "1433",
		map[string]string{"ACCEPT_EULA": "Y", "MSSQL_SA_PASSWORD": "Parcels-Secret-1", "TZ": databaseZone},
		"SQL Server is now ready for client connections", 1)
	exec(t, "sqlserver", fmt.Sprintf("sqlserver://sa:Parcels-Secret-1@%s:%s?encrypt=disable", host, port), "CREATE DATABASE parcels")
	url := fmt.Sprintf("sqlserver://%s:%s?database=parcels&encrypt=disable", host, port)
	tgt, err := Resolve(url, "sa", "Parcels-Secret-1")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, "sqlserver", tgt.DSN,
		`CREATE TABLE cells (reference VARCHAR(40) PRIMARY KEY, postcode NVARCHAR(10), code CHAR(8), grams INT, kilos DECIMAL(10,2), rate FLOAT, signed_for BIT, day DATE, planned_at DATETIME2, missing NVARCHAR(10),
			returned BIT, shop NCHAR(8), note NVARCHAR(MAX), label BIGINT, age TINYINT, ratio REAL, fee MONEY, token UNIQUEIDENTIFIER, zoned_at DATETIMEOFFSET, photo VARBINARY(8))`,
		`INSERT INTO cells VALUES ('PX-TYPES-1', N'10115', 'H91E2', 850, 12.50, 2, 1, '2026-09-24', '2026-09-24T07:40:00', NULL,
			0, N'willow', N'ring twice', 3000000000, 7, 0.1, 12.50, '6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11', '2026-09-24T07:40:00+02:00', 0x78)`)
	h := newHarness(t)
	h.ok("a space-db database with the following properties:", []string{"url", url}, []string{"user", "sa"}, []string{"password", "Parcels-Secret-1"})
	cellCases{
		table: "cells",
		passes: [][2]string{
			{"reference", "PX-TYPES-1"},
			{"postcode", `"10115"`},
			{"code", "H91E2"},
			{"grams", "850"},
			{"kilos", "12.5"},
			{"rate", "2.0"},
			{"signed_for", "true"},
			{"day", "2026-09-24"},
			{"planned_at", "2026-09-24 07:40:00"},
			{"missing", "null"},
			{"returned", "false"},
			{"shop", "willow"},
			{"note", "ring twice"},
			{"label", "3000000000"},
			{"age", "7"},
			{"ratio", "0.1"},
			{"fee", "12.50"},
			{"token", "6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11"},
			{"zoned_at", "2026-09-24T07:40:00+02:00"},
			{"zoned_at", "2026-09-24T05:40:00Z"},
			{"photo", "x"},
		},
		fails: [][3]string{
			{"grams", `"850"`, "850"},
			{"kilos", "12", "differs"},
			{"signed_for", "1", "differs"},
			{"missing", "x", "differs"},
			{"returned", "true", "differs"},
			{"returned", "0", "differs"},
			{"fee", "12", "differs"},
			{"zoned_at", "2026-09-24T07:40:00Z", "differs"},
			{"planned_at", "2026-09-24T07:40:00Z", `planned_at has no time zone (DATETIME2)`},
			{"zoned_at", "2026-09-24 07:40:00", `zoned_at holds a moment (DATETIMEOFFSET)`},
		},
	}.run(t, h)

	err = h.step("a selection of rows is retrieved from the cells table where:", []string{"zoned_at", "2026-09-24 07:40:00"})
	if err == nil || !strings.Contains(err.Error(), "zoned_at holds a moment in cells (DATETIMEOFFSET)") {
		t.Errorf("no offset on a DATETIMEOFFSET: %v", err)
	}
	exec(t, "sqlserver", tgt.DSN,
		`CREATE TABLE clock_times (reference VARCHAR(40) PRIMARY KEY, plain_at DATETIME2, old_at DATETIME, zoned_at DATETIMEOFFSET, epoch VARCHAR(20))`,
		`CREATE TABLE load_parcels (id BIGINT IDENTITY PRIMARY KEY, reference VARCHAR(40), kilos DECIMAL(10,2), at_time DATETIME2)`,
	)
	db, err := dbsql.Open("sqlserver", tgt.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	assertDatabaseZone(t, db, "SELECT DATEPART(TZOFFSET, SYSDATETIMEOFFSET())")
	relativeTimes(t, h, db, "clock_times", []string{"plain_at", "old_at", "zoned_at"},
		`SELECT DATEDIFF(MINUTE, plain_at, SYSDATETIME()), DATEDIFF(MINUTE, old_at, SYSDATETIME()), DATEDIFF(MINUTE, zoned_at, SYSDATETIMEOFFSET())
			FROM clock_times WHERE reference = 'PX-CLOCK'`)
	// Each names the identity column: IDENTITY_INSERT, in the transaction.
	oneConnectionAtATime(t, h, "load_parcels", "    id: 8000\n    kilos: 1.5\n    at_time: \"[MIN,MINUS,5]\"\n", false)
	manyAtOnce(t, h, "load_parcels", "id")
}

func TestColumnChecksSQLite(t *testing.T) {
	path := t.TempDir() + "/cells.db"
	exec(t, "sqlite", path,
		"CREATE TABLE cells (reference TEXT PRIMARY KEY, grams INTEGER, kilos REAL, signed BOOLEAN, anything, missing TEXT, returned BOOLEAN)",
		"INSERT INTO cells VALUES ('PX-TYPES-1', 850, 12.5, 1, 7, NULL, 0)")
	h := newHarness(t)
	h.db("sqlite:" + path)
	cellCases{
		table: "cells",
		passes: [][2]string{
			{"reference", "PX-TYPES-1"}, {"grams", "850"}, {"kilos", "12.5"}, {"signed", "true"}, {"anything", "7"}, {"missing", "null"}, {"returned", "false"},
		},
		fails: [][3]string{{"grams", `"850"`, "850"}, {"kilos", "12", "differs"}, {"anything", "7.0", "differs"}, {"returned", "true", "differs"}},
	}.run(t, h)
}
