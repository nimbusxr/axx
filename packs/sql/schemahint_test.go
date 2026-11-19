package sql

import (
	"context"
	dbsql "database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A seed the database refuses for a column or table it does not have names
// what the database has, so a seed written by guessing shows what to write.
func TestSeedErrorsNameTheColumnsAndTables(t *testing.T) {
	db, err := dbsql.Open("sqlite", filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, q := range []string{
		"CREATE TABLE parcels (reference TEXT PRIMARY KEY, sender TEXT, weight_grams INTEGER)",
		"CREATE TABLE manifest_lines (id TEXT PRIMARY KEY)",
		"CREATE TABLE scans (id TEXT NOT NULL)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{Target: Target{Dialect: sqlite}, DB: db}
	seed := func(yaml string) error {
		p := filepath.Join(t.TempDir(), "seed.yaml")
		if err := os.WriteFile(p, []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		ds, err := LoadDataset(p)
		if err != nil {
			t.Fatal(err)
		}
		return svc.insert(context.Background(), ds, time.Now())
	}
	err = seed("parcels:\n  - reference: PX-1\n    zone: DE-1\n")
	if err == nil || !strings.Contains(err.Error(), "; parcels has the columns reference, sender, weight_grams") {
		t.Errorf("unknown column: %v", err)
	}
	err = seed("parcel:\n  - reference: PX-1\n")
	if err == nil || !strings.Contains(err.Error(), "; the database has the tables manifest_lines, parcels, scans") {
		t.Errorf("unknown table: %v", err)
	}
	// A key the table already holds is most often a row of an earlier run.
	if _, err := db.Exec("INSERT INTO parcels (reference) VALUES ('PX-2')"); err != nil {
		t.Fatal(err)
	}
	err = seed("parcels:\n  - reference: PX-2\n")
	if err == nil || !strings.HasSuffix(err.Error(), DuplicateKeyHint()) {
		t.Errorf("duplicate key: %v", err)
	}
	for _, msg := range []string{
		`ERROR: duplicate key value violates unique constraint "parcels_pkey" (SQLSTATE 23505)`,
		`Error 1062 (23000): Duplicate entry 'PX-2' for key 'parcels.PRIMARY'`,
		`mssql: Violation of PRIMARY KEY constraint 'PK_parcels'. Cannot insert duplicate key in object 'dbo.parcels'.`,
	} {
		if !duplicateKey.MatchString(msg) {
			t.Errorf("not a duplicate key: %s", msg)
		}
	}
	// Db2's errors, as its messages word them, and the catalog a hint asks.
	for msg, pattern := range map[string]interface{ MatchString(string) bool }{
		`db2: SQL0803N  An error or warning occurred.  One or more values in the INSERT statement ... constrains table "APP.SEEDED" from having duplicate values for the index key.`: duplicateKey,
		`db2: SQL0204N  "APP.SEEDD" is an undefined name.`:                                                 unknownTable,
		`db2: SQL0206N  "NO_SUCH_COLUMN" is not valid in the context where it is used.`:                    unknownColumn,
		`db2: SQL0798N  A value cannot be specified for column "ID" which is defined as GENERATED ALWAYS.`: alwaysIdentity,
	} {
		if !pattern.MatchString(msg) {
			t.Errorf("not recognized: %s", msg)
		}
	}
	onDb2 := &Service{Target: Target{Dialect: db2db}}
	for got, want := range map[string]string{
		onDb2.columnsQuery("", "seeded"):       "SELECT colname FROM syscat.columns WHERE tabschema = CURRENT SCHEMA AND tabname = UPPER('seeded') ORDER BY colno",
		onDb2.columnsQuery("depot", "shelves"): "SELECT colname FROM syscat.columns WHERE tabschema = UPPER('depot') AND tabname = UPPER('shelves') ORDER BY colno",
		onDb2.tablesQuery("depot"):             "SELECT tabname FROM syscat.tables WHERE tabschema = UPPER('depot') AND type = 'T' ORDER BY tabname",
	} {
		if got != want {
			t.Errorf("Db2's catalog is asked\n %s\nwant\n %s", got, want)
		}
	}
	// Any other refusal is left as the database words it.
	err = seed("scans:\n  - id: null\n")
	if err == nil || strings.Contains(err.Error(), "; ") {
		t.Errorf("other refusal: %v", err)
	}
}
