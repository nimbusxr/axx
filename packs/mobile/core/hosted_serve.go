package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/internal/hostwire"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// The host's side of phones through the axx host (hosted.go has the run's).
// For each of the run's scenarios that registers an app, the host has a
// scenario of its own, in a suite of the project's packs (core.HostRun.Suite),
// in which it registers the app as the run's step did, and runs the
// platform pack's own code: its device pool, its installs, its resets.
//
// The run is an agent's. It asks for what a Device does and nothing else;
// the files it names are the project's (the suite is confined to it, and
// what it names outright is checked here); and the app, which is the
// agent's code, starts once the person who runs the host approved it.

// ServeHost serves a platform pack's side in the axx host over conn, until
// the run closes it or ctx ends: then every device it started for the run
// is stopped. register registers an app in a scenario from its
// registration's table, as the pack's registration step does.
func ServeHost(ctx context.Context, run core.HostRun, conn net.Conn, register func(sc *core.Scenario, app string, t *core.Table) error) error {
	defer conn.Close()
	h := &hostSide{ctx: ctx, run: run, register: register, scenarios: map[string]*mirror{}}
	h.srv = hostwire.NewServer(conn, map[string]hostwire.Handler{
		"register":   hostwire.Op(h.registerApp),
		"notes":      hostwire.Op(h.notes),
		"see":        hostwire.Op(h.see),
		"phone.move": hostwire.Op(h.movePhone),
		"start":      hostwire.Op(h.start),
		"end":        hostwire.Op(h.end),
		"revive":     hostwire.Op(h.revive),
		"stop":       hostwire.Op(h.stop),

		"screen": deviceOp(h, func(ctx context.Context, d *served, _ appRef) (any, error) {
			s, err := d.Screen(ctx)
			return wireOf(s), err
		}),
		"launch":       deviceOp(h, func(ctx context.Context, d *served, _ appRef) (any, error) { return nil, d.Launch(ctx) }),
		"restart":      deviceOp(h, func(ctx context.Context, d *served, _ appRef) (any, error) { return nil, d.Restart(ctx) }),
		"background":   deviceOp(h, func(ctx context.Context, d *served, _ appRef) (any, error) { return nil, d.Background(ctx) }),
		"openLink":     deviceOp(h, func(ctx context.Context, d *served, p textParams) (any, error) { return nil, d.OpenLink(ctx, p.Text) }),
		"hideKeyboard": deviceOp(h, func(ctx context.Context, d *served, _ appRef) (any, error) { return d.HideKeyboard(ctx) }),
		"swipe":        deviceOp(h, func(ctx context.Context, d *served, p textParams) (any, error) { return nil, d.Swipe(ctx, p.Text) }),
		"scroll":       deviceOp(h, func(ctx context.Context, d *served, p textParams) (any, error) { return d.Scroll(ctx, p.Text) }),
		"systemBars":   deviceOp(h, func(ctx context.Context, d *served, _ appRef) (any, error) { return d.SystemBars(ctx) }),
		"describe":     deviceOp(h, func(_ context.Context, d *served, _ appRef) (any, error) { return d.Describe(), nil }),
		"scrollToShow": deviceOp(h, func(ctx context.Context, d *served, p nodeParams) (any, error) {
			return d.ScrollToShow(ctx, p.node(), p.Text)
		}),
		"scrollRow": deviceOp(h, func(ctx context.Context, d *served, p nodeParams) (any, error) {
			row := p.node()
			if row == nil {
				return nil, errors.New("the run named no row to scroll")
			}
			return d.ScrollRow(ctx, row, p.Text)
		}),
		"appLog": deviceOp(h, func(ctx context.Context, d *served, _ appRef) (any, error) {
			if l, ok := d.Device.(appLog); ok {
				return l.AppLog(ctx), nil
			}
			return "", nil
		}),
		"backgroundTask": deviceOp(h, func(ctx context.Context, d *served, p textParams) (any, error) {
			b, ok := d.Device.(BackgroundTasks)
			if !ok {
				return nil, fmt.Errorf("the %s app's device cannot run its background tasks", p.App)
			}
			return nil, b.RunBackgroundTask(ctx, p.Text)
		}),
		"sceneObject": deviceOp(h, func(ctx context.Context, d *served, p textParams) (any, error) {
			v, ok := d.Device.(SceneView)
			if !ok {
				return nil, fmt.Errorf("the %s app's camera sees no scene: give it one with \"the %s app's camera sees <scene>\"", p.App, p.App)
			}
			x, y, err := v.SceneObject(ctx, p.Text)
			return [2]float64{x, y}, err
		}),
		"travel": deviceOp(h, h.travel),

		"notifications.open": deviceOp(h, func(ctx context.Context, d *served, _ appRef) (any, error) {
			n, err := d.OpenNotifications(ctx)
			if err != nil {
				return nil, err
			}
			d.notifications = n
			return nil, nil
		}),
		"notifications.shows": notificationOp(h, func(ctx context.Context, n Notifications, p notificationParams) (any, error) {
			return n.Shows(ctx, p.Text)
		}),
		"notifications.texts": notificationOp(h, func(ctx context.Context, n Notifications, _ notificationParams) (any, error) {
			return n.Texts(ctx), nil
		}),
		"notifications.close": notificationOp(h, func(ctx context.Context, n Notifications, _ notificationParams) (any, error) {
			return nil, n.Close(ctx)
		}),
		"notifications.open-one": notificationOp(h, func(ctx context.Context, n Notifications, p notificationParams) (any, error) {
			return nil, n.Open(ctx, p.Text)
		}),
		"notifications.act": notificationOp(h, func(ctx context.Context, n Notifications, p notificationParams) (any, error) {
			return nil, n.Act(ctx, p.Text, p.Action)
		}),
		"notifications.log": notificationOp(h, func(ctx context.Context, n Notifications, _ notificationParams) (any, error) {
			if l, ok := n.(notificationLog); ok {
				return l.NotificationLog(ctx), nil
			}
			return "", nil
		}),
		"notifications.system": notificationOp(h, func(ctx context.Context, n Notifications, _ notificationParams) (any, error) {
			if s, ok := n.(notificationSystem); ok {
				return s.NotificationSystem(ctx), nil
			}
			return "", nil
		}),

		"files": fileOp(h, true, func(_ context.Context, f core.Files, _ filesParams) (any, error) { return f.Where(), nil }),
		"files.read": fileOp(h, false, func(ctx context.Context, f core.Files, p filesParams) (any, error) {
			body, found, err := f.Read(ctx, p.Name)
			return struct {
				Body  []byte `json:"body"`
				Found bool   `json:"found"`
			}{body, found}, err
		}),
		"files.write": fileOp(h, false, func(ctx context.Context, f core.Files, p filesParams) (any, error) {
			return nil, f.Write(ctx, p.Name, p.Body)
		}),
		"files.list":  fileOp(h, false, func(ctx context.Context, f core.Files, p filesParams) (any, error) { return f.List(ctx, p.Max) }),
		"files.empty": fileOp(h, false, func(ctx context.Context, f core.Files, _ filesParams) (any, error) { return nil, f.Empty(ctx) }),
	})
	err := h.srv.Serve(ctx, conn)
	h.close()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// hostSide is one run's connection, in the host.
type hostSide struct {
	ctx      context.Context
	run      core.HostRun
	srv      *hostwire.Server
	register func(sc *core.Scenario, app string, t *core.Table) error

	mu        sync.Mutex
	scenarios map[string]*mirror
}

// mirror is one of the run's scenarios, in the host: the apps it registered
// and the devices they run on.
type mirror struct {
	key    string
	sc     *core.Scenario
	cancel context.CancelFunc
	// op is held by the call that works in the scenario: a scenario's steps
	// come one after the other, and the scenario's context is the call's.
	op sync.Mutex

	mu      sync.Mutex
	devices map[string]*served
	holds   []func()
	// noted is what the run knows of the scenario's notes.
	noted map[string]string
}

// served is an app on the device the host started for it.
type served struct {
	Device
	// sc is the host's scenario the app runs in.
	sc            *core.Scenario
	notifications Notifications
	// files are the app's files by their path, as the device gives them.
	files map[string]core.Files
}

// sink sends what the host's scenario logs and attaches to the run's.
type sink struct {
	srv *hostwire.Server
	key string
}

func (s sink) Log(_ *core.Scenario, msg string) {
	s.srv.Tell("log", toldLog{Scenario: s.key, Text: msg})
}

func (s sink) Attach(_ *core.Scenario, mediaType string, body []byte, name string) {
	s.srv.Tell("attach", toldAttachment{Scenario: s.key, MediaType: mediaType, Body: body, Name: name})
}

func (h *hostSide) mirror(key string) (*mirror, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.scenarios[key]
	if m == nil {
		return nil, fmt.Errorf("the axx host has no scenario %q of this run: it registers an app first", key)
	}
	return m, nil
}

// in runs fn in the run's scenario: alone in it, with the scenario's
// context the call's, so that what the pack's code does for the step ends
// when the run no longer waits for it.
func (h *hostSide) in(ctx context.Context, key string, fn func(m *mirror) (any, error)) (any, error) {
	m, err := h.mirror(key)
	if err != nil {
		return nil, err
	}
	m.op.Lock()
	defer m.op.Unlock()
	m.sc.SetContext(ctx)
	defer m.sc.SetContext(h.ctx)
	// What a pack notes of the scenario here reaches the run before the
	// call's answer does.
	defer h.tellNotes(m)
	return fn(m)
}

// tellNotes tells the run what the packs noted of the scenario in the host
// (core.Scenario.Note) since it last heard.
func (h *hostSide) tellNotes(m *mirror) {
	notes := m.sc.Notes()
	m.mu.Lock()
	var news map[string]string
	for name, value := range notes {
		if v, ok := m.noted[name]; ok && v == value {
			continue
		}
		if news == nil {
			news = map[string]string{}
		}
		news[name], m.noted[name] = value, value
	}
	m.mu.Unlock()
	if len(news) > 0 {
		h.srv.Tell("notes", notesParams{Scenario: m.key, Notes: news})
	}
}

// notes takes what the run's packs noted of the scenario.
func (h *hostSide) notes(ctx context.Context, p notesParams) (any, error) {
	return h.in(ctx, p.Scenario, func(m *mirror) (any, error) {
		m.take(p.Notes)
		return nil, nil
	})
}

func (m *mirror) take(notes map[string]string) {
	m.mu.Lock()
	for name, value := range notes {
		m.noted[name] = value
	}
	m.mu.Unlock()
	for name, value := range notes {
		m.sc.Note(name, value)
	}
}

func (h *hostSide) registerApp(ctx context.Context, p registerParams) (any, error) {
	suite, err := h.run.Suite()
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	m := h.scenarios[p.Scenario]
	made := m == nil
	if made {
		if p.Dir != "" {
			if err := suite.Admits(p.Dir); err != nil {
				h.mu.Unlock()
				return nil, fmt.Errorf("the scenario's feature file: %w", err)
			}
		}
		info := p.Info
		info.Dir = p.Dir
		scCtx, cancel := context.WithCancel(h.ctx)
		m = &mirror{key: p.Scenario, cancel: cancel, devices: map[string]*served{}, noted: map[string]string{}}
		m.sc = core.NewScenario(scCtx, info, suite, sink{srv: h.srv, key: p.Scenario})
		// A wait for what the run's scenarios share (a device another one
		// has) is no part of the step's own time, in the run either.
		m.sc.SetHold(func() func() {
			h.srv.Tell("hold", scenarioRef{Scenario: m.key})
			return func() { h.srv.Tell("release", scenarioRef{Scenario: m.key}) }
		})
		h.scenarios[p.Scenario] = m
	}
	h.mu.Unlock()
	return h.in(ctx, p.Scenario, func(m *mirror) (any, error) {
		m.take(p.Notes)
		if made {
			// The packs that build on this one set up in the host's
			// scenario what they set up in the run's (core.HostScenario).
			if err := suite.BeginHostScenario(m.sc); err != nil {
				h.mu.Lock()
				delete(h.scenarios, p.Scenario)
				h.mu.Unlock()
				m.cancel()
				return nil, err
			}
		}
		return nil, h.register(m.sc, p.App, Table(p.Rows))
	})
}

func (h *hostSide) see(ctx context.Context, p seeParams) (any, error) {
	return h.in(ctx, p.Scenario, func(m *mirror) (any, error) {
		app, err := mobileApp(m.sc, p.App)
		if err != nil {
			return nil, err
		}
		cam, ok := app.Platform.(Camera)
		if !ok {
			return nil, fmt.Errorf("the %s app's camera cannot be played on %s", p.App, app.Platform.Kind())
		}
		// The run named the file outright: the project's only.
		if err := m.sc.Suite().Admits(p.Path); err != nil {
			return nil, err
		}
		if err := cam.See(m.sc, p.Path); err != nil {
			return nil, err
		}
		var a struct {
			Scene bool       `json:"scene"`
			Pose  scene.Mat4 `json:"pose"`
		}
		if phone := cam.ScenePhone(); phone != nil {
			a.Scene, a.Pose = true, phone.Pose()
		}
		return a, nil
	})
}

func (h *hostSide) movePhone(ctx context.Context, p moveParams) (any, error) {
	return h.in(ctx, p.Scenario, func(m *mirror) (any, error) {
		app, err := mobileApp(m.sc, p.App)
		if err != nil {
			return nil, err
		}
		cam, ok := app.Platform.(Camera)
		if !ok || cam.ScenePhone() == nil {
			return nil, fmt.Errorf("the %s app's camera sees no scene to move through", p.App)
		}
		return nil, cam.ScenePhone().Move(ctx, p.Pose)
	})
}

func (h *hostSide) start(ctx context.Context, p appRef) (any, error) {
	return h.in(ctx, p.Scenario, func(m *mirror) (any, error) {
		app, err := mobileApp(m.sc, p.App)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		running := m.devices[p.App]
		m.mu.Unlock()
		if running != nil {
			return nil, fmt.Errorf("the %s app runs already in this scenario", p.App)
		}
		hp, ok := app.Platform.(HostPlatform)
		if !ok {
			return nil, fmt.Errorf("the %s app's platform, %s, is not one the axx host runs devices of", p.App, app.Platform.Kind())
		}
		// The app is the agent's code, and starts past its sandbox: the
		// person who runs the host says whether it may.
		if err := h.run.Approve(ctx, hp.HostApproval(m.sc)); err != nil {
			return nil, err
		}
		d, err := app.Platform.Start(m.sc)
		if err != nil {
			return nil, err
		}
		sd := &served{Device: d, sc: m.sc, files: map[string]core.Files{}}
		m.mu.Lock()
		m.devices[p.App] = sd
		m.mu.Unlock()
		// The packs that build on this one find the app's device in the
		// host's scenario as they do in a run's (OnDevice, the app's files
		// as an app launches). The host stops it: it is not in the order
		// the scenario stops its own in.
		r := scenarioRunning.Of(m.sc)
		r.mu.Lock()
		r.devices[p.App] = d
		r.mu.Unlock()
		h.hear(m, p.App, sd)
		h.run.Tell(fmt.Sprintf("started the %s app (%s)", p.App, d.ScreenKey()))
		return startAnswer{Session: session(d.Session()), Stream: d.Stream(), ScreenKey: d.ScreenKey()}, nil
	})
}

// session is what the run attaches to the app's session with.
func session(s *appium.Session) sessionInfo {
	if s == nil {
		return sessionInfo{}
	}
	url, selectors := s.Server()
	return sessionInfo{URL: url, Selectors: selectors, ID: s.Identity(), Capabilities: s.Capabilities}
}

// hear has the run hear of the touches the host's side makes on the device
// (a scroll of the pack's own): a video of the run draws them.
func (h *hostSide) hear(m *mirror, app string, d *served) {
	if s := d.Session(); s != nil && s.Touched == nil {
		s.Touched = func(t appium.Touch) {
			h.srv.Tell("touch", toldTouch{Scenario: m.key, App: app, Touch: t})
		}
	}
}

func (h *hostSide) revive(ctx context.Context, p appRef) (any, error) {
	return h.in(ctx, p.Scenario, func(m *mirror) (any, error) {
		d, err := m.device(p.App)
		if err != nil {
			return nil, err
		}
		s := d.Session()
		if s == nil || s.Revive == nil {
			return nil, fmt.Errorf("the %s app's driver stopped answering, and its device cannot start it again", p.App)
		}
		if err := s.Revive(ctx); err != nil {
			return nil, err
		}
		h.hear(m, p.App, d)
		return session(d.Session()), nil
	})
}

func (h *hostSide) stop(ctx context.Context, p stopParams) (any, error) {
	h.mu.Lock()
	ended := h.scenarios[p.Scenario] == nil
	h.mu.Unlock()
	if ended {
		// The scenario ended here already, and its apps with it.
		return deviceAnswer{}, nil
	}
	return h.in(ctx, p.Scenario, func(m *mirror) (any, error) {
		m.mu.Lock()
		d := m.devices[p.App]
		delete(m.devices, p.App)
		m.mu.Unlock()
		if d == nil {
			return deviceAnswer{}, nil
		}
		m.forget(p.App)
		return deviceAnswer{}, d.Stop(p.Failed)
	})
}

// end ends one of the run's scenarios in the host: an app it left running
// is stopped, and what the pack kept for the scenario goes.
func (h *hostSide) end(_ context.Context, p scenarioRef) (any, error) {
	h.mu.Lock()
	m := h.scenarios[p.Scenario]
	delete(h.scenarios, p.Scenario)
	h.mu.Unlock()
	if m == nil {
		return nil, nil
	}
	return nil, m.end(h.ctx)
}

func (m *mirror) end(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.sc.SetContext(context.WithoutCancel(ctx))
	m.mu.Lock()
	devices := m.devices
	m.devices = map[string]*served{}
	holds := m.holds
	m.holds = nil
	m.mu.Unlock()
	for _, release := range holds {
		release()
	}
	var errs []error
	for name, d := range devices {
		m.forget(name)
		if err := d.Stop(false); err != nil {
			errs = append(errs, fmt.Errorf("the %s app: %w", name, err))
		}
	}
	if err := m.sc.Close(); err != nil {
		errs = append(errs, err)
	}
	m.cancel()
	return errors.Join(errs...)
}

// close ends every scenario the run left: its connection ended.
func (h *hostSide) close() {
	h.mu.Lock()
	scenarios := h.scenarios
	h.scenarios = map[string]*mirror{}
	h.mu.Unlock()
	for _, m := range scenarios {
		if err := m.end(context.WithoutCancel(h.ctx)); err != nil && h.run.Logger != nil {
			h.run.Logger.Warn("ending a scenario the run left", "scenario", m.key, "error", err)
		}
	}
}

// forget takes an app's device out of the host's scenario as it stops.
func (m *mirror) forget(app string) {
	r := scenarioRunning.Of(m.sc)
	r.mu.Lock()
	delete(r.devices, app)
	r.mu.Unlock()
}

func (m *mirror) device(app string) (*served, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.devices[app]
	if d == nil {
		return nil, fmt.Errorf("the %s app is not running in the axx host", app)
	}
	return d, nil
}

// refOf is the scenario and app a call's parameters name.
type refOf interface{ ref() appRef }

func (r appRef) ref() appRef { return r }

// device makes a handler of an operation on a running app's device: its
// answer says which session the app is in afterwards.
func deviceOp[P refOf](h *hostSide, fn func(ctx context.Context, d *served, p P) (any, error)) hostwire.Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p P
		if err := hostwire.Read(raw, &p); err != nil {
			return nil, fmt.Errorf("the run's request cannot be read: %w", err)
		}
		r := p.ref()
		return h.in(ctx, r.Scenario, func(m *mirror) (any, error) {
			d, err := m.device(r.App)
			if err != nil {
				return nil, err
			}
			result, err := fn(ctx, d, p)
			if err != nil {
				return nil, err
			}
			h.hear(m, r.App, d)
			a := deviceAnswer{}
			if s := d.Session(); s != nil {
				a.Session = s.Identity()
			}
			if result != nil {
				b, err := json.Marshal(result)
				if err != nil {
					return nil, err
				}
				a.Result = b
			}
			return a, nil
		})
	}
}

func notificationOp(h *hostSide, fn func(ctx context.Context, n Notifications, p notificationParams) (any, error)) hostwire.Handler {
	return deviceOp(h, func(ctx context.Context, d *served, p notificationParams) (any, error) {
		if d.notifications == nil {
			return nil, fmt.Errorf("the %s app's notifications are not open", p.App)
		}
		return fn(ctx, d.notifications, p)
	})
}

// fileOp makes a handler of an operation on an app's files at a path. The
// files are the app's in the host's scenario (appcore.Files), which keeps
// what the scenario wrote there: a pack that builds on this one reads the
// app's files in the host as it does in a run (the model stubs an app starts
// with). where is for the one operation that asks where the files are, on
// the device.
func fileOp(h *hostSide, where bool, fn func(ctx context.Context, f core.Files, p filesParams) (any, error)) hostwire.Handler {
	return deviceOp(h, func(ctx context.Context, d *served, p filesParams) (any, error) {
		// The path is one in the app's own files, whoever asks.
		rel, err := sandboxPath("./" + p.Path)
		if err != nil {
			return nil, err
		}
		if !where {
			f, err := appcore.Files(d.sc, p.App, "./"+rel)
			if err != nil {
				return nil, err
			}
			return fn(ctx, f, p)
		}
		f := d.files[rel]
		if f == nil {
			if f, err = d.Files(ctx, rel); err != nil {
				return nil, err
			}
			d.files[rel] = f
		}
		return fn(ctx, f, p)
	})
}

func (h *hostSide) travel(_ context.Context, d *served, p travelParams) (any, error) {
	t, ok := d.Device.(Traveler)
	if !ok {
		return nil, fmt.Errorf("the %s app's device cannot travel a route", p.App)
	}
	// The journey outlives the step that starts it.
	arrived, err := t.Travel(context.WithoutCancel(h.ctx), p.Route)
	if err != nil {
		return nil, err
	}
	go func() {
		select {
		case <-arrived:
			h.srv.Tell("arrived", toldArrival{Scenario: p.Scenario, App: p.App, Journey: p.Journey})
		case <-h.ctx.Done():
		}
	}()
	return nil, nil
}

func (p nodeParams) node() *Node {
	if p.Screen == nil || p.Node < 0 || p.Node >= len(p.Screen.Nodes) {
		return nil
	}
	return p.Screen.screen().Nodes[p.Node]
}

func (p textParams) ref() appRef         { return p.appRef }
func (p nodeParams) ref() appRef         { return p.appRef }
func (p filesParams) ref() appRef        { return p.appRef }
func (p notificationParams) ref() appRef { return p.appRef }
func (p travelParams) ref() appRef       { return p.appRef }
