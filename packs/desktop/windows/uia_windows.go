//go:build windows

package desktopwindows

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/uia"
)

// UI Automation's control types.
const (
	typeButton      = 50000
	typeCheckBox    = 50002
	typeEdit        = 50004
	typeHyperlink   = 50005
	typeImage       = 50006
	typeListItem    = 50007
	typeMenuBar     = 50010
	typeMenuItem    = 50011
	typeRadioButton = 50013
	typeScrollBar   = 50014
	typeTabItem     = 50019
	typeText        = 50020
	typeTreeItem    = 50024
	typeDataItem    = 50029
	typeWindow      = 50032
	typeHeader      = 50034
	typeTitleBar    = 50037
)

var space = regexp.MustCompile(`\s+`)

// collapse is a text with its whitespace collapsed, as steps compare texts.
func collapse(s string) string { return strings.TrimSpace(space.ReplaceAllString(s, " ")) }

// uiaTree is an app's windows as UI Automation has them. Its methods run on
// the worker's thread.
type uiaTree struct {
	p *proc
	c *uia.Client
}

// ctl is a control UI Automation found. Its methods ask on the worker's
// thread.
type ctl struct {
	e *uia.Element
	w *worker
	// enabledUnknown is whether the app says nothing true of the control's
	// enabled state (Flutter says every control is enabled).
	enabledUnknown bool
}

func (c ctl) Name() string {
	var n string
	_ = c.w.do(func() error { n = name(c.e); return nil })
	return n
}

func (c ctl) Enabled() (bool, bool) {
	var on bool
	_ = c.w.do(func() error { on = c.e.Enabled(); return nil })
	return on, !c.enabledUnknown
}

func (c ctl) Value() (string, bool) {
	var v string
	var edit bool
	_ = c.w.do(func() error {
		v, edit = c.e.Value(), c.e.ControlType() == typeEdit
		return nil
	})
	return v, edit
}

func (t *uiaTree) control(e *uia.Element) ctl {
	return ctl{e: e, w: t.p.w, enabledUnknown: t.p.flutter}
}

// names are what an element is called: its name and help, and each of
// their lines.
func names(e *uia.Element) []string {
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
	add(e.HelpText())
	return out
}

func name(e *uia.Element) string {
	if n := names(e); len(n) > 0 {
		return n[0]
	}
	return ""
}

func named(e *uia.Element, n string) bool { return slices.Contains(names(e), n) }

// shown is whether the element shows: on screen, with a size.
func shown(e *uia.Element) bool {
	b := e.Bounds()
	return !e.Offscreen() && b.Right > b.Left && b.Bottom > b.Top
}

func visit(e *uia.Element, fn func(*uia.Element) bool) bool {
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

// holds is whether the element or one under it has the name, shown or not.
func holds(e *uia.Element, n string) bool {
	found := false
	visit(e, func(x *uia.Element) bool {
		found = found || named(x, n)
		return !found
	})
	return found
}

func hasText(e *uia.Element, text string) bool {
	found := false
	visit(e, func(x *uia.Element) bool {
		found = found || (named(x, text) || x.ControlType() == typeEdit && collapse(x.Value()) == text) && shown(x)
		return !found
	})
	return found
}

// isSwitch is whether the element is a switch: a toggle people read as one.
func isSwitch(e *uia.Element) bool {
	_, toggles := e.Toggled()
	return toggles && (strings.Contains(strings.ToLower(e.LocalizedType()), "switch") || e.ClassName() == "ToggleSwitch")
}

// hasHeader is whether the element has column headers right under it.
func hasHeader(e *uia.Element) bool {
	if e == nil {
		return false
	}
	kids, _ := e.Children()
	for _, k := range kids {
		if k.ControlType() == typeHeader {
			return true
		}
	}
	return false
}

// is is whether e, under parent, is a control of the kind, whatever its
// name. Flutter's menus are its own drawing: their items have no role of
// their own, so any element is one.
func (t *uiaTree) is(kind string, e, parent *uia.Element) bool {
	ty := e.ControlType()
	switch kind {
	case "button":
		return ty == typeButton && !isSwitch(e) && (parent == nil || parent.ControlType() != typeTitleBar)
	case "field":
		return ty == typeEdit
	case "checkbox":
		return ty == typeCheckBox && !isSwitch(e)
	case "switch":
		return isSwitch(e)
	case "radio button":
		return ty == typeRadioButton
	case "tab":
		return ty == typeTabItem
	case "menu":
		// A menu bar's item, or an item that opens a menu (WPF's menu bar is
		// a menu), or a button that does (Chromium draws Electron's menu bar,
		// its menus buttons).
		return ty == typeMenuItem && (parent != nil && parent.ControlType() == typeMenuBar || e.Expands()) ||
			ty == typeButton && (e.ClassName() == "SubmenuButton" || e.Expands()) || t.p.flutter
	case "menu item":
		return ty == typeMenuItem && (parent == nil || parent.ControlType() != typeMenuBar) || t.p.flutter
	case "list item":
		return ty == typeListItem
	case "row":
		// A data item; or an item of a list with column headers, which is a
		// table (Windows Forms' list view in its details).
		return ty == typeDataItem || ty == typeTreeItem || ty == typeListItem && hasHeader(parent)
	case "link":
		return ty == typeHyperlink
	case "image":
		return ty == typeImage
	case "text":
		return ty == typeText
	case "element":
		return true
	}
	return false
}

// byText are the kinds named by a text they hold too.
var byText = map[string]bool{"list item": true, "row": true, "link": true}

func (t *uiaTree) matches(kind, n string, e, parent *uia.Element) bool {
	if !t.is(kind, e, parent) {
		return false
	}
	if byText[kind] {
		return holds(e, n)
	}
	return named(e, n)
}

// search adds the controls of the kind named n under e to found: a control
// in another that matches counts once, as the outer one.
func (t *uiaTree) search(e, parent *uia.Element, kind, n string, onlyShown bool, found *[]*uia.Element) {
	if e == nil {
		return
	}
	// Flutter's places are untrue once it has scrolled: what it has in its
	// tree, it has built to show.
	if t.matches(kind, n, e, parent) && (!onlyShown || t.p.flutter || shown(e)) {
		*found = append(*found, e)
		return
	}
	kids, _ := e.Children()
	for _, k := range kids {
		t.search(k, e, kind, n, onlyShown, found)
	}
}

// places are where controls of the kind are looked for: the app's windows,
// the front one first, its open menus among them; for a menu item, the
// system's menus too (Win32's), which are windows of no process.
func (t *uiaTree) places(kind string) []*uia.Element {
	places := t.p.windows()
	if kind == "menu item" || kind == "element" {
		if root, err := t.c.Root(); err == nil {
			kids, _ := root.Children()
			for _, k := range kids {
				if k.ClassName() == "#32768" {
					places = append(places, k)
				}
			}
		}
	}
	return places
}

func (t *uiaTree) find(kind, n string, onlyShown bool) []*uia.Element {
	var found []*uia.Element
	for _, at := range t.places(kind) {
		t.search(at, nil, kind, n, onlyShown, &found)
		if len(found) > 0 {
			return distinct(found)
		}
	}
	return nil
}

// distinct leaves out an element found again: WPF has an open menu's items
// under the menu and in its popup.
func distinct(found []*uia.Element) []*uia.Element {
	var out []*uia.Element
	for _, e := range found {
		if !slices.ContainsFunc(out, e.Same) {
			out = append(out, e)
		}
	}
	return out
}

func (t *uiaTree) names(kind string) []string {
	var out []string
	var each func(e, parent *uia.Element)
	each = func(e, parent *uia.Element) {
		if t.is(kind, e, parent) && shown(e) {
			if n := t.label(kind, e); n != "" && !slices.Contains(out, n) {
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
	for _, at := range t.places(kind) {
		each(at, nil)
	}
	return out
}

// label names an element of the kind in a failure: its name, or for a
// control named by its texts, the first of them.
func (t *uiaTree) label(kind string, e *uia.Element) string {
	if n := name(e); n != "" || !byText[kind] {
		return n
	}
	var first string
	visit(e, func(x *uia.Element) bool {
		if !x.Same(e) && shown(x) {
			first = name(x)
		}
		return first == ""
	})
	return first
}

func (t *uiaTree) shows(text string) bool {
	for _, w := range t.p.windows() {
		if hasText(w, text) {
			return true
		}
	}
	return false
}

func (t *uiaTree) texts() []string {
	var out []string
	for _, w := range t.p.windows() {
		visit(w, func(e *uia.Element) bool {
			if shown(e) {
				if n := name(e); n != "" && !slices.Contains(out, n) {
					out = append(out, n)
				}
			}
			return len(out) < 200
		})
	}
	return out
}

func center(e *uia.Element) (int, int, bool) {
	b := e.Bounds()
	if b.Right <= b.Left || b.Bottom <= b.Top {
		return 0, 0, false
	}
	return int(b.Left+b.Right) / 2, int(b.Top+b.Bottom) / 2, true
}

// settled waits for the element to stop moving, 2 seconds at most.
func settled(e *uia.Element) {
	last := e.Bounds()
	for wait := time.Now(); time.Since(wait) < 2*time.Second; {
		time.Sleep(100 * time.Millisecond)
		now := e.Bounds()
		if now == last {
			return
		}
		last = now
	}
}

// usable is the part of the element a click can reach: in its window, and
// in the screen's work area (a window can reach past the screen, or under
// the taskbar).
func usable(e *uia.Element) uia.Rect {
	r := uia.Intersect(e.Bounds(), uia.WorkArea())
	for _, a := range areas(e) {
		// Its areas show what they hold only in themselves (not every
		// ancestor: WPF holds a tab's content in its tab, the size of its
		// header).
		if b := a.Bounds(); b.Right > b.Left && b.Bottom > b.Top {
			r = uia.Intersect(r, b)
		}
	}
	return r
}

func inside(x, y int, r uia.Rect) bool {
	return x >= int(r.Left) && x < int(r.Right) && y >= int(r.Top) && y < int(r.Bottom)
}

// inView is whether a click in the element's middle reaches it: the middle
// is in every area that holds it, the window and the work area, and the hit
// test there finds it. WinUI's hit test stops at its content's bridge, above
// the element: that says nothing covers it. Elsewhere a hit above the
// element is what covers it (Qt's page, scrolled over it).
func (t *uiaTree) inView(e *uia.Element) bool {
	x, y, ok := center(e)
	if !ok || e.Offscreen() || !inside(x, y, usable(e)) {
		return false
	}
	hit := t.c.ElementAt(x, y)
	if hit == nil {
		return false
	}
	for at := hit; at != nil; at = at.Parent() {
		if at.Same(e) {
			return true
		}
	}
	if !strings.Contains(hit.ClassName(), "DesktopChildSiteBridge") {
		return false
	}
	for at := e.Parent(); at != nil; at = at.Parent() {
		if at.Same(hit) {
			return true
		}
	}
	return false
}

// areas are what the element is in that scroll what they hold, the
// innermost first, and last the window.
func areas(e *uia.Element) []*uia.Element {
	var out []*uia.Element
	for at := e.Parent(); at != nil; at = at.Parent() {
		if at.ControlType() == typeWindow {
			return append(out, at)
		}
		if isArea(at) {
			out = append(out, at)
		}
	}
	return out
}

// isArea is whether the element shows part of what it holds and scrolls the
// rest into view: a list, a table, what scrolls when asked, or what holds a
// scroll bar (Windows Forms' scrolling panel, which is not asked). A text
// field scrolls its text, and is not one.
func isArea(e *uia.Element) bool {
	switch uia.ControlTypeName(e.ControlType()) {
	case "List", "DataGrid", "Table", "Tree":
		return true
	case "Edit", "Document", "Window":
		return false
	}
	if e.Scrolls() {
		return true
	}
	kids, _ := e.Children()
	for _, k := range kids {
		if k.ControlType() == typeScrollBar {
			return true
		}
	}
	return false
}

// reveal scrolls the element into view as far as it goes: the app scrolls
// it there when asked (its ScrollItem pattern), or else each area it is in,
// the outermost first, until the next one in (or the element) shows in it.
// Flutter's places never follow a scroll (flutter/flutter#189124), so its
// controls are never scrolled: one out of view takes its own action.
func (t *uiaTree) reveal(e *uia.Element) {
	if t.p.flutter || t.inView(e) {
		return
	}
	if e.ScrollIntoView() == nil {
		time.Sleep(300 * time.Millisecond)
		if t.inView(e) {
			return
		}
	}
	as := areas(e)
	for i := len(as) - 1; i >= 0; i-- {
		inner := e
		if i > 0 {
			inner = as[i-1]
		}
		t.bringInto(as[i], inner)
	}
}

// bringInto scrolls the area until what it holds (inner) shows in it, or
// fills it when it is the taller: a step at a time, when the area scrolls
// when asked (WinUI takes no wheel that SendInput turns), or else the wheel
// over a part of the area that no area in it covers, as that one would take
// the turns.
func (t *uiaTree) bringInto(a, inner *uia.Element) {
	var in []*uia.Element
	kids, _ := a.Children()
	for _, k := range kids {
		areasIn(k, &in)
	}
	still := 0
	for range 150 {
		v, f := usable(a), inner.Bounds()
		if v.Right <= v.Left || v.Bottom <= v.Top || f.Bottom <= f.Top || still >= 3 {
			return
		}
		if f.Top >= v.Top-1 && f.Bottom <= v.Bottom+1 || f.Top <= v.Top+1 && f.Bottom >= v.Bottom-1 {
			return
		}
		up := f.Top < v.Top
		// A page at a time while it is more than a page away, then a step
		// (Windows Forms' step is a few pixels).
		far := f.Bottom < v.Top-(v.Bottom-v.Top) || f.Top > v.Bottom+(v.Bottom-v.Top)
		if a.Scroll(up, far) != nil {
			x, y, ok := spot(v, in)
			if !ok {
				return
			}
			lines := -1
			if up {
				lines = 1
			}
			_ = uia.Scroll(x, y, lines)
		}
		time.Sleep(60 * time.Millisecond)
		if inner.Bounds() == f { // it did not move: at the area's end, or the area does not scroll
			still++
		} else {
			still = 0
		}
	}
}

// areasIn adds e, or the outermost areas under it, to as.
func areasIn(e *uia.Element, as *[]*uia.Element) {
	if isArea(e) {
		*as = append(*as, e)
		return
	}
	kids, _ := e.Children()
	for _, k := range kids {
		areasIn(k, as)
	}
}

// spot is a point in v that none of the areas covers: its middle, or near
// one of its edges.
func spot(v uia.Rect, as []*uia.Element) (int, int, bool) {
	mx, my := int(v.Left+v.Right)/2, int(v.Top+v.Bottom)/2
	for _, p := range [][2]int{
		{mx, my},
		{int(v.Left) + 8, my},
		{int(v.Right) - 8, my},
		{mx, int(v.Top) + 8},
		{mx, int(v.Bottom) - 8},
		{int(v.Left) + 8, int(v.Top) + 8},
		{int(v.Left) + 8, int(v.Bottom) - 8},
	} {
		free := true
		for _, a := range as {
			if inside(p[0], p[1], a.Bounds()) {
				free = false
				break
			}
		}
		if free {
			return p[0], p[1], true
		}
	}
	return 0, 0, false
}

// fingerprint changes as the area scrolls: how far it has scrolled, when it
// scrolls when asked and says so (WPF's table does not; Qt's says 0 and does
// not scroll when asked), else what shows first in it, and where.
func fingerprint(a *uia.Element) string {
	if p, ok := a.ScrollPercent(); ok && a.Scrolls() {
		return fmt.Sprintf("%.3f%%", p)
	}
	var b strings.Builder
	n := 0
	visit(a, func(e *uia.Element) bool {
		if e.Same(a) {
			return true
		}
		if r := e.Bounds(); r.Right > r.Left && !e.Offscreen() {
			fmt.Fprintf(&b, "%s %+v;", e.Name(), r)
			n++
		}
		return n < 16 // past a table's headers, to its rows
	})
	return b.String()
}

// scrollTo finds the control and brings it into view. Lists make their
// items as they scroll (WPF, WinUI), so when it is not there, each list and
// table is brought into view and scrolled, a page at a time, until it is.
func (t *uiaTree) scrollTo(kind, n string) *uia.Element {
	first := func(in *uia.Element) *uia.Element {
		var found []*uia.Element
		t.search(in, nil, kind, n, false, &found)
		if len(found) > 0 {
			return found[0]
		}
		return nil
	}
	win := t.p.window()
	if win == nil {
		return nil
	}
	if e := first(win); e != nil {
		if t.inView(e) || t.p.flutter {
			return e // Flutter's out of view takes its own action
		}
		t.reveal(e)
		if f := first(t.p.window()); f != nil && t.inView(f) {
			return f
		}
	}
	var lists, panes []*uia.Element
	visit(win, func(e *uia.Element) bool {
		switch uia.ControlTypeName(e.ControlType()) {
		case "List", "DataGrid", "Table", "Tree":
			lists = append(lists, e)
		default:
			if !e.Same(win) && isArea(e) {
				panes = append(panes, e)
			}
		}
		return true
	})
	for _, box := range append(lists, panes...) {
		t.reveal(box)
		var in []*uia.Element
		kids, _ := box.Children()
		for _, k := range kids {
			areasIn(k, &in)
		}
		for _, up := range []bool{false, true} { // down to the end, then up to the start
			still := 0
			for range 80 {
				if f := first(box); f != nil && t.inView(f) {
					time.Sleep(300 * time.Millisecond) // a list scrolls on a moment after the wheel stops
					if f := first(box); f != nil && t.inView(f) {
						return f
					}
				}
				before := fingerprint(box)
				if box.Scroll(up, true) != nil {
					x, y, ok := spot(usable(box), in)
					if !ok {
						break
					}
					lines := -3
					if up {
						lines = 3
					}
					_ = uia.Scroll(x, y, lines)
				}
				time.Sleep(150 * time.Millisecond)
				if fingerprint(box) == before {
					if still++; still >= 3 {
						break // at its end, or it does not scroll
					}
				} else {
					still = 0
				}
			}
		}
	}
	return nil
}

// click brings the element into view and clicks its middle. Out of the
// pointer's reach, it takes its own action, or its row's; or else its
// default action, as Windows' older accessibility (MSAA) gives it
// (Flutter's tap).
func (t *uiaTree) click(e *uia.Element) error {
	t.reveal(e)
	settled(e)
	if !t.inView(e) {
		for _, act := range []func(*uia.Element) error{(*uia.Element).Invoke, (*uia.Element).DefaultAction} {
			for at := e; at != nil && at.ControlType() != typeWindow; at = at.Parent() {
				if act(at) == nil {
					t.p.sc.Log("the %s %q is out of the pointer's reach: it took its own action", uia.ControlTypeName(at.ControlType()), name(at))
					time.Sleep(300 * time.Millisecond)
					return nil
				}
			}
		}
	}
	x, y, ok := center(e)
	if !ok {
		t.p.sc.Log("the %s has no place on the screen: it took its own action", uia.ControlTypeName(e.ControlType()))
		return e.Invoke()
	}
	if err := uia.Click(x, y); err != nil {
		return err
	}
	time.Sleep(250 * time.Millisecond)
	return nil
}

// origin is the element's top left, in pixels, once it is in view and
// still.
func (t *uiaTree) origin(e *uia.Element) (int, int, error) {
	t.reveal(e)
	settled(e)
	b := e.Bounds()
	if b.Right <= b.Left {
		return 0, 0, fmt.Errorf("the %s has no place on the screen", uia.ControlTypeName(e.ControlType()))
	}
	return int(b.Left), int(b.Top), nil
}

// node is the element and those under it as nodes.
func (t *uiaTree) node(e *uia.Element, depth int, count *int) *desktopcore.Node {
	*count++
	c := t.control(e)
	n := &desktopcore.Node{Role: uia.ControlTypeName(e.ControlType()), Name: name(e), ID: e.AutomationID(), Attrs: map[string]string{}, Control: c}
	if e.ControlType() == typeEdit {
		n.Value = e.Value()
	}
	if cls := e.ClassName(); cls != "" {
		n.Attrs["class"] = cls
	}
	if !t.p.flutter {
		n.Attrs["enabled"] = fmt.Sprint(e.Enabled())
	}
	if depth > 60 || *count > 5000 {
		return n
	}
	kids, _ := e.Children()
	for _, k := range kids {
		n.Children = append(n.Children, t.node(k, depth+1, count))
	}
	return n
}
