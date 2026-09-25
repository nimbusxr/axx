package report

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/runner"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func testOptions(color bool) Options {
	return Options{
		Color:   color,
		Version: "0.1.0",
		BaseDir: "/work",
		Now:     func() time.Time { return t0 },
	}
}

// replay drives a reporter the way the runner does: envelopes, scenarios in
// completion order, then Finish.
func replay(t *testing.T, rep runner.Reporter, envs []*messages.Envelope, res *runner.RunResult) {
	t.Helper()
	for _, e := range envs {
		rep.Envelope(e)
	}
	for _, s := range res.Scenarios {
		rep.ScenarioFinished(s)
	}
	if err := rep.Finish(res); err != nil {
		t.Fatalf("Finish: %v", err)
	}
}

func render(t *testing.T, name string, opts Options, envs []*messages.Envelope, res *runner.RunResult) []byte {
	t.Helper()
	var buf bytes.Buffer
	rep, err := New(name, &buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	replay(t, rep, envs, res)
	return buf.Bytes()
}

func golden(t *testing.T, file string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", file)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run `go test ./internal/report -update` to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file (rerun with -update if intended)\n%s",
			file, strings.Join(unifiedDiff(splitLines(string(want)), splitLines(string(got)), 2), "\n"))
	}
}

func TestGolden(t *testing.T) {
	res, fx := handRun(t)
	envs := handEnvelopes(fx)
	for _, tc := range []struct {
		file  string
		name  string
		color bool
	}{
		{"pretty.golden", "pretty", false},
		{"pretty-color.golden", "pretty", true},
		{"progress.golden", "progress", false},
		{"compact.golden", "compact", false},
		{"junit.golden", "junit", false},
		{"messages.golden", "messages", false},
		{"cucumber-json.golden", "cucumber-json", false},
		{"agent.golden", "agent", false},
		{"teamcity.golden", "teamcity", false},
	} {
		t.Run(tc.file, func(t *testing.T) {
			golden(t, tc.file, render(t, tc.name, testOptions(tc.color), envs, res))
		})
	}
	t.Run("html.golden", func(t *testing.T) {
		var buf bytes.Buffer
		stub := htmlAssets{
			template: embeddedAssets.template,
			css:      "/* main.css */",
			icon:     "data:image/svg+xml;base64,AA==",
			js:       func(w io.Writer) error { _, err := io.WriteString(w, "/* main.js */"); return err },
		}
		replay(t, newHTML(&writer{w: &buf}, stub), envs, res)
		golden(t, "html.golden", buf.Bytes())
	})
}

func TestNamesAndUnknownReporter(t *testing.T) {
	want := []string{"pretty", "progress", "compact", "junit", "messages", "cucumber-json", "html", "agent", "teamcity"}
	if got := Names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Names() = %v", got)
	}
	for _, n := range Names() {
		if _, err := New(n, io.Discard, Options{}); err != nil {
			t.Errorf("New(%q): %v", n, err)
		}
	}
	_, err := New("tap", io.Discard, Options{})
	var ae *axxerr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("want *axxerr.Error, got %T %v", err, err)
	}
	if ae.Exit != exitcode.Usage || ae.Code != CodeUnknownReporter || !strings.Contains(ae.Message, `"tap"`) {
		t.Errorf("unexpected error: %+v", ae)
	}
	for _, n := range want {
		if !strings.Contains(ae.Hint, n) {
			t.Errorf("hint %q does not list %s", ae.Hint, n)
		}
	}
}

func TestJUnitIsWellFormed(t *testing.T) {
	res, _ := handRun(t)
	// Characters XML cannot carry must not break the document.
	nasty := "bad <tag> & \"quotes\" ]]> \x1b[31mred\x1b[0m \x00\x07 \xff end"
	res.Scenarios[0].Steps[1].Logs = []string{nasty}
	res.Scenarios[1].Steps[2].Err = errors.New(nasty)
	out := render(t, "junit", testOptions(false), nil, res)
	assertWellFormedXML(t, out)

	var doc junitSuites
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Tests != len(res.Scenarios) || len(doc.Suites) != 2 {
		t.Fatalf("tests=%d suites=%d", doc.Tests, len(doc.Suites))
	}
	// failed + undefined + ambiguous; pending + skipped.
	if doc.Failures != 7 || doc.Skipped != 2 {
		t.Errorf("failures=%d skipped=%d", doc.Failures, doc.Skipped)
	}
	names := map[string]bool{}
	for _, s := range doc.Suites {
		for _, c := range s.Cases {
			if names[c.Classname+"/"+c.Name] {
				t.Errorf("duplicate testcase name %q", c.Name)
			}
			names[c.Classname+"/"+c.Name] = true
		}
	}
	if !strings.Contains(string(out), "bad &lt;tag&gt; &amp;") || !strings.Contains(string(out), "]]]]><![CDATA[>") {
		t.Errorf("escaping missing:\n%s", out)
	}
}

func assertWellFormedXML(t *testing.T, b []byte) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatalf("malformed XML: %v", err)
		}
	}
}

func TestJSONFormatsAreValid(t *testing.T) {
	res, fx := handRun(t)
	for _, name := range []string{"cucumber-json", "agent"} {
		out := render(t, name, testOptions(false), nil, res)
		if !json.Valid(out) {
			t.Errorf("%s: invalid JSON:\n%s", name, out)
		}
	}

	var features []cjFeature
	if err := json.Unmarshal(render(t, "cucumber-json", testOptions(false), nil, res), &features); err != nil {
		t.Fatal(err)
	}
	if len(features) != 2 || features[0].ID != "orders" || len(features[0].Tags) != 1 {
		t.Fatalf("features: %+v", features)
	}
	var bg, sc int
	for _, el := range features[0].Elements {
		switch el.Type {
		case "background":
			bg++
		case "scenario":
			sc++
		}
	}
	if bg != 7 || sc != 7 {
		t.Errorf("orders feature: %d backgrounds, %d scenarios", bg, sc)
	}
	if id := features[0].Elements[len(features[0].Elements)-1].ID; id != "orders;order-<qty>-items;quantities;3" {
		t.Errorf("outline row id = %q", id)
	}

	var rep AgentReport
	if err := json.Unmarshal(render(t, "agent", testOptions(false), nil, res), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.SchemaVersion != 1 || rep.Result != "failed" || rep.NotRun != 2 || len(rep.Undefined) != 1 {
		t.Errorf("agent report: %+v", rep)
	}
	kinds := map[string]bool{}
	for _, f := range rep.Failures {
		if f.Error != nil {
			kinds[f.Error.Kind] = true
		}
	}
	for _, k := range []string{KindAssertion, KindError, KindTimeout, KindPanic, KindUndefined, KindAmbiguous, KindPending} {
		if !kinds[k] {
			t.Errorf("no failure of kind %s", k)
		}
	}

	// Every NDJSON line is one valid envelope.
	out := render(t, "messages", testOptions(false), handEnvelopes(fx), res)
	sc2 := bufio.NewScanner(bytes.NewReader(out))
	sc2.Buffer(nil, 1<<20)
	n := 0
	for sc2.Scan() {
		var env messages.Envelope
		if err := json.Unmarshal(sc2.Bytes(), &env); err != nil {
			t.Fatalf("line %d: %v", n+1, err)
		}
		n++
	}
	if n != len(handEnvelopes(fx)) {
		t.Errorf("%d lines, want %d", n, len(handEnvelopes(fx)))
	}
}

func TestAgentContextIsVerbatimAndRobust(t *testing.T) {
	res, _ := handRun(t)
	dup := res.Scenarios[1]
	dup.Context["odd"] = map[string]any{"fn": func() {}}
	var rep AgentReport
	if err := json.Unmarshal(render(t, "agent", testOptions(false), nil, res), &rep); err != nil {
		t.Fatal(err)
	}
	f := rep.Failures[0]
	if f.Scenario != "Reject duplicate order" || f.Rerun != "axx run features/orders.feature:15" {
		t.Fatalf("first failure: %+v", f)
	}
	var restCtx map[string]any
	if err := json.Unmarshal(f.Context["rest"], &restCtx); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(dup.Context["rest"])
	got, _ := json.Marshal(restCtx)
	if string(got) != string(want) {
		t.Errorf("context not verbatim:\n got %s\nwant %s", got, want)
	}
	if len(f.Context["odd"]) == 0 {
		t.Error("unencodable context value dropped instead of stringified")
	}
	if string(f.Error.Actual) != `{"id":7,"sku":"A1","status":"created"}` {
		t.Errorf("actual = %s", f.Error.Actual)
	}
}

func TestRerunCommandOption(t *testing.T) {
	res, _ := handRun(t)
	opts := testOptions(false)
	opts.RerunCommand = func(r *runner.ScenarioResult) string { return "make e2e ARGS=" + opts.scenarioLoc(r) }
	out := string(render(t, "compact", opts, nil, res))
	if !strings.Contains(out, "rerun make e2e ARGS=features/orders.feature:15") {
		t.Errorf("custom rerun command not used:\n%s", out)
	}
}

func TestCompactIsQuietWhenPassing(t *testing.T) {
	res, _ := handRun(t)
	var passing []*runner.ScenarioResult
	for _, s := range res.Scenarios {
		if s.Status == runner.Passed {
			passing = append(passing, s)
		}
	}
	out := render(t, "compact", testOptions(false), nil, &runner.RunResult{Scenarios: passing, Duration: 1500 * time.Millisecond})
	if string(out) != "PASS 2  1.5s\n" {
		t.Errorf("compact output for a passing run: %q", out)
	}
}

// htmlJSSHA256 is the checksum of @cucumber/html-formatter's dist/main.js.
const htmlJSSHA256 = "91063fd520049873df51ad63a278dd0ca7e8b3e36354e79cb9822ecca47b0017"

func TestHTMLEmbedsAssetsAndEveryEnvelope(t *testing.T) {
	res, fx := handRun(t)
	envs := handEnvelopes(fx)
	out := string(render(t, "html", testOptions(false), envs, res))

	var js bytes.Buffer
	if err := embeddedAssets.js(&js); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(js.Bytes())
	if hex.EncodeToString(sum[:]) != htmlJSSHA256 {
		t.Errorf("vendored main.js checksum mismatch")
	}
	for _, want := range []string{
		"<!DOCTYPE html>", "<title>axx</title>", `<link rel="icon" href="data:image/svg+xml;base64,`,
		"<style>\n" + htmlCSS + "\n\t</style>",
		"<div id=\"content\">",
		"<script>\nwindow.CUCUMBER_MESSAGES = [{\"meta\":",
		"}];\n</script>",
		"<script>\n" + js.String() + "\n</script>",
		"</html>\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("html output lacks %.80q", want)
		}
	}
	for i, e := range envs {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, string(b)) {
			t.Errorf("envelope %d missing from html output", i)
		}
	}
	if strings.Contains(out, "<script>alert") || strings.Contains(out, "boom </script>") {
		t.Error("envelope content was not escaped for the script element")
	}
}

func TestHTMLWithoutEnvelopes(t *testing.T) {
	var buf bytes.Buffer
	stub := htmlAssets{template: embeddedAssets.template, js: func(io.Writer) error { return nil }}
	rep := newHTML(&writer{w: &buf}, stub)
	if err := rep.Finish(&runner.RunResult{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "window.CUCUMBER_MESSAGES = [];") || !strings.HasSuffix(buf.String(), "</html>\n") {
		t.Errorf("empty report:\n%s", buf.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteErrorsSurfaceFromFinish(t *testing.T) {
	res, fx := handRun(t)
	for _, n := range Names() {
		rep, err := New(n, failingWriter{}, testOptions(false))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range handEnvelopes(fx) {
			rep.Envelope(e)
		}
		for _, s := range res.Scenarios {
			rep.ScenarioFinished(s)
		}
		if err := rep.Finish(res); err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Errorf("%s: Finish error = %v", n, err)
		}
	}
}

func TestUnifiedDiff(t *testing.T) {
	a := strings.Split("a b c d e f g h i j", " ")
	b := strings.Split("a b X d e f g h i j k", " ")
	got := strings.Join(unifiedDiff(a, b, 1), "\n")
	want := "@@ -2,3 +2,3 @@\n b\n-c\n+X\n d\n@@ -10 +10,2 @@\n j\n+k"
	if got != want {
		t.Errorf("diff:\n%s\nwant:\n%s", got, want)
	}
	if d := unifiedDiff(a, a, 3); d != nil {
		t.Errorf("equal inputs produced %v", d)
	}
	// Large inputs fall back to a replace-all diff instead of a huge table.
	big := make([]string, 3000)
	big2 := make([]string, 3000)
	for i := range big {
		big[i] = strings.Repeat("x", i%7) + string(rune('a'+i%26))
		big2[i] = string(rune('A' + i%26))
	}
	if d := unifiedDiff(big, big2, 3); len(d) != 6001 {
		t.Errorf("big diff has %d lines", len(d))
	}
}

func TestDiffValues(t *testing.T) {
	if d := diffValues(409, 201, 3); d != nil {
		t.Errorf("scalars must not diff: %v", d)
	}
	if d := diffValues(`{"a":1,"b":2}`, map[string]any{"b": 2, "a": 1}, 3); d != nil {
		t.Errorf("equivalent JSON must not diff: %v", d)
	}
	if d := diffValues("line1\nline2", "line1\nline3", 3); len(d) == 0 {
		t.Error("multi-line strings must diff")
	}
	if got := renderValue(`{"a": 1}`, 80); got != `{"a":1}` {
		t.Errorf("renderValue(json string) = %s", got)
	}
	if got := renderValue("plain", 80); got != `"plain"` {
		t.Errorf("renderValue(string) = %s", got)
	}
}

// TestRealRun drives every reporter from the real runner, with parallel
// workers, to catch anything the hand-built results miss.
func TestRealRun(t *testing.T) {
	ids := &syncIDs{}
	fx := parseFixtures(t, ids.NewID)
	reg := testRegistry(t)
	for _, id := range []string{"orders.resend", "health.panic", "health.slow", "health.skip", "orders.refunds", "rest.response.body"} {
		def, _ := reg.Def(id)
		switch id {
		case "orders.resend":
			def.Step.Run = func(sc *core.Scenario, _ core.Args) error {
				sc.Log("POST /orders -> 201")
				sc.Attach("application/json", []byte(`{"id":7}`), "response.json")
				return nil
			}
		case "health.panic":
			def.Step.Run = func(*core.Scenario, core.Args) error { panic("boom") }
		case "health.slow":
			def.Step.Timeout = 20 * time.Millisecond
			def.Step.Run = func(sc *core.Scenario, _ core.Args) error { <-sc.Context().Done(); return sc.Context().Err() }
		case "health.skip":
			def.Step.Run = func(*core.Scenario, core.Args) error { return core.ErrSkip }
		case "orders.refunds":
			def.Step.Run = func(*core.Scenario, core.Args) error { return core.ErrPending }
		case "rest.response.body":
			def.Step.Run = func(*core.Scenario, core.Args) error {
				return core.Fail("body", `{"error":"duplicate"}`, map[string]any{"id": 7})
			}
		}
	}
	bufs := map[string]*lockedBuffer{}
	var reps []runner.Reporter
	for _, n := range Names() {
		bufs[n] = &lockedBuffer{}
		rep, err := New(n, bufs[n], Options{Color: n == "pretty", Version: "0.1.0"})
		if err != nil {
			t.Fatal(err)
		}
		reps = append(reps, rep)
	}
	rec := &recorder{}
	r, err := runner.New(runner.Options{
		Registry: reg, Workers: 3, Messages: true, Docs: fx.docs, NewID: ids.NewID,
		Reporters: append(reps, rec), TimeoutGrace: 50 * time.Millisecond,
		Hooks: []runner.PackHook{{Pack: "db", Hook: core.Hook{ID: "db.reset", Phase: core.BeforeScenario}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	res := r.Run(context.Background(), fx.pickles)
	if res.Worst() != runner.Failed {
		t.Fatalf("worst = %v", res.Worst())
	}
	for _, n := range []string{"agent", "cucumber-json"} {
		if !json.Valid(bufs[n].Bytes()) {
			t.Errorf("%s: invalid JSON", n)
		}
	}
	assertWellFormedXML(t, bufs["junit"].Bytes())
	lines := strings.Split(strings.TrimSpace(bufs["messages"].String()), "\n")
	if len(lines) != len(rec.envelopes) {
		t.Errorf("messages: %d lines for %d envelopes", len(lines), len(rec.envelopes))
	}
	html := bufs["html"].String()
	for i, e := range rec.envelopes {
		b, _ := json.Marshal(e)
		if !strings.Contains(html, string(b)) {
			t.Errorf("html lacks envelope %d", i)
		}
	}
	for _, want := range []string{"panic: boom", "timeout: step timed out after 20ms", "did you mean:"} {
		if !strings.Contains(stripANSI(bufs["pretty"].String()), want) {
			t.Errorf("pretty output lacks %q", want)
		}
	}
	if !strings.Contains(bufs["compact"].String(), "FAIL features/health.feature:3  Panicky") {
		t.Errorf("compact output:\n%s", bufs["compact"].String())
	}
	checkTeamcity(t, bufs["teamcity"].String())
}

// checkTeamcity checks a teamcity report of the fixtures: every message
// parses, the tree is consistent although scenarios ran in parallel, and
// failures, output and ignored steps carry what the IDE shows.
func checkTeamcity(t *testing.T, out string) {
	t.Helper()
	open := map[string]string{} // node id -> kind while open
	seen := map[string]bool{}
	var all []tcMessage
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "##teamcity[") {
			continue
		}
		m, err := parseTC(line)
		if err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		all = append(all, m)
		id := m.attrs["nodeId"]
		switch m.name {
		case "testSuiteStarted", "testStarted":
			if seen[id] {
				t.Errorf("node %s started twice", id)
			}
			if p := m.attrs["parentNodeId"]; p != "0" && open[p] != "suite" {
				t.Errorf("node %s has parent %s, which is not an open suite", id, p)
			}
			if m.attrs["locationHint"] != "" && !strings.HasPrefix(m.attrs["locationHint"], "file:///") {
				t.Errorf("location %q", m.attrs["locationHint"])
			}
			seen[id] = true
			open[id] = map[bool]string{true: "suite", false: "test"}[m.name == "testSuiteStarted"]
		case "testSuiteFinished", "testFinished":
			if open[id] == "" {
				t.Errorf("node %s finished but not open", id)
			}
			delete(open, id)
		case "testFailed", "testIgnored", "testStdOut":
			if open[id] != "test" {
				t.Errorf("%s for %s, which is not an open test", m.name, id)
			}
		}
	}
	if len(open) != 0 {
		t.Errorf("nodes left open: %v", open)
	}
	if len(all) < 3 || all[0].name != "enteredTheMatrix" || all[len(all)-1].name != "testingFinished" {
		t.Errorf("framing: %v ... %v", all[0], all[len(all)-1])
	}
	has := func(name string, pred func(map[string]string) bool) bool {
		for _, m := range all {
			if m.name == name && pred(m.attrs) {
				return true
			}
		}
		return false
	}
	checks := map[string]bool{
		"comparison failure": has("testFailed", func(a map[string]string) bool {
			return a["type"] == "comparisonFailure" && a["expected"] == `{"error":"duplicate"}` && a["actual"] == `{"id":7}`
		}),
		"panic": has("testFailed", func(a map[string]string) bool { return a["message"] == "panic: boom" }),
		"undefined": has("testFailed", func(a map[string]string) bool {
			return strings.HasPrefix(a["message"], "Undefined step") && strings.Contains(a["details"], "did you mean")
		}),
		"output": has("testStdOut", func(a map[string]string) bool {
			return strings.Contains(a["out"], "log: POST /orders -> 201") && strings.Contains(a["out"], "response.json")
		}),
		"pending":  has("testIgnored", func(a map[string]string) bool { return a["message"] == "pending" }),
		"skipped":  has("testIgnored", func(a map[string]string) bool { return a["message"] == "skipped" }),
		"example":  has("testSuiteStarted", func(a map[string]string) bool { return strings.HasSuffix(a["name"], "(example 2)") }),
		"no hooks": !has("testStarted", func(a map[string]string) bool { return strings.Contains(a["name"], "hook") }),
	}
	for what, ok := range checks {
		if !ok {
			t.Errorf("teamcity: no %s\n%s", what, out)
		}
	}
}

type tcMessage struct {
	name  string
	attrs map[string]string
}

// parseTC parses one service message, undoing TeamCity escaping.
func parseTC(line string) (tcMessage, error) {
	body, ok := strings.CutPrefix(line, "##teamcity[")
	if !ok || !strings.HasSuffix(body, "]") {
		return tcMessage{}, errors.New("not a service message")
	}
	body = strings.TrimSuffix(body, "]")
	name, rest, _ := strings.Cut(body, " ")
	m := tcMessage{name: name, attrs: map[string]string{}}
	for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
		key, after, ok := strings.Cut(rest, "='")
		if !ok {
			return m, fmt.Errorf("bad attribute in %q", rest)
		}
		var val strings.Builder
		i := 0
		for ; i < len(after); i++ {
			c := after[i]
			if c == '|' && i+1 < len(after) {
				i++
				val.WriteByte(map[byte]byte{'n': '\n', 'r': '\r', '|': '|', '\'': '\'', '[': '[', ']': ']'}[after[i]])
				continue
			}
			if c == '\'' {
				break
			}
			val.WriteByte(c)
		}
		if i >= len(after) {
			return m, fmt.Errorf("unterminated value for %s", key)
		}
		m.attrs[key] = val.String()
		rest = after[i+1:]
	}
	return m, nil
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

func (b *lockedBuffer) String() string { return string(b.Bytes()) }

type recorder struct {
	mu        sync.Mutex
	envelopes []*messages.Envelope
}

func (r *recorder) Envelope(e *messages.Envelope) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.envelopes = append(r.envelopes, e)
}

func (r *recorder) ScenarioFinished(*runner.ScenarioResult) {}

func (r *recorder) Finish(*runner.RunResult) error { return nil }

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"features/x.feature:14":      "features/x.feature:14",
		"features/my file.feature:3": "'features/my file.feature:3'",
		"features/it's.feature:3":    `'features/it'\''s.feature:3'`,
		"":                           "''",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestCompactAssertionShapes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{core.Fail("status", 409, 201), "status: expected 409, got 201"},
		{core.Failf("plain failure"), "plain failure"},
		{core.Fail("two\nlines", "a", "b"), "two\nlines\nexpected \"a\", got \"b\""},
		{core.Fail("text", "a\nb", "a\nc"), "text\ndiff -expected +actual\n@@ -1,2 +1,2 @@\n a\n-b\n+c"},
	} {
		f := describe(&runner.StepResult{Status: runner.Failed, Err: tc.err})
		if got := strings.Join(f.compactLines(""), "\n"); got != tc.want {
			t.Errorf("%v:\n got %q\nwant %q", tc.err, got, tc.want)
		}
	}
}

func TestRunErrorsAreReported(t *testing.T) {
	res := &runner.RunResult{
		Duration:  1500 * time.Millisecond,
		RunErrors: []runner.RunError{{Source: "mock", Message: "Calls to the mocked addresses service broke its contract:\n  GET /v1/postcodes/DE/00012"}},
	}
	pretty := string(render(t, "pretty", testOptions(false), nil, res))
	if !strings.Contains(pretty, "Run failures:\n  x Calls to the mocked addresses service broke its contract:  (mock)\n        GET /v1/postcodes/DE/00012\n") {
		t.Errorf("pretty:\n%s", pretty)
	}
	compact := string(render(t, "compact", testOptions(false), nil, res))
	if !strings.Contains(compact, "FAIL run  mock\n  Calls to the mocked") || !strings.Contains(compact, "RUN FAIL 1") {
		t.Errorf("compact:\n%s", compact)
	}
	junit := string(render(t, "junit", testOptions(false), nil, res))
	if !strings.Contains(junit, `<testsuite name="axx run"`) || !strings.Contains(junit, `failures="1"`) || !strings.Contains(junit, `type="RunError"`) {
		t.Errorf("junit:\n%s", junit)
	}
	var agent AgentReport
	if err := json.Unmarshal(render(t, "agent", testOptions(false), nil, res), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.Result != "failed" || len(agent.RunErrors) != 1 || agent.RunErrors[0].Source != "mock" {
		t.Errorf("agent: %+v", agent)
	}
}
