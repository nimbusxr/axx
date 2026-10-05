//go:build !windows

package desktopwindows

import (
	"context"
	"fmt"
	"runtime"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// Claim is never reached off Windows: a windows registration does nothing
// there.
func (driver) Claim(*core.Scenario) (desktopcore.Desktop, error) {
	return nil, fmt.Errorf("windows apps run on Windows, not on %s", runtime.GOOS)
}

func checks() []core.Check {
	return []core.Check{{Name: "Windows desktop", Run: func(context.Context) core.CheckResult {
		return core.CheckResult{
			Status: core.CheckWarn, Detail: "Windows apps run on Windows only",
			Hint: "run the Windows scenarios on Windows: elsewhere, their windows registrations do nothing",
		}
	}}}
}
