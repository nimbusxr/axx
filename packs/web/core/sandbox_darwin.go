package webcore

import (
	"errors"

	"golang.org/x/sys/unix"
)

// fullChromiumRefused says why Playwright's full Chromium cannot start on this
// machine, or "" when it can. As it starts, the full Chromium asks macOS
// whether it runs in a virtual machine (the kern.hv_vmm_present sysctl), and
// quits when a sandbox refuses the answer, as the sandbox of agent harnesses
// such as Claude Code's does. Playwright's headless shell does not ask.
func fullChromiumRefused() string {
	if _, err := unix.SysctlUint32("kern.hv_vmm_present"); errors.Is(err, unix.EPERM) {
		return "this sandbox refuses a read that the full Chromium makes as it starts (kern.hv_vmm_present)"
	}
	return ""
}
