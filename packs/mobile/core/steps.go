package mobilecore

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/imagediff"
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
			ID: "mobile-core.swipe", Keyword: "When", Since: since,
			Expr:     "the {word} app is swiped {direction}",
			Doc:      "Swipe across the screen: down, from the top, refreshes a list that pulls to refresh; left and right page through what pages.",
			Examples: []string{"When the courier app is swiped down"},
			Run: func(sc *core.Scenario, a core.Args) error {
				dir := a.String(1)
				return onDevice(sc, a.String(0), false, func(ctx context.Context, d Device) error { return d.Swipe(ctx, dir) })
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

// find is the one control of kind k named name, as the screen shows it now (a check's own
// wait, within its time, is around it): a few looks, as a busy machine needs.
func find(sc *core.Scenario, d Device, app string, k kind, name string) (*Node, error) {
	n, _, err := look(sc, d, app, k, name, 0)
	return n, err
}

// look is find, and whether the control came only after the first look at the screen: one that
// may still be coming in, as a dialog does.
func look(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*Node, bool, error) {
	var found []*Node
	var last *Screen
	hid := false
	looks := 0
	ok, err := waitUntil(sc, wait, func() (bool, error) {
		looks++
		s, err := d.Screen(sc.Context())
		if err != nil {
			return false, err
		}
		last, found = s, matches(s, k, name)
		// The control is there but not shown: the keyboard may cover it. A person closes the
		// keyboard to reach it, and so does axx, once.
		if len(found) == 0 && !hid && hidden(s, k, name) != nil {
			hid = true
			closed, err := d.HideKeyboard(sc.Context())
			if err != nil {
				return false, fmt.Errorf("closing the keyboard over the %s %s: %w", quoted(name), k.noun, err)
			}
			if closed {
				return false, nil
			}
		}
		return len(found) == 1, nil
	})
	switch {
	case err != nil:
		return nil, false, err
	case ok:
		return found[0], looks > 1, nil
	case len(found) > 1:
		return nil, false, several(app, found, k, name)
	}
	return nil, false, missing(app, last, k, name)
}

// settle waits, two seconds at most, until the control's part of the screen is still: a control
// that has only just appeared may still be coming in, as a dialog grows and fades in, and a tap on
// it then is lost (iOS's Save Password dialog after a sign-in). A person waits for it to land.
func settle(ctx context.Context, d Device, r Rect) error {
	win, err := d.Session().Window(ctx)
	if err != nil || win.Width <= 0 || r.Width <= 0 || r.Height <= 0 {
		return err
	}
	var last image.Image
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); {
		b, err := d.Session().Screenshot(ctx)
		if err != nil {
			return err
		}
		shot, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("the screenshot is not a PNG: %w", err)
		}
		// The screenshot is in pixels, the control in the window's points.
		scale := float64(shot.Bounds().Dx()) / win.Width
		area := image.Rect(int(r.X*scale), int(r.Y*scale), int((r.X+r.Width)*scale), int((r.Y+r.Height)*scale)).Add(shot.Bounds().Min).Intersect(shot.Bounds())
		part := image.NewRGBA(image.Rect(0, 0, area.Dx(), area.Dy()))
		draw.Draw(part, part.Bounds(), shot, area.Min, draw.Src)
		if last != nil && imagediff.Compare(last, part, imagediff.Options{}).Differ == 0 {
			return nil
		}
		last = part
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

// element is the one control of kind k named name, as Appium refers to it:
// found by its name on the screen, or by a selector.
func element(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*appium.Element, error) {
	el, _, _, err := target(sc, d, app, k, name, wait)
	return el, err
}

// target is the control of that name, the node the screen has for it (none for a control a
// selector names), and whether it came only after the first look at the screen.
func target(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*appium.Element, *Node, bool, error) {
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
			return nil, nil, false, err
		}
		if !found {
			return nil, nil, false, core.Failf("No %s in the %s app matches %q", k.noun, app, name)
		}
		return el, nil, false, nil
	}
	deadline := time.Now().Add(wait)
	for {
		n, came, err := look(sc, d, app, k, name, time.Until(deadline))
		if err != nil {
			return nil, nil, false, err
		}
		if n.ByPoint {
			return nil, n, came, nil
		}
		el, err := d.Session().Find(sc.Context(), n.Using, n.Value)
		if !appium.IsNoSuchElement(err) || !time.Now().Before(deadline) {
			return el, n, came, err
		}
		// The control moved between reading the screen and finding it, like
		// a dialog's button as the dialog comes in: read the screen again.
		select {
		case <-sc.Context().Done():
			return nil, nil, false, sc.Context().Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// scrollTo scrolls until the control is shown: the platform scrolls to it (iOS to the node its
// screen has out of view, Android until its text shows), else it pages down, then up.
func scrollTo(sc *core.Scenario, d Device, app string, k kind, name string) error {
	if s, err := d.Screen(sc.Context()); err == nil && len(matches(s, k, name)) == 0 {
		if ok, err := d.ScrollToShow(sc.Context(), hidden(s, k, name), name); err == nil && ok {
			if s, err := d.Screen(sc.Context()); err == nil && len(matches(s, k, name)) > 0 {
				return nil
			}
		}
	}
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

// minLooks is how many times a wait looks before it gives up, however long
// each look takes: a busy machine reads a screen in seconds, and a check that
// gave up after one look would fail on it.
const minLooks = 3

// waitUntil calls check until it reports true, or d has passed and it has
// looked minLooks times.
func waitUntil(sc *core.Scenario, d time.Duration, check func() (bool, error)) (bool, error) {
	deadline := time.Now().Add(d)
	for looks := 1; ; looks++ {
		ok, err := check()
		if err != nil || ok {
			return ok, err
		}
		if !time.Now().Before(deadline) && looks >= minLooks {
			return false, nil
		}
		select {
		case <-sc.Context().Done():
			return false, sc.Context().Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
