package mobilecore

import (
	"context"
	"fmt"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

const since = "0.1.5"

// actionTimeout is how long an action waits for its control to be shown; a
// variable, for tests.
var actionTimeout = cloudstep.DefaultWait

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "mobile-core.launch", Keyword: "When", Since: since,
			Expr: "the {word} app is launched",
			Doc: "Launch the app on the device the scenario leased, reset for the scenario (see the app's platform pack), " +
				"or bring it to the front if it is already running.",
			Examples: []string{"When the courier app is launched"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return onDevice(sc, a.String(0), true, func(ctx context.Context, d Device) error { return d.Launch(ctx) })
			},
		},
		{
			ID: "mobile-core.link", Keyword: "When", Since: since,
			Expr: "the {word} app is opened with the {string} link",
			Doc: "Open a link in the app, as tapping it in another app would: a deep link jumps straight to the screen a scenario tests. " +
				"The app is launched first when it is not running.",
			Examples: []string{`When the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9401" link`},
			Run: func(sc *core.Scenario, a core.Args) error {
				link := secrets.Expand(sc, a.String(1))
				return onDevice(sc, a.String(0), true, func(ctx context.Context, d Device) error { return d.OpenLink(ctx, link) })
			},
		},
		{
			ID: "mobile-core.background", Keyword: "When", Since: since,
			Expr: "the {word} app is sent to the background",
			Doc:  "Send the app to the background, as switching to another app does.",
			Examples: []string{
				"When the courier app is sent to the background",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				return onDevice(sc, a.String(0), false, func(ctx context.Context, d Device) error { return d.Background(ctx) })
			},
		},
		{
			ID: "mobile-core.foreground", Keyword: "When", Since: since,
			Expr:     "the {word} app is brought back",
			Doc:      "Bring the app back to the front from the background, where it left off.",
			Examples: []string{"When the courier app is brought back"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return onDevice(sc, a.String(0), false, func(ctx context.Context, d Device) error { return d.Launch(ctx) })
			},
		},
		{
			ID: "mobile-core.restart", Keyword: "When", Since: since,
			Expr:     "the {word} app is restarted",
			Doc:      "Stop the app and start it again, keeping what it stored: what survives a restart, like a sign-in, is still there.",
			Examples: []string{"When the courier app is restarted"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return onDevice(sc, a.String(0), false, func(ctx context.Context, d Device) error { return d.Restart(ctx) })
			},
		},
		{
			ID: "mobile-core.tap", Keyword: "When", Since: since,
			Expr:     "the {string} {control} is tapped in the {word} app",
			Doc:      "Tap a control, found by the name people see, or by an `id=` or `xpath=` selector.",
			Examples: []string{`When the "Sign in" button is tapped in the courier app`, `When the "PX-MOB-9401" list item is tapped in the courier app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				name, k, app := secrets.Expand(sc, a.String(0)), a.Value(1).(kind), a.String(2)
				return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
					el, err := element(sc, d, app, k, name, actionTimeout)
					if err != nil {
						return err
					}
					if err := el.Click(ctx); err != nil {
						return fmt.Errorf("cannot tap the %s %s: %w", quoted(name), k.noun, err)
					}
					return nil
				})
			},
		},
		{
			ID: "mobile-core.fill", Keyword: "When", Since: since,
			Expr:     "the {string} field in the {word} app is filled with {string}",
			Doc:      "Replace what a field holds with a text, as typing it would. `${env:..}` values are secrets: masked in logs and failures.",
			Examples: []string{`When the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				name, app, value := secrets.Expand(sc, a.String(0)), a.String(1), secrets.Expand(sc, a.String(2))
				return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
					el, err := element(sc, d, app, kinds["field"], name, actionTimeout)
					if err != nil {
						return err
					}
					if err := el.Clear(ctx); err != nil {
						return fmt.Errorf("cannot empty the %s field: %w", quoted(name), err)
					}
					if err := el.Type(ctx, value); err != nil {
						return fmt.Errorf("cannot type into the %s field: %w", quoted(name), err)
					}
					return nil
				})
			},
		},
		{
			ID: "mobile-core.swipe", Keyword: "When", Since: since,
			Expr:     "the {word} app is swiped {direction}",
			Doc:      "Swipe across the screen: down, from the top, refreshes a list that pulls to refresh; left and right page through what pages.",
			Examples: []string{"When the courier app is swiped down"},
			Run: func(sc *core.Scenario, a core.Args) error {
				dir := a.String(1)
				return onDevice(sc, a.String(0), false, func(ctx context.Context, d Device) error { return d.Swipe(ctx, dir) })
			},
		},
		{
			ID: "mobile-core.scroll", Keyword: "When", Since: since,
			Expr:     "the {string} {control} is scrolled into view in the {word} app",
			Doc:      "Scroll the screen until a control is shown: down first, then up.",
			Examples: []string{`When the "PX-MOB-9412" list item is scrolled into view in the courier app`},
			Run: func(sc *core.Scenario, a core.Args) error {
				name, k, app := secrets.Expand(sc, a.String(0)), a.Value(1).(kind), a.String(2)
				return onDevice(sc, app, false, func(ctx context.Context, d Device) error { return scrollTo(sc, d, app, k, name) })
			},
		},
	}
}

// onDevice runs a step on the named app's device: started first when start
// is true. A failed step attaches a screenshot, and its error masks the
// scenario's secrets.
func onDevice(sc *core.Scenario, app string, start bool, fn func(ctx context.Context, d Device) error) error {
	d, err := device(sc, app, start)
	if err != nil {
		return err
	}
	if err := fn(sc.Context(), d); err != nil {
		screenshot(sc, d, app)
		return secrets.Hide(sc, err)
	}
	return nil
}

// screenshot attaches what the app shows.
func screenshot(sc *core.Scenario, d Device, app string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(sc.Context()), 10*time.Second)
	defer cancel()
	if b, err := d.Session().Screenshot(ctx); err == nil {
		sc.Attach("image/png", b, "the "+app+" app")
	}
}

// find waits for the one control of kind k named name, and returns it with
// the screen it is on.
func find(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*Node, error) {
	var found []*Node
	var last *Screen
	ok, err := waitUntil(sc, wait, func() (bool, error) {
		s, err := d.Screen(sc.Context())
		if err != nil {
			return false, err
		}
		last, found = s, matches(s, k, name)
		return len(found) == 1, nil
	})
	switch {
	case err != nil:
		return nil, err
	case ok:
		return found[0], nil
	case len(found) > 1:
		return nil, several(app, found, k, name)
	}
	return nil, missing(app, last, k, name)
}

// element is the one control of kind k named name, as Appium refers to it:
// found by its name on the screen, or by a selector.
func element(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*appium.Element, error) {
	if using, value, ok := selector(name); ok {
		var el *appium.Element
		found, err := waitUntil(sc, wait, func() (bool, error) {
			e, err := d.Session().Find(sc.Context(), using, value)
			if appium.IsNoSuchElement(err) {
				return false, nil
			}
			el = e
			return err == nil, err
		})
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, core.Failf("No %s in the %s app matches %q", k.noun, app, name)
		}
		return el, nil
	}
	deadline := time.Now().Add(wait)
	for {
		n, err := find(sc, d, app, k, name, time.Until(deadline))
		if err != nil {
			return nil, err
		}
		el, err := d.Session().Find(sc.Context(), n.Using, n.Value)
		if !appium.IsNoSuchElement(err) || !time.Now().Before(deadline) {
			return el, err
		}
		// The control moved between reading the screen and finding it, like
		// a dialog's button as the dialog comes in: read the screen again.
		select {
		case <-sc.Context().Done():
			return nil, sc.Context().Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// scrollTo scrolls until the control is shown: down, then up.
func scrollTo(sc *core.Scenario, d Device, app string, k kind, name string) error {
	for _, dir := range []string{"down", "up"} {
		for range 20 {
			s, err := d.Screen(sc.Context())
			if err != nil {
				return err
			}
			if len(matches(s, k, name)) > 0 {
				return nil
			}
			moved, err := d.Scroll(sc.Context(), dir)
			if err != nil {
				return err
			}
			if !moved {
				break
			}
		}
	}
	s, err := d.Screen(sc.Context())
	if err != nil {
		return err
	}
	return missing(app, s, k, name)
}

// waitUntil calls check until it reports true or d has passed.
func waitUntil(sc *core.Scenario, d time.Duration, check func() (bool, error)) (bool, error) {
	deadline := time.Now().Add(d)
	for {
		ok, err := check()
		if err != nil || ok {
			return ok, err
		}
		if !time.Now().Before(deadline) {
			return false, nil
		}
		select {
		case <-sc.Context().Done():
			return false, sc.Context().Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
