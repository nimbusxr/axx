//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
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

// ShellArgv runs line through cmd.exe.
func ShellArgv(line string) []string { return []string{comspec(), "/C", line} }

// Setup starts cmd in its own process group; NewGroup then puts it in
// a Job Object.
func Setup(cmd *exec.Cmd) {
	attr := &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if a := cmd.Args; len(a) == 3 && a[0] == comspec() && a[1] == "/C" {
		// Hand the line to cmd.exe verbatim: with /S, cmd strips exactly the
		// outer quotes. Go's argv quoting would escape inner quotes with
		// backslashes, which cmd.exe does not understand.
		attr.CmdLine = syscall.EscapeArg(a[0]) + ` /S /C "` + a[2] + `"`
	}
	cmd.SysProcAttr = attr
}

// Group is the process tree of an app: a Job Object for apps this
// process started (closing it kills the tree, even if axx crashes), or a
// plain process handle for an app recorded in a state file.
type Group struct {
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

// NewGroup puts a freshly started command in a new Job Object that kills
// all its processes when the job is closed.
func NewGroup(cmd *exec.Cmd) (*Group, error) {
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
	return &Group{pid: cmd.Process.Pid, job: job}, nil
}

// OpenGroup opens a process recorded in a state file. Its children were
// in the recording run's Job Object and died when that run exited.
func OpenGroup(pid, _ int) (*Group, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("invalid process id %d", pid)
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	switch {
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return &Group{pid: pid}, nil // no such process: already gone
	case err != nil:
		return nil, fmt.Errorf("open process %d: %w", pid, err)
	}
	return &Group{pid: pid, proc: h}, nil
}

// ids returns the process id twice: Windows has no process group ids here.
func (g *Group) IDs() (pid, pgid int) { return g.pid, g.pid }

// alive reports whether any process of the tree still runs.
func (g *Group) Alive() bool {
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
func (g *Group) Interrupt(string) bool { return false }

// kill terminates every process of the tree.
func (g *Group) Kill() error {
	switch {
	case g.job != 0:
		return windows.TerminateJobObject(g.job, 1)
	case g.proc != 0:
		return windows.TerminateProcess(g.proc, 1)
	default:
		return nil
	}
}

// jobProcesses mirrors JOBOBJECT_BASIC_PROCESS_ID_LIST, with room for 256
// processes.
type jobProcesses struct {
	Assigned uint32
	Listed   uint32
	IDs      [256]uintptr
}

// KillAndWait terminates every process of the tree, and waits up to timeout
// until each has ended. The job counts a process out before its handles,
// such as the one on its working folder, are closed; its process handle is
// signaled only after that.
func (g *Group) KillAndWait(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	if g.job == 0 {
		err := g.Kill()
		if g.proc != 0 {
			_, _ = windows.WaitForSingleObject(g.proc, millis(time.Until(deadline)))
		}
		return err
	}
	for {
		var list jobProcesses
		if err := windows.QueryInformationJobObject(g.job, windows.JobObjectBasicProcessIdList,
			uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil && !errors.Is(err, windows.ERROR_MORE_DATA) {
			return errors.Join(fmt.Errorf("list the job's processes: %w", err), g.Kill())
		}
		n := min(int(list.Listed), len(list.IDs))
		// The handles are opened before the processes end, so that their
		// ends can be waited for.
		handles := make([]windows.Handle, 0, n)
		for _, id := range list.IDs[:n] {
			if h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(id)); err == nil {
				handles = append(handles, h)
			}
		}
		err := g.Kill()
		for _, h := range handles {
			_, _ = windows.WaitForSingleObject(h, millis(time.Until(deadline)))
			_ = windows.CloseHandle(h)
		}
		// A process started while the list was read is waited for next.
		if n == 0 || !g.Alive() || time.Now().After(deadline) {
			return err
		}
	}
}

func millis(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	return uint32(d.Milliseconds())
}

// release closes the handles (closing the job kills what is left of it).
func (g *Group) Release() {
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
	if g.proc != 0 {
		_ = windows.CloseHandle(g.proc)
		g.proc = 0
	}
}

// ProcessAlive reports whether a process with this pid is running.
func ProcessAlive(pid int) bool {
	g, err := OpenGroup(pid, pid)
	if err != nil {
		return false
	}
	defer g.Release()
	return g.Alive()
}
