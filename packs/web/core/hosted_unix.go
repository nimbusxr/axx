//go:build !windows

package webcore

import (
	"bytes"
	"io"
	"os"
	"syscall"
)

// interrupt asks a process to end.
func interrupt(p *os.Process) { _ = p.Signal(syscall.SIGTERM) }

// launchConfig gives Playwright's server its options, which have the
// address the guard reaches it at: through its standard input, never in a
// file. A run's agent is the same user, and reads that user's files; the
// address would take it to Playwright's server past the guard.
func launchConfig(_ string, data []byte) (path string, stdin io.Reader, cleanup func(), err error) {
	return "/dev/stdin", bytes.NewReader(data), func() {}, nil
}
