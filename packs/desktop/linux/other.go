//go:build !linux

package desktoplinux

import (
	"context"
	"fmt"
	"runtime"

	"github.com/nimbusxr/axx/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// Claim is never reached off Linux: a linux registration does nothing
// there.
func (driver) Claim(*core.Scenario) (desktopcore.Desktop, error) {
	return nil, fmt.Errorf("linux apps run on Linux, not on %s", runtime.GOOS)
}

func checks() []core.Check {
	return []core.Check{{Name: "Linux desktops", Run: func(context.Context) core.CheckResult {
		return core.CheckResult{
			Status: core.CheckWarn, Detail: "Linux apps run on Linux only",
			Hint: "run the Linux scenarios on Linux (a CI runner, a container): elsewhere, their linux registrations do nothing",
		}
	}}}
}
