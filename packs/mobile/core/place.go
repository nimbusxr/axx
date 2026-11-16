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
		n := nodeOf(ctx, d, s, el, Rect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height})
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

// nodeOf is the screen's node an element is: the one at the element's place. A screen can have
// several nodes at one place. One inside another (a row and the text that fills it) is scrolled
// to as the other is, and the innermost is taken. Nodes apart from each other are another matter:
// a web page gives every control below the screen the same place, cut to the screen's edge, so
// the answers "Yes" of five questions further down are all there, and the one taken for the
// element is the one the page is asked to show. The driver tells those apart: it finds what a
// locator matches in the screen's order, and gives an element the same ID each time it finds it.
func nodeOf(ctx context.Context, d Device, s *Screen, el *appium.Element, r Rect) *Node {
	var at []*Node
	for _, n := range s.Nodes {
		if n.Bounds == r && n.Using != "" {
			at = append(at, n)
		}
	}
	if len(at) == 0 {
		return nil
	}
	innermost := at[len(at)-1]
	if nested(at) {
		return innermost
	}
	asked := map[[2]string]bool{}
	for _, n := range at {
		locator := [2]string{n.Using, n.Value}
		if asked[locator] {
			continue
		}
		asked[locator] = true
		// What the locator matches on this screen, in its order, and what the driver finds by
		// it: the element is the one at its own place among them. A screen that changed in
		// between has another count, and says nothing.
		var alike []*Node
		for _, m := range s.Nodes {
			if m.Using == n.Using && m.Value == n.Value {
				alike = append(alike, m)
			}
		}
		els, err := d.Session().FindAll(ctx, n.Using, n.Value)
		if err != nil || len(els) != len(alike) {
			continue
		}
		for i, e := range els {
			if e.ID == el.ID {
				return alike[i]
			}
		}
	}
	return innermost
}

// nested is whether each of the nodes, in the screen's order, is inside the one before it.
func nested(nodes []*Node) bool {
	for i := 1; i < len(nodes); i++ {
		inside := false
		for p := nodes[i].Parent; p != nil && !inside; p = p.Parent {
			inside = p == nodes[i-1]
		}
		if !inside {
			return false
		}
	}
	return true
}
