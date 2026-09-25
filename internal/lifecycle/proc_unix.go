//go:build unix

package lifecycle

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// setupCmd makes cmd the leader of a new process group, so that stopping
// the app also stops everything it spawned. On Linux the child is also
// killed if axx itself dies.
func setupCmd(cmd *exec.Cmd) {
	attr := &syscall.SysProcAttr{Setpgid: true}
	setDeathSignal(attr)
	cmd.SysProcAttr = attr
}

// shellArgv runs line through the POSIX shell.
func shellArgv(line string) []string { return []string{"/bin/sh", "-c", line} }

// procGroup is the process group of an app.
type procGroup struct {
	pid  int
	pgid int
}

// newProcGroup returns the group led by a freshly started command.
func newProcGroup(cmd *exec.Cmd) (*procGroup, error) {
	pid := cmd.Process.Pid
	return &procGroup{pid: pid, pgid: pid}, nil
}

// openProcGroup returns a group recorded in a state file.
func openProcGroup(pid, pgid int) (*procGroup, error) {
	if pgid == 0 {
		pgid = pid
	}
	// Never let a corrupt state file turn into kill(-1) or kill(0).
	if pgid <= 1 {
		return nil, fmt.Errorf("invalid process group %d", pgid)
	}
	return &procGroup{pid: pid, pgid: pgid}, nil
}

// ids returns the leader's pid and the group id.
func (g *procGroup) ids() (pid, pgid int) { return g.pid, g.pgid }

// alive reports whether any process of the group still runs (zombies do
// not count).
func (g *procGroup) alive() bool {
	return syscall.Kill(-g.pgid, 0) == nil && groupHasLiveMember(g.pgid)
}

// interrupt sends the named stop signal (SIGTERM if the name is unknown) to
// every process of the group and reports whether that worked.
func (g *procGroup) interrupt(name string) bool {
	sig, ok := stopSignals[canonicalSignal(name)]
	if !ok {
		sig = syscall.SIGTERM
	}
	return ignoreGone(syscall.Kill(-g.pgid, sig)) == nil
}

// kill sends SIGKILL to every process of the group.
func (g *procGroup) kill() error { return ignoreGone(syscall.Kill(-g.pgid, syscall.SIGKILL)) }

// release frees OS resources held for the group.
func (g *procGroup) release() {}

// processAlive reports whether a process with this pid runs.
func processAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil && !isZombie(pid)
}

func ignoreGone(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
