//go:build darwin && integration

package ax

import (
	"testing"
	"time"

	"github.com/ebitengine/purego"
)

// TestModifiersComeUp presses F13, a key no app answers, with sets of
// modifiers, and checks the system holds none of them after it: a modifier
// left down holds for every click and key after it (with Control, a click is
// a right click, and macOS's region capture copies to the clipboard).
func TestModifiersComeUp(t *testing.T) {
	if err := loadInput(); err != nil {
		t.Fatal(err)
	}
	trusted, err := Trusted()
	if err != nil {
		t.Fatal(err)
	}
	if !trusted {
		t.Fatal("this process may not post input: allow the app that runs it in System Settings > Privacy & Security > Accessibility")
	}
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var flagsState func(state int32) uint64
	purego.RegisterLibFunc(&flagsState, cg, "CGEventSourceFlagsState")
	const hidSystemState, f13 = 1, 105
	const all = flagShift | flagControl | flagOption | flagCommand
	for _, flags := range []uint64{flagShift, flagControl | flagShift, flagCommand, flagOption | flagCommand, all} {
		if err := stroke(f13, flags, nil, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
		if held := flagsState(hidSystemState) & all; held != 0 {
			t.Errorf("after F13 with the modifiers %#x, the system holds %#x", flags, held)
		}
	}
}
