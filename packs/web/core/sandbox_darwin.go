package webcore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

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

// chromiumEnv is the environment of the full Chromium: axx's own, with
// MAC_CHROMIUM_TMPDIR set to axx's temp folder; nil to leave Chromium's as it
// is. As it starts, the full Chromium makes its profile's lock in a folder of
// macOS's per-user temp folder, whatever $TMPDIR says, unless
// MAC_CHROMIUM_TMPDIR names another; and a sandbox may keep it from writing
// there, as Claude Code's does. Outside a sandbox, axx's temp folder is that
// per-user folder, so nothing changes. The lock is a Unix socket in a folder
// of its own (com.google.chrome.for.testing.XXXXXX/SingletonSocket), and macOS
// takes a socket path of 103 bytes at most: a temp folder too long for it is
// left to Chromium's own.
func chromiumEnv() map[string]string {
	if os.Getenv("MAC_CHROMIUM_TMPDIR") != "" {
		return nil
	}
	dir := filepath.Clean(os.TempDir())
	if len(dir)+len("/com.google.chrome.for.testing.XXXXXX/SingletonSocket") > 103 {
		return nil
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	env["MAC_CHROMIUM_TMPDIR"] = dir
	return env
}
