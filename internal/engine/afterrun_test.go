package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	messages "github.com/cucumber/messages/go/v35"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/runner"
)

type finisherPack struct {
	err   error
	calls *int
}

func (finisherPack) Manifest() core.Manifest { return core.Manifest{Name: "finisher"} }

func (p finisherPack) Finish(context.Context, *core.Suite) error {
	*p.calls++
	return p.err
}

type plainPack struct{}

func (plainPack) Manifest() core.Manifest { return core.Manifest{Name: "plain"} }

func TestAfterRunCollectsFinisherErrors(t *testing.T) {
	calls := 0
	e := &Engine{
		Suite: core.NewSuite(core.SuiteOptions{}),
		Packs: []NamedPack{
			{Name: "plain", Pack: plainPack{}},
			{Name: "quiet", Pack: finisherPack{calls: &calls}},
			{Name: "mock", Pack: finisherPack{err: errors.New("a call broke its contract"), calls: &calls}},
		},
	}
	if err := e.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := e.AfterRun(context.Background(), nil)
	if calls != 2 || len(got) != 1 || got[0].Source != "mock" || got[0].Message != "a call broke its contract" {
		t.Fatalf("calls %d, run errors %+v", calls, got)
	}
}

type preparerPack struct {
	plans *[]*core.Plan
}

func (preparerPack) Manifest() core.Manifest {
	return core.Manifest{Name: "prep", Steps: []core.StepDef{
		{ID: "prep.listen", Keyword: "Given", Arg: core.ArgTable, Expr: "a listener", Run: func(*core.Scenario, core.Args) error { return nil }},
	}}
}

func (p preparerPack) Prepare(_ context.Context, _ *core.Suite, plan *core.Plan) error {
	*p.plans = append(*p.plans, plan)
	return nil
}

func TestPrepareGetsThePlanOfTheRun(t *testing.T) {
	var plans []*core.Plan
	e, err := New(Options{Packs: []NamedPack{{Name: "core", Pack: core.ParamsPack()}, {Name: "prep", Pack: preparerPack{plans: &plans}}}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := dir + "/x.feature"
	src := "Feature: x\n  Scenario: one\n    Given a listener\n      | url | udp://0.0.0.0:5140 |\n    And an undefined step\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	set, err := e.LoadFeatures([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	pickles, err := set.Apply(feature.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Prepare(context.Background(), pickles); err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Scenarios) != 1 {
		t.Fatalf("plans: %+v", plans)
	}
	sc := plans[0].Scenarios[0]
	if sc.Name != "one" || len(sc.Steps) != 2 {
		t.Fatalf("scenario: %+v", sc)
	}
	got := plans[0].Steps("prep.listen")
	if len(got) != 1 || got[0].Pack != "prep" || got[0].Table == nil || got[0].Table.Rows[0][1] != "udp://0.0.0.0:5140" {
		t.Errorf("planned step: %+v", got)
	}
	if sc.Steps[1].Definition != "" {
		t.Errorf("an undefined step has no definition: %+v", sc.Steps[1])
	}
}

// runFinisherPack keeps the run it is handed.
type runFinisherPack struct{ got *core.Run }

func (runFinisherPack) Manifest() core.Manifest { return core.Manifest{Name: "controls"} }

func (p runFinisherPack) FinishRun(_ context.Context, _ *core.Suite, run core.Run) error {
	*p.got = run
	run.Say(fmt.Sprintf("%d scenarios ran", len(run.Scenarios)))
	if len(run.Scenarios) == 0 {
		return errors.New("no scenario ran")
	}
	return nil
}

// A pack that looks at the run once it has finished is handed every scenario that ran, with its
// tags and how it ended, and what kind of run it was. What it returns fails the run, as a
// Finisher's does.
func TestAPackIsHandedTheFinishedRun(t *testing.T) {
	var got core.Run
	e := &Engine{Suite: core.NewSuite(core.SuiteOptions{}), Packs: []NamedPack{{Name: "controls", Pack: runFinisherPack{got: &got}}}}
	if err := e.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	pickle := func(id, name string, line int, tags ...string) *feature.Pickle {
		return &feature.Pickle{Pickle: &messages.Pickle{Id: id, Name: name, Uri: "features/sign-in.feature"}, Line: line, TagNames: tags}
	}
	res := &runner.RunResult{Started: started, Scenarios: []*runner.ScenarioResult{
		{Pickle: pickle("1", "A short password is refused", 7, "@asvs:6.2.1"), Status: runner.Passed, Started: started, Duration: 2 * time.Second},
		{Pickle: pickle("2", "A session ends", 12, "@asvs:7.2.1", "@smoke"), Status: runner.Failed, Started: started.Add(2 * time.Second), Duration: 3 * time.Second},
	}}
	e.RunIs("2/3", "")
	if errs := e.AfterRun(context.Background(), res); len(errs) != 0 {
		t.Fatalf("run errors: %+v", errs)
	}
	if len(got.Scenarios) != 2 || got.Shard != "2/3" || got.Partial != "" || !got.Started.Equal(started) || !got.Ended.Equal(started.Add(5*time.Second)) {
		t.Fatalf("the run: %+v", got)
	}
	first, second := got.Scenarios[0], got.Scenarios[1]
	if first.Status != "passed" || first.URI != "features/sign-in.feature" || first.Line != 7 || first.Name != "A short password is refused" || len(first.Tags) != 1 || first.Tags[0] != "@asvs:6.2.1" {
		t.Errorf("the first scenario: %+v", first)
	}
	if second.Status != "failed" || len(second.Tags) != 2 {
		t.Errorf("the second scenario: %+v", second)
	}

	if said := e.Said(); len(said) != 1 || said[0] != "2 scenarios ran" {
		t.Errorf("what the pack said of the run: %q", said)
	}

	// A run that stopped before its last scenario is not a run of them all.
	res.NotRun = 1
	e.RunIs("", "")
	_ = e.AfterRun(context.Background(), res)
	if got.Partial == "" {
		t.Error("a run that left scenarios out is taken for a whole one")
	}
	// What the pack returns fails the run.
	errs := e.AfterRun(context.Background(), &runner.RunResult{})
	if len(errs) != 1 || errs[0].Source != "controls" || errs[0].Message != "no scenario ran" {
		t.Errorf("run errors: %+v", errs)
	}
}
