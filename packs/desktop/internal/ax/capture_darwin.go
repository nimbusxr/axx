//go:build darwin

package ax

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

var (
	captureOnce    sync.Once
	captureErr     error
	capturePreflit func() bool
)

// ScreenCaptureAllowed reports whether macOS lets this process capture the
// screen with other apps' windows on it: the app that started it is allowed
// in System Settings > Privacy & Security > Screen Recording. Without it, a
// capture shows the desktop's wallpaper, not the windows.
func ScreenCaptureAllowed() (bool, error) {
	captureOnce.Do(func() {
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			captureErr = fmt.Errorf("cannot load CoreGraphics: %w", err)
			return
		}
		purego.RegisterLibFunc(&capturePreflit, cg, "CGPreflightScreenCaptureAccess")
	})
	if captureErr != nil {
		return false, captureErr
	}
	return capturePreflit(), nil
}
