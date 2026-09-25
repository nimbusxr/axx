//go:build integration

package sql

import (
	"context"
	dbsql "database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

const schema = `
CREATE SCHEMA space;
CREATE TABLE space.missions (id text primary key, name text, status text, budget integer, launch_date timestamp, metadata jsonb);
CREATE TABLE space.crew (id text primary key, name text unique);
`

var (
	pgOnce sync.Once
	pgURL  string
	pgErr  error
)

// postgresURL starts one PostgreSQL 16 container for the package.
func postgresURL(t *testing.T) string {
	t.Helper()
	pgOnce.Do(func() {
		ctx := context.Background()
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("space_explorer"), tcpostgres.WithUsername("space"), tcpostgres.WithPassword("secret"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
		if err != nil {
			pgErr = err
			return
		}
		host, _ := c.Host(ctx)
		port, _ := c.MappedPort(ctx, "5432/tcp")
		pgURL = fmt.Sprintf("jdbc:postgresql://%s:%s/space_explorer", host, port.Port())
		db, err := dbsql.Open("pgx", fmt.Sprintf("postgres://space:secret@%s:%s/space_explorer?sslmode=disable", host, port.Port()))
		if err == nil {
			_, err = db.Exec(schema)
			_ = db.Close()
		}
		pgErr = err
	})
	if pgErr != nil {
		t.Skipf("postgres container unavailable: %v", pgErr)
	}
	return pgURL
}

type harness struct {
	t   *testing.T
	reg *match.Registry
	sc  *core.Scenario
	dir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	reg := match.NewRegistry()
	for name, p := range map[string]core.Pack{"core": core.ParamsPack()} {
		if err := reg.AddParams(name, p.Manifest().Params); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.AddPack("sql", Pack().Manifest()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	suite := core.NewSuite(core.SuiteOptions{ResolvePath: func(p string) (string, error) { return filepath.Join(dir, p), nil }})
	t.Cleanup(func() { _ = suite.Close(context.Background()) })
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: t.Name(), Name: t.Name()}, suite, nil)
	t.Cleanup(func() {
		if err := sc.Close(); err != nil {
			t.Errorf("scenario cleanup: %v", err)
		}
	})
	return &harness{t: t, reg: reg, sc: sc, dir: dir}
}

func (h *harness) step(text string, rows ...[]string) error {
	h.t.Helper()
	ms := h.reg.Match(text)
	if len(ms) != 1 {
		h.t.Fatalf("%q matched %d definitions", text, len(ms))
	}
	var tbl *core.Table
	if rows != nil {
		tbl = &core.Table{Rows: rows}
	}
	args, err := h.reg.Resolve(h.sc, ms[0], text, tbl, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	h.sc.SetContext(ctx)
	return ms[0].Def().Step.Run(h.sc, args)
}

func (h *harness) ok(text string, rows ...[]string) {
	h.t.Helper()
	if err := h.step(text, rows...); err != nil {
		h.t.Fatalf("%s: %v", text, err)
	}
}

func (h *harness) file(name, content string) {
	h.t.Helper()
	p := filepath.Join(h.dir, name)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) db(url string) {
	h.ok("a space-db database with the following properties:", []string{"url", url}, []string{"user", "space"}, []string{"password", "secret"})
}

func uniq(t *testing.T) string {
	return strings.ToLower(strings.NewReplacer("/", "-", "_", "-").Replace(t.Name()))
}

func TestSeedSelectCount(t *testing.T) {
	h := newHarness(t)
	h.db(postgresURL(t))
	id := uniq(t)
	h.file("seeds/m.yaml", fmt.Sprintf("space.missions:\n  - id: %[1]s-1\n    name: \"O'Hara\"\n    status: planned\n    budget: 500000\n    metadata: '{\"priority\": \"high\", \"crew\": {\"size\": 3}}'\n  - id: %[1]s-2\n    status: planned\n    launch_date: \"[DAY,NOW]\"\n", id))
	h.ok("a seeds/m.yaml db seed")
	h.ok("a selection of rows is retrieved from the space.missions table where:", []string{"id", id + "-1"})
	h.ok("the selection has 1 row")
	h.ok("a selection of rows is retrieved from the space.missions table on space-db where:", []string{"name", "O'Hara"})
	h.ok("the 2nd selection has 1 row")
	h.ok("a 3rd selection of rows is retrieved from the space.missions table where:", []string{"status", "planned"}, []string{"launch_date", "null"}, []string{"name", "O'Hara"})
	h.ok("the 3rd selection has more than 0 rows")
	h.ok("the 3rd selection on space-db has fewer than 2 rows")
	if err := h.step("the selection has 5 rows"); err == nil || !core.IsAssertion(err) {
		t.Fatalf("wrong count must be an assertion failure: %v", err)
	}
	h.ok("a selection of rows is retrieved from the space.missions table where the metadata jsonb column contains:", []string{"priority", "high"})
	h.ok("the 4th selection has 1 row")
	if err := h.step("the 9th selection has 1 row"); err == nil || !strings.Contains(err.Error(), "4 selection") {
		t.Fatalf("missing selection message: %v", err)
	}
	h.ok("the 1st row metadata property for the selection json properties are:", []string{"priority", "high"}, []string{"crew.size", "3"}, []string{"crew.lead", "undefined"})
	h.ok("the 1st row metadata property for the 4th selection on space-db json properties match:", []string{"priority", "hi.*"}, []string{"$.crew.size", `\d`})
	// The context is public: custom packs see the same service and selections.
	svc, err := Context(h.sc).Service("space-db")
	if err != nil || len(svc.Selections()) != 4 {
		t.Fatalf("context: %v %d selections", err, len(svc.Selections()))
	}
	if def, _ := Context(h.sc).Service(); def != svc {
		t.Fatal("the first registered service is the default")
	}
	extra, err := svc.Query(context.Background(), "space.missions", "SELECT * FROM space.missions WHERE id = '"+id+"-1'")
	if err != nil || len(extra.Rows) != 1 {
		t.Fatalf("query: %v", err)
	}
	svc.AddSelection(extra)
	h.ok("the 5th selection has 1 row")
	if err := h.step("the 1st row metadata property for the selection json properties are:", []string{"priority", "low"}); err == nil || !core.IsAssertion(err) {
		t.Fatalf("JSON property mismatch must be an assertion failure: %v", err)
	}
}

func TestSeedFormats(t *testing.T) {
	h := newHarness(t)
	h.db(postgresURL(t))
	id := uniq(t)
	h.file("x.xml", fmt.Sprintf(`<dataset><space.crew id="%[1]s-x1" name="%[1]s-x1"/></dataset>`, id))
	h.file("j.json", fmt.Sprintf(`{"space.crew": [{"id": "%[1]s-j1", "name": "%[1]s-j1"}]}`, id))
	h.file("csv/space.crew.csv", fmt.Sprintf("id,name\n%[1]s-c1,%[1]s-c1\n", id))
	for _, f := range []string{"x.xml", "j.json", "csv/space.crew.csv"} {
		h.ok("a " + f + " db seed")
	}
	h.ok("a selection of rows is retrieved from the space.crew table where:", []string{"id", id + "-c1"})
	h.ok("the selection has 1 row")
	// A failing seed rolls back entirely.
	h.file("dup.yaml", fmt.Sprintf("space.crew:\n  - id: %[1]s-d1\n    name: %[1]s-d\n  - id: %[1]s-d2\n    name: %[1]s-d\n", id))
	if err := h.step("a dup.yaml db seed"); err == nil {
		t.Fatal("duplicate name must fail")
	}
	h.ok("a 2nd selection of rows is retrieved from the space.crew table where:", []string{"id", id + "-d1"})
	h.ok("the 2nd selection has 0 rows")
}

func TestPolling(t *testing.T) {
	h := newHarness(t)
	url := postgresURL(t)
	h.db(url)
	id := uniq(t)
	go func() {
		time.Sleep(700 * time.Millisecond)
		tgt, _ := Resolve(url, "space", "secret")
		db, _ := dbsql.Open("pgx", tgt.DSN)
		defer db.Close()
		_, _ = db.Exec("INSERT INTO space.missions (id, status) VALUES ($1, 'late')", id)
	}()
	start := time.Now()
	h.ok("within 10s a selection of at least 1 row is retrieved from the space.missions table where:", []string{"id", id})
	h.ok("the selection has 1 row")
	if time.Since(start) > 5*time.Second {
		t.Errorf("polling should stop as soon as the row appears")
	}
	h.ok("within 1s a 2nd selection of at least 1 row is retrieved from the space.missions table where:", []string{"id", "never-" + id})
	h.ok("the 2nd selection has 0 rows")
}

func TestLocks(t *testing.T) {
	h := newHarness(t)
	url := postgresURL(t)
	h.db(url)
	id := uniq(t)
	h.file("l.yaml", fmt.Sprintf("space.crew:\n  - id: %[1]s\n    name: %[1]s\n", id))
	h.ok("a l.yaml db seed")
	h.ok("the rows in the space.crew table are locked where:", []string{"id", id})
	tgt, _ := Resolve(url, "space", "secret")
	db, _ := dbsql.Open("pgx", tgt.DSN)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, err := db.ExecContext(ctx, "UPDATE space.crew SET name = name WHERE id = $1", id)
	cancel()
	if err == nil {
		t.Fatal("update should block while the row is locked")
	}
	h.ok("the row locks are released")
	if _, err := db.Exec("UPDATE space.crew SET name = name WHERE id = $1", id); err != nil {
		t.Fatalf("update after release: %v", err)
	}
}

func insertMission(t *testing.T, url, id, name string) error {
	t.Helper()
	tgt, _ := Resolve(url, "space", "secret")
	db, err := dbsql.Open("pgx", tgt.DSN)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("INSERT INTO space.missions (id, name) VALUES ($1, $2)", id, name)
	return err
}

func TestTriggers(t *testing.T) {
	url := postgresURL(t)
	t.Run("unlimited", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		name := "trig-" + uniq(t)
		h.ok("a before insert trigger on the space.missions table will raise a 23505 exception where:", []string{"name", name})
		for i := 0; i < 2; i++ {
			err := insertMission(t, url, fmt.Sprintf("%s-%d", name, i), name)
			if err == nil || !strings.Contains(err.Error(), "23505") {
				t.Fatalf("insert %d should raise 23505: %v", i, err)
			}
		}
		if err := insertMission(t, url, name+"-other", "someone-else"); err != nil {
			t.Fatalf("non-matching insert must succeed: %v", err)
		}
		h.ok("the before insert trigger on the space.missions table was raised 2 times")
	})
	t.Run("limited", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		name := "lim-" + uniq(t)
		h.ok("a 1st ordered before insert trigger on the space.missions table on space-db will raise a 23505 exception 1 time where:", []string{"name", name})
		if err := insertMission(t, url, name+"-a", name); err == nil {
			t.Fatal("first insert should raise")
		}
		if err := insertMission(t, url, name+"-b", name); err != nil {
			t.Fatalf("second insert should succeed: %v", err)
		}
		h.ok("the 1st ordered before insert trigger on the space.missions table on space-db was raised 1 time")
	})
	t.Run("insert and raise", func(t *testing.T) {
		h := newHarness(t)
		h.db(url)
		name := "phantom-" + uniq(t)
		h.ok("a before insert trigger on the space.missions table will insert and raise a 23505 exception 1 time where:", []string{"name", name})
		if err := insertMission(t, url, name, name); err == nil {
			t.Fatal("insert should raise")
		}
		h.ok("a selection of rows is retrieved from the space.missions table where:", []string{"id", name})
		h.ok("the selection has 1 row") // the phantom row was committed
		h.ok("the before insert trigger on the space.missions table was raised 1 time")
	})
	t.Run("cleanup drops the trigger", func(t *testing.T) {
		name := "gone-" + uniq(t)
		func() {
			h := newHarness(t)
			h.db(url)
			h.ok("a before insert trigger on the space.missions table will raise a 23505 exception where:", []string{"name", name})
			if err := h.sc.Close(); err != nil {
				t.Fatal(err)
			}
		}()
		if err := insertMission(t, url, name, name); err != nil {
			t.Fatalf("trigger should be dropped after the scenario: %v", err)
		}
	})
}
