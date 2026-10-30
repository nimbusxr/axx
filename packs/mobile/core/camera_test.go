package mobilecore

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// A phone kept to a speed is somewhere along its path from the moment it sets off, a moment no
// time has passed included, though the path starts with the phone standing still: its time on
// the path goes on from 0 to the path's end, never NaN (a time no pose has).
func TestAPhoneAtASpeedIsOnItsPathFromTheStart(t *testing.T) {
	s, err := scene.Load(filepath.Join("..", "..", "..", "examples", "parcels", "acceptance", "scenes", "parcel-on-porch.glb"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Path("up the garden path")
	if err != nil {
		t.Fatal(err)
	}
	tm, err := pathTiming(s, p, &speed{metresPerSecond: 1.4})
	if err != nil {
		t.Fatal(err)
	}
	prev := 0.0
	for e := time.Duration(0); ; e += 10 * time.Millisecond {
		at, done := tm.at(e)
		if math.IsNaN(at) || at < prev || at > p.Duration {
			t.Fatalf("%s after setting off, the phone is at %v of the path's %v s (before, %v)", e, at, p.Duration, prev)
		}
		prev = at
		if done {
			break
		}
	}
}
