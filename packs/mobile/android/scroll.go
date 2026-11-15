package mobileandroid

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// wholeScreen has the session list what the screen has out of view as well (a page's fields
// further down, a scroll view's content past its edges), marked as not shown, where it is: a
// step then knows that a control is there and where, and has its view show it, with no search.
// It is for a device with axx's scroll service: without it, a control out of view is found as
// before, by looking a page at a time.
func wholeScreen(ctx context.Context, s *appium.Session) {
	_ = s.Settings(ctx, map[string]any{"allowInvisibleElements": true})
}

// scrollService is axx's scroll service on the scenario's device; nil when it has none.
func (d *running) scrollService() *scrolls {
	if d.dev == nil {
		return nil
	}
	return d.dev.scrolls
}

// request is a request to the scroll service about a control, which the service finds by what the
// screen's source gave of it: its class, description, resource ID and place, and its text. A
// field is found without its text, which is what it holds, and which axx reads apart from its
// name (a web page's number field).
func request(n *mobilecore.Node, op string) map[string]any {
	req := map[string]any{
		"op": op, "class": n.Class, "desc": n.Label, "id": n.ID,
		"bounds": []int{int(n.Bounds.X), int(n.Bounds.Y), int(n.Bounds.X + n.Bounds.Width), int(n.Bounds.Y + n.Bounds.Height)},
	}
	if n.Role != mobilecore.RoleField {
		req["text"] = n.Text
	}
	return req
}

// page asks a view to scroll a page toward direction, and reports whether it moved. asked is
// false when the view could not be asked: the device has no scroll service, the service does not
// find the view, or the view offers no scroll at all (a view only a finger scrolls).
func (d *running) page(ctx context.Context, n *mobilecore.Node, direction string) (moved, asked bool) {
	sv := d.scrollService()
	if sv == nil {
		return false, false
	}
	req := request(n, "scroll")
	req["direction"] = direction
	for try := 0; ; try++ {
		a, err := sv.ask(ctx, req)
		if err != nil || !a.Found || !a.Scrolls {
			return false, false
		}
		// A view that took the action and did not move was still busy with the scroll before
		// (a web page ignores a scroll asked for as it ends one): it is asked once more.
		if a.Moved || !a.Performed || try > 0 {
			return a.Moved, true
		}
		select {
		case <-ctx.Done():
			return false, true
		case <-time.After(120 * time.Millisecond):
		}
	}
}

// show has a control the screen lists out of view brought onto the screen by its view, and
// reports whether it is there: on the screen, and above the keyboard when one shows. A control a
// keyboard still covers once its view was scrolled is not: the step then closes the keyboard.
func (d *running) show(ctx context.Context, n *mobilecore.Node) bool {
	sv := d.scrollService()
	s, err := d.Screen(ctx)
	if sv == nil || err != nil {
		return false
	}
	req := request(n, "show")
	var at []float64
	for range 3 {
		a, err := sv.ask(ctx, req)
		if err != nil || !a.Found || !a.Offered || !a.Performed || len(a.Bounds) != 4 {
			return false
		}
		// Where it is now is where the service finds it next.
		at, req["bounds"] = a.Bounds, a.Bounds
		if onScreen(at, s.Size) {
			break
		}
		// A view busy with the scroll before takes the action and does not move.
		select {
		case <-ctx.Done():
			return false
		case <-time.After(120 * time.Millisecond):
		}
	}
	if !onScreen(at, s.Size) {
		return false
	}
	kb, up := d.keyboard(ctx)
	if !up {
		return true
	}
	// With a keyboard up, a page keeps the field being typed in on the screen: a web page shows
	// the control it was asked for, and goes back to the field a moment later. So where the
	// control is is asked once more, a moment on. One that did not stay is not shown: the step
	// closes the keyboard, as a person does to get to the rest of a page, and scrolls again.
	where := func() ([]float64, bool) {
		req["op"] = "where"
		a, err := sv.ask(ctx, req)
		if err != nil || !a.Found || len(a.Bounds) != 4 {
			return nil, false
		}
		req["bounds"] = a.Bounds
		return a.Bounds, onScreen(a.Bounds, s.Size)
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(300 * time.Millisecond):
	}
	at, stayed := where()
	if !stayed {
		return false
	}
	if !covers(kb, at) {
		return true
	}
	// Under the keyboard: its view is scrolled a page on, which brings it above the keyboard
	// unless the view is at its end.
	for view := n.Parent; view != nil; view = view.Parent {
		if !view.Scrolling {
			continue
		}
		if moved, _ := d.page(ctx, view, "down"); !moved {
			return false
		}
		at, stayed = where()
		return stayed && !covers(kb, at)
	}
	return false
}

// onScreen is whether a place (left, top, right, bottom) is on a screen of that size. A web page
// gives what is out of view a place cut to the screen's edge, its bottom above its top.
func onScreen(b []float64, size mobilecore.Rect) bool {
	return b[0] < b[2] && b[1] < b[3] && b[2] > 0 && b[3] > 0 && b[0] < size.Width && b[1] < size.Height
}

// covers is whether the keyboard covers the middle of a place.
func covers(kb mobilecore.Rect, b []float64) bool {
	x, y := (b[0]+b[2])/2, (b[1]+b[3])/2
	return x >= kb.X && x < kb.X+kb.Width && y >= kb.Y && y < kb.Y+kb.Height
}

var imeFrame = regexp.MustCompile(`(?:type=ime|ITYPE_IME) frame=\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\][^\n]*visible=true`)

// keyboard is where the keyboard is on the screen, when one shows: the window manager's frame
// of the input method, which is what covers the app.
func (d *running) keyboard(ctx context.Context) (mobilecore.Rect, bool) {
	if d.dev == nil {
		return mobilecore.Rect{}, false
	}
	out, err := d.sdk.shell(ctx, d.dev.serial, "dumpsys window | grep -E 'type=ime|ITYPE_IME'")
	if err != nil {
		return mobilecore.Rect{}, false
	}
	return keyboardFrame(out)
}

func keyboardFrame(windows string) (mobilecore.Rect, bool) {
	m := imeFrame.FindStringSubmatch(windows)
	if m == nil {
		return mobilecore.Rect{}, false
	}
	v := make([]float64, 4)
	for i := range v {
		v[i], _ = strconv.ParseFloat(m[i+1], 64)
	}
	r := mobilecore.Rect{X: v[0], Y: v[1], Width: v[2] - v[0], Height: v[3] - v[1]}
	return r, r.Width > 0 && r.Height > 0
}

// under marks what the keyboard covers as not shown, as a person does not see it: a step that
// uses such a control has its view scroll it above the keyboard, or closes the keyboard. Only a
// screen with something in focus is asked for its keyboard: a keyboard shows for a field that
// has the focus.
func (d *running) under(ctx context.Context, src string, s *mobilecore.Screen) {
	if d.dev == nil || !strings.Contains(src, `focused="true"`) {
		return
	}
	kb, up := d.keyboard(ctx)
	if !up {
		return
	}
	for _, n := range s.Nodes {
		b := n.Bounds
		if n.Displayed && b.Width > 0 && b.Height > 0 && covers(kb, []float64{b.X, b.Y, b.X + b.Width, b.Y + b.Height}) {
			n.Displayed = false
		}
	}
}

// OutOfView is whether a control a selector found is out of view: the driver finds what the
// screen lists out of view too, and does not scroll to it to tap it, so the step does.
func (d *running) OutOfView(ctx context.Context, el *appium.Element) bool {
	if d.scrollService() == nil {
		return false
	}
	shown, err := el.Attribute(ctx, "displayed")
	return err == nil && shown == "false"
}

// passed is what a device remembers of the controls that went by as it scrolled: Android lists
// nothing of a list's rows past what the list has made, so a step that looks for one looks a
// page at a time, and knowing that a row went off the top says which way to look first. It only
// ever chooses the first direction; a memory that is out of date costs a look the other way.
type passed struct {
	mu      sync.Mutex
	toward  string         // the direction the last scroll went, until the screen is read again
	showing []string       // the names the screen showed when it was last read
	where   map[string]int // a name that went by: 1 above the screen, -1 below
}

// passing notes that the screen was scrolled toward direction: what its next reading no longer
// shows went by that way.
func (d *running) passing(direction string) {
	if direction != "up" && direction != "down" {
		return
	}
	d.seen.mu.Lock()
	d.seen.toward = direction
	d.seen.mu.Unlock()
}

// read keeps what a reading of the screen shows.
func (d *running) read(s *mobilecore.Screen) {
	var names []string
	for _, n := range s.Visible() {
		if name := n.Name(); name != "" {
			names = append(names, name)
		}
	}
	d.seen.mu.Lock()
	defer d.seen.mu.Unlock()
	if d.seen.where == nil || len(d.seen.where) > 4000 {
		d.seen.where = map[string]int{}
	}
	if d.seen.toward != "" {
		side := 1 // scrolled down: what left went off the top
		if d.seen.toward == "up" {
			side = -1
		}
		now := map[string]bool{}
		for _, name := range names {
			now[name] = true
		}
		for _, name := range d.seen.showing {
			if !now[name] {
				d.seen.where[name] = side
			}
		}
		d.seen.toward = ""
	}
	for _, name := range names {
		delete(d.seen.where, name)
	}
	d.seen.showing = names
}

// forget drops what went by: the app shows another screen.
func (d *running) forget() {
	d.seen.mu.Lock()
	d.seen.toward, d.seen.showing, d.seen.where = "", nil, nil
	d.seen.mu.Unlock()
}

// toward is the directions to look for a name in, in order: down and then up, as a person
// reads a screen, or up first for a name that went off the top.
func (d *running) toward(name string) []string {
	want := strings.Join(strings.Fields(name), " ")
	d.seen.mu.Lock()
	defer d.seen.mu.Unlock()
	for seen, side := range d.seen.where {
		if side > 0 && strings.Contains(seen, want) {
			return []string{"up", "down"}
		}
	}
	return []string{"down", "up"}
}
