//go:build darwin

package desktopmacos

import (
	"fmt"
	"strings"
	"time"

	"github.com/nimbusxr/axx/packs/desktop/internal/ax"
)

// area is a rectangle on the screen, in points.
type area struct{ x, y, w, h float64 }

func frameOf(e *ax.Element) (area, bool) {
	pos, size, err := e.Frame()
	return area{pos.X, pos.Y, size.Width, size.Height}, err == nil
}

func (a area) intersect(b area) area {
	x, y := max(a.x, b.x), max(a.y, b.y)
	return area{x, y, max(min(a.x+a.w, b.x+b.w)-x, 0), max(min(a.y+a.h, b.y+b.h)-y, 0)}
}

func (a area) empty() bool      { return a.w <= 0 || a.h <= 0 }
func (a area) middle() ax.Point { return ax.Point{X: a.x + a.w/2, Y: a.y + a.h/2} }
func (a area) contains(p ax.Point) bool {
	return p.X >= a.x && p.X < a.x+a.w && p.Y >= a.y && p.Y < a.y+a.h
}
func (a area) holds(b area) bool     { return b.y >= a.y-1 && b.y+b.h <= a.y+a.h+1 }
func (a area) coveredBy(b area) bool { return b.y <= a.y+1 && b.y+b.h >= a.y+a.h-1 }
func (a area) String() string        { return fmt.Sprintf("%.0f,%.0f %.0fx%.0f", a.x, a.y, a.w, a.h) }

// scrolls is whether an element is an area that shows only part of what it
// holds: a scroll area, and a table or list (their own scroll areas, in
// toolkits that give them none, like Qt).
func scrolls(e *ax.Element) bool {
	switch e.String("AXRole") {
	case "AXScrollArea", "AXTable", "AXOutline", "AXList":
		return true
	}
	return false
}

// clipping are the areas the element is in, the innermost first. inWindow
// is whether the element is in a window (a menu bar's items are not).
func clipping(e *ax.Element) (areas []*ax.Element, inWindow bool) {
	for at, ok := parentOf(e); ok; at, ok = parentOf(at) {
		switch {
		case scrolls(at):
			areas = append(areas, at)
		case at.String("AXRole") == "AXWindow":
			return areas, true
		}
	}
	return areas, false
}

// visible is the part of the element that shows: in each area that clips
// it, in its window, and in the screen's visible frame (not under the menu
// bar or the Dock).
func (p *proc) visible(e *ax.Element) (area, bool) {
	v, ok := frameOf(e)
	if !ok {
		return v, false
	}
	areas, inWindow := clipping(e)
	for _, a := range areas {
		if f, ok := frameOf(a); ok && !f.empty() {
			v = v.intersect(f)
		}
	}
	if w, ok := frameOf(p.window()); ok && inWindow {
		v = v.intersect(w)
	}
	v = v.intersect(p.screen)
	return v, !v.empty()
}

// inView is whether a click in the element's middle reaches it: the middle
// shows, and the app has there the element, or one in exactly its place, or
// a part of it under the area that holds it (Java makes its elements anew
// each time they are read, and hits a row's cell as the table's; WebKit hits
// a checkbox's label). A hit test that stops above the element, in the area
// that holds it, says nothing covers it.
func (p *proc) inView(e *ax.Element) bool {
	f, ok := frameOf(e)
	if !ok || f.w < 2 || f.h < 2 {
		// Flutter squashes what is scrolled out of its area onto the area's
		// edge, a point high or wide.
		return false
	}
	if v, ok := p.visible(e); !ok || !v.contains(f.middle()) {
		return false
	}
	hit, err := p.root.ElementAt(f.middle())
	if err != nil {
		return true // no hit test: what clips it is all there is to go by
	}
	// Its ancestors up to the area that holds it.
	var above []*ax.Element
	for at, ok := parentOf(e); ok; at, ok = parentOf(at) {
		above = append(above, at)
		if scrolls(at) || at.String("AXRole") == "AXWindow" {
			break
		}
	}
	isAbove := func(x *ax.Element) bool {
		for _, a := range above {
			if a.Equal(x) {
				return true
			}
		}
		return false
	}
	h, _ := frameOf(hit)
	inside := h.x >= f.x && h.y >= f.y && h.x+h.w <= f.x+f.w && h.y+h.h <= f.y+f.h
	if isAbove(hit) {
		return true
	}
	for at, ok := hit, true; ok; at, ok = parentOf(at) {
		if g, ok := frameOf(at); ok && g == f || at.Equal(e) || inside && isAbove(at) {
			return true
		}
		if at.String("AXRole") == "AXWindow" {
			break
		}
	}
	return false
}

// scrollIntoView brings the element into view as a person does. The app
// scrolls it there when asked (AppKit and the web engines scroll every area
// it is in); else the wheel turns over each area it is in, the outermost
// first, until the next one in (or the element) shows in it.
func (p *proc) scrollIntoView(e *ax.Element) error {
	if p.inView(e) {
		return nil
	}
	if e.Perform("AXScrollToVisible") == nil {
		time.Sleep(300 * time.Millisecond)
		if p.inView(e) {
			return nil
		}
	}
	areas, _ := clipping(e)
	if len(areas) == 0 {
		return nil // in nothing that scrolls, as far as the tree says (Flutter's lists are groups)
	}
	if err := p.Front(); err != nil {
		return err
	}
	for i := len(areas) - 1; i >= 0; i-- {
		inner := e
		if i > 0 {
			inner = areas[i-1]
		}
		p.wheelInto(areas[i], inner)
	}
	if p.inView(e) {
		return nil
	}
	f, _ := frameOf(e)
	v, _ := p.visible(e)
	var in []string
	for _, a := range areas {
		g, _ := frameOf(a)
		in = append(in, a.String("AXRole")+" "+g.String())
	}
	w, _ := frameOf(p.window())
	return fmt.Errorf("scrolled every area the %s %q is in (%s), and it is not in view: %v of it (at %v) shows, in the window at %v and the screen's visible frame %v",
		e.String("AXRole"), name(e), strings.Join(in, ", "), v, f, w, p.screen)
}

// wheelInto turns the wheel over the area until what it holds (inner) shows
// in it, or fills it when it is the taller. The wheel turns over a part of
// the area that no area inside it covers (a list, a table), as that one
// would take the turns.
func (p *proc) wheelInto(a, inner *ax.Element) {
	var inside []*ax.Element
	for _, k := range children(a) {
		areasIn(k, &inside)
	}
	for range 80 {
		v, ok := p.visible(a)
		f, ok2 := frameOf(inner)
		if !ok || !ok2 || v.holds(f) || v.coveredBy(f) {
			return
		}
		lines := -2 // down
		if f.y < v.y {
			lines = 2 // up
		}
		at, ok := spot(v, inside)
		if !ok {
			return
		}
		_ = ax.Scroll(at, lines)
		time.Sleep(40 * time.Millisecond)
	}
}

// areasIn adds e, or the outermost areas under it, to areas.
func areasIn(e *ax.Element, areas *[]*ax.Element) {
	if scrolls(e) {
		*areas = append(*areas, e)
		return
	}
	for _, k := range children(e) {
		areasIn(k, areas)
	}
}

// spot is a point in v that none of the areas covers: its middle, or near
// one of its edges.
func spot(v area, areas []*ax.Element) (ax.Point, bool) {
	m := v.middle()
	for _, pt := range []ax.Point{
		m,
		{X: v.x + 6, Y: m.Y},
		{X: v.x + v.w - 6, Y: m.Y},
		{X: m.X, Y: v.y + 6},
		{X: m.X, Y: v.y + v.h - 6},
		{X: v.x + 6, Y: v.y + 6},
		{X: v.x + 6, Y: v.y + v.h - 6},
	} {
		free := true
		for _, a := range areas {
			if f, ok := frameOf(a); ok && f.contains(pt) {
				free = false
				break
			}
		}
		if free {
			return pt, true
		}
	}
	return ax.Point{}, false
}

// scrollTo finds the control and brings it into view, as a person looks for
// it: some toolkits (Qt 5) have only the rows that show in their tree, so
// each list and table is brought into view and its wheel turned until the
// control is there.
func (p *proc) scrollTo(kind, name string) (*ax.Element, error) {
	first := func() *ax.Element {
		if found := p.find(kind, name, true); len(found) > 0 {
			return found[0]
		}
		return nil
	}
	if e := first(); e != nil {
		return e, p.scrollIntoView(e)
	}
	var boxes []*ax.Element
	for _, w := range p.windows() {
		ax.Walk(w, func(e *ax.Element) bool {
			if scrolls(e) {
				boxes = append(boxes, e)
			}
			return true
		})
	}
	for _, box := range boxes {
		if err := p.scrollIntoView(box); err != nil {
			return nil, err
		}
		for _, lines := range []int{-3, 3} { // down to the end, then up to the start
			for range 40 {
				if e := first(); e != nil {
					return e, p.scrollIntoView(e)
				}
				v, ok := p.visible(box)
				if !ok {
					break
				}
				if err := p.Front(); err != nil {
					return nil, err
				}
				_ = ax.Scroll(v.middle(), lines)
				time.Sleep(40 * time.Millisecond)
			}
		}
	}
	return nil, nil
}

// settled waits for the element to stop moving (a tab slides in), 2 seconds
// at most: a click lands where it is.
func settled(e *ax.Element) {
	last, _, _ := e.Frame()
	for wait := time.Now(); time.Since(wait) < 2*time.Second; {
		time.Sleep(100 * time.Millisecond)
		now, _, err := e.Frame()
		if err != nil || now == last {
			return
		}
		last = now
	}
}
