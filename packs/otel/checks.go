package otel

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

func sortedKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }

// condition is a row of a `where:` table.
type condition struct {
	field string
	// attribute is the key after `attribute `.
	attribute string
	value     core.Value
}

// readWhere reads a table's rows; fields are the rows it takes besides attributes.
func readWhere(t *core.Table, fields ...string) ([]condition, error) {
	if t == nil {
		return nil, nil
	}
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	var out []condition
	for _, p := range pairs {
		v, err := core.ParseValue(p.Value, p.Null, true)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Key, err)
		}
		c := condition{field: p.Key, value: v}
		if key, ok := strings.CutPrefix(p.Key, "attribute "); ok {
			c.field, c.attribute = "attribute", strings.TrimSpace(key)
		} else if !slices.Contains(fields, p.Key) {
			return nil, fmt.Errorf("unknown row %q (supported: %s, attribute <key>)", p.Key, strings.Join(fields, ", "))
		}
		out = append(out, c)
	}
	return out, nil
}

// holds compares a value that is there, as JSON, by the rule (ADR 0014).
func (c condition) holds(actual string, hints *[]string) bool {
	if c.value.Undefined() {
		return false
	}
	same, hint := jsonassert.Strict(c.label(), actual, c.value)
	if hint != "" && !slices.Contains(*hints, hint) {
		*hints = append(*hints, hint)
	}
	return same
}

func (c condition) label() string {
	if c.attribute != "" {
		return "attribute " + c.attribute
	}
	return c.field
}

// attributeHolds compares an attribute: `undefined` holds of one that is not there.
func (c condition) attributeHolds(attrs map[string]string, hints *[]string) bool {
	actual, ok := attrs[c.attribute]
	if !ok {
		return c.value.Undefined()
	}
	return c.holds(actual, hints)
}

func textJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (c condition) spanHolds(s Span, hints *[]string) bool {
	switch c.field {
	case "name":
		return c.holds(textJSON(s.Name), hints)
	case "status":
		return c.holds(textJSON(s.Status), hints)
	case "kind":
		return c.holds(textJSON(s.Kind), hints)
	case "service":
		return c.holds(textJSON(s.Service), hints)
	}
	return c.attributeHolds(s.Attributes, hints)
}

func (c condition) logHolds(l Log, hints *[]string) bool {
	switch c.field {
	case "severity":
		return c.holds(textJSON(l.Severity), hints)
	case "body":
		want, ok := c.value.Str()
		return ok && strings.Contains(l.Body, want)
	case "service":
		return c.holds(textJSON(l.Service), hints)
	}
	return c.attributeHolds(l.Attributes, hints)
}

func matchSpan(conds []condition, s Span, hints *[]string) bool {
	for _, c := range conds {
		if !c.spanHolds(s, hints) {
			return false
		}
	}
	return true
}

func matchLog(conds []condition, l Log, hints *[]string) bool {
	for _, c := range conds {
		if !c.logHolds(l, hints) {
			return false
		}
	}
	return true
}

func hintText(hints []string) string {
	if len(hints) == 0 {
		return ""
	}
	return " (" + strings.Join(hints, "; ") + ")"
}

// emptyTraceHelp says why a scenario's trace can be empty.
const emptyTraceHelp = "no span has this scenario's trace ID: does the service read the traceparent header (W3C Trace Context), export to the Collector, and sample what its parent sampled? The Collector's file shows what has arrived"

func spanLines(spans []Span) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = spanLine(s)
	}
	return out
}

func traceHasSpan(sc *core.Scenario, a core.Args) error {
	st := Context(sc)
	if err := st.needTrace(); err != nil {
		return err
	}
	conds, err := readWhere(a.Table, "name", "status", "kind", "service")
	if err != nil {
		return err
	}
	var hints []string
	d := within(a)
	err = waitFor(sc, d, st, func() (bool, error) {
		return slices.ContainsFunc(st.Spans(), func(s Span) bool { return matchSpan(conds, s, &hints) }), nil
	})
	if !errors.Is(err, errTimeUp) {
		return err
	}
	spans := st.Spans()
	if len(spans) == 0 {
		return core.Failf("this scenario's trace (%s) has no such span within %s: %s", st.traceID, d, emptyTraceHelp)
	}
	return core.Failf("this scenario's trace has no such span within %s%s; it has: %s", d, hintText(hints), seenList(spanLines(spans)))
}

func traceHasNoSpan(sc *core.Scenario, a core.Args) error {
	st := Context(sc)
	if err := st.needTrace(); err != nil {
		return err
	}
	conds, err := readWhere(a.Table, "name", "status", "kind", "service")
	if err != nil {
		return err
	}
	if err := st.refresh(); err != nil {
		return err
	}
	var hints, found []string
	for _, s := range st.Spans() {
		if matchSpan(conds, s, &hints) {
			found = append(found, spanLine(s))
		}
	}
	if len(found) > 0 {
		return core.Failf("this scenario's trace should have no such span; it has: %s", seenList(found))
	}
	return nil
}

func spansNamed(spans []Span, name string) []Span {
	var out []Span
	for _, s := range spans {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func spanIsChild(sc *core.Scenario, a core.Args) error {
	st := Context(sc)
	if err := st.needTrace(); err != nil {
		return err
	}
	child, parent := a.String(0), a.String(1)
	err := waitFor(sc, defaultWait, st, func() (bool, error) {
		spans := st.Spans()
		parents := spansNamed(spans, parent)
		return slices.ContainsFunc(spansNamed(spans, child), func(c Span) bool {
			return slices.ContainsFunc(parents, func(p Span) bool { return c.ParentSpanID == p.SpanID })
		}), nil
	})
	if !errors.Is(err, errTimeUp) {
		return err
	}
	spans := st.Spans()
	if len(spans) == 0 {
		return core.Failf("the %q span is not a child of the %q span: %s", child, parent, emptyTraceHelp)
	}
	return core.Failf("the %q span is not a child of the %q span within %s; the trace has: %s", child, parent, defaultWait, seenList(spanLines(spans)))
}

func spanTook(sc *core.Scenario, a core.Args) error {
	st := Context(sc)
	if err := st.needTrace(); err != nil {
		return err
	}
	name := a.String(0)
	limit := a.Value(1).(time.Duration)
	err := waitFor(sc, defaultWait, st, func() (bool, error) { return len(spansNamed(st.Spans(), name)) > 0, nil })
	if errors.Is(err, errTimeUp) {
		spans := st.Spans()
		if len(spans) == 0 {
			return core.Failf("no %q span in this scenario's trace: %s", name, emptyTraceHelp)
		}
		return core.Failf("no %q span in this scenario's trace within %s; it has: %s", name, defaultWait, seenList(spanLines(spans)))
	}
	if err != nil {
		return err
	}
	for _, s := range spansNamed(st.Spans(), name) {
		if s.Duration() >= limit {
			return core.Fail(fmt.Sprintf("the %q span took too long", name), "less than "+limit.String(), s.Duration().String())
		}
	}
	return nil
}

func traceHasLog(sc *core.Scenario, a core.Args) error {
	st := Context(sc)
	if err := st.needTrace(); err != nil {
		return err
	}
	conds, err := readWhere(a.Table, "severity", "body", "service")
	if err != nil {
		return err
	}
	var hints []string
	d := within(a)
	err = waitFor(sc, d, st, func() (bool, error) {
		return slices.ContainsFunc(st.Logs(), func(l Log) bool { return matchLog(conds, l, &hints) }), nil
	})
	if !errors.Is(err, errTimeUp) {
		return err
	}
	logs := st.Logs()
	lines := make([]string, len(logs))
	for i, l := range logs {
		lines[i] = fmt.Sprintf("%s %s (%s)%s", l.Severity, l.Body, l.Service, attributeText(l.Attributes))
	}
	if len(lines) == 0 {
		return core.Failf("this scenario's trace has no such log within %s, and no log at all: does the service log with the span's context, and export its logs to the Collector?", d)
	}
	return core.Failf("this scenario's trace has no such log within %s%s; it has: %s", d, hintText(hints), seenList(lines))
}

// metricHelp is why a metric may not have arrived.
const metricHelp = "SDKs export metrics every 60 seconds by default: set OTEL_METRIC_EXPORT_INTERVAL, in milliseconds, in the service's test environment"

// seriesOf are the telemetry's points of a metric that match, by series, in the file's
// order, whatever came before the scenario too.
func (t *Telemetry) seriesOf(metric string, kind metricKind, conds []condition, hints *[]string) map[string][]point {
	out := map[string][]point{}
	points := t.store.pointsOf(func(p point) bool { return p.metric == metric && p.kind == kind })
	for _, p := range points {
		if t.service != "" && p.service != t.service {
			continue
		}
		matches := true
		for _, c := range conds {
			var holds bool
			if c.field == "service" {
				holds = c.holds(textJSON(p.service), hints)
			} else {
				holds = c.attributeHolds(p.attributes, hints)
			}
			if !holds {
				matches = false
				break
			}
		}
		if matches {
			out[p.series] = append(out[p.series], p)
		}
	}
	return out
}

// sinceStart is what a series has added since the scenario registered the telemetry: a
// delta's points add up, and a cumulative's last point less its last before then. A
// cumulative series that started again (its start time changed) counts from nothing.
func (t *Telemetry) sinceStart(points []point) float64 {
	var total, baseline float64
	var baselineStart string
	var last *point
	for i, p := range points {
		if p.seq <= t.from {
			baseline, baselineStart = p.number(), p.start
			continue
		}
		if !p.cumulative {
			total += p.number()
			continue
		}
		last = &points[i]
	}
	if last != nil {
		if baselineStart != "" && baselineStart != last.start {
			baseline = 0
		}
		total += last.number() - baseline
	}
	return total
}

// count checks a counter's or a histogram's count.
func count(sc *core.Scenario, a core.Args, kind metricKind, verb string) error {
	t := a.Value(1).(*Telemetry)
	want, metric := a.Int(2), a.String(3)
	conds, err := readWhere(a.Table, "service")
	if err != nil {
		return err
	}
	st := Context(sc)
	var got float64
	var hints []string
	wants := float64(want)
	d := within(a)
	if want == 0 && !a.Present(0) {
		d = 0
	}
	err = waitFor(sc, d, st, func() (bool, error) {
		got = 0
		for _, points := range t.seriesOf(metric, kind, conds, &hints) {
			got += t.sinceStart(points)
		}
		if got > wants {
			return false, core.Fail(fmt.Sprintf("the %s telemetry %s more %s than expected", t.name, verb, metric), want, got)
		}
		return got == wants && want > 0, nil
	})
	if errors.Is(err, errTimeUp) {
		if want == 0 && got == 0 {
			return nil
		}
		return core.Fail(fmt.Sprintf("the %s telemetry %s %s %s within %s%s; %s; it has: %s", t.name, verb, plural(want, "value"), metric, d, hintText(hints), metricHelp,
			seenList(t.metricsSeen())), want, got)
	}
	return err
}

// metricsSeen says what a failure shows of the telemetry's metrics.
func (t *Telemetry) metricsSeen() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range t.store.pointsOf(func(p point) bool { return p.seq > t.from }) {
		line := fmt.Sprintf("%s (%s)%s", p.metric, p.kind, attributeText(p.attributes))
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out
}

func counted(sc *core.Scenario, a core.Args) error {
	return count(sc, a, kindCounter, "counted")
}

func recorded(sc *core.Scenario, a core.Args) error {
	return count(sc, a, kindHistogram, "recorded")
}

func gauge(sc *core.Scenario, a core.Args) error {
	t := a.Value(1).(*Telemetry)
	metric := a.String(2)
	want, err := core.ParseValue(a.String(3), false, false)
	if err != nil {
		return err
	}
	conds, err := readWhere(a.Table, "service")
	if err != nil {
		return err
	}
	st := Context(sc)
	var hints []string
	last := ""
	d := within(a)
	err = waitFor(sc, d, st, func() (bool, error) {
		latest := -1
		for _, points := range t.seriesOf(metric, kindGauge, conds, &hints) {
			if p := points[len(points)-1]; p.seq > t.from && p.seq > latest {
				latest, last = p.seq, p.value
			}
		}
		if latest < 0 {
			return false, nil
		}
		same, hint := jsonassert.Strict(metric, last, want)
		if hint != "" && !slices.Contains(hints, hint) {
			hints = append(hints, hint)
		}
		return same, nil
	})
	if errors.Is(err, errTimeUp) {
		if last == "" {
			last = "nothing"
		}
		return core.Fail(fmt.Sprintf("the %s telemetry's %s gauge is not %s within %s%s; %s; it has: %s", t.name, metric, a.String(3), d, hintText(hints), metricHelp,
			seenList(t.metricsSeen())), a.String(3), last)
	}
	return err
}

func notContains(sc *core.Scenario, a core.Args) error {
	st := Context(sc)
	all := st.telemetry.All()
	if len(all) == 0 {
		_, err := st.telemetry.Default()
		return err
	}
	if err := st.refresh(); err != nil {
		return err
	}
	text := a.String(0)
	escaped := strings.Trim(textJSON(text), `"`)
	lines := 0
	for _, t := range all {
		for _, line := range t.store.linesSince(t.from) {
			lines++
			if strings.Contains(line, text) || strings.Contains(line, escaped) {
				return core.Failf("the %s telemetry contains %q", t.name, text)
			}
		}
	}
	if lines == 0 {
		return core.Failf("nothing has arrived since the telemetry was registered, so this looked at nothing: put it after a check that waits")
	}
	return nil
}
