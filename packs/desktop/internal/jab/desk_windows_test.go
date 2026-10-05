//go:build windows && integration

package jab

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

	"github.com/nimbusxr/axx/packs/desktop/internal/uia"
)

// TestDepotDesk works the Swing build of the depot desk on Windows as a clerk
// does, through the Java Access Bridge (UI Automation sees only a Java
// window's frame), with the pointer and the keyboard (SendInput): the same
// journey as the other OSes' walks. AXX_JAVA names java.exe (its bin has the
// bridge's DLL) and AXX_DESK_ARGS its arguments (-jar and the desk's jar).
func TestDepotDesk(t *testing.T) {
	java := os.Getenv("AXX_JAVA")
	if java == "" {
		t.Skip("AXX_JAVA names java.exe")
	}
	runtime.LockOSThread()
	uia.DPIAware()
	c, err := New(filepath.Join(filepath.Dir(java), "WindowsAccessBridge-64.dll"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := uia.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(u.Close)
	d := &desk{t: t, c: c, u: u, java: java, args: strings.Fields(os.Getenv("AXX_DESK_ARGS")), home: t.TempDir()}
	d.start()
	if os.Getenv("AXX_DEBUG") != "" {
		d.dump(d.root, 0)
		return
	}
	k := func(env, otherwise string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return otherwise
	}
	arrival, row, link := k("AXX_DESK_ARRIVAL", "list item"), k("AXX_DESK_ROW", "row"), k("AXX_DESK_LINK", "link")

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
	d.click(d.find("checkbox", "Print label"))
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

	d.click(d.find("tab", "Handover"))
	d.shows("Not signed")
	pad := d.find("image", "Courier signature")
	d.clickAt(pad, 40, 40)
	d.shows("Signed")
	d.click(d.find("button", "Clear signature"))
	d.shows("Not signed")
	d.drag(pad, 40, 80, 300, 80)
	d.shows("Signed")
	d.click(d.find(link, "Handover rules"))
	d.shows("Parcels are handed over to the courier at 18:00.")

	d.click(d.find("tab", "Arrivals"))
	d.fill("Reference", "PX-DSK-4203")
	d.key("Enter")
	d.shows("Registered PX-DSK-4203: Express")
	d.stopped()

	t.Log("a new scenario: a new home")
	d.home = t.TempDir()
	d.start()
	d.shows("No parcels registered yet")
	d.checked("radio button", "Standard", true)
	d.missing(arrival, "PX-DSK-4203")
}

type desk struct {
	t          *testing.T
	c          *Client
	u          *uia.Client
	java, home string
	args       []string
	cmd        *exec.Cmd
	root       *Element
	scale      float64 // screen pixels for each of Java's points
}

func (d *desk) start() {
	d.t.Helper()
	cmd := exec.Command(d.java, d.args...)
	h := d.home
	cmd.Env = append(os.Environ(), "USERPROFILE="+h, "APPDATA="+filepath.Join(h, `AppData\Roaming`),
		"LOCALAPPDATA="+filepath.Join(h, `AppData\Local`), "TEMP="+filepath.Join(h, "Temp"), "TMP="+filepath.Join(h, "Temp"),
		// The bridge on in this Java, for this launch, and Java's home.
		"JAVA_TOOL_OPTIONS=-Djavax.accessibility.assistive_technologies=com.sun.java.accessibility.AccessBridge -Duser.home="+h)
	for _, dir := range []string{`AppData\Roaming`, `AppData\Local`, "Temp"} {
		_ = os.MkdirAll(filepath.Join(h, dir), 0o755)
	}
	if err := cmd.Start(); err != nil {
		d.t.Fatal(err)
	}
	d.cmd = cmd
	d.t.Cleanup(d.kill)
	for wait := time.Now(); time.Since(wait) < 30*time.Second; {
		d.c.Pump(250 * time.Millisecond)
		ws, _ := d.u.WindowsOf(cmd.Process.Pid)
		for _, w := range ws {
			if w.Name() != "Depot desk" || !d.c.IsJavaWindow(w.Handle()) {
				continue
			}
			root, err := d.c.Window(w.Handle())
			if err != nil {
				continue
			}
			d.root = root
			b := w.Bounds()
			_, _, jw, _ := root.Bounds()
			d.scale = float64(b.Right-b.Left) / float64(jw)
			d.t.Logf("the bridge knows the window after %s (%.2f pixels a point)", time.Since(wait).Round(time.Millisecond), d.scale)
			d.fit(w)
			return
		}
	}
	d.t.Fatal("the bridge never knew the desk's window within 30s")
}

// fit moves the window into the screen's work area, and makes it smaller if
// it is the bigger, as a person does with a window that reaches past the
// screen or under the taskbar: the app's own content scrolls.
func (d *desk) fit(w *uia.Element) {
	d.t.Helper()
	f, a := w.Bounds(), uia.WorkArea()
	if uia.Intersect(f, a) == f {
		return
	}
	width, height := min(f.Right-f.Left, a.Right-a.Left), min(f.Bottom-f.Top, a.Bottom-a.Top)
	left, top := min(max(f.Left, a.Left), a.Right-width), min(max(f.Top, a.Top), a.Bottom-height)
	if err := uia.MoveWindow(w.Handle(), uia.Rect{Left: left, Top: top, Right: left + width, Bottom: top + height}); err != nil {
		d.t.Fatal(err)
	}
	d.c.Pump(300 * time.Millisecond)
	d.t.Logf("the window (%+v) reached past the work area (%+v): now %+v", f, a, w.Bounds())
}

func (d *desk) kill() {
	if d.cmd == nil || d.cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(d.cmd.Process.Pid), "/T", "/F").Run()
	_ = d.cmd.Wait()
}

func (d *desk) stopped() {
	d.kill()
	time.Sleep(500 * time.Millisecond)
}

func (d *desk) restart() {
	d.t.Helper()
	d.stopped()
	d.start()
}

func (d *desk) dump(e *Element, depth int) {
	if depth > 14 {
		return
	}
	x, y, w, h := e.Bounds()
	if n := e.Name(); !strings.HasPrefix(n, "PX-DSK-41") || n == "PX-DSK-4101" || n == "PX-DSK-4138" {
		d.t.Logf("%s%s %q states=%s at %d,%d %dx%d actions=%v", strings.Repeat("  ", depth), e.Role(), n, e.States(), x, y, w, h, e.Actions())
	}
	for _, k := range e.Children() {
		d.dump(k, depth+1)
	}
}

func (d *desk) eventually(what string, ok func() bool) {
	d.t.Helper()
	for wait := time.Now(); time.Since(wait) < 10*time.Second; {
		if ok() {
			return
		}
		d.c.Pump(150 * time.Millisecond)
	}
	d.t.Fatalf("waited 10s for %s", what)
}

func visit(e *Element, fn func(*Element) bool) bool {
	if !fn(e) {
		return false
	}
	if e.Role() == "table" {
		return true // its children are its one cell renderer: see visibleCells
	}
	for _, k := range e.Children() {
		if !visit(k, fn) {
			return false
		}
	}
	return true
}

func shown(e *Element) bool { return e.Has("showing") }

func holds(e *Element, name string) bool {
	found := false
	visit(e, func(x *Element) bool {
		found = found || x.Name() == name
		return !found
	})
	return found
}

// roles are the bridge's roles of each kind.
var roles = map[string][]string{
	"button": {"push button"}, "field": {"text", "password text"}, "checkbox": {"check box"},
	"radio button": {"radio button"}, "tab": {"page tab"}, "list item": {"list item"},
	"row": {"row"}, "link": {"hyperlink"}, "image": {"icon"}, "text": {"label"},
	"menu": {"menu"}, "menu item": {"menu item"},
}

func matches(kind, name string, e *Element) bool {
	switch kind {
	case "element":
		return e.Name() == name
	case "row":
		// A row with the text (a JTable's cells are found by visibleCells).
		return e.Role() == "row" && holds(e, name)
	}
	return slices.Contains(roles[kind], e.Role()) && e.Name() == name
}

func search(e *Element, kind, name string, onlyShown bool) *Element {
	var found *Element
	visit(e, func(x *Element) bool {
		if matches(kind, name, x) && (!onlyShown || shown(x)) {
			found = x
		}
		return found == nil
	})
	return found
}

func (d *desk) find(kind, name string) *Element {
	d.t.Helper()
	var found *Element
	d.eventually(fmt.Sprintf("the %q %s", name, kind), func() bool {
		found = search(d.root, kind, name, true)
		return found != nil
	})
	return found
}

func (d *desk) missing(kind, name string) {
	d.t.Helper()
	d.c.Pump(500 * time.Millisecond)
	if search(d.root, kind, name, true) != nil {
		d.t.Fatalf("the %q %s is there", name, kind)
	}
}

func (d *desk) shows(text string) {
	d.t.Helper()
	d.eventually(fmt.Sprintf("the desk to show %q", text), func() bool { return search(d.root, "element", text, true) != nil })
	d.t.Logf("shows %q", text)
}

func (d *desk) enabled(kind, name string, want bool) {
	d.t.Helper()
	d.eventually(fmt.Sprintf("the %q %s to be enabled: %v", name, kind, want), func() bool {
		e := search(d.root, kind, name, true)
		return e != nil && e.Has("enabled") == want
	})
}

func (d *desk) checked(kind, name string, want bool) {
	d.t.Helper()
	d.eventually(fmt.Sprintf("the %q %s to be on: %v", name, kind, want), func() bool {
		e := search(d.root, kind, name, true)
		return e != nil && e.Has("checked") == want
	})
}

func (d *desk) value(field, want string) {
	d.t.Helper()
	d.eventually(fmt.Sprintf("the %q field to hold %q", field, want), func() bool {
		e := search(d.root, "field", field, true)
		return e != nil && e.Text() == want
	})
}

// px is a point of Java's on the screen, in pixels.
func (d *desk) px(x, y int) (int, int) {
	return int(float64(x) * d.scale), int(float64(y) * d.scale)
}

func center(e *Element) (int, int) {
	x, y, w, h := e.Bounds()
	return x + w/2, y + h/2
}

// inView is whether a click in the element's middle reaches it: the middle
// is in every viewport it is in and in the work area, and the hit test there
// finds it, or stops above it (Java's hit test stops short).
func (d *desk) inView(e *Element) bool {
	if !shown(e) {
		return false
	}
	x, y := center(e)
	if vx, vy, vw, vh := d.visiblePart(e); x < vx || x >= vx+vw || y < vy || y >= vy+vh {
		return false
	}
	hit := d.root.At(x, y)
	for at := hit; at != nil; at = at.Parent() {
		if at.Same(e) {
			return true
		}
	}
	return hit != nil && isAbove(hit, e)
}

// viewports are the viewports the element is in, the innermost first: a
// scroll pane shows what it holds in its viewport.
func viewports(e *Element) []*Element {
	var out []*Element
	for at := e.Parent(); at != nil; at = at.Parent() {
		if at.Role() == "viewport" {
			out = append(out, at)
		}
	}
	return out
}

// visiblePart is the part of the element that shows, in Java's points: in
// each viewport it is in, and in the screen's work area (not under the
// taskbar).
func (d *desk) visiblePart(e *Element) (x, y, w, h int) {
	x, y, w, h = e.Bounds()
	x2, y2 := x+w, y+h
	for _, v := range viewports(e) {
		vx, vy, vw, vh := v.Bounds()
		x, y, x2, y2 = max(x, vx), max(y, vy), min(x2, vx+vw), min(y2, vy+vh)
	}
	a := uia.WorkArea()
	x, y = max(x, int(float64(a.Left)/d.scale)), max(y, int(float64(a.Top)/d.scale))
	x2, y2 = min(x2, int(float64(a.Right)/d.scale)), min(y2, int(float64(a.Bottom)/d.scale))
	return x, y, max(x2-x, 0), max(y2-y, 0)
}

// inPart is whether the cell's middle is in the part of its table that shows.
func (d *desk) inPart(cell, table *Element) bool {
	x, y, w, h := d.visiblePart(table)
	cx, cy := center(cell)
	return cx >= x && cx < x+w && cy >= y && cy < y+h
}

// reveal scrolls the element into view as far as it goes: each viewport it
// is in, the outermost first, until the next one in (or the element) shows
// in it.
func (d *desk) reveal(e *Element) {
	if d.inView(e) {
		return
	}
	vs := viewports(e)
	for i := len(vs) - 1; i >= 0; i-- {
		inner := e
		if i > 0 {
			inner = vs[i-1]
		}
		d.bringInto(vs[i], inner)
	}
}

// bringInto turns the wheel over the viewport until what it holds (inner)
// shows in it, or fills it when it is the taller, over a part of it that no
// viewport in it covers, as that one would take the turns.
func (d *desk) bringInto(v, inner *Element) {
	var inside []*Element
	visit(v, func(e *Element) bool {
		if !e.Same(v) && e.Role() == "viewport" {
			inside = append(inside, e)
		}
		return true
	})
	still := 0
	for range 120 {
		vx, vy, vw, vh := d.visiblePart(v)
		_, fy, _, fh := inner.Bounds()
		if vw <= 0 || vh <= 0 || fh <= 0 || still >= 3 {
			return
		}
		holds := fy >= vy-1 && fy+fh <= vy+vh+1
		covers := fy <= vy+1 && fy+fh >= vy+vh-1
		if holds || covers {
			return
		}
		lines := -1
		if fy < vy {
			lines = 1
		}
		px, py, ok := freeSpot(vx, vy, vw, vh, inside)
		if !ok {
			return
		}
		x, y := d.px(px, py)
		_ = uia.Scroll(x, y, lines)
		d.c.Pump(100 * time.Millisecond)
		if _, gy, _, _ := inner.Bounds(); gy == fy {
			still++
		} else {
			still = 0
		}
	}
}

// freeSpot is a point in the rectangle that none of the elements covers: its
// middle, or near one of its edges.
func freeSpot(x, y, w, h int, covers []*Element) (int, int, bool) {
	mx, my := x+w/2, y+h/2
	for _, p := range [][2]int{{mx, my}, {x + 6, my}, {x + w - 6, my}, {mx, y + 6}, {mx, y + h - 6}, {x + 6, y + 6}, {x + 6, y + h - 6}} {
		free := true
		for _, c := range covers {
			cx, cy, cw, ch := c.Bounds()
			if p[0] >= cx && p[0] < cx+cw && p[1] >= cy && p[1] < cy+ch {
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

// visibleCells are the cells of a table that show. The bridge gives a
// JTable's children, and its table API's cells, as the table's one cell
// renderer: one label holding whatever cell it drew last, with no place. Its
// visible children are the cells themselves, each with its text and place.
func (d *desk) visibleCells(table *Element) []*Element {
	var cells []*Element
	for _, c := range table.Visible() {
		if table.Same(c.Parent()) {
			cells = append(cells, c)
		}
	}
	return cells
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

// scrollTo turns the wheel over the window's lists and tables until the
// control shows in view, finding it anew after each turn.
func (d *desk) scrollTo(kind, name string) *Element {
	d.t.Helper()
	var boxes []*Element
	visit(d.root, func(e *Element) bool {
		if r := e.Role(); r == "table" || r == "list" {
			boxes = append(boxes, e)
		}
		return true
	})
	for _, box := range boxes {
		// The box first shows in the window (the page scrolls), then the
		// wheel turns over the middle of the part of it that shows.
		if p := box.Parent(); p != nil && p.Role() == "viewport" {
			d.reveal(p)
		}
		find := func() *Element {
			if kind == "row" && box.Role() == "table" {
				for _, c := range d.visibleCells(box) {
					if c.Name() == name && d.inPart(c, box) {
						return c
					}
				}
				return nil
			}
			if e := search(box, kind, name, false); e != nil && d.inView(e) {
				return e
			}
			return nil
		}
		for _, lines := range []int{-3, 3} {
			for range 40 {
				if e := find(); e != nil {
					return e
				}
				bx, by, bw, bh := d.visiblePart(box)
				if bw <= 0 || bh <= 0 {
					break
				}
				x, y := d.px(bx+bw/2, by+bh/2)
				_ = uia.Scroll(x, y, lines)
				d.c.Pump(150 * time.Millisecond)
			}
		}
	}
	if e := search(d.root, kind, name, false); e != nil {
		x, y, w, h := e.Bounds()
		var at []string
		cx, cy := center(e)
		for hit := d.root.At(cx, cy); hit != nil && len(at) < 5; hit = hit.Parent() {
			at = append(at, hit.Role()+" "+strconv.Quote(hit.Name()))
		}
		var vp string
		for p := e.Parent(); p != nil; p = p.Parent() {
			if p.Role() == "viewport" {
				vx, vy, vw, vh := p.Bounds()
				vp = fmt.Sprintf("%d,%d %dx%d", vx, vy, vw, vh)
				break
			}
		}
		d.t.Logf("the %q %s is at %d,%d %dx%d (%s); its viewport %s; at its middle %v; work area %+v; %.2f pixels a point",
			name, kind, x, y, w, h, e.States(), vp, at, uia.WorkArea(), d.scale)
	} else {
		d.t.Logf("no %q %s in the tree", name, kind)
	}
	d.t.Fatalf("scrolled every list and table, and found no %q %s in view", name, kind)
	return nil
}

func (d *desk) click(e *Element) {
	d.t.Helper()
	d.reveal(e)
	x, y := d.px(center(e))
	if err := uia.Click(x, y); err != nil {
		d.t.Fatal(err)
	}
	d.c.Pump(250 * time.Millisecond)
}

func (d *desk) key(spec string) {
	d.t.Helper()
	if err := uia.Key(spec); err != nil {
		d.t.Fatal(err)
	}
	d.c.Pump(250 * time.Millisecond)
}

func (d *desk) fill(field, text string) {
	d.t.Helper()
	for try := 0; try < 4; try++ {
		f := d.find("field", field)
		d.click(f)
		d.key("ControlOrMeta+a")
		if err := uia.Type(text); err != nil {
			d.t.Fatal(err)
		}
		for wait := time.Now(); time.Since(wait) < 2*time.Second; d.c.Pump(100 * time.Millisecond) {
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
	ex, ey, _, _ := e.Bounds()
	px, py := d.px(ex, ey)
	if err := uia.Click(px+int(float64(x)*d.scale), py+int(float64(y)*d.scale)); err != nil {
		d.t.Fatal(err)
	}
	d.c.Pump(250 * time.Millisecond)
}

func (d *desk) drag(e *Element, x1, y1, x2, y2 int) {
	d.t.Helper()
	ex, ey, _, _ := e.Bounds()
	px, py := d.px(ex, ey)
	s := d.scale
	if err := uia.Drag(px+int(float64(x1)*s), py+int(float64(y1)*s), px+int(float64(x2)*s), py+int(float64(y2)*s)); err != nil {
		d.t.Fatal(err)
	}
	d.c.Pump(250 * time.Millisecond)
}

func (d *desk) openMenu(menu, item string) *Element {
	d.t.Helper()
	m := d.find("menu", menu)
	for try := 0; try < 3; try++ {
		d.click(m)
		for wait := time.Now(); time.Since(wait) < 3*time.Second; d.c.Pump(150 * time.Millisecond) {
			if it := search(m, "menu item", item, true); it != nil {
				return it
			}
		}
		d.key("Escape")
	}
	d.t.Fatalf("clicked the %q menu 3 times, and its %q item did not show", menu, item)
	return nil
}

func (d *desk) menuItemEnabled(menu, item string, want bool) {
	d.t.Helper()
	it := d.openMenu(menu, item)
	got := it.Has("enabled")
	d.key("Escape")
	d.key("Escape")
	if got != want {
		d.t.Fatalf("the %q menu item is enabled: %v, not %v", item, got, want)
	}
}

func (d *desk) chooseMenuItem(menu, item string) {
	d.t.Helper()
	d.click(d.openMenu(menu, item))
	d.c.Pump(400 * time.Millisecond)
}
