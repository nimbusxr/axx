//go:build darwin

package ax

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/ebitengine/purego"
)

// What a person's pointer does: CGEvents posted to the system, as the
// mouse posts them. Posting them needs the Accessibility permission.
var (
	inputOnce               sync.Once
	inputErr                error
	cgEventCreateMouseEvent func(source uintptr, kind uint32, at Point, button uint32) uintptr
	cgEventPost             func(tap uint32, event uintptr)

	cgEventCreateKeyboardEvent      func(source uintptr, key uint16, down bool) uintptr
	cgEventKeyboardSetUnicodeString func(event uintptr, length uint64, text *uint16)
	cgEventSetFlags                 func(event uintptr, flags uint64)
	cgEventCreateScrollWheelEvent2  func(source uintptr, units uint32, count uint32, wheel1, wheel2, wheel3 int32) uintptr
	cgEventSetLocation              func(event uintptr, at Point)
	cgEventCreate                   func(source uintptr) uintptr
	cgEventGetLocation              func(event uintptr) Point
)

const (
	eventLeftMouseDown    = 1
	eventLeftMouseUp      = 2
	eventMouseMoved       = 5
	eventLeftMouseDragged = 6
	mouseButtonLeft       = 0
	hidEventTap           = 0
)

func loadInput() error {
	inputOnce.Do(func() {
		if err := load(); err != nil {
			inputErr = err
			return
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			inputErr = fmt.Errorf("cannot load CoreGraphics: %w", err)
			return
		}
		purego.RegisterLibFunc(&cgEventCreateMouseEvent, cg, "CGEventCreateMouseEvent")
		purego.RegisterLibFunc(&cgEventPost, cg, "CGEventPost")
		purego.RegisterLibFunc(&cgEventCreateKeyboardEvent, cg, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgEventKeyboardSetUnicodeString, cg, "CGEventKeyboardSetUnicodeString")
		purego.RegisterLibFunc(&cgEventSetFlags, cg, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgEventCreateScrollWheelEvent2, cg, "CGEventCreateScrollWheelEvent2")
		purego.RegisterLibFunc(&cgEventSetLocation, cg, "CGEventSetLocation")
		purego.RegisterLibFunc(&cgEventCreate, cg, "CGEventCreate")
		purego.RegisterLibFunc(&cgEventGetLocation, cg, "CGEventGetLocation")
	})
	return inputErr
}

func post(kind uint32, at Point) error {
	ev := cgEventCreateMouseEvent(0, kind, at, mouseButtonLeft)
	if ev == 0 {
		return fmt.Errorf("cannot make a mouse event")
	}
	cgEventPost(hidEventTap, ev)
	cfRelease(ev)
	return nil
}

// Move moves the pointer to a point on the screen.
func Move(at Point) error {
	if err := loadInput(); err != nil {
		return err
	}
	return post(eventMouseMoved, at)
}

// Click moves the pointer to a point on the screen and clicks there.
func Click(at Point) error {
	if err := loadInput(); err != nil {
		return err
	}
	for _, kind := range []uint32{eventMouseMoved, eventLeftMouseDown, eventLeftMouseUp} {
		if err := post(kind, at); err != nil {
			return err
		}
		time.Sleep(30 * time.Millisecond)
	}
	return nil
}

// Center is the middle of the element on the screen.
func (e *Element) Center() (Point, error) {
	p, err := e.Attribute("AXPosition")
	if err != nil {
		return Point{}, err
	}
	s, err := e.Attribute("AXSize")
	if err != nil {
		return Point{}, err
	}
	pos, ok1 := p.(Point)
	size, ok2 := s.(Size)
	if !ok1 || !ok2 {
		return Point{}, fmt.Errorf("the element has no place on the screen")
	}
	return Point{X: pos.X + size.Width/2, Y: pos.Y + size.Height/2}, nil
}

// Type types text into whatever has the keyboard focus, a character at a
// time, as keys a person presses. A character on the keyboard is its key
// (with Shift), and the character itself, for apps that read either (Qt
// reads the key); one off the keyboard is the character alone.
func Type(text string) error {
	if err := loadInput(); err != nil {
		return err
	}
	for _, r := range text {
		units := utf16.Encode([]rune{r})
		code, shift := usKey(r)
		var flags uint64
		if shift {
			flags = flagShift
		}
		if err := stroke(code, flags, units, 0); err != nil {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// usKey is the key (and whether with Shift) that types r on a US keyboard,
// or key 0 for a character it has no key for.
func usKey(r rune) (uint16, bool) {
	const shifted = `~!@#$%^&*()_+{}|:"<>?`
	const plain = "`1234567890-=[]\\;',./"
	switch {
	case r >= 'a' && r <= 'z':
		return keyCodes[string(r)], false
	case r >= 'A' && r <= 'Z':
		return keyCodes[strings.ToLower(string(r))], true
	case r == ' ':
		return keyCodes["Space"], false
	}
	codes := []uint16{50, 18, 19, 20, 21, 23, 22, 26, 28, 25, 29, 27, 24, 33, 30, 42, 41, 39, 43, 47, 44}
	if i := strings.IndexRune(plain, r); i >= 0 {
		return codes[i], false
	}
	if i := strings.IndexRune(shifted, r); i >= 0 {
		return codes[i], true
	}
	return 0, false
}

// Drag presses the pointer at from, moves it to to in steps, as a hand
// does, and lets go there.
func Drag(from, to Point) error {
	if err := loadInput(); err != nil {
		return err
	}
	if err := post(eventMouseMoved, from); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	if err := post(eventLeftMouseDown, from); err != nil {
		return err
	}
	// A hand holds the button a moment before it moves, and moves over half a
	// second, at the screen's 60 frames a second: what reads drags as a
	// person makes them (macOS's region capture among them) misses one
	// that is all over in a few frames.
	time.Sleep(120 * time.Millisecond)
	const steps = 30
	for i := 1; i <= steps; i++ {
		at := Point{X: from.X + (to.X-from.X)*float64(i)/steps, Y: from.Y + (to.Y-from.Y)*float64(i)/steps}
		if err := post(eventLeftMouseDragged, at); err != nil {
			return err
		}
		time.Sleep(16 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond)
	return post(eventLeftMouseUp, to)
}

// Modifier flags of key events (CGEventFlags).
const (
	flagShift   = 0x20000
	flagControl = 0x40000
	flagOption  = 0x80000
	flagCommand = 0x100000
)

// modifiers are the modifier keys, by their flags, in the order a hand
// holds them down, with their virtual key codes.
var modifiers = []struct {
	flag uint64
	code uint16
}{{flagControl, 59}, {flagOption, 58}, {flagShift, 56}, {flagCommand, 55}}

// stroke presses a key with the modifiers flags names, as a hand does: each
// modifier down, the key down and up, and the modifiers up. The system holds
// a modifier down until its key comes up, whatever flags a key's own events
// carry: one never let go stays held for the clicks and keys after it (a
// click with Control held is a right click, and macOS's region capture
// copies to the clipboard). units is the character the key types, if any;
// pause is the time between events.
func stroke(code uint16, flags uint64, units []uint16, pause time.Duration) error {
	key := func(code uint16, down bool, flags uint64, units []uint16) error {
		ev := cgEventCreateKeyboardEvent(0, code, down)
		if ev == 0 {
			return fmt.Errorf("cannot make a key event")
		}
		cgEventSetFlags(ev, flags)
		if len(units) > 0 {
			cgEventKeyboardSetUnicodeString(ev, uint64(len(units)), &units[0])
		}
		cgEventPost(hidEventTap, ev)
		cfRelease(ev)
		time.Sleep(pause)
		return nil
	}
	var held uint64
	for _, m := range modifiers {
		if flags&m.flag != 0 {
			held |= m.flag
			if err := key(m.code, true, held, nil); err != nil {
				return err
			}
		}
	}
	err := key(code, true, flags, units)
	if err == nil {
		err = key(code, false, flags, units)
	}
	// The modifiers come up even when the key failed: one left down holds
	// for everything after it.
	for i := len(modifiers) - 1; i >= 0; i-- {
		if m := modifiers[i]; held&m.flag != 0 {
			held &^= m.flag
			if uerr := key(m.code, false, held, nil); err == nil {
				err = uerr
			}
		}
	}
	return err
}

// keyCodes are the virtual key codes of the keys keys names, on an ANSI
// keyboard.
var keyCodes = map[string]uint16{
	"Enter": 36, "Escape": 53, "Tab": 48, "Backspace": 51, "Delete": 117, "Space": 49,
	"ArrowLeft": 123, "ArrowRight": 124, "ArrowDown": 125, "ArrowUp": 126,
	"Home": 115, "End": 119, "PageUp": 116, "PageDown": 121,
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7, "c": 8, "v": 9, "b": 11,
	"q": 12, "w": 13, "e": 14, "r": 15, "y": 16, "t": 17, "o": 31, "u": 32, "i": 34, "p": 35,
	"l": 37, "j": 38, "k": 40, "n": 45, "m": 46,
	"1": 18, "2": 19, "3": 20, "4": 21, "6": 22, "5": 23, "9": 25, "7": 26, "8": 28, "0": 29,
}

// PressKey presses a key, with its modifiers, as the web pack names them:
// "Enter", "Escape", "Control+Shift+S", "ControlOrMeta+Z" (Command here).
func PressKey(spec string) error {
	if err := loadInput(); err != nil {
		return err
	}
	parts := strings.Split(spec, "+")
	var flags uint64
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "Shift":
			flags |= flagShift
		case "Control":
			flags |= flagControl
		case "Alt":
			flags |= flagOption
		case "Meta", "ControlOrMeta":
			flags |= flagCommand
		default:
			return fmt.Errorf("%s: no modifier %q", spec, m)
		}
	}
	name := parts[len(parts)-1]
	code, ok := keyCodes[name]
	if !ok {
		code, ok = keyCodes[strings.ToLower(name)]
		if ok && name != strings.ToLower(name) {
			flags |= flagShift
		}
	}
	if !ok {
		return fmt.Errorf("%s: no key %q", spec, name)
	}
	return stroke(code, flags, nil, 20*time.Millisecond)
}

// Scroll turns the mouse wheel over a point by lines: up when lines is
// positive, down when negative.
func Scroll(at Point, lines int) error {
	if err := loadInput(); err != nil {
		return err
	}
	if err := post(eventMouseMoved, at); err != nil {
		return err
	}
	const unitLine = 1
	ev := cgEventCreateScrollWheelEvent2(0, unitLine, 1, int32(lines), 0, 0)
	if ev == 0 {
		return fmt.Errorf("cannot make a scroll event")
	}
	cgEventSetLocation(ev, at)
	cgEventPost(hidEventTap, ev)
	cfRelease(ev)
	time.Sleep(60 * time.Millisecond)
	return nil
}

// Frame is the element's place on the screen: its top left corner and size.
func (e *Element) Frame() (Point, Size, error) {
	p, err := e.Attribute("AXPosition")
	if err != nil {
		return Point{}, Size{}, err
	}
	s, err := e.Attribute("AXSize")
	if err != nil {
		return Point{}, Size{}, err
	}
	pos, ok1 := p.(Point)
	size, ok2 := s.(Size)
	if !ok1 || !ok2 {
		return Point{}, Size{}, fmt.Errorf("the element has no place on the screen")
	}
	return pos, size, nil
}

// PointerLocation is where the pointer is, in points from the main screen's
// top left.
func PointerLocation() (Point, error) {
	if err := loadInput(); err != nil {
		return Point{}, err
	}
	ev := cgEventCreate(0)
	if ev == 0 {
		return Point{}, fmt.Errorf("cannot ask where the pointer is")
	}
	defer cfRelease(ev)
	return cgEventGetLocation(ev), nil
}
