//go:build windows

package uia

import (
	"os"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	registerClassEx = user32.NewProc("RegisterClassExW")
	createWindowEx  = user32.NewProc("CreateWindowExW")
	destroyWindow   = user32.NewProc("DestroyWindow")
	defWindowProc   = user32.NewProc("DefWindowProcW")
	loadIcon        = user32.NewProc("LoadIconW")
	shellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
)

// wndClassEx is WNDCLASSEXW.
type wndClassEx struct {
	size, style                   uint32
	wndProc                       uintptr
	clsExtra, wndExtra            int32
	instance, icon, cursor, brush uintptr
	menuName, className           *uint16
	iconSm                        uintptr
}

// notifyIconData is NOTIFYICONDATAW.
type notifyIconData struct {
	size            uint32
	hwnd            uintptr
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            uintptr
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	version         uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guidItem        windows.GUID
	balloonIcon     uintptr
}

var defProc = syscall.NewCallback(func(h, msg, w, l uintptr) uintptr {
	r, _, _ := defWindowProc.Call(h, msg, w, l)
	return r
})

// newWindow makes a top-level window of the class, as an app's. The test's
// thread stays locked: its cleanup destroys the window on the thread that
// made it.
func newWindow(t *testing.T, class string, exStyle, style uint32, w, h int32) uintptr {
	t.Helper()
	name, _ := windows.UTF16PtrFromString(class)
	wc := wndClassEx{wndProc: defProc, className: name}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != windows.ERROR_CLASS_ALREADY_EXISTS {
		t.Fatalf("RegisterClassEx %s: %v", class, err)
	}
	hwnd, _, err := createWindowEx.Call(uintptr(exStyle), uintptr(unsafe.Pointer(name)), 0, uintptr(style), 0, 0, uintptr(w), uintptr(h), 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatalf("CreateWindowEx %s: %v", class, err)
	}
	t.Cleanup(func() { _, _, _ = destroyWindow.Call(hwnd) })
	return hwnd
}

// The event loop window tao keeps shown is no window of its app's: no
// person sees it, and a capture of it panics tao (tauri-apps/tao#1140).
func TestVisibleWindowsLeaveOutEventLoops(t *testing.T) {
	runtime.LockOSThread()
	const (
		visible    = 0x10000000 // WS_VISIBLE
		popup      = 0x80000000 // WS_POPUP
		overlapped = 0x00CF0000 // WS_OVERLAPPEDWINDOW
		// What tao gives it: WS_EX_NOACTIVATE, WS_EX_TRANSPARENT,
		// WS_EX_LAYERED and WS_EX_TOOLWINDOW.
		eventTarget = 0x08000000 | 0x20 | 0x00080000 | 0x80
	)
	app := newWindow(t, "axx test window", 0, visible|overlapped, 300, 200)
	loop := newWindow(t, "Tao Thread Event Target", eventTarget, visible|popup, 0, 0)
	got := VisibleWindows([]int{os.Getpid()})
	if !slices.Contains(got, app) {
		t.Errorf("VisibleWindows %v left out the app's window %#x", got, app)
	}
	if slices.Contains(got, loop) {
		t.Errorf("VisibleWindows %v has tao's event loop window %#x", got, loop)
	}
}

// An app's icon in the taskbar's tray is found by the window it gave the
// shell, whether that window shows or not, and is gone once removed.
func TestInTray(t *testing.T) {
	runtime.LockOSThread()
	pids := []int{os.Getpid()}
	hidden := newWindow(t, "axx test tray window", 0, 0, 0, 0)
	if InTray(pids) {
		t.Fatal("an icon in the tray before one was added")
	}
	const (
		add, remove = 0, 2        // NIM_ADD, NIM_DELETE
		iconTip     = 0x2 | 0x4   // NIF_ICON | NIF_TIP
		appIcon     = 32512       // IDI_APPLICATION
		id          = trayIDs / 2 // a number apps give
	)
	icon, _, _ := loadIcon.Call(0, appIcon)
	data := notifyIconData{hwnd: hidden, id: id, flags: iconTip, icon: icon}
	data.size = uint32(unsafe.Sizeof(data))
	copy(data.tip[:], windows.StringToUTF16("axx test icon"))
	if r, _, err := shellNotifyIcon.Call(add, uintptr(unsafe.Pointer(&data))); r == 0 {
		t.Fatalf("Shell_NotifyIcon NIM_ADD: %v", err)
	}
	t.Cleanup(func() { _, _, _ = shellNotifyIcon.Call(remove, uintptr(unsafe.Pointer(&data))) })
	if !InTray(pids) {
		t.Error("the icon added is not found in the tray")
	}
	if r, _, err := shellNotifyIcon.Call(remove, uintptr(unsafe.Pointer(&data))); r == 0 {
		t.Fatalf("Shell_NotifyIcon NIM_DELETE: %v", err)
	}
	if InTray(pids) {
		t.Error("the icon removed is still found in the tray")
	}
}
