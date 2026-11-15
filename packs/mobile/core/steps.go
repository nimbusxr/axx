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
				if dir == "forward" || dir == "back" {
					return fmt.Errorf("a swipe goes up, down, left or right, not %s", dir)
				}
				return onDevice(sc, a.String(0), false, func(ctx context.Context, d Device) error {
					if err := still(ctx, d); err != nil {
						return err
					}
					return d.Swipe(ctx, dir)
				})
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

// note keeps, in the run's log, a scroll that failed under a search the step goes on from.
func note(sc *core.Scenario, what, control string, err error) {
	if l := sc.Suite().Logger(); l != nil {
		l.Debug(what, "control", control, "error", err)
	}
}

// reachAfter is how long a step that acts on a control waits for one the screen does not list
// before it scrolls for it: a control that is only late (a sheet's button, a list's row after a
// request) comes in that time on a busy machine, and a screen scrolled while it comes in is not
// the screen the scenario meant. A person looks a moment, too, before they scroll.
var reachAfter = 1500 * time.Millisecond

// find is the one control of kind k named name, as the screen shows it now (a check's own
// wait, within its time, is around it): a few looks, as a busy machine needs.
func find(sc *core.Scenario, d Device, app string, k kind, name string) (*Node, error) {
	n, _, err := look(sc, d, app, k, name, 0)
	return n, err
}

// look is find, and whether the control came only after the first look at the screen: one that
// may still be coming in, as a dialog does.
func look(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*Node, bool, error) {
	return lookFor(sc, d, app, k, name, wait, false)
}

// lookFor is look for a step that acts on the control when reach is true: a control the screen
// has out of view (further down a page) is scrolled into view, once, as a person scrolls to what
// they are about to use. A check does not: what it asks is whether the control shows.
func lookFor(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration, reach bool) (*Node, bool, error) {
	var found []*Node
	reached, since := false, time.Now()
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
		// The control is there but not shown, and the step is about to use it: it is scrolled to,
		// above the keyboard when one shows, which stays as it is.
		if n := hidden(s, k, name); len(found) == 0 && reach && !reached && looks > 1 && n != nil {
			// At rest first: a control still coming in with its sheet is not one to scroll to.
			if err := still(sc.Context(), d); err != nil {
				return false, err
			}
			shown, err := d.ScrollToShow(sc.Context(), n, name)
			// The control moved between reading the screen and scrolling to it, as a page
			// still laying itself out moves its fields: read the screen again.
			if appium.IsNoSuchElement(err) || appium.IsStale(err) {
				return false, nil
			}
			reached = true
			if err != nil {
				return false, fmt.Errorf("scrolling to the %s %s: %w", quoted(name), k.noun, err)
			}
			if shown {
				return false, nil
			}
		}
		// The screen does not list the control at all, and the step is about to use it. A phone
		// lists nothing of a list or a screen past what shows (Android never does; iOS not the
		// rows a list has yet to make), so the control may be further down: once the screen is at
		// rest and still without it, the step scrolls for it as the scroll step does, until its
		// name shows. A control only late in coming (a sheet's, a screen's after a request) is
		// waited for first: the step scrolls once it has been missing for reachAfter.
		if len(found) == 0 && reach && !reached && time.Since(since) >= reachAfter && hidden(s, k, name) == nil {
			reached = true
			if err := still(sc.Context(), d); err != nil {
				return false, err
			}
			// A screen that changes under the search (a sheet that came in after all) fails a
			// scroll, not the step: the look goes on for what the screen has by then.
			if _, err := scrollFor(sc, d, k, name, false); err != nil && sc.Context().Err() == nil {
				note(sc, "scrolling for a control the screen does not list", name, err)
			}
			return false, sc.Context().Err()
		}
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
	var last image.Image
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); {
		part, err := looksOf(ctx, d, r)
		if err != nil || part == nil {
			return err
		}
		if last != nil && same(last, part) {
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

// landed is the control to tap once it has stopped moving, two seconds at most. A list still
// scrolling, or a drawer still sliding in, moves its controls, and a tap on one then lands where it
// was, or only stops the list or the drawer (Android's file picker's Downloads, on a busy
// emulator). A control that moved is looked for again where it landed: the row the platform found
// may show another row's text by then, as a list draws its rows again as it scrolls. One still
// moving after two seconds is tapped where it is, as a person taps a control that never keeps
// still.
func landed(ctx context.Context, sc *core.Scenario, d Device, app string, k kind, name string, el *appium.Element) (*appium.Element, error) {
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); {
		if moved, err := moving(ctx, el); err != nil || !moved {
			// Where it is is only asked to wait for it: a control that cannot say is tapped as it is,
			// and one gone from the driver's cache found again as the tap fails.
			return el, ctx.Err()
		}
		found, _, _, err := target(sc, d, app, k, name, actionTimeout)
		if err != nil {
			return nil, err
		}
		if found != nil {
			el = found
		}
	}
	return el, nil
}

// moving reports whether a control is moving: where it is, read twice a moment apart, differs.
func moving(ctx context.Context, el *appium.Element) (bool, error) {
	before, err := el.Rect(ctx)
	if err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}
	after, err := el.Rect(ctx)
	return err == nil && after != before, err
}

// still waits, two seconds at most, until the screen has stopped moving, its system bars left out
// (the clock): a person swipes a screen at rest. A swipe while a sheet still slides away is lost,
// as iOS drops touches then (the Save Password sheet after a sign-in, as the app's screen comes
// back), and a pull to refresh with it.
func still(ctx context.Context, d Device) error {
	var last image.Image
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); {
		shot, err := maskedShot(ctx, d)
		if err != nil {
			return err
		}
		if last != nil && same(last, shot) {
			return nil
		}
		last = shot
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

// looksOf is what the screen shows where a control is (in the window's points), or nil when it
// cannot say.
func looksOf(ctx context.Context, d Device, r Rect) (image.Image, error) {
	win, err := d.Session().Window(ctx)
	if err != nil || win.Width <= 0 || r.Width <= 0 || r.Height <= 0 {
		return nil, err
	}
	b, err := d.Session().Screenshot(ctx)
	if err != nil {
		return nil, err
	}
	shot, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("the screenshot is not a PNG: %w", err)
	}
	// The screenshot is in pixels, the control in the window's points.
	scale := float64(shot.Bounds().Dx()) / win.Width
	area := image.Rect(int(r.X*scale), int(r.Y*scale), int((r.X+r.Width)*scale), int((r.Y+r.Height)*scale)).Add(shot.Bounds().Min).Intersect(shot.Bounds())
	part := image.NewRGBA(image.Rect(0, 0, area.Dx(), area.Dy()))
	draw.Draw(part, part.Bounds(), shot, area.Min, draw.Src)
	return part, nil
}

// same reports whether two looks of a control are the same, pixel for pixel.
func same(a, b image.Image) bool { return imagediff.Compare(a, b, imagediff.Options{}).Differ == 0 }

// element is the one control of kind k named name, as Appium refers to it:
// found by its name on the screen, or by a selector.
func element(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*appium.Element, error) {
	el, _, _, err := target(sc, d, app, k, name, wait)
	return el, err
}

// reached is target for a step that acts on the control: one the screen has out of view is
// scrolled into view first (lookFor).
func reached(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*appium.Element, *Node, bool, error) {
	return targetFor(sc, d, app, k, name, wait, true)
}

// target is the control of that name, the node the screen has for it (none for a control a
// selector names), and whether it came only after the first look at the screen.
func target(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration) (*appium.Element, *Node, bool, error) {
	return targetFor(sc, d, app, k, name, wait, false)
}

func targetFor(sc *core.Scenario, d Device, app string, k kind, name string, wait time.Duration, reach bool) (*appium.Element, *Node, bool, error) {
	if using, value, ok := selector(name); ok {
		var el *appium.Element
		find := func() (bool, error) {
			e, err := d.Session().Find(sc.Context(), using, value)
			if appium.IsNoSuchElement(err) {
				return false, nil
			}
			el = e
			return err == nil, err
		}
		paged, since := false, time.Now()
		found, err := waitUntil(sc, wait, func() (bool, error) {
			ok, err := find()
			if err != nil || ok || !reach || paged || time.Since(since) < reachAfter {
				return ok, err
			}
			// Nothing matches, and the step is about to use the control. A driver that finds
			// only what the screen lists (Android's) finds nothing of what is further down: once
			// the screen is at rest, the step pages down it, then up, until something matches.
			paged = true
			if err := still(sc.Context(), d); err != nil {
				return false, err
			}
			ok, err = pageFor(sc, d, find)
			if err != nil && sc.Context().Err() == nil {
				// A screen that changes under the search fails a scroll, not the step.
				note(sc, "paging for a control a selector names", name, err)
				return false, nil
			}
			return ok, err
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
		n, came, err := lookFor(sc, d, app, k, name, time.Until(deadline), reach)
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
	// A control a selector names is found wherever it is, and brought into view from there.
	if _, _, ok := selector(name); ok {
		el, _, _, err := reached(sc, d, app, k, name, actionTimeout)
		if err != nil {
			return err
		}
		if err := bringIntoView(sc.Context(), d, el); err != nil {
			return err
		}
		if shown, err := el.Attribute(sc.Context(), "visible"); err == nil && shown == "false" {
			return core.Failf("The %s %s in the %s app cannot be scrolled into view: it is not in a part of the screen that scrolls", quoted(name), k.noun, app)
		}
		return nil
	}
	shown, err := scrollFor(sc, d, k, name, true)
	if err != nil || shown {
		return err
	}
	s, err := d.Screen(sc.Context())
	if err != nil {
		return err
	}
	return missing(app, s, k, name)
}

// pageFor pages down the screen, then up, until shown says the control is there, and reports
// whether it is: for a control only a selector finds, which the platform cannot scroll to by its
// name.
func pageFor(sc *core.Scenario, d Device, shown func() (bool, error)) (bool, error) {
	for _, dir := range []string{"down", "up"} {
		for range 20 {
			if ok, err := shown(); err != nil || ok {
				return ok, err
			}
			moved, err := d.Scroll(sc.Context(), dir)
			if err != nil {
				return false, err
			}
			if !moved {
				break
			}
		}
	}
	return shown()
}

// scrollFor scrolls until the control is shown, and reports whether it is: the platform scrolls
// to it (iOS to the node its screen has out of view, Android until its text shows), else it
// pages down, then up. With rows, it looks along each row that scrolls sideways as it passes it,
// to the row's end and back.
func scrollFor(sc *core.Scenario, d Device, k kind, name string, rows bool) (bool, error) {
	if s, err := d.Screen(sc.Context()); err == nil && len(matches(s, k, name)) == 0 {
		if ok, err := d.ScrollToShow(sc.Context(), hidden(s, k, name), name); err == nil && ok {
			if s, err := d.Screen(sc.Context()); err == nil && len(matches(s, k, name)) > 0 {
				return true, nil
			}
		}
	}
	for _, dir := range []string{"down", "up"} {
		for range 20 {
			s, err := d.Screen(sc.Context())
			if err != nil {
				return false, err
			}
			if len(matches(s, k, name)) > 0 {
				return true, nil
			}
			// A row that scrolls sideways shows only what fits, and the phone lists nothing past
			// its ends: a control there is found as a person finds it, looking along each row as
			// they pass it.
			if rows {
				for _, row := range s.Rows() {
					found, err := lookAlong(sc, d, row, k, name)
					if err != nil || found {
						return found, err
					}
				}
			}
			moved, err := d.Scroll(sc.Context(), dir)
			if err != nil {
				return false, err
			}
			if !moved {
				break
			}
		}
	}
	return false, nil
}

// lookAlong scrolls a row that scrolls sideways to its end and back, and reports whether a
// control k named name showed on the way.
func lookAlong(sc *core.Scenario, d Device, row *Node, k kind, name string) (bool, error) {
	for _, dir := range []string{"right", "left"} {
		for range 10 {
			moved, err := d.ScrollRow(sc.Context(), row, dir)
			if err != nil {
				return false, err
			}
			s, err := d.Screen(sc.Context())
			if err != nil {
				return false, err
			}
			if len(matches(s, k, name)) > 0 {
				return true, nil
			}
			if !moved {
				break
			}
		}
	}
	return false, nil
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
