//go:build darwin

package desktopmacos

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/ax"
)

// proc is a macOS app as it runs: the root of its accessibility tree.
type proc struct {
	sc     *core.Scenario
	app    *desktopcore.App
	pid    int
	root   *ax.Element
	screen area // the visible frame of the window's screen
	// openMenu is the menu a click opened: the next click or key waits for
	// it to close, as a click while it closes goes nowhere.
	openMenu *ax.Element
	stop     func() error
	exited   func() bool
	// flutter is whether the app is a Flutter app, which says every control
	// is enabled, a disabled one too.
	flutter bool
}

// control is an element a step found.
type control struct {
	e *ax.Element
	// enabledUnknown is whether the app's toolkit says nothing true of the
	// control's enabled state.
	enabledUnknown bool
}

// control is a control a step found. Flutter's own controls say nothing true
// of their enabled state; its menus are AppKit's, which do.
func (p *proc) control(e *ax.Element) control {
	r := e.String("AXRole")
	return control{e: e, enabledUnknown: p.flutter && r != "AXMenuItem" && r != "AXMenuBarItem" && r != "AXMenu"}
}

func (c control) Name() string { return name(c.e) }

func (c control) Enabled() (bool, bool) {
	v, err := c.e.Attribute("AXEnabled")
	b, ok := v.(bool)
	return b, err == nil && ok && !c.enabledUnknown
}

func (c control) Value() (string, bool) {
	v, err := c.e.Attribute("AXValue")
	if err != nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// windows are the app's windows, the focused one first.
func (p *proc) windows() []*ax.Element {
	ws, _ := p.root.Elements("AXWindows")
	if v, err := p.root.Attribute("AXFocusedWindow"); err == nil {
		if f, ok := v.(*ax.Element); ok {
			ws = slices.DeleteFunc(ws, func(w *ax.Element) bool { return w.Equal(f) })
			ws = append([]*ax.Element{f}, ws...)
		}
	}
	return ws
}

// window is the app's front window.
func (p *proc) window() *ax.Element {
	if ws := p.windows(); len(ws) > 0 {
		return ws[0]
	}
	return p.root
}

// menuBars are the app's menu bar, and its windows' own (Swing's).
func (p *proc) menuBars() []*ax.Element {
	var bars []*ax.Element
	if v, err := p.root.Attribute("AXMenuBar"); err == nil {
		if bar, ok := v.(*ax.Element); ok {
			bars = append(bars, bar)
		}
	}
	for _, w := range p.windows() {
		ax.Walk(w, func(e *ax.Element) bool {
			if e.String("AXRole") == "AXMenuBar" {
				bars = append(bars, e)
				return false
			}
			return true
		})
	}
	return bars
}

// places are where controls of the kind are looked for: the windows for
// most; the menu bars for a menu; for a menu item, the windows (a pop-up
// menu) and the menus open in the menu bars.
func (p *proc) places(kind string) []*ax.Element {
	switch kind {
	case "menu":
		return p.menuBars()
	case "menu item":
		places := p.windows()
		for _, bar := range p.menuBars() {
			for _, item := range children(bar) {
				if item.Bool("AXSelected") {
					places = append(places, item)
				}
			}
		}
		return places
	}
	return p.windows()
}

func (p *proc) find(kind, name string, onlyShown bool) []*ax.Element {
	var found []*ax.Element
	for _, at := range p.places(kind) {
		search(at, nil, kind, name, onlyShown, &found)
		if len(found) > 0 {
			return found // the front window's, before the others'
		}
	}
	return found
}

func (p *proc) Find(k appcore.Kind, name string, shown bool) ([]desktopcore.Control, error) {
	var out []desktopcore.Control
	for _, e := range p.find(k.Noun, collapse(name), shown) {
		out = append(out, p.control(e))
	}
	return out, nil
}

func (p *proc) Names(k appcore.Kind) []string {
	var out []string
	for _, at := range p.places(k.Noun) {
		each(at, nil, k.Noun, func(e *ax.Element) {
			if n := label(k.Noun, e); n != "" && !slices.Contains(out, n) {
				out = append(out, n)
			}
		})
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
		ax.Walk(w, func(e *ax.Element) bool {
			if !shown(e) {
				return true
			}
			for _, t := range []string{name(e), collapse(e.String("AXValue"))} {
				if t != "" && !slices.Contains(out, t) {
					out = append(out, t)
				}
			}
			return len(out) < 200
		})
	}
	return out
}

// Front brings the app to the front and makes sure it has the keyboard:
// keys go to the app in front, and must never go to another.
func (p *proc) Front() error {
	_ = p.root.SetBool("AXFrontmost", true)
	_ = p.window().Perform("AXRaise")
	for wait := time.Now(); time.Since(wait) < 3*time.Second; time.Sleep(50 * time.Millisecond) {
		if p.root.Bool("AXFrontmost") {
			return nil
		}
	}
	return fmt.Errorf("the %s app (process %d) does not come to the front: stopping before a click or a key reaches another app", p.app.Name, p.pid)
}

func (p *proc) ScrollTo(k appcore.Kind, name string) (desktopcore.Control, error) {
	e, err := p.scrollTo(k.Noun, collapse(name))
	if err != nil || e == nil {
		return nil, err
	}
	return p.control(e), nil
}

func (p *proc) ScrollIntoView(c desktopcore.Control) error {
	return p.scrollIntoView(c.(control).e)
}

// menuClosed waits for the menu a click opened to close. Swing's menu bar
// item stays selected, so it waits 2 seconds at most.
func (p *proc) menuClosed() {
	if p.openMenu == nil {
		return
	}
	for wait := time.Now(); time.Since(wait) < 2*time.Second && p.openMenu.Bool("AXSelected"); time.Sleep(100 * time.Millisecond) {
	}
	time.Sleep(200 * time.Millisecond)
	p.openMenu = nil
}

// ready brings the app to the front and the control into view, where it
// stays still.
func (p *proc) ready(e *ax.Element) error {
	if err := p.Front(); err != nil {
		return err
	}
	if err := p.scrollIntoView(e); err != nil {
		return err
	}
	settled(e)
	return nil
}

func (p *proc) Click(c desktopcore.Control) error {
	e := c.(control).e
	role := e.String("AXRole")
	if role != "AXMenuItem" {
		p.menuClosed()
	}
	if err := p.ready(e); err != nil {
		return err
	}
	at, err := e.Center()
	if err != nil {
		p.sc.Log("the %s has no place on the screen: pressed through accessibility (%v)", role, err)
		if err := e.Perform("AXPress"); err != nil {
			return fmt.Errorf("cannot press the %s: %w", role, err)
		}
	} else if err := ax.Click(at); err != nil {
		return err
	}
	switch role {
	case "AXMenuBarItem":
		p.openMenu = e
	case "AXMenuItem":
		p.menuClosed()
	}
	time.Sleep(150 * time.Millisecond)
	return nil
}

// at is a point from the element's top left.
func at(e *ax.Element, x, y float64) (ax.Point, error) {
	pos, _, err := e.Frame()
	if err != nil {
		return ax.Point{}, fmt.Errorf("the %s has no place on the screen: %w", e.String("AXRole"), err)
	}
	return ax.Point{X: pos.X + x, Y: pos.Y + y}, nil
}

func (p *proc) ClickAt(c desktopcore.Control, x, y float64) error {
	e := c.(control).e
	p.menuClosed()
	if err := p.ready(e); err != nil {
		return err
	}
	pt, err := at(e, x, y)
	if err != nil {
		return err
	}
	return ax.Click(pt)
}

func (p *proc) Drag(c desktopcore.Control, x1, y1, x2, y2 float64) error {
	e := c.(control).e
	p.menuClosed()
	if err := p.ready(e); err != nil {
		return err
	}
	from, err := at(e, x1, y1)
	if err != nil {
		return err
	}
	to, err := at(e, x2, y2)
	if err != nil {
		return err
	}
	return ax.Drag(from, to)
}

func (p *proc) Key(spec string) error {
	if err := p.Front(); err != nil {
		return err
	}
	if err := ax.PressKey(spec); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	p.menuClosed()
	return nil
}

func (p *proc) Type(text string) error {
	if err := p.Front(); err != nil {
		return err
	}
	return ax.Type(text)
}

// Window captures the front window with screencapture, as the window server
// draws it, without its shadow.
func (p *proc) Window() (image.Image, float64, error) {
	w := p.window()
	id, err := w.WindowID()
	if err != nil {
		return nil, 0, err
	}
	if ok, err := ax.ScreenCaptureAllowed(); err == nil && !ok {
		return nil, 0, fmt.Errorf("macOS does not let axx capture other apps' windows: allow %s in System Settings > Privacy & Security > Screen Recording", topApp())
	}
	f, err := os.CreateTemp("", "axx-window-*.png")
	if err != nil {
		return nil, 0, err
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)
	if out, err := exec.CommandContext(p.sc.Context(), "screencapture", "-x", "-o", "-l", strconv.FormatUint(uint64(id), 10), path).CombinedOutput(); err != nil {
		return nil, 0, fmt.Errorf("screencapture: %w %s", err, out)
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()
	img, err := png.Decode(r)
	if err != nil {
		return nil, 0, fmt.Errorf("the window's capture is not a PNG: %w", err)
	}
	scale := 1.0
	if fr, ok := frameOf(w); ok && fr.w > 0 {
		scale = float64(img.Bounds().Dx()) / fr.w
		// The capture is whole pixels: 2.0, not 1.998.
		scale = float64(int(scale*4+0.5)) / 4
	}
	return img, scale, nil
}

// Tree is the app's windows and menu bar as nodes: role, name, identifier
// and value, and the subrole and enabled state.
func (p *proc) Tree() (*desktopcore.Node, error) {
	root := &desktopcore.Node{Role: "AXApplication", Name: name(p.root)}
	count := 0
	var add func(e *ax.Element, depth int) *desktopcore.Node
	add = func(e *ax.Element, depth int) *desktopcore.Node {
		count++
		c := p.control(e)
		n := &desktopcore.Node{Role: e.String("AXRole"), Name: name(e), Attrs: map[string]string{}, Control: c}
		n.ID = e.String("AXIdentifier")
		if n.ID == "" {
			n.ID = e.String("AXDOMIdentifier")
		}
		if v, ok := c.Value(); ok {
			n.Value = v
		}
		if sub := e.String("AXSubrole"); sub != "" {
			n.Attrs["subrole"] = sub
		}
		if on, ok := c.Enabled(); ok {
			n.Attrs["enabled"] = strconv.FormatBool(on)
		}
		if depth > 60 || count > 5000 {
			return n
		}
		for _, k := range children(e) {
			n.Children = append(n.Children, add(k, depth+1))
		}
		return n
	}
	for _, w := range p.windows() {
		root.Children = append(root.Children, add(w, 1))
	}
	if v, err := p.root.Attribute("AXMenuBar"); err == nil {
		if bar, ok := v.(*ax.Element); ok {
			root.Children = append(root.Children, add(bar, 1))
		}
	}
	return root, nil
}

func (p *proc) Stop() error  { return p.stop() }
func (p *proc) Exited() bool { return p.exited() }

func (p *proc) Describe() map[string]any {
	return map[string]any{"app": filepath.Base(p.app.App), "process": p.pid, "window": p.window().String("AXTitle")}
}
