//go:build windows

package desktopwindows

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"path/filepath"
	"slices"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/jab"
	"github.com/nimbusxr/axx/packs/desktop/internal/uia"
)

// proc is a Windows app as it runs: its windows read through UI
// Automation, or a Java window through the Java Access Bridge.
type proc struct {
	sc     *core.Scenario
	app    *desktopcore.App
	w      *worker
	pid    int
	exited func() bool
	stop   func() error
	// flutter is whether the app is a Flutter app: its engine serves MSAA
	// only, says every control is enabled, and its places never follow a
	// scroll (flutter/flutter#189124).
	flutter bool
	u       *uiaTree
	java    *javaTree
}

// pids are the app's process and those it started.
func (p *proc) pids() []int { return append([]int{p.pid}, uia.Descendants(p.pid)...) }

// windows are the app's top-level windows, the one in front first, its
// open menus among them. On the worker's thread.
func (p *proc) windows() []*uia.Element {
	var ws []*uia.Element
	for _, pid := range p.pids() {
		mine, _ := p.w.uia.WindowsOf(pid)
		for _, w := range mine {
			if b := w.Bounds(); b.Right > b.Left && b.Bottom > b.Top {
				ws = append(ws, w)
			}
		}
	}
	front := uia.ForegroundProcess()
	slices.SortStableFunc(ws, func(a, b *uia.Element) int {
		af, bf := a.ProcessID() == front, b.ProcessID() == front
		switch {
		case af && !bf:
			return -1
		case bf && !af:
			return 1
		}
		return 0
	})
	return ws
}

// window is the app's main window: the largest of its windows.
func (p *proc) window() *uia.Element {
	var best *uia.Element
	area := int32(0)
	for _, w := range p.windows() {
		b := w.Bounds()
		if a := (b.Right - b.Left) * (b.Bottom - b.Top); a > area {
			best, area = w, a
		}
	}
	return best
}

func (p *proc) Find(k appcore.Kind, name string, shown bool) ([]desktopcore.Control, error) {
	var out []desktopcore.Control
	err := p.w.do(func() error {
		if p.java != nil {
			found := p.java.search(p.java.root, k.Noun, collapse(name), shown)
			if k.Noun == "element" {
				found = captionsOut(found, func(e *jab.Element) bool { return e.Role() == "label" })
			}
			for _, e := range found {
				out = append(out, jctl{e: e, w: p.w})
			}
			return nil
		}
		found := p.u.find(k.Noun, collapse(name), shown)
		if k.Noun == "element" {
			found = captionsOut(found, func(e *uia.Element) bool { return e.ControlType() == uiaText })
		}
		for _, e := range found {
			out = append(out, p.u.control(e))
		}
		return nil
	})
	return out, err
}

func (p *proc) Names(k appcore.Kind) []string {
	var out []string
	_ = p.w.do(func() error {
		if p.java != nil {
			out = p.java.names(k.Noun)
		} else {
			out = p.u.names(k.Noun)
		}
		return nil
	})
	return out
}

func (p *proc) Shows(text string) (bool, error) {
	var ok bool
	err := p.w.do(func() error {
		if p.java != nil {
			ok = p.java.shows(collapse(text))
		} else {
			ok = p.u.shows(collapse(text))
		}
		return nil
	})
	return ok, err
}

func (p *proc) Texts() []string {
	var out []string
	_ = p.w.do(func() error {
		if p.java != nil {
			out = p.java.texts()
		} else {
			out = p.u.texts()
		}
		return nil
	})
	return out
}

// Front brings the app's main window to the front, and fails before a click
// or a key could reach another app.
func (p *proc) Front() error {
	return p.w.do(func() error {
		// UI Automation lists an app's windows a moment late at times.
		for wait := time.Now(); time.Since(wait) < 3*time.Second; time.Sleep(50 * time.Millisecond) {
			w := p.window()
			if w == nil {
				continue
			}
			// Its main window in front, its controls drawn as active (Qt's
			// field's line); else, after a second, another of its windows
			// (a dialog it shows over it).
			if uia.Foreground(w.Handle()) || time.Since(wait) > time.Second && slices.Contains(p.pids(), uia.ForegroundProcess()) {
				return nil
			}
		}
		if p.window() == nil {
			return fmt.Errorf("the %s app has no window", p.app.Name)
		}
		return fmt.Errorf("the %s app (process %d) does not come to the front: stopping before a click or a key reaches another app", p.app.Name, p.pid)
	})
}

func (p *proc) ScrollTo(k appcore.Kind, name string) (desktopcore.Control, error) {
	var c desktopcore.Control
	err := p.w.do(func() error {
		if p.java != nil {
			if e := p.java.scrollTo(k.Noun, collapse(name)); e != nil {
				c = jctl{e: e, w: p.w}
			}
			return nil
		}
		if e := p.u.scrollTo(k.Noun, collapse(name)); e != nil {
			c = p.u.control(e)
		}
		return nil
	})
	return c, err
}

func (p *proc) ScrollIntoView(c desktopcore.Control) error {
	return p.w.do(func() error {
		switch c := c.(type) {
		case jctl:
			p.java.reveal(c.e)
		case ctl:
			p.u.reveal(c.e)
		}
		return nil
	})
}

func (p *proc) Click(c desktopcore.Control) error {
	if err := p.Front(); err != nil {
		return err
	}
	return p.w.do(func() error {
		switch c := c.(type) {
		case jctl:
			return p.java.click(c.e)
		case ctl:
			return p.u.click(c.e)
		}
		return fmt.Errorf("not a control of this app")
	})
}

// origin is the control's top left on the screen, and the screen pixels of
// a point, once it is in view and still.
func (p *proc) origin(c desktopcore.Control) (x, y int, scale float64, err error) {
	err = p.w.do(func() error {
		switch c := c.(type) {
		case jctl:
			x, y, err = p.java.origin(c.e)
			scale = p.java.scale
		case ctl:
			x, y, err = p.u.origin(c.e)
			scale = 1
			if w := p.window(); w != nil {
				scale = uia.Scale(w.Handle())
			}
		}
		return err
	})
	return x, y, scale, err
}

func (p *proc) ClickAt(c desktopcore.Control, x, y float64) error {
	if err := p.Front(); err != nil {
		return err
	}
	ox, oy, s, err := p.origin(c)
	if err != nil {
		return err
	}
	if err := uia.Click(ox+int(x*s), oy+int(y*s)); err != nil {
		return err
	}
	time.Sleep(250 * time.Millisecond)
	return nil
}

func (p *proc) Drag(c desktopcore.Control, x1, y1, x2, y2 float64) error {
	if err := p.Front(); err != nil {
		return err
	}
	ox, oy, s, err := p.origin(c)
	if err != nil {
		return err
	}
	if err := uia.Drag(ox+int(x1*s), oy+int(y1*s), ox+int(x2*s), oy+int(y2*s)); err != nil {
		return err
	}
	time.Sleep(250 * time.Millisecond)
	return nil
}

func (p *proc) Key(spec string) error {
	if err := p.Front(); err != nil {
		return err
	}
	if err := uia.Key(spec); err != nil {
		return err
	}
	p.w.pump(250 * time.Millisecond)
	return nil
}

func (p *proc) Type(text string) error {
	if err := p.Front(); err != nil {
		return err
	}
	return uia.Type(text)
}

// Away moves the pointer beside the app's window, in the work area (not on
// the taskbar).
func (p *proc) Away() error {
	return p.w.do(func() error {
		w := p.window()
		if w == nil {
			return nil
		}
		r, ok := uia.WindowRect(w.Handle())
		if !ok {
			return nil
		}
		wa := uia.WorkArea()
		at := desktopcore.AwaySpot(image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)),
			image.Rect(int(wa.Left), int(wa.Top), int(wa.Right), int(wa.Bottom)))
		return uia.Move(at.X, at.Y)
	})
}

// Window captures the main window as it draws itself (PrintWindow, all of
// its content).
func (p *proc) Window() (image.Image, float64, error) {
	var img image.Image
	scale := 1.0
	err := p.w.do(func() error {
		// UI Automation lists an app's windows a moment late at times.
		w := p.window()
		for wait := time.Now(); w == nil && time.Since(wait) < 3*time.Second; w = p.window() {
			time.Sleep(50 * time.Millisecond)
		}
		if w == nil {
			return fmt.Errorf("the %s app has no window", p.app.Name)
		}
		shot, err := uia.CaptureWindow(w.Handle())
		if err != nil {
			return err
		}
		// A window that draws itself smaller for a capture than it shows
		// (WinUI's, at its own size) is cropped to what it drew, and places
		// on the screen are scaled to it.
		display, capture := uia.Scale(w.Handle()), uia.CaptureScale(w.Handle())
		k := min(capture/display, 1)
		if k < 1 {
			b := shot.Bounds()
			drawn := image.NewRGBA(image.Rect(0, 0, int(float64(b.Dx())*k), int(float64(b.Dy())*k)))
			draw.Draw(drawn, drawn.Bounds(), shot, b.Min, draw.Src)
			shot = drawn
		}
		img, scale = shot, capture
		if at, ok := p.caret(w.Handle(), display); ok {
			if k < 1 {
				at = image.Rect(int(float64(at.Min.X)*k), int(float64(at.Min.Y)*k), int(math.Ceil(float64(at.Max.X)*k)), int(math.Ceil(float64(at.Max.Y)*k)))
			}
			img = desktopcore.HideCaret(img, at)
		}
		return nil
	})
	return img, scale, err
}

// caret is where the text cursor of the app's focused field is in a capture
// of the window hwnd at scale: a column a few pixels wide, a line high. A
// line said to be outside its field is the field's height: the cursor is in
// it. On the worker's thread.
func (p *proc) caret(hwnd uintptr, scale float64) (image.Rectangle, bool) {
	win, ok := uia.WindowRect(hwnd)
	if !ok {
		return image.Rectangle{}, false
	}
	var c, f uia.Rect
	if p.java != nil {
		x, y, h, e, ok := p.w.jab.Caret(hwnd)
		if !ok || !slices.Contains(javaRoles["field"], e.Role()) {
			return image.Rectangle{}, false // a table has a caret too
		}
		fx, fy, fw, fh := e.Bounds()
		l, t := p.java.px(fx, fy)
		r, b := p.java.px(fx+fw, fy+fh)
		f = uia.Rect{Left: int32(l), Top: int32(t), Right: int32(r), Bottom: int32(b)}
		px, py := p.java.px(x, y)
		_, ph := p.java.px(0, h)
		if py < t || py > b {
			// The bridge gives the caret in Java's points where it gives
			// controls in pixels: scaled by the display's.
			px, py, ph = int(float64(x)*scale), int(float64(y)*scale), int(float64(h)*scale)
		}
		c = uia.Rect{Left: int32(px), Top: int32(py), Right: int32(px), Bottom: int32(py + ph)}
	} else {
		var pid int
		if c, f, pid, ok = p.w.uia.Caret(); !ok || !slices.Contains(p.pids(), pid) {
			return image.Rectangle{}, false
		}
	}
	top, bottom := c.Top, c.Bottom
	if f.Bottom > f.Top && (top < f.Top || bottom > f.Bottom) {
		top, bottom = f.Top, f.Bottom
	}
	// A cursor taller than its line (WinForms', a pixel past its field).
	pad := max(3, (bottom-top)/3)
	top, bottom = top-pad, bottom+pad
	if f.Bottom > f.Top {
		top, bottom = max(top, f.Top-2), min(bottom, f.Bottom+2)
	}
	// The cursor and a pixel of its anti-aliasing to the left, never the
	// last letter: at a display's scale the place is rounded a pixel either
	// way, and a letter's edge hidden in one screenshot and not the next
	// differs. To the right, more: Qt 5 says a character ends a few pixels
	// before it draws the cursor.
	left, right := int32(1), int32(math.Ceil(6*scale))
	return image.Rect(int(c.Left-left-win.Left), int(top-win.Top), int(c.Left+right-win.Left), int(bottom-win.Top)), true
}

func (p *proc) Tree() (*desktopcore.Node, error) {
	root := &desktopcore.Node{Role: "Application", Name: filepath.Base(p.app.App)}
	err := p.w.do(func() error {
		count := 0
		if p.java != nil {
			root.Children = append(root.Children, p.java.node(p.java.root, 1, &count))
			return nil
		}
		for _, w := range p.windows() {
			root.Children = append(root.Children, p.u.node(w, 1, &count))
		}
		return nil
	})
	return root, err
}

func (p *proc) Stop() error  { return p.stop() }
func (p *proc) Exited() bool { return p.exited() }

func (p *proc) Describe() map[string]any {
	d := map[string]any{"app": filepath.Base(p.app.App), "process": p.pid}
	if p.java != nil {
		d["through"] = "the Java Access Bridge"
	}
	return d
}

// attachJava reads the app's main window through the Java Access Bridge,
// when it is a Java window: UI Automation sees only its frame. On the
// worker's thread.
func (p *proc) attachJava(w *uia.Element) error {
	if w.ClassName() != "SunAwtFrame" && w.ClassName() != "SunAwtDialog" {
		return nil
	}
	dll, err := bridgeDLL(p.app)
	if err != nil {
		return err
	}
	c, err := p.w.java(dll)
	if err != nil {
		return err
	}
	hwnd := w.Handle()
	for wait := time.Now(); time.Since(wait) < 20*time.Second; c.Pump(250 * time.Millisecond) {
		if !c.IsJavaWindow(hwnd) {
			continue
		}
		root, err := c.Window(hwnd)
		if err != nil {
			continue
		}
		b := w.Bounds()
		_, _, jw, _ := root.Bounds()
		scale := 1.0
		if jw > 0 {
			scale = float64(b.Right-b.Left) / float64(jw)
		}
		p.java = &javaTree{p: p, root: root, scale: scale}
		return nil
	}
	return fmt.Errorf("the Java Access Bridge did not reach the app's window within 20s: is the bridge on in its Java (axx switches it on through JAVA_TOOL_OPTIONS)")
}

// captionsOut leaves out the texts among elements found by a name when a
// control is among them too: a text that shows a control's name (a switch's
// caption) is not the element a step names.
func captionsOut[E any](found []E, isText func(E) bool) []E {
	var controls []E
	for _, e := range found {
		if !isText(e) {
			controls = append(controls, e)
		}
	}
	if len(controls) == 0 {
		return found
	}
	return controls
}
