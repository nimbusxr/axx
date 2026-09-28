//go:build unix

package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// Setup makes cmd the leader of a new process group, so that stopping
// the app also stops everything it spawned. On Linux the child is also
// killed if axx itself dies.
func Setup(cmd *exec.Cmd) {
	attr := &syscall.SysProcAttr{Setpgid: true}
	setDeathSignal(attr)
	cmd.SysProcAttr = attr
}

// ShellArgv runs line through the POSIX shell.
func ShellArgv(line string) []string { return []string{"/bin/sh", "-c", line} }

// Group is the process group of an app.
type Group struct {
	pid  int
	pgid int
}

// NewGroup returns the group led by a freshly started command.
func NewGroup(cmd *exec.Cmd) (*Group, error) {
	pid := cmd.Process.Pid
	return &Group{pid: pid, pgid: pid}, nil
}

// OpenGroup returns a group recorded in a state file.
func OpenGroup(pid, pgid int) (*Group, error) {
	if pgid == 0 {
		pgid = pid
	}
	// Never let a corrupt state file turn into kill(-1) or kill(0).
	if pgid <= 1 {
		return nil, fmt.Errorf("invalid process group %d", pgid)
	}
	return &Group{pid: pid, pgid: pgid}, nil
}

// ids returns the leader's pid and the group id.
func (g *Group) IDs() (pid, pgid int) { return g.pid, g.pgid }

// alive reports whether any process of the group still runs (zombies do
// not count).
func (g *Group) Alive() bool {
	return syscall.Kill(-g.pgid, 0) == nil && groupHasLiveMember(g.pgid)
}

// interrupt sends the named stop signal (SIGTERM if the name is unknown) to
// every process of the group and reports whether that worked.
func (g *Group) Interrupt(name string) bool {
	sig, ok := stopSignals[CanonicalSignal(name)]
	if !ok {
		sig = syscall.SIGTERM
	}
	return ignoreGone(syscall.Kill(-g.pgid, sig)) == nil
}

// kill sends SIGKILL to every process of the group.
func (g *Group) Kill() error { return ignoreGone(syscall.Kill(-g.pgid, syscall.SIGKILL)) }

// release frees OS resources held for the group.
func (g *Group) Release() {}

// ProcessAlive reports whether a process with this pid runs.
func ProcessAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil && !isZombie(pid)
}

func ignoreGone(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
