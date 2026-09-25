package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/config"
)

const (
	defaultReadyTimeout  = 60 * time.Second
	defaultReadyInterval = time.Second
	// probeTimeout bounds connecting and waiting for a response in the http
	// and tcp checks.
	probeTimeout = 5 * time.Second
)

// readySpec is an app's validated readiness configuration.
type readySpec struct {
	urls     []string
	tcp      string
	exec     []string
	log      *regexp.Regexp
	timeout  time.Duration
	interval time.Duration
}

// parseReady validates apps.<name>.ready.
func parseReady(app config.App) (*readySpec, error) {
	spec := &readySpec{timeout: defaultReadyTimeout, interval: defaultReadyInterval}
	r := app.Ready
	if r == nil {
		return spec, nil
	}
	bad := func(field, format string, args ...any) error {
		return configErr(CodeInvalidConfig, "apps.%s.ready.%s: %s", app.Name, field, fmt.Sprintf(format, args...))
	}
	if r.Timeout < 0 || r.Interval < 0 {
		return nil, bad("timeout", "durations must not be negative")
	}
	spec.timeout = r.Timeout.Or(defaultReadyTimeout)
	spec.interval = r.Interval.Or(defaultReadyInterval)
	if r.HTTP != nil {
		if len(r.HTTP.URL) == 0 {
			return nil, bad("http.url", "no URL given")
		}
		for _, raw := range r.HTTP.URL {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, bad("http.url", "%q is not an http(s) URL", raw)
			}
		}
		spec.urls = append([]string(nil), r.HTTP.URL...)
	}
	if r.TCP != "" {
		if _, _, err := net.SplitHostPort(r.TCP); err != nil {
			return nil, bad("tcp", "%q is not host:port", r.TCP)
		}
		spec.tcp = r.TCP
	}
	if !r.Exec.IsZero() {
		argv, err := commandArgv(r.Exec, app.Shell)
		if err != nil {
			return nil, bad("exec", "%v", err)
		}
		spec.exec = argv
	}
	if r.Log != "" {
		re, err := regexp.Compile(r.Log)
		if err != nil {
			return nil, bad("log", "invalid regular expression: %v", err)
		}
		spec.log = re
	}
	return spec, nil
}

// check is one readiness condition.
type check struct {
	// field is the config key under apps.<name>.ready, for hints.
	field string
	// probe returns nil once the condition holds.
	probe func(ctx context.Context) error
}

// checkFailure is the last reason a check did not pass.
type checkFailure struct {
	field string
	err   error
}

// outcome is how waiting for readiness ended.
type outcome int

const (
	outcomeReady outcome = iota
	outcomeExited
	outcomeTimeout
	outcomeCancelled
)

// pollReady runs the checks every interval until all have passed (each
// needs to pass once), the process exits (exited is closed), timeout
// elapses or ctx is cancelled. wake, when closed, triggers an immediate
// re-check (the log check uses it). On timeout it returns the checks that
// never passed with their last error.
func pollReady(ctx context.Context, checks []check, timeout, interval time.Duration, exited, wake <-chan struct{}) (outcome, []checkFailure) {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Probes also stop as soon as the process exits.
	probeCtx, cancelProbes := context.WithCancel(deadline)
	defer cancelProbes()
	if exited != nil {
		go func() {
			select {
			case <-exited:
				cancelProbes()
			case <-probeCtx.Done():
			}
		}()
	}

	pending := make([]checkFailure, len(checks))
	for i, c := range checks {
		pending[i] = checkFailure{field: c.field, err: errors.New("not checked yet")}
	}
	remaining := append([]check(nil), checks...)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if closed(exited) {
			return outcomeExited, nil
		}
		var still []check
		var failures []checkFailure
		for i, c := range remaining {
			err := c.probe(probeCtx)
			if err == nil {
				continue
			}
			if probeCtx.Err() != nil {
				// Interrupted by the deadline or an exit: keep the last
				// meaningful reason.
				err = pending[i].err
			}
			still = append(still, c)
			failures = append(failures, checkFailure{field: c.field, err: err})
		}
		remaining, pending = still, failures
		if len(remaining) == 0 {
			return outcomeReady, nil
		}
		select {
		case <-exited:
			return outcomeExited, nil
		case <-deadline.Done():
			if ctx.Err() != nil {
				return outcomeCancelled, nil
			}
			return outcomeTimeout, pending
		case <-wake:
			wake = nil // closed channels stay ready; re-check once
		case <-tick.C:
		}
	}
}

// closed reports whether ch is closed (false for nil).
func closed(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// newHTTPClient returns the client for http checks: 5s to connect and 5s
// for the response headers, like the original runner.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: probeTimeout}).DialContext,
			TLSHandshakeTimeout:   probeTimeout,
			ResponseHeaderTimeout: probeTimeout,
			DisableKeepAlives:     true,
		},
		Timeout: 2 * probeTimeout,
	}
}

// httpProbe passes when every URL answers GET with a 2xx status.
func httpProbe(client *http.Client, urls []string) func(context.Context) error {
	return func(ctx context.Context) error {
		for _, u := range urls {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode > 299 {
				return fmt.Errorf("GET %s returned %s", u, resp.Status)
			}
		}
		return nil
	}
}

// tcpProbe passes when addr accepts a TCP connection.
func tcpProbe(addr string) func(context.Context) error {
	return func(ctx context.Context) error {
		d := net.Dialer{Timeout: probeTimeout}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		return conn.Close()
	}
}

// execProbe passes when the command exits 0.
func execProbe(argv []string, dir string, env []string) func(context.Context) error {
	return func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir, cmd.Env = dir, env
		cmd.WaitDelay = time.Second
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		msg := fmt.Sprintf("`%s` failed: %v", displayArgv(argv), err)
		if last := lastLine(out); last != "" {
			msg += " (" + last + ")"
		}
		return errors.New(msg)
	}
}

// logProbe passes once matched is closed.
func logProbe(re *regexp.Regexp, matched <-chan struct{}) func(context.Context) error {
	return func(context.Context) error {
		if closed(matched) {
			return nil
		}
		return fmt.Errorf("no output line matched %s yet", re)
	}
}

// lastLine returns the last non-blank line of out, shortened.
func lastLine(out []byte) string {
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	s := strings.TrimSpace(string(lines[len(lines)-1]))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}
