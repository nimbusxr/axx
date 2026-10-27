//go:build !windows

package modelswindows

import (
	"runtime"
	"sync"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
)

// changes are none off Windows: a windows registration does nothing there.
type changes struct{ mu sync.Mutex }

func (*changes) restore() error { return nil }

func speakWith(*core.Scenario, *changes, *desktopcore.Launch) error {
	return unsupported("Windows apps speak on Windows, not on %s", runtime.GOOS)
}

func answerAI(_ *core.Scenario, _ *changes, _ *desktopcore.Launch, stubs []*platform.Artifact) error {
	if len(stubs) == 0 {
		return nil
	}
	return unsupported("Windows apps call the Windows AI APIs on Windows, not on %s", runtime.GOOS)
}
