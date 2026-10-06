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
	root    *atspi.Element
	exited  func() bool
	// gtk4 is whether the app is made with GTK 4, which hit-tests what its
	// areas clip away; flutter whether with Flutter, which gives no places
	// and says whether a control can be used by its offering a click; java
	// whether a Java app, whose bridge does not report what is typed into
	// a field.
	gtk4, flutter, java bool
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

// windows are the app's windows, the active one first: its frames, dialogs
// and open menus.
func (p *proc) windows() []*atspi.Element {
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

// window is the app's main window: its first frame.
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
	return out, nil
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

func (p *proc) Names(k appcore.Kind) []string { return p.names(k.Noun) }

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

// Front is nothing to do: the desktop is the scenario's, and its windows
// have no window manager to stack them. Keys go to the window under the
// pointer, which a click leaves there.
func (p *proc) Front() error {
	if p.exited() {
		return fmt.Errorf("the %s app has stopped", p.app.Name)
	}
	return nil
}

func (p *proc) ScrollTo(k appcore.Kind, n string) (desktopcore.Control, error) {
	if e := p.scrollTo(k.Noun, collapse(n)); e != nil {
		return control{e: e, p: p}, nil
	}
	return nil, nil
}

func (p *proc) ScrollIntoView(c desktopcore.Control) error {
	e := c.(control).e
	if !e.HasPlaces() {
		return nil
	}
	return p.scrollIntoView(e)
}

func (p *proc) Click(c desktopcore.Control) error {
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
		x, y, ok := center(e)
		if !ok {
			return fmt.Errorf("the %s %q lost its place on the screen as it scrolled", e.Role(), name(e))
		}
		if err := p.in.Click(x, y); err != nil {
			return err
		}
		if err := p.in.FocusUnderPointer(); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
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
func (p *proc) origin(e *atspi.Element) (int, int, error) {
	if !e.HasPlaces() {
		return 0, 0, fmt.Errorf("the %s app's toolkit gives no places on the screen (Flutter, on Linux): nothing can be clicked at a place on it", p.app.Name)
	}
	if err := p.scrollIntoView(e); err != nil {
		return 0, 0, err
	}
	settled(e)
	r := e.Extents()
	if !placed(r) {
		return 0, 0, fmt.Errorf("the %s %q has no place on the screen", e.Role(), name(e))
	}
	return int(r.X), int(r.Y), nil
}

func (p *proc) ClickAt(c desktopcore.Control, x, y float64) error {
	ox, oy, err := p.origin(c.(control).e)
	if err != nil {
		return err
	}
	return p.in.Click(ox+int(x), oy+int(y))
}

func (p *proc) Drag(c desktopcore.Control, x1, y1, x2, y2 float64) error {
	ox, oy, err := p.origin(c.(control).e)
	if err != nil {
		return err
	}
	return p.in.Drag(ox+int(x1), oy+int(y1), ox+int(x2), oy+int(y2))
}

func (p *proc) Key(spec string) error {
	if err := p.in.Key(spec); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)
	return nil
}

// Type types the text a moment after a click: Java's keys go to the field
// a little after it takes the focus.
func (p *proc) Type(text string) error {
	time.Sleep(300 * time.Millisecond)
	return p.in.Type(text)
}

// Away moves the pointer beside the app's window, on the scenario's screen.
func (p *proc) Away() error {
	r := p.window().Extents()
	if !placed(r) {
		return nil // no places (Flutter): no pointer either
	}
	w, h := p.in.Size()
	at := desktopcore.AwaySpot(image.Rect(int(r.X), int(r.Y), int(r.X+r.Width), int(r.Y+r.Height)), image.Rect(0, 0, w, h))
	return p.in.Move(at.X, at.Y)
}

// Window is the app's main window as the screen shows it: the desktop is
// the scenario's, so nothing covers it.
func (p *proc) Window() (image.Image, float64, error) {
	shot, err := p.in.Screenshot()
	if err != nil {
		return nil, 0, err
	}
	r := p.window().Extents()
	if !placed(r) || p.flutter {
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
	walk(p.window())
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
	return add(p.root, 0), nil
}

// Stop asks the app's process group to quit, and stops it when it has not
// within 10 seconds: an app's helpers outlive it (Electron's renderers).
func (p *proc) Stop() error {
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
	name, version := p.root.Toolkit()
	return map[string]any{"app": filepath.Base(p.app.App), "process": p.pid, "toolkit": strings.TrimSpace(name + " " + version)}
}
