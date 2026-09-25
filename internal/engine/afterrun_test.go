package engine

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
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
	got := e.AfterRun(context.Background())
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
