package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

type hostHooksPack struct {
	name  string
	hooks []core.Hook
}

func (p hostHooksPack) Manifest() core.Manifest { return core.Manifest{Name: p.name, Hooks: p.hooks} }

// A scenario the axx host makes for one of a run's gets the packs' hooks of
// the host's phase, and no other: in their order, and those whose tags it
// has.
func TestAHostsScenarioGetsThePacksHostHooks(t *testing.T) {
	var ran []string
	hook := func(id string, phase core.Phase, order int, tags string) core.Hook {
		return core.Hook{ID: id, Phase: phase, Order: order, Tags: tags, Run: func(sc *core.Scenario) error {
			ran = append(ran, id+" in "+sc.Name)
			sc.Note(id, "set up")
			return nil
		}}
	}
	e, err := New(Options{Packs: []NamedPack{
		{Name: "core", Pack: core.ParamsPack()},
		{Name: "stubs", Pack: hostHooksPack{name: "stubs", hooks: []core.Hook{
			hook("stubs.prepare", core.BeforeStep, 0, ""),
			hook("stubs.prepare.host", core.HostScenario, 2, ""),
			hook("stubs.camera.host", core.HostScenario, 1, "@camera"),
			hook("stubs.printer.host", core.HostScenario, 0, "@printer"),
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	sc := core.NewScenario(t.Context(), core.ScenarioInfo{Name: "a torn label", Tags: []string{"@camera"}}, e.Suite, nil)
	if err := e.Suite.BeginHostScenario(sc); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ran, "; "); got != "stubs.camera.host in a torn label; stubs.prepare.host in a torn label" {
		t.Errorf("the hooks that ran: %s", got)
	}
	if v, _ := sc.Noted("stubs.prepare.host"); v != "set up" {
		t.Errorf("what the hook noted of the scenario: %q", v)
	}

	// A hook that fails says whose it is.
	e, err = New(Options{Packs: []NamedPack{{Name: "stubs", Pack: hostHooksPack{name: "stubs", hooks: []core.Hook{
		{ID: "stubs.prepare.host", Phase: core.HostScenario, Run: func(*core.Scenario) error { return errors.New("no compiler") }},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	err = e.Suite.BeginHostScenario(core.NewScenario(t.Context(), core.ScenarioInfo{}, e.Suite, nil))
	if err == nil || !strings.Contains(err.Error(), "pack stubs failed to set up the scenario in the axx host") || !strings.Contains(err.Error(), "no compiler") {
		t.Errorf("a hook that fails: %v", err)
	}

	// A suite without packs' hooks has nothing to run.
	if err := core.NewSuite(core.SuiteOptions{}).BeginHostScenario(sc); err != nil {
		t.Error(err)
	}
}
