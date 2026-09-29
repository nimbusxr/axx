package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Spec is a command to run to completion.
type Spec struct {
	// Argv is the program and its arguments; no shell is involved.
	Argv []string
	// Dir is the working directory ("" is the current one).
	Dir string
	// Env is the full environment (nil is this process's).
	Env []string
	// Stdin is the command's input (nil is none, never this process's own).
	Stdin []byte
	// Timeout stops the command when it runs longer (0: until ctx ends).
	Timeout time.Duration
	// Limit is how many bytes of each stream are kept (0: 8 MiB).
	Limit int
}

// Result is how a command ended and what it wrote.
type Result struct {
	// ExitCode is the command's exit code: -1 when it was stopped.
	ExitCode       int
	Stdout, Stderr []byte
	// Truncated says a stream wrote more than the limit kept.
	Truncated bool
	// TimedOut says the command ran past its timeout and was stopped.
	TimedOut bool
	Duration time.Duration
}

const (
	defaultLimit = 8 << 20
	// stopGrace is how long a stopped command may take to exit before its
	// group is killed.
	stopGrace = 2 * time.Second
	// pipesGrace bounds the wait for pipes a process the command left
	// behind keeps open.
	pipesGrace = 2 * time.Second
)

// Run runs a command in a process group of its own and waits for it. A
// command that runs past its timeout, or whose ctx ends, is interrupted,
// given a moment, then killed with everything it started; whatever it left
// running when it exited is killed too. The error is only for a command
// that could not start, or whose ctx ended; an exit code, even a failing
// one, is the Result's.
func Run(ctx context.Context, s Spec) (*Result, error) {
	if len(s.Argv) == 0 {
		return nil, errors.New("no command to run")
	}
	limit := s.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	stdout, stderr := &capped{max: limit}, &capped{max: limit}
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...) //nolint:noctx // stopped below, with everything it started, when ctx ends
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdin = bytes.NewReader(s.Stdin)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = pipesGrace
	Setup(cmd)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	g, err := NewGroup(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	defer g.Release()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var timeout <-chan time.Time
	if s.Timeout > 0 {
		t := time.NewTimer(s.Timeout)
		defer t.Stop()
		timeout = t.C
	}
	res := &Result{}
	var ended error
	select {
	case <-done:
	case <-timeout:
		res.TimedOut = true
		stop(g, done)
	case <-ctx.Done():
		stop(g, done)
		ended = ctx.Err()
	}
	// What the command started and left running goes with it, and is gone
	// when Run returns: a folder it ran in can be removed.
	if g.Alive() {
		_ = g.KillAndWait(stopGrace)
	}
	res.Duration = time.Since(start)
	res.Stdout, res.Stderr = stdout.bytes(), stderr.bytes()
	res.Truncated = stdout.cut || stderr.cut
	res.ExitCode = -1
	if cmd.ProcessState != nil && !res.TimedOut && ended == nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ended != nil {
		return res, fmt.Errorf("the command was stopped: %w", ended)
	}
	return res, nil
}

// stop interrupts the command's group, gives it stopGrace to exit, then
// kills it.
func stop(g *Group, done <-chan error) {
	if g.Interrupt("SIGTERM") {
		select {
		case <-done:
			return
		case <-time.After(stopGrace):
		}
	}
	_ = g.Kill()
	<-done
}

// capped keeps the first max bytes written to it.
type capped struct {
	mu  sync.Mutex
	b   bytes.Buffer
	max int
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.max - c.b.Len(); len(p) > room {
		c.b.Write(p[:max(room, 0)])
		c.cut = true
		return len(p), nil
	}
	c.b.Write(p)
	return len(p), nil
}

func (c *capped) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Clone(c.b.Bytes())
}
