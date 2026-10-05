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

	"github.com/nimbusxr/axx/internal/proc"
)

// Options configures a Manager.
type Options struct {
	// ConfigDir is the directory services.<name>.dir is relative to (the
	// directory of axx.yaml).
	ConfigDir string
	// Stdout and Stderr receive the services' output, each line prefixed with
	// "[<app>] ". IDE protocol lines ("[AXX-IDE] ...") go to Stdout without
	// a prefix. Nil discards.
	Stdout, Stderr io.Writer
	// Logger receives progress and warnings. Nil discards.
	Logger *slog.Logger
	// NoStart skips the lifecycle entirely: nothing is started, waited for,
	// stopped or cleaned up.
	NoStart bool
	// Attach names services the developer runs themselves (from an IDE, in any
	// language): axx does not start or stop them but waits for their
	// readiness checks.
	Attach map[string]bool
	// Debug names services to run in debug mode; DebugAll selects every service with
	// a debug section.
	Debug    map[string]bool
	DebugAll bool
	// StateFile, when set, records the running services (pids, process groups,
	// cleanups) so that Reap can stop them if this run is killed.
	StateFile string
	// Env is the base environment for services (default os.Environ()); each
	// service's env is appended to it.
	Env []string
}

// Manager starts and stops the services of one run. Start and Stop must not be
// called concurrently; Tail and Started may be called at any time.
type Manager struct {
	opts    Options
	log     *slog.Logger
	console *console
	http    *http.Client
	cfg     config.Services
	apps    map[string]*service
	// graph is set when some service declares dependsOn: services then start as
	// soon as their dependencies are ready. Otherwise they start one after
	// another in declaration order.
	graph bool

	mu sync.Mutex
	// up lists the services started (launched or attached), in start order,
	// until Stop has stopped them.
	up []*service
	// unclean are the services this run stopped whose cleanup failed.
	unclean []stateService
	// stateLoaded is set once the state file has been checked for entries
	// of an earlier run; inherited holds them so they are not forgotten.
	stateLoaded bool
	inherited   []stateService
}

// service is one services.<name> entry with its validated settings and, once
// started, its run.
type service struct {
	cfg     config.Service
	ready   *readySpec
	debug   *debugSpec
	cleanup []string
	tail    *ring

	// Set while starting, before the service is published in Manager.up.
	dir      string
	env      []string
	attached bool
	proc     *process
	// after lists the started services this one started after; it stops
	// before them.
	after []*service
	// cleaned is set (under Manager.mu) once stopped and cleaned up.
	cleaned bool
}

// New validates the services (dependencies, readiness, stop and debug settings,
// the names in opts) and returns a Manager. Working directories and commands
// are checked when a service starts.
func New(apps config.Services, opts Options) (*Manager, error) {
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
		apps:    make(map[string]*service, len(apps)),
		graph:   usesDependsOn(apps),
	}
	for _, c := range apps {
		if _, dup := m.apps[c.Name]; dup {
			return nil, configErr(CodeInvalidConfig, "service %s is declared twice", c.Name).
				WithHint("give every entry under services a unique name")
		}
		a, err := newService(c)
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
	}{{"services to attach", opts.Attach}, {"services to debug", opts.Debug}} {
		for _, name := range slices.Sorted(maps.Keys(set.names)) {
			if err := m.known(name, set.what); err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

func newService(c config.Service) (*service, error) {
	a := &service{cfg: c, tail: newRing(tailLines)}
	var err error
	if a.ready, err = parseReady(c); err != nil {
		return nil, err
	}
	if a.debug, err = parseDebug(c); err != nil {
		return nil, err
	}
	if err := proc.CheckSignal(c.Stop.Signal); err != nil {
		return nil, configErr(CodeInvalidConfig, "services.%s.stop.signal: %v", c.Name, err)
	}
	if c.Stop.Grace < 0 {
		return nil, configErr(CodeInvalidConfig, "services.%s.stop.grace must not be negative", c.Name)
	}
	if !c.Cleanup.IsZero() {
		if a.cleanup, err = commandArgv(c.Cleanup, c.Shell); err != nil {
			return nil, configErr(CodeInvalidConfig, "services.%s.cleanup: %v", c.Name, err)
		}
	}
	return a, nil
}

// known returns an error unless name is a declared service.
func (m *Manager) known(name, what string) error {
	if _, ok := m.apps[name]; ok {
		return nil
	}
	names := make([]string, 0, len(m.cfg))
	for _, c := range m.cfg {
		names = append(names, c.Name)
	}
	return configErr(CodeUnknownService, "unknown service %q in %s", name, what).
		WithHint("use one of the services declared in axx.yaml: %s", strings.Join(names, ", "))
}

// Start starts the named services and the services they depend on, and waits until
// all of them are ready. A nil names starts every enabled service. Disabled services
// and services already started are skipped; attached services are only waited for.
//
// If a service fails to start or become ready, or ctx is cancelled, Start stops
// every service the Manager has started, runs their cleanups and returns the
// error (for a cancelled ctx, one that matches context.Canceled).
func (m *Manager) Start(ctx context.Context, names []string) error {
	if m.opts.NoStart {
		m.log.Info("not starting services (no-start)")
		return nil
	}
	if names == nil {
		for _, c := range m.cfg {
			names = append(names, c.Name)
		}
	}
	for _, n := range names {
		if err := m.known(n, "services to start"); err != nil {
			return err
		}
	}
	todo := m.pending(withDependencies(m.cfg, names))
	if len(todo) == 0 {
		return nil
	}
	if err := m.checkEarlierRuns(); err != nil {
		return err
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
			if err := m.startService(runCtx, a); err != nil {
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
			"starting services was interrupted; started services were stopped and cleaned up")
	}
	if stopErr != nil {
		return errors.Join(first, stopErr)
	}
	return first
}

// pending returns the named services that are not up yet.
func (m *Manager) pending(names []string) []*service {
	m.mu.Lock()
	defer m.mu.Unlock()
	up := make(map[*service]bool, len(m.up))
	for _, a := range m.up {
		up[a] = true
	}
	var out []*service
	for _, n := range names {
		if a := m.apps[n]; !up[a] {
			out = append(out, a)
		}
	}
	return out
}

// startService starts one service (or, for an attached service, only waits for it).
func (m *Manager) startService(ctx context.Context, a *service) error {
	name := a.cfg.Name
	a.env = serviceEnv(m.opts.Env, a.cfg.Env)
	a.proc, a.attached, a.cleaned = nil, m.opts.Attach[name], false
	if a.attached {
		m.log.Info("waiting for attached service (start it yourself)", "service", name)
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
		return configErr(CodeNoCommand, "service %s has no command to run", name).
			WithHint("set services.%s.command", name)
	}
	m.log.Info("starting service", "service", name, "command", displayArgv(argv), "dir", dir)
	p, err := m.launch(a, argv)
	if err != nil {
		return err
	}
	a.proc = p
	m.publish(a)
	if plan.debug != nil && plan.debug.mode == modeAppListens {
		// Announce the attach request as soon as the debug port listens, not
		// after readiness: services started with --inspect-brk or a suspended
		// delve wait for the debugger before they become ready.
		stop := m.announceAttach(ctx, a, *plan.debug)
		defer stop()
	}
	return m.awaitReady(ctx, a, plan)
}

// announceAttach prints the IDE attach line once the service's debug port
// listens. The returned func is called when readiness is settled: it makes
// sure a port that already listens is announced before Start returns, and
// otherwise keeps watching in the background until the service exits.
func (m *Manager) announceAttach(ctx context.Context, a *service, d debugger) func() {
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
func (m *Manager) publish(a *service) {
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
		a.after = []*service{m.up[len(m.up)-1]}
	}
	m.up = append(m.up, a)
	if a.proc != nil {
		m.saveStateLocked()
	}
}

// Stop stops every started service, dependents before their dependencies (the
// reverse of start order), runs each service's cleanup right after it stops and
// removes the state file. Services that do not depend on each other stop
// concurrently. Stop returns the errors of all services joined; a cancelled ctx
// skips grace periods and interrupts cleanups.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	up := append([]*service(nil), m.up...)
	m.mu.Unlock()
	if len(up) == 0 {
		return nil
	}

	dependents := make(map[*service][]*service, len(up))
	stopped := make(map[*service]chan struct{}, len(up))
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
			errs[i] = m.stopService(ctx, a)
		})
	}
	wg.Wait()

	m.mu.Lock()
	m.up = nil
	m.saveStateLocked()
	m.mu.Unlock()
	return joinErrs(nonNil(errs))
}

// Tail returns the last lines (up to 200) the service and its cleanup printed,
// oldest first. It is empty for unknown services.
func (m *Manager) Tail(app string) []string {
	a, ok := m.apps[app]
	if !ok {
		return nil
	}
	return a.tail.snapshot()
}

// Started returns the names of the services this Manager launched and has not
// stopped yet, in start order. Attached services are not included.
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
