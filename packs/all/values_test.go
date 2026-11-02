package all_test

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/packs/all"
)

// The {string}s that hold no value (ADR 0014), by step and argument. Is
// something set to it or checked against it? Then it is a value, and the step
// declares it (TextArgs, TypedArgs).
var (
	// names find something, as written: a label, a page, a file, a screenshot.
	names = []string{
		"rest.request.payload.example 1", "rest.request.payload.example.on 1", "files.screenshot 3",
		"web-core.open 0", "web-core.fill 0", "web-core.select 1", "web-core.option 0", "web-core.check 0",
		"web-core.uncheck 0", "web-core.upload 1", "web-core.button 0", "web-core.link 0", "web-core.tab 0",
		"web-core.menuitem 0", "web-core.element.click 0", "web-core.key 1", "web-core.hover 0", "web-core.url 1",
		"web-core.value 1", "web-core.disabled 1", "web-core.enabled 1", "web-core.downloaded 1",
		"web-core.download.contains 0", "web-core.download.row 0", "web-core.download.identical 0",
		"web-core.element.shown 1", "web-core.element.hidden 1", "web-core.element.text 1",
		"web-core.element.attribute 1", "web-core.element.focus 1", "web-core.checkbox.ticked 1",
		"web-core.checkbox.unticked 1", "web-core.option.selected 1", "web-core.option.unselected 1",
		"web-core.field.disabled 1", "web-core.field.enabled 1", "web-core.element.count 2",
		"web-core.element.dblclick 0", "web-core.element.rightclick 0", "web-core.element.tap 0",
		"web-core.element.scroll 0", "web-core.element.drag 0", "web-core.element.drag 2",
		"web-core.select.many 0", "web-screenshots.page 1", "web-screenshots.page.masked 1",
		"web-screenshots.element 1", "web-screenshots.element 3", "web-a11y.element 0",
		"web-a11y.element.structure 1", "web-lighthouse.scores 0", "web-lighthouse.loads 0", "app-core.tapped 0",
		"app-core.clicked 0", "app-core.fill 0", "app-core.scroll 0", "app-core.control.shown 1",
		"app-core.control.enabled 1", "app-core.control.disabled 1", "app-core.field.value 1",
		"app-core.screenshot 2", "mobile-core.link 1", "mobile-core.phone-to-camera 1", "mobile-core.phone-path 2",
		"mobile-core.camera-tap 0", "mobile-core.notification-action 0", "mobile-core.notification-action 1",
		"mobile-core.background-task 0", "desktop-core.click-at 0", "desktop-core.click-at-anchor 0",
		"desktop-core.drag 4", "desktop-core.drag-from-anchor 5", "models-core.microphone 1",
		"models-core.speaker 2",
	}
	// patterns have a grammar of their own: a regular expression, a URL pattern.
	patterns = []string{
		"logs.entry 2", "logs.count 3", "cli.output.line 1", "web-core.request.sent 2", "web-network.fail 0",
		"web-network.status 0", "web-network.file 0", "web-network.slow 0",
	}
	// commandLines are split by the step, whose quotes are the shell's.
	commandLines = []string{
		"cli.run 1", "cli.run.input 1",
	}
)

// Every {string} of every step is a value it declares or one of those above: a
// new step says which.
func TestEveryStringIsClassified(t *testing.T) {
	e := allPacks(t)
	exempt := slices.Concat(names, patterns, commandLines)
	seen := map[string]bool{}
	for _, d := range e.Registry.Defs() {
		for i, name := range d.Names {
			if name != "string" {
				continue
			}
			key := d.Step.ID + " " + strconv.Itoa(i)
			value := slices.Contains(d.Step.TextArgs, i) || slices.Contains(d.Step.TypedArgs, i)
			switch {
			case value && slices.Contains(exempt, key):
				t.Errorf("%s (%s) is declared a value and listed as none", key, d.Step.Expr)
			case !value && !slices.Contains(exempt, key):
				t.Errorf("%s (%s): is something set to it or checked against it? Declare it (TextArgs, TypedArgs); a name or a pattern goes in the lists above", key, d.Step.Expr)
			}
			seen[key] = true
		}
	}
	for _, key := range exempt {
		if !seen[key] {
			t.Errorf("%s is no step's {string}", key)
		}
	}
}

// Every form of every step's value, the wordings of Also and the forms of a
// REST family included, reads by the rule: as the step resolves, and in what axx
// validate reports, with the same message.
func TestEveryFormOfAValueReads(t *testing.T) {
	e := allPacks(t)
	samples := map[string]string{"string": "'x'"}
	for _, p := range e.Registry.Params() {
		if len(p.Type.Examples) > 0 && p.Type.Name != "string" {
			samples[p.Type.Name] = p.Type.Examples[0]
		}
	}
	suite := core.NewSuite(core.SuiteOptions{})
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{}, suite, nil)
	forms := 0
	for _, v := range e.Registry.Variants() {
		step := v.Def.Step
		if len(step.TextArgs)+len(step.TypedArgs) == 0 {
			continue
		}
		line := sampleLine(v.Expr, samples)
		ms := e.Registry.Match(line)
		if len(ms) != 1 || ms[0].Def().Step.ID != step.ID {
			t.Errorf("%s: %q matches %d steps", v.Expr, line, len(ms))
			continue
		}
		if es := ms[0].ValueErrors(); len(es) != 0 {
			t.Errorf("%s: %q: %v", v.Expr, line, es)
		}
		for i, a := range ms[0].Args {
			text := slices.Contains(step.TextArgs, i)
			if !a.Present || (!text && !slices.Contains(step.TypedArgs, i)) || a.Param != "string" {
				continue
			}
			forms++
			with := func(form string) string { return line[:a.Start] + form + line[a.Start+len(a.Raw):] }
			wrong, right, got, message := "'01067'", `'"01067"'`, `"01067"`,
				`01067 could be the number 1067 or the text 01067: write '1067' for the number, or '"01067"' for the text`
			if text {
				wrong, right, got, message = "'5'", `'"5"'`, "5", `5 reads as an integer, and only text fits here: write '"5"' for the text`
			}
			if ps := cloudtest.ValueProblems(t, e.Registry, with(wrong)); !slices.Equal(ps, []string{message}) {
				t.Errorf("%s: axx validate reports %q, want %s", with(wrong), ps, message)
			}
			if _, err := resolved(t, e.Registry, sc, with(wrong), i); err != nil && err.Error() != message+" ["+core.ValueErrorCode+"]" && isValue(err) {
				t.Errorf("%s: %v, want %s", with(wrong), err, message)
			}
			for form, want := range map[string]string{right: got, "'PX-4101'": "PX-4101", "'${env:AXX_COUNT}'": "${env:AXX_COUNT}"} {
				if ps := cloudtest.ValueProblems(t, e.Registry, with(form)); len(ps) != 0 {
					t.Errorf("%s: axx validate reports %q", with(form), ps)
				}
				if s, err := resolved(t, e.Registry, sc, with(form), i); isValue(err) || (err == nil && s != want) {
					t.Errorf("%s: %q %v, want %q", with(form), s, err, want)
				}
			}
		}
	}
	if forms < 100 {
		t.Errorf("only %d forms of values", forms)
	}
}

func allPacks(t *testing.T) *engine.Engine {
	t.Helper()
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func isValue(err error) bool {
	var ve *core.ValueError
	return errors.As(err, &ve)
}

// resolved is what Run gets for argument i of a step. A step that names a service
// fails on it here, for lack of one.
func resolved(t *testing.T, reg *match.Registry, sc *core.Scenario, line string, i int) (string, error) {
	t.Helper()
	ms := reg.Match(line)
	if len(ms) != 1 {
		t.Fatalf("%q matches %d steps", line, len(ms))
	}
	a, err := reg.Resolve(sc, ms[0], line, nil, nil)
	if err != nil {
		return "", err
	}
	return a.String(i), nil
}

var (
	optionalText = regexp.MustCompile(`\([a-z]+\)`)
	alternative  = regexp.MustCompile(`(\w+)/\w+`)
	parameter    = regexp.MustCompile(`\{\w*\}`)
)

// sampleLine is a step line of an expression's form, its parameters filled in.
func sampleLine(expr string, samples map[string]string) string {
	expr = optionalText.ReplaceAllString(expr, "")
	expr = alternative.ReplaceAllString(expr, "$1")
	return parameter.ReplaceAllStringFunc(expr, func(p string) string {
		return samples[strings.Trim(p, "{}")]
	})
}
