package mobilecore

import (
	"context"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// A control its driver cannot hit. iOS's test framework taps an element at a point it works out
// from what the app says of the element, and for some it cannot work one out: a radio button of
// a web page, past the page's first screen, is one ("activation point invalid"). It then taps
// nothing, and the driver answers that it tapped. The driver does say so when asked (the
// element is not hittable), so the step asks first, and taps such a control as a finger does: at
// its place on the screen, once it shows there.

// hitTests reports whether the app's platform has a driver that says which controls it cannot
// hit.
func hitTests(sc *core.Scenario, app string) bool {
	a, err := mobileApp(sc, app)
	return err == nil && a.Platform.Kind() == "ios"
}

// tapWhereItIs taps the control at its place on the screen when the driver says it cannot hit
// it, and reports whether it did: a control out of view is brought into view first, and one the
// driver can hit by then is left to the driver, as is one that still does not show.
func tapWhereItIs(ctx context.Context, sc *core.Scenario, d Device, el *appium.Element, what string) (bool, error) {
	unhittable := func() bool {
		hit, err := el.Attribute(ctx, "hittable")
		return err == nil && hit == "false"
	}
	shows := func() bool {
		shown, err := el.Attribute(ctx, "visible")
		return err == nil && shown == "true"
	}
	if !unhittable() {
		return false, ctx.Err()
	}
	if !shows() {
		if err := bringIntoView(ctx, d, el); err != nil {
			return false, err
		}
		if !unhittable() || !shows() {
			return false, ctx.Err()
		}
	}
	r, err := el.Rect(ctx)
	if err != nil || r.Width <= 0 || r.Height <= 0 {
		return false, ctx.Err() //nolint:nilerr // a control that cannot say where it is is the driver's to tap
	}
	x, y := r.X+r.Width/2, r.Y+r.Height/2
	sc.Log("the driver cannot place its own tap on the %s: it is tapped where it is, at %.0f, %.0f", what, x, y)
	return true, d.Session().Drag(ctx, x, y, x, y, 0)
}

// bringIntoView scrolls until the control shows, as the scroll step does for a control found by
// its name. Where no scroll shows it while a keyboard is up (it is under the keyboard and the
// page is at its end, or the page goes back to the field being typed in), the keyboard is
// closed, and the control scrolled to again. The control is at rest when it returns: a tap on
// one still moving lands where it was.
func bringIntoView(ctx context.Context, d Device, el *appium.Element) error {
	// A control that moves between reading the screen and scrolling to it (a page still laying
	// itself out) is read again, a few times.
	closed := false
	for range 5 {
		r, err := el.Rect(ctx)
		if err != nil {
			return nil //nolint:nilerr // a control that cannot say where it is is left where it is
		}
		s, err := d.Screen(ctx)
		if err != nil {
			return err
		}
		n := nodeAt(s, Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height})
		if n == nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}
		shown, err := d.ScrollToShow(ctx, n, n.Name())
		if appium.IsNoSuchElement(err) || appium.IsStale(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !shown && !closed {
			closed = true
			hid, err := d.HideKeyboard(ctx)
			if err != nil {
				return err
			}
			if hid {
				// The screen without the keyboard is another: the control is scrolled to again.
				continue
			}
		}
		break
	}
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); {
		if moved, err := moving(ctx, el); err != nil || !moved {
			break
		}
	}
	return ctx.Err()
}

// nodeAt is the screen's node at exactly that place, the innermost when several are, or nil.
func nodeAt(s *Screen, r Rect) *Node {
	var found *Node
	for _, n := range s.Nodes {
		if n.Bounds == r && n.Using != "" {
			found = n
		}
	}
	return found
}
