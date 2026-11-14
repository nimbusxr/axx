//go:build unix

package proc

import (
	"os"
	"os/exec"
	"testing"
)

// A process this one may not signal runs all the same: the first process of
// the system, which only root may signal. A process that has gone does not.
func TestAProcessThisOneMayNotSignalRuns(t *testing.T) {
	if !ProcessAlive(1) {
		t.Error("the system's first process: not running")
	}
	if !ProcessAlive(os.Getpid()) {
		t.Error("this process: not running")
	}
	gone := exec.Command(os.Args[0], "-test.run=^$")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	if ProcessAlive(gone.Process.Pid) {
		t.Error("a process that ended: running")
	}
	if ProcessAlive(0) || ProcessAlive(-1) {
		t.Error("no process: running")
	}
}
