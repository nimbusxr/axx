// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"context"
	"sync"
	"time"
)

// stepContext is the context of a timed step or hook: done when its time is
// up or its parent is done. Its clock stops while it is held (core's
// Scenario.Hold), so time spent waiting on what the scenarios share does not
// count; the same context goes on, and code that holds it sees its deadline
// move. While held it reports its parent's deadline, or none.
type stepContext struct {
	parent context.Context
	done   chan struct{}

	mu        sync.Mutex
	err       error
	remaining time.Duration
	deadline  time.Time
	holds     int
	timer     *time.Timer
}

func newStepContext(parent context.Context, limit time.Duration) *stepContext {
	c := &stepContext{parent: parent, done: make(chan struct{}), remaining: limit}
	c.mu.Lock()
	c.startLocked()
	c.mu.Unlock()
	go func() {
		select {
		case <-parent.Done():
			c.finish(parent.Err(), false)
		case <-c.done:
		}
	}()
	return c
}

// startLocked runs the clock for what remains. The caller holds c.mu.
func (c *stepContext) startLocked() {
	if c.remaining < 0 {
		c.remaining = 0
	}
	c.deadline = time.Now().Add(c.remaining)
	c.timer = time.AfterFunc(c.remaining, func() { c.finish(context.DeadlineExceeded, true) })
}

// finish ends the context with err, unless it has ended; own is true when
// the step's own time is up, which a hold puts off.
func (c *stepContext) finish(err error, own bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil || (own && c.holds > 0) {
		return
	}
	c.err = err
	if c.timer != nil {
		c.timer.Stop()
	}
	close(c.done)
}

// hold stops the clock until the function it returns is called.
func (c *stepContext) hold() func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return func() {}
	}
	if c.holds == 0 {
		if c.timer.Stop() {
			c.remaining = time.Until(c.deadline)
		} else {
			c.remaining = 0 // the time was up as the hold began
		}
	}
	c.holds++
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.holds--
			if c.holds == 0 && c.err == nil {
				c.startLocked()
			}
		})
	}
}

// cancel ends the context once its step is over.
func (c *stepContext) cancel() { c.finish(context.Canceled, false) }

func (c *stepContext) Deadline() (time.Time, bool) {
	c.mu.Lock()
	held, d := c.holds > 0, c.deadline
	c.mu.Unlock()
	pd, ok := c.parent.Deadline()
	if held {
		return pd, ok
	}
	if ok && pd.Before(d) {
		return pd, true
	}
	return d, true
}

func (c *stepContext) Done() <-chan struct{} { return c.done }

func (c *stepContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *stepContext) Value(key any) any { return c.parent.Value(key) }
