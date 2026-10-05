//go:build darwin && integration

package ax

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestDepotDesk works a build of the depot desk (examples/parcels/depot-desk)
// as a clerk does, with the pointer and the keyboard: every control its spec
// names, a restart that keeps the day's arrivals, and a new start that is
// reset. Its parcels are not among those expected, so an arrival is never
// mistaken for a row of the expected ones. AXX_DESK names the app (a .app or an executable), AXX_DESK_ARGS its
// arguments, and AXX_DESK_PREFS the preferences domain it keeps its settings
// in, which the reset empties, as cfprefsd keeps them whatever the home.
// AXX_DESK_PRINT_LABEL is the kind of the Print label control: "switch", or
// "checkbox" in toolkits with no switch; AXX_DESK_LINK that of Handover rules:
// "link", or "text" in toolkits with no link; AXX_DESK_ARRIVAL that of an
// arrival: "list item", or "row" where a list is a table (AppKit, SwiftUI).
// For a toolkit that gives fewer roles (Flutter on macOS): AXX_DESK_TAB and
// AXX_DESK_ROW are the kinds of a tab and of an expected parcel's row,
// AXX_DESK_FIELD=only finds the Reference field as the window's only text
// field (it has no name), and AXX_DESK_ENABLED=unreported skips the checks of
// a button's enabled state, which it does not report.
func TestDepotDesk(t *testing.T) {
	app := os.Getenv("AXX_DESK")
	if app == "" {
		t.Skip("AXX_DESK names a depot desk build")
	}
	kind := func(env, otherwise string) string {
		if k := os.Getenv(env); k != "" {
			return k
		}
		return otherwise
	}
	printLabel, link := kind("AXX_DESK_PRINT_LABEL", "switch"), kind("AXX_DESK_LINK", "link")
	arrival := kind("AXX_DESK_ARRIVAL", "list item")
	tab, row := kind("AXX_DESK_TAB", "tab"), kind("AXX_DESK_ROW", "row")
	enabledReported := os.Getenv("AXX_DESK_ENABLED") != "unreported"
	// The reset empties the app's preferences, and puts back what was there
	// when the test ends (Java's domain is every Java app's).
	domain := os.Getenv("AXX_DESK_PREFS")
	resetPrefs := func() {
		if domain != "" {
			_ = exec.Command("defaults", "delete", domain).Run()
		}
	}
	if domain != "" {
		kept := filepath.Join(t.TempDir(), "kept.plist")
		if exec.Command("defaults", "export", domain, kept).Run() == nil {
			t.Cleanup(func() {
				if out, err := exec.Command("defaults", "import", domain, kept).CombinedOutput(); err != nil {
					t.Errorf("putting back the %s preferences: %v %s", domain, err, out)
				}
			})
		}
	}
	resetPrefs()
	t.Cleanup(resetPrefs) // runs first: the import above then puts back what was there

	d := &desk{t: t, app: app, args: strings.Fields(os.Getenv("AXX_DESK_ARGS")), home: t.TempDir(), onlyField: os.Getenv("AXX_DESK_FIELD") == "only"}
	buttonEnabled := func(name string, want bool) {
		t.Helper()
		if enabledReported {
			d.enabled("button", name, want)
		} else {
			t.Logf("not checking that %q is enabled: %v (not reported)", name, want)
		}
	}
	d.start()
	d.find("image", "Leipzig depot")
	d.shows("No parcels registered yet")
	buttonEnabled("Register", false)
	d.menuItemEnabled("Depot", "Close day", false)

	d.fill("Reference", "PX-DSK-4201")
	buttonEnabled("Register", true)
	d.key("Escape")
	d.value("Reference", "")

	d.fill("Reference", "PX-DSK-4201")
	d.click(d.find("checkbox", "Fragile"))
	d.click(d.find("radio button", "Express"))
	d.click(d.find(printLabel, "Print label"))
	d.click(d.find("button", "Register"))
	d.shows("Registered PX-DSK-4201: Express, fragile, label printed")
	d.value("Reference", "")
	d.checked("checkbox", "Fragile", false)

	d.fill("Reference", "PX-DSK-4202")
	d.key("Enter")
	d.shows("Registered PX-DSK-4202: Express, label printed")
	d.fill("Reference", "PX-DSK-4201")
	d.key("Enter")
	d.shows("PX-DSK-4201 is already registered")
	d.key("Escape")

	d.click(d.find(arrival, "PX-DSK-4201"))
	d.shows("PX-DSK-4201: Express, fragile")
	expectedRow := d.scrollTo(row, "PX-DSK-4138")
	d.click(expectedRow)
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

	t.Log("a new scenario: a new home, and the preferences emptied")
	resetPrefs()
	d.home = t.TempDir()
	d.start()
	d.shows("No parcels registered yet")
	d.checked("radio button", "Standard", true)
	d.missing(arrival, "PX-DSK-4203")
}

// TestDeskFits proves the fit a launch makes: a window that reaches past the
// screen comes back into its visible frame. macOS keeps a window's height in
// the screen itself, and lets it reach past the screen's right edge.
func TestDeskFits(t *testing.T) {
	app := os.Getenv("AXX_DESK")
	if app == "" {
		t.Skip("AXX_DESK names a depot desk build")
	}
	d := &desk{t: t, app: app, args: strings.Fields(os.Getenv("AXX_DESK_ARGS")), home: t.TempDir()}
	d.start()
	w := d.window()
	f, _ := frameOf(w)
	if err := w.SetPosition(Point{X: d.screen.x + d.screen.w - 200, Y: f.y}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	f, _ = frameOf(w)
	if d.screen.intersect(f) == f {
		t.Fatalf("the window (%v) is in the screen's visible frame (%v) already: nothing to fit", f, d.screen)
	}
	d.fit()
	time.Sleep(300 * time.Millisecond)
	if g, _ := frameOf(w); d.screen.intersect(g) != g {
		t.Fatalf("the window is at %v, past the screen's visible frame (%v)", g, d.screen)
	}
	d.stopped()
}

// desk is a running depot desk build.
type desk struct {
	t         *testing.T
	app, home string
	args      []string
	root, win *Element
	pid       int
	stop      func()
	onlyField bool // the window's only text field is the field steps name
	screen    area // the visible frame of the window's screen
}

func (d *desk) start() {
	d.t.Helper()
	d.root, d.win, d.stop = launchIn(d.t, d.home, d.app, d.args...)
	var err error
	if d.pid, err = d.root.PID(); err != nil {
		d.t.Fatal(err)
	}
	// What VoiceOver sets on an app as it starts reading it: Flutter builds
	// its semantics for it. Electron builds its tree for its own attribute.
	_ = d.root.SetBool("AXEnhancedUserInterface", true)
	_ = d.root.SetBool("AXManualAccessibility", true)
	d.fit()
}

// fit moves the window into the screen's visible frame, and makes it smaller
// if it is the bigger, as a person does with a window that reaches past the
// screen or under the Dock: the app's own content scrolls.
func (d *desk) fit() {
	d.t.Helper()
	w := d.window()
	f, ok := frameOf(w)
	if !ok {
		d.t.Fatal("the window has no place on the screen")
	}
	pos, size, err := VisibleFrame(Point{X: f.x + f.w/2, Y: f.y + 10})
	if err != nil {
		d.t.Fatal(err)
	}
	d.screen = area{pos.X, pos.Y, size.Width, size.Height}
	if d.screen.intersect(f) == f {
		return
	}
	fitted := Size{Width: min(f.w, d.screen.w), Height: min(f.h, d.screen.h)}
	at := Point{X: min(max(f.x, d.screen.x), d.screen.x+d.screen.w-fitted.Width), Y: min(max(f.y, d.screen.y), d.screen.y+d.screen.h-fitted.Height)}
	if err := w.SetSize(fitted); err != nil {
		d.t.Fatal(err)
	}
	if err := w.SetPosition(at); err != nil {
		d.t.Fatal(err)
	}
	g, _ := frameOf(w)
	d.t.Logf("the window (%v) reached past the screen's visible frame (%v): now %v", f, d.screen, g)
}

// stopped stops the app and waits for its process to end.
func (d *desk) stopped() {
	d.t.Helper()
	d.stop()
	for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
		if syscall.Kill(d.pid, 0) != nil {
			return
		}
	}
	d.t.Fatalf("the desk (process %d) did not stop", d.pid)
}

func (d *desk) restart() {
	d.t.Helper()
	d.stopped()
	d.start()
}

// eventually waits up to 10 seconds for ok.
func (d *desk) eventually(what string, ok func() bool) {
	d.t.Helper()
	for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(150 * time.Millisecond) {
		if ok() {
			return
		}
	}
	d.t.Fatalf("waited 10s for %s", what)
}

// window is the desk's window as it is now.
func (d *desk) window() *Element {
	ws, _ := d.root.Elements("AXWindows")
	for _, w := range ws {
		if w.String("AXTitle") == "Depot desk" {
			return w
		}
	}
	if len(ws) > 0 {
		return ws[0]
	}
	return d.win
}

// names are what an element is called: its title, description, tooltip,
// placeholder, and a text's value.
func names(e *Element) []string {
	var out []string
	add := func(v string) {
		if v == "" {
			return
		}
		out = append(out, v)
		// And each of its lines: Flutter joins the texts it merges into one
		// label, and a hint after a label, with new lines.
		if strings.Contains(v, "\n") {
			out = append(out, strings.Split(v, "\n")...)
		}
	}
	for _, a := range []string{"AXTitle", "AXDescription", "AXHelp", "AXPlaceholderValue"} {
		add(e.String(a))
	}
	if e.String("AXRole") == "AXStaticText" {
		add(e.String("AXValue"))
	}
	if v, err := e.Attribute("AXTitleUIElement"); err == nil {
		if label, ok := v.(*Element); ok {
			out = append(out, label.String("AXValue"), label.String("AXTitle"))
		}
	}
	return out
}

func named(e *Element, name string) bool {
	for _, n := range names(e) {
		if n == name {
			return true
		}
	}
	return false
}

func hasText(e *Element, name string) bool {
	found := false
	walk(e, func(x *Element) bool {
		found = found || (named(x, name) || x.String("AXValue") == name) && shown(x)
		return !found
	})
	return found
}

// shown is whether an element shows: Java has hidden components in the tree,
// of no size.
func shown(e *Element) bool {
	_, size, err := e.Frame()
	return err != nil || size.Width > 0 || size.Height > 0
}

// matches is whether e, under parent, is a control of the kind named name.
func matches(kind, name string, e, parentElement *Element) bool {
	role, sub := e.String("AXRole"), e.String("AXSubrole")
	parent := ""
	if parentElement != nil {
		parent = parentElement.String("AXRole")
	}
	switch kind {
	case "button":
		return role == "AXButton" && sub != "AXSwitch" && sub != "AXCloseButton" &&
			sub != "AXMinimizeButton" && sub != "AXFullScreenButton" && sub != "AXZoomButton" && named(e, name)
	case "field":
		return (role == "AXTextField" || role == "AXTextArea" || role == "AXComboBox") && named(e, name)
	case "checkbox":
		return role == "AXCheckBox" && sub != "AXSwitch" && named(e, name)
	case "switch":
		return (role == "AXSwitch" || sub == "AXSwitch") && named(e, name)
	case "radio button":
		return role == "AXRadioButton" && !isTab(e, parentElement) && named(e, name)
	case "tab":
		return role == "AXRadioButton" && isTab(e, parentElement) && named(e, name)
	case "list item":
		return parent == "AXList" && (named(e, name) || hasText(e, name))
	case "row":
		// A row with the text; in a table that has no rows (Qt 5), the cell.
		return role == "AXRow" && hasText(e, name) ||
			(parent == "AXTable" || parent == "AXOutline") && (role == "AXStaticText" || role == "AXCell") && named(e, name)
	case "link":
		return role == "AXLink" && (named(e, name) || hasText(e, name))
	case "image":
		return role == "AXImage" && named(e, name)
	case "text":
		return role == "AXStaticText" && named(e, name)
	case "element":
		return named(e, name)
	}
	return false
}

// isTab is whether a radio button is a tab: a tab button (AppKit), one with
// no on or off of its own (Qt), or one of the tabs its tab group lists
// (WebKit). A tab group holds its tab's controls too (AppKit), radio buttons
// among them.
func isTab(e, parent *Element) bool {
	if e.String("AXSubrole") == "AXTabButton" {
		return true
	}
	if _, err := e.Attribute("AXValue"); err != nil {
		return true
	}
	if parent == nil || parent.String("AXRole") != "AXTabGroup" {
		return false
	}
	tabs, _ := parent.Elements("AXTabs")
	pos, _, err := e.Frame()
	for _, t := range tabs {
		if p, _, terr := t.Frame(); err == nil && terr == nil && p == pos && t.String("AXTitle") == e.String("AXTitle") {
			return true
		}
	}
	return false
}

// search is the first control of the kind named name under e, the first in
// the tree's order.
func search(e *Element, kind, name string, parent *Element) *Element {
	if matches(kind, name, e, parent) && shown(e) {
		return e
	}
	role := e.String("AXRole")
	kids, _ := e.Children()
	for _, k := range kids {
		if found := search(k, kind, name, e); found != nil {
			return found
		}
	}
	if role == "AXTabGroup" && hasDead(kids) {
		for _, tab := range tabsDrawn(e) {
			if (kind == "tab" || kind == "element") && named(tab, name) {
				return tab
			}
		}
	}
	return nil
}

// hasDead is whether some of the elements are gone as they are read (Java
// gives a tab group's tabs that way).
func hasDead(es []*Element) bool {
	for _, e := range es {
		if _, err := e.Attribute("AXRole"); err != nil {
			return true
		}
	}
	return false
}

// tabsDrawn are the tabs drawn along the top of a tab group, found where a
// person sees them: what the screen has at points along its top edge.
func tabsDrawn(g *Element) []*Element {
	pos, size, err := g.Frame()
	if err != nil {
		return nil
	}
	app, err := g.PID()
	if err != nil {
		return nil
	}
	root, err := Application(app)
	if err != nil {
		return nil
	}
	var tabs []*Element
	seen := map[string]bool{}
	for y := pos.Y + 4; y < pos.Y+40; y += 8 {
		for x := pos.X + 4; x < pos.X+size.Width; x += 12 {
			e, err := root.ElementAt(Point{X: x, Y: y})
			if err != nil || e.String("AXRole") != "AXRadioButton" {
				continue
			}
			if n := strings.Join(names(e), "|"); !seen[n] {
				seen[n] = true
				tabs = append(tabs, e)
			}
		}
	}
	return tabs
}

// find waits for the control of the kind named name in the desk's window.
func (d *desk) find(kind, name string) *Element {
	d.t.Helper()
	var found *Element
	d.eventually(fmt.Sprintf("the %q %s", name, kind), func() bool {
		found = search(d.window(), kind, name, nil)
		return found != nil
	})
	return found
}

// field is the field named name, or, for a toolkit that names no field
// (onlyField), the window's only text field.
func (d *desk) field(name string) *Element {
	d.t.Helper()
	if !d.onlyField {
		return d.find("field", name)
	}
	var found *Element
	d.eventually("the window's text field", func() bool {
		found = nil
		walk(d.window(), func(e *Element) bool {
			if e.String("AXRole") == "AXTextField" {
				found = e
			}
			return found == nil
		})
		return found != nil
	})
	return found
}

func (d *desk) missing(kind, name string) {
	d.t.Helper()
	time.Sleep(500 * time.Millisecond)
	if search(d.window(), kind, name, nil) != nil {
		d.t.Fatalf("the %q %s is there", name, kind)
	}
}

func (d *desk) shows(text string) {
	d.t.Helper()
	d.eventually(fmt.Sprintf("the desk to show %q", text), func() bool { return hasText(d.window(), text) })
	d.t.Logf("shows %q", text)
}

func (d *desk) enabled(kind, name string, want bool) {
	d.t.Helper()
	e := d.find(kind, name)
	d.eventually(fmt.Sprintf("the %q %s to be enabled: %v", name, kind, want), func() bool { return e.Bool("AXEnabled") == want })
}

func on(e *Element) bool {
	v, _ := e.Attribute("AXValue")
	return fmt.Sprint(v) == "1" || v == true
}

func (d *desk) checked(kind, name string, want bool) {
	d.t.Helper()
	e := d.find(kind, name)
	d.eventually(fmt.Sprintf("the %q %s to be on: %v", name, kind, want), func() bool { return on(e) == want })
}

func (d *desk) value(field, want string) {
	d.t.Helper()
	e := d.field(field)
	d.eventually(fmt.Sprintf("the %q field to hold %q (it holds %q)", field, want, e.String("AXValue")), func() bool { return e.String("AXValue") == want })
}

// front brings the desk to the front and makes sure it has the keyboard:
// keys go to the app in front, and must never go to another.
func (d *desk) front() {
	d.t.Helper()
	_ = d.root.SetBool("AXFrontmost", true)
	_ = d.window().Perform("AXRaise")
	for wait := time.Now(); time.Since(wait) < 3*time.Second; time.Sleep(50 * time.Millisecond) {
		if d.root.Bool("AXFrontmost") {
			return
		}
	}
	d.t.Fatalf("the desk (process %d) is not in front: stopping before any key goes to another app", d.pid)
}

// scrollTo finds the control and brings it into view, as a person looks for
// it: some toolkits (Qt 5) have only the rows that show in their tree, so
// each list and table is brought into view and its wheel turned until the
// control is there.
func (d *desk) scrollTo(kind, name string) *Element {
	d.t.Helper()
	if e := search(d.window(), kind, name, nil); e != nil {
		d.scrollIntoView(e)
		return e
	}
	var boxes []*Element
	walk(d.window(), func(e *Element) bool {
		switch e.String("AXRole") {
		case "AXTable", "AXOutline", "AXList", "AXScrollArea":
			boxes = append(boxes, e)
		}
		return true
	})
	for _, box := range boxes {
		d.scrollIntoView(box)
		for _, lines := range []int{-3, 3} { // down to the end, then up to the start
			for range 40 {
				if e := search(d.window(), kind, name, nil); e != nil {
					d.scrollIntoView(e)
					return e
				}
				v, ok := d.visible(box)
				if !ok {
					break
				}
				d.front()
				_ = Scroll(v.middle(), lines)
			}
		}
	}
	d.t.Fatalf("scrolled every list and table, and found no %q %s", name, kind)
	return nil
}

// area is a rectangle on the screen, in points.
type area struct{ x, y, w, h float64 }

func frameOf(e *Element) (area, bool) {
	pos, size, err := e.Frame()
	return area{pos.X, pos.Y, size.Width, size.Height}, err == nil
}

func (a area) intersect(b area) area {
	x, y := max(a.x, b.x), max(a.y, b.y)
	return area{x, y, max(min(a.x+a.w, b.x+b.w)-x, 0), max(min(a.y+a.h, b.y+b.h)-y, 0)}
}

func (a area) empty() bool   { return a.w <= 0 || a.h <= 0 }
func (a area) middle() Point { return Point{X: a.x + a.w/2, Y: a.y + a.h/2} }
func (a area) contains(p Point) bool {
	return p.X >= a.x && p.X < a.x+a.w && p.Y >= a.y && p.Y < a.y+a.h
}
func (a area) holds(b area) bool     { return b.y >= a.y-1 && b.y+b.h <= a.y+a.h+1 }
func (a area) coveredBy(b area) bool { return b.y <= a.y+1 && b.y+b.h >= a.y+a.h-1 }
func (a area) String() string        { return fmt.Sprintf("%.0f,%.0f %.0fx%.0f", a.x, a.y, a.w, a.h) }

func parent(e *Element) (*Element, bool) {
	v, err := e.Attribute("AXParent")
	p, ok := v.(*Element)
	return p, err == nil && ok
}

// clipping are the areas the element is in that show only part of what
// they hold, the innermost first: scroll areas, and tables and lists (their
// own scroll areas, in toolkits that give them none, like Qt). inWindow is
// whether the element is in a window (a menu bar's items are not).
func clipping(e *Element) (areas []*Element, inWindow bool) {
	for at, ok := parent(e); ok; at, ok = parent(at) {
		switch at.String("AXRole") {
		case "AXScrollArea", "AXTable", "AXOutline", "AXList":
			areas = append(areas, at)
		case "AXWindow":
			return areas, true
		}
	}
	return areas, false
}

// visible is the part of the element that shows: in each area that clips
// it, in its window, and in the screen's visible frame (not under the menu
// bar or the Dock).
func (d *desk) visible(e *Element) (area, bool) {
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
	if w, ok := frameOf(d.window()); ok && inWindow {
		v = v.intersect(w)
	}
	v = v.intersect(d.screen)
	return v, !v.empty()
}

// inView is whether a click in the element's middle reaches it: the middle
// shows, and the app has there the element, or one in exactly its place, or
// a part of it under the area that holds it (Java makes its elements anew
// each time they are read, and hits a row's cell as the table's; WebKit hits
// a checkbox's label). A hit test that stops above the element, in the area
// that holds it, says nothing covers it.
func (d *desk) inView(e *Element) bool {
	f, ok := frameOf(e)
	if !ok || f.w < 2 || f.h < 2 {
		// Flutter squashes what is scrolled out of its area onto the area's
		// edge, a point high or wide.
		return false
	}
	if v, ok := d.visible(e); !ok || !v.contains(f.middle()) {
		return false
	}
	hit, err := d.root.ElementAt(f.middle())
	if err != nil {
		return true // no hit test: what clips it is all there is to go by
	}
	// Its ancestors up to the area that holds it.
	var above []*Element
	for at, ok := parent(e); ok; at, ok = parent(at) {
		above = append(above, at)
		if r := at.String("AXRole"); r == "AXScrollArea" || r == "AXTable" || r == "AXOutline" || r == "AXList" || r == "AXWindow" {
			break
		}
	}
	isAbove := func(x *Element) bool {
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
	for at, ok := hit, true; ok; at, ok = parent(at) {
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
func (d *desk) scrollIntoView(e *Element) {
	d.t.Helper()
	if d.inView(e) {
		return
	}
	if e.Perform("AXScrollToVisible") == nil {
		time.Sleep(300 * time.Millisecond)
		if d.inView(e) {
			return
		}
	}
	areas, _ := clipping(e)
	if len(areas) == 0 {
		return // in nothing that scrolls, as far as the tree says (Flutter's lists are groups)
	}
	d.front()
	for i := len(areas) - 1; i >= 0; i-- {
		inner := e
		if i > 0 {
			inner = areas[i-1]
		}
		d.wheelInto(areas[i], inner)
	}
	if !d.inView(e) {
		f, _ := frameOf(e)
		v, _ := d.visible(e)
		var in []string
		for _, a := range areas {
			g, _ := frameOf(a)
			in = append(in, a.String("AXRole")+" "+g.String())
		}
		w, _ := frameOf(d.window())
		var hits []string
		if hit, err := d.root.ElementAt(f.middle()); err == nil {
			for at, ok := hit, true; ok && len(hits) < 6; at, ok = parent(at) {
				g, _ := frameOf(at)
				hits = append(hits, at.String("AXRole")+" "+strings.Join(names(at), "|")+" "+g.String())
			}
		}
		var up []string
		for at, ok := parent(e); ok && len(up) < 6; at, ok = parent(at) {
			g, _ := frameOf(at)
			up = append(up, at.String("AXRole")+" "+g.String())
		}
		d.t.Fatalf("scrolled every area the element (%s %v) is in, and it is not in view: %v of it shows; it is in %v, the window at %v, the screen's visible frame %v; at its middle %v; its ancestors %v",
			e.String("AXRole"), f, v, in, w, d.screen, hits, up)
	}
}

// wheelInto turns the wheel over the area until what it holds (inner) shows
// in it, or fills it when it is the taller. The wheel turns over a part of
// the area that no area inside it covers (a list, a table), as that one
// would take the turns.
func (d *desk) wheelInto(a, inner *Element) {
	var inside []*Element
	for _, k := range children(a) {
		areasIn(k, &inside)
	}
	for range 80 {
		v, ok := d.visible(a)
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
		if os.Getenv("AXX_DEBUG_WHEEL") != "" {
			fmt.Printf("wheel %s: visible %v, inner %s %v, at %v, %d\n", a.String("AXRole"), v, inner.String("AXRole"), f, at, lines)
		}
		_ = Scroll(at, lines)
		time.Sleep(40 * time.Millisecond)
	}
}

func children(e *Element) []*Element {
	kids, _ := e.Children()
	return kids
}

// areasIn adds e, or the outermost areas under it, to areas: scroll areas,
// tables and lists.
func areasIn(e *Element, areas *[]*Element) {
	switch e.String("AXRole") {
	case "AXScrollArea", "AXTable", "AXOutline", "AXList":
		*areas = append(*areas, e)
		return
	}
	for _, k := range children(e) {
		areasIn(k, areas)
	}
}

// spot is a point in v that none of the areas covers: its middle, or near
// one of its edges.
func spot(v area, areas []*Element) (Point, bool) {
	m := v.middle()
	for _, p := range []Point{
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
			if f, ok := frameOf(a); ok && f.contains(p) {
				free = false
				break
			}
		}
		if free {
			return p, true
		}
	}
	return Point{}, false
}

// settled waits for the element to stop moving (a tab slides in), 2 seconds
// at most: a click lands where it is.
func settled(e *Element) {
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

func (d *desk) click(e *Element) {
	d.t.Helper()
	d.front()
	d.scrollIntoView(e)
	settled(e)
	at, err := e.Center()
	if err != nil {
		d.t.Logf("no place on the screen: pressing through accessibility (%v)", err)
		if err := e.Perform("AXPress"); err != nil {
			d.t.Fatal(err)
		}
		return
	}
	if os.Getenv("AXX_DEBUG_CLICK") != "" {
		f, _ := frameOf(e)
		var hits []string
		if hit, err := d.root.ElementAt(at); err == nil {
			for x, ok := hit, true; ok && len(hits) < 3; x, ok = parent(x) {
				g, _ := frameOf(x)
				hits = append(hits, x.String("AXRole")+" "+strings.Join(names(x), "|")+" "+g.String())
			}
		}
		fmt.Printf("click %s %q %v at %v: %v\n", e.String("AXRole"), names(e), f, at, hits)
	}
	if err := Click(at); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
}

func (d *desk) key(spec string) {
	d.t.Helper()
	d.front()
	if err := PressKey(spec); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
}

// fill clicks into the field, selects its text, and types over it, until the
// field holds the text: a click while the window moves (a tab sliding in)
// misses it, and some fields (Flutter's) never say they have the focus.
func (d *desk) fill(field, text string) {
	d.t.Helper()
	for try := 0; try < 4; try++ {
		f := d.field(field)
		d.click(f)
		d.key("ControlOrMeta+a")
		d.front()
		if err := Type(text); err != nil {
			d.t.Fatal(err)
		}
		for wait := time.Now(); time.Since(wait) < 2*time.Second; time.Sleep(100 * time.Millisecond) {
			if f.String("AXValue") == text {
				return
			}
		}
		d.t.Logf("the %q field holds %q, not %q: filling it again", field, f.String("AXValue"), text)
	}
	d.t.Fatalf("filled the %q field 4 times, and it does not hold %q", field, text)
}

func (d *desk) at(e *Element, x, y float64) Point {
	d.t.Helper()
	v, err := e.Attribute("AXPosition")
	p, ok := v.(Point)
	if err != nil || !ok {
		d.t.Fatalf("the element has no place on the screen: %v", err)
	}
	return Point{X: p.X + x, Y: p.Y + y}
}

func (d *desk) clickAt(e *Element, x, y float64) {
	d.t.Helper()
	d.front()
	d.scrollIntoView(e)
	settled(e)
	if err := Click(d.at(e, x, y)); err != nil {
		d.t.Fatal(err)
	}
}

func (d *desk) drag(e *Element, x1, y1, x2, y2 float64) {
	d.t.Helper()
	d.front()
	d.scrollIntoView(e)
	settled(e)
	if err := Drag(d.at(e, x1, y1), d.at(e, x2, y2)); err != nil {
		d.t.Fatal(err)
	}
}

// menuBar is the app's menu bar.
func (d *desk) menuBar() *Element {
	d.t.Helper()
	v, err := d.root.Attribute("AXMenuBar")
	bar, ok := v.(*Element)
	if err != nil || !ok {
		d.t.Fatalf("the desk has no menu bar: %v", err)
	}
	return bar
}

// openMenuIn clicks the menu in the menu bar, and returns its item and the
// menu.
func (d *desk) openMenuIn(menu, item string) (*Element, *Element) {
	d.t.Helper()
	var m *Element
	d.eventually(fmt.Sprintf("the %q menu", menu), func() bool {
		// In the menu bar, or in a menu bar of the window's own (Swing's).
		for _, in := range []*Element{d.menuBar(), d.window()} {
			if m = searchRole(in, "AXMenuBarItem", menu); m != nil {
				return true
			}
		}
		return false
	})
	d.click(m)
	var it *Element
	d.eventually(fmt.Sprintf("the %q menu item", item), func() bool {
		it = searchRole(m, "AXMenuItem", item)
		return it != nil
	})
	return it, m
}

// searchRole is the first element under e of the role named name.
func searchRole(e *Element, role, name string) *Element {
	var found *Element
	walk(e, func(x *Element) bool {
		if x.String("AXRole") == role && named(x, name) {
			found = x
		}
		return found == nil
	})
	return found
}

// menuClosed waits for the menu to close: a click while it closes goes
// nowhere. Swing's menu bar item stays selected, so it waits 2 seconds at
// most.
func (d *desk) menuClosed(m *Element) {
	d.t.Helper()
	for wait := time.Now(); time.Since(wait) < 2*time.Second && m.Bool("AXSelected"); time.Sleep(100 * time.Millisecond) {
	}
	time.Sleep(200 * time.Millisecond)
}

func (d *desk) menuItemEnabled(menu, item string, want bool) {
	d.t.Helper()
	it, m := d.openMenuIn(menu, item)
	got := it.Bool("AXEnabled")
	d.key("Escape")
	d.menuClosed(m)
	if got != want {
		d.t.Fatalf("the %q menu item is enabled: %v, not %v", item, got, want)
	}
}

func (d *desk) chooseMenuItem(menu, item string) {
	d.t.Helper()
	it, m := d.openMenuIn(menu, item)
	d.click(it)
	d.menuClosed(m)
}
