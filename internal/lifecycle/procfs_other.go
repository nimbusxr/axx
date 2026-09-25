//go:build unix && !linux

package lifecycle

// groupHasLiveMember defers to kill(-pgid, 0): without /proc there is no
// cheap way to tell zombies apart, and outside Linux containers orphans are
// reaped by launchd or init.
func groupHasLiveMember(int) bool { return true }

// isZombie cannot tell without /proc.
func isZombie(int) bool { return false }
