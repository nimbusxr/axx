//go:build linux

package atspi

import (
	"encoding/binary"
	"fmt"
	"image"
	"strings"
	"time"
	"unicode"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// Input is a person's pointer and keyboard on an X11 display (Xvfb, or
// Xwayland): events posted through XTest, as the devices post them.
type Input struct {
	x    *xgb.Conn
	root xproto.Window
	keys map[xproto.Keysym]key
}

type key struct {
	code  xproto.Keycode
	shift bool
}

const keysymShiftL = 0xffe1

// NewInput connects to the display that $DISPLAY names.
func NewInput() (*Input, error) { return NewInputOn("") }

// NewInputOn connects to a display, like ":99" ("" is $DISPLAY).
func NewInputOn(display string) (*Input, error) {
	x, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, fmt.Errorf("no X display %s: %w", display, err)
	}
	if err := xtest.Init(x); err != nil {
		x.Close()
		return nil, fmt.Errorf("the X server has no XTest: %w", err)
	}
	setup := xproto.Setup(x)
	n := byte(setup.MaxKeycode - setup.MinKeycode + 1)
	m, err := xproto.GetKeyboardMapping(x, setup.MinKeycode, n).Reply()
	if err != nil {
		x.Close()
		return nil, err
	}
	in := &Input{x: x, root: setup.DefaultScreen(x).Root, keys: map[xproto.Keysym]key{}}
	per := int(m.KeysymsPerKeycode)
	for i := range int(n) {
		for j := 0; j < per && j < 2; j++ {
			ks := m.Keysyms[i*per+j]
			if _, seen := in.keys[ks]; ks != 0 && !seen {
				in.keys[ks] = key{code: setup.MinKeycode + xproto.Keycode(i), shift: j == 1}
			}
		}
	}
	return in, nil
}

// Close closes the connection.
func (in *Input) Close() { in.x.Close() }

func (in *Input) fake(kind byte, detail byte, x, y int16) error {
	return xtest.FakeInputChecked(in.x, kind, detail, xproto.TimeCurrentTime, in.root, x, y, 0).Check()
}

// Click moves the pointer to a point on the screen and clicks there, with
// the pauses of a hand: Java drops a press that comes with the move.
func (in *Input) Click(x, y int) error {
	if err := in.fake(xproto.MotionNotify, 0, int16(x), int16(y)); err != nil {
		return err
	}
	if err := in.fake(xproto.ButtonPress, 1, 0, 0); err != nil {
		return err
	}
	return in.fake(xproto.ButtonRelease, 1, 0, 0)
}

// Type types text, a key at a time, into whatever has the keyboard focus.
// Characters of Latin-1 have keysyms equal to their code points.
func (in *Input) Type(text string) error {
	shift, ok := in.keys[keysymShiftL]
	if !ok {
		return fmt.Errorf("the keyboard has no Shift")
	}
	for _, r := range text {
		k, ok := in.keys[xproto.Keysym(r)]
		if !ok {
			return fmt.Errorf("no key types %q", r)
		}
		if k.shift {
			if err := in.fake(xproto.KeyPress, byte(shift.code), 0, 0); err != nil {
				return err
			}
		}
		if err := in.fake(xproto.KeyPress, byte(k.code), 0, 0); err != nil {
			return err
		}
		if err := in.fake(xproto.KeyRelease, byte(k.code), 0, 0); err != nil {
			return err
		}
		if k.shift {
			if err := in.fake(xproto.KeyRelease, byte(shift.code), 0, 0); err != nil {
				return err
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// Size is the screen's size, in pixels.
func (in *Input) Size() (width, height int) {
	s := xproto.Setup(in.x).DefaultScreen(in.x)
	return int(s.WidthInPixels), int(s.HeightInPixels)
}

// Move moves the pointer to a point on the screen.
func (in *Input) Move(x, y int) error { return in.fake(xproto.MotionNotify, 0, int16(x), int16(y)) }

// Drag presses the pointer at one point, moves it to another in steps, as a
// hand does, and lets go there.
func (in *Input) Drag(x1, y1, x2, y2 int) error {
	if err := in.Move(x1, y1); err != nil {
		return err
	}
	if err := in.fake(xproto.ButtonPress, 1, 0, 0); err != nil {
		return err
	}
	const steps = 12
	for i := 1; i <= steps; i++ {
		time.Sleep(15 * time.Millisecond)
		if err := in.Move(x1+(x2-x1)*i/steps, y1+(y2-y1)*i/steps); err != nil {
			return err
		}
	}
	return in.fake(xproto.ButtonRelease, 1, 0, 0)
}

// Scroll turns the mouse wheel over a point by lines: up when lines is
// positive, down when negative (X's buttons 4 and 5).
func (in *Input) Scroll(x, y, lines int) error {
	if err := in.Move(x, y); err != nil {
		return err
	}
	button := byte(4)
	if lines < 0 {
		button, lines = 5, -lines
	}
	for range lines {
		if err := in.fake(xproto.ButtonPress, button, 0, 0); err != nil {
			return err
		}
		if err := in.fake(xproto.ButtonRelease, button, 0, 0); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// keysyms are the X keysyms of the keys Key names.
var keysyms = map[string]xproto.Keysym{
	"Enter": 0xff0d, "Escape": 0xff1b, "Tab": 0xff09, "Backspace": 0xff08, "Delete": 0xffff,
	"ArrowLeft": 0xff51, "ArrowUp": 0xff52, "ArrowRight": 0xff53, "ArrowDown": 0xff54,
	"Home": 0xff50, "End": 0xff57, "PageUp": 0xff55, "PageDown": 0xff56, "Space": 0x20,
	"Shift": 0xffe1, "Control": 0xffe3, "ControlOrMeta": 0xffe3, "Alt": 0xffe9, "Meta": 0xffeb,
}

// Key presses a key, with its modifiers, as the web pack names them: "Enter",
// "Escape", "Control+Shift+S", "ControlOrMeta+A" (Control here).
func (in *Input) Key(spec string) error {
	var codes []byte
	for _, part := range strings.Split(spec, "+") {
		sym, ok := keysyms[part]
		if !ok && len([]rune(part)) == 1 {
			sym, ok = xproto.Keysym(unicode.ToLower([]rune(part)[0])), true
		}
		k, found := in.keys[sym]
		if !ok || !found {
			return fmt.Errorf("%s: no key %q", spec, part)
		}
		codes = append(codes, byte(k.code))
	}
	for _, c := range codes {
		if err := in.fake(xproto.KeyPress, c, 0, 0); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	for i := len(codes) - 1; i >= 0; i-- {
		if err := in.fake(xproto.KeyRelease, codes[i], 0, 0); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// Activate asks the window manager to bring the process's top window to the
// front, with the keyboard, as a click on it in a list of windows does
// (EWMH's _NET_ACTIVE_WINDOW). Without a window manager, nothing.
func (in *Input) Activate(pid int) error {
	atom := func(name string) xproto.Atom {
		a, err := xproto.InternAtom(in.x, true, uint16(len(name)), name).Reply()
		if err != nil {
			return xproto.AtomNone
		}
		return a.Atom
	}
	stacking, wmPID, active := atom("_NET_CLIENT_LIST_STACKING"), atom("_NET_WM_PID"), atom("_NET_ACTIVE_WINDOW")
	if stacking == xproto.AtomNone || wmPID == xproto.AtomNone || active == xproto.AtomNone {
		return nil
	}
	list, err := xproto.GetProperty(in.x, false, in.root, stacking, xproto.AtomWindow, 0, 4096).Reply()
	if err != nil {
		return err
	}
	var top xproto.Window
	for i := 0; i+4 <= len(list.Value); i += 4 {
		w := xproto.Window(binary.LittleEndian.Uint32(list.Value[i:]))
		p, err := xproto.GetProperty(in.x, false, w, wmPID, xproto.AtomCardinal, 0, 1).Reply()
		if err == nil && len(p.Value) >= 4 && int(binary.LittleEndian.Uint32(p.Value)) == pid {
			top = w // the list runs bottom to top
		}
	}
	if top == 0 {
		return nil
	}
	// The app's window in use already: its open menu stays open.
	if a, err := xproto.GetProperty(in.x, false, in.root, active, xproto.AtomWindow, 0, 1).Reply(); err == nil && len(a.Value) >= 4 {
		w := xproto.Window(binary.LittleEndian.Uint32(a.Value))
		if p, err := xproto.GetProperty(in.x, false, w, wmPID, xproto.AtomCardinal, 0, 1).Reply(); err == nil && len(p.Value) >= 4 && int(binary.LittleEndian.Uint32(p.Value)) == pid {
			return nil
		}
	}
	const fromPager = 2 // the request comes from a person's choice, as a pager's
	ev := xproto.ClientMessageEvent{
		Format: 32, Window: top, Type: active,
		Data: xproto.ClientMessageDataUnionData32New([]uint32{fromPager, uint32(xproto.TimeCurrentTime), 0, 0, 0}),
	}
	return xproto.SendEventChecked(in.x, false, in.root,
		xproto.EventMaskSubstructureNotify|xproto.EventMaskSubstructureRedirect, string(ev.Bytes())).Check()
}

// Place is where a process's window of a size is on the screen, as the
// window manager placed it: an app that knows places in its window only
// (GTK 4) needs it.
func (in *Input) Place(pid int, width, height int32) (x, y int32, ok bool) {
	r, ok := in.window(pid, width, height)
	return int32(r.Min.X), int32(r.Min.Y), ok
}

// Window is the process's top window on the screen, as the window manager
// has it (its own title bar too, when it draws one): the window a
// screenshot of the app is of.
func (in *Input) Window(pid int) (image.Rectangle, bool) { return in.window(pid, 0, 0) }

// window is a process's window of a size (any, for none) on the screen, the
// top one first (EWMH's _NET_CLIENT_LIST_STACKING). A window that draws its
// own shadow (GTK's) says how wide it is (_GTK_FRAME_EXTENTS).
func (in *Input) window(pid int, width, height int32) (image.Rectangle, bool) {
	atom := func(name string) xproto.Atom {
		a, err := xproto.InternAtom(in.x, true, uint16(len(name)), name).Reply()
		if err != nil {
			return xproto.AtomNone
		}
		return a.Atom
	}
	clients, wmPID, frame := atom("_NET_CLIENT_LIST_STACKING"), atom("_NET_WM_PID"), atom("_GTK_FRAME_EXTENTS")
	if clients == xproto.AtomNone || wmPID == xproto.AtomNone {
		return image.Rectangle{}, false
	}
	list, err := xproto.GetProperty(in.x, false, in.root, clients, xproto.AtomWindow, 0, 4096).Reply()
	if err != nil {
		return image.Rectangle{}, false
	}
	near := func(a, b int32) bool { return a-b <= 2 && b-a <= 2 }
	// The top window first: the list runs bottom to top.
	for i := len(list.Value) - 4; i >= 0; i -= 4 {
		w := xproto.Window(binary.LittleEndian.Uint32(list.Value[i:]))
		p, err := xproto.GetProperty(in.x, false, w, wmPID, xproto.AtomCardinal, 0, 1).Reply()
		if err != nil || len(p.Value) < 4 || int(binary.LittleEndian.Uint32(p.Value)) != pid {
			continue
		}
		g, err := xproto.GetGeometry(in.x, xproto.Drawable(w)).Reply()
		if err != nil {
			continue
		}
		var left, right, top, bottom int32
		if frame != xproto.AtomNone {
			if e, err := xproto.GetProperty(in.x, false, w, frame, xproto.AtomCardinal, 0, 4).Reply(); err == nil && len(e.Value) >= 16 {
				left, right = int32(binary.LittleEndian.Uint32(e.Value)), int32(binary.LittleEndian.Uint32(e.Value[4:]))
				top, bottom = int32(binary.LittleEndian.Uint32(e.Value[8:])), int32(binary.LittleEndian.Uint32(e.Value[12:]))
			}
		}
		cw, ch := int32(g.Width)-left-right, int32(g.Height)-top-bottom
		if width > 0 && (!near(cw, width) || !near(ch, height)) {
			continue
		}
		t, err := xproto.TranslateCoordinates(in.x, w, in.root, int16(left), int16(top)).Reply()
		if err != nil {
			continue
		}
		return image.Rect(int(t.DstX), int(t.DstY), int(t.DstX)+int(cw), int(t.DstY)+int(ch)), true
	}
	return image.Rectangle{}, false
}

// FocusUnderPointer gives the keyboard to the window under the pointer when
// no window has it, as a window manager does when a window is clicked: GTK 4
// takes keys only in a window that has the focus. An app that took the
// keyboard itself keeps it: Java gives it to a window of its own, and loses
// keys when another takes it.
func (in *Input) FocusUnderPointer() error {
	f, err := xproto.GetInputFocus(in.x).Reply()
	if err != nil {
		return err
	}
	if f.Focus != xproto.InputFocusNone && f.Focus != xproto.InputFocusPointerRoot {
		return nil
	}
	p, err := xproto.QueryPointer(in.x, in.root).Reply()
	if err != nil {
		return err
	}
	if p.Child == 0 {
		return nil
	}
	return xproto.SetInputFocusChecked(in.x, xproto.InputFocusPointerRoot, p.Child, xproto.TimeCurrentTime).Check()
}
