//go:build linux

package desktoplinux

import (
	"context"
	"os/exec"

	"github.com/nimbusxr/axx/core"
)

func checks() []core.Check {
	need := func(name, tool, hint string) core.Check {
		return core.Check{Name: name, Run: func(context.Context) core.CheckResult {
			if _, err := exec.LookPath(tool); err != nil {
				return core.CheckResult{Status: core.CheckFail, Detail: "no " + tool, Hint: hint}
			}
			return core.CheckResult{Status: core.CheckOK, Detail: tool + " is installed"}
		}}
	}
	return []core.Check{
		need("Xvfb", "Xvfb", "install it (Debian and Ubuntu: apt install xvfb): it runs axx's desktops"),
		need("D-Bus", "dbus-daemon", "install it (Debian and Ubuntu: apt install dbus-daemon): each scenario has a session bus of its own"),
		{Name: "at-spi2-core", Run: func(context.Context) core.CheckResult {
			if _, err := busLauncher(); err != nil {
				return core.CheckResult{Status: core.CheckFail, Detail: err.Error(), Hint: "apt install at-spi2-core"}
			}
			return core.CheckResult{Status: core.CheckOK, Detail: "at-spi2-core is installed"}
		}},
		{Name: "user and mount namespaces", Run: func(context.Context) core.CheckResult {
			if err := probeNamespaces(); err != nil {
				return core.CheckResult{
					Status: core.CheckWarn, Detail: "this host does not give user and mount namespaces: Linux desktop scenarios run one at a time (" + err.Error() + ")",
					Hint: "for more than one desktop (packs." + Name + ".desktops), allow them: a container's seccomp profile (Docker: --security-opt seccomp=unconfined), or Ubuntu's AppArmor restriction (kernel.apparmor_restrict_unprivileged_userns=0)",
				}
			}
			return core.CheckResult{Status: core.CheckOK, Detail: "user and mount namespaces are given: more than one desktop can run at once"}
		}},
		{Name: "java-atk-wrapper", Run: func(context.Context) core.CheckResult {
			if !exists(atkWrapper) {
				return core.CheckResult{
					Status: core.CheckWarn, Detail: "no java-atk-wrapper: Java apps cannot be read",
					Hint: "for Java apps, install it (Debian and Ubuntu: apt install libatk-wrapper-java-jni)",
				}
			}
			return core.CheckResult{Status: core.CheckOK, Detail: "java-atk-wrapper is installed"}
		}},
	}
}
