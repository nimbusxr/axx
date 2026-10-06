//go:build linux

package atspi

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb/xproto"
)

// Gnome is the pointer, the keyboard and the screen of a headless GNOME
// Shell on Wayland: its remote desktop takes the pointer's and the keys'
// events, and axx's GNOME Shell extension (us.nimbusxr.axx.Desktop) takes
// screenshots. On Wayland an app has no place on the screen of its own: the
// extension puts each window at the screen's corner, so the app's
// coordinates are the screen's.
type Gnome struct {
	bus           *dbus.Conn
	session       dbus.BusObject // the remote desktop session
	stream        dbus.ObjectPath
	width, height int
	dir           string // where screenshots are written
}

const (
	remoteDesktop = "org.gnome.Mutter.RemoteDesktop"
	screenCast    = "org.gnome.Mutter.ScreenCast"
	// DesktopName is axx's GNOME Shell extension's name on the session bus.
	DesktopName = "us.nimbusxr.axx.Desktop"
	btnLeft     = 0x110 // the evdev code of the left button
)

// NewGnome starts a remote desktop session on the GNOME Shell of the
// session bus at addr, whose screen is width by height pixels: its pointer
// moves on a recording of that screen, which carries no picture anywhere.
func NewGnome(addr string, width, height int, dir string) (*Gnome, error) {
	bus, err := dbus.Connect(addr)
	if err != nil {
		return nil, err
	}
	g := &Gnome{bus: bus, width: width, height: height, dir: dir}
	var rd dbus.ObjectPath
	if err := bus.Object(remoteDesktop, "/org/gnome/Mutter/RemoteDesktop").Call(remoteDesktop+".CreateSession", 0).Store(&rd); err != nil {
		bus.Close()
		return nil, fmt.Errorf("GNOME Shell's remote desktop: %w", err)
	}
	g.session = bus.Object(remoteDesktop, rd)
	id, err := g.session.GetProperty(remoteDesktop + ".Session.SessionId")
	if err != nil {
		bus.Close()
		return nil, err
	}
	var sc dbus.ObjectPath
	opts := map[string]dbus.Variant{"remote-desktop-session-id": id}
	if err := bus.Object(screenCast, "/org/gnome/Mutter/ScreenCast").Call(screenCast+".CreateSession", 0, opts).Store(&sc); err != nil {
		bus.Close()
		return nil, fmt.Errorf("GNOME Shell's screen cast: %w", err)
	}
	if err := bus.Object(screenCast, sc).Call(screenCast+".Session.RecordMonitor", 0, "", map[string]dbus.Variant{}).Store(&g.stream); err != nil {
		bus.Close()
		return nil, fmt.Errorf("GNOME Shell's screen cast of its screen: %w", err)
	}
	if err := g.session.Call(remoteDesktop+".Session.Start", 0).Err; err != nil {
		bus.Close()
		return nil, fmt.Errorf("starting GNOME Shell's remote desktop: %w", err)
	}
	// The first key the remote desktop takes holds no modifier (Control+A
	// typed an "a"): a Shift, pressed and let go, is that key.
	shift := uint32(keysyms["Shift"])
	if err := g.keysym(shift, true); err == nil {
		_ = g.keysym(shift, false)
	}
	return g, nil
}

// Close ends the remote desktop session.
func (g *Gnome) Close() {
	_ = g.session.Call(remoteDesktop+".Session.Stop", 0).Err
	g.bus.Close()
}

func (g *Gnome) call(method string, args ...any) error {
	return g.session.Call(remoteDesktop+".Session."+method, 0, args...).Err
}

// Size is the screen's size, in pixels.
func (g *Gnome) Size() (width, height int) { return g.width, g.height }

// Move moves the pointer to a point on the screen.
func (g *Gnome) Move(x, y int) error {
	return g.call("NotifyPointerMotionAbsolute", string(g.stream), float64(x), float64(y))
}

func (g *Gnome) button(down bool) error { return g.call("NotifyPointerButton", int32(btnLeft), down) }

// Click moves the pointer to a point and clicks there, as a hand does: the
// pointer comes to rest over it, then the button goes down and up. GNOME
// Shell gives a window the pointer a moment after it arrives, and a press
// before then is lost (a GTK 4 notebook's tab does not switch).
func (g *Gnome) Click(x, y int) error {
	for i := 3; i >= 0; i-- {
		if err := g.Move(x-8*i, y); err != nil {
			return err
		}
		time.Sleep(16 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	if err := g.button(true); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	return g.button(false)
}

// Drag presses the pointer at one point, moves it to another in steps, as a
// hand does, and lets go there.
func (g *Gnome) Drag(x1, y1, x2, y2 int) error {
	if err := g.Move(x1, y1); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	if err := g.button(true); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	const steps = 30
	for i := 1; i <= steps; i++ {
		if err := g.Move(x1+(x2-x1)*i/steps, y1+(y2-y1)*i/steps); err != nil {
			return err
		}
		time.Sleep(16 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond)
	return g.button(false)
}

// Scroll turns the mouse wheel over a point by lines: up when lines is
// positive, down when negative.
func (g *Gnome) Scroll(x, y, lines int) error {
	if err := g.Move(x, y); err != nil {
		return err
	}
	const vertical = 0
	return g.call("NotifyPointerAxisDiscrete", uint32(vertical), int32(-lines))
}

func (g *Gnome) keysym(sym uint32, down bool) error {
	return g.call("NotifyKeyboardKeysym", sym, down)
}

// Key presses a key, with its modifiers, as the web pack names them: "Enter",
// "Escape", "Control+Shift+S", "ControlOrMeta+A" (Control here).
func (g *Gnome) Key(spec string) error {
	var syms []uint32
	for _, part := range strings.Split(spec, "+") {
		sym, ok := keysyms[part]
		if !ok && len([]rune(part)) == 1 {
			sym, ok = xproto.Keysym(unicode.ToLower([]rune(part)[0])), true
		}
		if !ok {
			return fmt.Errorf("%s: no key %q", spec, part)
		}
		syms = append(syms, uint32(sym))
	}
	for _, s := range syms {
		if err := g.keysym(s, true); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	for i := len(syms) - 1; i >= 0; i-- {
		if err := g.keysym(syms[i], false); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// Type types text, a key at a time, into whatever has the keyboard focus:
// GNOME Shell finds each character's key (with Shift) on its keyboard.
func (g *Gnome) Type(text string) error {
	for _, r := range text {
		sym := uint32(r)
		if r > 0xff {
			sym = 0x01000000 | uint32(r) // a Unicode keysym
		}
		if err := g.keysym(sym, true); err != nil {
			return err
		}
		if err := g.keysym(sym, false); err != nil {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// FocusUnderPointer does nothing: GNOME Shell gives the keyboard to a
// window as it is clicked, as a window manager does.
func (g *Gnome) FocusUnderPointer() error { return nil }

// Screenshot is the screen as GNOME Shell shows it.
func (g *Gnome) Screenshot() (*image.RGBA, error) {
	f, err := os.CreateTemp(g.dir, "screen-*.png")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)
	if err := g.bus.Object(DesktopName, "/us/nimbusxr/axx").Call(DesktopName+".Screenshot", 0, path).Err; err != nil {
		return nil, fmt.Errorf("GNOME Shell's screenshot: %w", err)
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	img, err := png.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("GNOME Shell's screenshot is not a PNG: %w", err)
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out, nil
}

// Frame is a window as GNOME Shell has it: its frame, its surface (buffer),
// which holds the frame and a shadow around it, and whether it is an X11
// app's, on Xwayland.
type Frame struct {
	Frame, Surface image.Rectangle
	X11            bool
}

// Frames are the windows of the process pid.
func (g *Gnome) Frames(pid int) ([]Frame, error) {
	var rows []struct{ X, Y, W, H, SX, SY, SW, SH, Client int32 }
	if err := g.bus.Object(DesktopName, "/us/nimbusxr/axx").Call(DesktopName+".Frames", 0, int32(pid)).Store(&rows); err != nil {
		return nil, err
	}
	var out []Frame
	for _, r := range rows {
		out = append(out, Frame{
			Frame:   image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H)),
			Surface: image.Rect(int(r.SX), int(r.SY), int(r.SX+r.SW), int(r.SY+r.SH)),
			X11:     r.Client == 1,
		})
	}
	return out, nil
}

// X11Display is GNOME Shell's Xwayland display, where X11 apps show, and
// the X authority file it takes; "" when it has none.
func (g *Gnome) X11Display() (display, authority string) {
	_ = g.bus.Object(DesktopName, "/us/nimbusxr/axx").Call(DesktopName+".Display", 0).Store(&display, &authority)
	return display, authority
}

// WaitForDesktop waits for axx's GNOME Shell extension on the session bus at
// addr: GNOME Shell is up, with the extension's desktop.
func WaitForDesktop(addr string, d time.Duration) error {
	bus, err := dbus.Connect(addr)
	if err != nil {
		return err
	}
	defer bus.Close()
	for wait := time.Now(); time.Since(wait) < d; time.Sleep(200 * time.Millisecond) {
		var has bool
		if bus.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, DesktopName).Store(&has) == nil && has {
			return nil
		}
	}
	return errors.New("GNOME Shell did not start with axx's extension")
}
