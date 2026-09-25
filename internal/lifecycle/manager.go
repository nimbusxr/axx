package lifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// Options configures a Manager.
type Options struct {
	// ConfigDir is the directory apps.<name>.dir is relative to (the
	// directory of axx.yaml).
	ConfigDir string
	// Stdout and Stderr receive the apps' output, each line prefixed with
	// "[<app>] ". IDE protocol lines ("[AXX-IDE] ...") go to Stdout without
	// a prefix. Nil discards.
	Stdout, Stderr io.Writer
	// Logger receives progress and warnings. Nil discards.
	Logger *slog.Logger
	// NoStart skips the lifecycle entirely: nothing is started, waited for,
	// stopped or cleaned up.
	NoStart bool
	// Attach names apps the developer runs themselves (from an IDE, in any
	// language): axx does not start or stop them but waits for their
	// readiness checks.
	Attach map[string]bool
	// Debug names apps to run in debug mode; DebugAll selects every app with
	// a debug section.
	Debug    map[string]bool
	DebugAll bool
	// StateFile, when set, records the running apps (pids, process groups,
	// cleanups) so that Reap can stop them if this run is killed.
	StateFile string
	// Env is the base environment for apps (default os.Environ()); each
	// app's env is appended to it.
	Env []string
}

// Manager starts and stops the apps of one run. Start and Stop must not be
// called concurrently; Tail and Started may be called at any time.
type Manager struct {
	opts    Options
	log     *slog.Logger
	console *console
	http    *http.Client
	cfg     config.Apps
	apps    map[string]*app
	// graph is set when some app declares dependsOn: apps then start as
	// soon as their dependencies are ready. Otherwise they start one after
	// another in declaration order.
	graph bool

	mu sync.Mutex
	// up lists the apps started (launched or attached), in start order,
	// until Stop has stopped them.
	up []*app
	// stateLoaded is set once the state file has been checked for entries
	// of an earlier run; inherited holds them so they are not forgotten.
	stateLoaded bool
	inherited   []stateApp
}

// app is one apps.<name> entry with its validated settings and, once
// started, its run.
type app struct {
	cfg     config.App
	ready   *readySpec
	debug   *debugSpec
	cleanup []string
	tail    *ring

	// Set while starting, before the app is published in Manager.up.
	dir      string
	env      []string
	attached bool
	proc     *process
	// after lists the started apps this one started after; it stops
	// before them.
	after []*app
	// cleaned is set (under Manager.mu) once stopped and cleaned up.
	cleaned bool
}

// New validates the apps (dependencies, readiness, stop and debug settings,
// the names in opts) and returns a Manager. Working directories and commands
// are checked when an app starts.
func New(apps config.Apps, opts Options) (*Manager, error) {
	if opts.Env == nil {
		opts.Env = os.Environ()
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	m := &Manager{
		opts:    opts,
		log:     logger,
		console: newConsole(opts.Stdout, opts.Stderr),
		http:    newHTTPClient(),
		cfg:     apps,
		apps:    make(map[string]*app, len(apps)),
		graph:   usesDependsOn(apps),
	}
	for _, c := range apps {
		if _, dup := m.apps[c.Name]; dup {
			return nil, configErr(CodeInvalidConfig, "app %s is declared twice", c.Name).
				WithHint("give every entry under apps a unique name")
		}
		a, err := newApp(c)
		if err != nil {
			return nil, err
		}
		m.apps[c.Name] = a
	}
	if err := validateGraph(apps); err != nil {
		return nil, err
	}
	for _, set := range []struct {
		what  string
		names map[string]bool
	}{{"apps to attach", opts.Attach}, {"apps to debug", opts.Debug}} {
		for _, name := range slices.Sorted(maps.Keys(set.names)) {
			if err := m.known(name, set.what); err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

func newApp(c config.App) (*app, error) {
	a := &app{cfg: c, tail: newRing(tailLines)}
	var err error
	if a.ready, err = parseReady(c); err != nil {
		return nil, err
	}
	if a.debug, err = parseDebug(c); err != nil {
		return nil, err
	}
	if err := checkSignal(c.Stop.Signal); err != nil {
		return nil, configErr(CodeInvalidConfig, "apps.%s.stop.signal: %v", c.Name, err)
	}
	if c.Stop.Grace < 0 {
		return nil, configErr(CodeInvalidConfig, "apps.%s.stop.grace must not be negative", c.Name)
	}
	if !c.Cleanup.IsZero() {
		if a.cleanup, err = commandArgv(c.Cleanup, c.Shell); err != nil {
			return nil, configErr(CodeInvalidConfig, "apps.%s.cleanup: %v", c.Name, err)
		}
	}
	return a, nil
}

// known returns an error unless name is a declared app.
func (m *Manager) known(name, what string) error {
	if _, ok := m.apps[name]; ok {
		return nil
	}
	names := make([]string, 0, len(m.cfg))
	for _, c := range m.cfg {
		names = append(names, c.Name)
	}
	return configErr(CodeUnknownApp, "unknown app %q in %s", name, what).
		WithHint("use one of the apps declared in axx.yaml: %s", strings.Join(names, ", "))
}

// Start starts the named apps and the apps they depend on, and waits until
// all of them are ready. A nil names starts every enabled app. Disabled apps
// and apps already started are skipped; attached apps are only waited for.
//
// If an app fails to start or become ready, or ctx is cancelled, Start stops
// every app the Manager has started, runs their cleanups and returns the
// error (for a cancelled ctx, one that matches context.Canceled).
func (m *Manager) Start(ctx context.Context, names []string) error {
	if m.opts.NoStart {
		m.log.Info("not starting apps (no-start)")
		return nil
	}
	if names == nil {
		for _, c := range m.cfg {
			names = append(names, c.Name)
		}
	}
	for _, n := range names {
		if err := m.known(n, "apps to start"); err != nil {
			return err
		}
	}
	todo := m.pending(withDependencies(m.cfg, names))
	if len(todo) == 0 {
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(map[string]chan struct{}, len(todo))
	for _, a := range todo {
		ready[a.cfg.Name] = make(chan struct{})
	}
	var (
		first error
		once  sync.Once
		wg    sync.WaitGroup
	)
	for i, a := range todo {
		var deps []string
		switch {
		case m.graph:
			deps = a.cfg.DependsOn
		case i > 0:
			deps = []string{todo[i-1].cfg.Name}
		}
		wg.Go(func() {
			for _, d := range deps {
				ch, ok := ready[d]
				if !ok {
					continue // already up, or disabled
				}
				select {
				case <-ch:
				case <-runCtx.Done():
					return
				}
			}
			if runCtx.Err() != nil {
				return
			}
			if err := m.startApp(runCtx, a); err != nil {
				once.Do(func() {
					first = err
					cancel()
				})
				return
			}
			close(ready[a.cfg.Name])
		})
	}
	wg.Wait()
	if first == nil && ctx.Err() == nil {
		return nil
	}

	stopErr := m.Stop(context.WithoutCancel(ctx))
	if ctx.Err() != nil {
		first = axxerr.Wrap(ctx.Err(), CodeInterrupted, exitcode.Interrupted,
			"starting apps was interrupted; started apps were stopped and cleaned up")
	}
	if stopErr != nil {
		return errors.Join(first, stopErr)
	}
	return first
}

// pending returns the named apps that are not up yet.
func (m *Manager) pending(names []string) []*app {
	m.mu.Lock()
	defer m.mu.Unlock()
	up := make(map[*app]bool, len(m.up))
	for _, a := range m.up {
		up[a] = true
	}
	var out []*app
	for _, n := range names {
		if a := m.apps[n]; !up[a] {
			out = append(out, a)
		}
	}
	return out
}

// startApp starts one app (or, for an attached app, only waits for it).
func (m *Manager) startApp(ctx context.Context, a *app) error {
	name := a.cfg.Name
	a.env = appEnv(m.opts.Env, a.cfg.Env)
	a.proc, a.attached, a.cleaned = nil, m.opts.Attach[name], false
	if a.attached {
		m.log.Info("waiting for attached app (start it yourself)", "app", name)
		// Best effort: only a ready.exec check runs there.
		if a.dir, _ = resolveDir(a.cfg, m.opts.ConfigDir); a.dir == "" {
			a.dir = m.opts.ConfigDir
		}
		m.publish(a)
		return m.awaitReady(ctx, a, launchPlan{})
	}

	dir, err := resolveDir(a.cfg, m.opts.ConfigDir)
	if err != nil {
		return err
	}
	a.dir = dir
	plan, err := m.plan(ctx, a)
	if err != nil {
		return err
	}
	argv, err := commandArgv(plan.command, a.cfg.Shell)
	if err != nil {
		return configErr(CodeNoCommand, "app %s has no command to run", name).
			WithHint("set apps.%s.command", name)
	}
	m.log.Info("starting app", "app", name, "command", displayArgv(argv), "dir", dir)
	p, err := m.launch(a, argv)
	if err != nil {
		return err
	}
	a.proc = p
	m.publish(a)
	if plan.debug != nil && plan.debug.mode == modeAppListens {
		// Announce the attach request as soon as the debug port listens, not
		// after readiness: apps started with --inspect-brk or a suspended
		// delve wait for the debugger before they become ready.
		stop := m.announceAttach(ctx, a, *plan.debug)
		defer stop()
	}
	return m.awaitReady(ctx, a, plan)
}

// announceAttach prints the IDE attach line once the app's debug port
// listens. The returned func is called when readiness is settled: it makes
// sure a port that already listens is announced before Start returns, and
// otherwise keeps watching in the background until the app exits.
func (m *Manager) announceAttach(ctx context.Context, a *app, d debugger) func() {
	var once sync.Once
	announce := func() { once.Do(func() { m.console.Println(d.attachLine(a.cfg.Name)) }) }
	announced := make(chan struct{})
	wctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			if debuggerListening(wctx, d.host, d.port) {
				announce()
				close(announced)
				return
			}
			select {
			case <-wctx.Done():
				return
			case <-a.proc.done:
				return
			case <-tick.C:
			}
		}
	}()
	return func() {
		select {
		case <-announced:
		default:
			if debuggerListening(ctx, d.host, d.port) {
				announce()
			}
		}
		go func() {
			select {
			case <-announced:
			case <-a.proc.done:
			}
			cancel()
		}()
	}
}

// publish records a as up (so Stop will stop it) and saves the state file.
func (m *Manager) publish(a *app) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.after = nil
	if m.graph {
		for _, d := range a.cfg.DependsOn {
			for _, u := range m.up {
				if u.cfg.Name == d {
					a.after = append(a.after, u)
				}
			}
		}
	} else if len(m.up) > 0 {
		a.after = []*app{m.up[len(m.up)-1]}
	}
	m.up = append(m.up, a)
	if a.proc != nil {
		m.saveStateLocked()
	}
}

// Stop stops every started app, dependents before their dependencies (the
// reverse of start order), runs each app's cleanup right after it stops and
// removes the state file. Apps that do not depend on each other stop
// concurrently. Stop returns the errors of all apps joined; a cancelled ctx
// skips grace periods and interrupts cleanups.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	up := append([]*app(nil), m.up...)
	m.mu.Unlock()
	if len(up) == 0 {
		return nil
	}

	dependents := make(map[*app][]*app, len(up))
	stopped := make(map[*app]chan struct{}, len(up))
	for _, a := range up {
		stopped[a] = make(chan struct{})
		for _, d := range a.after {
			dependents[d] = append(dependents[d], a)
		}
	}
	errs := make([]error, len(up))
	var wg sync.WaitGroup
	for i, a := range up {
		wg.Go(func() {
			defer close(stopped[a])
			for _, d := range dependents[a] {
				<-stopped[d]
			}
			errs[i] = m.stopApp(ctx, a)
		})
	}
	wg.Wait()

	m.mu.Lock()
	m.up = nil
	m.saveStateLocked()
	m.mu.Unlock()
	return joinErrs(nonNil(errs))
}

// Tail returns the last lines (up to 200) the app and its cleanup printed,
// oldest first. It is empty for unknown apps.
func (m *Manager) Tail(app string) []string {
	a, ok := m.apps[app]
	if !ok {
		return nil
	}
	return a.tail.snapshot()
}

// Started returns the names of the apps this Manager launched and has not
// stopped yet, in start order. Attached apps are not included.
func (m *Manager) Started() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for _, a := range m.up {
		if a.proc != nil && !a.cleaned {
			names = append(names, a.cfg.Name)
		}
	}
	return names
}

// nonNil drops nil errors.
func nonNil(errs []error) []error {
	out := errs[:0]
	for _, e := range errs {
		if e != nil {
			out = append(out, e)
		}
	}
	return out
}
