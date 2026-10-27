//go:build !windows

package modelswindows

import (
	"runtime"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// speakWith is never reached off Windows: a windows registration does nothing there.
func speakWith(*core.Scenario, *desktopcore.Launch) error {
	return unsupported("Windows apps speak on Windows, not on %s", runtime.GOOS)
}

func restore(*core.Scenario) error { return nil }
