//go:build windows

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// comspec returns the command interpreter.
func comspec() string {
	if c := os.Getenv("ComSpec"); c != "" {
		return c
	}
	return "cmd.exe"
}

// shellArgv runs line through cmd.exe.
func shellArgv(line string) []string { return []string{comspec(), "/C", line} }

// setupCmd starts cmd in its own process group; newProcGroup then puts it in
// a Job Object.
func setupCmd(cmd *exec.Cmd) {
	attr := &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if a := cmd.Args; len(a) == 3 && a[0] == comspec() && a[1] == "/C" {
		// Hand the line to cmd.exe verbatim: with /S, cmd strips exactly the
		// outer quotes. Go's argv quoting would escape inner quotes with
		// backslashes, which cmd.exe does not understand.
		attr.CmdLine = syscall.EscapeArg(a[0]) + ` /S /C "` + a[2] + `"`
	}
	cmd.SysProcAttr = attr
}

// procGroup is the process tree of an app: a Job Object for apps this
// process started (closing it kills the tree, even if axx crashes), or a
// plain process handle for an app recorded in a state file.
type procGroup struct {
	pid  int
	job  windows.Handle
	proc windows.Handle
}

// jobAccounting mirrors JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// newProcGroup puts a freshly started command in a new Job Object that kills
// all its processes when the job is closed.
func newProcGroup(cmd *exec.Cmd) (*procGroup, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("open process %d: %w", cmd.Process.Pid, err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("assign process %d to job object: %w", cmd.Process.Pid, err)
	}
	return &procGroup{pid: cmd.Process.Pid, job: job}, nil
}

// openProcGroup opens a process recorded in a state file. Its children were
// in the recording run's Job Object and died when that run exited.
func openProcGroup(pid, _ int) (*procGroup, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("invalid process id %d", pid)
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	switch {
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return &procGroup{pid: pid}, nil // no such process: already gone
	case err != nil:
		return nil, fmt.Errorf("open process %d: %w", pid, err)
	}
	return &procGroup{pid: pid, proc: h}, nil
}

// ids returns the process id twice: Windows has no process group ids here.
func (g *procGroup) ids() (pid, pgid int) { return g.pid, g.pid }

// alive reports whether any process of the tree still runs.
func (g *procGroup) alive() bool {
	switch {
	case g.job != 0:
		var acct jobAccounting
		err := windows.QueryInformationJobObject(g.job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&acct)), uint32(unsafe.Sizeof(acct)), nil)
		return err == nil && acct.ActiveProcesses > 0
	case g.proc != 0:
		ev, err := windows.WaitForSingleObject(g.proc, 0)
		return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
	default:
		return false
	}
}

// interrupt cannot ask a process tree to stop on Windows (console apps
// have no SIGTERM, and CTRL_BREAK makes a JVM print a thread dump instead of
// exiting), so the tree is terminated right away.
func (g *procGroup) interrupt(string) bool { return false }

// kill terminates every process of the tree.
func (g *procGroup) kill() error {
	switch {
	case g.job != 0:
		return windows.TerminateJobObject(g.job, 1)
	case g.proc != 0:
		return windows.TerminateProcess(g.proc, 1)
	default:
		return nil
	}
}

// release closes the handles (closing the job kills what is left of it).
func (g *procGroup) release() {
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
	if g.proc != 0 {
		_ = windows.CloseHandle(g.proc)
		g.proc = 0
	}
}

// processAlive reports whether a process with this pid is running.
func processAlive(pid int) bool {
	g, err := openProcGroup(pid, pid)
	if err != nil {
		return false
	}
	defer g.release()
	return g.alive()
}
