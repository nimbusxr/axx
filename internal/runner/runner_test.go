package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

type recorder struct {
	mu        sync.Mutex
	envelopes []*messages.Envelope
	finished  []*ScenarioResult
	run       *RunResult
}

func (r *recorder) Envelope(e *messages.Envelope) {
	r.mu.Lock()
	r.envelopes = append(r.envelopes, e)
	r.mu.Unlock()
}
func (r *recorder) ScenarioFinished(s *ScenarioResult) { r.finished = append(r.finished, s) }
func (r *recorder) Finish(res *RunResult) error        { r.run = res; return nil }

var counter = core.NewStateKey("test.counter", func(*core.Scenario) *int { n := 0; return &n }, nil)

type fixture struct {
	closed   atomic.Int32
	inflight atomic.Int32
	peak     atomic.Int32
	hookLog  []string
	hookMu   sync.Mutex
}

func (f *fixture) pack() core.Manifest {
	closing := core.NewStateKey("test.closing", func(*core.Scenario) *int { n := 1; return &n },
		func(*core.Scenario, *int) error { f.closed.Add(1); return nil })
	log := func(s string) {
		f.hookMu.Lock()
		f.hookLog = append(f.hookLog, s)
		f.hookMu.Unlock()
	}
	return core.Manifest{
		Name: "test",
		Params: []core.ParamType{{Name: "ordinal", Regexps: []string{`(\d+)(?:st|nd|rd|th)`}, Transform: func(_ *core.Scenario, _ string, g []*string) (any, error) {
			return len(*g[0]), nil
		}}},
		Steps: []core.StepDef{
			{ID: "pass", Expr: "a passing step", Run: func(sc *core.Scenario, _ core.Args) error { *counter.Of(sc)++; _ = closing.Of(sc); return nil }},
			{ID: "fail", Expr: "a failing step", Run: func(*core.Scenario, core.Args) error { return core.Fail("status mismatch", 200, 500) }},
			{ID: "pending", Expr: "a pending step", Run: func(*core.Scenario, core.Args) error { return core.ErrPending }},
			{ID: "panic", Expr: "a panicking step", Run: func(*core.Scenario, core.Args) error { panic("boom") }},
			{ID: "slow", Expr: "a slow step", Timeout: 50 * time.Millisecond, Run: func(sc *core.Scenario, _ core.Args) error {
				<-sc.Context().Done()
				return sc.Context().Err()
			}},
			{ID: "hang", Expr: "a hanging step", Timeout: 20 * time.Millisecond, Run: func(*core.Scenario, core.Args) error {
				time.Sleep(2 * time.Second)
				return nil
			}},
			{ID: "count", Expr: "the counter is {int}", Run: func(sc *core.Scenario, a core.Args) error {
				if got := *counter.Of(sc); got != a.Int(0) {
					return core.Fail("counter", a.Int(0), got)
				}
				return nil
			}},
			{ID: "table", Expr: "a table step:", Arg: core.ArgTable, Run: func(_ *core.Scenario, a core.Args) error {
				pairs, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				if pairs[0].Value != "v" {
					return core.Fail("table value", "v", pairs[0].Value)
				}
				return nil
			}},
			{ID: "busy", Expr: "a busy step", Run: func(*core.Scenario, core.Args) error {
				n := f.inflight.Add(1)
				for {
					p := f.peak.Load()
					if n <= p || f.peak.CompareAndSwap(p, n) {
						break
					}
				}
				time.Sleep(30 * time.Millisecond)
				f.inflight.Add(-1)
				return nil
			}},
			{ID: "ord", Expr: "the[[ {ordinal}]] thing", Run: func(_ *core.Scenario, a core.Args) error {
				if a.IntOr(0, 1) < 1 {
					return errors.New("bad")
				}
				return nil
			}},
			{ID: "dup1", Expr: "an ambiguous step"},
			{ID: "dup2", Expr: "an {word} step"},
		},
		Hooks: []core.Hook{
			{ID: "before", Phase: core.BeforeScenario, Run: func(sc *core.Scenario) error { log("before:" + sc.Name); return nil }},
			{ID: "after", Phase: core.AfterScenario, Run: func(sc *core.Scenario) error { log("after:" + sc.Name); return nil }},
			{ID: "tagged", Phase: core.BeforeScenario, Tags: "@broken", Run: func(*core.Scenario) error { return errors.New("hook exploded") }},
		},
	}
}

func load(t *testing.T, src string) (*feature.Set, []*feature.Pickle) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.feature"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	set, err := feature.Load([]string{"."}, dir, (&messages.Incrementing{}).NewId)
	if err != nil {
		t.Fatal(err)
	}
	return set, set.Pickles
}

func newRunner(t *testing.T, f *fixture, set *feature.Set, mod func(*Options)) (*Runner, *recorder) {
	t.Helper()
	reg := match.NewRegistry()
	m := f.pack()
	if err := reg.AddPack("test", m); err != nil {
		t.Fatal(err)
	}
	var hooks []PackHook
	for _, h := range m.Hooks {
		hooks = append(hooks, PackHook{Pack: "test", Hook: h})
	}
	rec := &recorder{}
	opts := Options{
		Registry: reg, Hooks: hooks, Workers: 4, Reporters: []Reporter{rec}, Messages: true, Docs: set.Docs,
		TimeoutGrace: 100 * time.Millisecond,
	}
	if mod != nil {
		mod(&opts)
	}
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return r, rec
}

const suite = `Feature: runner

  Background:
    Given a passing step

  Scenario: happy
    When a passing step
    Then the counter is 2
    And the 3rd thing
    And the thing
    And a table step:
      | k | v |

  Scenario: sad
    When a failing step
    Then a passing step
    And an undefined step here

  Scenario: pending
    Then a pending step

  Scenario: panicky
    Then a panicking step

  Scenario: slow
    Then a slow step

  Scenario: hang
    Then a hanging step

  Scenario: ambiguous
    Then an ambiguous step

  @broken
  Scenario: broken hook
    Then a passing step
`

func byName(res *RunResult) map[string]*ScenarioResult {
	out := map[string]*ScenarioResult{}
	for _, s := range res.Scenarios {
		out[s.Pickle.Name] = s
	}
	return out
}

func TestStatuses(t *testing.T) {
	f := &fixture{}
	set, pickles := load(t, suite)
	r, rec := newRunner(t, f, set, nil)
	res := r.Run(context.Background(), pickles)
	got := byName(res)
	want := map[string]Status{
		"happy": Passed, "sad": Failed, "pending": Pending, "panicky": Failed,
		"slow": Failed, "hang": Failed, "ambiguous": Ambiguous, "broken hook": Failed,
	}
	for name, st := range want {
		if got[name] == nil || got[name].Status != st {
			t.Errorf("%s: status %v, want %v", name, got[name].Status, st)
		}
	}
	sad := got["sad"]
	if sad.Steps[1].Status != Failed || !core.IsAssertion(sad.Steps[1].Err) {
		t.Errorf("assertion failure not recorded: %+v", sad.Steps[1])
	}
	if sad.Steps[2].Status != Skipped || sad.Steps[3].Status != Undefined {
		t.Errorf("after failure: %v %v", sad.Steps[2].Status, sad.Steps[3].Status)
	}
	var pe *PanicError
	if !errors.As(got["panicky"].Steps[1].Err, &pe) || pe.Stack == "" {
		t.Errorf("panic not captured: %v", got["panicky"].Steps[1].Err)
	}
	var te *TimeoutError
	if !errors.As(got["slow"].Steps[1].Err, &te) || te.Ignored {
		t.Errorf("slow: %v", got["slow"].Steps[1].Err)
	}
	if !errors.As(got["hang"].Steps[1].Err, &te) || !te.Ignored {
		t.Errorf("hang should be abandoned: %v", got["hang"].Steps[1].Err)
	}
	if len(got["ambiguous"].Steps[1].Candidates) != 2 {
		t.Errorf("candidates: %v", got["ambiguous"].Steps[1].Candidates)
	}
	if bh := got["broken hook"]; bh.Steps[1].Status != Skipped {
		t.Errorf("steps after failing Before hook must be skipped: %v", bh.Steps[1].Status)
	}
	if int(f.closed.Load()) != countPassingScenariosTouching(res) {
		t.Errorf("closers ran %d times", f.closed.Load())
	}
	if len(rec.finished) != len(pickles) || rec.run != res {
		t.Errorf("reporter calls: %d finished", len(rec.finished))
	}
	// After hooks run for every scenario, even failed ones.
	afters := 0
	for _, l := range f.hookLog {
		if strings.HasPrefix(l, "after:") {
			afters++
		}
	}
	if afters != len(pickles) {
		t.Errorf("after hooks ran %d times, want %d", afters, len(pickles))
	}
}

// every scenario whose background ran a passing step touched the closing state
func countPassingScenariosTouching(res *RunResult) int {
	n := 0
	for _, s := range res.Scenarios {
		if len(s.Steps) > 0 && s.Steps[0].Status == Passed {
			n++
		}
	}
	return n
}

func TestParallelismAndExclusive(t *testing.T) {
	var b strings.Builder
	b.WriteString("Feature: p\n")
	for i := 0; i < 8; i++ {
		b.WriteString("  Scenario: s" + string(rune('a'+i)) + "\n    Given a busy step\n")
	}
	b.WriteString("  @isolated\n  Scenario: solo\n    Given a busy step\n")
	f := &fixture{}
	set, pickles := load(t, b.String())
	r, _ := newRunner(t, f, set, func(o *Options) { o.Exclusive = []string{"isolated"} })
	res := r.Run(context.Background(), pickles)
	if res.Worst() != Passed {
		t.Fatalf("worst = %v", res.Worst())
	}
	if p := f.peak.Load(); p < 2 || p > 4 {
		t.Errorf("peak concurrency %d, want 2..4", p)
	}
	if res.Scenarios[len(res.Scenarios)-1].Pickle.Name != "solo" || res.Scenarios[len(res.Scenarios)-1].Worker != 0 {
		t.Errorf("exclusive scenario must run in the serial phase")
	}
}

func TestFailFast(t *testing.T) {
	var b strings.Builder
	b.WriteString("Feature: ff\n  Scenario: first\n    Given a failing step\n")
	for i := 0; i < 20; i++ {
		b.WriteString("  Scenario: later\n    Given a busy step\n")
	}
	f := &fixture{}
	set, pickles := load(t, b.String())
	r, _ := newRunner(t, f, set, func(o *Options) { o.Workers = 1; o.FailFast = true })
	res := r.Run(context.Background(), pickles)
	if res.NotRun == 0 || len(res.Scenarios)+res.NotRun != len(pickles) {
		t.Fatalf("fail-fast: ran %d, notRun %d of %d", len(res.Scenarios), res.NotRun, len(pickles))
	}
}

func TestDryRun(t *testing.T) {
	f := &fixture{}
	set, pickles := load(t, suite)
	r, _ := newRunner(t, f, set, func(o *Options) { o.DryRun = true })
	res := r.Run(context.Background(), pickles)
	got := byName(res)
	if got["happy"].Status != Skipped || got["sad"].Status != Undefined || got["ambiguous"].Status != Ambiguous {
		t.Errorf("dry run statuses: %v %v %v", got["happy"].Status, got["sad"].Status, got["ambiguous"].Status)
	}
	if f.closed.Load() != 0 || len(f.hookLog) != 0 {
		t.Error("dry run must not execute steps or hooks")
	}
}

func TestMessagesAreConsistent(t *testing.T) {
	f := &fixture{}
	set, pickles := load(t, suite)
	r, rec := newRunner(t, f, set, nil)
	r.Run(context.Background(), pickles)
	counts := map[string]int{}
	started := map[string]bool{}
	for _, e := range rec.envelopes {
		switch {
		case e.TestCaseStarted != nil:
			counts["tcs"]++
		case e.TestCaseFinished != nil:
			counts["tcf"]++
		case e.TestStepStarted != nil:
			started[e.TestStepStarted.TestCaseStartedId+"/"+e.TestStepStarted.TestStepId] = true
			counts["tss"]++
		case e.TestStepFinished != nil:
			if !started[e.TestStepFinished.TestCaseStartedId+"/"+e.TestStepFinished.TestStepId] {
				t.Errorf("step finished before started: %+v", e.TestStepFinished)
			}
			counts["tsf"]++
		case e.TestRunStarted != nil:
			counts["trs"]++
		case e.TestRunFinished != nil:
			counts["trf"]++
			if e.TestRunFinished.Success {
				t.Error("run with failures must not be successful")
			}
		}
	}
	if counts["tcs"] != len(pickles) || counts["tcf"] != len(pickles) || counts["tss"] != counts["tsf"] || counts["trs"] != 1 || counts["trf"] != 1 {
		t.Errorf("envelope counts: %v", counts)
	}
	if rec.envelopes[0].Meta == nil {
		t.Error("first envelope must be meta")
	}
}

func TestInterruptStopsRun(t *testing.T) {
	var b strings.Builder
	b.WriteString("Feature: i\n")
	for i := 0; i < 10; i++ {
		b.WriteString("  Scenario: s\n    Given a busy step\n")
	}
	f := &fixture{}
	set, pickles := load(t, b.String())
	r, _ := newRunner(t, f, set, func(o *Options) { o.Workers = 1 })
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	res := r.Run(ctx, pickles)
	if !res.Interrupted || res.NotRun == 0 {
		t.Fatalf("interrupted=%v notRun=%d", res.Interrupted, res.NotRun)
	}
}

func TestAfterRunErrorsFailTheRun(t *testing.T) {
	f := &fixture{}
	set, pickles := load(t, "Feature: ok\n  Scenario: fine\n    Given a passing step\n")
	calls := 0
	r, rec := newRunner(t, f, set, func(o *Options) {
		o.AfterRun = func(context.Context) []RunError {
			calls++
			return []RunError{{Source: "mock", Message: "a call broke its contract"}}
		}
	})
	res := r.Run(context.Background(), pickles)
	if calls != 1 || len(res.RunErrors) != 1 || res.Worst() != Failed {
		t.Fatalf("calls %d, run errors %v, worst %v", calls, res.RunErrors, res.Worst())
	}
	var finished *messages.TestRunFinished
	for _, e := range rec.envelopes {
		if e.TestRunFinished != nil {
			finished = e.TestRunFinished
		}
	}
	if finished == nil || finished.Success || finished.Exception == nil ||
		finished.Exception.Message != "mock: a call broke its contract" {
		t.Errorf("TestRunFinished: %+v", finished)
	}

	// Not for dry runs.
	calls = 0
	r, _ = newRunner(t, f, set, func(o *Options) {
		o.DryRun = true
		o.AfterRun = func(context.Context) []RunError { calls++; return nil }
	})
	r.Run(context.Background(), pickles)
	if calls != 0 {
		t.Error("AfterRun must not run for a dry run")
	}
}
