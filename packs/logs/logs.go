// Package logs checks what services logged: a positive way to prove that
// something did not happen, by asserting the entry that says so ("line
// ML-KES-0413-1 rejected: duplicate reference"). A log is a file axx reads
// or an address axx listens on (see source.go); patterns are regular
// expressions matched over everything the log received since the scenario
// registered it.
package logs

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/javare"
)

const (
	defaultWait = 10 * time.Second
	pollEvery   = 100 * time.Millisecond
)

const packDoc = `Assert the entries your services log, read from files or sent to axx over UDP, TCP or HTTP.

A **log** is where a service's log lines are: a file axx reads, or an address axx listens on while services send their lines to it. Register it with ` + "`the {word} log with the following properties:`" + `, then assert the entries a scenario must produce. Only what the log received after the scenario registered it counts, and every step waits for its entries (10 seconds unless ` + "`within {duration}`" + ` says otherwise).

Use logs to prove that something did **not** happen: have the service log its decision ("line ML-KES-0413-1 rejected: duplicate reference") and assert that entry, rather than waiting and hoping nothing arrives. Match on data unique to the scenario (a reference, an id): scenarios run in parallel and share the log.

**The url** says where the lines are:

| url | axx |
| --- | --- |
| ` + "`file:///var/log/app.log`" + `, ` + "`file://logs/app.log`" + ` | reads what is appended to the file (relative to axx.yaml); ` + "`file://.axx/logs/apps.log`" + ` is the console of the apps axx starts |
| ` + "`udp://0.0.0.0:5140`" + ` | listens; each datagram is one or more lines (e.g. Docker's syslog log driver, an app's syslog handler) |
| ` + "`tcp://0.0.0.0:5150`" + ` | listens; newline-delimited or octet-counted (RFC 6587) messages |
| ` + "`http://0.0.0.0:5160/logs`" + `, ` + "`https://...`" + ` | listens; the body of each POST or PUT to that path (e.g. a Fluent Bit or Vector http output). https uses a self-signed certificate |

axx opens listeners before it starts the apps, for the log steps of the scenarios in the run, so services can send from the start; ` + "`axx up`" + ` keeps them open between runs. Services in containers reach them at ` + "`host.docker.internal`" + `.

**Patterns** are regular expressions (Java syntax), searched in the log's text, not matched against whole lines: ` + "`^`" + ` and ` + "`$`" + ` match at line boundaries, every match counts, and a pattern can span lines (` + "`\\n`" + `, or ` + "`(?s)`" + ` to let ` + "`.`" + ` match newlines), which covers multi-line entries such as stack traces.`

// Pack returns the logs pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var _ core.Preparer = pack{}

// Prepare opens the listeners the run's log steps name, before the apps
// start.
func (pack) Prepare(_ context.Context, s *core.Suite, plan *core.Plan) error {
	l := runListeners(s)
	for _, st := range plan.Steps("logs.log") {
		raw, err := urlProperty(st.Table)
		if err != nil {
			continue // the step reports it when it runs
		}
		src, err := parseSource(s.ProjectDir(), s.Interpolate(raw))
		if err != nil || !src.network() {
			continue
		}
		if err := l.ensure(src); err != nil {
			return err
		}
	}
	return nil
}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      "logs",
		Namespace: "logs",
		Doc:       packDoc,
		Steps:     steps(),
	}
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "logs.log", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the {word} log with the following properties:",
			Doc: "Register a log under a name. Properties: `url` (required; `file://`, `udp://`, `tcp://`, `http://` or `https://`, " +
				"`${env:..}`/`${sys:..}` are expanded). Assertions only look at what the log receives from now on.",
			Examples:   []string{"Given the parcels log with the following properties:"},
			TableTypes: map[string]string{"url": "url"},
			Run:        addLog,
		},
		{
			ID: "logs.entry", Keyword: "Then",
			Expr: "[[within {duration} ]]the {word} log has an entry matching {string}",
			Doc: "Wait (10s, or the given time) until the log has a match for the regular expression. The pattern is searched in " +
				"the log's text: `^` and `$` match at line boundaries and a pattern can span lines.",
			Examples: []string{
				"Then the parcels log has an entry matching 'registration refused reference=PX-EVT-4003'",
				"Then within 30s the parcels log has an entry matching 'manifest line processed line=ML-MAP-0019-3 status=IMPORTED'",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				lg, err := lookup(sc, a.String(1))
				if err != nil {
					return err
				}
				return expect(sc, wait(a), []want{{log: lg, pattern: a.String(2)}})
			},
		},
		{
			ID: "logs.entries", Keyword: "Then", Arg: core.ArgTable,
			Expr: "[[within {duration} ]]the {word} log has entries matching:",
			Doc: "Wait until the log has a match for every regular expression in the table (one per row). Each row needs a match " +
				"of its own: the same pattern in two rows needs two matches.",
			Examples: []string{"Then the parcels log has entries matching:"},
			Run: func(sc *core.Scenario, a core.Args) error {
				lg, err := lookup(sc, a.String(1))
				if err != nil {
					return err
				}
				var ws []want
				for _, row := range a.Table.Rows {
					if len(row) != 1 {
						return fmt.Errorf("the table must have one column (a regular expression per row), got %d", len(row))
					}
					ws = append(ws, want{log: lg, pattern: row[0]})
				}
				return expect(sc, wait(a), ws)
			},
		},
		{
			ID: "logs.count", Keyword: "Then",
			Expr: "[[within {duration} ]]the {word} log has {int} entry/entries matching {string}",
			Doc: "Wait until the log has the given number of matches for the regular expression, for example one per retry. " +
				"More matches than that fail the step.",
			Examples: []string{"Then the parcels log has 2 entries matching 'storing parcel PX-DBF-3002 failed, retrying'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				lg, err := lookup(sc, a.String(1))
				if err != nil {
					return err
				}
				return expectCount(sc, wait(a), lg, a.Int(2), a.String(3))
			},
		},
		{
			ID: "logs.across", Keyword: "Then", Arg: core.ArgTable,
			Expr: "[[within {duration} ]]the logs have entries matching:",
			Doc: "Wait until every log in the table has a match for its regular expression (`log | pattern` rows), for example the " +
				"service's own entry and its dependency's. Each row needs a match of its own.",
			Examples: []string{"Then the logs have entries matching:"},
			Run: func(sc *core.Scenario, a core.Args) error {
				var ws []want
				for _, row := range a.Table.Rows {
					if len(row) != 2 {
						return fmt.Errorf("the table must have two columns (log | regular expression), got %d", len(row))
					}
					lg, err := lookup(sc, row[0])
					if err != nil {
						return err
					}
					ws = append(ws, want{log: lg, pattern: row[1]})
				}
				return expect(sc, wait(a), ws)
			},
		},
	}
}

func wait(a core.Args) time.Duration {
	if a.Present(0) {
		return a.Value(0).(time.Duration)
	}
	return defaultWait
}

// Log is a log registered in a scenario.
type Log struct {
	Name string
	URL  string
	// File is the file axx reads (for network logs, the one its listener
	// writes).
	File string

	start int64  // where the scenario's part of the file begins
	pos   int64  // how far it has been read
	text  []byte // what was read since start
}

// ScenarioContext holds the logs registered in a scenario.
type ScenarioContext struct {
	logs map[string]*Log
}

var stateKey = core.NewStateKey("logs", func(*core.Scenario) *ScenarioContext {
	return &ScenarioContext{logs: map[string]*Log{}}
}, nil)

// Context returns the logs pack's context for a scenario.
func Context(sc *core.Scenario) *ScenarioContext { return stateKey.Of(sc) }

// Log returns the log registered under name.
func (c *ScenarioContext) Log(name string) (*Log, bool) {
	l, ok := c.logs[name]
	return l, ok
}

func lookup(sc *core.Scenario, name string) (*Log, error) {
	if l, ok := Context(sc).Log(name); ok {
		return l, nil
	}
	return nil, fmt.Errorf("no log named %q in this scenario; register it first with \"the %s log with the following properties:\"", name, name)
}

func urlProperty(t *core.Table) (string, error) {
	if t == nil {
		return "", fmt.Errorf(`Property "url" is required`) //nolint:staticcheck // user-facing message
	}
	pairs, err := t.Pairs()
	if err != nil {
		return "", err
	}
	for _, p := range pairs {
		switch p.Key {
		case "url":
			return p.Value, nil
		default:
			return "", fmt.Errorf("unknown log property %q (supported: url)", p.Key)
		}
	}
	return "", fmt.Errorf(`Property "url" is required`) //nolint:staticcheck // user-facing message
}

func addLog(sc *core.Scenario, a core.Args) error {
	name := a.String(0)
	st := Context(sc)
	if _, ok := st.logs[name]; ok {
		return fmt.Errorf("a log named %q is already registered in this scenario", name)
	}
	raw, err := urlProperty(a.Table)
	if err != nil {
		return err
	}
	s := sc.Suite()
	src, err := parseSource(s.ProjectDir(), s.Interpolate(raw))
	if err != nil {
		return err
	}
	if src.network() {
		// Normally opened before the apps started (Prepare); this covers logs
		// registered outside a planned run, e.g. by a custom step.
		if err := runListeners(s).ensure(src); err != nil {
			return err
		}
	}
	lg := &Log{Name: name, URL: raw, File: src.file(s.ProjectDir())}
	if fi, err := os.Stat(lg.File); err == nil {
		lg.start, lg.pos = fi.Size(), fi.Size()
	}
	st.logs[name] = lg
	return nil
}

// read appends what the file received since the last read. A file that
// shrank was rotated or truncated: it is read again from its start.
func (l *Log) read() error {
	f, err := os.Open(l.File)
	if os.IsNotExist(err) {
		return nil // not written yet
	}
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() < l.pos {
		l.start, l.pos, l.text = 0, 0, nil
	}
	if fi.Size() == l.pos {
		return nil
	}
	if _, err := f.Seek(l.pos, io.SeekStart); err != nil {
		return err
	}
	b, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil {
		return err
	}
	l.text = append(l.text, b...)
	l.pos += int64(len(b))
	return nil
}

// Text returns what the log received since the scenario registered it (as
// far as it has been read).
func (l *Log) Text() string { return string(l.text) }

// want is one pattern a log must match.
type want struct {
	log     *Log
	pattern string
	re      *javare.Regexp
}

type span struct{ start, end int }

func compile(ws []want) error {
	for i := range ws {
		re, err := javare.CompileFlags(ws[i].pattern, javare.Multiline)
		if err != nil {
			return fmt.Errorf("invalid regular expression %q: %w", ws[i].pattern, err)
		}
		ws[i].re = re
	}
	return nil
}

// matches returns the spans of every match of re in text.
func matches(re *javare.Regexp, text string) ([]span, error) {
	ms, err := re.FindAll(text)
	if err != nil {
		return nil, err
	}
	out := make([]span, 0, len(ms))
	for _, m := range ms {
		out = append(out, span{m.Start[0], m.End[0]})
	}
	return out, nil
}

// poll reads the logs and calls check until it reports done or the time is
// up; check's last message explains a failure.
func poll(sc *core.Scenario, d time.Duration, logs []*Log, check func() (bool, string, error)) error {
	deadline := time.Now().Add(d)
	for {
		for _, l := range logs {
			if err := l.read(); err != nil {
				return err
			}
		}
		done, msg, err := check()
		if err != nil || done {
			return err
		}
		if !time.Now().Before(deadline) {
			return core.Failf("%s", msg)
		}
		select {
		case <-sc.Context().Done():
			return sc.Context().Err()
		case <-time.After(pollEvery):
		}
	}
}

// expect waits until every want has a match of its own (per log, a match
// satisfies one row only).
func expect(sc *core.Scenario, d time.Duration, ws []want) error {
	if err := compile(ws); err != nil {
		return err
	}
	logs := distinctLogs(ws)
	return poll(sc, d, logs, func() (bool, string, error) {
		missing := []want{}
		for _, lg := range logs {
			var rows []want
			for _, w := range ws {
				if w.log == lg {
					rows = append(rows, w)
				}
			}
			unmatched, err := assign(lg.Text(), rows)
			if err != nil {
				return false, "", err
			}
			missing = append(missing, unmatched...)
		}
		if len(missing) == 0 {
			return true, "", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "No entry matched within %s:", fmtDuration(d))
		for _, w := range missing {
			fmt.Fprintf(&b, "\n  %s log: %s", w.log.Name, w.pattern)
		}
		for _, lg := range logs {
			b.WriteString("\n" + tail(lg))
		}
		return false, b.String(), nil
	})
}

func expectCount(sc *core.Scenario, d time.Duration, lg *Log, n int, pattern string) error {
	ws := []want{{log: lg, pattern: pattern}}
	if err := compile(ws); err != nil {
		return err
	}
	got := 0
	return poll(sc, d, []*Log{lg}, func() (bool, string, error) {
		spans, err := matches(ws[0].re, lg.Text())
		if err != nil {
			return false, "", err
		}
		got = len(spans)
		msg := fmt.Sprintf("The %s log has %d %s matching %s, want %d (waited %s).\n%s",
			lg.Name, got, plural(got, "entry", "entries"), pattern, n, fmtDuration(d), tail(lg))
		switch {
		case got > n:
			return false, "", core.Fail(msg, n, got)
		case got == n:
			return true, "", nil
		}
		return false, msg, nil
	})
}

func distinctLogs(ws []want) []*Log {
	var out []*Log
	seen := map[*Log]bool{}
	for _, w := range ws {
		if !seen[w.log] {
			seen[w.log] = true
			out = append(out, w.log)
		}
	}
	return out
}

// assign gives every row a match of its own (a bipartite matching of rows
// to match spans) and returns the rows left without one.
func assign(text string, rows []want) ([]want, error) {
	cands := make([][]span, len(rows))
	for i, w := range rows {
		spans, err := matches(w.re, text)
		if err != nil {
			return nil, err
		}
		cands[i] = spans
	}
	owner := map[span]int{}
	var try func(row int, seen map[span]bool) bool
	try = func(row int, seen map[span]bool) bool {
		for _, s := range cands[row] {
			if seen[s] {
				continue
			}
			seen[s] = true
			cur, taken := owner[s]
			if !taken || try(cur, seen) {
				owner[s] = row
				return true
			}
		}
		return false
	}
	var missing []want
	for i := range rows {
		if !try(i, map[span]bool{}) {
			missing = append(missing, rows[i])
		}
	}
	return missing, nil
}

// tail shows the last lines a log received in this scenario.
func tail(lg *Log) string {
	text := strings.TrimRight(lg.Text(), "\n")
	if text == "" {
		return fmt.Sprintf("The %s log (%s) received nothing since the scenario registered it.", lg.Name, lg.URL)
	}
	lines := strings.Split(text, "\n")
	const show = 15
	var b strings.Builder
	fmt.Fprintf(&b, "The %s log (%s) received %d %s since the scenario registered it", lg.Name, lg.URL, len(lines), plural(len(lines), "line", "lines"))
	if len(lines) > show {
		fmt.Fprintf(&b, "; the last %d:", show)
		lines = lines[len(lines)-show:]
	} else {
		b.WriteString(":")
	}
	for _, l := range lines {
		if r := []rune(l); len(r) > 300 {
			l = string(r[:300]) + "…"
		}
		b.WriteString("\n  " + l)
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func fmtDuration(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.String()
}
