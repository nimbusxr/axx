//go:build darwin

package ax

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// cgRect is CGRect (NSRect).
type cgRect struct{ X, Y, Width, Height float64 }

var (
	appKitOnce sync.Once
	appKitErr  error
)

// VisibleFrame is the part of the screen at a point that windows show in:
// the screen but its menu bar and the Dock, in the accessibility tree's
// coordinates (points down from the top left of the main screen). A point on
// no screen gets the main screen's.
func VisibleFrame(at Point) (Point, Size, error) {
	appKitOnce.Do(func() {
		_, appKitErr = purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	})
	if appKitErr != nil {
		return Point{}, Size{}, fmt.Errorf("cannot load AppKit: %w", appKitErr)
	}
	// An autorelease pool belongs to the thread that made it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	screens := objc.ID(objc.GetClass("NSScreen")).Send(objc.RegisterName("screens"))
	n := objc.Send[uint](screens, objc.RegisterName("count"))
	if n == 0 {
		return Point{}, Size{}, fmt.Errorf("no screen")
	}
	screen := func(i uint) objc.ID { return screens.Send(objc.RegisterName("objectAtIndex:"), i) }
	frame := func(s objc.ID, which string) cgRect { return objc.Send[cgRect](s, objc.RegisterName(which)) }
	// Cocoa's y runs up from the bottom of the main screen (the first);
	// the tree's runs down from its top.
	mainHeight := frame(screen(0), "frame").Height
	pick := screen(0)
	for i := range n {
		f := frame(screen(i), "frame")
		top := mainHeight - (f.Y + f.Height)
		if at.X >= f.X && at.X < f.X+f.Width && at.Y >= top && at.Y < top+f.Height {
			pick = screen(i)
			break
		}
	}
	v := frame(pick, "visibleFrame")
	return Point{X: v.X, Y: mainHeight - (v.Y + v.Height)}, Size{Width: v.Width, Height: v.Height}, nil
}
