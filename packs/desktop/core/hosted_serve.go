package desktopcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/internal/hostscenario"
	"github.com/nimbusxr/axx/packs/internal/hostwire"
)

// The host's side of desktop apps through the axx host (hosted.go has the
// run's). For each of the run's scenarios that registers an app, the host
// has a scenario of its own (hostscenario), in which it registers the app
// from the rows the run's step read, claims the desktop with the OS pack's
// driver and runs the driver's own code: its starts, its resets, its
// accessibility tree.
//
// The run is an agent's. It asks for what a Desktop and a Process do and
// nothing else; the app is a file of the project's (the suite is confined
// to it) or one the OS finds by its name, and starts with what its
// registration says. All of that the person who runs the host approves
// before the host resets, starts or reads the app.

// ServeHost serves an OS pack's side in the axx host over conn, until the
// run closes it or ctx ends: then every app it started for the run is
// stopped and its desktop given back. driver is the pack's, and parse reads
// an app's registration from its rows, as the pack's registration step
// does, with the driver.
func ServeHost(ctx context.Context, run core.HostRun, conn net.Conn, driver Driver, parse func(sc *core.Scenario, name string, rows []Property) (*App, error)) error {
	h := &hostSide{run: run, driver: driver, parse: parse}
	return hostscenario.Serve(ctx, run, conn, hostscenario.Side{
		Ops: func(host *hostscenario.Host) map[string]hostwire.Handler {
			h.host = host
			return h.ops()
		},
		Begin: func(*hostscenario.Mirror) any {
			return &mirror{apps: map[string]*App{}, procs: map[string]*served{}}
		},
		// The scenario's own end stops its apps and gives its desktop back
		// (release): the host's scenario keeps them as a run's does.
	})
}

type hostSide struct {
	host   *hostscenario.Host
	run    core.HostRun
	driver Driver
	parse  func(sc *core.Scenario, name string, rows []Property) (*App, error)
}

// mirror is what one of the run's scenarios has in the host beyond its
// scenario there: the apps it registered and those that run.
type mirror struct {
	mu    sync.Mutex
	apps  map[string]*App
	procs map[string]*served
}

// served is an app that runs for the run: its process, and the controls
// the run has of it, by what the run calls them.
type served struct {
	Process

	mu       sync.Mutex
	controls map[uint64]Control
	next     uint64
}

func (h *hostSide) ops() map[string]hostwire.Handler {
	return map[string]hostwire.Handler{
		"claim":    hostwire.Op(h.claim),
		"register": hostwire.Op(h.register),
		"reset":    hostwire.Op(h.reset),
		"start":    hostwire.Op(h.start),
		"watch":    hostwire.Op(h.watch),
		"screen":   hostwire.Op(h.screen),
		"release":  hostwire.Op(h.release),

		"proc.find": h.proc(func(p *served, a *procAnswer, q procParams) error {
			k, err := kindOf(q.Kind)
			if err != nil {
				return err
			}
			found, err := p.Find(k, q.Name, q.Shown)
			a.Controls = p.keepAll(found)
			return err
		}),
		"proc.names": h.proc(func(p *served, a *procAnswer, q procParams) error {
			k, err := kindOf(q.Kind)
			if err != nil {
				return err
			}
			a.Names = p.Names(k)
			return nil
		}),
		"proc.shows": h.proc(func(p *served, a *procAnswer, q procParams) (err error) {
			a.OK, err = p.Shows(q.Text)
			return err
		}),
		"proc.texts": h.proc(func(p *served, a *procAnswer, _ procParams) error {
			a.Names = p.Texts()
			return nil
		}),
		"proc.front": h.proc(func(p *served, _ *procAnswer, _ procParams) error { return p.Front() }),
		"proc.away":  h.proc(func(p *served, _ *procAnswer, _ procParams) error { return p.Away() }),
		"proc.leave": h.proc(func(p *served, _ *procAnswer, _ procParams) error { return p.Leave() }),
		"proc.scrollTo": h.proc(func(p *served, a *procAnswer, q procParams) error {
			k, err := kindOf(q.Kind)
			if err != nil {
				return err
			}
			c, err := p.ScrollTo(k, q.Name)
			if err == nil && c != nil {
				a.Control = p.keep(c)
			}
			return err
		}),
		"proc.scrollIntoView": h.control(func(p *served, c Control, _ *procAnswer, _ procParams) error { return p.ScrollIntoView(c) }),
		"proc.click":          h.control(func(p *served, c Control, _ *procAnswer, _ procParams) error { return p.Click(c) }),
		"proc.clickAt": h.control(func(p *served, c Control, _ *procAnswer, q procParams) error {
			return p.ClickAt(c, q.Anchor, q.X, q.Y)
		}),
		"proc.drag": h.control(func(p *served, c Control, _ *procAnswer, q procParams) error {
			return p.Drag(c, q.Anchor, q.X, q.Y, q.X2, q.Y2)
		}),
		"proc.control.enabled": h.control(func(_ *served, c Control, a *procAnswer, _ procParams) error {
			a.OK, a.Reported = c.Enabled()
			return nil
		}),
		"proc.control.value": h.control(func(_ *served, c Control, a *procAnswer, _ procParams) error {
			a.Text, a.Reported = c.Value()
			return nil
		}),
		"proc.key":  h.proc(func(p *served, _ *procAnswer, q procParams) error { return p.Key(q.Text) }),
		"proc.type": h.proc(func(p *served, _ *procAnswer, q procParams) error { return p.Type(q.Text) }),
		"proc.window": h.proc(func(p *served, a *procAnswer, _ procParams) error {
			img, scale, err := p.Window()
			if err != nil {
				return err
			}
			a.PNG, a.Scale = encodePNG(img), scale
			return nil
		}),
		"proc.tree": h.proc(func(p *served, a *procAnswer, _ procParams) error {
			tree, err := p.Tree()
			if err != nil {
				return err
			}
			a.Tree = p.wire(tree)
			return nil
		}),
		"proc.exited": h.proc(func(p *served, a *procAnswer, _ procParams) error {
			a.OK = p.Exited()
			return nil
		}),
		"proc.stop":     hostwire.Op(h.stop),
		"proc.snapshot": hostwire.Op(h.snapshot),
		"proc.describe": hostwire.Op(h.describe),
	}
}

func kindOf(word string) (appcore.Kind, error) {
	k, ok := appcore.KindOf(word)
	if !ok {
		return appcore.Kind{}, fmt.Errorf("%q is no kind of control", word)
	}
	return k, nil
}

// in runs fn in the run's scenario, alone in it (hostscenario.Host.In).
func (h *hostSide) in(ctx context.Context, key string, fn func(sc *core.Scenario, m *mirror) (any, error)) (any, error) {
	return h.host.In(ctx, key, func(hm *hostscenario.Mirror) (any, error) { return fn(hm.SC, hm.Data.(*mirror)) })
}

// claim claims the desktop for the scenario, as registering its first app
// does in a run.
func (h *hostSide) claim(ctx context.Context, p scenarioRef) (any, error) {
	return h.in(ctx, p.Scenario, func(sc *core.Scenario, _ *mirror) (any, error) {
		desk, err := desktopOf(sc, h.driver)
		if err != nil {
			return nil, err
		}
		return claimAnswer{DataDir: desk.DataDir()}, nil
	})
}

// register registers an app in the host's scenario from its rows. It
// starts and reads nothing yet: the system's app is watched once the person
// approved it (watch).
func (h *hostSide) register(ctx context.Context, p registerParams) (any, error) {
	return h.in(ctx, p.Scenario, func(sc *core.Scenario, m *mirror) (any, error) {
		desk, err := desktopOf(sc, h.driver)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		app := m.apps[p.App]
		m.mu.Unlock()
		if app != nil {
			return registerAnswer{Home: desk.Home(app)}, nil
		}
		if app, err = h.parse(sc, p.App, p.Rows); err != nil {
			return nil, err
		}
		if app.Name != p.App {
			return nil, fmt.Errorf("the %s app registers in the axx host as %s: the host and the run read its name differently", p.App, app.Name)
		}
		if err := appcore.Register(sc, &appcore.App{Name: app.Name, Platform: app.Driver.Platform(), Family: family{}, Data: app}, true); err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.apps[p.App] = app
		m.mu.Unlock()
		return registerAnswer{Home: desk.Home(app)}, nil
	})
}

// app is the scenario's app of the name, with its desktop, once the person
// who runs the host approved it.
func (h *hostSide) app(ctx context.Context, sc *core.Scenario, m *mirror, name string) (*App, Desktop, error) {
	m.mu.Lock()
	app := m.apps[name]
	m.mu.Unlock()
	if app == nil {
		return nil, nil, fmt.Errorf("the axx host has no %s app in this scenario: the run registers it first", name)
	}
	desk, err := desktopOf(sc, h.driver)
	if err != nil {
		return nil, nil, err
	}
	// The app is the agent's code, and starts past its sandbox; resetting
	// it changes what the OS keeps for the person. They say whether it may.
	if err := h.run.Approve(ctx, approval(sc, app)); err != nil {
		return nil, nil, err
	}
	return app, desk, nil
}

// approval says, for the person who runs the host, what running the app on
// their machine is: everything of its registration, which decides what
// runs.
func approval(sc *core.Scenario, app *App) core.HostApproval {
	project := sc.Suite().ProjectDir()
	rel := func(p string) string {
		if r, err := filepath.Rel(project, p); err == nil && filepath.IsAbs(p) && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return p
	}
	platform := app.Driver.Platform()
	var b strings.Builder
	if app.System {
		fmt.Fprintf(&b, "app: %s (the system's: read as it runs, never started, reset or stopped)\n", app.App)
		return core.HostApproval{
			Subject: "the " + app.Name + " app",
			Text:    fmt.Sprintf("platform: %s\n", platform) + b.String(),
			Warnings: []string{
				"The run reads what this app of yours shows from the scenario's start on, and clicks and types in it as a person does.",
				"The windows it shows during a scenario are closed as the scenario ends.",
			},
		}
	}
	fmt.Fprintf(&b, "platform: %s\napp: %s\n", platform, rel(app.App))
	if len(app.Args) > 0 {
		fmt.Fprintf(&b, "args: %s\n", strings.Join(app.Arguments(), " "))
	}
	for _, e := range app.EnvList() {
		name, value, _ := strings.Cut(e, "=")
		fmt.Fprintf(&b, "env.%s: %s\n", name, value)
	}
	extra := make([]string, 0, len(app.Extra))
	for k := range app.Extra {
		extra = append(extra, k)
	}
	slices.Sort(extra)
	for _, k := range extra {
		fmt.Fprintf(&b, "%s: %s\n", k, app.Extra[k])
	}
	if app.Locale != "" {
		fmt.Fprintf(&b, "locale: %s\n", app.Locale)
	}
	if app.Timezone != "" {
		fmt.Fprintf(&b, "timezone: %s\n", app.Timezone)
	}
	a := core.HostApproval{Subject: "the " + app.Name + " app", Text: b.String()}
	a.Warnings = append(a.Warnings,
		"The app is a program of the project's. It runs on this machine as you, outside the agent's sandbox, with your files and your network.",
		"The approval is for what is written here, not for one build of the app: the agent can change the app, and the next run starts what it built.")
	switch platform {
	case "linux":
		a.Warnings = append(a.Warnings, "It runs in a desktop of axx's own, not on yours.")
	default:
		a.Warnings = append(a.Warnings, "It runs on your desktop: while a scenario has it, the screen, the pointer and the keyboard are the scenario's.")
	}
	if len(extra) > 0 {
		a.Warnings = append(a.Warnings, "What the registration names of your settings ("+strings.Join(extra, ", ")+") is emptied before each scenario; yours are kept aside and put back when the run ends.")
	}
	return a
}

func (h *hostSide) reset(ctx context.Context, p appRef) (any, error) {
	return h.in(ctx, p.Scenario, func(sc *core.Scenario, m *mirror) (any, error) {
		app, desk, err := h.app(ctx, sc, m, p.App)
		if err != nil {
			return nil, err
		}
		if app.System {
			return nil, fmt.Errorf("the %s app is the system's: it is never reset", app.Name)
		}
		if err := desk.Reset(sc, app); err != nil {
			return nil, err
		}
		h.wasReset(sc, app)
		return nil, nil
	})
}

// wasReset notes, in the host's scenario, that the app's home is the
// scenario's: the run emptied it, and what a pack reads of it here (the
// app's files, as it launches) must not empty it again.
func (h *hostSide) wasReset(sc *core.Scenario, app *App) {
	s := scenarios.Of(sc)
	s.mu.Lock()
	s.reset[app.Name] = true
	s.mu.Unlock()
}

// start starts the app, with what the packs that build on this one do as
// an app launches (OnLaunch, in the host's scenario).
func (h *hostSide) start(ctx context.Context, p appRef) (any, error) {
	return h.in(ctx, p.Scenario, func(sc *core.Scenario, m *mirror) (any, error) {
		app, desk, err := h.app(ctx, sc, m, p.App)
		if err != nil {
			return nil, err
		}
		if app.System {
			return nil, fmt.Errorf("the %s app is the system's: it runs already, and the scenario does not start it", app.Name)
		}
		h.wasReset(sc, app)
		started, err := launching(sc, app, desk, desk.Home(app))
		if err != nil {
			return nil, err
		}
		proc, err := desk.Start(sc, started)
		if err != nil {
			return nil, secrets.Hide(sc, fmt.Errorf("cannot start the %s app: %w", app.Name, err))
		}
		h.run.Tell(fmt.Sprintf("started the %s app", app.Name))
		return h.serve(sc, m, app, proc), nil
	})
}

func (h *hostSide) watch(ctx context.Context, p appRef) (any, error) {
	return h.in(ctx, p.Scenario, func(sc *core.Scenario, m *mirror) (any, error) {
		app, desk, err := h.app(ctx, sc, m, p.App)
		if err != nil {
			return nil, err
		}
		if !app.System {
			return nil, fmt.Errorf("the %s app is not the system's: the scenario starts it", app.Name)
		}
		proc, err := desk.Watch(sc, app)
		if err != nil {
			return nil, secrets.Hide(sc, fmt.Errorf("cannot read the %s app: %w", app.Name, err))
		}
		return h.serve(sc, m, app, proc), nil
	})
}

// serve keeps the app's process for the run's calls, and in the host's
// scenario, which stops it as it ends, as a run's does.
func (h *hostSide) serve(sc *core.Scenario, m *mirror, app *App, proc Process) startAnswer {
	s := scenarios.Of(sc)
	s.mu.Lock()
	if !slices.Contains(s.order, app.Name) {
		s.order = append(s.order, app.Name)
	}
	s.running[app.Name] = proc
	s.mu.Unlock()
	m.mu.Lock()
	m.procs[app.Name] = &served{Process: proc, controls: map[uint64]Control{}}
	m.mu.Unlock()
	_, snapshots := proc.(snapshotter)
	return startAnswer{Snapshots: snapshots}
}

// screen is what the desktop shows of the scenario, asked beside its steps.
func (h *hostSide) screen(_ context.Context, p scenarioRef) (any, error) {
	hm, err := h.host.Mirror(p.Scenario)
	if err != nil {
		return screenAnswer{}, nil //nolint:nilerr // a scenario that ended shows nothing
	}
	s := scenarios.Of(hm.SC)
	s.mu.Lock()
	desk := s.desktop
	s.mu.Unlock()
	if desk == nil {
		return screenAnswer{}, nil
	}
	sh, err := desk.Screen()
	if err != nil || sh.Image == nil {
		return screenAnswer{}, err
	}
	var b bytes.Buffer
	// A frame of a video, ten a second: encoded fast, not small.
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, sh.Image); err != nil {
		return nil, err
	}
	return screenAnswer{PNG: b.Bytes(), Scale: sh.Scale, Pointer: sh.Pointer, HasPointer: sh.HasPointer}, nil
}

// release gives the scenario's desktop back, its apps stopped: the run's
// scenario ends.
func (h *hostSide) release(ctx context.Context, p scenarioRef) (any, error) {
	if _, err := h.host.Mirror(p.Scenario); err != nil {
		return nil, nil //nolint:nilerr // the scenario ended here already, and its desktop with it
	}
	return h.in(ctx, p.Scenario, func(sc *core.Scenario, m *mirror) (any, error) {
		m.mu.Lock()
		m.procs = map[string]*served{}
		m.mu.Unlock()
		s := scenarios.Of(sc)
		return nil, release(sc, s)
	})
}

// served is the app's process for the run, or an error that names it.
func (m *mirror) served(app string) (*served, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.procs[app]
	if p == nil {
		return nil, fmt.Errorf("the %s app does not run on the axx host's desktop in this scenario", app)
	}
	return p, nil
}

// proc makes a handler of what a Process does. A failure the layer above
// acts on by its kind crosses in the answer, as what it is.
func (h *hostSide) proc(fn func(p *served, a *procAnswer, q procParams) error) hostwire.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var q procParams
		if err := hostwire.Read(raw, &q); err != nil {
			return nil, fmt.Errorf("the run's request cannot be read: %w", err)
		}
		return h.in(ctx, q.Scenario, func(_ *core.Scenario, m *mirror) (any, error) {
			p, err := m.served(q.App)
			if err != nil {
				return nil, err
			}
			p.drop(q.Drop)
			a := &procAnswer{}
			err = fn(p, a, q)
			var (
				off    *Off
				lost   *Lost
				failed *core.AssertionError
			)
			switch {
			case err == nil:
			case errors.As(err, &off):
				a.Off = off
			case errors.As(err, &lost):
				a.Lost = lost.Error()
			case errors.As(err, &failed):
				a.Failed = failed
			default:
				return nil, err
			}
			return a, nil
		})
	}
}

// control makes a handler of what a Process does with a control the run
// has of it.
func (h *hostSide) control(fn func(p *served, c Control, a *procAnswer, q procParams) error) hostwire.Handler {
	return h.proc(func(p *served, a *procAnswer, q procParams) error {
		p.mu.Lock()
		c := p.controls[q.Control]
		p.mu.Unlock()
		if c == nil {
			// The run finds the control again, as it does one the app drew anew.
			return &Lost{Err: fmt.Errorf("the axx host no longer has the control of the %s app the run names", q.App)}
		}
		return fn(p, c, a, q)
	})
}

// keep keeps a control for the run, and gives what the run calls it.
func (p *served) keep(c Control) *wireControl {
	if c == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	p.controls[p.next] = c
	return &wireControl{ID: p.next, Name: c.Name()}
}

func (p *served) keepAll(cs []Control) []wireControl {
	out := make([]wireControl, 0, len(cs))
	for _, c := range cs {
		out = append(out, *p.keep(c))
	}
	return out
}

// drop forgets the controls the run has no longer.
func (p *served) drop(ids []uint64) {
	if len(ids) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range ids {
		delete(p.controls, id)
	}
}

func (p *served) wire(n *Node) *wireNode {
	if n == nil {
		return nil
	}
	w := &wireNode{Role: n.Role, Name: n.Name, ID: n.ID, Value: n.Value, Attrs: n.Attrs, Control: p.keep(n.Control)}
	for _, c := range n.Children {
		w.Children = append(w.Children, p.wire(c))
	}
	return w
}

// stop stops the app, keeping its home; a scenario that ended here stopped
// it already.
func (h *hostSide) stop(ctx context.Context, q procParams) (any, error) {
	if _, err := h.host.Mirror(q.Scenario); err != nil {
		return procAnswer{}, nil //nolint:nilerr // the scenario ended here already, and its apps with it
	}
	return h.in(ctx, q.Scenario, func(sc *core.Scenario, m *mirror) (any, error) {
		m.mu.Lock()
		p := m.procs[q.App]
		delete(m.procs, q.App)
		m.mu.Unlock()
		if p == nil {
			return procAnswer{}, nil
		}
		s := scenarios.Of(sc)
		s.mu.Lock()
		delete(s.running, q.App)
		s.mu.Unlock()
		return procAnswer{}, p.Stop()
	})
}

// snapshot captures the app's window for a trace, beside the scenario's
// next step: the capture is taken while that step finds what it acts on.
func (h *hostSide) snapshot(_ context.Context, q procParams) (any, error) {
	p, err := h.beside(q)
	if err != nil {
		return nil, err
	}
	snap, ok := p.Process.(snapshotter)
	if !ok {
		return nil, fmt.Errorf("the %s app's process captures no window for a trace", q.App)
	}
	b, scale, err := snap.Snapshot()
	return procAnswer{PNG: b, Scale: scale}, err
}

// describe is the app as it runs, for a failed scenario's context.
func (h *hostSide) describe(_ context.Context, q procParams) (any, error) {
	p, err := h.beside(q)
	if err != nil {
		return nil, err
	}
	return procAnswer{Describe: p.Describe()}, nil
}

// beside is the app's process for a call that is not in the scenario's
// turn.
func (h *hostSide) beside(q procParams) (*served, error) {
	hm, err := h.host.Mirror(q.Scenario)
	if err != nil {
		return nil, err
	}
	return hm.Data.(*mirror).served(q.App)
}
