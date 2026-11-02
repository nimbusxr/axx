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
		h.ok(sel, []string{f[0], f[1]})
		if got := selected(); got != 1 {
			t.Errorf("%s %s selected %d rows, not 1", f[0], f[1], got)
		}
	}
	for _, f := range c.misses {
		h.ok(sel, []string{f[0], f[1]})
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
			status parcels.pickup_status, fee money, details jsonb, photo bytea);
		INSERT INTO parcels.pickups VALUES ('PX-TYPES-1', '10115', 'H91E2', 'Willow', 850, 3000000000, 2, 12.50, 1.5, 0.5, true, '2026-09-24',
			'2026-09-24T07:40:00Z', '6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11', 'PLANNED', 12.50, '{"zone":"DE-1","pieces":3,"kilos":3.0}', 'x');
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
		status ENUM('PLANNED','DONE'), details JSON, photo BLOB)`,
	`INSERT INTO pickups VALUES ('PX-TYPES-1', '10115', 'H91E2', 'ring twice', 850, 3000000000, 2, 2026, 12.50, 1.5, 0.5, TRUE, 3, b'1',
		'2026-09-24', '2026-09-24 07:40:00', 'PLANNED', '{"zone":"DE-1"}', 'x')`,
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
		},
		misses: [][2]string{{"grams", "851"}, {"signed_for", "false"}, {"reference", `"0"`}},
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
		map[string]string{"MARIADB_ROOT_PASSWORD": "secret", "MARIADB_DATABASE": "parcels", "MARIADB_USER": "space", "MARIADB_PASSWORD": "secret"},
		"port: 3306  mariadb.org binary distribution", 1)
	url := fmt.Sprintf("jdbc:mariadb://%s:%s/parcels", host, port)
	tgt, _ := Resolve(url, "space", "secret")
	exec(t, "mysql", tgt.DSN, mysqlSchema...)
	h := newHarness(t)
	h.db(url)
	mysqlCases().run(t, h, true)
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
			kilos DECIMAL(10,2), rate FLOAT, ratio REAL, fee MONEY, signed_for BIT, day DATE, planned_at DATETIME2, token UNIQUEIDENTIFIER)`,
		`INSERT INTO dbo.pickups VALUES ('PX-TYPES-1', N'10115', 'H91E2', N'willow', 850, 3000000000, 2, 7, 12.50, 1.5, 0.5, 12.50, 1, '2026-09-24',
			'2026-09-24T07:40:00', '6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11')`,
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
			{"fee", "12.50"},
			{"signed_for", "true"},
			{"day", "2026-09-24"},
			{"token", "6f1c3a52-0d0e-4a57-9a1f-2d8c1b0f7e11"},
		},
		misses: [][2]string{{"grams", "851"}, {"signed_for", "false"}},
	}.run(t, h, true)
}
