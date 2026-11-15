// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLock(f *os.File) error { return flock(f, unix.LOCK_EX) }

// tryShared takes the lock with others that take it shared, unless a process
// holds it alone.
func tryShared(f *os.File) error { return flock(f, unix.LOCK_SH) }

func flock(f *os.File, how int) error {
	err := unix.Flock(int(f.Fd()), how|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errBusy
	}
	return err
}

func unlockFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
