//go:build darwin

package desktopmacos

import (
	"context"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/ax"
)

func checks() []core.Check {
	return []core.Check{
		{Name: "macOS Accessibility", Run: func(context.Context) core.CheckResult {
			ok, err := ax.Trusted()
			switch {
			case err != nil:
				return core.CheckResult{Status: core.CheckFail, Detail: err.Error()}
			case !ok:
				return core.CheckResult{
					Status: core.CheckFail, Detail: topApp() + " may not use the accessibility tree",
					Hint: "allow " + topApp() + " in System Settings > Privacy & Security > Accessibility (Device Control & Data Access, on macOS 27)",
				}
			}
			return core.CheckResult{Status: core.CheckOK, Detail: topApp() + " may use the accessibility tree"}
		}},
		{Name: "macOS Screen Recording", Run: func(context.Context) core.CheckResult {
			ok, err := ax.ScreenCaptureAllowed()
			switch {
			case err != nil:
				return core.CheckResult{Status: core.CheckWarn, Detail: err.Error()}
			case !ok:
				return core.CheckResult{
					Status: core.CheckWarn, Detail: topApp() + " may not capture other apps' windows: screenshot steps fail",
					Hint: "allow " + topApp() + " in System Settings > Privacy & Security > Screen Recording",
				}
			}
			return core.CheckResult{Status: core.CheckOK, Detail: topApp() + " may capture windows"}
		}},
	}
}
