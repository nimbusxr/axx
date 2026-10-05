//go:build windows && integration

package uia

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDepotDesk works a build of the depot desk (examples/parcels/depot-desk)
// as a clerk does, through UI Automation, with the pointer and the keyboard
// (SendInput): every control its spec names, a restart that keeps the day's
// arrivals, and a new start that is reset (a new home, and the registry key
// it keeps its settings in emptied). Its parcels are not among those
// expected, so an arrival is never mistaken for a row of the expected ones.
//
// AXX_DESK names the app and AXX_DESK_ARGS its arguments; AXX_DESK_REGISTRY
// the key under HKEY_CURRENT_USER the app keeps its settings in. The kinds
// of a few controls differ by toolkit: AXX_DESK_PRINT_LABEL ("switch", or
// "checkbox"), AXX_DESK_LINK ("link"), AXX_DESK_ARRIVAL ("list item"),
// AXX_DESK_TAB ("tab") and AXX_DESK_ROW ("row").
func TestDepotDesk(t *testing.T) {
	app := os.Getenv("AXX_DESK")
	if app == "" {
		t.Skip("AXX_DESK names a depot desk build")
	}
	runtime.LockOSThread()
	DPIAware()
	kind := func(env, otherwise string) string {
		if k := os.Getenv(env); k != "" {
			return k
		}
		return otherwise
	}
	printLabel, link := kind("AXX_DESK_PRINT_LABEL", "switch"), kind("AXX_DESK_LINK", "link")
	// AXX_DESK_ENABLED=unreported: the toolkit says every control is enabled
	// (Flutter); AXX_DESK_MENU_ITEM=element: its menu items have no role of
	// their own.
	enabledReported := os.Getenv("AXX_DESK_ENABLED") != "unreported"
	arrival, tab, row := kind("AXX_DESK_ARRIVAL", "list item"), kind("AXX_DESK_TAB", "tab"), kind("AXX_DESK_ROW", "row")
	resetRegistry := func() {
		if key := os.Getenv("AXX_DESK_REGISTRY"); key != "" {
			_ = exec.Command("reg", "delete", `HKCU\`+key, "/f").Run()
		}
	}
	resetRegistry()
	t.Cleanup(resetRegistry)

	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	d := &desk{t: t, c: c, app: app, args: strings.Fields(os.Getenv("AXX_DESK_ARGS")), home: t.TempDir(), enabledReported: enabledReported}
	d.start()
	if os.Getenv("AXX_DEBUG") != "" {
		d.shows("No parcels registered yet")
		d.dump(d.window(), 0)
		return
	}
	d.find("image", "Leipzig depot")
	d.shows("No parcels registered yet")
	d.enabled("button", "Register", false)
	d.menuItemEnabled("Depot", "Close day", false)

	d.fill("Reference", "PX-DSK-4201")
	d.enabled("button", "Register", true)
	d.key("Escape")
	d.value("Reference", "")

	d.fill("Reference", "PX-DSK-4201")
	d.click(d.find("checkbox", "Fragile"))
	d.click(d.find("radio button", "Express"))
	d.click(d.find(printLabel, "Print label"))
	d.click(d.find("button", "Register"))
	d.shows("Registered PX-DSK-4201: Express, fragile, label printed")
	d.value("Reference", "")

	d.fill("Reference", "PX-DSK-4202")
	d.key("Enter")
	d.shows("Registered PX-DSK-4202: Express, label printed")
	d.fill("Reference", "PX-DSK-4201")
	d.key("Enter")
	d.shows("PX-DSK-4201 is already registered")
	d.key("Escape")

	d.click(d.find(arrival, "PX-DSK-4201"))
	d.shows("PX-DSK-4201: Express, fragile")
	d.click(d.scrollTo(row, "PX-DSK-4138"))
	d.value("Reference", "PX-DSK-4138")
	d.key("Escape")

	t.Log("restarting: the arrivals and the service level stay")
	d.restart()
	d.shows("2 parcels registered today")
	d.find(arrival, "PX-DSK-4202")
	d.checked("radio button", "Express", true)
	d.menuItemEnabled("Depot", "Close day", true)
	d.chooseMenuItem("Depot", "Close day")
	d.shows("Day closed: 2 parcels handed over")
	d.menuItemEnabled("Depot", "Close day", false)

	d.click(d.find(tab, "Handover"))
	d.shows("Not signed")
	pad := d.find("element", "Courier signature")
	d.clickAt(pad, 40, 40)
	d.shows("Signed")
	d.click(d.find("button", "Clear signature"))
	d.shows("Not signed")
	d.drag(pad, 40, 80, 300, 80)
	d.shows("Signed")
	d.click(d.find(link, "Handover rules"))
	d.shows("Parcels are handed over to the courier at 18:00.")

	d.click(d.find(tab, "Arrivals"))
	d.fill("Reference", "PX-DSK-4203")
	d.key("Enter")
	d.shows("Registered PX-DSK-4203: Express")
	d.stopped()

	t.Log("a new scenario: a new home, and the registry key emptied")
	resetRegistry()
	d.home = t.TempDir()
	d.start()
	d.shows("No parcels registered yet")
	d.checked("radio button", "Standard", true)
	d.missing(arrival, "PX-DSK-4203")
}

// desk is a running depot desk build.
type desk struct {
	t         *testing.T
	c         *Client
	app, home string
	args      []string
	cmd       *exec.Cmd
	// enabledReported: the toolkit says when a control is not enabled.
	enabledReported bool
}

func (d *desk) start() {
	d.t.Helper()
	cmd := exec.Command(d.app, d.args...)
	h := d.home
	cmd.Env = append(os.Environ(), "USERPROFILE="+h, "APPDATA="+filepath.Join(h, `AppData\Roaming`),
		"LOCALAPPDATA="+filepath.Join(h, `AppData\Local`), "TEMP="+filepath.Join(h, "Temp"), "TMP="+filepath.Join(h, "Temp"),
		"JAVA_TOOL_OPTIONS="+strings.TrimSpace(os.Getenv("JAVA_TOOL_OPTIONS")+" -Duser.home="+h))
	for _, dir := range []string{`AppData\Roaming`, `AppData\Local`, "Temp"} {
		_ = os.MkdirAll(filepath.Join(h, dir), 0o755)
	}
	if err := cmd.Start(); err != nil {
		d.t.Fatal(err)
	}
	d.cmd = cmd
	d.t.Cleanup(func() { d.kill() })
	for wait := time.Now(); time.Since(wait) < 30*time.Second; time.Sleep(250 * time.Millisecond) {
		if w := d.window(); w != nil {
			d.t.Logf("%s's window after %s", filepath.Base(d.app), time.Since(wait).Round(time.Millisecond))
			d.fit(w)
			return
		}
	}
	d.t.Fatalf("no window of %s within 30s", d.app)
}

// fit moves the window into the screen's work area, and makes it smaller if
// it is the bigger, as a person does with a window that reaches past the
// screen or under the taskbar: the app's own content scrolls.
func (d *desk) fit(w *Element) {
	d.t.Helper()
	f, a := w.Bounds(), WorkArea()
	if Intersect(f, a) == f {
		return
	}
	width, height := min(f.Right-f.Left, a.Right-a.Left), min(f.Bottom-f.Top, a.Bottom-a.Top)
	left, top := min(max(f.Left, a.Left), a.Right-width), min(max(f.Top, a.Top), a.Bottom-height)
	if err := MoveWindow(w.Handle(), Rect{Left: left, Top: top, Right: left + width, Bottom: top + height}); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	d.t.Logf("the window (%+v) reached past the work area (%+v): now %+v", f, a, w.Bounds())
}

// kill stops the app and every process it started.
func (d *desk) kill() {
	if d.cmd == nil || d.cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(d.cmd.Process.Pid), "/T", "/F").Run()
	_ = d.cmd.Wait()
}

func (d *desk) stopped() {
	d.t.Helper()
	d.kill()
	time.Sleep(500 * time.Millisecond)
}

func (d *desk) restart() {
	d.t.Helper()
	d.stopped()
	d.start()
}

func (d *desk) eventually(what string, ok func() bool) {
	d.t.Helper()
	for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(150 * time.Millisecond) {
		if ok() {
			return
		}
	}
	d.t.Fatalf("waited 10s for %s", what)
}

// windows are the app's top-level windows, its open menus among them.
func (d *desk) windows() []*Element {
	ws, _ := d.c.WindowsOf(d.cmd.Process.Pid)
	return ws
}

// window is the desk's window: the app's window named Depot desk.
func (d *desk) window() *Element {
	for _, w := range d.windows() {
		if w.Name() == "Depot desk" {
			return w
		}
	}
	return nil
}

// names are what an element is called: its name, help and description, and
// each of their lines.
func names(e *Element) []string {
	var out []string
	add := func(v string) {
		if v == "" {
			return
		}
		out = append(out, v)
		if strings.Contains(v, "\n") {
			out = append(out, strings.Split(v, "\n")...)
		}
	}
	add(e.Name())
	add(e.HelpText())
	return out
}

func named(e *Element, name string) bool { return slices.Contains(names(e), name) }

// shown is whether the element shows: on screen, with a size.
func shown(e *Element) bool {
	b := e.Bounds()
	return !e.Offscreen() && b.Right > b.Left && b.Bottom > b.Top
}

func visit(e *Element, fn func(*Element) bool) bool {
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
func holds(e *Element, name string) bool {
	found := false
	visit(e, func(x *Element) bool {
		found = found || named(x, name)
		return !found
	})
	return found
}

func hasText(e *Element, name string) bool {
	found := false
	visit(e, func(x *Element) bool {
		found = found || (named(x, name) || x.ControlType() == typeEdit && x.Value() == name) && shown(x)
		return !found
	})
	return found
}

const (
	typeButton      = 50000
	typeCheckBox    = 50002
	typeEdit        = 50004
	typeHyperlink   = 50005
	typeImage       = 50006
	typeListItem    = 50007
	typeMenuItem    = 50011
	typeRadioButton = 50013
	typeTabItem     = 50019
	typeText        = 50020
	typeTreeItem    = 50024
	typeDataItem    = 50029
	typeWindow      = 50032
	typeScrollBar   = 50014
	typeHeader      = 50034
	typeTitleBar    = 50037
)

// isSwitch is whether the element is a switch: a toggle people read as one.
func isSwitch(e *Element) bool {
	_, toggles := e.Toggled()
	return toggles && (strings.Contains(strings.ToLower(e.LocalizedType()), "switch") || e.ClassName() == "ToggleSwitch")
}

// hasHeader is whether the element has column headers right under it.
func hasHeader(e *Element) bool {
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

// matches is whether e, under parent, is a control of the kind named name.
func matches(kind, name string, e, parent *Element) bool {
	t := e.ControlType()
	switch kind {
	case "button":
		return t == typeButton && !isSwitch(e) && (parent == nil || parent.ControlType() != typeTitleBar) && named(e, name)
	case "field":
		return t == typeEdit && named(e, name)
	case "checkbox":
		return t == typeCheckBox && !isSwitch(e) && named(e, name)
	case "switch":
		return isSwitch(e) && named(e, name)
	case "radio button":
		return t == typeRadioButton && named(e, name)
	case "tab":
		return t == typeTabItem && named(e, name)
	case "list item":
		return t == typeListItem && holds(e, name)
	case "row":
		// A data item; or an item of a list with column headers, which is a
		// table (Windows Forms' list view in its details).
		return (t == typeDataItem || t == typeTreeItem || t == typeListItem && hasHeader(parent)) && holds(e, name)
	case "link":
		return t == typeHyperlink && holds(e, name)
	case "image":
		return t == typeImage && named(e, name)
	case "text":
		return t == typeText && named(e, name)
	case "element":
		return named(e, name)
	}
	return false
}

func search(e *Element, kind, name string, parent *Element, onlyShown bool) *Element {
	if e == nil {
		return nil
	}
	if matches(kind, name, e, parent) && (!onlyShown || shown(e)) {
		return e
	}
	kids, _ := e.Children()
	for _, k := range kids {
		if found := search(k, kind, name, e, onlyShown); found != nil {
			return found
		}
	}
	return nil
}

func (d *desk) find(kind, name string) *Element {
	d.t.Helper()
	var found *Element
	d.eventually(fmt.Sprintf("the %q %s", name, kind), func() bool {
		if w := d.window(); w != nil {
			found = search(w, kind, name, nil, true)
		}
		return found != nil
	})
	return found
}

func (d *desk) missing(kind, name string) {
	d.t.Helper()
	time.Sleep(500 * time.Millisecond)
	if search(d.window(), kind, name, nil, true) != nil {
		d.t.Fatalf("the %q %s is there", name, kind)
	}
}

func (d *desk) shows(text string) {
	d.t.Helper()
	for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(150 * time.Millisecond) {
		if w := d.window(); w != nil && hasText(w, text) {
			d.t.Logf("shows %q", text)
			return
		}
	}
	var texts []string
	if w := d.window(); w != nil {
		visit(w, func(e *Element) bool {
			if e.ControlType() == typeText && shown(e) && len(texts) < 30 && !strings.HasPrefix(e.Name(), "PX-DSK-41") {
				texts = append(texts, e.Name())
			}
			return true
		})
	}
	d.t.Fatalf("waited 10s for the desk to show %q; it shows %q", text, texts)
}

func (d *desk) enabled(kind, name string, want bool) {
	d.t.Helper()
	if !d.enabledReported {
		d.t.Logf("not checking that the %q %s is enabled: %v (not reported)", name, kind, want)
		return
	}
	d.eventually(fmt.Sprintf("the %q %s to be enabled: %v", name, kind, want), func() bool {
		e := search(d.window(), kind, name, nil, true)
		return e != nil && e.Enabled() == want
	})
}

// on is whether a checkbox, switch or radio button is on.
func on(e *Element) bool {
	if v, toggles := e.Toggled(); toggles {
		return v
	}
	v, _ := e.Selected()
	return v
}

func (d *desk) checked(kind, name string, want bool) {
	d.t.Helper()
	d.eventually(fmt.Sprintf("the %q %s to be on: %v", name, kind, want), func() bool {
		e := search(d.window(), kind, name, nil, true)
		return e != nil && on(e) == want
	})
}

func (d *desk) value(field, want string) {
	d.t.Helper()
	// Its value reads wherever it is: scrolled out of view, it is off screen.
	d.eventually(fmt.Sprintf("the %q field to hold %q", field, want), func() bool {
		e := search(d.window(), "field", field, nil, false)
		return e != nil && e.Value() == want
	})
}

func center(e *Element) (int, int, bool) {
	b := e.Bounds()
	if b.Right <= b.Left || b.Bottom <= b.Top {
		return 0, 0, false
	}
	return int(b.Left+b.Right) / 2, int(b.Top+b.Bottom) / 2, true
}

// settled waits for the element to stop moving, 2 seconds at most.
func settled(e *Element) {
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

// usable is the part of the element a click can reach: in its window, and in
// the screen's work area (a window can reach past the screen, or under the
// taskbar).
func (d *desk) usable(e *Element) Rect {
	r := Intersect(e.Bounds(), WorkArea())
	for _, a := range d.areas(e) {
		// Its areas show what they hold only in themselves (not every
		// ancestor: WPF holds a tab's content in its tab, the size of its
		// header).
		if b := a.Bounds(); b.Right > b.Left && b.Bottom > b.Top {
			r = Intersect(r, b)
		}
	}
	return r
}

func inside(x, y int, r Rect) bool {
	return x >= int(r.Left) && x < int(r.Right) && y >= int(r.Top) && y < int(r.Bottom)
}

// inView is whether a click in the element's middle reaches it: the middle
// is in every area that holds it, the window and the work area, and the hit
// test there finds it. WinUI's hit test stops at its content's bridge, above
// the element: that says nothing covers it. Elsewhere a hit above the element
// is what covers it (Qt's page, scrolled over it).
func (d *desk) inView(e *Element) bool {
	x, y, ok := center(e)
	if !ok || e.Offscreen() || !inside(x, y, d.usable(e)) {
		return false
	}
	hit := d.c.ElementAt(x, y)
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

// areas are what the element is in that scroll what they hold, the innermost
// first: those that scroll when asked (the Scroll pattern), lists and tables,
// and last the window.
func (d *desk) areas(e *Element) []*Element {
	var out []*Element
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
func isArea(e *Element) bool {
	switch ControlTypeName(e.ControlType()) {
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
func (d *desk) reveal(e *Element) {
	if d.inView(e) {
		return
	}
	if e.ScrollIntoView() == nil {
		time.Sleep(300 * time.Millisecond)
		if d.inView(e) {
			return
		}
	}
	areas := d.areas(e)
	for i := len(areas) - 1; i >= 0; i-- {
		inner := e
		if i > 0 {
			inner = areas[i-1]
		}
		d.bringInto(areas[i], inner)
	}
}

// bringInto scrolls the area until what it holds (inner) shows in it, or
// fills it when it is the taller: a step at a time, when the area scrolls
// when asked (WinUI takes no wheel that SendInput turns), or else the wheel
// over a part of the area that no area in it covers, as that one would take
// the turns.
func (d *desk) bringInto(a, inner *Element) {
	var inside []*Element
	kids, _ := a.Children()
	for _, k := range kids {
		d.areasIn(k, &inside)
	}
	still := 0
	for range 150 {
		v, f := d.usable(a), inner.Bounds()
		if v.Right <= v.Left || v.Bottom <= v.Top || f.Bottom <= f.Top || still >= 3 {
			return
		}
		holds := f.Top >= v.Top-1 && f.Bottom <= v.Bottom+1
		covers := f.Top <= v.Top+1 && f.Bottom >= v.Bottom-1
		if holds || covers {
			return
		}
		up := f.Top < v.Top
		// A page at a time while it is more than a page away, then a step
		// (Windows Forms' step is a few pixels).
		far := f.Bottom < v.Top-(v.Bottom-v.Top) || f.Top > v.Bottom+(v.Bottom-v.Top)
		if os.Getenv("AXX_DEBUG_ROW") != "" {
			d.t.Logf("    into %s %+v: %s %+v; up %v, far %v", ControlTypeName(a.ControlType()), v, ControlTypeName(inner.ControlType()), f, up, far)
		}
		if a.Scroll(up, far) != nil {
			x, y, ok := spot(v, inside)
			if !ok {
				return
			}
			lines := -1
			if up {
				lines = 1
			}
			_ = Scroll(x, y, lines)
		}
		time.Sleep(60 * time.Millisecond)
		if inner.Bounds() == f { // it did not move: at the area's end, or the area does not scroll
			still++
		} else {
			still = 0
		}
	}
}

// areasIn adds e, or the outermost areas under it, to areas.
func (d *desk) areasIn(e *Element, areas *[]*Element) {
	if isArea(e) {
		*areas = append(*areas, e)
		return
	}
	kids, _ := e.Children()
	for _, k := range kids {
		d.areasIn(k, areas)
	}
}

// spot is a point in v that none of the areas covers: its middle, or near
// one of its edges.
func spot(v Rect, areas []*Element) (int, int, bool) {
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
		for _, a := range areas {
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
func fingerprint(a *Element) string {
	if p, ok := a.ScrollPercent(); ok && a.Scrolls() {
		return fmt.Sprintf("%.3f%%", p)
	}
	var b strings.Builder
	n := 0
	visit(a, func(e *Element) bool {
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

// firstShown names the first few items that show in the box (a trace).
func firstShown(box *Element) string {
	var names []string
	visit(box, func(e *Element) bool {
		if !e.Same(box) && !e.Offscreen() && e.Name() != "" {
			names = append(names, e.Name())
		}
		return len(names) < 6
	})
	return strings.Join(names, ", ")
}

// scrollTo finds the control and brings it into view. Lists make their items
// as they scroll (WPF, WinUI), so when it is not there, each list and table
// is brought into view and scrolled, a page at a time, until it is.
func (d *desk) scrollTo(kind, name string) *Element {
	d.t.Helper()
	win := d.window()
	if e := search(win, kind, name, nil, false); e != nil {
		if d.inView(e) {
			return e
		}
		if os.Getenv("AXX_DESK_SCROLL") == "unreachable" && e.ScrollIntoView() != nil {
			// Flutter gives its controls' places as if nothing had
			// scrolled (flutter/flutter#189124): once a list scrolls, no
			// place in it is true. The control is left where it is and
			// acted on through accessibility.
			d.t.Logf("the %q %s is out of view: it is acted on through accessibility", name, kind)
			return e
		}
		d.reveal(e)
		if f := search(d.window(), kind, name, nil, false); f != nil && d.inView(f) {
			return f
		}
	}
	var lists, panes []*Element
	visit(win, func(e *Element) bool {
		switch ControlTypeName(e.ControlType()) {
		case "List", "DataGrid", "Table", "Tree":
			lists = append(lists, e)
		default:
			if !e.Same(win) && isArea(e) {
				panes = append(panes, e)
			}
		}
		return true
	})
	debug := os.Getenv("AXX_DEBUG_ROW") != ""
	for _, box := range append(lists, panes...) {
		d.reveal(box)
		if debug {
			d.t.Logf("box %s %q at %+v, shows %+v; scrolls %v", ControlTypeName(box.ControlType()), box.Name(), box.Bounds(), d.usable(box), box.Scrolls())
		}
		var inside []*Element
		kids, _ := box.Children()
		for _, k := range kids {
			d.areasIn(k, &inside)
		}
		for _, up := range []bool{false, true} { // down to the end, then up to the start
			still := 0
			for range 80 {
				if f := search(box, kind, name, nil, false); f != nil && d.inView(f) {
					time.Sleep(300 * time.Millisecond) // a list scrolls on a moment after the wheel stops
					if f := search(box, kind, name, nil, false); f != nil && d.inView(f) {
						return f
					}
				}
				if debug {
					if f := search(box, kind, name, nil, false); f != nil {
						x, y, _ := center(f)
						var at []string
						for h := d.c.ElementAt(x, y); h != nil && len(at) < 3; h = h.Parent() {
							at = append(at, ControlTypeName(h.ControlType())+" "+strconv.Quote(h.Name()))
						}
						d.t.Logf("  the %s at %+v, %+v shows, offscreen %v; at its middle %v", kind, f.Bounds(), d.usable(f), f.Offscreen(), at)
					}
				}
				before := fingerprint(box)
				err := box.Scroll(up, true)
				if err != nil {
					u := d.usable(box)
					x, y, ok := spot(u, inside)
					if !ok {
						break
					}
					lines := -3
					if up {
						lines = 3
					}
					_ = Scroll(x, y, lines)
				}
				time.Sleep(150 * time.Millisecond)
				if debug {
					p, ok := box.ScrollPercent()
					d.t.Logf("  scrolled (asked: %v); moved %v; at %.1f%% (%v); first %q", err, fingerprint(box) != before, p, ok, firstShown(box))
				}
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
	if e := search(win, kind, name, nil, false); e != nil {
		x, y, _ := center(e)
		var chain []string
		for at := d.c.ElementAt(x, y); at != nil && len(chain) < 8; at = at.Parent() {
			chain = append(chain, ControlTypeName(at.ControlType())+" "+strconv.Quote(at.Name()))
		}
		d.t.Logf("the %q %s is at %+v (off screen: %v; %+v of it shows); at its middle: %v", name, kind, e.Bounds(), e.Offscreen(), d.usable(e), chain)
	}
	d.t.Fatalf("scrolled every list and table, and found no %q %s in view", name, kind)
	return nil
}

func (d *desk) click(e *Element) {
	d.t.Helper()
	if os.Getenv("AXX_DESK_SCROLL") != "unreachable" {
		d.reveal(e)
	}
	settled(e)
	if !d.inView(e) {
		// Out of the pointer's reach: its action, or its row's; or else its
		// default action, as Windows' older accessibility (MSAA) gives it
		// (Flutter's tap).
		for _, act := range []func(*Element) error{(*Element).Invoke, (*Element).DefaultAction} {
			for at := e; at != nil && at.ControlType() != typeWindow; at = at.Parent() {
				if act(at) == nil {
					d.t.Logf("out of reach: %s %q acted on through accessibility", ControlTypeName(at.ControlType()), at.Name())
					time.Sleep(300 * time.Millisecond)
					return
				}
			}
		}
	}
	x, y, ok := center(e)
	if os.Getenv("AXX_DEBUG_ROW") != "" {
		var at []string
		for h := d.c.ElementAt(x, y); h != nil && len(at) < 3; h = h.Parent() {
			at = append(at, ControlTypeName(h.ControlType())+" "+strconv.Quote(h.Name()))
		}
		d.t.Logf("click %s %q at %d,%d (%+v shows); there: %v", ControlTypeName(e.ControlType()), e.Name(), x, y, d.usable(e), at)
	}
	if !ok {
		d.t.Logf("no place on the screen: clicking through accessibility")
		if err := e.Invoke(); err != nil {
			d.t.Fatal(err)
		}
		return
	}
	if err := Click(x, y); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
}

func (d *desk) key(spec string) {
	d.t.Helper()
	if err := Key(spec); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
}

// fill clicks into the field, selects its text and types over it, until the
// field holds the text.
func (d *desk) fill(field, text string) {
	d.t.Helper()
	for try := 0; try < 4; try++ {
		f := d.find("field", field)
		d.click(f)
		time.Sleep(200 * time.Millisecond)
		d.key("ControlOrMeta+a")
		if err := Type(text); err != nil {
			d.t.Fatal(err)
		}
		for wait := time.Now(); time.Since(wait) < 2*time.Second; time.Sleep(100 * time.Millisecond) {
			if f.Value() == text {
				return
			}
		}
		d.t.Logf("the %q field holds %q, not %q: filling it again", field, f.Value(), text)
	}
	d.t.Fatalf("filled the %q field 4 times, and it does not hold %q", field, text)
}

func (d *desk) clickAt(e *Element, x, y int) {
	d.t.Helper()
	settled(e)
	b := e.Bounds()
	if err := Click(int(b.Left)+x, int(b.Top)+y); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
}

func (d *desk) drag(e *Element, x1, y1, x2, y2 int) {
	d.t.Helper()
	settled(e)
	b := e.Bounds()
	if err := Drag(int(b.Left)+x1, int(b.Top)+y1, int(b.Left)+x2, int(b.Top)+y2); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
}

// openMenu clicks the menu in the window's menu bar, and returns its item,
// which may be in a window of its own (a popup menu).
func (d *desk) openMenu(menu, item string) *Element {
	d.t.Helper()
	m := d.find("element", menu)
	for try := 0; try < 3; try++ {
		d.click(m)
		for wait := time.Now(); time.Since(wait) < 3*time.Second; time.Sleep(150 * time.Millisecond) {
			for _, w := range d.windows() {
				var it *Element
				visit(w, func(e *Element) bool {
					if (e.ControlType() == typeMenuItem || os.Getenv("AXX_DESK_MENU_ITEM") == "element") && named(e, item) && shown(e) {
						it = e
					}
					return it == nil
				})
				if it != nil {
					return it
				}
			}
			// A menu of the system's own (Win32's) is a window of no process.
			if root, err := d.c.Root(); err == nil {
				kids, _ := root.Children()
				for _, k := range kids {
					if k.ClassName() == "#32768" {
						if it := search(k, "element", item, nil, true); it != nil {
							return it
						}
					}
				}
			}
		}
		d.key("Escape")
		time.Sleep(300 * time.Millisecond)
	}
	d.t.Fatalf("clicked the %q menu 3 times, and its %q item did not show", menu, item)
	return nil
}

func (d *desk) menuItemEnabled(menu, item string, want bool) {
	d.t.Helper()
	it := d.openMenu(menu, item)
	got := it.Enabled()
	if !d.enabledReported {
		d.t.Logf("not checking that the %q menu item is enabled: %v (not reported)", item, want)
		got = want
	}
	for range 2 {
		d.key("Escape")
	}
	time.Sleep(300 * time.Millisecond)
	if got != want {
		d.t.Fatalf("the %q menu item is enabled: %v, not %v", item, got, want)
	}
}

func (d *desk) chooseMenuItem(menu, item string) {
	d.t.Helper()
	d.click(d.openMenu(menu, item))
	time.Sleep(400 * time.Millisecond)
}

// dump logs the tree under e, to learn what a toolkit gives (AXX_DEBUG).
func (d *desk) dump(e *Element, depth int) {
	if e == nil || depth > 14 {
		return
	}
	n := e.Name()
	if strings.HasPrefix(n, "PX-DSK-41") && n != "PX-DSK-4101" && n != "PX-DSK-4138" {
		return
	}
	on, toggles := e.Toggled()
	d.t.Logf("%s%s %q class=%q type=%q enabled=%v offscreen=%v toggle=%v/%v", strings.Repeat("  ", depth), ControlTypeName(e.ControlType()), n, e.ClassName(), e.LocalizedType(), e.Enabled(), e.Offscreen(), toggles, on)
	kids, _ := e.Children()
	for _, k := range kids {
		d.dump(k, depth+1)
	}
}
