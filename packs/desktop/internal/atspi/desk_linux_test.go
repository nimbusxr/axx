//go:build linux && integration

package atspi

import (
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestDepotDesk works a build of the depot desk (examples/parcels/depot-desk)
// as a clerk does, with the pointer and the keyboard (XTest): every control
// its spec names, a restart that keeps the day's arrivals, and a new start
// that is reset (a new home). Its parcels are not among those expected, so an
// arrival is never mistaken for a row of the expected ones.
//
// AXX_DESK names the app's executable and AXX_DESK_ARGS its arguments. The
// kinds of a few controls differ by toolkit: AXX_DESK_PRINT_LABEL ("switch",
// or "checkbox"), AXX_DESK_LINK ("link", or "text"), AXX_DESK_ARRIVAL ("list
// item"), AXX_DESK_TAB ("tab"), AXX_DESK_ROW ("row") and AXX_DESK_RADIO
// ("radio button"). AXX_DESK_MENU=unreachable skips the Depot menu, for a
// toolkit whose open menus are not in the tree (GTK 4.14's popovers), and
// AXX_DESK_VALUE=unreported a field's value, for one that does not report
// what is typed into it (Java's bridge), and AXX_DESK_ENABLED=unreported a
// button's enabled state, for one that does not report it; with
// AXX_DESK_ENABLED=actions, a control is enabled when it offers an action
// like a click (Flutter offers none on one that is not), and with
// AXX_DESK_MENU_ITEM=element a menu's items are found by name, whatever
// their role (Flutter's are panels); and
// AXX_DESK_POSITIONS=none the signature pad, for one that gives no places on
// the screen (Flutter), where nothing can be drawn.
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
	arrival, tab, row := kind("AXX_DESK_ARRIVAL", "list item"), kind("AXX_DESK_TAB", "tab"), kind("AXX_DESK_ROW", "row")
	radio := kind("AXX_DESK_RADIO", "radio button")
	menu := os.Getenv("AXX_DESK_MENU") != "unreachable"
	buttonEnabled := func(d *desk, name string, want bool) {
		t.Helper()
		if os.Getenv("AXX_DESK_ENABLED") == "unreported" {
			t.Logf("not checking that %q is enabled: %v (not reported)", name, want)
			return
		}
		d.enabled("button", name, want)
	}
	_ = buttonEnabled

	c := native(t)
	input, err := NewInput()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(input.Close)
	d := &desk{
		t: t, c: c, in: input, app: app, args: strings.Fields(os.Getenv("AXX_DESK_ARGS")), home: t.TempDir(),
		valueUnreported: os.Getenv("AXX_DESK_VALUE") == "unreported",
	}
	// A failed walk keeps what the screen showed, at AXX_SHOT.
	defer func() {
		if shot := os.Getenv("AXX_SHOT"); shot != "" && t.Failed() {
			if img, err := input.Screenshot(); err == nil {
				if f, err := os.Create(shot); err == nil {
					_ = png.Encode(f, img)
					_ = f.Close()
				}
			}
		}
	}()
	d.start()
	d.find("image", "Leipzig depot")
	d.shows("No parcels registered yet")
	buttonEnabled(d, "Register", false)
	if menu {
		d.menuItemEnabled("Depot", "Close day", false)
	} else {
		t.Log("not using the Depot menu: its items are not in the tree")
	}

	d.fill("Reference", "PX-DSK-4201")
	buttonEnabled(d, "Register", true)
	d.key("Escape")
	d.value("Reference", "")

	d.fill("Reference", "PX-DSK-4201")
	d.click(d.find("checkbox", "Fragile"))
	d.click(d.find(radio, "Express"))
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
	d.checked(radio, "Express", true)
	if menu {
		d.menuItemEnabled("Depot", "Close day", true)
		d.chooseMenuItem("Depot", "Close day")
		d.shows("Day closed: 2 parcels handed over")
		d.menuItemEnabled("Depot", "Close day", false)
	}

	d.click(d.find(tab, "Handover"))
	d.shows("Not signed")
	pad := d.find("element", "Courier signature")
	if os.Getenv("AXX_DESK_POSITIONS") == "none" {
		t.Log("not drawing on the signature pad: no places on the screen")
	} else {
		d.clickAt(pad, 40, 40)
		d.shows("Signed")
		d.click(d.find("button", "Clear signature"))
		d.shows("Not signed")
		d.drag(pad, 40, 80, 300, 80)
		d.shows("Signed")
	}
	d.click(d.find(link, "Handover rules"))
	d.shows("Parcels are handed over to the courier at 18:00.")

	d.click(d.find(tab, "Arrivals"))
	d.fill("Reference", "PX-DSK-4203")
	d.key("Enter")
	d.shows("Registered PX-DSK-4203: Express")
	d.stopped()

	t.Log("a new scenario: a new home")
	d.home = t.TempDir()
	d.start()
	d.shows("No parcels registered yet")
	d.checked(radio, "Standard", true)
	d.missing(arrival, "PX-DSK-4203")
}

// desk is a running depot desk build.
type desk struct {
	t         *testing.T
	c         *Client
	in        *Input
	app, home string
	args      []string
	cmd       *exec.Cmd
	root      *Element
	// valueUnreported: a field's value is not read, as the toolkit does not
	// report what is typed into it.
	valueUnreported bool
	gtk4            bool // the app is made with GTK 4
}

func (d *desk) start() {
	d.t.Helper()
	cmd := exec.Command(d.app, d.args...)
	h := d.home
	cmd.Env = append(os.Environ(), "HOME="+h, "XDG_DATA_HOME="+filepath.Join(h, ".local/share"),
		"XDG_CONFIG_HOME="+filepath.Join(h, ".config"), "XDG_CACHE_HOME="+filepath.Join(h, ".cache"),
		"XDG_STATE_HOME="+filepath.Join(h, ".local/state"),
		"JAVA_TOOL_OPTIONS="+strings.TrimSpace(os.Getenv("JAVA_TOOL_OPTIONS")+" -Duser.home="+h))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		d.t.Fatal(err)
	}
	d.cmd = cmd
	d.t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	for wait := time.Now(); time.Since(wait) < 30*time.Second; time.Sleep(250 * time.Millisecond) {
		if root, err := d.c.ApplicationOf(cmd.Process.Pid); err == nil {
			d.root = root
			name, version := root.Toolkit()
			d.gtk4 = name == "GTK" && strings.HasPrefix(version, "4.")
			if IsFlutter(cmd.Process.Pid) {
				root.WithoutPlaces()
			}
			d.t.Logf("%s on the bus after %s", filepath.Base(d.app), time.Since(wait).Round(time.Millisecond))
			return
		}
	}
	d.t.Fatalf("%s did not come on the accessibility bus within 30s", d.app)
}

// stopped stops the app and every process it started.
func (d *desk) stopped() {
	d.t.Helper()
	_ = syscall.Kill(-d.cmd.Process.Pid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = d.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-d.cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
	time.Sleep(300 * time.Millisecond)
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

// window is the desk's window as it is now: the app's frame named Depot desk.
func (d *desk) window() *Element {
	kids, _ := d.root.Children()
	for _, k := range kids {
		if k.Name() == "Depot desk" {
			return k
		}
	}
	if len(kids) > 0 {
		return kids[0]
	}
	return d.root
}

var textRoles = []string{"label", "static", "paragraph", "text", "heading", "caption"}

// names are what an element is called: its name and description, a text's
// text, and each of their lines.
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
	add(e.Description())
	// A text's text, but not a field's: what a field holds is not its name.
	if slices.Contains(textRoles, e.Role()) && !e.Is(StateEditable) {
		add(e.Text())
	}
	return out
}

func named(e *Element, name string) bool { return slices.Contains(names(e), name) }

// visit calls fn on e and the elements under it, in the tree's order, while
// fn says to go on.
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

// shown is whether the element shows: on the screen, by its state.
func shown(e *Element) bool { return e.Is(StateShowing) }

// holds is whether the element or one under it has the text, shown or not:
// a row is known by its texts wherever it is.
func holds(e *Element, name string) bool {
	found := false
	visit(e, func(x *Element) bool {
		found = found || named(x, name) || x.Text() == name
		return !found
	})
	return found
}

func hasText(e *Element, name string) bool {
	found := false
	visit(e, func(x *Element) bool {
		found = found || (named(x, name) || x.Text() == name) && shown(x)
		return !found
	})
	return found
}

// matches is whether e, under parent, is a control of the kind named name.
func matches(kind, name string, e, parent *Element) bool {
	role := e.Role()
	parentRole := ""
	if parent != nil {
		parentRole = parent.Role()
	}
	switch kind {
	case "button":
		return role == "push button" && named(e, name)
	case "field":
		// An entry, or a text that can be edited (a label's text is a text too).
		return (role == "entry" || role == "password text" || role == "text" && e.Is(StateEditable)) && named(e, name)
	case "checkbox":
		return role == "check box" && named(e, name)
	case "switch":
		return (role == "toggle button" || role == "switch") && named(e, name)
	case "radio button":
		return role == "radio button" && named(e, name)
	case "tab":
		return role == "page tab" && named(e, name)
	case "list item":
		return role == "list item" && holds(e, name)
	case "row":
		// A row with the text; in a table with no rows around its cells, the
		// cell: a table cell (GTK), or what a table holds with nothing under
		// it (Swing's cells are their renderers, labels; GTK 4's table holds
		// a list of its rows).
		return role == "table row" && holds(e, name) ||
			role == "table cell" && parentRole != "table row" && holds(e, name) ||
			(parentRole == "table" || parentRole == "tree table") && e.ChildCount() == 0 && role != "column header" && role != "table column header" && holds(e, name)
	case "link":
		return role == "link" && (named(e, name) || hasText(e, name))
	case "image":
		return (role == "image" || role == "icon") && named(e, name)
	case "text":
		return slices.Contains(textRoles, role) && named(e, name)
	case "element":
		return named(e, name)
	}
	return false
}

func search(e *Element, kind, name string, parent *Element) *Element {
	if matches(kind, name, e, parent) && shown(e) {
		return e
	}
	kids, _ := e.Children()
	for _, k := range kids {
		if found := search(k, kind, name, e); found != nil {
			return found
		}
	}
	return nil
}

func (d *desk) find(kind, name string) *Element {
	d.t.Helper()
	var found *Element
	d.eventually(fmt.Sprintf("the %q %s", name, kind), func() bool {
		found = search(d.window(), kind, name, nil)
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
	for wait := time.Now(); time.Since(wait) < 10*time.Second; time.Sleep(150 * time.Millisecond) {
		if hasText(d.window(), text) {
			d.t.Logf("shows %q", text)
			return
		}
	}
	// What it shows instead, as a failed step says.
	var texts []string
	visit(d.window(), func(e *Element) bool {
		if slices.Contains(textRoles, e.Role()) && shown(e) && len(texts) < 30 {
			if n := names(e); len(n) > 0 && !strings.HasPrefix(n[0], "PX-DSK-41") {
				texts = append(texts, n[0])
			}
		}
		return true
	})
	d.t.Fatalf("waited 10s for the desk to show %q; it shows %q", text, texts)
}

func (d *desk) enabled(kind, name string, want bool) {
	d.t.Helper()
	d.eventually(fmt.Sprintf("the %q %s to be enabled: %v", name, kind, want), func() bool {
		e := search(d.window(), kind, name, nil)
		return e != nil && isEnabled(e) == want
	})
}

// clickActions are the names toolkits give the action a click takes.
var clickActions = []string{"click", "Tap", "press", "activate", "toggle", "jump"}

// isEnabled is whether the control takes clicks: by its state, or, with
// AXX_DESK_ENABLED=actions, by its offering an action like a click (Flutter
// says nothing of its state, and offers none when not enabled).
func isEnabled(e *Element) bool {
	if os.Getenv("AXX_DESK_ENABLED") == "actions" {
		for _, a := range e.Actions() {
			if slices.Contains(clickActions, a) {
				return true
			}
		}
		return false
	}
	return e.Is(StateSensitive)
}

func (d *desk) checked(kind, name string, want bool) {
	d.t.Helper()
	e := d.find(kind, name)
	d.eventually(fmt.Sprintf("the %q %s to be on: %v", name, kind, want), func() bool { return e.Is(StateChecked) == want })
}

func (d *desk) value(field, want string) {
	d.t.Helper()
	if d.valueUnreported {
		d.t.Logf("not checking that the %q field holds %q (not reported)", field, want)
		return
	}
	// Its value reads wherever it is: GTK 3 says a field scrolled out of
	// view does not show.
	var e *Element
	d.eventually(fmt.Sprintf("the %q field", field), func() bool {
		e = searchAny(d.window(), "field", field, nil)
		return e != nil
	})
	d.eventually(fmt.Sprintf("the %q field to hold %q (it holds %q)", field, want, e.Text()), func() bool { return e.Text() == want })
}

// center is the middle of the element on the screen.
func center(e *Element) (int, int, bool) {
	r := e.Extents()
	if r.Width <= 0 || r.Height <= 0 {
		return 0, 0, false
	}
	return int(r.X + r.Width/2), int(r.Y + r.Height/2), true
}

// settled waits for the element to stop moving (a tab slides in), 2 seconds
// at most.
func settled(e *Element) {
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
// its window: GTK 4 gives a scrolled window no role of its own, so the window
// stands for the areas the tree does not name.
func (d *desk) clipping(e *Element) []*Element {
	win := d.window()
	var areas []*Element
	for at := e.Parent(); at != nil && !at.Same(win); at = at.Parent() {
		if slices.Contains(areaRoles, at.Role()) {
			areas = append(areas, at)
		}
	}
	return append(areas, win)
}

// scrollIntoView brings the element into view as a person does. The app
// scrolls it there when asked (Component's ScrollTo: the web engines scroll
// every area it is in); else the wheel turns over each area it is in, the
// outermost first, until the next one in (or the element) shows in it.
func (d *desk) scrollIntoView(e *Element) {
	d.t.Helper()
	d.reveal(e)
	if !d.inView(e) {
		areas := d.clipping(e)
		var in []string
		for _, a := range areas {
			in = append(in, fmt.Sprintf("%s %+v", a.Role(), a.Extents()))
		}
		var hits []string
		if x, y, ok := center(e); ok {
			for at := deepestAt(d.window(), x, y); at != nil && len(hits) < 5; at = at.Parent() {
				hits = append(hits, fmt.Sprintf("%s %q %+v showing %v", at.Role(), at.Name(), at.Extents(), at.Is(StateShowing)))
			}
		}
		d.t.Fatalf("scrolled every area the %s %q is in, and it is not in view: it is at %+v, %+v of it shows; it is in %v; at its middle %v",
			e.Role(), e.Name(), e.Extents(), visiblePart(e, d.window()), in, hits)
	}
}

// reveal scrolls the element into view as far as it goes, as scrollIntoView
// does, and says nothing if it is not then in view: a list's middle may not
// show, and its rows do.
func (d *desk) reveal(e *Element) {
	debug := os.Getenv("AXX_DEBUG_ROW") != ""
	if d.inView(e) {
		if debug {
			d.t.Logf("    reveal %s: in view", e.Role())
		}
		return
	}
	if e.ScrollTo() {
		time.Sleep(300 * time.Millisecond)
		if d.inView(e) {
			if debug {
				d.t.Logf("    reveal %s: scrolled to by the app", e.Role())
			}
			return
		}
	}
	areas := d.clipping(e)
	if debug {
		var in []string
		for _, a := range areas {
			in = append(in, a.Role())
		}
		d.t.Logf("    reveal %s: in %v", e.Role(), in)
	}
	for i := len(areas) - 1; i >= 0; i-- {
		inner := e
		if i > 0 {
			inner = areas[i-1]
		}
		d.wheelInto(areas[i], inner)
	}
}

// wheelInto turns the wheel over the area until what it holds (inner) shows
// in it, or fills it when it is the taller. The wheel turns over a part of
// the area that no area inside it covers (a list, a table), as that one
// would take the turns.
func (d *desk) wheelInto(a, inner *Element) {
	var inside []*Element
	kids, _ := a.Children()
	for _, k := range kids {
		areasIn(k, &inside)
	}
	debug := os.Getenv("AXX_DEBUG_ROW") != ""
	// Where it has no place yet (GTK 3 gives what it has not drawn none), it
	// is looked for as a person looks: down to the end, then up.
	look, still := -2, 0
	for range 120 {
		v, f := visiblePart(a, d.window()), inner.Extents()
		unplaced := f.X < -1e8 || f.Y < -1e8
		holds := !unplaced && f.Y >= v.Y-1 && f.Y+f.Height <= v.Y+v.Height+1
		covers := !unplaced && f.Y <= v.Y+1 && f.Y+f.Height >= v.Y+v.Height-1
		if debug {
			d.t.Logf("    into %s %+v: %s %+v; holds %v, covers %v", a.Role(), v, inner.Role(), f, holds, covers)
		}
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
		_ = d.in.Scroll(x, y, lines)
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
func areasIn(e *Element, areas *[]*Element) {
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
func spot(v Rect, areas []*Element) (int, int, bool) {
	mx, my := int(v.X+v.Width/2), int(v.Y+v.Height/2)
	for _, p := range [][2]int{
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
			if placed(r) && p[0] >= int(r.X) && p[0] < int(r.X+r.Width) && p[1] >= int(r.Y) && p[1] < int(r.Y+r.Height) {
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

// inView is whether a click in the element's middle reaches it: it shows,
// and nothing covers it (GTK 3 gives a cell it has not laid out the least
// integer, and a web page's rows below its scroll box their own place).
func (d *desk) inView(e *Element) bool {
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
	win := d.window()
	from := win
	for at := e.Parent(); at != nil && !at.Same(from); at = at.Parent() {
		if r := at.Role(); r == "document web" || r == "document frame" {
			from = at
			break
		}
	}
	// Its middle shows: in every area it is in, and in its window. GTK 4's
	// hit test finds what an area clips away too. A web engine's hit test
	// knows its scroll boxes (which have no role of their own), and WebKit
	// moves its scroll panes' places with what they scroll: in a page, the
	// window is all that is checked here.
	v := visiblePart(e, win)
	if !from.Same(win) {
		v = intersect(e.Extents(), win.Extents())
	}
	if x < int(v.X) || x >= int(v.X+v.Width) || y < int(v.Y) || y >= int(v.Y+v.Height) {
		return false
	}
	hit := deepestAt(from, x, y)
	if hit != nil && !hit.Is(StateShowing) || d.gtk4 {
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
	// says nothing covers it: then it is in view if it is in what its
	// viewport or scroll pane shows.
	return hit != nil && isAbove(hit, e)
}

// isAbove is whether a is e or one of e's ancestors.
func isAbove(a, e *Element) bool {
	for at := e; at != nil; at = at.Parent() {
		if at.Same(a) {
			return true
		}
	}
	return false
}

// scrollTo finds the control and brings it into view, as a person scrolls to
// it: some toolkits have only what shows in their tree (GTK 3 makes a
// table's cells anew as it scrolls), others what does not show too, marked
// so (Chromium). The app is asked to scroll it into view (Component's
// ScrollTo, or GTK 4's own action), and else the wheel turns over each list,
// table and scroll pane, where it shows in the window, until it is in view.
func (d *desk) scrollTo(kind, name string) *Element {
	d.t.Helper()
	win := d.window()
	if e := searchAny(win, kind, name, nil); e != nil {
		if !e.HasPlaces() {
			// An app with no places (Flutter's) is acted on through
			// accessibility, in view or not.
			return e
		}
		if d.inView(e) {
			return e
		}
		// After a scroll, found anew: GTK 4 gives a row's element to the
		// rows that scroll into its place.
		again := func() *Element {
			time.Sleep(400 * time.Millisecond)
			if f := searchAny(win, kind, name, nil); f != nil && d.inView(f) {
				return f
			}
			return nil
		}
		scrolled := e.ScrollTo()
		if os.Getenv("AXX_DEBUG_ROW") != "" {
			time.Sleep(400 * time.Millisecond)
			f := searchAny(win, kind, name, nil)
			x, y, _ := center(f)
			var hits []string
			for at := deepestAt(win, x, y); at != nil && len(hits) < 4; at = at.Parent() {
				hits = append(hits, fmt.Sprintf("%s %q %+v", at.Role(), at.Name(), at.Extents()))
			}
			var up []string
			for at := f.Parent(); at != nil && len(up) < 6; at = at.Parent() {
				up = append(up, fmt.Sprintf("%s %+v", at.Role(), at.Extents()))
			}
			d.t.Logf("ScrollTo %v: the %s at %+v, in view %v; at its middle %v; its ancestors %v", scrolled, kind, f.Extents(), d.inView(f), hits, up)
		}
		if scrolled {
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
	var lists, panes []*Element
	visit(win, func(e *Element) bool {
		switch e.Role() {
		case "table", "tree table", "list", "list box":
			lists = append(lists, e)
		case "scroll pane":
			panes = append(panes, e)
		}
		return true
	})
	debug := os.Getenv("AXX_DEBUG_ROW") != ""
	for _, box := range append(lists, panes...) {
		d.reveal(box)
		if debug {
			d.t.Logf("box %s %q at %+v, shows %+v", box.Role(), box.Name(), box.Extents(), visiblePart(box, win))
		}
		var inside []*Element
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
				if e := searchAny(box, kind, name, nil); e != nil && d.inView(e) {
					// GTK 4 scrolls on a moment after the wheel stops: once it
					// rests, found anew, it is there or not.
					time.Sleep(400 * time.Millisecond)
					if f := searchAny(box, kind, name, nil); f != nil && d.inView(f) {
						return f
					}
				}
				before := fingerprint(box)
				_ = d.in.Scroll(x, y, lines)
				time.Sleep(150 * time.Millisecond)
				if fingerprint(box) == before {
					still++ // at its end, or it does not scroll
				} else {
					still = 0
				}
				if debug {
					f := searchAny(box, kind, name, nil)
					var at string
					if f != nil {
						at = fmt.Sprintf("%+v in view %v", f.Extents(), d.inView(f))
					}
					d.t.Logf("  wheel %d at %d,%d: still %d; the %s %s", lines, x, y, still, kind, at)
				}
			}
		}
	}
	d.t.Fatalf("scrolled every list and table, and found no %q %s in view", name, kind)
	return nil
}

// placed is whether the element has a place on the screen: GTK 3 gives what
// it has not drawn the least integer.
func placed(r Rect) bool { return r.X > -1e8 && r.Y > -1e8 && r.Width > 0 && r.Height > 0 }

// fingerprint is what shows first in the box, and where: it changes as the
// box scrolls.
func fingerprint(box *Element) string {
	v := box.Extents()
	var b strings.Builder
	n := 0
	visit(box, func(e *Element) bool {
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

// searchAny is search, shown or not.
func searchAny(e *Element, kind, name string, parent *Element) *Element {
	if matches(kind, name, e, parent) {
		return e
	}
	kids, _ := e.Children()
	for _, k := range kids {
		if found := searchAny(k, kind, name, e); found != nil {
			return found
		}
	}
	return nil
}

// visiblePart is the part of the element that shows: in its viewports and
// scroll panes, and in its window, when the window says where it is (Java's
// does not).
func visiblePart(e, win *Element) Rect {
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
func intersect(a, b Rect) Rect {
	x1, y1 := max(a.X, b.X), max(a.Y, b.Y)
	x2, y2 := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	return Rect{X: x1, Y: y1, Width: x2 - x1, Height: y2 - y1}
}

// deepestAt is the deepest element at the point: an element answers for
// itself and its children, and Chromium's window answers with the view that
// holds its page, which then answers for the page.
func deepestAt(e *Element, x, y int) *Element {
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

func (d *desk) click(e *Element) {
	d.t.Helper()
	if _, _, ok := center(e); !ok {
		// No place on the screen (Flutter): its action, at once, as asking
		// it to scroll remakes it.
		d.t.Logf("no place on the screen: clicking through accessibility")
		d.activate(e)
		return
	}
	d.scrollIntoView(e)
	settled(e)
	x, y, ok := center(e)
	if !ok {
		d.t.Fatalf("the element lost its place on the screen as it scrolled")
	}
	if err := d.in.Click(x, y); err != nil {
		d.t.Fatal(err)
	}
	if err := d.in.FocusUnderPointer(); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
}

// activate takes the element's action that a click would: its name differs
// by toolkit.
func (d *desk) activate(e *Element) {
	d.t.Helper()
	var acts []string
	// An app that was just asked for a place it has not (Flutter's) answers
	// nothing for a few seconds.
	for wait := time.Now(); time.Since(wait) < 6*time.Second; time.Sleep(200 * time.Millisecond) {
		acts = e.Actions()
		for _, a := range clickActions {
			if slices.Contains(acts, a) {
				if err := e.Do(a); err != nil {
					d.t.Fatal(err)
				}
				time.Sleep(300 * time.Millisecond)
				return
			}
		}
	}
	d.t.Fatalf("the element (%s %q) has no place on the screen and no action like a click: %v", e.Role(), e.Name(), acts)
}

func (d *desk) key(spec string) {
	d.t.Helper()
	if err := d.in.Key(spec); err != nil {
		d.t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
}

// fill clicks into the field, selects its text and types over it, until the
// field holds the text. Keys go to the window under the pointer (there is no
// window manager), which the click leaves over the field.
func (d *desk) fill(field, text string) {
	d.t.Helper()
	for try := 0; try < 4; try++ {
		f := d.find("field", field)
		if _, _, ok := center(f); ok {
			d.click(f)
		} else {
			// No place on the screen (Flutter): the field takes the focus by
			// its own action, and the keys go to its window, under the pointer.
			// Its Focus action, else its Tap; it offers neither while it has
			// the focus already.
			acts := f.Actions()
			for _, a := range []string{"Focus", "Tap"} {
				if slices.Contains(acts, a) {
					if err := f.Do(a); err != nil {
						d.t.Fatal(err)
					}
					break
				}
			}
			_ = d.in.Move(40, 40)
			_ = d.in.FocusUnderPointer()
		}
		// Typed a moment after the click: Java's keys go to the field a little
		// after it takes the focus.
		time.Sleep(300 * time.Millisecond)
		d.key("ControlOrMeta+a")
		if err := d.in.Type(text); err != nil {
			d.t.Fatal(err)
		}
		if d.valueUnreported {
			time.Sleep(300 * time.Millisecond)
			return
		}
		for wait := time.Now(); time.Since(wait) < 2*time.Second; time.Sleep(100 * time.Millisecond) {
			if f.Text() == text {
				return
			}
		}
		d.t.Logf("the %q field holds %q, not %q: filling it again", field, f.Text(), text)
	}
	d.t.Fatalf("filled the %q field 4 times, and it does not hold %q", field, text)
}

func (d *desk) clickAt(e *Element, x, y int) {
	d.t.Helper()
	settled(e)
	r := e.Extents()
	if err := d.in.Click(int(r.X)+x, int(r.Y)+y); err != nil {
		d.t.Fatal(err)
	}
}

func (d *desk) drag(e *Element, x1, y1, x2, y2 int) {
	d.t.Helper()
	settled(e)
	r := e.Extents()
	if err := d.in.Drag(int(r.X)+x1, int(r.Y)+y1, int(r.X)+x2, int(r.Y)+y2); err != nil {
		d.t.Fatal(err)
	}
}

// openMenu clicks the menu in the window's menu bar, and returns its item. A
// menu bar just closed can take the click for closing (Electron's): then it
// clicks again.
func (d *desk) openMenu(menu, item string) *Element {
	d.t.Helper()
	var m *Element
	d.eventually(fmt.Sprintf("the %q menu", menu), func() bool {
		m = search(d.window(), "element", menu, nil)
		return m != nil
	})
	for try := 0; try < 3; try++ {
		d.click(m)
		for wait := time.Now(); time.Since(wait) < 3*time.Second; time.Sleep(150 * time.Millisecond) {
			var it *Element
			for _, in := range []*Element{m, d.root} {
				visit(in, func(e *Element) bool {
					if (e.Role() == "menu item" || os.Getenv("AXX_DESK_MENU_ITEM") == "element") && named(e, item) && shown(e) {
						it = e
					}
					return it == nil
				})
				if it != nil {
					return it
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
	got := isEnabled(it)
	d.closeMenu(menu, it)
	if got != want {
		d.t.Fatalf("the %q menu item is enabled: %v, not %v", item, got, want)
	}
}

// closeMenu closes the menu with Escape until it lets go of the keyboard:
// Swing's first Escape closes the menu, and leaves its menu bar selected,
// which takes the keys.
func (d *desk) closeMenu(menu string, item *Element) {
	d.t.Helper()
	for range 3 {
		d.key("Escape")
		time.Sleep(200 * time.Millisecond)
		m := search(d.window(), "element", menu, nil)
		if !shown(item) && (m == nil || !m.Is(StateSelected)) {
			return
		}
	}
}

func (d *desk) chooseMenuItem(menu, item string) {
	d.t.Helper()
	it := d.openMenu(menu, item)
	// An open menu's item never lies on the menu that opened it: one that
	// says it does gives its place in its popup, not on the screen (Java's),
	// and is chosen through accessibility.
	if m := search(d.window(), "element", menu, nil); m != nil {
		x, y, _ := center(it)
		if b := m.Extents(); x >= int(b.X) && x < int(b.X+b.Width) && y >= int(b.Y) && y < int(b.Y+b.Height) {
			d.t.Logf("the %q menu item gives no place on the screen: choosing it through accessibility", item)
			if err := it.Do("click"); err != nil {
				d.t.Fatal(err)
			}
			time.Sleep(300 * time.Millisecond)
			return
		}
	}
	d.click(it)
	time.Sleep(300 * time.Millisecond)
}
