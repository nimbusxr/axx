//go:build windows

package uia

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// keyInput is INPUT holding a KEYBDINPUT, as SendInput takes it on 64-bit
// Windows (40 bytes).
type keyInput struct {
	kind      uint32
	_         uint32
	vk, scan  uint16
	flags     uint32
	time      uint32
	_         uint32
	extraInfo uintptr
	_         [8]byte
}

const (
	inputKeyboard   = 1
	keyEventUnicode = 0x0004
	keyEventKeyUp   = 0x0002
)

var (
	user32       = syscall.NewLazyDLL("user32.dll")
	sendInput    = user32.NewProc("SendInput")
	setCursorPos = user32.NewProc("SetCursorPos")
	systemParams = user32.NewProc("SystemParametersInfoW")
	metrics      = user32.NewProc("GetSystemMetrics")
	dpiAware     = user32.NewProc("SetProcessDpiAwarenessContext")
	setWindowPos = user32.NewProc("SetWindowPos")
)

// DPIAware has the process see the screen in its pixels, as UI Automation
// gives places: otherwise Windows scales the places it moves the pointer to.
func DPIAware() {
	const perMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	_, _, _ = dpiAware.Call(perMonitorV2)
}

// Type types text into whatever has the keyboard focus, a character at a
// time, as keys a person presses.
func Type(text string) error {
	for _, r := range text {
		for _, unit := range utf16.Encode([]rune{r}) {
			in := []keyInput{
				{kind: inputKeyboard, scan: unit, flags: keyEventUnicode},
				{kind: inputKeyboard, scan: unit, flags: keyEventUnicode | keyEventKeyUp},
			}
			n, _, err := sendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
			if int(n) != len(in) {
				return fmt.Errorf("SendInput: %w", err)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

// mouseInput is INPUT holding a MOUSEINPUT (40 bytes on 64-bit Windows).
type mouseInput struct {
	kind      uint32
	_         uint32
	dx, dy    int32
	data      uint32
	flags     uint32
	time      uint32
	_         uint32
	extraInfo uintptr
}

const (
	inputMouse      = 0
	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseWheel      = 0x0800
	wheelDelta      = 120
	virtualKeyInput = 0
)

func mouse(flags, data uint32) error {
	in := mouseInput{kind: inputMouse, flags: flags, data: data}
	n, _, err := sendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if n != 1 {
		return fmt.Errorf("SendInput: %w", err)
	}
	return nil
}

// Move moves the pointer to a point on the screen as the mouse moves it, with
// a move input the app sees as it sees a person's (a cursor set in place
// sends it none).
func Move(x, y int) error {
	const moveAbsolute = 0x0001 | 0x8000 // MOUSEEVENTF_MOVE | MOUSEEVENTF_ABSOLUTE
	w, _, _ := metrics.Call(0)           // SM_CXSCREEN
	h, _, _ := metrics.Call(1)           // SM_CYSCREEN
	if w < 2 || h < 2 {
		if r, _, err := setCursorPos.Call(uintptr(x), uintptr(y)); r == 0 {
			return fmt.Errorf("SetCursorPos: %w", err)
		}
		return nil
	}
	in := mouseInput{
		kind: inputMouse, flags: moveAbsolute,
		dx: int32((x*65535 + int(w-1)/2) / int(w-1)), dy: int32((y*65535 + int(h-1)/2) / int(h-1)),
	}
	if n, _, err := sendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in)); n != 1 {
		return fmt.Errorf("SendInput: %w", err)
	}
	return nil
}

// Click moves the pointer to a point on the screen and clicks there.
func Click(x, y int) error {
	if err := Move(x, y); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	if err := mouse(mouseLeftDown, 0); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	return mouse(mouseLeftUp, 0)
}

// Drag presses the pointer at one point, moves it to another in steps, and
// lets go there.
func Drag(x1, y1, x2, y2 int) error {
	if err := Move(x1, y1); err != nil {
		return err
	}
	time.Sleep(30 * time.Millisecond)
	if err := mouse(mouseLeftDown, 0); err != nil {
		return err
	}
	const steps = 12
	for i := 1; i <= steps; i++ {
		time.Sleep(15 * time.Millisecond)
		if err := Move(x1+(x2-x1)*i/steps, y1+(y2-y1)*i/steps); err != nil {
			return err
		}
	}
	return mouse(mouseLeftUp, 0)
}

// Scroll turns the mouse wheel over a point by lines: up when positive.
func Scroll(x, y, lines int) error {
	if err := Move(x, y); err != nil {
		return err
	}
	time.Sleep(60 * time.Millisecond)
	return mouse(mouseWheel, uint32(int32(lines*wheelDelta)))
}

// virtualKeys are the Windows virtual keys of the keys Key names.
var virtualKeys = map[string]uint16{
	"Enter": 0x0D, "Escape": 0x1B, "Tab": 0x09, "Backspace": 0x08, "Delete": 0x2E, "Space": 0x20,
	"ArrowLeft": 0x25, "ArrowUp": 0x26, "ArrowRight": 0x27, "ArrowDown": 0x28,
	"Home": 0x24, "End": 0x23, "PageUp": 0x21, "PageDown": 0x22,
	"Shift": 0x10, "Control": 0x11, "ControlOrMeta": 0x11, "Alt": 0x12, "Meta": 0x5B,
}

// Key presses a key, with its modifiers, as the web pack names them: "Enter",
// "Escape", "Control+Shift+S", "ControlOrMeta+A" (Control here).
func Key(spec string) error {
	var vks []uint16
	for _, part := range strings.Split(spec, "+") {
		vk, ok := virtualKeys[part]
		if !ok && len(part) == 1 {
			vk, ok = uint16(strings.ToUpper(part)[0]), true
		}
		if !ok {
			return fmt.Errorf("%s: no key %q", spec, part)
		}
		vks = append(vks, vk)
	}
	send := func(vk uint16, flags uint32) error {
		in := keyInput{kind: inputKeyboard, vk: vk, flags: flags}
		n, _, err := sendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
		if n != 1 {
			return fmt.Errorf("SendInput: %w", err)
		}
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	for _, vk := range vks {
		if err := send(vk, virtualKeyInput); err != nil {
			return err
		}
	}
	for i := len(vks) - 1; i >= 0; i-- {
		if err := send(vks[i], keyEventKeyUp); err != nil {
			return err
		}
	}
	return nil
}

// MoveWindow moves and sizes a window (its handle), in the screen's pixels.
func MoveWindow(hwnd uintptr, r Rect) error {
	const noZOrder, noActivate = 0x0004, 0x0010
	if ok, _, err := setWindowPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), noZOrder|noActivate); ok == 0 {
		return fmt.Errorf("SetWindowPos: %w", err)
	}
	return nil
}

// WorkArea is the part of the primary screen windows show in: the screen but
// the taskbar.
func WorkArea() Rect {
	const getWorkArea = 0x0030 // SPI_GETWORKAREA
	var r Rect
	_, _, _ = systemParams.Call(getWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	return r
}

// Intersect is the part of a that is in b.
func Intersect(a, b Rect) Rect {
	r := Rect{Left: max(a.Left, b.Left), Top: max(a.Top, b.Top), Right: min(a.Right, b.Right), Bottom: min(a.Bottom, b.Bottom)}
	if r.Right < r.Left {
		r.Right = r.Left
	}
	if r.Bottom < r.Top {
		r.Bottom = r.Top
	}
	return r
}
