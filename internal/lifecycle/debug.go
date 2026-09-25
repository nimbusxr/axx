package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
)

// Debugger modes.
const (
	// modeIDEListens: the IDE listens and the app connects to it (JDWP with
	// server=n). axx checks the listener before launching the app.
	modeIDEListens = "ide-listens"
	// modeAppListens: the app listens and the IDE attaches (delve, node
	// --inspect, debugpy). axx asks the IDE to attach once the app is ready.
	modeAppListens = "app-listens"
)

// Strategies for apps.<name>.debug.onUnavailable.
const (
	onUnavailableFail     = "fail"
	onUnavailableFallback = "fallback"
	onUnavailableRetry    = "retry"
)

const (
	defaultDebugAttempts = 3
	defaultDebugDelay    = 5 * time.Second
)

// debugger is a validated apps.<name>.debug.debugger with defaults applied.
type debugger struct {
	typ, host, mode string
	port            int
}

// requestLine is the line IDE plugins watch for to start this app's
// debugger listener. Its format is a contract: keep it byte-stable.
func (d debugger) requestLine(app string) string {
	return fmt.Sprintf("[AXX-IDE] debug-listener-request name=%s type=%s host=%s port=%d", app, d.typ, d.host, d.port)
}

// attachLine is the line IDE plugins watch for to attach to an app that
// listens for a debugger. Its format is a contract: keep it byte-stable.
func (d debugger) attachLine(app string) string {
	return fmt.Sprintf("[AXX-IDE] debug-attach-request name=%s type=%s host=%s port=%d", app, d.typ, d.host, d.port)
}

func (d debugger) addr() string { return fmt.Sprintf("%s:%d", d.host, d.port) }

// debugSpec is a validated apps.<name>.debug.
type debugSpec struct {
	command       config.Command // zero: the normal command
	debugger      *debugger
	onUnavailable string
	attempts      int
	delay         time.Duration
}

// parseDebug validates apps.<name>.debug (nil when absent).
func parseDebug(app config.App) (*debugSpec, error) {
	d := app.Debug
	if d == nil {
		return nil, nil
	}
	bad := func(field, format string, args ...any) error {
		return configErr(CodeInvalidConfig, "apps.%s.debug.%s: %s", app.Name, field, fmt.Sprintf(format, args...))
	}
	spec := &debugSpec{
		command:       d.Command,
		onUnavailable: d.OnUnavailable,
		attempts:      d.Retry.Attempts,
		delay:         d.Retry.Delay.Or(defaultDebugDelay),
	}
	switch spec.onUnavailable {
	case "":
		spec.onUnavailable = onUnavailableRetry
	case onUnavailableFail, onUnavailableFallback, onUnavailableRetry:
	default:
		return nil, bad("onUnavailable", "%q is not one of fail, fallback, retry", d.OnUnavailable)
	}
	if spec.attempts < 0 || d.Retry.Delay < 0 {
		return nil, bad("retry", "attempts and delay must not be negative")
	}
	if spec.attempts == 0 {
		spec.attempts = defaultDebugAttempts
	}
	if g := d.Debugger; g != nil {
		dbg := &debugger{typ: g.Type, host: g.Host, mode: g.Mode, port: g.Port}
		if dbg.typ == "" {
			dbg.typ = "java"
		}
		if dbg.host == "" {
			dbg.host = "localhost"
		}
		switch dbg.mode {
		case "":
			dbg.mode = modeAppListens
			if dbg.typ == "java" {
				dbg.mode = modeIDEListens
			}
		case modeIDEListens, modeAppListens:
		default:
			return nil, bad("debugger.mode", "%q is not one of ide-listens, app-listens", g.Mode)
		}
		if dbg.port < 1 || dbg.port > 65535 {
			return nil, bad("debugger.port", "%d is not a TCP port (1-65535)", g.Port)
		}
		spec.debugger = dbg
	}
	return spec, nil
}

// launchPlan is what to run for an app and what to tell the IDE.
type launchPlan struct {
	command config.Command
	// debug is the debugger the app connects to or waits for; nil when the
	// app runs normally or in debug mode without a debugger.
	debug *debugger
}

// plan decides the command for a, handling debug mode: it asks the IDE for
// a listener, checks that one is listening and applies onUnavailable.
func (m *Manager) plan(ctx context.Context, a *app) (launchPlan, error) {
	normal := launchPlan{command: a.cfg.Command}
	if a.debug == nil || (!m.opts.DebugAll && !m.opts.Debug[a.cfg.Name]) {
		return normal, nil
	}
	p := launchPlan{command: a.cfg.Command, debug: a.debug.debugger}
	if !a.debug.command.IsZero() {
		p.command = a.debug.command
	}
	dbg := a.debug.debugger
	if dbg == nil || dbg.mode == modeAppListens {
		m.log.Info("starting app in debug mode", "app", a.cfg.Name)
		return p, nil
	}

	name := a.cfg.Name
	m.console.Println(dbg.requestLine(name))
	if debuggerListening(ctx, dbg.host, dbg.port) {
		m.log.Info("debugger is listening; starting app in debug mode", "app", name, "debugger", dbg.addr())
		return p, nil
	}
	switch a.debug.onUnavailable {
	case onUnavailableFail:
		return launchPlan{}, debuggerUnavailable(name, dbg, 0)
	case onUnavailableFallback:
		m.log.Warn("no debugger is listening; starting app without debugging",
			"app", name, "debugger", dbg.addr())
		return normal, nil
	}
	for attempt := 1; attempt <= a.debug.attempts; attempt++ {
		m.log.Warn("waiting for the IDE debugger to listen; start it now",
			"app", name, "debugger", dbg.addr(),
			"attempt", fmt.Sprintf("%d/%d", attempt, a.debug.attempts), "nextCheckIn", a.debug.delay)
		if err := sleep(ctx, a.debug.delay); err != nil {
			return launchPlan{}, err
		}
		// Re-announce so an IDE plugin that attached late still sees it.
		m.console.Println(dbg.requestLine(name))
		if debuggerListening(ctx, dbg.host, dbg.port) {
			m.log.Info("debugger is listening; starting app in debug mode", "app", name, "debugger", dbg.addr())
			return p, nil
		}
	}
	return launchPlan{}, debuggerUnavailable(name, dbg, a.debug.attempts)
}

// debuggerUnavailable explains how to start the IDE's debug listener.
func debuggerUnavailable(app string, d *debugger, attempts int) *axxerr.Error {
	waited := ""
	if attempts > 0 {
		waited = fmt.Sprintf(" (checked %d more times)", attempts)
	}
	example := "the debug listener for " + d.typ + " apps"
	if d.typ == "java" {
		example = `a "Remote JVM Debug" run configuration with debugger mode "Listen to remote JVM"`
	}
	return envErr(CodeDebuggerUnavailable, `no debugger is listening on %s for app %s%s

axx starts %s in debug mode, where the app connects to your IDE's debugger,
but nothing is listening on %s.

To fix this:
  1. Start a debug listener in your IDE on port %d, for example
     %s.
     The axx IDE plugin does this automatically when it sees the
     [AXX-IDE] debug-listener-request line.
  2. Wait until the IDE says it is listening.
  3. Run axx again.`, d.addr(), app, waited, app, d.addr(), d.port, example).
		WithHint("set apps.%s.debug.onUnavailable to retry (wait for the debugger) or fallback (run without debugging)", app)
}

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
