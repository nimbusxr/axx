// SPDX-License-Identifier: Apache-2.0

// Package filelock holds an exclusive lock on a file across processes. The
// lock goes with the process that holds it, so a killed process never leaves
// it held.
package filelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// errBusy is what tryLock returns while another process holds the lock.
var errBusy = errors.New("locked by another process")

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
