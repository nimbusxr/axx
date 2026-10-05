//go:build linux

package desktoplinux

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nimbusxr/axx/packs/desktop/internal/atspi"
)

var space = regexp.MustCompile(`\s+`)

// collapse is a text with its whitespace collapsed, as steps compare texts.
func collapse(s string) string { return strings.TrimSpace(space.ReplaceAllString(s, " ")) }

var textRoles = []string{"label", "static", "paragraph", "text", "heading", "caption"}

// names are what an element is called: its name and description, a text's
// text, and each of their lines.
func names(e *atspi.Element) []string {
	var out []string
	add := func(v string) {
		if v = strings.TrimSpace(v); v == "" {
			return
		}
		out = append(out, collapse(v))
		if strings.Contains(v, "\n") {
			for _, l := range strings.Split(v, "\n") {
				if l = collapse(l); l != "" {
					out = append(out, l)
				}
			}
		}
	}
	add(e.Name())
	add(e.Description())
	// A text's text, but not a field's: what a field holds is not its name.
	if slices.Contains(textRoles, e.Role()) && !e.Is(atspi.StateEditable) {
		add(e.Text())
	}
	return out
}

func name(e *atspi.Element) string {
	if n := names(e); len(n) > 0 {
		return n[0]
	}
	return ""
}

func named(e *atspi.Element, n string) bool { return slices.Contains(names(e), n) }

// visit calls fn on e and the elements under it, in the tree's order, while
// fn says to go on.
func visit(e *atspi.Element, fn func(*atspi.Element) bool) bool {
	if !fn(e) {
		return false
	}
	kids, _ := e.Children()
	for _, k := range kids {
		if !visit(k, fn) {
			return false
		}
	}
	return true
}

// shown is whether the element shows: on the screen, by its state.
func shown(e *atspi.Element) bool { return e.Is(atspi.StateShowing) }

// holds is whether the element or one under it has the text, shown or not:
// a row is known by its texts wherever it is.
func holds(e *atspi.Element, n string) bool {
	found := false
	visit(e, func(x *atspi.Element) bool {
		found = found || named(x, n) || collapse(x.Text()) == n
		return !found
	})
	return found
}

func hasText(e *atspi.Element, text string) bool {
	found := false
	visit(e, func(x *atspi.Element) bool {
		found = found || (named(x, text) || collapse(x.Text()) == text) && shown(x)
		return !found
	})
	return found
}

// is is whether e, under parent, is a control of the kind, whatever its
// name. Flutter's menus are its own drawing: their items are panels, so any
// element is one.
func (p *proc) is(kind string, e, parent *atspi.Element) bool {
	role, parentRole := e.Role(), ""
	if parent != nil {
		parentRole = parent.Role()
	}
	switch kind {
	case "button":
		return role == "push button" && parentRole != "menu bar"
	case "field":
		// An entry, or a text that can be edited (a label's text is a text too).
		return role == "entry" || role == "password text" || role == "text" && e.Is(atspi.StateEditable)
	case "checkbox":
		return role == "check box"
	case "switch":
		return role == "toggle button" || role == "switch"
	case "radio button":
		return role == "radio button"
	case "tab":
		return role == "page tab"
	case "menu":
		// A menu bar's item: a menu (GTK 3, Java), a menu item (GTK 4, Qt),
		// or a button (Chromium draws Electron's).
		return (role == "menu" || role == "menu item" || role == "push button") && parentRole == "menu bar" || p.flutter
	case "menu item":
		return (role == "menu item" || role == "check menu item" || role == "radio menu item") && parentRole != "menu bar" || p.flutter
	case "list item":
		return role == "list item"
	case "row":
		// A row; in a table with no rows around its cells, the cell: a table
		// cell (GTK), or what a table holds with nothing under it (Swing's
		// cells are their renderers, labels; GTK 4's table holds a list of
		// its rows).
		return role == "table row" || role == "table cell" && parentRole != "table row" ||
			(parentRole == "table" || parentRole == "tree table") && e.ChildCount() == 0 && role != "column header" && role != "table column header"
	case "link":
		return role == "link"
	case "image":
		return role == "image" || role == "icon"
	case "text":
		return slices.Contains(textRoles, role)
	case "element":
		return true
	}
	return false
}

// byText are the kinds named by a text they hold too.
var byText = map[string]bool{"list item": true, "row": true, "link": true}

func (p *proc) matches(kind, n string, e, parent *atspi.Element) bool {
	if !p.is(kind, e, parent) {
		return false
	}
	if named(e, n) {
		return true
	}
	return byText[kind] && holds(e, n)
}

// search adds the controls of the kind named n under e to found: a control
// in another that matches counts once, as the outer one.
func (p *proc) search(e, parent *atspi.Element, kind, n string, onlyShown bool, found *[]*atspi.Element) {
	if p.matches(kind, n, e, parent) && (!onlyShown || shown(e)) {
		*found = append(*found, e)
		return
	}
	kids, _ := e.Children()
	for _, k := range kids {
		p.search(k, e, kind, n, onlyShown, found)
	}
}

func (p *proc) find(kind, n string, onlyShown bool) []*atspi.Element {
	var found []*atspi.Element
	for _, w := range p.windows() {
		p.search(w, nil, kind, n, onlyShown, &found)
		if len(found) > 0 {
			return distinct(found)
		}
	}
	return nil
}

// distinct leaves out an element found again, or its twin: of its role and
// name, in its place (Java has an open menu's items under the menu and in
// its popup).
func distinct(found []*atspi.Element) []*atspi.Element {
	var out []*atspi.Element
	for _, e := range found {
		if !slices.ContainsFunc(out, func(o *atspi.Element) bool {
			if o.Same(e) {
				return true
			}
			r := e.Extents()
			return placed(r) && o.Extents() == r && o.Role() == e.Role() && name(o) == name(e)
		}) {
			out = append(out, e)
		}
	}
	return out
}

func (p *proc) names(kind string) []string {
	var out []string
	var each func(e, parent *atspi.Element)
	each = func(e, parent *atspi.Element) {
		if p.is(kind, e, parent) && shown(e) {
			if n := p.label(kind, e); n != "" && !slices.Contains(out, n) {
				out = append(out, n)
			}
			if kind != "element" {
				return
			}
		}
		kids, _ := e.Children()
		for _, k := range kids {
			each(k, e)
		}
	}
	for _, w := range p.windows() {
		each(w, nil)
	}
	return out
}

func (p *proc) label(kind string, e *atspi.Element) string {
	if n := name(e); n != "" || !byText[kind] {
		return n
	}
	var first string
	visit(e, func(x *atspi.Element) bool {
		if !x.Same(e) && shown(x) {
			first = name(x)
			if first == "" {
				first = collapse(x.Text())
			}
		}
		return first == ""
	})
	return first
}

// center is the middle of the element on the screen.
func center(e *atspi.Element) (int, int, bool) {
	r := e.Extents()
	if r.Width <= 0 || r.Height <= 0 {
		return 0, 0, false
	}
	return int(r.X + r.Width/2), int(r.Y + r.Height/2), true
}

// settled waits for the element to stop moving (a tab slides in), 2
// seconds at most.
func settled(e *atspi.Element) {
	last := e.Extents()
	for wait := time.Now(); time.Since(wait) < 2*time.Second; {
		time.Sleep(100 * time.Millisecond)
		now := e.Extents()
		if now == last {
			return
		}
		last = now
	}
}

// areaRoles are the roles of what shows part of what it holds, and scrolls
// the rest into view.
var areaRoles = []string{"scroll pane", "viewport", "table", "tree table", "list", "list box"}

// clipping are the areas the element is in, the innermost first, and last
// its window: GTK 4 gives a scrolled window no role of its own, so the
// window stands for the areas the tree does not name.
func (p *proc) clipping(e *atspi.Element) []*atspi.Element {
	win := p.window()
	var areas []*atspi.Element
	for at := e.Parent(); at != nil && !at.Same(win); at = at.Parent() {
		if slices.Contains(areaRoles, at.Role()) {
			areas = append(areas, at)
		}
	}
	return append(areas, win)
}

// scrollIntoView brings the element into view as a person does, and fails
// when it cannot.
func (p *proc) scrollIntoView(e *atspi.Element) error {
	p.reveal(e)
	if p.inView(e) {
		return nil
	}
	var in []string
	for _, a := range p.clipping(e) {
		in = append(in, fmt.Sprintf("%s %+v", a.Role(), a.Extents()))
	}
	return fmt.Errorf("scrolled every area the %s %q is in, and it is not in view: it is at %+v, %+v of it shows; it is in %s",
		e.Role(), name(e), e.Extents(), visiblePart(e, p.window()), strings.Join(in, ", "))
}

// reveal scrolls the element into view as far as it goes. The app scrolls
// it there when asked (Component's ScrollTo: the web engines scroll every
// area it is in); else the wheel turns over each area it is in, the
// outermost first, until the next one in (or the element) shows in it.
func (p *proc) reveal(e *atspi.Element) {
	if p.inView(e) {
		return
	}
	if e.ScrollTo() {
		time.Sleep(300 * time.Millisecond)
		if p.inView(e) {
			return
		}
	}
	areas := p.clipping(e)
	for i := len(areas) - 1; i >= 0; i-- {
		inner := e
		if i > 0 {
			inner = areas[i-1]
		}
		p.wheelInto(areas[i], inner)
	}
}

// wheelInto turns the wheel over the area until what it holds (inner) shows
// in it, or fills it when it is the taller, over a part of the area that no
// area inside it covers.
func (p *proc) wheelInto(a, inner *atspi.Element) {
	var inside []*atspi.Element
	kids, _ := a.Children()
	for _, k := range kids {
		areasIn(k, &inside)
	}
	// Where it has no place yet (GTK 3 gives what it has not drawn none), it
	// is looked for as a person looks: down to the end, then up.
	look, still := -2, 0
	for range 120 {
		v, f := visiblePart(a, p.window()), inner.Extents()
		unplaced := f.X < -1e8 || f.Y < -1e8
		holds := !unplaced && f.Y >= v.Y-1 && f.Y+f.Height <= v.Y+v.Height+1
		covers := !unplaced && f.Y <= v.Y+1 && f.Y+f.Height >= v.Y+v.Height-1
		if v.Width <= 0 || v.Height <= 0 || f.Height <= 0 || holds || covers {
			return
		}
		lines := -2 // down
		switch {
		case unplaced:
			if still >= 3 { // at the end: the other way, once
				if look > 0 {
					return
				}
				look, still = 2, 0
			}
			lines = look
		case f.Y < v.Y:
			lines = 2
		}
		x, y, ok := spot(v, inside)
		if !ok {
			return
		}
		before := fingerprint(a)
		_ = p.in.Scroll(x, y, lines)
		time.Sleep(100 * time.Millisecond)
		if fingerprint(a) == before {
			still++
		} else {
			still = 0
		}
	}
}

// areasIn adds e, or the outermost areas under it, to areas. A viewport is
// its scroll pane's own view of what it holds, not an area in it.
func areasIn(e *atspi.Element, areas *[]*atspi.Element) {
	if r := e.Role(); r != "viewport" && slices.Contains(areaRoles, r) {
		*areas = append(*areas, e)
		return
	}
	kids, _ := e.Children()
	for _, k := range kids {
		areasIn(k, areas)
	}
}

// spot is a point in v that none of the areas covers: its middle, or near
// one of its edges.
func spot(v atspi.Rect, areas []*atspi.Element) (int, int, bool) {
	mx, my := int(v.X+v.Width/2), int(v.Y+v.Height/2)
	for _, pt := range [][2]int{
		{mx, my},
		{int(v.X) + 6, my},
		{int(v.X+v.Width) - 6, my},
		{mx, int(v.Y) + 6},
		{mx, int(v.Y+v.Height) - 6},
		{int(v.X) + 6, int(v.Y) + 6},
		{int(v.X) + 6, int(v.Y+v.Height) - 6},
	} {
		free := true
		for _, a := range areas {
			r := a.Extents()
			if placed(r) && pt[0] >= int(r.X) && pt[0] < int(r.X+r.Width) && pt[1] >= int(r.Y) && pt[1] < int(r.Y+r.Height) {
				free = false
				break
			}
		}
		if free {
			return pt[0], pt[1], true
		}
	}
	return 0, 0, false
}

// inView is whether a click in the element's middle reaches it: it shows,
// and nothing covers it (GTK 3 gives a cell it has not laid out the least
// integer, and a web page's rows below its scroll box their own place).
func (p *proc) inView(e *atspi.Element) bool {
	x, y, ok := center(e)
	if !ok || x < 0 || y < 0 {
		return false
	}
	// An open menu's items show where they are: the menu is a popup, over
	// the window, which a hit test from the window does not reach.
	for at := e.Parent(); at != nil; at = at.Parent() {
		if r := at.Role(); r == "menu" || r == "popup menu" {
			return true
		}
		if r := at.Role(); r == "frame" || r == "window" || r == "application" {
			break
		}
	}
	// Hit-tested from its web page, when it is in one: Chromium's window
	// answers with its own views, not the page in them.
	win := p.window()
	from := win
	for at := e.Parent(); at != nil && !at.Same(from); at = at.Parent() {
		if r := at.Role(); r == "document web" || r == "document frame" {
			from = at
			break
		}
	}
	// Its middle shows: in every area it is in, and in its window. A web
	// engine's hit test knows its scroll boxes (which have no role of their
	// own), and WebKit moves its scroll panes' places with what they scroll:
	// in a page, the window is all that is checked here.
	v := visiblePart(e, win)
	if !from.Same(win) {
		v = intersect(e.Extents(), win.Extents())
	}
	if x < int(v.X) || x >= int(v.X+v.Width) || y < int(v.Y) || y >= int(v.Y+v.Height) {
		return false
	}
	hit := deepestAt(from, x, y)
	if hit != nil && !hit.Is(atspi.StateShowing) || p.gtk4 {
		// GTK 4 hit-tests a notebook's hidden pages, which it says show: a
		// hit says nothing there, and what clips the element (every area
		// it is in has its role) is all there is.
		return true
	}
	for at := hit; at != nil; at = at.Parent() {
		if at.Same(e) {
			return true
		}
		if at.Same(from) {
			break
		}
	}
	// A hit that stops above it (Java's goes no deeper than its root pane)
	// says nothing covers it.
	return hit != nil && isAbove(hit, e)
}

// isAbove is whether a is e or one of e's ancestors.
func isAbove(a, e *atspi.Element) bool {
	for at := e; at != nil; at = at.Parent() {
		if at.Same(a) {
			return true
		}
	}
	return false
}

// scrollTo finds the control and brings it into view, as a person scrolls
// to it: some toolkits have only what shows in their tree (GTK 3 makes a
// table's cells anew as it scrolls), others what does not show too.
func (p *proc) scrollTo(kind, n string) *atspi.Element {
	first := func(in *atspi.Element) *atspi.Element {
		var found []*atspi.Element
		p.search(in, nil, kind, n, false, &found)
		if len(found) > 0 {
			return found[0]
		}
		return nil
	}
	win := p.window()
	if e := first(win); e != nil {
		if !e.HasPlaces() || p.inView(e) {
			// An app with no places (Flutter's) is acted on through
			// accessibility, in view or not.
			return e
		}
		// After a scroll, found anew: GTK 4 gives a row's element to the
		// rows that scroll into its place.
		again := func() *atspi.Element {
			time.Sleep(400 * time.Millisecond)
			if f := first(win); f != nil && p.inView(f) {
				return f
			}
			return nil
		}
		if e.ScrollTo() {
			if f := again(); f != nil {
				return f
			}
		}
		if slices.Contains(e.Actions(), "listitem.scroll-to") && e.Do("listitem.scroll-to") == nil {
			if f := again(); f != nil {
				return f
			}
		}
	}
	// Tables and lists first, then the areas that hold them.
	var lists, panes []*atspi.Element
	visit(win, func(e *atspi.Element) bool {
		switch e.Role() {
		case "table", "tree table", "list", "list box":
			lists = append(lists, e)
		case "scroll pane":
			panes = append(panes, e)
		}
		return true
	})
	for _, box := range append(lists, panes...) {
		p.reveal(box)
		var inside []*atspi.Element
		kids, _ := box.Children()
		for _, k := range kids {
			areasIn(k, &inside)
		}
		for _, lines := range []int{-3, 3} { // down to the end, then up to the start
			still := 0
			for range 40 {
				r := visiblePart(box, win)
				x, y, ok := spot(r, inside)
				if r.Width <= 0 || r.Height <= 0 || !ok || still >= 3 {
					break
				}
				if e := first(box); e != nil && p.inView(e) {
					// GTK 4 scrolls on a moment after the wheel stops: once it
					// rests, found anew, it is there or not.
					time.Sleep(400 * time.Millisecond)
					if f := first(box); f != nil && p.inView(f) {
						return f
					}
				}
				before := fingerprint(box)
				_ = p.in.Scroll(x, y, lines)
				time.Sleep(150 * time.Millisecond)
				if fingerprint(box) == before {
					still++ // at its end, or it does not scroll
				} else {
					still = 0
				}
			}
		}
	}
	return nil
}

// placed is whether the element has a place on the screen: GTK 3 gives
// what it has not drawn the least integer.
func placed(r atspi.Rect) bool { return r.X > -1e8 && r.Y > -1e8 && r.Width > 0 && r.Height > 0 }

// fingerprint is what shows first in the box, and where: it changes as the
// box scrolls.
func fingerprint(box *atspi.Element) string {
	v := box.Extents()
	var b strings.Builder
	n := 0
	visit(box, func(e *atspi.Element) bool {
		if e.Same(box) {
			return true
		}
		r := e.Extents()
		if mx, my := r.X+r.Width/2, r.Y+r.Height/2; placed(r) && (!placed(v) || mx >= v.X && mx < v.X+v.Width && my >= v.Y && my < v.Y+v.Height) {
			fmt.Fprintf(&b, "%s %v;", e.Name(), r)
			n++
		}
		return n < 8
	})
	return b.String()
}

// visiblePart is the part of the element that shows: in its viewports and
// scroll panes, and in its window, when the window says where it is (Java's
// does not).
func visiblePart(e, win *atspi.Element) atspi.Rect {
	r := e.Extents()
	for at := e.Parent(); at != nil && !at.Same(win); at = at.Parent() {
		if slices.Contains(areaRoles, at.Role()) {
			if b := at.Extents(); placed(b) {
				if !placed(r) {
					r = b // with no place of its own, it is as far as its area shows
				} else {
					r = intersect(r, b)
				}
			}
		}
	}
	if w := win.Extents(); w.Width > 0 && w.Height > 0 {
		r = intersect(r, w)
	}
	return r
}

// intersect is the part of a that is in b.
func intersect(a, b atspi.Rect) atspi.Rect {
	x1, y1 := max(a.X, b.X), max(a.Y, b.Y)
	x2, y2 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	return atspi.Rect{X: x1, Y: y1, Width: x2 - x1, Height: y2 - y1}
}

// deepestAt is the deepest element at the point: an element answers for
// itself and its children, and Chromium's window answers with the view that
// holds its page, which then answers for the page.
func deepestAt(e *atspi.Element, x, y int) *atspi.Element {
	at := e.ElementAt(x, y)
	for range 64 {
		if at == nil {
			return nil
		}
		next := at.ElementAt(x, y)
		if next == nil || next.Same(at) {
			return at
		}
		at = next
	}
	return at
}

// clickActions are the names toolkits give the action a click takes.
var clickActions = []string{"click", "Tap", "press", "activate", "toggle", "jump", "doDefault"}

// activate takes the element's action that a click would: its name differs
// by toolkit. An app that was just asked for a place it has not (Flutter's)
// answers nothing for a few seconds.
func activate(e *atspi.Element) error {
	var acts []string
	for wait := time.Now(); time.Since(wait) < 6*time.Second; time.Sleep(200 * time.Millisecond) {
		acts = e.Actions()
		for _, a := range clickActions {
			if slices.Contains(acts, a) {
				if err := e.Do(a); err != nil {
					return err
				}
				time.Sleep(300 * time.Millisecond)
				return nil
			}
		}
	}
	return fmt.Errorf("the %s %q has no place on the screen and no action like a click (it offers %v)", e.Role(), name(e), acts)
}
