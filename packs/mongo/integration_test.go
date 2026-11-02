//go:build integration

package mongo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

func TestSeedAgainstRealMongo(t *testing.T) {
	ctx := context.Background()
	// With a user to create, the image first runs a mongod that listens only
	// inside the container, then restarts it: the second "Waiting for
	// connections" is the server the test reaches.
	c, err := tcmongo.Run(ctx, "mongo:7", tcmongo.WithUsername("space"), tcmongo.WithPassword("secret"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Waiting for connections").WithOccurrence(2).WithStartupTimeout(2*time.Minute),
			wait.ForListeningPort("27017/tcp")))
	if err != nil {
		t.Skipf("mongo container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "27017/tcp")
	url := fmt.Sprintf("mongodb://%s:%s/space_explorer?authSource=admin", host, port.Port())

	dir := t.TempDir()
	seed := filepath.Join(dir, "telemetry.json")
	if err := os.WriteFile(seed, []byte(`{"telemetry": [{"_id": {"$oid": "65a1b2c3d4e5f60718293a4b"}, "mission": "m1", "speed": 7.5, `+
		`"mass": 6.0, "crew": 3, "orbits": {"$numberLong": "12"}, "budget": {"$numberDecimal": "1.50"}, "callSign": "0042"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := match.NewRegistry()
	if err := reg.AddPack("core", core.ParamsPack().Manifest()); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddPack("mongo", Pack().Manifest()); err != nil {
		t.Fatal(err)
	}
	suite := core.NewSuite(core.SuiteOptions{ResolvePath: func(p string) (string, error) { return filepath.Join(dir, p), nil }})
	t.Cleanup(func() { _ = suite.Close(ctx) })
	sc := core.NewScenario(ctx, core.ScenarioInfo{}, suite, nil)
	run := func(text string, rows ...[]string) error {
		ms := reg.Match(text)
		if len(ms) != 1 {
			t.Fatalf("%q matched %d", text, len(ms))
		}
		var tbl *core.Table
		if rows != nil {
			tbl = &core.Table{Rows: rows}
		}
		args, err := reg.Resolve(sc, ms[0], text, tbl, nil)
		if err != nil {
			return err
		}
		return ms[0].Def().Step.Run(sc, args)
	}
	if err := run("a space-mongodb mongo database with the following properties:", []string{"url", url}, []string{"user", "space"}, []string{"password", "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := run("a telemetry.json mongo db seed"); err != nil {
		t.Fatal(err)
	}
	if err := run("a telemetry.json MongoDB seed for space-mongodb"); err == nil || !strings.Contains(err.Error(), "duplicate key") || !strings.HasSuffix(err.Error(), duplicateKeyHint) {
		t.Fatalf("second insert of the same _id should fail with duplicate key: %v", err)
	}
	if err := run("a selection of documents is retrieved from the telemetry collection where:", []string{"mission", "m1"}, []string{"speed", "7.5"}); err != nil {
		t.Fatal(err)
	}
	if err := run("a 2nd selection of documents is retrieved from the telemetry collection on space-mongodb where:", []string{"speed", `"7.5"`}); err != nil {
		t.Fatal(err)
	}
	if err := run("a 3rd selection of documents is retrieved from the telemetry collection where:", []string{"mission", "m1"}); err != nil {
		t.Fatal(err)
	}
	if err := run("within 2s a selection of at least 1 document is retrieved from the telemetry collection where:", []string{"_id", `{"$oid": "65a1b2c3d4e5f60718293a4b"}`}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"the selection has 1 document",
		"the 2nd selection has 0 documents",
		"the 3rd selection on space-mongodb has fewer than 2 documents",
		"the 4th selection has more than 0 documents",
	} {
		if err := run(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if err := run("the 2nd selection has 1 document"); err == nil || !core.IsAssertion(err) {
		t.Fatalf("count mismatch must be an assertion failure: %v", err)
	}
	if err := run("the 1st document for the selection properties are:", []string{"mission", "m1"}, []string{"speed", "7.5"}, []string{"_id", "65a1b2c3d4e5f60718293a4b"}, []string{"fuel", "undefined"}); err != nil {
		t.Fatal(err)
	}
	if err := run("the 1st document for the 3rd selection on space-mongodb properties match:", []string{"mission", "m\\d"}); err != nil {
		t.Fatal(err)
	}
	// Values mean what they say (ADR 0014): a whole double is a decimal, a
	// Decimal128 a number, and a string that reads as a number is in quotes.
	if err := run("the 1st document for the selection properties are:", []string{"mass", "6.0"}, []string{"crew", "3"}, []string{"orbits", "12"},
		[]string{"budget", "1.50"}, []string{"callSign", `"0042"`}); err != nil {
		t.Fatal(err)
	}
	// A pattern matches a value's text: a Decimal128's digits, and a double's as a decimal.
	if err := run("the 1st document for the selection properties match:", []string{"budget", `\d+\.\d{2}`}, []string{"mass", `\d+\.\d`}); err != nil {
		t.Fatal(err)
	}
	if err := run("the 1st document for the selection properties are:", []string{"mass", "6"}); !core.IsAssertion(err) || !strings.Contains(err.Error(), "mass is the decimal 6.0: write 6.0") {
		t.Fatalf("a whole double is a decimal: %v", err)
	}
	if err := run("a selection of documents is retrieved from the telemetry collection where:", []string{"orbits", "12L"}, []string{"callSign", `"0042"`}); err != nil {
		t.Fatal(err)
	}
	if err := run("a selection of documents is retrieved from the telemetry collection where:", []string{"callSign", ""}); err == nil || !strings.Contains(err.Error(), "callSign: the cell is empty") {
		t.Fatalf("an empty cell should ask for the value: %v", err)
	}
	// A filter is as strict as a check: MongoDB's own equality would take the
	// first of these for all of the numbers, and null for no field at all.
	if err := os.WriteFile(filepath.Join(dir, "filters.json"), []byte(`{"filters": [
		{"_id": "int", "n": 6, "m": null, "tags": [1, 2]},
		{"_id": "long", "n": {"$numberLong": "6"}, "tags": [1.0, 2.0]},
		{"_id": "double", "n": 6.0, "scans": [{"count": 3.0}]},
		{"_id": "decimal", "n": {"$numberDecimal": "6.0"}, "scans": [{"count": 3}]},
		{"_id": "decimal-whole", "n": {"$numberDecimal": "6"}},
		{"_id": "text", "n": "6"},
		{"_id": "price", "p": {"$numberDecimal": "1.1"}},
		{"_id": "price-double", "p": 1.1},
		{"_id": "nested", "o": {"a": 1}},
		{"_id": "nested-double", "o": {"a": 1.0}}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run("a filters.json mongo db seed"); err != nil {
		t.Fatal(err)
	}
	svc0, err := Context(sc).Service("space-mongodb")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		field, cell string
		want        []string
	}{
		{"n", "6", []string{"int", "long", "decimal-whole"}},
		{"n", "6L", []string{"int", "long", "decimal-whole"}},
		{"n", "6.0", []string{"double", "decimal"}},
		{"n", `"6"`, []string{"text"}},
		{"m", "null", []string{"int"}},
		{"o", "undefined", []string{"int", "long", "double", "decimal", "decimal-whole", "text", "price", "price-double"}},
		{"o.a", "undefined", []string{"int", "long", "double", "decimal", "decimal-whole", "text", "price", "price-double"}},
		{"tags", "1", []string{"int"}},
		{"tags", "1.0", []string{"long"}},
		{"scans.count", "3", []string{"decimal"}},
		{"scans.count", "3.0", []string{"double"}},
		{"p", "1.1", []string{"price", "price-double"}},
		{"o", `{"a": 1}`, []string{"nested"}},
	} {
		if err := run("a selection of documents is retrieved from the filters collection where:", []string{c.field, c.cell}); err != nil {
			t.Fatalf("%s %s: %v", c.field, c.cell, err)
		}
		all := svc0.Selections()
		var got []string
		for _, d := range all[len(all)-1].Docs {
			got = append(got, fmt.Sprint(d[0].Value))
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s %s selected %v, want %v", c.field, c.cell, got, c.want)
		}
	}
	// A poll counts what a check would accept, and says what to write.
	err = run("within 1s a selection of at least 1 document is retrieved from the filters collection where:", []string{"_id", "double"}, []string{"n", "6"})
	if !core.IsAssertion(err) || !strings.Contains(err.Error(), "n is the decimal 6.0: write 6.0") {
		t.Errorf("a poll for an integer on a double should say what to write: %v", err)
	}
	if err := run("the 1st document for the selection properties are:", []string{"mission", "m2"}); !core.IsAssertion(err) {
		t.Fatalf("property mismatch must be an assertion failure: %v", err)
	}
	if err := run("within 1s a 6th selection of at least 1 document is retrieved from the telemetry collection where:", []string{"mission", "never"}); !core.IsAssertion(err) {
		t.Fatalf("polling that runs out of time with fewer documents should fail: %v", err)
	}
	svc, err := Context(sc).Service("space-mongodb")
	if err != nil || len(svc.Selections()) != 20 || svc.DB() == nil {
		t.Fatalf("context: %d selections: %v", len(svc.Selections()), err)
	}

	var doc bson.M
	if err := svc.db.Collection("telemetry").FindOne(ctx, bson.M{"mission": "m1"}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["_id"].(bson.ObjectID); !ok || doc["speed"] != 7.5 {
		t.Fatalf("document: %#v", doc)
	}
}
