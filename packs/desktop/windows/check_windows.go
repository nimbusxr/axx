//go:build windows

package desktopwindows

import (
	"context"
	"os/exec"
	"path/filepath"

	"github.com/nimbusxr/axx/core"
)

func checks() []core.Check {
	return []core.Check{
		{Name: "Windows desktop", Run: func(context.Context) core.CheckResult {
			if ok, station := interactive(); !ok {
				return core.CheckResult{
					Status: core.CheckFail, Detail: "this session has no desktop (its window station is " + station + ")",
					Hint: "run axx on the signed-in user's desktop: a terminal there, or a scheduled task for that user (schtasks /it); not as a service or over SSH",
				}
			}
			return core.CheckResult{Status: core.CheckOK, Detail: "this session has the user's desktop"}
		}},
		{Name: "Java Access Bridge", Run: func(context.Context) core.CheckResult {
			if java, err := exec.LookPath("java"); err == nil && exists(filepath.Join(filepath.Dir(java), "WindowsAccessBridge-64.dll")) {
				return core.CheckResult{Status: core.CheckOK, Detail: "the Java on PATH has the Java Access Bridge"}
			}
			return core.CheckResult{
				Status: core.CheckWarn, Detail: "no Java with the Java Access Bridge on PATH: Java apps cannot be read",
				Hint: "for Java apps, run them with a JDK's java.exe (its bin has WindowsAccessBridge-64.dll)",
			}
		}},
	}
}
