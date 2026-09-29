package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// The dialog steps are for the dialogs the system shows over an app: a
// request for a permission, an alert. A dialog of the app's own is part of
// its screen: its buttons are tapped as any other.
func dialogSteps() []core.StepDef {
	answer := func(verb string, fn func(ctx context.Context, s *appium.Session) error) core.StepDef {
		return core.StepDef{
			ID: "mobile-core.dialog." + verb, Keyword: "When", Since: since,
			Expr: "the {word} app's dialog is " + verb,
			Doc: "Answer the dialog the system shows over the app, like a request for a permission: accepted allows, dismissed does not. " +
				"It waits for the dialog. A dialog of the app's own is part of its screen: tap its buttons.",
			Examples: []string{"When the courier app's dialog is " + verb},
			Run: func(sc *core.Scenario, a core.Args) error {
				app := a.String(0)
				return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
					text, err := dialog(sc, d, app, actionTimeout)
					if err != nil {
						return err
					}
					// An answer given while the dialog still comes in can go
					// unnoticed (iOS's): answer until it is gone, or another
					// dialog shows.
					for range 3 {
						if err := fn(ctx, d.Session()); err != nil {
							return fmt.Errorf("the %s app's dialog could not be %s: %w", app, verb, err)
						}
						answered, err := waitUntil(sc, 2*time.Second, func() (bool, error) {
							t, err := d.Session().AlertText(ctx)
							var ae *appium.Error
							if errors.As(err, &ae) && ae.Code == "no such alert" {
								return true, nil
							}
							return err == nil && t != text, err
						})
						if err != nil || answered {
							return err
						}
					}
					return core.Failf("The %s app's dialog is still shown: it could not be %s", app, verb)
				})
			},
		}
	}
	return []core.StepDef{
		answer("accepted", func(ctx context.Context, s *appium.Session) error { return s.AcceptAlert(ctx) }),
		answer("dismissed", func(ctx context.Context, s *appium.Session) error { return s.DismissAlert(ctx) }),
		{
			ID: "mobile-core.dialog.shows", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {word} app's dialog shows {string}",
			Doc:      "Check the text of the dialog the system shows over the app: it waits for the dialog (10 seconds, or `within`).",
			Examples: []string{`Then the courier app's dialog shows "send you notifications"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				wait, app, want := cloudstep.Wait(a, 0), a.String(1), secrets.Expand(sc, a.String(2))
				return onDevice(sc, app, false, func(_ context.Context, d Device) error {
					var text string
					ok, err := waitUntil(sc, wait, func() (bool, error) {
						t, err := dialog(sc, d, app, 0)
						if err != nil {
							return false, nil //nolint:nilerr // no dialog yet: wait
						}
						text = t
						return strings.Contains(clean(t), clean(want)), nil
					})
					if err != nil || ok {
						return err
					}
					if text == "" {
						return core.Failf("The %s app shows no dialog", app)
					}
					return core.Fail(fmt.Sprintf("The %s app's dialog does not show %q", app, want), want, text)
				})
			},
		},
	}
}

// dialog waits for the dialog the system shows over the app, and returns its
// text.
func dialog(sc *core.Scenario, d Device, app string, wait time.Duration) (string, error) {
	var text string
	ok, err := waitUntil(sc, wait, func() (bool, error) {
		t, err := d.Session().AlertText(sc.Context())
		var ae *appium.Error
		if errors.As(err, &ae) && ae.Code == "no such alert" {
			return false, nil
		}
		text = t
		return err == nil, err
	})
	if err != nil {
		return "", err
	}
	if !ok {
		return "", core.Failf("The %s app shows no dialog", app)
	}
	return text, nil
}
