package logs

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

type harness struct {
	t     *testing.T
	reg   *match.Registry
	suite *core.Suite
	dir   string
	sc    *core.Scenario
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	reg := match.NewRegistry()
	for _, p := range []struct {
		n string
		p core.Pack
	}{{"core", core.ParamsPack()}, {"logs", Pack()}} {
		if err := reg.AddPack(p.n, p.p.Manifest()); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	suite := core.NewSuite(core.SuiteOptions{ProjectDir: dir})
	t.Cleanup(func() { _ = suite.Close(context.Background()) })
	h := &harness{t: t, reg: reg, suite: suite, dir: dir}
	h.sc = h.scenario()
	return h
}

func (h *harness) scenario() *core.Scenario {
	return core.NewScenario(context.Background(), core.ScenarioInfo{Name: "test"}, h.suite, nil)
}

func (h *harness) step(text string, table ...[]string) error {
	h.t.Helper()
	ms := h.reg.Match(text)
	if len(ms) != 1 {
		h.t.Fatalf("%q matched %d definitions", text, len(ms))
	}
	var tbl *core.Table
	if table != nil {
		tbl = &core.Table{Rows: table}
	}
	args, err := h.reg.Resolve(h.sc, ms[0], text, tbl, nil)
	if err != nil {
		return err
	}
	return ms[0].Def().Step.Run(h.sc, args)
}

func (h *harness) ok(text string, table ...[]string) {
	h.t.Helper()
	if err := h.step(text, table...); err != nil {
		h.t.Fatalf("%s: %v", text, err)
	}
}

func (h *harness) fails(text string, want string, table ...[]string) error {
	h.t.Helper()
	err := h.step(text, table...)
	if err == nil || !strings.Contains(err.Error(), want) {
		h.t.Fatalf("%s: want an error containing %q, got %v", text, want, err)
	}
	return err
}

// prepare runs the pack's before-the-apps-start hook for these log urls.
func (h *harness) prepare(urls ...string) {
	h.t.Helper()
	plan := &core.Plan{}
	for _, u := range urls {
		plan.Scenarios = append(plan.Scenarios, core.PlannedScenario{Steps: []core.PlannedStep{{
			Definition: "logs.log", Table: &core.Table{Rows: [][]string{{"url", u}}},
		}}})
	}
	if err := (pack{}).Prepare(context.Background(), h.suite, plan); err != nil {
		h.t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

// later runs f after a short delay, as a service writing while a step waits.
func later(f func()) {
	go func() {
		time.Sleep(150 * time.Millisecond)
		f()
	}()
}

func freePort(t *testing.T, network string) string {
	t.Helper()
	if network == "udp" {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer pc.Close()
		return pc.LocalAddr().String()
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func TestFileLogOnlyCountsWhatArrivesAfterRegistration(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.dir, "logs", "app.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "manifest line processed line=OLD-1 status=IMPORTED\n")
	h.ok("the parcels log with the following properties:", []string{"url", "file://logs/app.log"})

	_ = h.fails("within 1s the parcels log has an entry matching 'line=OLD-1'", "No entry matched within 1s")
	later(func() { appendTo(t, path, "manifest line processed line=ML-1 status=IMPORTED\n") })
	h.ok("the parcels log has an entry matching 'line=ML-1 status=IMPORTED'")
}

func TestPatternsAreGlobalAndMultiline(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.dir, "app.log")
	h.ok("the parcels log with the following properties:", []string{"url", "file://" + filepath.ToSlash(path)})
	appendTo(t, path, strings.Join([]string{
		"2026-09-24 10:00:01 ERROR registration failed reference=PX-1",
		"java.lang.IllegalStateException: address service unavailable",
		"    at example.Parcels.register(Parcels.java:42)",
		"2026-09-24 10:00:02 INFO registration refused reference=PX-2",
		"",
	}, "\n"))

	// ^ and $ match at line boundaries.
	h.ok("within 1s the parcels log has an entry matching '^java\\.lang\\.IllegalStateException: address service unavailable$'")
	// A pattern can span lines, with \n or (?s).
	h.ok(`within 1s the parcels log has an entry matching 'reference=PX-1\njava\.lang\.IllegalStateException'`)
	h.ok("within 1s the parcels log has an entry matching '(?s)reference=PX-1.*at example\\.Parcels\\.register'")
	// Every match counts.
	h.ok("within 1s the parcels log has 2 entries matching '^\\S+ \\S+ (ERROR|INFO) registration'")
	_ = h.fails("within 1s the parcels log has an entry matching '(unclosed'", "invalid regular expression")
}

func TestEachRowNeedsAMatchOfItsOwn(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.dir, "app.log")
	h.ok("the parcels log with the following properties:", []string{"url", "file://" + filepath.ToSlash(path)})
	appendTo(t, path, "retrying reference=PX-1 attempt=1\nstored reference=PX-1\n")

	h.ok("within 1s the parcels log has entries matching:", []string{"retrying reference=PX-1"}, []string{"stored reference=PX-1"})
	// One match cannot satisfy two rows.
	err := h.fails("within 1s the parcels log has entries matching:", "No entry matched within 1s",
		[]string{"retrying reference=PX-1"}, []string{"retrying reference=PX-1"})
	if !core.IsAssertion(err) || !strings.Contains(err.Error(), "received 2 lines since the scenario registered it") {
		t.Errorf("the failure should show what the log received: %v", err)
	}
	// Rows share candidates: the matching finds an assignment (row 1 could
	// take either line, row 2 only the second).
	h.ok("within 1s the parcels log has entries matching:", []string{"reference=PX-1"}, []string{"stored reference=PX-1"})
}

func TestCountingEntries(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.dir, "app.log")
	h.ok("the parcels log with the following properties:", []string{"url", "file://" + filepath.ToSlash(path)})
	later(func() {
		appendTo(t, path, "storing parcel PX-9 failed, retrying\nstoring parcel PX-9 failed, retrying\n")
	})

	h.ok("within 2s the parcels log has 2 entries matching 'storing parcel PX-9 failed'")
	h.ok("the parcels log has 1 entry matching 'PX-9 failed, retrying\\nstoring'")
	err := h.fails("the parcels log has 1 entry matching 'storing parcel PX-9 failed'", "has 2 entries matching")
	if !core.IsAssertion(err) {
		t.Errorf("more matches than wanted is an assertion failure: %v", err)
	}
	_ = h.fails("within 1s the parcels log has 3 entries matching 'storing parcel PX-9 failed'", "has 2 entries matching storing parcel PX-9 failed, want 3 (waited 1s)")
}

func TestEntriesAcrossLogs(t *testing.T) {
	h := newHarness(t)
	app, mock := filepath.Join(h.dir, "app.log"), filepath.Join(h.dir, "mock.log")
	h.ok("the parcels log with the following properties:", []string{"url", "file://" + filepath.ToSlash(app)})
	h.ok("the addresses log with the following properties:", []string{"url", "file://" + filepath.ToSlash(mock)})
	later(func() {
		appendTo(t, app, "registration refused reference=PX-ADR-1104 reason=\"address service unavailable\"\n")
		appendTo(t, mock, "Request received:\n172.25.0.7 - GET /v1/postcodes/DE/00012\n")
	})

	h.ok("within 2s the logs have entries matching:",
		[]string{"parcels", "registration refused reference=PX-ADR-1104"},
		[]string{"addresses", "Request received:\\n\\S+ - GET /v1/postcodes/DE/00012"})
	_ = h.fails("within 1s the logs have entries matching:", "addresses log: POST",
		[]string{"parcels", "PX-ADR-1104"}, []string{"addresses", "POST"})
	_ = h.fails("the logs have entries matching:", `no log named "billing"`, []string{"billing", "x"})
}

func TestARotatedFileIsReadFromItsStart(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.dir, "app.log")
	appendTo(t, path, strings.Repeat("old line\n", 20))
	h.ok("the parcels log with the following properties:", []string{"url", "file://" + filepath.ToSlash(path)})
	if err := os.WriteFile(path, []byte("after rotation reference=PX-7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.ok("within 1s the parcels log has an entry matching 'after rotation reference=PX-7'")
}

func TestUDPListener(t *testing.T) {
	h := newHarness(t)
	addr := freePort(t, "udp")
	h.prepare("udp://" + addr)
	h.ok("the parcels log with the following properties:", []string{"url", "udp://" + addr})
	later(func() {
		conn, err := net.Dial("udp", addr)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(`<30>Sep 24 22:42:41 parcels[1]: msg="registration refused" reference=PX-EVT-4003`))
	})
	h.ok("within 2s the parcels log has an entry matching 'parcels\\[1\\]: msg=\"registration refused\" reference=PX-EVT-4003$'")
}

func TestTCPListenerReadsLinesAndOctetCountedFrames(t *testing.T) {
	h := newHarness(t)
	addr := freePort(t, "tcp")
	h.prepare("tcp://" + addr)
	h.ok("the billing log with the following properties:", []string{"url", "tcp://" + addr})
	later(func() {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		msg := "<14>1 2026-09-24T10:00:00Z host billing - - - invoice created parcel=PX-1"
		_, _ = fmt.Fprintf(conn, "invoice queued parcel=PX-1\r\n%d %s", len(msg), msg)
	})
	h.ok("within 2s the billing log has entries matching:", []string{"^invoice queued parcel=PX-1$"}, []string{"invoice created parcel=PX-1$"})
}

func TestHTTPListener(t *testing.T) {
	h := newHarness(t)
	addr := freePort(t, "tcp")
	h.prepare("http://"+addr+"/logs", "http://"+addr+"/audit")
	h.ok("the stack log with the following properties:", []string{"url", "http://" + addr + "/logs"})
	h.ok("the audit log with the following properties:", []string{"url", "http://" + addr + "/audit"})

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(`{"container_name":"/parcels-app-1","log":"parcel registered reference=PX-2"}` + "\n"))
	_ = zw.Close()
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/logs", &gz)
	req.Header.Set("Content-Encoding", "gzip")
	for _, r := range []struct {
		req  *http.Request
		want int
	}{
		{mustRequest(t, http.MethodPost, "http://"+addr+"/logs", `{"log":"parcel registered reference=PX-1"}`), http.StatusNoContent},
		{req, http.StatusNoContent},
		{mustRequest(t, http.MethodPut, "http://"+addr+"/audit", "cancelled reference=PX-1\n"), http.StatusNoContent},
		{mustRequest(t, http.MethodPost, "http://"+addr+"/nope", "x"), http.StatusNotFound},
		{mustRequest(t, http.MethodGet, "http://"+addr+"/logs", ""), http.StatusMethodNotAllowed},
	} {
		resp, err := http.DefaultClient.Do(r.req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != r.want {
			t.Errorf("%s %s: %d, want %d", r.req.Method, r.req.URL.Path, resp.StatusCode, r.want)
		}
	}
	h.ok("within 2s the stack log has 2 entries matching 'parcel registered reference=PX-\\d'")
	h.ok("within 2s the audit log has an entry matching '^cancelled reference=PX-1$'")
}

func TestHTTPSListener(t *testing.T) {
	h := newHarness(t)
	addr := freePort(t, "tcp")
	h.prepare("https://" + addr + "/logs")
	h.ok("the stack log with the following properties:", []string{"url", "https://" + addr + "/logs"})
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} //nolint:gosec // the listener's self-signed certificate
	resp, err := client.Post("https://"+addr+"/logs", "text/plain", strings.NewReader("tls line reference=PX-3\n"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	h.ok("within 2s the stack log has an entry matching 'tls line reference=PX-3'")
}

func TestAListenerAnotherAxxHoldsIsReused(t *testing.T) {
	h := newHarness(t)
	// Another process (the parent, standing in for `axx up`) holds the port.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	addr := pc.LocalAddr().String()
	src, _ := parseSource(h.dir, "udp://"+addr)
	if err := os.MkdirAll(listenDir(h.dir), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(owner{PID: os.Getppid()})
	if err := os.WriteFile(ownerFile(h.dir, src), b, 0o644); err != nil {
		t.Fatal(err)
	}

	h.prepare("udp://" + addr) // does not try to bind the busy port
	h.ok("the parcels log with the following properties:", []string{"url", "udp://" + addr})
	later(func() { appendTo(t, src.file(h.dir), "from the other process reference=PX-4\n") })
	h.ok("within 2s the parcels log has an entry matching 'from the other process reference=PX-4'")
}

func TestListenersCloseWithTheSuite(t *testing.T) {
	h := newHarness(t)
	addr := freePort(t, "tcp")
	h.prepare("tcp://" + addr)
	src, _ := parseSource(h.dir, "tcp://"+addr)
	if _, err := os.Stat(ownerFile(h.dir, src)); err != nil {
		t.Fatalf("an open listener has an owner file: %v", err)
	}
	if err := h.suite.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownerFile(h.dir, src)); !os.IsNotExist(err) {
		t.Errorf("the owner file should be gone: %v", err)
	}
	if conn, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		conn.Close()
		t.Error("the listener should be closed")
	}
}

func TestRegistrationErrors(t *testing.T) {
	h := newHarness(t)
	_ = h.fails("the parcels log with the following properties:", "unknown log property", []string{"path", "x"})
	_ = h.fails("the parcels log with the following properties:", "unknown scheme", []string{"url", "ftp://x"})
	_ = h.fails("the parcels log with the following properties:", "needs host:port", []string{"url", "udp://nowhere"})
	_ = h.fails("the parcels log with the following properties:", "have no path", []string{"url", "tcp://127.0.0.1:1/x"})
	h.ok("the parcels log with the following properties:", []string{"url", "file://app.log"})
	_ = h.fails("the parcels log with the following properties:", "already registered", []string{"url", "file://app.log"})
	_ = h.fails("the orders log has an entry matching 'x'", `no log named "orders"`)
}

func mustRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return req
}
