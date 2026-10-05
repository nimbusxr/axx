//go:build windows

package uia

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	getWindowRect          = user32.NewProc("GetWindowRect")
	getDC                  = user32.NewProc("GetDC")
	releaseDC              = user32.NewProc("ReleaseDC")
	printWindow            = user32.NewProc("PrintWindow")
	getDpiForWindow        = user32.NewProc("GetDpiForWindow")
	setForegroundWindow    = user32.NewProc("SetForegroundWindow")
	getForegroundWindow    = user32.NewProc("GetForegroundWindow")
	showWindow             = user32.NewProc("ShowWindow")
	getWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
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

// Scale is the window's display scale: 1.75 at 175%.
func Scale(hwnd uintptr) float64 {
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
	_, _, _ = showWindow.Call(hwnd, restore)
	_, _, _ = setForegroundWindow.Call(hwnd)
	front, _, _ := getForegroundWindow.Call()
	return front == hwnd
}

// ForegroundProcess is the process of the window in front.
func ForegroundProcess() int {
	front, _, _ := getForegroundWindow.Call()
	var pid uint32
	_, _, _ = getWindowThreadProcess.Call(front, uintptr(unsafe.Pointer(&pid)))
	return int(pid)
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
	var out []int
	var add func(p int)
	add = func(p int) {
		for _, c := range kids[p] {
			if c != p {
				out = append(out, c)
				add(c)
			}
		}
	}
	add(pid)
	return out
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
