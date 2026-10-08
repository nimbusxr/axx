//go:build darwin

package desktopmacos

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
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
	// before are the windows the system's app (owner: system) had as it was
	// watched: the person's, which the scenario neither reads nor sees.
	before []*ax.Element
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
	if p.before != nil {
		ws = slices.DeleteFunc(ws, func(w *ax.Element) bool { return slices.ContainsFunc(p.before, w.Equal) })
	}
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

// menuBars are the app's menu bar, its status items' (a tray app's icon on
// the menu bar's right, whose menu it opens), and its windows' own (Swing's).
func (p *proc) menuBars() []*ax.Element {
	var bars []*ax.Element
	for _, attr := range []string{"AXMenuBar", "AXExtrasMenuBar"} {
		if v, err := p.root.Attribute(attr); err == nil {
			if bar, ok := v.(*ax.Element); ok {
				bars = append(bars, bar)
			}
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
	case "menu item", "element":
		// An open menu's items; its menu, its title in the menu bar, is
		// found after them (titles).
		places := p.windows()
		for _, bar := range p.menuBars() {
			for _, item := range children(bar) {
				if item.Bool("AXSelected") {
					places = append(places, item)
				}
			}
		}
		// A status item does not say its menu is open: the one clicked is.
		if p.openMenu != nil && p.openMenu.String("AXSubrole") == "AXMenuExtra" {
			places = append(places, p.openMenu)
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
			return once(found) // the front window's, before the others'
		}
	}
	if kind == "element" {
		for _, t := range p.titles() {
			if named(t, name) {
				found = append(found, t)
			}
		}
	}
	return found
}

// once is the elements found, each once: the web engines give a table's
// cells under its rows and again under its columns.
func once(found []*ax.Element) []*ax.Element {
	var out []*ax.Element
	for _, e := range found {
		if !slices.ContainsFunc(out, e.Equal) {
			out = append(out, e)
		}
	}
	return out
}

// captionsOut leaves out the texts among elements found by a name when a
// control is among them too: a text that shows a control's name (a switch's
// caption) is not the element a step names.
func captionsOut(found []*ax.Element) []*ax.Element {
	var controls []*ax.Element
	for _, e := range found {
		if e.String("AXRole") != "AXStaticText" {
			controls = append(controls, e)
		}
	}
	if len(controls) == 0 {
		return found
	}
	return controls
}

// titles are the menus' titles in the menu bar, which is the app's on
// macOS, not its window's: elements too, but not the items of the menus
// they open while those are closed.
func (p *proc) titles() []*ax.Element {
	var out []*ax.Element
	for _, bar := range p.menuBars() {
		out = append(out, children(bar)...)
	}
	return out
}

func (p *proc) Find(k appcore.Kind, name string, shown bool) ([]desktopcore.Control, error) {
	var out []desktopcore.Control
	found := p.find(k.Noun, collapse(name), shown)
	if k.Noun == "element" {
		found = captionsOut(found)
	}
	for _, e := range found {
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
	if k.Noun == "element" {
		for _, t := range p.titles() {
			if n := name(t); n != "" && !slices.Contains(out, n) {
				out = append(out, n)
			}
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

// Away moves the pointer beside the app's window, in the screen's visible
// frame (not on the Dock).
func (p *proc) Away() error { return p.away(false) }

// Leave moves the pointer beside the app's window, when the screen has room.
func (p *proc) Leave() error { return p.away(true) }

func (p *proc) away(beside bool) error {
	w, ok := frameOf(p.window())
	if !ok {
		return nil
	}
	rect := func(a area) image.Rectangle { return image.Rect(int(a.x), int(a.y), int(a.x+a.w), int(a.y+a.h)) }
	at := desktopcore.AwaySpot(rect(w), rect(p.screen))
	if beside {
		if at, ok = desktopcore.Beside(rect(w), rect(p.screen)); !ok {
			return nil
		}
	}
	return ax.Move(ax.Point{X: float64(at.X), Y: float64(at.Y)})
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

// at is a point from the element's anchor.
func at(e *ax.Element, from desktopcore.Anchor, x, y float64) (ax.Point, error) {
	pos, size, err := e.Frame()
	if err != nil {
		return ax.Point{}, &desktopcore.Lost{Err: fmt.Errorf("the %s has no place on the screen: %w", e.String("AXRole"), err)}
	}
	px, py := from.Place(pos.X, pos.Y, size.Width, size.Height, x, y)
	return ax.Point{X: px, Y: py}, nil
}

// on is whether a step's place is on the element: an *desktopcore.Off when
// not.
func on(e *ax.Element, from desktopcore.Anchor, x, y float64) error {
	_, size, err := e.Frame()
	if err != nil {
		return &desktopcore.Lost{Err: fmt.Errorf("the %s has no place on the screen: %w", e.String("AXRole"), err)}
	}
	return from.On(size.Width, size.Height, x, y)
}

func (p *proc) ClickAt(c desktopcore.Control, from desktopcore.Anchor, x, y float64) error {
	e := c.(control).e
	p.menuClosed()
	if err := p.ready(e); err != nil {
		return err
	}
	if err := on(e, from, x, y); err != nil {
		return err
	}
	pt, err := at(e, from, x, y)
	if err != nil {
		return err
	}
	return ax.Click(pt)
}

func (p *proc) Drag(c desktopcore.Control, from desktopcore.Anchor, x1, y1, x2, y2 float64) error {
	e := c.(control).e
	p.menuClosed()
	if err := p.ready(e); err != nil {
		return err
	}
	if err := on(e, from, x1, y1); err != nil {
		return err
	}
	start, err := at(e, from, x1, y1)
	if err != nil {
		return err
	}
	end, err := at(e, from, x2, y2)
	if err != nil {
		return err
	}
	return ax.Drag(start, end)
}

// keyWait is how long a key waits for the app to show a window.
const keyWait = 5 * time.Second

// shown waits for the app to show a window, as a person waits to see an
// app before pressing a key in it: an app still opening takes keys before
// it is ready for them (Snap readies its overlay, then shows it, and sets
// its tool after a key chose one). An app that shows none in time (one in
// the menu bar) takes the key as it is.
func (p *proc) shown() {
	for wait := time.Now(); len(p.windows()) == 0; time.Sleep(50 * time.Millisecond) {
		if time.Since(wait) > keyWait {
			p.sc.Log("the %s app showed no window within %s: pressing the key as it is", p.app.Name, keyWait)
			return
		}
	}
}

func (p *proc) Key(spec string) error {
	p.shown()
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
	p.shown()
	if err := p.Front(); err != nil {
		return err
	}
	return ax.Type(text)
}

// Window captures the front window with screencapture, as the window server
// draws it, without its shadow.
func (p *proc) Window() (image.Image, float64, error) {
	b, w, err := p.capture()
	if err != nil {
		return nil, 0, err
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, 0, fmt.Errorf("the window's capture is not a PNG: %w", err)
	}
	scale := scaleOf(w, img.Bounds().Dx())
	if at, ok := p.caret(w, scale); ok {
		img = desktopcore.HideCaret(img, at)
	}
	return img, scale, nil
}

// Snapshot is the front window as screencapture writes it, cursor and all,
// for a trace: nothing decoded or encoded again.
func (p *proc) Snapshot() ([]byte, float64, error) {
	b, w, err := p.capture()
	if err != nil {
		return nil, 0, err
	}
	scale := 1.0
	if len(b) >= 24 && string(b[12:16]) == "IHDR" {
		scale = scaleOf(w, int(binary.BigEndian.Uint32(b[16:20])))
	}
	return b, scale, nil
}

// capture is the front window as screencapture writes it, a PNG, and the
// window.
func (p *proc) capture() ([]byte, *ax.Element, error) {
	w := p.window()
	id, err := w.WindowID()
	if err != nil {
		return nil, nil, err
	}
	if ok, err := ax.ScreenCaptureAllowed(); err == nil && !ok {
		return nil, nil, fmt.Errorf("macOS does not let axx capture other apps' windows: allow %s in System Settings > Privacy & Security > Screen Recording", topApp())
	}
	f, err := os.CreateTemp("", "axx-window-*.png")
	if err != nil {
		return nil, nil, err
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)
	if out, err := exec.CommandContext(p.sc.Context(), "screencapture", "-x", "-o", "-l", strconv.FormatUint(uint64(id), 10), path).CombinedOutput(); err != nil {
		return nil, nil, fmt.Errorf("screencapture: %w %s", err, out)
	}
	b, err := os.ReadFile(path)
	return b, w, err
}

// scaleOf is the scale of a capture of the window that is width pixels
// wide: whole pixels, 2.0 rather than 1.998.
func scaleOf(w *ax.Element, width int) float64 {
	fr, ok := frameOf(w)
	if !ok || fr.w <= 0 {
		return 1
	}
	return float64(int(float64(width)/fr.w*4+0.5)) / 4
}

// caret is where the text cursor of the app's focused field is in a capture
// of the window w at scale: a column a few points wide, a line high. Some
// fields (AppKit's, empty) say the line is above them: the cursor is in the
// field, its height.
func (p *proc) caret(w *ax.Element, scale float64) (image.Rectangle, bool) {
	v, err := p.root.Attribute("AXFocusedUIElement")
	f, _ := v.(*ax.Element)
	if err != nil || f == nil {
		return image.Rectangle{}, false
	}
	at, size, ok := f.Caret()
	field, ok2 := frameOf(f)
	win, ok3 := frameOf(w)
	if !ok || !ok2 || !ok3 {
		return image.Rectangle{}, false
	}
	top, bottom := at.Y, at.Y+size.Height
	if top < field.y || bottom > field.y+field.h {
		top, bottom = field.y, field.y+field.h
	}
	// A cursor taller than its line, within its field.
	pad := max(2, (bottom-top)/4)
	top, bottom = max(top-pad, field.y), min(bottom+pad, field.y+field.h)
	const half = 1.5 // points each side: the cursor, and its anti-aliasing
	px := func(v float64) int { return int(math.Round(v * scale)) }
	return image.Rect(px(at.X-half-win.x), px(top-win.y), px(at.X+half-win.x), px(bottom-win.y)), true
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
	for _, attr := range []string{"AXMenuBar", "AXExtrasMenuBar"} {
		if v, err := p.root.Attribute(attr); err == nil {
			if bar, ok := v.(*ax.Element); ok {
				n := add(bar, 1)
				// The Apple menu is the system's, and its Recent Items name
				// the machine's own apps and documents: kept out of outlines.
				if attr == "AXMenuBar" && len(n.Children) > 0 && n.Children[0].Name == "Apple" {
					n.Children[0].Children = nil
				}
				root.Children = append(root.Children, n)
			}
		}
	}
	return root, nil
}

func (p *proc) Stop() error  { return p.stop() }
func (p *proc) Exited() bool { return p.exited() }

func (p *proc) Describe() map[string]any {
	return map[string]any{"app": filepath.Base(p.app.App), "process": p.pid, "window": p.window().String("AXTitle")}
}
