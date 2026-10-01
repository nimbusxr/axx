// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStepContextTimesOut(t *testing.T) {
	c := newStepContext(context.Background(), 30*time.Millisecond)
	defer c.cancel()
	if d, ok := c.Deadline(); !ok || time.Until(d) > 30*time.Millisecond {
		t.Errorf("deadline = %v, %v", d, ok)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("never timed out")
	}
	if !errors.Is(c.Err(), context.DeadlineExceeded) {
		t.Errorf("err = %v", c.Err())
	}
}

func TestStepContextHoldStopsTheClock(t *testing.T) {
	c := newStepContext(context.Background(), 50*time.Millisecond)
	defer c.cancel()
	release := c.hold()
	if _, ok := c.Deadline(); ok {
		t.Error("a held step reports a deadline")
	}
	select {
	case <-c.Done():
		t.Fatalf("ended while held: %v", c.Err())
	case <-time.After(150 * time.Millisecond):
	}
	release()
	release() // a second call does nothing
	if d, ok := c.Deadline(); !ok || time.Until(d) <= 0 {
		t.Errorf("deadline after the hold = %v, %v", d, ok)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("the clock never went on after the hold")
	}
	if !errors.Is(c.Err(), context.DeadlineExceeded) {
		t.Errorf("err = %v", c.Err())
	}
}

func TestStepContextParentEndsItWhileHeld(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	c := newStepContext(parent, time.Hour)
	defer c.cancel()
	defer c.hold()()
	cancel()
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("the parent's end did not end a held step")
	}
	if !errors.Is(c.Err(), context.Canceled) {
		t.Errorf("err = %v", c.Err())
	}
}

func TestStepContextParentTimeoutEndsItWhileHeld(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	c := newStepContext(parent, time.Hour)
	defer c.cancel()
	defer c.hold()()
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("the scenario's timeout did not end a held step")
	}
	if !errors.Is(c.Err(), context.DeadlineExceeded) {
		t.Errorf("err = %v", c.Err())
	}
}

func TestStepContextNestedHolds(t *testing.T) {
	c := newStepContext(context.Background(), 30*time.Millisecond)
	defer c.cancel()
	outer := c.hold()
	inner := c.hold()
	inner()
	select {
	case <-c.Done():
		t.Fatal("ended while the outer hold lasted")
	case <-time.After(100 * time.Millisecond):
	}
	outer()
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("never timed out after the holds")
	}
}
