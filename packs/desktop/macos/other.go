//go:build !darwin

package desktopmacos

import (
	"context"
	"fmt"
	"runtime"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// Claim is never reached off a Mac: a macos registration does nothing
// there.
func (driver) Claim(*core.Scenario) (desktopcore.Desktop, error) {
	return nil, fmt.Errorf("macOS apps run on a Mac, not on %s", runtime.GOOS)
}

func checks() []core.Check {
	return []core.Check{{Name: "macOS desktop", Run: func(context.Context) core.CheckResult {
		return core.CheckResult{
			Status: core.CheckWarn, Detail: "macOS apps run on macOS only",
			Hint: "run the macOS scenarios on a Mac: elsewhere, their macos registrations do nothing",
		}
	}}}
}
