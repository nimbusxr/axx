package sql

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		url, dialect, dsnPart string
	}{
		{"jdbc:postgresql://localhost:5432/space_explorer", "postgresql", "postgres://u:p@localhost:5432/space_explorer?default_query_exec_mode=describe_exec&sslmode=disable"},
		{"postgres://h:1/db?default_query_exec_mode=exec", "postgresql", "default_query_exec_mode=exec&"},
		{"jdbc:postgresql://db/app?currentSchema=space", "postgresql", "search_path=space"},
		{"postgres://h:1/db?sslmode=require", "postgresql", "sslmode=require"},
		{"jdbc:mysql://localhost:3306/app", "mysql", "u:p@tcp(localhost:3306)/app?"},
		{"jdbc:mariadb://m/app", "mysql", "tcp(m:3306)/app"},
		{"jdbc:sqlserver://sql:1433;databaseName=app;encrypt=false", "sqlserver", "database=app"},
		{"jdbc:sqlite:/tmp/x.db", "sqlite", "/tmp/x.db"},
	}
	for _, c := range cases {
		tgt, err := Resolve(c.url, "u", "p")
		if err != nil {
			t.Errorf("%s: %v", c.url, err)
			continue
		}
		if tgt.Dialect.Name != c.dialect || !strings.Contains(tgt.DSN, c.dsnPart) {
			t.Errorf("%s: %s %q (want %s containing %q)", c.url, tgt.Dialect.Name, tgt.DSN, c.dialect, c.dsnPart)
		}
	}
	tgt, _ := Resolve("jdbc:postgresql://localhost:15432/space?ssl=false", "u", "p")
	if tgt.Port != "15432" || tgt.Database != "space" {
		t.Errorf("postgres target: %+v", tgt)
	}
	if _, err := Resolve("jdbc:oracle:thin:@x", "u", "p"); err == nil {
		t.Error("unsupported URL must error")
	}
}

func TestLiteralAndIdent(t *testing.T) {
	if got := postgres.Literal("it's"); got != "'it''s'" {
		t.Errorf("postgres literal %s", got)
	}
	if got := mysql.Literal(`a\'b`); got != `'a\\''b'` {
		t.Errorf("mysql literal %s", got)
	}
	for _, ok := range []string{"missions", "space.missions", "a_b$1", "db.space.missions"} {
		if _, err := Ident(ok); err != nil {
			t.Errorf("%s should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"missions; drop table x", "a b", "1abc", "a.b.c.d", ""} {
		if _, err := Ident(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestWhereClause(t *testing.T) {
	got, err := whereClause(postgres, &core.Table{Rows: [][]string{{"status", "planned"}, {"note", "null"}, {"name", "O'Hara"}}}, "NEW.")
	if err != nil {
		t.Fatal(err)
	}
	want := "NEW.status = 'planned' AND NEW.note IS NULL AND NEW.name = 'O''Hara'"
	if got != want {
		t.Fatalf("got %s", got)
	}
	if _, err := whereClause(postgres, &core.Table{}, ""); err == nil || !strings.Contains(err.Error(), "No conditions provided") {
		t.Errorf("empty table: %v", err)
	}
}

// A where value means what it says (ADR 0014): a string is a SQL string, a
// number a number and a boolean a boolean, so a text column's number is in
// double quotes.
func TestWhereClauseTypedValues(t *testing.T) {
	rows := [][]string{
		{"reference", "PX-1"},
		{"postcode", `"10115"`},
		{"weight_grams", "850"},
		{"price", "1.50"},
		{"parcel_id", "3000000000L"},
		{"signed", "true"},
		{"express", "false"},
		{"quote", `"it's"`},
		{"details", `{"zone": "DE-1"}`},
		{"note", `"null"`},
	}
	for d, want := range map[Dialect]string{
		postgres: "reference = 'PX-1' AND postcode = '10115' AND weight_grams = 850 AND price = 1.50 AND parcel_id = 3000000000 AND " +
			`signed = TRUE AND express = FALSE AND quote = 'it''s' AND details = '{"zone": "DE-1"}' AND note = 'null'`,
		sqlserver: "reference = 'PX-1' AND postcode = '10115' AND weight_grams = 850 AND price = 1.50 AND parcel_id = 3000000000 AND " +
			`signed = 1 AND express = 0 AND quote = 'it''s' AND details = '{"zone": "DE-1"}' AND note = 'null'`,
	} {
		got, err := whereClause(d, &core.Table{Rows: rows}, "")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s:\ngot  %s\nwant %s", d.Name, got, want)
		}
	}
	for _, c := range []struct{ value, want string }{
		{"", `reference: the cell is empty: write "" for the empty string, or null for null`},
		{"undefined", "reference: undefined (no such property) is not a value here"},
		{"01067", `reference: 01067 could be the number 1067 or the text 01067: write 1067 for the number, or "01067" for the text`},
	} {
		_, err := whereClause(postgres, &core.Table{Rows: [][]string{{"reference", c.value}}}, "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %s", c.value, err, c.want)
		}
	}
}

type pgError struct{ code string }

func (e pgError) Error() string    { return "operator does not exist: text = integer" }
func (e pgError) SQLState() string { return e.code }

func TestContainmentJSON(t *testing.T) {
	got, err := containmentJSON([]core.Pair{
		{Key: "priority", Value: "high"},
		{Key: "crew.lead", Value: "Ada"},
		{Key: "crew.size", Value: "3"},
		{Key: "crew.code", Value: `"3"`},
		{Key: "fuel", Value: "0.75"},
		{Key: "manned", Value: "true"},
		{Key: "tags", Value: `["a", "b"]`},
		{Key: "note", Value: "null"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"priority":"high","crew":{"lead":"Ada","size":3,"code":"3"},"fuel":0.75,"manned":true,"tags":["a","b"],"note":null}`; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if _, err := containmentJSON([]core.Pair{{Key: "crew.size", Value: "", Null: true}}); err == nil || !strings.HasPrefix(err.Error(), "crew.size: the cell is empty") {
		t.Errorf("an empty cell should ask for the value: %v", err)
	}
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDatasetFormats(t *testing.T) {
	dir := t.TempDir()
	yml := write(t, dir, "m.yaml", "space.missions:\n  - id: m1\n    name: \"Artemis\"\n    budget: 500000\n    metadata: '{\"a\": 1}'\n  - id: m2\n    launch_date: \"[DAY,NOW]\"\nspace.crew: []\n")
	ds, err := LoadDataset(yml)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Tables) != 2 || ds.Tables[0].Name != "space.missions" || len(ds.Tables[0].Rows) != 2 || ds.Tables[0].Rows[0].Columns[2] != "budget" {
		t.Fatalf("yaml: %+v", ds)
	}
	xmlp := write(t, dir, "x.xml", `<?xml version="1.0"?><dataset><space.missions id="x1" name="A &amp; B"/><space.missions id="x2"/><space.empty/></dataset>`)
	ds, err = LoadDataset(xmlp)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Tables) != 2 || len(ds.Tables[0].Rows) != 2 || ds.Tables[0].Rows[0].Values[1] != "A & B" || len(ds.Tables[1].Rows) != 0 {
		t.Fatalf("xml: %+v", ds)
	}
	jp := write(t, dir, "j.json", `{"space.missions": [{"id": "j1", "meta": {"k": [1,2]}, "n": null}]}`)
	ds, err = LoadDataset(jp)
	if err != nil {
		t.Fatal(err)
	}
	if v := ds.Tables[0].Rows[0].Values; v[1] != `{"k":[1,2]}` || v[2] != nil {
		t.Fatalf("json: %#v", v)
	}
	csvDir := filepath.Join(dir, "csv")
	write(t, csvDir, "table-ordering.txt", "space.b\nspace.a\n")
	write(t, csvDir, "space.a.csv", "id,name\na1,null\n")
	write(t, csvDir, "space.b.csv", "id\nb1\nb2\n")
	ds, err = LoadDataset(filepath.Join(csvDir, "space.a.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if ds.Tables[0].Name != "space.b" || len(ds.Tables[0].Rows) != 2 || ds.Tables[1].Rows[0].Values[1] != nil {
		t.Fatalf("csv: %+v", ds)
	}
	if _, err := LoadDataset(write(t, dir, "old.xls", "x")); err == nil || !strings.Contains(err.Error(), ".xlsx") {
		t.Errorf(".xls must explain the alternative: %v", err)
	}
}

func TestResolveValue(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	cases := map[any]string{
		nil: "NULL", "[null]": "NULL", "x": "'x'", true: "'true'", uint64(7): "'7'", 2.5: "'2.5'",
		"[DAY,NOW]": "'2026-09-24 10:00:00+00:00'", "[DAY,PLUS,1]": "'2026-09-25 10:00:00+00:00'", "[HOUR,MINUS,2]": "'2026-09-24 08:00:00+00:00'",
		"[UNIX_TIMESTAMP]": "'1790244000'",
	}
	for in, want := range cases {
		got, err := resolveValue(postgres, in, now)
		if err != nil || got != want {
			t.Errorf("%v: got %s %v, want %s", in, got, err, want)
		}
	}
	// A time counted from now is the machine's wall clock, and PostgreSQL gets its offset: a
	// timestamptz column holds the same moment on a machine in Des Moines as on one in UTC.
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	local := now.In(chicago)
	for d, want := range map[Dialect]string{postgres: "'2026-09-24 04:00:00-05:00'", mysql: "'2026-09-24 04:00:00'", sqlserver: "'2026-09-24 04:00:00'"} {
		if got, err := resolveValue(d, "[HOUR,MINUS,1]", local); err != nil || got != want {
			t.Errorf("%s: got %s %v, want %s", d.Name, got, err, want)
		}
	}
	if _, err := resolveValue(postgres, "groovy:new Date()", now); err == nil {
		t.Error("scripted values must be rejected")
	}
}

func TestReadDbUnitCSV(t *testing.T) {
	in := "id,name,metadata\r\n" +
		"m1,Format CSV Mission,\"{\\\"purpose\\\": \\\"demo\\\"}\"\n" +
		"m2,\"a, b\",null\n" +
		"\n" +
		"m3,back\\\\slash,\"multi\nline\"\n"
	got, err := readDbUnitCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"id", "name", "metadata"},
		{"m1", "Format CSV Mission", `{"purpose": "demo"}`},
		{"m2", "a, b", "null"},
		{"m3", `back\slash`, "multi\nline"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if _, err := readDbUnitCSV(strings.NewReader("a,\"open\n")); err == nil {
		t.Error("an unclosed quote must be an error")
	}
}
