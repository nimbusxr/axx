package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"time"
)

const (
	// defaultGrace is how long an app may take to exit after the stop signal.
	defaultGrace = 10 * time.Second
	// killWait bounds the wait for a killed group to disappear.
	killWait = 5 * time.Second
	// pollEvery is how often a stopping group is checked.
	pollEvery = 25 * time.Millisecond
)

// stopSignals are the signals apps.<name>.stop.signal may name.
var stopSignals = map[string]syscall.Signal{
	"SIGTERM": syscall.SIGTERM,
	"SIGINT":  syscall.SIGINT,
	"SIGHUP":  syscall.SIGHUP,
	"SIGQUIT": syscall.SIGQUIT,
	"SIGKILL": syscall.SIGKILL,
}

// canonicalSignal returns the stopSignals key for a signal name ("SIGTERM",
// "TERM", "sigint"; empty means SIGTERM). It does not check the name.
func canonicalSignal(name string) string {
	n := strings.ToUpper(strings.TrimSpace(name))
	if n == "" {
		return "SIGTERM"
	}
	if !strings.HasPrefix(n, "SIG") {
		n = "SIG" + n
	}
	return n
}

// checkSignal validates apps.<name>.stop.signal.
func checkSignal(name string) error {
	if _, ok := stopSignals[canonicalSignal(name)]; !ok {
		return fmt.Errorf("unsupported stop signal %q (use SIGTERM or SIGINT)", name)
	}
	return nil
}

// terminate stops a process group: it sends the stop signal, gives the group
// grace to exit, then kills whatever is left. A cancelled ctx skips the
// grace period.
func terminate(ctx context.Context, g *procGroup, signal string, grace time.Duration) error {
	if !g.alive() {
		return nil
	}
	if ctx.Err() == nil && g.interrupt(signal) && waitGone(ctx, g, grace) {
		return nil
	}
	if err := g.kill(); err != nil {
		return err
	}
	waitGone(context.WithoutCancel(ctx), g, killWait)
	return nil
}

// waitGone waits up to d for every process of g to exit.
func waitGone(ctx context.Context, g *procGroup, d time.Duration) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		if !g.alive() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return !g.alive()
		case <-tick.C:
		}
	}
}
