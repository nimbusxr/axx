package webcore

import (
	"io"
	"os"
	"path/filepath"
)

// interrupt ends a process: Windows has no signal that asks.
func interrupt(p *os.Process) { _ = p.Kill() }

// launchConfig gives Playwright's server its options, which have the
// address the guard reaches it at. Windows has no file that is a process's
// standard input, so they are in a file of the browser's own folder until
// the server has read them.
func launchConfig(dir string, data []byte) (path string, stdin io.Reader, cleanup func(), err error) {
	path = filepath.Join(dir, "launch.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", nil, nil, err
	}
	return path, nil, func() { _ = os.Remove(path) }, nil
}
