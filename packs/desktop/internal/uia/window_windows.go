//go:build windows

package uia

import (
	"fmt"
	"image"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	shcore                 = syscall.NewLazyDLL("shcore.dll")
	monitorFromWindow      = user32.NewProc("MonitorFromWindow")
	getDpiForMonitor       = shcore.NewProc("GetDpiForMonitor")
	getWindowRect          = user32.NewProc("GetWindowRect")
	getDC                  = user32.NewProc("GetDC")
	releaseDC              = user32.NewProc("ReleaseDC")
	printWindow            = user32.NewProc("PrintWindow")
	getDpiForWindow        = user32.NewProc("GetDpiForWindow")
	setForegroundWindow    = user32.NewProc("SetForegroundWindow")
	bringWindowToTop       = user32.NewProc("BringWindowToTop")
	attachThreadInput      = user32.NewProc("AttachThreadInput")
	isIconic               = user32.NewProc("IsIconic")
	getForegroundWindow    = user32.NewProc("GetForegroundWindow")
	isWindow               = user32.NewProc("IsWindow")
	findWindow             = user32.NewProc("FindWindowW")
	showWindow             = user32.NewProc("ShowWindow")
	getWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	sendMessage            = user32.NewProc("SendMessageW")
	postMessage            = user32.NewProc("PostMessageW")
	sendMessageTimeout     = user32.NewProc("SendMessageTimeoutW")
	createCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	createDIBSection       = gdi32.NewProc("CreateDIBSection")
	selectObject           = gdi32.NewProc("SelectObject")
	deleteObject           = gdi32.NewProc("DeleteObject")
	deleteDC               = gdi32.NewProc("DeleteDC")
)

// bitmapInfo is BITMAPINFOHEADER, for a top-down 32-bit bitmap.
type bitmapInfo struct {
	size                         uint32
	width, height                int32
	planes, bitCount             uint16
	compression, sizeImage       uint32
	xPelsPerMeter, yPelsPerMeter int32
	clrUsed, clrImportant        uint32
	_                            [4]byte // one RGBQUAD, unused
}

// CaptureWindow is a picture of the window as it draws itself, all of its
// content (WebView2's too), whatever covers it.
func CaptureWindow(hwnd uintptr) (*image.RGBA, error) {
	var r Rect
	if ok, _, err := getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return nil, fmt.Errorf("GetWindowRect: %w", err)
	}
	w, h := r.Right-r.Left, r.Bottom-r.Top
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("the window has no size")
	}
	screen, _, _ := getDC.Call(0)
	defer func() { _, _, _ = releaseDC.Call(0, screen) }()
	mem, _, _ := createCompatibleDC.Call(screen)
	if mem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer func() { _, _, _ = deleteDC.Call(mem) }()
	info := bitmapInfo{size: 40, width: w, height: -h, planes: 1, bitCount: 32}
	var bits unsafe.Pointer
	bmp, _, _ := createDIBSection.Call(mem, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 {
		return nil, fmt.Errorf("CreateDIBSection failed")
	}
	defer func() { _, _, _ = deleteObject.Call(bmp) }()
	old, _, _ := selectObject.Call(mem, bmp)
	defer func() { _, _, _ = selectObject.Call(mem, old) }()
	const renderFullContent = 2 // PW_RENDERFULLCONTENT
	if ok, _, err := printWindow.Call(hwnd, mem, renderFullContent); ok == 0 {
		return nil, fmt.Errorf("PrintWindow: %w", err)
	}
	src := unsafe.Slice((*byte)(bits), int(w)*int(h)*4)
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(src); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 255 // BGRA
	}
	return img, nil
}

// WindowRect is the window's place on the screen, in pixels: where its
// capture (CaptureWindow) starts.
func WindowRect(hwnd uintptr) (Rect, bool) {
	var r Rect
	ok, _, _ := getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0
}

// CaptureScale is the scale the window draws itself at for a capture
// (CaptureWindow): the display's, but 1 for WinUI's, which says it is at 96
// dots an inch and draws at its own size there, at the top left of what
// GetWindowRect gives.
func CaptureScale(hwnd uintptr) float64 {
	dpi, _, _ := getDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// Scale is the scale of the display the window is on: 1.75 at 175%. It is
// the monitor's, not what the window says (WinUI's says 96 dots an inch).
func Scale(hwnd uintptr) float64 {
	const nearest, effective = 2, 0 // MONITOR_DEFAULTTONEAREST, MDT_EFFECTIVE_DPI
	if monitor, _, _ := monitorFromWindow.Call(hwnd, nearest); monitor != 0 && getDpiForMonitor.Find() == nil {
		var x, y uint32
		if r, _, _ := getDpiForMonitor.Call(monitor, effective, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); r == 0 && x > 0 {
			return float64(x) / 96
		}
	}
	dpi, _, _ := getDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// Foreground brings the window to the front, restored if it is minimized,
// and reports whether it is the window in front now.
func Foreground(hwnd uintptr) bool {
	const restore = 9 // SW_RESTORE
	if iconic, _, _ := isIconic.Call(hwnd); iconic != 0 {
		// Restore a minimized window only: a maximized one would be made
		// smaller.
		_, _, _ = showWindow.Call(hwnd, restore)
	}
	front, _, _ := getForegroundWindow.Call()
	if front == hwnd {
		return true
	}
	_, _, _ = setForegroundWindow.Call(hwnd)
	if front, _, _ = getForegroundWindow.Call(); front == hwnd || front == 0 {
		return front == hwnd
	}
	// Windows lets the app with the last input put a window in front: one
	// that another app's start took the front from shares the input of the
	// window in front for a moment, as a person's click would give it.
	frontThread, _, _ := getWindowThreadProcess.Call(front, 0)
	self := uintptr(windows.GetCurrentThreadId())
	if frontThread != 0 && frontThread != self {
		_, _, _ = attachThreadInput.Call(self, frontThread, 1)
		_, _, _ = setForegroundWindow.Call(hwnd)
		_, _, _ = bringWindowToTop.Call(hwnd)
		_, _, _ = attachThreadInput.Call(self, frontThread, 0)
	}
	front, _, _ = getForegroundWindow.Call()
	return front == hwnd
}

// HideKeyboardCues has a window draw no keyboard cues (a focus rectangle,
// underlined access keys) until a key shows them, as a window a person opens
// with the mouse does: Windows otherwise shows them in a new window when the
// machine's last input was a key, whatever app had it.
func HideKeyboardCues(hwnd uintptr) {
	const (
		changeUIState = 0x0127    // WM_CHANGEUISTATE
		set           = 1         // UIS_SET
		hide          = 0x1 | 0x2 // UISF_HIDEFOCUS | UISF_HIDEACCEL
	)
	_, _, _ = sendMessage.Call(hwnd, changeUIState, uintptr(hide<<16|set), 0)
}

// ForegroundWindow is the window in front.
func ForegroundWindow() uintptr {
	front, _, _ := getForegroundWindow.Call()
	return front
}

// IsWindow is whether the window is still there.
func IsWindow(hwnd uintptr) bool {
	ok, _, _ := isWindow.Call(hwnd)
	return ok != 0
}

// Desktop is the desktop's window (Explorer's Program Manager), which a
// click on the desktop brings to the front.
func Desktop() uintptr {
	class, _ := windows.UTF16PtrFromString("Progman")
	hwnd, _, _ := findWindow.Call(uintptr(unsafe.Pointer(class)), 0)
	return hwnd
}

// Answers is whether the window's app takes a message within d: its thread
// is waiting for what comes next, not busy.
func Answers(hwnd uintptr, d time.Duration) bool {
	const abortIfHung = 0x0002 // SMTO_ABORTIFHUNG
	var result uintptr
	ok, _, _ := sendMessageTimeout.Call(hwnd, 0, 0, 0, abortIfHung, uintptr(d.Milliseconds()), uintptr(unsafe.Pointer(&result)))
	return ok != 0
}

// Terminate stops the process at once, as Task Manager's End task does.
func Terminate(pid int) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	_ = windows.TerminateProcess(h, 1)
}

// ForegroundProcess is the process of the window in front.
func ForegroundProcess() int {
	front, _, _ := getForegroundWindow.Call()
	var pid uint32
	_, _, _ = getWindowThreadProcess.Call(front, uintptr(unsafe.Pointer(&pid)))
	return int(pid)
}

// Named are the processes of the executable named exe (explorer.exe), the
// case as Windows ignores it.
func Named(exe string) []int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap) //nolint:errcheck
	var out []int
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if strings.EqualFold(windows.UTF16ToString(e.ExeFile[:]), exe) {
			out = append(out, int(e.ProcessID))
		}
	}
	return out
}

// Close asks the window to close, as its close button does.
func Close(hwnd uintptr) {
	const wmClose = 0x0010
	_, _, _ = postMessage.Call(hwnd, wmClose, 0, 0)
}

// Descendants are the processes pid started, and those they started.
func Descendants(pid int) []int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap) //nolint:errcheck
	kids := map[int][]int{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		kids[int(e.ParentProcessID)] = append(kids[int(e.ParentProcessID)], int(e.ProcessID))
	}
	return descendants(kids, pid, started)
}

// started is when the process started, as a FILETIME's 100-nanosecond
// ticks.
func started(pid int) (int64, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // a process's number
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h) //nolint:errcheck
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, false
	}
	return created.Nanoseconds() / 100, true
}

// Expands is whether the element opens and closes, as a menu does (its
// ExpandCollapse pattern).
func (e *Element) Expands() bool {
	p, err := e.pattern(patternExpandCollapse, iidIExpandCollapse)
	if err != nil {
		return false
	}
	p.release()
	return true
}

// The windows EnumWindows lists, of the processes asked for (those that
// show, or every one): one callback for every call (a process makes only so
// many).
var (
	enumMu    sync.Mutex
	enumPIDs  []int
	enumShown bool
	enumOut   []uintptr
	enumCB    = syscall.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		if enumShown && !windows.IsWindowVisible(h) {
			return 1
		}
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(h, &pid); err == nil && slices.Contains(enumPIDs, int(pid)) {
			enumOut = append(enumOut, uintptr(h))
		}
		return 1
	})
)

func topWindows(pids []int, shown bool) []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumPIDs, enumShown, enumOut = pids, shown, nil
	_ = windows.EnumWindows(enumCB, nil)
	return enumOut
}

// eventTargets are the classes of the windows tao and winit (the windowing
// of Tauri's apps and Rust's) keep for their event loops: shown, as Windows
// paints only a window that shows and they take its paint for the end of
// their queue, and drawing nothing. No person sees one, and a capture of one
// (PrintWindow) paints it: a paint that comes as the loop flushes its
// paints panics it (tauri-apps/tao#1140).
var eventTargets = []string{"Tao Thread Event Target", "Winit Thread Event Target"}

// VisibleWindows are the processes' top-level windows that show, in the
// order Windows stacks them; not a toolkit's event loop window.
func VisibleWindows(pids []int) []uintptr {
	return slices.DeleteFunc(topWindows(pids, true), func(h uintptr) bool {
		return slices.Contains(eventTargets, className(h))
	})
}

func className(h uintptr) string {
	buf := make([]uint16, 256)
	n, _ := windows.GetClassName(windows.HWND(h), &buf[0], int32(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	notifyIconGetRect = shell32.NewProc("Shell_NotifyIconGetRect")
)

// notifyIconIdentifier is NOTIFYICONIDENTIFIER: an icon in the taskbar's
// tray, by the window its app gave it and its number.
type notifyIconIdentifier struct {
	size uint32
	hwnd uintptr
	id   uint32
	guid windows.GUID
}

// trayIDs are the icon numbers asked for: an app numbers its icons itself,
// from 0 or 1 up as most do.
const trayIDs = 32

// InTray is whether the processes have an icon in the taskbar's tray, shown
// or among those it hides: the shell knows each by its app's window and
// number. An icon an app names by a GUID instead is not found.
func InTray(pids []int) bool {
	for _, h := range topWindows(pids, false) {
		for id := range uint32(trayIDs) {
			n := notifyIconIdentifier{hwnd: h, id: id}
			n.size = uint32(unsafe.Sizeof(n))
			var r Rect
			if hr, _, _ := notifyIconGetRect.Call(uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&r))); hr == 0 {
				return true
			}
		}
	}
	return false
}

// Alive is whether the process runs.
func Alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h) //nolint:errcheck
	var code uint32
	const stillActive = 259
	return windows.GetExitCodeProcess(h, &code) == nil && code == stillActive
}

var getSystemMetrics = user32.NewProc("GetSystemMetrics")

// ScreenSize is the main screen's size, in pixels.
func ScreenSize() (width, height int) {
	const cx, cy = 0, 1 // SM_CXSCREEN, SM_CYSCREEN
	w, _, _ := getSystemMetrics.Call(cx)
	h, _, _ := getSystemMetrics.Call(cy)
	return int(w), int(h)
}

// CursorPos is where the pointer is on the screen, in pixels.
func CursorPos() (x, y int) {
	var pt struct{ X, Y int32 }
	_, _, _ = getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y)
}
