package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// awaitReady waits until a passes its readiness checks. A launched app that
// exits with a non-zero code first fails at once; one that exits 0 (a
// launcher like `docker compose up -d`) keeps being checked until the
// timeout. An app without checks is ready immediately.
func (m *Manager) awaitReady(ctx context.Context, a *app, plan launchPlan) error {
	name := a.cfg.Name
	spec := a.ready
	var checks []check
	if len(spec.urls) > 0 {
		checks = append(checks, check{field: "http.url", probe: httpProbe(m.http, spec.urls)})
	}
	if spec.tcp != "" {
		checks = append(checks, check{field: "tcp", probe: tcpProbe(spec.tcp)})
	}
	if spec.exec != nil {
		checks = append(checks, check{field: "exec", probe: execProbe(spec.exec, a.dir, a.env)})
	}
	var exited, wake <-chan struct{}
	if p := a.proc; p != nil {
		exited = p.done
		if spec.log != nil {
			checks = append(checks, check{field: "log", probe: logProbe(spec.log, p.logMatched)})
			wake = p.logMatched
		}
	} else if spec.log != nil {
		m.log.Warn("axx cannot see the output of an attached app; skipping its ready.log check", "app", name)
	}
	if len(checks) == 0 {
		m.log.Warn("app has no readiness check; treating it as ready at once",
			"app", name, "hint", "set apps."+name+".ready so tests do not start before the app is up")
		return nil
	}

	m.log.Info("waiting for app to be ready", "app", name, "timeout", spec.timeout)
	start := time.Now()
	result, failures := pollReady(ctx, checks, spec.timeout, spec.interval, exited, wake)
	if result == outcomeExited && launcherExit(a) {
		// A command such as `docker compose up -d` starts the app and exits 0:
		// keep polling the non-log checks until the timeout.
		var remaining []check
		for _, c := range checks {
			if c.field != "log" {
				remaining = append(remaining, c)
			}
		}
		if len(remaining) > 0 {
			m.log.Info("app command exited 0 before ready; still waiting for readiness", "app", name)
			left := spec.timeout - time.Since(start)
			if left <= 0 {
				left = spec.interval
			}
			result, failures = pollReady(ctx, remaining, left, spec.interval, nil, nil)
		}
	}
	switch result {
	case outcomeReady:
		m.log.Info("app is ready", "app", name)
		return nil
	case outcomeExited:
		a.proc.pipes.wait(drainWait) // let the last lines arrive
		return exitedEarly(a, plan)
	case outcomeCancelled:
		return ctx.Err()
	default:
		return notReady(a, failures)
	}
}

// exitedEarly reports an app that exited before it was ready.
func exitedEarly(a *app, plan launchPlan) error {
	name := a.cfg.Name
	msg := fmt.Sprintf("app %s %s before it was ready", name, describeExit(a.proc.state))
	if tail := a.tail.snapshot(); len(tail) > 0 {
		msg += "; last output:" + formatTail(tail, errorTailLines)
	} else {
		msg += " (it printed nothing)"
	}
	e := envErr(CodeExitedEarly, "%s", msg)
	if d := plan.debug; d != nil && d.mode == modeIDEListens {
		return e.WithHint("in debug mode %s connects to the debugger on %s: make sure your IDE's debug listener is running, then run axx again", name, d.addr())
	}
	return e.WithHint("run `%s` in %s to see why it stops (apps.%s.command)", displayArgv(a.proc.argv), a.dir, name)
}

// notReady reports an app whose checks did not pass in time.
func notReady(a *app, failures []checkFailure) error {
	name := a.cfg.Name
	var b strings.Builder
	fmt.Fprintf(&b, "app %s was not ready after %s", name, a.ready.timeout)
	fields := make([]string, 0, len(failures))
	for _, f := range failures {
		fmt.Fprintf(&b, "\n  ready.%s: %v", f.field, f.err)
		fields = append(fields, "apps."+name+".ready."+f.field)
	}
	if a.proc != nil {
		if tail := a.tail.snapshot(); len(tail) > 0 {
			b.WriteString("\nlast output:" + formatTail(tail, errorTailLines))
		}
	}
	e := envErr(CodeNotReady, "%s", b.String())
	check := strings.Join(fields, " and ")
	if a.attached {
		return e.WithHint("%s is attached, so axx does not start it: start it yourself, check %s, or raise apps.%s.ready.timeout", name, check, name)
	}
	return e.WithHint("check %s, or raise apps.%s.ready.timeout (now %s)", check, name, a.ready.timeout)
}

// launcherExit reports whether the app's process exited cleanly (code 0).
func launcherExit(a *app) bool {
	return a.proc != nil && a.proc.state != nil && a.proc.state.Success()
}
