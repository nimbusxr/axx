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
// grace to exit, then kills whatever is left, which it reports (killed).
// Signalled is false when it could send no stop signal, and ended the group
// at once: Windows has none for a process. A cancelled ctx skips the grace
// period.
func terminate(ctx context.Context, g *proc.Group, signal string, grace time.Duration) (killed, signalled bool, err error) {
	if !g.Alive() {
		return false, true, nil
	}
	signalled = ctx.Err() == nil && g.Interrupt(signal)
	if signalled && waitGone(ctx, g, grace) {
		return false, true, nil
	}
	if err := g.Kill(); err != nil {
		return true, signalled, err
	}
	waitGone(context.WithoutCancel(ctx), g, killWait)
	return true, signalled, nil
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
