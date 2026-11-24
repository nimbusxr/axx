package recording

import (
	"sync"

	"github.com/nimbusxr/axx/core"
)

// A scenario's screen is also for what shows the run to people in a picture
// of its own: the demo pack films a stage, and a browser or a phone is a part
// of its picture. Such a pack watches the screens (Watch); a pack that has a
// screen says so (Screen) whenever one is watched (Watched), whatever its own
// videos setting says. The screens are named by what they are: "browser",
// "phone", "desktop".

var watching struct {
	sync.Mutex
	wants  func(sc *core.Scenario) bool
	screen func(sc *core.Scenario, surface string, look Look)
}

// Watch has screen told of every screen of the scenarios wants says yes to.
// There is one watcher: the last one set. Nil for both stops it.
func Watch(wants func(sc *core.Scenario) bool, screen func(sc *core.Scenario, surface string, look Look)) {
	watching.Lock()
	watching.wants, watching.screen = wants, screen
	watching.Unlock()
}

// Watched reports whether the scenario's screens are watched: a pack then
// has its screen ready, as for a video.
func Watched(sc *core.Scenario) bool {
	watching.Lock()
	wants := watching.wants
	watching.Unlock()
	return wants != nil && wants(sc)
}

// Screen says the scenario has a screen of that kind, which look sees. It
// does nothing for a scenario nobody watches.
func Screen(sc *core.Scenario, surface string, look Look) {
	watching.Lock()
	wants, screen := watching.wants, watching.screen
	watching.Unlock()
	if wants != nil && screen != nil && wants(sc) {
		screen(sc, surface, look)
	}
}
