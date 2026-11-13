package lifecycle

import (
	"context"
	"time"

	"github.com/nimbusxr/axx/internal/proc"
)

const (
	// defaultGrace is how long a service may take to exit after the stop signal.
	defaultGrace = 10 * time.Second
	// killWait bounds the wait for a killed group to disappear.
	killWait = 5 * time.Second
	// pollEvery is how often a stopping group is checked.
	pollEvery = 25 * time.Millisecond
)

// terminate stops a process group: it sends the stop signal, gives the group
// grace to exit, then kills whatever is left, which it reports. A cancelled
// ctx skips the grace period.
func terminate(ctx context.Context, g *proc.Group, signal string, grace time.Duration) (killed bool, err error) {
	if !g.Alive() {
		return false, nil
	}
	if ctx.Err() == nil && g.Interrupt(signal) && waitGone(ctx, g, grace) {
		return false, nil
	}
	if err := g.Kill(); err != nil {
		return true, err
	}
	waitGone(context.WithoutCancel(ctx), g, killWait)
	return true, nil
}

// waitGone waits up to d for every process of g to exit.
func waitGone(ctx context.Context, g *proc.Group, d time.Duration) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		if !g.Alive() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return !g.Alive()
		case <-tick.C:
		}
	}
}
