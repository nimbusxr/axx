package lifecycle

import "syscall"

// setDeathSignal makes the kernel SIGKILL the child when axx dies, so a
// crashed or killed run does not leave apps behind.
func setDeathSignal(attr *syscall.SysProcAttr) { attr.Pdeathsig = syscall.SIGKILL }
