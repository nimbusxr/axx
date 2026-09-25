//go:build unix && !linux

package lifecycle

import "syscall"

// setDeathSignal is a no-op: only Linux can tie a child's life to its
// parent. Elsewhere `axx down` reaps apps left behind (see Reap).
func setDeathSignal(*syscall.SysProcAttr) {}
