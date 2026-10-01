// SPDX-License-Identifier: Apache-2.0

package filelock

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestLockWaitsForAnotherProcess holds the lock in a child process (this test
// binary, re-run as the holder) and checks that Lock waits for it, says so,
// and gets the lock once the holder exits.
func TestLockWaitsForAnotherProcess(t *testing.T) {
	if path := os.Getenv("FILELOCK_HOLD"); path != "" {
		unlock, err := Lock(context.Background(), path, nil)
		if err != nil {
			os.Exit(2)
		}
		_ = os.WriteFile(path+".held", nil, 0o644)
		time.Sleep(700 * time.Millisecond)
		unlock()
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "sub", "build.lock")
	holder := exec.Command(os.Args[0], "-test.run=^TestLockWaitsForAnotherProcess$")
	holder.Env = append(os.Environ(), "FILELOCK_HOLD="+path)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Wait() }()
	for deadline := time.Now().Add(10 * time.Second); ; {
		if _, err := os.Stat(path + ".held"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the holder never took the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	waited := false
	start := time.Now()
	unlock, err := Lock(context.Background(), path, func() { waited = true })
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if !waited {
		t.Error("Lock did not say it was waiting for the holder")
	}
	if time.Since(start) < 200*time.Millisecond {
		t.Errorf("Lock returned after %s, while the holder still held it", time.Since(start))
	}
}

func TestLockGivesUpWhenTheContextEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build.lock")
	unlock, err := Lock(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	// A second lock in this process is refused on Windows and by flock on
	// another descriptor alike, so it waits until the context ends.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := Lock(ctx, path, nil); err == nil {
		t.Fatal("got a second lock while the first was held")
	}
}

func TestUnlockLetsTheNextOneIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build.lock")
	unlock, err := Lock(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	again, err := Lock(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestTryLockSaysWhenItIsHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device.lock")
	unlock, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("first TryLock: %v %v", ok, err)
	}
	if _, ok, err := TryLock(path); err != nil || ok {
		t.Fatalf("second TryLock while held: %v %v", ok, err)
	}
	unlock()
	again, ok, err := TryLock(path)
	if err != nil || !ok {
		t.Fatalf("TryLock after unlock: %v %v", ok, err)
	}
	again()
}
