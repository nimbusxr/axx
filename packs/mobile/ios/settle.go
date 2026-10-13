package mobileios

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/npm"
)

// The simulator axx clones (templateOf) is booted once to set it up. Booted, iOS goes on for a
// minute or two with work it does once: its services' first start, their indexes and caches.
// Shut down at once, the simulator leaves that work to every clone, which does it on each boot:
// on a Mac with 16 cores, a clone's first minute took 431 processor-seconds (297 processes)
// cloned from a simulator shut down at once, and 250 (214) from one left to settle. Three clones
// booting at once on a Mac mini with 12 cores starved the rest of a run: an app's notification
// took half a minute to show. So the simulator axx clones is left booted until it comes to rest,
// once.

// How a simulator coming to rest is told: its processes take less than settleQuiet of processor
// time in settleEvery, after settleAtLeast; it is not waited for longer than settleWithin.
const (
	settleEvery   = 15 * time.Second
	settleQuiet   = 3.0 // processor-seconds in settleEvery: a fifth of a core
	settleAtLeast = time.Minute
	settleWithin  = 10 * time.Minute
)

// settledDir is where axx marks the simulators it clones that it left to settle.
func settledDir() string { return filepath.Join(npm.CacheDir("mobile"), "settled") }

func settledMark(udid string) string { return filepath.Join(settledDir(), udid) }

// isSettled reports whether axx left the simulator to settle before it was cloned: one it set up
// before it did so has not been.
func isSettled(udid string) bool {
	_, err := os.Stat(settledMark(udid))
	return err == nil
}

// markSettled marks the simulator settled.
func markSettled(udid string) error {
	if err := os.MkdirAll(settledDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settledMark(udid), nil, 0o644)
}

// settle waits, the simulator booted, until it comes to rest, and reports whether it did.
func settle(ctx context.Context, udid string) bool {
	started := time.Now()
	last, ok := simulatorCPU(ctx, udid)
	for {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(settleEvery):
		}
		now, found := simulatorCPU(ctx, udid)
		if found && ok && now-last < settleQuiet && time.Since(started) >= settleAtLeast {
			return true
		}
		if time.Since(started) >= settleWithin {
			return false
		}
		last, ok = now, found
	}
}

// simulatorCPU is the processor time, in seconds, the simulator's processes have taken: those its
// launchd_sim started. It reports false when the simulator does not run.
func simulatorCPU(ctx context.Context, udid string) (float64, bool) {
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=,time=,command=").Output()
	if err != nil {
		return 0, false
	}
	return simulatorCPUIn(string(out), udid)
}

// simulatorCPUIn is simulatorCPU from what ps listed.
func simulatorCPUIn(ps, udid string) (float64, bool) {
	type proc struct {
		pid, ppid string
		cpu       float64
	}
	var procs []proc
	launchd := ""
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		procs = append(procs, proc{f[0], f[1], cpuSeconds(f[2])})
		if strings.HasSuffix(f[3], "launchd_sim") && strings.Contains(line, udid) {
			launchd = f[0]
		}
	}
	if launchd == "" {
		return 0, false
	}
	var total float64
	for _, p := range procs {
		if p.ppid == launchd {
			total += p.cpu
		}
	}
	return total, true
}

// cpuSeconds reads ps's processor time, like 0:31.95, 12:01.23 or 1-02:03:04.
func cpuSeconds(s string) float64 {
	days := 0.0
	if d, rest, ok := strings.Cut(s, "-"); ok {
		days, _ = strconv.ParseFloat(d, 64)
		s = rest
	}
	total := 0.0
	for _, part := range strings.Split(s, ":") {
		v, _ := strconv.ParseFloat(part, 64)
		total = total*60 + v
	}
	return days*86400 + total
}
