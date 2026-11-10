//go:build darwin

package desktopmacos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/launcher"
	"github.com/nimbusxr/axx/packs/desktop/internal/ax"
)

// The settings macOS keeps its permissions in, as macOS 27 names them, and
// as macOS names them before.
const (
	accessibilitySettings = "System Settings > Privacy & Security > Device Control and Data Access (Accessibility before macOS 27)"
	screenSettings        = "System Settings > Privacy & Security > Screen & System Audio Recording (Screen Recording before macOS 15)"
)

func checks() []core.Check {
	return []core.Check{
		{Name: "macOS launcher", Run: func(context.Context) core.CheckResult {
			if app := underLauncher(); app != "" {
				return core.CheckResult{Status: core.CheckOK, Detail: "axx runs under axx's launcher (" + app + "), the app macOS asks for its permissions"}
			}
			if os.Getenv(launcherEnv) == "off" {
				return core.CheckResult{Status: core.CheckOK, Detail: launcherEnv + "=off: axx runs with " + topApp() + "'s permissions"}
			}
			if _, err := launcher.ResponsibleFor(os.Getpid()); err != nil {
				return core.CheckResult{
					Status: core.CheckWarn, Detail: "axx cannot run under axx's launcher on this macOS: " + err.Error(),
					Hint: "axx runs with " + topApp() + "'s permissions",
				}
			}
			return core.CheckResult{
				Status: core.CheckOK, Detail: "axx's launcher is not installed: axx runs with " + topApp() + "'s permissions",
				Hint: "axx downloads it from its release the first time, and an axx built from source does not: set " + launcherEnv + " to the path of axx's launcher, axx.app, to run under it",
			}
		}},
		{Name: "macOS Accessibility", Run: func(context.Context) core.CheckResult {
			ok, err := ax.Trusted()
			switch {
			case err != nil:
				return core.CheckResult{Status: core.CheckFail, Detail: err.Error()}
			case !ok:
				if underLauncher() != "" {
					// macOS lists the launcher, turned off, for the person to turn on.
					_, _ = ax.AskTrust()
				}
				return core.CheckResult{
					Status: core.CheckFail, Detail: topApp() + " may not use the accessibility tree",
					Hint: "turn on " + topApp() + " in " + accessibilitySettings,
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
				if underLauncher() != "" {
					askScreenCapture()
				}
				return core.CheckResult{
					Status: core.CheckWarn, Detail: topApp() + " may not capture other apps' windows: screenshot steps fail",
					Hint: "turn on " + topApp() + " in " + screenSettings,
				}
			}
			return core.CheckResult{Status: core.CheckOK, Detail: topApp() + " may capture windows"}
		}},
	}
}

// askScreenCapture captures the screen once, which macOS refuses: what lists
// axx's launcher in its settings, for the person to turn on. Asking for it
// (CGRequestScreenCaptureAccess) does not, under the launcher (ADR 0016's
// evidence). The capture is removed.
func askScreenCapture() {
	dir, err := os.MkdirTemp("", "axx-ask-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(dir)
	_ = exec.CommandContext(context.Background(), "screencapture", "-x", filepath.Join(dir, "screen.png")).Run()
}
