//go:build integration

package faults

import (
	"bytes"
	"context"
	dbsql "database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

const toxiproxyImage = "ghcr.io/shopify/toxiproxy:2.13.0"

// lab is a real PostgreSQL behind a real Toxiproxy.
type lab struct {
	api, direct, proxied string
}

var (
	labOnce sync.Once
	theLab  lab
	labErr  error
)

func toxiproxy(ctx context.Context, networkName string, env map[string]string) (testcontainers.Container, error) {
	req := testcontainers.ContainerRequest{
		Image: toxiproxyImage, ExposedPorts: []string{"8474/tcp", "15432/tcp"}, Env: env,
		WaitingFor: wait.ForListeningPort("8474/tcp").WithStartupTimeout(time.Minute),
	}
	if networkName != "" {
		req.Networks = []string{networkName}
	}
	return testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
}

func endpoint(ctx context.Context, c testcontainers.Container, port string) (string, string, error) {
	host, err := c.Host(ctx)
	if err != nil {
		return "", "", err
	}
	mapped, err := c.MappedPort(ctx, port)
	if err != nil {
		return "", "", err
	}
	return host, mapped.Port(), nil
}

func startLab(t *testing.T) lab {
	t.Helper()
	labOnce.Do(func() {
		ctx := context.Background()
		nw, err := network.New(ctx)
		if err != nil {
			labErr = err
			return
		}
		pg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image: "postgres:16-alpine", ExposedPorts: []string{"5432/tcp"},
				Networks: []string{nw.Name}, NetworkAliases: map[string][]string{nw.Name: {"postgres"}},
				Env:        map[string]string{"POSTGRES_DB": "parcels", "POSTGRES_USER": "parcels", "POSTGRES_PASSWORD": "parcels"},
				WaitingFor: wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute),
			},
			Started: true,
		})
		if err != nil {
			labErr = err
			return
		}
		tp, err := toxiproxy(ctx, nw.Name, nil)
		if err != nil {
			labErr = err
			return
		}
		host, port, err := endpoint(ctx, tp, "8474/tcp")
		if err != nil {
			labErr = err
			return
		}
		theLab.api = "http://" + host + ":" + port
		_, proxyPort, err := endpoint(ctx, tp, "15432/tcp")
		if err != nil {
			labErr = err
			return
		}
		pgHost, pgPort, err := endpoint(ctx, pg, "5432/tcp")
		if err != nil {
			labErr = err
			return
		}
		theLab.direct = fmt.Sprintf("postgres://parcels:parcels@%s:%s/parcels?sslmode=disable", pgHost, pgPort)
		theLab.proxied = fmt.Sprintf("postgres://parcels:parcels@%s:%s/parcels?sslmode=disable", host, proxyPort)
		labErr = createProxy(theLab.api, "parcels-db")
		if labErr != nil {
			return
		}
		db, err := dbsql.Open("pgx", theLab.direct)
		if err != nil {
			labErr = err
			return
		}
		defer db.Close()
		_, labErr = db.Exec("CREATE TABLE parcels (reference text PRIMARY KEY)")
	})
	if labErr != nil {
		t.Skipf("containers unavailable: %v", labErr)
	}
	return theLab
}

func createProxy(api, name string) error {
	b, _ := json.Marshal(map[string]any{"name": name, "listen": "0.0.0.0:15432", "upstream": "postgres:5432", "enabled": true})
	res, err := http.Post(api+"/proxies", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("creating the proxy: status %d", res.StatusCode)
	}
	return nil
}

type harness struct {
	t   *testing.T
	reg *match.Registry
	sc  *core.Scenario
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	reg := match.NewRegistry()
	if err := reg.AddParams("core", core.ParamsPack().Manifest().Params); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddPack(Name, Pack().Manifest()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	suite := core.NewSuite(core.SuiteOptions{ResolvePath: func(p string) (string, error) { return filepath.Join(dir, p), nil }})
	t.Cleanup(func() { _ = suite.Close(context.Background()) })
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: t.Name(), Name: t.Name()}, suite, nil)
	h := &harness{t: t, reg: reg, sc: sc}
	t.Cleanup(func() {
		if err := sc.Close(); err != nil {
			t.Errorf("scenario cleanup: %v", err)
		}
	})
	return h
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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

func (h *harness) proxy(api string) {
	h.t.Helper()
	h.ok("the local fault proxy with the following properties:", []string{"url", api})
}

// connect opens a connection through the proxy.
func connect(t *testing.T, dsn string) *dbsql.DB {
	t.Helper()
	db, err := dbsql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	return db
}

// fetch asks for that many bytes, and says how long the answer took.
func fetch(db *dbsql.DB, size int, limit time.Duration) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	start := time.Now()
	var s string
	err := db.QueryRowContext(ctx, "SELECT repeat('x', $1::int)", size).Scan(&s)
	return time.Since(start), err
}

func toxics(t *testing.T, api, proxy string) []Toxic {
	t.Helper()
	res, err := http.Get(api + "/proxies/" + proxy + "/toxics")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out []Toxic
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Each fault does to a real database's connection, open or new, what its step says,
// and is restored.
func TestFaultsOnARealConnection(t *testing.T) {
	l := startLab(t)
	const big, small = 300_000, 100
	for _, c := range []struct {
		step    string
		size    int
		limit   time.Duration
		fails   bool
		atLeast time.Duration
		atMost  time.Duration
	}{
		{step: "is cut off", size: small, limit: 5 * time.Second, fails: true, atMost: 2 * time.Second},
		{step: "is slowed by 1s", size: small, limit: 10 * time.Second, atLeast: time.Second},
		{step: "is slowed by 1s going out", size: small, limit: 10 * time.Second, atLeast: time.Second},
		{step: `is limited to 100 KB/s`, size: big, limit: 15 * time.Second, atLeast: 2 * time.Second},
		{step: "hangs", size: small, limit: 2 * time.Second, fails: true, atLeast: 2 * time.Second},
		{step: "hangs going out", size: small, limit: 2 * time.Second, fails: true, atLeast: 2 * time.Second},
		{step: "hangs for 1s, then closes", size: small, limit: 8 * time.Second, fails: true, atLeast: 900 * time.Millisecond, atMost: 4 * time.Second},
		{step: "is reset", size: small, limit: 5 * time.Second, fails: true, atMost: 2 * time.Second},
		{step: "is reset after 1s", size: small, limit: 8 * time.Second, fails: true, atLeast: 900 * time.Millisecond, atMost: 4 * time.Second},
		{step: "takes 1s to close", size: small, limit: 5 * time.Second},
		{step: "breaks into 100-byte pieces, 1ms apart", size: 100_000, limit: 20 * time.Second, atLeast: 500 * time.Millisecond},
		{step: "closes after 50 KB", size: big, limit: 5 * time.Second, fails: true},
		// An open connection counts from the step: a short answer gets through 1 KB,
		// and not through 100 bytes.
		{step: "closes after 1 KB", size: small, limit: 5 * time.Second},
		{step: "closes after 100 bytes", size: small, limit: 5 * time.Second, fails: true},
		{step: "loses 100% of its data", size: small, limit: 2 * time.Second, fails: true, atLeast: 2 * time.Second},
	} {
		t.Run(c.step, func(t *testing.T) {
			h := newHarness(t)
			h.proxy(l.api)
			open := connect(t, l.proxied)
			if _, err := fetch(open, small, 10*time.Second); err != nil {
				t.Fatalf("before the fault: %v", err)
			}
			h.ok("the parcels-db connection " + c.step)
			// The connection open before the fault, as a service's pool holds it.
			took, err := fetch(open, c.size, c.limit)
			if c.fails != (err != nil) {
				t.Errorf("took %s, err: %v, want failure: %t", took, err, c.fails)
			}
			if took < c.atLeast || c.atMost > 0 && took > c.atMost {
				t.Errorf("took %s, want %s to %s (err: %v)", took, c.atLeast, c.atMost, err)
			}
			h.ok("the parcels-db connection is restored")
			if got := toxics(t, l.api, "parcels-db"); len(got) != 0 {
				t.Errorf("toxics left: %+v", got)
			}
			if took, err := fetch(connect(t, l.proxied), small, 10*time.Second); err != nil || took > 3*time.Second {
				t.Errorf("after restoring: %s, %v", took, err)
			}
		})
	}
}

// With a hang coming back, the database makes the write and its answer never comes;
// going out, the write never arrives. A raw connection, with no check before it is
// used: a pool's driver can wait on its check instead.
func TestDirections(t *testing.T) {
	l := startLab(t)
	direct := connect(t, l.direct)
	stored := func(ref string) bool {
		t.Helper()
		var n int
		if err := direct.QueryRow("SELECT count(*) FROM parcels WHERE reference = $1", ref).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	for _, c := range []struct {
		direction string
		stored    bool
	}{{"coming back", true}, {"going out", false}} {
		t.Run(c.direction, func(t *testing.T) {
			h := newHarness(t)
			h.proxy(l.api)
			conn, err := pgx.Connect(context.Background(), l.proxied+"&default_query_exec_mode=simple_protocol")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = conn.Close(ctx)
			}()
			h.ok("the parcels-db connection hangs " + c.direction)
			ref := "PX-FLT-" + strings.ReplaceAll(c.direction, " ", "-")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := conn.Exec(ctx, "INSERT INTO parcels VALUES ('"+ref+"')"); err == nil {
				t.Error("the write got its answer")
			}
			time.Sleep(500 * time.Millisecond)
			if got := stored(ref); got != c.stored {
				t.Errorf("stored: %t, want %t", got, c.stored)
			}
		})
	}
}

// On a share of connections, some have the fault and some do not.
func TestShareOfConnections(t *testing.T) {
	l := startLab(t)
	h := newHarness(t)
	h.proxy(l.api)
	h.ok("the parcels-db connection is reset on 50% of connections")
	if got := toxics(t, l.api, "parcels-db"); len(got) != 1 || got[0].Toxicity != 0.5 {
		t.Fatalf("toxics: %+v", got)
	}
	worked, failed := 0, 0
	for range 40 {
		db := connect(t, l.proxied)
		if _, err := fetch(db, 10, 5*time.Second); err != nil {
			failed++
		} else {
			worked++
		}
		_ = db.Close()
	}
	if worked == 0 || failed == 0 {
		t.Errorf("%d worked and %d failed: a share should do some of each", worked, failed)
	}
}

// `for {duration}` ends a fault by itself, while a step waits for its answer.
func TestForADuration(t *testing.T) {
	l := startLab(t)
	for _, step := range []string{"is cut off for 1s", "hangs for 1s", "is slowed by 200ms on 100% of connections for 1s"} {
		t.Run(step, func(t *testing.T) {
			h := newHarness(t)
			h.proxy(l.api)
			h.ok("the parcels-db connection " + step)
			time.Sleep(1500 * time.Millisecond)
			if got := toxics(t, l.api, "parcels-db"); len(got) != 0 {
				t.Errorf("toxics left: %+v", got)
			}
			if took, err := fetch(connect(t, l.proxied), 10, 5*time.Second); err != nil || took > 500*time.Millisecond {
				t.Errorf("after it ended: %s, %v", took, err)
			}
		})
	}
}

// When a scenario ends, its own faults end, and no one else's.
func TestAScenarioUndoesOnlyItsOwn(t *testing.T) {
	l := startLab(t)
	b, _ := json.Marshal(Toxic{Name: "someone-elses", Type: "latency", Stream: StreamDownstream, Toxicity: 1, Attributes: map[string]any{"latency": 1}})
	res, err := http.Post(l.api+"/proxies/parcels-db/toxics", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, l.api+"/proxies/parcels-db/toxics/someone-elses", nil)
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
		}
	}()
	h := newHarness(t)
	h.proxy(l.api)
	h.ok("the parcels-db connection is slowed by 1s")
	h.ok("the parcels-db connection is cut off")
	if got := toxics(t, l.api, "parcels-db"); len(got) != 2 {
		t.Fatalf("toxics: %+v", got)
	}
	if err := h.sc.Close(); err != nil {
		t.Fatal(err)
	}
	got := toxics(t, l.api, "parcels-db")
	if len(got) != 1 || got[0].Name != "someone-elses" {
		t.Errorf("toxics left: %+v", got)
	}
	if _, err := fetch(connect(t, l.proxied), 10, 5*time.Second); err != nil {
		t.Errorf("the connection is still cut off: %v", err)
	}
}

// Any toxic, by its type and attributes; what Toxiproxy refuses, it says why.
func TestAnyToxic(t *testing.T) {
	l := startLab(t)
	h := newHarness(t)
	h.proxy(l.api)
	h.ok("the parcels-db connection has a latency toxic with the following attributes:",
		[]string{"latency", "2000"}, []string{"jitter", "500"}, []string{"stream", `"upstream"`}, []string{"toxicity", "0.5"})
	got := toxics(t, l.api, "parcels-db")
	if len(got) != 1 || got[0].Type != "latency" || got[0].Stream != StreamUpstream || got[0].Toxicity != 0.5 ||
		got[0].Attributes["latency"] != float64(2000) || got[0].Attributes["jitter"] != float64(500) {
		t.Errorf("toxics: %+v", got)
	}
	err := h.step("the parcels-db connection has a sparkle toxic with the following attributes:", []string{"glow", "1"})
	if err == nil || !strings.Contains(err.Error(), "sparkle") {
		t.Errorf("an unknown type: %v", err)
	}
	err = h.step("the parcels-db connection has a latency toxic with the following attributes:", []string{"stream", `"sideways"`})
	if err == nil || !strings.Contains(err.Error(), "upstream") {
		t.Errorf("a wrong stream: %v", err)
	}
}

func TestWhatIsNotThere(t *testing.T) {
	l := startLab(t)
	h := newHarness(t)
	err := h.step("the parcels-db connection is cut off")
	if err == nil || !strings.Contains(err.Error(), "fault proxy with the following properties") {
		t.Errorf("no fault proxy: %v", err)
	}
	h.proxy(l.api)
	err = h.step("the billing-db connection is cut off")
	if err == nil || !strings.Contains(err.Error(), "billing-db") || !strings.Contains(err.Error(), "parcels-db") {
		t.Errorf("no such connection: %v", err)
	}
	err = newHarness(t).step("the local fault proxy with the following properties:", []string{"url", "http://127.0.0.1:1"})
	if err == nil || !strings.Contains(err.Error(), "could not reach") {
		t.Errorf("no Toxiproxy there: %v", err)
	}
	h.ok("the parcels-db connection is restored") // nothing to restore
}

func TestToxiproxyWithAToken(t *testing.T) {
	ctx := context.Background()
	c, err := toxiproxy(ctx, "", map[string]string{"TOXIPROXY_AUTH_TOKEN": "fault-proxy-token"})
	if err != nil {
		t.Skipf("toxiproxy unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, port, err := endpoint(ctx, c, "8474/tcp")
	if err != nil {
		t.Fatal(err)
	}
	api := "http://" + host + ":" + port
	err = newHarness(t).step("the local fault proxy with the following properties:", []string{"url", api})
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("no token: %v", err)
	}
	newHarness(t).ok("the local fault proxy with the following properties:", []string{"url", api}, []string{"token", "fault-proxy-token"})
}
