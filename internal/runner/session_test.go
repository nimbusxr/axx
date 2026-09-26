package runner

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

// A session keeps its scenario open between the steps it runs, a few at a
// time, with the packs' hooks around them as in a run.
func TestASessionKeepsItsScenarioBetweenSteps(t *testing.T) {
	var seen []string
	see := func(what string) { seen = append(seen, what) }
	pages := core.NewStateKey("test.pages", func(*core.Scenario) *[]string { return &[]string{} },
		func(_ *core.Scenario, p *[]string) error { see(fmt.Sprintf("cleanup %v", *p)); return nil })
	m := core.Manifest{
		Name: "test",
		Steps: []core.StepDef{
			{ID: "open", Expr: "the {string} page is opened", Run: func(sc *core.Scenario, a core.Args) error {
				*pages.Of(sc) = append(*pages.Of(sc), a.String(0))
				see(fmt.Sprintf("open %s at %d", a.String(0), sc.Step().Line))
				return nil
			}},
			{ID: "fail", Expr: "the page shows {string}", Run: func(*core.Scenario, core.Args) error { return errors.New("it does not") }},
		},
		Hooks: []core.Hook{
			{ID: "before", Phase: core.BeforeScenario, Run: func(*core.Scenario) error { see("before"); return nil }},
			{ID: "beforeStep", Phase: core.BeforeStep, Run: func(*core.Scenario) error { see("beforeStep"); return nil }},
			{ID: "after", Phase: core.AfterScenario, Run: func(*core.Scenario) error { see("after"); return nil }},
		},
	}
	reg := match.NewRegistry()
	if err := reg.AddPack("test", m); err != nil {
		t.Fatal(err)
	}
	var hooks []PackHook
	for _, h := range m.Hooks {
		hooks = append(hooks, PackHook{Pack: "test", Hook: h})
	}
	r, err := New(Options{Registry: reg, Hooks: hooks, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	s, before := r.NewSession(context.Background(), core.ScenarioInfo{ID: "session", Name: "session"})
	if len(before) != 1 || before[0].Status != Passed {
		t.Fatalf("before: %+v", before)
	}
	_, first := load(t, "Feature: f\n  Scenario: s\n    Given the \"/quote\" page is opened\n")
	_, second := load(t, "Feature: f\n  Scenario: s\n    When the \"/parcels/new\" page is opened\n    Then the page shows \"Register\"\n    And the \"/track\" page is opened\n")
	if got := s.Run(first[0]); len(got) != 1 || got[0].Status != Passed {
		t.Fatalf("first: %+v", got)
	}
	got := s.Run(second[0])
	statuses := []Status{got[0].Status, got[1].Status, got[2].Status}
	if !reflect.DeepEqual(statuses, []Status{Passed, Failed, Skipped}) || got[1].Err == nil {
		t.Fatalf("second: %v %v", statuses, got[1].Err)
	}
	if err := s.Close(Passed); err != nil {
		t.Fatal(err)
	}
	want := []string{"before", "beforeStep", "open /quote at 3", "beforeStep", "open /parcels/new at 6", "beforeStep", "after", "cleanup [/quote /parcels/new]"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("got  %q\nwant %q", seen, want)
	}
}
