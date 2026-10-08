//go:build darwin

package ax

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	shotOnce sync.Once
	shotErr  error

	cgWindowListCopyWindowInfo       func(option, relativeTo uint32) ref
	cgWindowListCreateImageFromArray func(bounds cgRect, windows ref, option uint32) ref
	cgImageGetWidth                  func(img ref) uintptr
	cgImageGetHeight                 func(img ref) uintptr
	cgImageGetBytesPerRow            func(img ref) uintptr
	cgImageGetBitmapInfo             func(img ref) uint32
	cgImageGetDataProvider           func(img ref) ref
	cgDataProviderCopyData           func(provider ref) ref
	cgMainDisplayID                  func() uint32
	cgDisplayBounds                  func(display uint32) cgRect
	cgEventCreate                    func(source uintptr) ref
	cgEventGetLocation               func(event ref) Point
	cfDataGetBytePtr                 func(data ref) uintptr
	cfDataGetLength                  func(data ref) int
	cfArrayCreate                    func(alloc uintptr, values unsafe.Pointer, n int, callbacks uintptr) ref
	cfDictionaryGetValue             func(dict, key ref) ref

	kCGWindowNumber, kCGWindowOwnerPID ref
)

const (
	windowListOnScreenOnly       = 1 << 0 // kCGWindowListOptionOnScreenOnly
	windowListExcludeDesktop     = 1 << 4 // kCGWindowListExcludeDesktopElements
	windowImageBoundsIgnoreFrame = 1 << 0 // kCGWindowImageBoundsIgnoreFraming
	windowImageNominalResolution = 1 << 4 // kCGWindowImageNominalResolution
	byteOrder32Little            = 2 << 12
	byteOrderMask                = 0x7000
	numberSInt32                 = 3 // kCFNumberSInt32Type
)

func loadScreenshots() error {
	shotOnce.Do(func() {
		if err := load(); err != nil {
			shotErr = err
			return
		}
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			shotErr = fmt.Errorf("cannot load CoreGraphics: %w", err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			shotErr = fmt.Errorf("cannot load CoreFoundation: %w", err)
			return
		}
		for name, fn := range map[string]any{
			"CGWindowListCopyWindowInfo":       &cgWindowListCopyWindowInfo,
			"CGWindowListCreateImageFromArray": &cgWindowListCreateImageFromArray,
			"CGImageGetWidth":                  &cgImageGetWidth,
			"CGImageGetHeight":                 &cgImageGetHeight,
			"CGImageGetBytesPerRow":            &cgImageGetBytesPerRow,
			"CGImageGetBitmapInfo":             &cgImageGetBitmapInfo,
			"CGImageGetDataProvider":           &cgImageGetDataProvider,
			"CGDataProviderCopyData":           &cgDataProviderCopyData,
			"CGMainDisplayID":                  &cgMainDisplayID,
			"CGDisplayBounds":                  &cgDisplayBounds,
			"CGEventCreate":                    &cgEventCreate,
			"CGEventGetLocation":               &cgEventGetLocation,
		} {
			purego.RegisterLibFunc(fn, cg, name)
		}
		for name, fn := range map[string]any{
			"CFDataGetBytePtr":     &cfDataGetBytePtr,
			"CFDataGetLength":      &cfDataGetLength,
			"CFArrayCreate":        &cfArrayCreate,
			"CFDictionaryGetValue": &cfDictionaryGetValue,
		} {
			purego.RegisterLibFunc(fn, cf, name)
		}
		for name, v := range map[string]*ref{"kCGWindowNumber": &kCGWindowNumber, "kCGWindowOwnerPID": &kCGWindowOwnerPID} {
			sym, err := purego.Dlsym(cg, name)
			if err != nil {
				shotErr = fmt.Errorf("cannot find %s: %w", name, err)
				return
			}
			// sym is the global's address, which holds the CFString.
			*v = **(**ref)(unsafe.Pointer(&sym))
		}
	})
	return shotErr
}

// WindowsOf are the window server's numbers of the windows on the screen
// that the processes own, the front one first.
func WindowsOf(pids []int) ([]uint32, error) {
	if err := loadScreenshots(); err != nil {
		return nil, err
	}
	list := cgWindowListCopyWindowInfo(windowListOnScreenOnly|windowListExcludeDesktop, 0)
	if list == 0 {
		return nil, errors.New("the window server lists no windows")
	}
	defer cfRelease(list)
	owned := map[int32]bool{}
	for _, pid := range pids {
		owned[int32(pid)] = true //nolint:gosec // a process number
	}
	var out []uint32
	for i := range cfArrayGetCount(list) {
		info := cfArrayGetValueAtIndex(list, i)
		var pid, num int32
		if p := cfDictionaryGetValue(info, kCGWindowOwnerPID); p == 0 || !cfNumberGetValue(p, numberSInt32, unsafe.Pointer(&pid)) || !owned[pid] {
			continue
		}
		if n := cfDictionaryGetValue(info, kCGWindowNumber); n != 0 && cfNumberGetValue(n, numberSInt32, unsafe.Pointer(&num)) {
			out = append(out, uint32(num)) //nolint:gosec // a window's number
		}
	}
	return out, nil
}

// CaptureWindows is the main screen with only the windows drawn, each where
// it is and as it shows (black elsewhere: the rest is the person's), a
// pixel a point.
func CaptureWindows(ids []uint32) (*image.RGBA, error) {
	if err := loadScreenshots(); err != nil {
		return nil, err
	}
	bounds := cgDisplayBounds(cgMainDisplayID())
	out := image.NewRGBA(image.Rect(0, 0, int(bounds.Width), int(bounds.Height)))
	for i := 3; i < len(out.Pix); i += 4 {
		out.Pix[i] = 255 // opaque black
	}
	if len(ids) == 0 {
		return out, nil
	}
	values := make([]uintptr, len(ids))
	for i, id := range ids {
		values[i] = uintptr(id)
	}
	arr := cfArrayCreate(0, unsafe.Pointer(&values[0]), len(values), 0)
	runtime.KeepAlive(values)
	if arr == 0 {
		return nil, errors.New("cannot list the windows to capture")
	}
	defer cfRelease(arr)
	img := cgWindowListCreateImageFromArray(bounds, arr, windowImageBoundsIgnoreFrame|windowImageNominalResolution)
	if img == 0 {
		return nil, errors.New("the window server captured nothing: may the terminal or the IDE that runs axx record the screen?")
	}
	defer cfRelease(img)
	w, h, stride := int(cgImageGetWidth(img)), int(cgImageGetHeight(img)), int(cgImageGetBytesPerRow(img))
	data := cgDataProviderCopyData(cgImageGetDataProvider(img))
	if data == 0 {
		return nil, errors.New("the window server's capture has no pixels")
	}
	defer cfRelease(data)
	src := unsafe.Slice((*byte)(cPointer(cfDataGetBytePtr(data))), cfDataGetLength(data))
	bgra := cgImageGetBitmapInfo(img)&byteOrderMask == byteOrder32Little
	for y := range min(h, out.Bounds().Dy()) {
		row := src[y*stride:]
		for x := range min(w, out.Bounds().Dx()) {
			p, o := row[x*4:x*4+4], out.Pix[y*out.Stride+x*4:]
			if bgra {
				o[0], o[1], o[2] = p[2], p[1], p[0]
			} else {
				o[0], o[1], o[2] = p[0], p[1], p[2]
			}
		}
	}
	return out, nil
}

// PointerLocation is where the pointer is, in points from the main screen's
// top left.
func PointerLocation() (Point, error) {
	if err := loadScreenshots(); err != nil {
		return Point{}, err
	}
	ev := cgEventCreate(0)
	if ev == 0 {
		return Point{}, errors.New("cannot ask where the pointer is")
	}
	defer cfRelease(ev)
	return cgEventGetLocation(ev), nil
}

// cPointer is a pointer to memory CoreFoundation owns, handed back as an
// address.
func cPointer(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}
