// SPDX-License-Identifier: Apache-2.0

// Package filelock holds an exclusive lock on a file across processes. The
// lock goes with the process that holds it, so a killed process never leaves
// it held. Whether a process holds one is a question of the file alone
// (Held): it has an answer where a process may not signal the holder, or
// cannot see it, as in a sandbox.
package filelock

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// errBusy is what tryLock returns while another process holds the lock.
var errBusy = errors.New("locked by another process")

// TryLock locks path, creating it and its directory, unless another process
// holds it: then ok is false.
func TryLock(path string) (unlock func(), ok bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := tryLock(f); err != nil {
		_ = f.Close()
		if errors.Is(err, errBusy) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		_ = unlockFile(f)
		_ = f.Close()
	}, true, nil
}

// Lock locks path, creating it and its directory, and returns the function
// that unlocks it. While another process holds the lock it calls waiting
// once (when not nil) and waits, until ctx ends.
func Lock(ctx context.Context, path string, waiting func()) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	unlock := func() {
		_ = unlockFile(f)
		_ = f.Close()
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for told := false; ; {
		err := tryLock(f)
		if err == nil {
			return unlock, nil
		}
		if !errors.Is(err, errBusy) {
			_ = f.Close()
			return nil, err
		}
		if !told && waiting != nil {
			waiting()
			told = true
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

// Held reports whether a process holds the lock on path now: a file that is
// not there is not locked. It creates nothing, needs only to read the file,
// and takes nothing another Held waits for.
func Held(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err := tryShared(f); err != nil {
		if errors.Is(err, errBusy) {
			return true, nil
		}
		return false, err
	}
	_ = unlockFile(f)
	return false, nil
}
