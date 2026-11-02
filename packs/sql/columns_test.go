package sql

import (
	"context"
	dbsql "database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/columntype"
)

// Each dialect's kinds, of the names its driver gives: pgx's (its type names,
// or what columnTypes looks up), go-sql-driver's, go-mssqldb's, and what an
// SQLite table declares.
func TestColumnKind(t *testing.T) {
	const (
		i = columntype.Integer
		d = columntype.Decimal
		b = columntype.Boolean
		x = columntype.Text
		o = columntype.Other
		f = columntype.IntegerOrBoolean
	)
	for dialect, kinds := range map[string]map[columntype.Kind][]string{
		postgres.Name: {
			i: {"INT2", "INT4", "INT8", "OID"},
			d: {"NUMERIC", "FLOAT4", "FLOAT8"},
			b: {"BOOL"},
			x: {"TEXT", "VARCHAR", "BPCHAR", "NAME", "CITEXT"},
			o: {"DATE", "TIME", "TIMESTAMP", "TIMESTAMPTZ", "INTERVAL", "UUID", "JSON", "JSONB", "BYTEA", "BIT", "VARBIT", "_TEXT", "_INT4", "PARCEL_STATUS", "MONEY"},
		},
		mysql.Name: {
			i: {"INT", "UNSIGNED INT", "BIGINT", "UNSIGNED BIGINT", "SMALLINT", "UNSIGNED SMALLINT", "MEDIUMINT", "UNSIGNED MEDIUMINT", "YEAR"},
			d: {"DECIMAL", "FLOAT", "DOUBLE"},
			x: {"CHAR", "VARCHAR", "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT"},
			f: {"TINYINT", "UNSIGNED TINYINT", "BIT"},
			o: {"DATE", "DATETIME", "TIMESTAMP", "TIME", "JSON", "ENUM", "SET", "BLOB", "BINARY", "VARBINARY", "GEOMETRY", "VECTOR"},
		},
		sqlserver.Name: {
			i: {"INT", "BIGINT", "SMALLINT", "TINYINT"},
			d: {"DECIMAL", "FLOAT", "REAL", "MONEY", "SMALLMONEY"},
			b: {"BIT"},
			x: {"CHAR", "VARCHAR", "NCHAR", "NVARCHAR", "TEXT", "NTEXT"},
			o: {"DATE", "DATETIME", "DATETIME2", "SMALLDATETIME", "DATETIMEOFFSET", "TIME", "UNIQUEIDENTIFIER", "BINARY", "VARBINARY", "IMAGE", "XML", "SQL_VARIANT"},
		},
		sqlite.Name: {
			i: {"INT", "INTEGER", "TINYINT", "TINYINT(1)", "SMALLINT", "MEDIUMINT", "BIGINT", "UNSIGNED BIG INT", "INT2", "INT8", "SERIAL", "BIGSERIAL", "INT UNSIGNED", "integer"},
			d: {"NUMERIC", "DECIMAL(10,2)", "DECIMAL(10, 2)", "FLOAT", "DOUBLE", "DOUBLE PRECISION", "REAL", "MONEY"},
			b: {"BOOL", "BOOLEAN"},
			x: {"TEXT", "VARCHAR(20)", "CHARACTER VARYING(255)", "NVARCHAR(100)", "NCHAR(55)", "NATIVE CHARACTER(70)", "VARYING CHARACTER(255)", "CLOB", "CITEXT", "text"},
			o: {"DATE", "DATETIME", "TIMESTAMP", "UUID", "JSON", "BLOB", "STRING"},
		},
	} {
		d := dialectNamed(t, dialect)
		for want, names := range kinds {
			for _, name := range names {
				got, known := d.columnKind(name)
				if !known || got != want {
					t.Errorf("%s %s: kind %v (known %v), want %v", dialect, name, got, known, want)
				}
			}
		}
	}
	if _, known := sqlite.columnKind(""); known {
		t.Error("an SQLite column with no declared type should have no kind")
	}
	if _, known := sqlite.columnKind("ANY"); known {
		t.Error("an SQLite column of ANY keeps what it is given, and should have no kind")
	}
}

func dialectNamed(t *testing.T, name string) Dialect {
	t.Helper()
	for _, d := range []Dialect{postgres, mysql, sqlite, sqlserver} {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no dialect %s", name)
	return Dialect{}
}

// sqliteDB is a harness with a database of parcels' tables: every type a
// scenario's tables declare, and columns with no declared type.
func sqliteDB(t *testing.T) *cloudtest.Harness {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parcels.db")
	db, err := dbsql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE pickups (reference VARCHAR(40) PRIMARY KEY, postcode TEXT, grams INTEGER, kilos DECIMAL(10,2), rate REAL,
			signed BOOLEAN, day DATE, details JSON, note);
		INSERT INTO pickups VALUES ('PX-1', '10115', 850, 12.5, 1.5, TRUE, '2026-09-24', '{"zone":"DE-1"}', 850);
		INSERT INTO pickups VALUES ('PX-2', '20095', 900, 2.0, 2.0, FALSE, '2026-09-25', '{}', 850.0);
		INSERT INTO pickups VALUES ('PX-3', 'H91 E2K3', 950, 3.0, 3.0, FALSE, NULL, '{}', '850');
		INSERT INTO pickups VALUES ('PX-4', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL);
		INSERT INTO pickups VALUES ('PX-5', 'ROW5', 1, 1, 1, TRUE, NULL, NULL, 1);
	`); err != nil {
		t.Fatal(err)
	}
	h := cloudtest.New(t, Pack())
	h.OK("a parcels-db database with the following properties:", [][]string{{"url", "sqlite:" + path}, {"user", "u"}, {"password", "p"}})
	return h
}

const (
	selectRows = "a selection of rows is retrieved from the pickups table where:"
	pollRows   = "within 30s a selection of at least 1 row is retrieved from the pickups table where:"
)

// What a value of another type does on SQLite, which would convert it: the
// reviewer's cases, each of a declared type.
func TestAValueOfAnotherTypeFailsOnSQLite(t *testing.T) {
	h := sqliteDB(t)
	for _, c := range []struct{ column, cell, want string }{
		{"postcode", "10115", `postcode is text in pickups (TEXT): write "10115"`},
		{"reference", "1", `reference is text in pickups (VARCHAR(40)): write "1"`},
		{"postcode", "true", `postcode is text in pickups (TEXT): write "true"`},
		{"grams", `"850"`, `grams is an integer in pickups (INTEGER): write 850`},
		{"grams", "850.0", `grams is an integer in pickups (INTEGER): write 850`},
		{"grams", "850.5", `grams is an integer in pickups (INTEGER): write an integer, not 850.5`},
		{"grams", "heavy", `grams is an integer in pickups (INTEGER): write an integer, not heavy`},
		{"grams", "true", `grams is an integer in pickups (INTEGER): write an integer, not true`},
		{"kilos", "12", `kilos is a decimal in pickups (DECIMAL(10,2)): write 12.0`},
		{"kilos", `"12.5"`, `kilos is a decimal in pickups (DECIMAL(10,2)): write 12.5`},
		{"rate", "2", `rate is a decimal in pickups (REAL): write 2.0`},
		{"signed", "1", `signed is a boolean in pickups (BOOLEAN): write true`},
		{"signed", `"false"`, `signed is a boolean in pickups (BOOLEAN): write false`},
		{"day", "2026", `day is written as a string in pickups (DATE): write "2026"`},
	} {
		err := h.Fails(selectRows, c.want, [][]string{{c.column, c.cell}})
		var ve *core.ValueError
		if !errors.As(err, &ve) {
			t.Errorf("%s %s: %v is not a value error, as the rule's are", c.column, c.cell, err)
		}
		if !strings.HasSuffix(err.Error(), "[AXX-E0207]") {
			t.Errorf("%s %s: %v should end with the code", c.column, c.cell, err)
		}
	}
	// Every condition that is wrong, at once.
	_ = h.Fails(selectRows, `postcode is text in pickups (TEXT): write "10115"; grams is an integer in pickups (INTEGER): write 850 [AXX-E0207]`,
		[][]string{{"postcode", "10115"}, {"grams", "850.0"}})
}

func TestAValueOfTheColumnsTypeSelects(t *testing.T) {
	h := sqliteDB(t)
	for i, c := range []struct {
		rows [][]string
		want int
	}{
		{[][]string{{"postcode", `"10115"`}}, 1},
		{[][]string{{"grams", "850"}}, 1},
		{[][]string{{"grams", "850L"}}, 1},
		{[][]string{{"kilos", "12.5"}, {"rate", "1.5"}}, 1},
		{[][]string{{"rate", "2.0"}}, 1},
		{[][]string{{"signed", "true"}}, 2},
		{[][]string{{"signed", "false"}}, 2},
		{[][]string{{"day", "2026-09-24"}}, 1},
		{[][]string{{"day", `"2026-09-24"`}}, 1},
		{[][]string{{"postcode", "null"}, {"grams", "null"}}, 1},
		{[][]string{{"details", `{"zone":"DE-1"}`}}, 1},
		{[][]string{{"pickups.grams", "850"}}, 1},
		{[][]string{{"GRAMS", "850"}}, 1},
	} {
		h.OK(selectRows, c.rows)
		h.OK(fmt.Sprintf("the %d%s selection has %d rows", i+1, ordinal(i+1), c.want))
	}
}

func ordinal(n int) string {
	switch {
	case n%100 >= 11 && n%100 <= 13:
		return "th"
	case n%10 == 1:
		return "st"
	case n%10 == 2:
		return "nd"
	case n%10 == 3:
		return "rd"
	}
	return "th"
}

// A column with no declared type gives no answer before the query: the rows the
// database selected are held to what each one stores.
func TestAColumnWithNoDeclaredTypeIsHeldToWhatItsRowsStore(t *testing.T) {
	h := sqliteDB(t)
	h.OK(selectRows, [][]string{{"note", "850"}})
	h.OK("the selection has 1 row")
	h.OK(selectRows, [][]string{{"note", "850.0"}})
	h.OK("the 2nd selection has 1 row")
	h.OK(selectRows, [][]string{{"note", `"850"`}})
	h.OK("the 3rd selection has 1 row")
	h.OK(selectRows, [][]string{{"note", "null"}, {"reference", "PX-4"}})
	h.OK("the 4th selection has 1 row")
	// The boolean of a database without booleans is an integer.
	h.OK(selectRows, [][]string{{"note", "1"}})
	h.OK("the 5th selection has 1 row")
	h.OK(selectRows, [][]string{{"note", "true"}})
	h.OK("the 6th selection has 0 rows")
	// A row dropped for its type says so in the check that misses it.
	h.OK(selectRows, [][]string{{"reference", "PX-2"}, {"note", "850"}})
	_ = h.Fails("the 7th selection has 1 row", "note is the decimal 850.0: write 850.0", nil)
	err := h.Fails("within 1s a selection of at least 1 row is retrieved from the pickups table where:", "note is the decimal 850.0: write 850.0",
		[][]string{{"reference", "PX-2"}, {"note", "850"}})
	if !core.IsAssertion(err) {
		t.Errorf("a poll that finds too few rows is a failed assertion: %v", err)
	}
}

func TestStoredJSON(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{{int64(850), "850"}, {850.0, "850.0"}, {850.5, "850.5"}, {"850", `"850"`}, {"a\"b", `"a\"b"`}} {
		if got := storedJSON(c.in); got != c.want {
			t.Errorf("%v: %s, want %s", c.in, got, c.want)
		}
	}
}

// A poll fails on a value of another type at once, as no wait can mend it.
func TestAPollFailsOnAValueOfAnotherTypeAtOnce(t *testing.T) {
	h := sqliteDB(t)
	start := time.Now()
	_ = h.Fails(pollRows, `postcode is text in pickups (TEXT): write "10115"`, [][]string{{"postcode", "10115"}})
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the poll took %s", took)
	}
}

// A column or a table it does not have is the database's to report, as it was.
func TestWhatIsNotInTheTableKeepsItsError(t *testing.T) {
	h := sqliteDB(t)
	_ = h.Fails(selectRows, "no such column: nosuch", [][]string{{"nosuch", "850"}})
	_ = h.Fails("a selection of rows is retrieved from the missing table where:", "no such table: missing", [][]string{{"grams", "850"}})
	_ = h.Fails("within 1s a selection of at least 1 row is retrieved from the pickups table where:", "no such column: nosuch", [][]string{{"nosuch", "850"}})
}

// A poll on a table the service has yet to create waits for it, and then holds
// its values to its columns.
func TestAPollHoldsATableMadeWhileItWaits(t *testing.T) {
	h := sqliteDB(t)
	svc, err := Context(h.SC).Service("parcels-db")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = svc.DB.ExecContext(context.Background(), "CREATE TABLE late (postcode TEXT)")
	}()
	_ = h.Fails("within 30s a selection of at least 1 row is retrieved from the late table where:", `postcode is text in late (TEXT): write "10115"`,
		[][]string{{"postcode", "10115"}})
}

// The type of a column is the database's each time: a scenario's service may
// change it as the scenario runs.
func TestAColumnsTypeIsReadEachTime(t *testing.T) {
	h := sqliteDB(t)
	svc, err := Context(h.SC).Service("parcels-db")
	if err != nil {
		t.Fatal(err)
	}
	h.OK(selectRows, [][]string{{"postcode", `"10115"`}})
	if _, err := svc.DB.ExecContext(context.Background(), "ALTER TABLE pickups RENAME COLUMN postcode TO old; ALTER TABLE pickups ADD COLUMN postcode INTEGER"); err != nil {
		t.Fatal(err)
	}
	_ = h.Fails(selectRows, "postcode is an integer in pickups (INTEGER): write 10115", [][]string{{"postcode", `"10115"`}})
}

func TestTypeMismatchHint(t *testing.T) {
	if err := typeMismatch(fmt.Errorf("query: %w", pgError{"42883"})); !strings.Contains(err.Error(), `a text column's number is in double quotes ("10115")`) {
		t.Errorf("an operator a value's type lacks should say what to write: %v", err)
	}
	if err := typeMismatch(pgError{"42P01"}); strings.Contains(err.Error(), "double quotes") {
		t.Errorf("another error should be as it is: %v", err)
	}
}
