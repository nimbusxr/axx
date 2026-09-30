package mobileios

import (
	"context"
	"os/exec"
	"runtime"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// checks are what `axx doctor` checks for the pack: Xcode, and an iOS
// runtime for its simulators.
func checks() []core.Check { return []core.Check{{Name: "Xcode", Run: checkXcode}} }

func checkXcode(ctx context.Context) core.CheckResult {
	if runtime.GOOS != "darwin" {
		return core.CheckResult{
			Status: core.CheckWarn, Detail: "iOS simulators run on macOS only",
			Hint: `run the iOS scenarios on a Mac: elsewhere, leave them out (like --tags "not @ios")`,
		}
	}
	out, err := exec.CommandContext(ctx, "xcodebuild", "-version").Output()
	if err != nil {
		return core.CheckResult{
			Status: core.CheckFail, Detail: "Xcode is not found: " + err.Error(),
			Hint: "install Xcode (the App Store), then sudo xcode-select -s /Applications/Xcode.app",
		}
	}
	xcode, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	rts, err := iosRuntimes(ctx)
	if err != nil {
		return core.CheckResult{Status: core.CheckFail, Detail: xcode + "; its simulators cannot be listed: " + err.Error()}
	}
	if len(rts) == 0 {
		return core.CheckResult{
			Status: core.CheckFail, Detail: xcode + "; no iOS runtime for simulators",
			Hint: "xcodebuild -downloadPlatform iOS, or Xcode's Settings, Components",
		}
	}
	var names []string
	for _, r := range rts {
		names = append(names, r.Name)
	}
	return core.CheckResult{Status: core.CheckOK, Detail: xcode + "; " + strings.Join(names, ", ")}
}
