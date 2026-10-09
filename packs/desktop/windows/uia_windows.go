//go:build windows

package desktopwindows

import (
	"fmt"
	"image"
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
	// pointerWas is where the pointer was before it showed a taskbar that
	// hides itself, where it goes back to once an icon's menu is chosen from.
	pointerWas *image.Point
	// frontWas is the window in front before the taskbar's tray was used,
	// which a person clicks back into once an icon's menu is chosen from: a
	// taskbar that hides itself stays on the screen while it is in front.
	frontWas uintptr
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
// uiaText is UIA_TextControlTypeId: a text, which may only show a control's
// name.
const uiaText = 50020

func (t *uiaTree) search(e, parent *uia.Element, kind, n string, onlyShown bool, found *[]*uia.Element) {
	if e == nil {
		return
	}
	// Flutter's places are untrue once it has scrolled: what it has in its
	// tree, it has built to show.
	if t.matches(kind, n, e, parent) && (!onlyShown || t.p.flutter || shown(e)) {
		// A text that holds a control of its name (WPF's link in its text
		// block): the element a step names is the control.
		if kind == "element" && e.ControlType() == uiaText {
			var inner []*uia.Element
			kids, _ := e.Children()
			for _, k := range kids {
				t.search(k, e, kind, n, onlyShown, &inner)
			}
			if inner = slices.DeleteFunc(inner, func(x *uia.Element) bool { return x.ControlType() == uiaText }); len(inner) > 0 {
				*found = append(*found, inner...)
				return
			}
		}
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
	if kind == "menu" || kind == "element" {
		return t.trayIcon(n, kind == "menu")
	}
	return nil
}

// The taskbar's tray (its notification area): the window of the taskbar,
// those of the icons it hides while they show, the hidden icons' button,
// and the apps' icons, named by their tooltips.
const (
	taskbarClass  = "Shell_TrayWnd"
	trayIconID    = "NotifyItemIcon"
	hiddenIconsID = "SystemTrayIcon"
	hiddenClass   = "SystemTray.NormalButton"
)

var hiddenIconsWindows = []string{"TopLevelWindowForOverflowXamlIsland", "NotifyIconOverflowWindow"}

// trayIcon is an app's icon of the name in the taskbar's tray. Looking for
// one, as a person does, opens the icons the tray hides when it is none of
// those it shows.
func (t *uiaTree) trayIcon(n string, open bool) []*uia.Element {
	if t.frontWas == 0 {
		t.frontWas = uia.ForegroundWindow()
	}
	t.showTaskbar()
	icons, hidden := t.tray()
	for _, e := range icons {
		if named(e, n) {
			return []*uia.Element{e}
		}
	}
	if !open || hidden == nil {
		return nil
	}
	if !t.toggle(hidden) {
		return nil
	}
	for wait := time.Now(); time.Since(wait) < 2*time.Second; time.Sleep(100 * time.Millisecond) {
		icons, _ = t.tray()
		for _, e := range icons {
			if named(e, n) {
				return []*uia.Element{e}
			}
		}
	}
	return nil
}

// showTaskbar brings the taskbar to the front, as a person's click on it
// does, and one that hides itself onto the screen, as a person does: the
// pointer at the screen's bottom edge, where it waits. Windows keeps the
// pointer's moves and clicks axx makes from the taskbar while a window in
// front runs elevated.
func (t *uiaTree) showTaskbar() {
	root, err := t.c.Root()
	if err != nil {
		return
	}
	kids, _ := root.Children()
	for _, k := range kids {
		if k.ClassName() != taskbarClass {
			continue
		}
		uia.Foreground(k.Handle())
		screen := uia.Screen()
		hidden := func() bool { return k.Bounds().Top >= screen.Bottom-8 }
		if !hidden() {
			return
		}
		if t.pointerWas == nil {
			x, y := uia.Pointer()
			t.pointerWas = &image.Point{X: x, Y: y}
		}
		_ = uia.Move(int(screen.Right/2), int(screen.Bottom-1))
		for wait := time.Now(); hidden() && time.Since(wait) < 2*time.Second; time.Sleep(50 * time.Millisecond) {
		}
		return
	}
}

// toggle presses the button that shows the tray's hidden icons, or hides
// them.
func (t *uiaTree) toggle(button *uia.Element) bool {
	if err := button.Invoke(); err != nil {
		x, y, ok := center(button)
		if !ok || uia.Click(x, y) != nil {
			return false
		}
	}
	return true
}

// leaveTray hides the tray's hidden icons, takes the pointer back from a
// taskbar that hides itself, and clicks back into the window that was in
// front, as a person does: the taskbar stays on the screen while any of
// them is left.
func (t *uiaTree) leaveTray() {
	// Shown by this search or an earlier one, or shown again as the menu
	// closes: hidden again, and seen to stay hidden. A press takes a moment
	// to hide them; pressed again before, it would show them again.
	var pressed time.Time
	for wait, hidden := time.Now(), 0; time.Since(wait) < 2*time.Second && hidden < 3; time.Sleep(100 * time.Millisecond) {
		_, button, shown := t.trayState()
		if button == nil {
			break
		}
		if !shown {
			hidden++
			continue
		}
		hidden = 0
		if time.Since(pressed) > 700*time.Millisecond {
			t.toggle(button)
			pressed = time.Now()
		}
	}
	if t.pointerWas != nil {
		_ = uia.Move(t.pointerWas.X, t.pointerWas.Y)
		t.pointerWas = nil
	}
	// The app's window may be gone with the menu's choice (an app quit from
	// its icon): then the desktop, which a person would click.
	switch win := t.p.window(); {
	case t.frontWas != 0 && uia.IsWindow(t.frontWas):
		uia.Foreground(t.frontWas)
	case win != nil:
		uia.Foreground(win.Handle())
	default:
		uia.Foreground(uia.Desktop())
	}
	t.frontWas = 0
}

// tray is the apps' icons the taskbar's tray shows, those it hides while
// they show too, and the button that shows the hidden ones while they do
// not.
func (t *uiaTree) tray() (icons []*uia.Element, hidden *uia.Element) {
	icons, button, shown := t.trayState()
	if shown {
		return icons, nil
	}
	return icons, button
}

// trayState is the apps' icons in the taskbar's tray, the button that shows
// or hides those it hides, and whether they show.
func (t *uiaTree) trayState() (icons []*uia.Element, hidden *uia.Element, shown bool) {
	root, err := t.c.Root()
	if err != nil {
		return nil, nil, false
	}
	kids, _ := root.Children()
	open := false
	var each func(e *uia.Element, depth int)
	each = func(e *uia.Element, depth int) {
		switch {
		case e.AutomationID() == trayIconID:
			icons = append(icons, e)
			return
		case e.AutomationID() == hiddenIconsID && e.ClassName() == hiddenClass:
			hidden = e
			return
		case depth > 8:
			return
		}
		grand, _ := e.Children()
		for _, g := range grand {
			each(g, depth+1)
		}
	}
	for _, k := range kids {
		switch cls := k.ClassName(); {
		case cls == taskbarClass:
			each(k, 0)
		case slices.Contains(hiddenIconsWindows, cls):
			open = true
			each(k, 0)
		}
	}
	return icons, hidden, open
}

// outside is whether an element is not in a window of the app's to bring
// to the front: an icon of the taskbar's tray, which the taskbar (Explorer)
// has, or an item of a menu open over the screen (Win32's, a tray icon's),
// which closes when its app's window comes to the front.
func (t *uiaTree) outside(e *uia.Element) bool {
	if !slices.Contains(t.p.pids(), e.ProcessID()) {
		return true
	}
	for at := e; at != nil; at = at.Parent() {
		if at.ClassName() == "#32768" {
			return true
		}
	}
	return false
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

// settled waits for the element to stop moving, 2 seconds at most, and says
// whether it moved.
func settled(e *uia.Element) (moved bool) {
	last := e.Bounds()
	for wait := time.Now(); time.Since(wait) < 2*time.Second; {
		time.Sleep(50 * time.Millisecond)
		now := e.Bounds()
		if now == last {
			return moved
		}
		last, moved = now, true
	}
	return moved
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
// controls are never scrolled: one out of view takes its own action. It
// says whether the element was in view as it was.
func (t *uiaTree) reveal(e *uia.Element) (inView bool) {
	if t.p.flutter {
		return false
	}
	if t.inView(e) {
		return true
	}
	if e.ScrollIntoView() == nil {
		for wait := time.Now(); time.Since(wait) < 300*time.Millisecond; {
			time.Sleep(30 * time.Millisecond)
			if t.inView(e) {
				return true
			}
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
	return false
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
	inView := t.reveal(e)
	if settled(e) || !inView {
		inView = t.inView(e)
	}
	if !inView {
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
	time.Sleep(afterInput)
	return nil
}

// origin is the element's top left, in pixels, once it is in view and
// still.
func (t *uiaTree) rect(e *uia.Element) (x, y, w, h int, err error) {
	t.reveal(e)
	settled(e)
	b := e.Bounds()
	if b.Right <= b.Left {
		return 0, 0, 0, 0, &desktopcore.Lost{Err: fmt.Errorf("the %s has no place on the screen", uia.ControlTypeName(e.ControlType()))}
	}
	return int(b.Left), int(b.Top), int(b.Right - b.Left), int(b.Bottom - b.Top), nil
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
