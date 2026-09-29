package mobilecore

import (
	"context"
	"fmt"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
)

func checkSteps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "mobile-core.shows", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {word} app shows {string}",
			Doc:      "Check that the app shows a text: in a control's text or label, whitespace collapsed. It waits for the text (10 seconds, or `within`).",
			Examples: []string{`Then the courier app shows "Today's deliveries"`, `Then within 20s the courier app shows "Delivered"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, app, text := cloudstep.Wait(a, 0), a.String(1), secrets.Expand(sc, a.String(2))
				return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
					var last *Screen
					ok, err := waitUntil(sc, wait, func() (bool, error) {
						s, err := d.Screen(ctx)
						last = s
						return err == nil && s.Shows(text), err
					})
					if err != nil || ok {
						return err
					}
					return core.Fail(fmt.Sprintf("The %s app does not show %q", app, text), text, shownTexts(last))
				})
			},
		},
		{
			ID: "mobile-core.hides", Keyword: "Then", Since: since, Absence: true,
			Expr:     "[[within {duration} ]]the {word} app does not show {string}",
			Doc:      "Check that the app does not show a text, or no longer does: it waits for the text to go (10 seconds, or `within`).",
			Examples: []string{`Then the courier app does not show "PX-MOB-9401"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, app, text := cloudstep.Wait(a, 0), a.String(1), secrets.Expand(sc, a.String(2))
				return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
					ok, err := waitUntil(sc, wait, func() (bool, error) {
						s, err := d.Screen(ctx)
						return err == nil && !s.Shows(text), err
					})
					if err != nil || ok {
						return err
					}
					return core.Failf("The %s app shows %q", app, text)
				})
			},
		},
		{
			ID: "mobile-core.control.shown", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {string} {control} is shown in the {word} app",
			Doc:      "Check that the app shows a control: it waits for it (10 seconds, or `within`).",
			Examples: []string{`Then the "Mark delivered" button is shown in the courier app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, name, k, app := cloudstep.Wait(a, 0), secrets.Expand(sc, a.String(1)), a.Value(2).(kind), a.String(3)
				return onDevice(sc, app, false, func(_ context.Context, d Device) error {
					_, err := element(sc, d, app, k, name, wait)
					return err
				})
			},
		},
		stateStep("enabled", "Check that a control can be used: it waits for it (10 seconds, or `within`).", true),
		stateStep("disabled", "Check that a control is shown but cannot be used, like a button until a form is complete.", false),
		{
			ID: "mobile-core.field.value", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {string} field in the {word} app has the value {string}",
			Doc:      "Check what a field holds: it waits for the value (10 seconds, or `within`). A password field holds its dots.",
			Examples: []string{`Then the "Courier ID" field in the courier app has the value "CR-LEJ-12"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, name, app, want := cloudstep.Wait(a, 0), secrets.Expand(sc, a.String(1)), a.String(2), secrets.Expand(sc, a.String(3))
				return onDevice(sc, app, false, func(_ context.Context, d Device) error {
					var got string
					ok, err := waitUntil(sc, wait, func() (bool, error) {
						n, err := find(sc, d, app, kinds["field"], name, 0)
						if err != nil {
							return false, nil //nolint:nilerr // not shown yet: wait
						}
						got = value(n)
						return got == clean(want), nil
					})
					if err != nil || ok {
						return err
					}
					if _, err := find(sc, d, app, kinds["field"], name, 0); err != nil {
						return err
					}
					return core.Fail(fmt.Sprintf("The %q field in the %s app does not have the value", name, app), want, got)
				})
			},
		},
	}
}

// stateStep checks that a control is enabled, or disabled.
func stateStep(state, doc string, enabled bool) core.StepDef {
	return core.StepDef{
		ID: "mobile-core.control." + state, Keyword: "Then", Since: since,
		Expr:     "[[within {duration} ]]the {string} {control} is " + state + " in the {word} app",
		Doc:      doc,
		Examples: []string{`Then the "Sign in" button is ` + state + ` in the courier app`},
		Run: func(sc *core.Scenario, a core.Args) error {
			wait, name, k, app := cloudstep.Wait(a, 0), secrets.Expand(sc, a.String(1)), a.Value(2).(kind), a.String(3)
			return onDevice(sc, app, false, func(_ context.Context, d Device) error {
				var last *Node
				ok, err := waitUntil(sc, wait, func() (bool, error) {
					n, err := find(sc, d, app, k, name, 0)
					if err != nil {
						return false, nil //nolint:nilerr // not shown yet: wait
					}
					last = n
					return n.Enabled == enabled, nil
				})
				if err != nil || ok {
					return err
				}
				if last == nil {
					_, err := find(sc, d, app, k, name, 0)
					return err
				}
				return core.Fail(fmt.Sprintf("The %q %s in the %s app is not %s", name, k.noun, app, state), state, map[bool]string{true: "enabled", false: "disabled"}[last.Enabled])
			})
		},
	}
}

// value is what a field holds: nothing while it shows only its label.
func value(n *Node) string {
	t := clean(n.Text)
	if t == n.Hint || t == n.Label {
		return ""
	}
	return t
}

// shownTexts is what a screen shows, for a failure.
func shownTexts(s *Screen) string {
	if s == nil {
		return ""
	}
	var out []string
	seen := map[string]bool{}
	for _, n := range s.Visible() {
		for _, t := range []string{clean(n.Text), clean(n.Label)} {
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return cloudstep.Shown("texts", out, 30)
}
