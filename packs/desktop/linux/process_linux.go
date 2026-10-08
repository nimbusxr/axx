//go:build linux

package desktoplinux

import (
	"fmt"
	"image"
	"image/draw"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/atspi"
)

// proc is a Linux app as it runs on its desktop: the root of its tree on the
// scenario's accessibility bus.
type proc struct {
	sc  *core.Scenario
	app *desktopcore.App
	pid int
	in  seat
	// wayland is whether the app runs on a Wayland desktop.
	wayland bool
	// root is the app on the accessibility bus, from its first window on;
	// c is the bus, tray the desktop's tray, and trayOpen whether the menu of
	// the app's icon there is open.
	root     *atspi.Element
	c        *atspi.Client
	tray     *atspi.Tray
	trayOpen bool
	// drew is the window seen to show something.
	drew   *atspi.Element
	exited func() bool
	// gtk4 is whether the app is made with GTK 4, which hit-tests what its
	// areas clip away; flutter whether with Flutter, which gives no places
	// and says whether a control can be used by its offering a click; java
	// whether a Java app, whose bridge does not report what is typed into
	// a field.
	gtk4, flutter, java bool
	// named is the executable of the system's app the process is (owner:
	// system), found on the scenario's desktop when another app starts it;
	// watched is the desktop whose session it is read on, once there is one.
	named   string
	watched *desk
}

// control is a control the app has.
type control struct {
	e *atspi.Element
	p *proc
}

func (c control) Name() string { return name(c.e) }

func (c control) Enabled() (bool, bool) {
	if c.p.flutter {
		// Flutter says nothing of a control's state, and offers no action on
		// one that cannot be used.
		_, ok := clickAction(c.e.Actions())
		return ok, true
	}
	return c.e.Is(atspi.StateSensitive), true
}

func (c control) Value() (string, bool) {
	return c.e.Text(), !c.p.java
}

// top is the app on the accessibility bus, once it has a window: a tray app
// comes onto it with its first.
func (p *proc) top() *atspi.Element {
	if p.named != "" && p.c == nil && p.watched.s != nil {
		s := p.watched.s
		p.c, p.in, p.tray, p.wayland = s.c, s.seat, s.tray, s.d.wayland
	}
	if p.root != nil || p.c == nil {
		return p.root
	}
	if p.named != "" && p.pid == 0 {
		if _, pid, err := p.c.ApplicationNamed(p.named); err == nil {
			p.pid = pid
		}
		if p.pid == 0 {
			return nil
		}
	}
	root, err := p.c.ApplicationOf(p.pid)
	if err != nil {
		return nil
	}
	if kids, _ := root.Children(); len(kids) == 0 {
		return nil
	}
	p.root = root
	name, version := root.Toolkit()
	p.gtk4 = name == "GTK" && strings.HasPrefix(version, "4.")
	if !p.gtk4 {
		// Only GTK 4 knows places in its window only.
		root.WithScreenPlaces()
	}
	p.java = strings.Contains(strings.ToLower(name), "java") || strings.Contains(name, "J2SE")
	if atspi.IsFlutter(p.pid) {
		p.flutter = true
		root.WithoutPlaces()
	}
	return root
}

// windows are the app's windows, the active one first: its frames, dialogs
// and open menus.
func (p *proc) windows() []*atspi.Element {
	if p.top() == nil {
		return nil
	}
	kids, _ := p.root.Children()
	slices.SortStableFunc(kids, func(a, b *atspi.Element) int {
		const active = 1 // ATSPI_STATE_ACTIVE
		switch aa, ba := a.Is(active), b.Is(active); {
		case aa && !ba:
			return -1
		case ba && !aa:
			return 1
		}
		return 0
	})
	return kids
}

// window is the app's main window: its first frame. A tray app has none
// until it is used.
func (p *proc) window() *atspi.Element {
	ws := p.windows()
	for _, w := range ws {
		if r := w.Role(); r == "frame" || r == "window" {
			return w
		}
	}
	if len(ws) > 0 {
		return ws[0]
	}
	return p.root
}

func (p *proc) Find(k appcore.Kind, n string, shown bool) ([]desktopcore.Control, error) {
	var out []desktopcore.Control
	found := p.find(k.Noun, collapse(n), shown)
	if k.Noun == "element" {
		found = captionsOut(found)
	}
	for _, e := range found {
		out = append(out, control{e: e, p: p})
	}
	for _, c := range p.trayControls(k.Noun) {
		if collapse(c.Name()) == collapse(n) {
			out = append(out, c)
		}
	}
	return out, nil
}

// trayControl is the app's icon in the tray, or an entry of the menu the
// icon opens: chosen through the tray, as a click on them does.
type trayControl struct {
	item  atspi.TrayItem
	entry *atspi.TrayEntry // none for the icon
}

func (c trayControl) Name() string {
	if c.entry == nil {
		return c.item.Name()
	}
	return c.entry.Label
}

func (c trayControl) Enabled() (bool, bool) {
	if c.entry == nil {
		return true, true
	}
	return c.entry.Enabled, true
}

func (c trayControl) Value() (string, bool) { return "", false }

// trayControls are those of the app's tray icon of a kind: the icon is a
// menu (as a status item is on macOS), and its menu's entries, while it is
// open, menu items.
func (p *proc) trayControls(kind string) []desktopcore.Control {
	item, ok := p.tray.ItemOf(p.pid)
	if !ok {
		return nil
	}
	var out []desktopcore.Control
	if kind == "menu" || kind == "element" {
		out = append(out, trayControl{item: item})
	}
	if p.trayOpen && (kind == "menu item" || kind == "element") {
		entries, _ := item.Menu()
		for i := range entries {
			out = append(out, trayControl{item: item, entry: &entries[i]})
		}
	}
	return out
}

// captionsOut leaves out the labels among elements found by a name when a
// control is among them too: a label that shows a control's name (a
// switch's caption) is not the element a step names.
func captionsOut(found []*atspi.Element) []*atspi.Element {
	var controls []*atspi.Element
	for _, e := range found {
		if e.Role() != "label" {
			controls = append(controls, e)
		}
	}
	if len(controls) == 0 {
		return found
	}
	return controls
}

func (p *proc) Names(k appcore.Kind) []string {
	out := p.names(k.Noun)
	for _, c := range p.trayControls(k.Noun) {
		if n := c.Name(); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func (p *proc) Shows(text string) (bool, error) {
	text = collapse(text)
	for _, w := range p.windows() {
		if hasText(w, text) {
			return true, nil
		}
	}
	return false, nil
}

func (p *proc) Texts() []string {
	var out []string
	for _, w := range p.windows() {
		visit(w, func(e *atspi.Element) bool {
			if shown(e) {
				for _, t := range []string{name(e), collapse(e.Text())} {
					if t != "" && !slices.Contains(out, t) {
						out = append(out, t)
					}
				}
			}
			return len(out) < 200
		})
	}
	return out
}

// Front brings the app's top window to the front, with the keyboard, as a
// click on it does: the desktop's window manager opens a window an app shows
// on its own behind the one in use. A tray app with no window stays as it is.
func (p *proc) Front() error {
	if p.exited() {
		return fmt.Errorf("the %s app has stopped", p.app.Name)
	}
	if p.window() == nil {
		return nil
	}
	return p.in.Activate(p.pid)
}

func (p *proc) ScrollTo(k appcore.Kind, n string) (desktopcore.Control, error) {
	if e := p.scrollTo(k.Noun, collapse(n)); e != nil {
		return control{e: e, p: p}, nil
	}
	return nil, nil
}

func (p *proc) ScrollIntoView(c desktopcore.Control) error {
	if _, ok := c.(trayControl); ok {
		return nil
	}
	e := c.(control).e
	if !e.HasPlaces() {
		return nil
	}
	return p.scrollIntoView(e)
}

func (p *proc) Click(c desktopcore.Control) error {
	if t, ok := c.(trayControl); ok {
		if t.entry == nil {
			// The icon opens its menu.
			if _, err := t.item.Menu(); err != nil {
				return err
			}
			p.trayOpen = true
			return nil
		}
		p.trayOpen = false
		return t.item.Choose(t.entry.ID)
	}
	if err := p.Front(); err != nil {
		return err
	}
	e := c.(control).e
	if _, _, ok := center(e); !ok || !e.HasPlaces() {
		return p.act(e)
	}
	if r := e.Role(); strings.Contains(r, "menu item") {
		// An open menu's item never lies on the menu bar's menu that opened
		// it: one that says it does gives its place in its popup, not on the
		// screen (Java's), and is chosen through accessibility. On Wayland
		// every menu's popup is a window of its own, where the app does not
		// say (GTK 3).
		for at := e.Parent(); at != nil; at = at.Parent() {
			if pr := at.Parent(); pr != nil && pr.Role() == "menu bar" {
				if p.wayland || within(e, at.Extents()) {
					p.sc.Log("the %q menu item gives no place on the screen: chosen through accessibility", name(e))
					return activate(e)
				}
				break
			}
			if r := at.Role(); r == "menu bar" || r == "frame" {
				break
			}
		}
	}
	if err := p.scrollIntoView(e); err != nil {
		return err
	}
	settled(e)
	opens := p.is("menu", e, e.Parent())
	// A menu that does not open takes another click: a menu bar just used
	// takes the first for closing (Electron's; Swing's after a choice).
	for try := 0; ; try++ {
		// One with no place for a moment (a toolbar coming back as a drag
		// ends, a web view laying out) is waited for, as a person waits to
		// see it again.
		x, y, ok := center(e)
		for wait := time.Now(); !ok && time.Since(wait) < 2*time.Second; time.Sleep(100 * time.Millisecond) {
			x, y, ok = center(e)
		}
		if !ok {
			err := fmt.Errorf("the %s %q lost its place on the screen as it scrolled", e.Role(), name(e))
			if try == 0 {
				return &desktopcore.Lost{Err: err}
			}
			return err
		}
		if err := p.in.Click(x, y); err != nil {
			return err
		}
		if err := p.in.FocusUnderPointer(); err != nil {
			return err
		}
		time.Sleep(afterInput)
		if !opens || try == 2 || p.menuOpen(e) {
			return nil
		}
		p.sc.Log("the %q menu did not open: clicking it again", name(e))
	}
}

// menuOpen is whether the menu shows an item within a second and a half.
func (p *proc) menuOpen(menu *atspi.Element) bool {
	for wait := time.Now(); time.Since(wait) < 1500*time.Millisecond; time.Sleep(150 * time.Millisecond) {
		for _, in := range append([]*atspi.Element{menu}, p.windows()...) {
			found := false
			visit(in, func(x *atspi.Element) bool {
				found = found || strings.Contains(x.Role(), "menu item") && x.Parent() != nil && x.Parent().Role() != "menu bar" && shown(x)
				return !found
			})
			if found {
				return true
			}
		}
	}
	return false
}

// within is whether the element's middle is in r.
func within(e *atspi.Element, r atspi.Rect) bool {
	x, y, ok := center(e)
	return ok && x >= int(r.X) && x < int(r.X+r.Width) && y >= int(r.Y) && y < int(r.Y+r.Height)
}

// act clicks a control with no place on the screen (Flutter's) through its
// own action: a field takes the focus (its Focus action, else its Tap; it
// offers neither while it has the focus), and the keys then go to its
// window, under the pointer.
func (p *proc) act(e *atspi.Element) error {
	p.sc.Log("the %s %q has no place on the screen: it takes its own action", e.Role(), name(e))
	if p.is("field", e, nil) {
		acts := e.Actions()
		for _, a := range []string{"Focus", "Tap"} {
			if slices.Contains(acts, a) {
				if err := e.Do(a); err != nil {
					return err
				}
				break
			}
		}
		_ = p.in.Move(40, 40)
		return p.in.FocusUnderPointer()
	}
	return activate(e)
}

// origin is the control's top left on the screen, once it is in view.
// place is where on the screen a point from the element's anchor is, with
// the element in view, once the step's place (x, y from from) is seen to be
// on it.
func (p *proc) place(e *atspi.Element, from desktopcore.Anchor, x, y float64) (func(from desktopcore.Anchor, x, y float64) (int, int), error) {
	if !e.HasPlaces() {
		return nil, fmt.Errorf("the %s app's toolkit gives no places on the screen (Flutter, on Linux): nothing can be clicked at a place on it", p.app.Name)
	}
	if err := p.Front(); err != nil {
		return nil, err
	}
	p.drawn()
	if err := p.scrollIntoView(e); err != nil {
		return nil, err
	}
	settled(e)
	r := e.Extents()
	if !placed(r) {
		return nil, &desktopcore.Lost{Err: fmt.Errorf("the %s %q has no place on the screen", e.Role(), name(e))}
	}
	if err := from.On(float64(r.Width), float64(r.Height), x, y); err != nil {
		return nil, err
	}
	return func(from desktopcore.Anchor, x, y float64) (int, int) {
		px, py := from.Place(float64(r.X), float64(r.Y), float64(r.Width), float64(r.Height), x, y)
		return int(px), int(py)
	}, nil
}

// drawWait is how long a window may show nothing before a place on it is
// clicked.
const drawWait = 5 * time.Second

// drawn waits for the app's window to show something, as a person does
// before clicking a place on it: a window of one color all over (a web
// view's, until its first frame on a slow desktop) has drawn nothing yet.
func (p *proc) drawn() {
	w := p.window()
	if w == nil || p.drew != nil && p.drew.Same(w) {
		return
	}
	for wait := time.Now(); ; time.Sleep(200 * time.Millisecond) {
		shot, _, err := p.Window()
		if err != nil || !flat(shot) {
			p.drew = w
			return
		}
		if time.Since(wait) > drawWait {
			p.sc.Log("the %s app's window showed one color for %s: clicking at a place on it as it is", p.app.Name, drawWait)
			p.drew = w
			return
		}
	}
}

// flat is whether an image is one color all over, near enough.
func flat(img image.Image) bool {
	b := img.Bounds()
	if b.Empty() {
		return true
	}
	r0, g0, b0, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += max(1, b.Dy()/32) {
		for x := b.Min.X; x < b.Max.X; x += max(1, b.Dx()/32) {
			r, g, bl, _ := img.At(x, y).RGBA()
			if diff(r, r0) > 0x0800 || diff(g, g0) > 0x0800 || diff(bl, b0) > 0x0800 {
				return false
			}
		}
	}
	return true
}

func diff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// noPlace is the error of a click at a place on what has none on the screen.
func noPlace(c desktopcore.Control) error {
	return fmt.Errorf("%q is in the tray, which has no places on the screen: click it instead", c.Name())
}

// afterInput is how long the app has to take a click, a drag or a key
// before the next step: a web view moved away from at once keeps its hover.
const afterInput = 200 * time.Millisecond

func (p *proc) ClickAt(c desktopcore.Control, from desktopcore.Anchor, x, y float64) error {
	if _, ok := c.(trayControl); ok {
		return noPlace(c)
	}
	at, err := p.place(c.(control).e, from, x, y)
	if err != nil {
		return err
	}
	if err := p.in.Click(at(from, x, y)); err != nil {
		return err
	}
	time.Sleep(afterInput)
	return nil
}

func (p *proc) Drag(c desktopcore.Control, from desktopcore.Anchor, x1, y1, x2, y2 float64) error {
	if _, ok := c.(trayControl); ok {
		return noPlace(c)
	}
	at, err := p.place(c.(control).e, from, x1, y1)
	if err != nil {
		return err
	}
	sx, sy := at(from, x1, y1)
	ex, ey := at(from, x2, y2)
	if err := p.in.Drag(sx, sy, ex, ey); err != nil {
		return err
	}
	time.Sleep(afterInput)
	return nil
}

// ready is whether the app is on the scenario's desktop to take input: the
// system's app is, once another app starts it there.
func (p *proc) ready() error {
	if p.in == nil {
		p.top()
	}
	if p.in == nil {
		return fmt.Errorf("the %s app is not on the scenario's desktop yet", p.app.Name)
	}
	return nil
}

func (p *proc) Key(spec string) error {
	if err := p.ready(); err != nil {
		return err
	}
	if err := p.in.Key(spec); err != nil {
		return err
	}
	time.Sleep(afterInput)
	return nil
}

// Type types the text a moment after a click: Java's keys go to the field
// a little after it takes the focus.
func (p *proc) Type(text string) error {
	if err := p.ready(); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	return p.in.Type(text)
}

// Away moves the pointer beside the app's window, on the scenario's screen.
func (p *proc) Away() error { return p.away(false) }

// Leave moves the pointer beside the app's window, when the screen has room.
func (p *proc) Leave() error { return p.away(true) }

func (p *proc) away(beside bool) error {
	if err := p.ready(); err != nil {
		return err
	}
	win := p.window()
	if win == nil {
		return nil // no window to move away from
	}
	r := win.Extents()
	if !placed(r) {
		return nil // no places (Flutter): no pointer either
	}
	w, h := p.in.Size()
	window, screen := image.Rect(int(r.X), int(r.Y), int(r.X+r.Width), int(r.Y+r.Height)), image.Rect(0, 0, w, h)
	at := desktopcore.AwaySpot(window, screen)
	if beside {
		var ok bool
		if at, ok = desktopcore.Beside(window, screen); !ok {
			return nil
		}
	}
	return p.in.Move(at.X, at.Y)
}

// Window is the app's main window as the screen shows it: the desktop is
// the scenario's, so nothing covers it.
func (p *proc) Window() (image.Image, float64, error) {
	if err := p.ready(); err != nil {
		return nil, 0, err
	}
	shot, err := p.in.Screenshot()
	if err != nil {
		return nil, 0, err
	}
	w := p.window()
	if w == nil {
		return shot, 1, nil // a tray app with no window: the screen
	}
	r := w.Extents()
	// The window as the window manager has it, its own title bar too: the
	// one place for every toolkit's window, Flutter's and Java's too.
	if x, ok := p.in.Window(p.pid); ok {
		r = atspi.Rect{X: int32(x.Min.X), Y: int32(x.Min.Y), Width: int32(x.Dx()), Height: int32(x.Dy())}
	} else if !placed(r) || p.flutter {
		if at, ok := p.caret(); ok {
			return desktopcore.HideCaret(shot, at), 1, nil
		}
		return shot, 1, nil
	}
	rect := image.Rect(int(r.X), int(r.Y), int(r.X+r.Width), int(r.Y+r.Height)).Intersect(shot.Bounds())
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), shot, rect.Min, draw.Src)
	if at, ok := p.caret(); ok {
		return desktopcore.HideCaret(out, at.Sub(rect.Min)), 1, nil
	}
	return out, 1, nil
}

// caret is where the text cursor of the window's focused text is on the
// screen: a column a few pixels wide, a line high. A line said to be outside
// its text is the text's height: the cursor is in it.
func (p *proc) caret() (image.Rectangle, bool) {
	// The innermost element that has the focus: a web view has it, and the
	// field in its page.
	var f *atspi.Element
	n := 0
	var walk func(e *atspi.Element) bool
	walk = func(e *atspi.Element) bool {
		if n++; n > 2000 {
			return false
		}
		focused := e.Is(atspi.StateFocused)
		if focused {
			f = e
		}
		kids, _ := e.Children()
		for _, k := range kids {
			if walk(k) {
				return true
			}
		}
		return focused
	}
	if w := p.window(); w != nil {
		walk(w)
	}
	if f == nil {
		return image.Rectangle{}, false
	}
	c, ok := f.Caret()
	if !ok {
		return image.Rectangle{}, false
	}
	top, bottom := c.Y, c.Y+c.Height
	t := f.Extents()
	if placed(t) && (top < t.Y || bottom > t.Y+t.Height) {
		top, bottom = t.Y, t.Y+t.Height
	}
	// A cursor taller than its line, within its field (its ends smoothed a
	// pixel or two past what the field says it is: Qt 6's).
	pad := max(4, (bottom-top)/3)
	top, bottom = top-pad, bottom+pad
	if placed(t) {
		top, bottom = max(top, t.Y-2), min(bottom, t.Y+t.Height+2)
	}
	// The cursor and its anti-aliasing (Qt 6's reaches 2 pixels left); to
	// the right, more: Qt 5 says a character ends a few pixels before it
	// draws the cursor.
	const left, right = 2, 6
	return image.Rect(int(c.X-left), int(top), int(c.X+right), int(bottom)), true
}

func (p *proc) Tree() (*desktopcore.Node, error) {
	count := 0
	var add func(e *atspi.Element, depth int) *desktopcore.Node
	add = func(e *atspi.Element, depth int) *desktopcore.Node {
		count++
		c := control{e: e, p: p}
		n := &desktopcore.Node{Role: e.Role(), Name: name(e), ID: e.Attributes()["id"], Attrs: map[string]string{}, Control: c}
		if p.is("field", e, nil) {
			n.Value = e.Text()
		}
		if on, ok := c.Enabled(); ok {
			n.Attrs["enabled"] = fmt.Sprint(on)
		}
		if depth > 60 || count > 5000 {
			return n
		}
		kids, _ := e.Children()
		for _, k := range kids {
			n.Children = append(n.Children, add(k, depth+1))
		}
		return n
	}
	var root *desktopcore.Node
	if p.top() != nil {
		root = add(p.root, 0)
	} else {
		root = &desktopcore.Node{Role: "application", Name: p.app.Name, Attrs: map[string]string{}}
	}
	// The app's tray icon, and its menu while it is open.
	if item, ok := p.tray.ItemOf(p.pid); ok {
		icon := &desktopcore.Node{Role: "tray icon", Name: item.Name(), Attrs: map[string]string{}, Control: trayControl{item: item}}
		if p.trayOpen {
			entries, _ := item.Menu()
			for i := range entries {
				icon.Children = append(icon.Children, &desktopcore.Node{
					Role: "menu item", Name: entries[i].Label,
					Attrs: map[string]string{"enabled": fmt.Sprint(entries[i].Enabled)}, Control: trayControl{item: item, entry: &entries[i]},
				})
			}
		}
		root.Children = append(root.Children, icon)
	}
	return root, nil
}

// Stop asks the app's process group to quit, and stops it when it has not
// within 10 seconds: an app's helpers outlive it (Electron's renderers).
func (p *proc) Stop() error {
	if p.named != "" {
		return nil // the system's app: its windows go with the scenario's session
	}
	_ = syscall.Kill(-p.pid, syscall.SIGTERM)
	for wait := time.Now(); !p.exited() && time.Since(wait) < 10*time.Second; time.Sleep(100 * time.Millisecond) {
	}
	_ = syscall.Kill(-p.pid, syscall.SIGKILL)
	for wait := time.Now(); !p.exited() && time.Since(wait) < 5*time.Second; time.Sleep(100 * time.Millisecond) {
	}
	if !p.exited() {
		return fmt.Errorf("process %d did not stop", p.pid)
	}
	return nil
}

func (p *proc) Exited() bool { return p.exited() }

func (p *proc) Describe() map[string]any {
	if p.top() == nil {
		return map[string]any{"app": filepath.Base(p.app.App), "process": p.pid, "window": "none: in the tray"}
	}
	name, version := p.root.Toolkit()
	return map[string]any{"app": filepath.Base(p.app.App), "process": p.pid, "toolkit": strings.TrimSpace(name + " " + version)}
}
