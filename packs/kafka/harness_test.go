package kafka

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

// harness runs kafka steps by text in one scenario.
type harness struct {
	t     *testing.T
	reg   *match.Registry
	suite *core.Suite
	sc    *core.Scenario
	dir   string
	sink  *logSink
}

type logSink struct {
	mu   sync.Mutex
	logs []string
}

func (s *logSink) Log(_ *core.Scenario, msg string) {
	s.mu.Lock()
	s.logs = append(s.logs, msg)
	s.mu.Unlock()
}

func (s *logSink) Attach(*core.Scenario, string, []byte, string) {}

func (s *logSink) all() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.logs, "\n")
}

func newHarness(t *testing.T, packConfig string) *harness {
	t.Helper()
	reg := match.NewRegistry()
	if err := reg.AddPack("core", core.ParamsPack().Manifest()); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddPack("kafka", Pack().Manifest()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	opts := core.SuiteOptions{ResolvePath: func(p string) (string, error) {
		if filepath.IsAbs(p) {
			return p, nil
		}
		full := filepath.Join(dir, p)
		if _, err := os.Stat(full); err != nil {
			return "", err
		}
		return full, nil
	}}
	if packConfig != "" {
		opts.PackConfig = map[string]json.RawMessage{"kafka": json.RawMessage(packConfig)}
	}
	suite := core.NewSuite(opts)
	t.Cleanup(func() { _ = suite.Close(context.Background()) })
	sink := &logSink{}
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{Name: t.Name()}, suite, sink)
	return &harness{t: t, reg: reg, suite: suite, sc: sc, dir: dir, sink: sink}
}

func (h *harness) file(name, content string) {
	h.t.Helper()
	p := filepath.Join(h.dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// run executes a step; rows become its data table, a single string row
// prefixed "doc:" its doc string.
func (h *harness) run(text string, rows ...[]string) error {
	h.t.Helper()
	ms := h.reg.Match(text)
	if len(ms) != 1 {
		h.t.Fatalf("%q matched %d step definitions", text, len(ms))
	}
	var tbl *core.Table
	var doc *core.DocString
	if len(rows) == 1 && len(rows[0]) == 1 && strings.HasPrefix(rows[0][0], "doc:") {
		doc = &core.DocString{Content: strings.TrimPrefix(rows[0][0], "doc:")}
	} else if rows != nil {
		tbl = &core.Table{Rows: rows}
	}
	args, err := h.reg.Resolve(h.sc, ms[0], text, tbl, doc)
	if err != nil {
		return err
	}
	return ms[0].Def().Step.Run(h.sc, args)
}

func (h *harness) must(text string, rows ...[]string) {
	h.t.Helper()
	if err := h.run(text, rows...); err != nil {
		h.t.Fatalf("%s: %v", text, err)
	}
}

func (h *harness) fails(text, want string, rows ...[]string) {
	h.t.Helper()
	err := h.run(text, rows...)
	if err == nil || !strings.Contains(err.Error(), want) {
		h.t.Fatalf("%s: want error containing %q, got %v", text, want, err)
	}
}

func (h *harness) client(service, topic string) *TopicClient {
	h.t.Helper()
	svc, err := stateKey.Of(h.sc).services.Get(service)
	if err != nil {
		h.t.Fatal(err)
	}
	tc, ok := svc.client(topic)
	if !ok {
		h.t.Fatalf("no client for %s", topic)
	}
	return tc
}

// offline registers a service whose brokers refuse connections, so topic
// clients can be created without Kafka.
func (h *harness) offline(name string) {
	h.t.Helper()
	h.must("the "+name+" kafka service with the following properties:", []string{"brokers", "127.0.0.1:1"})
}
