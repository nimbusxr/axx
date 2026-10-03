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
	if err == nil || !strings.Contains(err.Error(), "; the database has the tables manifest_lines, parcels") {
		t.Errorf("unknown table: %v", err)
	}
	// Any other refusal is left as the database words it.
	if _, err := db.Exec("INSERT INTO parcels (reference) VALUES ('PX-2')"); err != nil {
		t.Fatal(err)
	}
	err = seed("parcels:\n  - reference: PX-2\n")
	if err == nil || strings.Contains(err.Error(), "has the") {
		t.Errorf("duplicate key: %v", err)
	}
}
