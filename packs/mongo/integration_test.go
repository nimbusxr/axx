//go:build integration

package mongo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

func TestSeedAgainstRealMongo(t *testing.T) {
	ctx := context.Background()
	c, err := tcmongo.Run(ctx, "mongo:7", tcmongo.WithUsername("space"), tcmongo.WithPassword("secret"))
	if err != nil {
		t.Skipf("mongo container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "27017/tcp")
	url := fmt.Sprintf("mongodb://%s:%s/space_explorer?authSource=admin", host, port.Port())

	dir := t.TempDir()
	seed := filepath.Join(dir, "telemetry.json")
	if err := os.WriteFile(seed, []byte(`{"telemetry": [{"_id": {"$oid": "65a1b2c3d4e5f60718293a4b"}, "mission": "m1", "speed": 7.5}]}`), 0o644); err != nil {
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
	if err := run("a telemetry.json MongoDB seed for space-mongodb"); err == nil || !strings.Contains(err.Error(), "duplicate key") {
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
	if err := run("the 1st document for the selection properties are:", []string{"mission", "m2"}); !core.IsAssertion(err) {
		t.Fatalf("property mismatch must be an assertion failure: %v", err)
	}
	svc, err := Context(sc).Service("space-mongodb")
	if err != nil || len(svc.Selections()) != 4 || svc.DB() == nil {
		t.Fatalf("context: %v", err)
	}

	var doc bson.M
	if err := svc.db.Collection("telemetry").FindOne(ctx, bson.M{"mission": "m1"}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["_id"].(bson.ObjectID); !ok || doc["speed"] != 7.5 {
		t.Fatalf("document: %#v", doc)
	}
}
