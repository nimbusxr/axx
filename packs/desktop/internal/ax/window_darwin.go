//go:build darwin

package ax

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

var (
	windowOnce sync.Once
	windowErr  error
	getWindow  func(el ref, id *uint32) int32
)

// WindowID is the window server's number for a window element (its
// CGWindowID), which screencapture -l captures. ApplicationServices has the
// call, unpublished, as every tool that captures one app's window uses it.
func (e *Element) WindowID() (uint32, error) {
	windowOnce.Do(func() {
		if windowErr = load(); windowErr != nil {
			return
		}
		as, err := purego.Dlopen("/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			windowErr = fmt.Errorf("cannot load ApplicationServices: %w", err)
			return
		}
		purego.RegisterLibFunc(&getWindow, as, "_AXUIElementGetWindow")
	})
	if windowErr != nil {
		return 0, windowErr
	}
	var id uint32
	code := getWindow(e.ref, &id)
	runtime.KeepAlive(e)
	if code != axSuccess {
		return 0, &Error{Call: "the window's number", Code: code}
	}
	return id, nil
}

// Walk visits the element and those under it, in the tree's order, until
// visit returns false; it reports whether it went through them all.
func Walk(e *Element, visit func(*Element) bool) bool {
	if !visit(e) {
		return false
	}
	kids, _ := e.Children()
	for _, k := range kids {
		if !Walk(k, visit) {
			return false
		}
	}
	return true
}
