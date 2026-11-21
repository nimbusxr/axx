package lint

import (
	"fmt"
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v35"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/packs/all"
)

// A pack of a project's, with a step it deprecated for another.
type customsPack struct{}

func (customsPack) Manifest() core.Manifest {
	run := func(*core.Scenario, core.Args) error { return nil }
	return core.Manifest{Name: "customs", Steps: []core.StepDef{
		{ID: "customs.declared", Keyword: "Given", Expr: "the parcel {word} is declared", Run: run, DeprecatedBy: "use `a declaration for the parcel {word}`"},
		{ID: "customs.declaration", Keyword: "Given", Expr: "a declaration for the parcel {word}", Run: run},
	}}
}

const deprecatedFeature = `Feature: customs

  Background:
    Given the parcel PX-CUS-1001 is declared

  Scenario: a parcel is cleared
    Given a declaration for the parcel PX-CUS-1002

  Scenario: a parcel is held
    Given the parcel PX-CUS-1003 is declared
`

func TestCheckDeprecated(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: append(engine.Ordered(all.Packs()), engine.NamedPack{Name: "customs", Pack: customsPack{}})})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("customs.feature", []byte(deprecatedFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	rr := CheckDeprecated(e.Registry, pickles, "")
	if rr.Mode != ModeWarn || !rr.OK() {
		t.Errorf("a deprecated step works: the check warns, mode %s, ok %v", rr.Mode, rr.OK())
	}
	var got []string
	for _, f := range rr.Findings {
		if f.Severity != SeverityWarning || f.Code != CodeDeprecatedStep {
			t.Errorf("finding: %+v", f)
		}
		got = append(got, fmt.Sprintf("%d %s", f.Locations[0].Line, f.Message))
	}
	// The Background's step once, though two scenarios run it.
	want := []string{
		"4 this step is deprecated: use `a declaration for the parcel {word}`. It works until the next major version of the customs pack, which drops it",
		"10 this step is deprecated: use `a declaration for the parcel {word}`. It works until the next major version of the customs pack, which drops it",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// None of axx's own steps is deprecated without saying what to use, and the features of the
// packs' own steps give no finding today.
func TestNoStepOfAxxIsDeprecatedWithoutAReplacement(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range e.Registry.Defs() {
		if by := d.Step.DeprecatedBy; by != "" && len(strings.Fields(by)) < 2 {
			t.Errorf("%s is deprecated by %q: say what to use instead", d.Step.ID, by)
		}
	}
}
