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
		{"jdbc:postgresql://localhost:5432/space_explorer", "postgresql", "postgres://u:p@localhost:5432/space_explorer?sslmode=disable"},
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
	got, err := whereClause(postgres, &core.Table{Rows: [][]string{{"status", "planned"}, {"note", "NULL"}, {"name", "O'Hara"}}}, "NEW.")
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

func TestContainmentJSON(t *testing.T) {
	got, err := containmentJSON([]core.Pair{{Key: "priority", Value: "high"}, {Key: "crew.lead", Value: "Ada"}, {Key: "crew.size", Value: "3"}, {Key: "note", Value: "null"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"priority":"high","crew":{"lead":"Ada","size":"3"},"note":null}` {
		t.Fatalf("got %s", got)
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
		"[DAY,NOW]": "'2026-09-24 10:00:00'", "[DAY,PLUS,1]": "'2026-09-25 10:00:00'", "[HOUR,MINUS,2]": "'2026-09-24 08:00:00'",
		"[UNIX_TIMESTAMP]": "'1790244000'",
	}
	for in, want := range cases {
		got, err := resolveValue(postgres, in, now)
		if err != nil || got != want {
			t.Errorf("%v: got %s %v, want %s", in, got, err, want)
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
