// SPDX-License-Identifier: Apache-2.0

//go:build windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryLock(f *os.File) error { return lockFile(f, windows.LOCKFILE_EXCLUSIVE_LOCK) }

// tryShared takes the lock with others that take it shared, unless a process
// holds it alone.
func tryShared(f *os.File) error { return lockFile(f, 0) }

func lockFile(f *os.File, how uint32) error {
	ol := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(f.Fd()), how|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return errBusy
	}
	return err
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}
