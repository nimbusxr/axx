package otel

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

const pollEvery = 200 * time.Millisecond

var errTimeUp = errors.New("time is up")

// Telemetry is what a Collector's file exporter writes, as a scenario sees it.
type Telemetry struct {
	name, service, path string
	store               *store
	// from is where the file stood when the scenario registered it.
	from int
}

// ScenarioContext is the pack's view of a running scenario.
type ScenarioContext struct {
	telemetry *core.Services[*Telemetry]
	traceID   string
}

var stateKey = core.NewStateKey(Name, func(sc *core.Scenario) *ScenarioContext {
	st := &ScenarioContext{
		telemetry: core.NewServices[*Telemetry]("Telemetry", "No telemetry set").RegisteredBy("the {word} telemetry with the following properties:"),
	}
	sc.Describe(Name, st.describe)
	return st
}, nil)

// Context returns the pack's context for a running scenario.
func Context(sc *core.Scenario) *ScenarioContext { return stateKey.Of(sc) }

// TraceID is the trace the scenario's requests carry, once telemetry is registered.
func (st *ScenarioContext) TraceID() string { return st.traceID }

// Refresh reads what the Collector has written since the last look.
func (st *ScenarioContext) Refresh() error { return st.refresh() }

func (st *ScenarioContext) refresh() error {
	for _, t := range st.telemetry.All() {
		if err := t.store.refresh(); err != nil {
			return fmt.Errorf("could not read the %s telemetry: %w", t.name, err)
		}
	}
	return nil
}

// Spans are the spans of the scenario's trace that have arrived, once each: two
// telemetries can read one file.
func (st *ScenarioContext) Spans() []Span {
	var out []Span
	for _, s := range st.stores() {
		out = append(out, s.allSpans(func(sp Span) bool { return sp.TraceID == st.traceID })...)
	}
	return out
}

// Logs are the log records of the scenario's trace that have arrived.
func (st *ScenarioContext) Logs() []Log {
	var out []Log
	for _, s := range st.stores() {
		out = append(out, s.allLogs(func(l Log) bool { return l.TraceID == st.traceID })...)
	}
	return out
}

func (st *ScenarioContext) stores() []*store {
	var out []*store
	seen := map[*store]bool{}
	for _, t := range st.telemetry.All() {
		if !seen[t.store] {
			seen[t.store] = true
			out = append(out, t.store)
		}
	}
	return out
}

// describe is the scenario's trace, for the report of a failure.
func (st *ScenarioContext) describe() any {
	if st.traceID == "" {
		return nil
	}
	_ = st.refresh()
	out := map[string]any{"traceId": st.traceID}
	var spans []string
	for _, s := range st.Spans() {
		spans = append(spans, spanLine(s))
	}
	if len(spans) > 0 {
		out["spans"] = spans
	}
	return out
}

// spanLine says what a failure shows of a span.
func spanLine(s Span) string {
	return fmt.Sprintf("%s (%s, %s, %s, %s)%s", s.Name, s.Service, s.Kind, s.Status, s.Duration().Round(time.Millisecond), attributeText(s.Attributes))
}

func attributeText(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attrs))
	for _, k := range sortedKeys(attrs) {
		parts = append(parts, k+"="+attrs[k])
	}
	return " " + strings.Join(parts, " ")
}

func register(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	t := &Telemetry{name: a.String(0)}
	raw := ""
	for _, p := range pairs {
		v := strings.TrimSpace(sc.Suite().Interpolate(p.Value))
		switch p.Key {
		case "url":
			raw = v
		case "service":
			t.service = v
		default:
			return fmt.Errorf("unknown telemetry property %q (supported: url, service)", p.Key)
		}
	}
	if raw == "" {
		return errors.New(`the telemetry property "url" is required`)
	}
	rest, ok := strings.CutPrefix(raw, "file://")
	if !ok || rest == "" {
		return fmt.Errorf("telemetry's url is the file a Collector's file exporter writes, file://..., not %q", raw)
	}
	t.path = filepath.FromSlash(rest)
	if !filepath.IsAbs(t.path) {
		t.path = filepath.Join(sc.Suite().ProjectDir(), t.path)
	}
	path := t.path
	t.store, err = core.Cached(sc.Suite(), "otel.store:"+path, func() (*store, error) { return &store{path: path}, nil })
	if err != nil {
		return err
	}
	if err := t.store.refresh(); err != nil {
		return fmt.Errorf("could not read the %s telemetry: %w", t.name, err)
	}
	t.from = t.store.position()
	st := Context(sc)
	if err := st.telemetry.Add(t.name, t); err != nil {
		return err
	}
	if st.traceID == "" {
		trace := make([]byte, 16)
		span := make([]byte, 8)
		_, _ = rand.Read(trace)
		_, _ = rand.Read(span)
		st.traceID = hex.EncodeToString(trace)
		sc.SetRequestHeader("traceparent", "00-"+st.traceID+"-"+hex.EncodeToString(span)+"-01")
	}
	sc.Log("registered the %s telemetry: %s; this scenario's trace is %s", t.name, t.path, st.traceID)
	return nil
}

// needTrace is the error of a check of the trace with no telemetry to read.
func (st *ScenarioContext) needTrace() error {
	if st.traceID != "" {
		return nil
	}
	_, err := st.telemetry.Default()
	return err
}
