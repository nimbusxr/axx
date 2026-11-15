//go:build unix

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts cmd in its own session so it survives this process.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func signalTerm(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscall.SIGTERM)
	}
}
