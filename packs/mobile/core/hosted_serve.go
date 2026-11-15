package mobilecore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/hostscenario"
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
	h := &hostSide{ctx: ctx, run: run, register: register}
	return hostscenario.Serve(ctx, run, conn, hostscenario.Side{
		Ops: func(host *hostscenario.Host) map[string]hostwire.Handler {
			h.host = host
			return h.ops()
		},
		Begin: func(m *hostscenario.Mirror) any {
			return &mirror{key: m.Key, sc: m.SC, devices: map[string]*served{}}
		},
		End: func(_ context.Context, m *hostscenario.Mirror) error { return m.Data.(*mirror).end() },
	})
}

// ops are the operations the run asks of the phones: what a Device does,
// and nothing else.
func (h *hostSide) ops() map[string]hostwire.Handler {
	return map[string]hostwire.Handler{
		"register":   hostwire.Op(h.registerApp),
		"see":        hostwire.Op(h.see),
		"phone.move": hostwire.Op(h.movePhone),
		"start":      hostwire.Op(h.start),
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

		"files": fileOp(h, func(_ context.Context, f core.Files, _ filesParams) (any, error) { return f.Where(), nil }),
		"files.read": fileOp(h, func(ctx context.Context, f core.Files, p filesParams) (any, error) {
			body, found, err := f.Read(ctx, p.Name)
			return struct {
				Body  []byte `json:"body"`
				Found bool   `json:"found"`
			}{body, found}, err
		}),
		"files.write": fileOp(h, func(ctx context.Context, f core.Files, p filesParams) (any, error) {
			return nil, f.Write(ctx, p.Name, p.Body)
		}),
		"files.list":  fileOp(h, func(ctx context.Context, f core.Files, p filesParams) (any, error) { return f.List(ctx, p.Max) }),
		"files.empty": fileOp(h, func(ctx context.Context, f core.Files, _ filesParams) (any, error) { return nil, f.Empty(ctx) }),
	}
}

// hostSide is one run's connection, in the host.
type hostSide struct {
	ctx      context.Context
	host     *hostscenario.Host
	run      core.HostRun
	register func(sc *core.Scenario, app string, t *core.Table) error
}

// mirror is one of the run's scenarios, in the host (hostscenario.Mirror):
// the apps it registered and the devices they run on.
type mirror struct {
	key string
	sc  *core.Scenario

	mu      sync.Mutex
	devices map[string]*served
}

// served is an app on the device the host started for it.
type served struct {
	Device
	notifications Notifications
	files         map[string]core.Files
}

// in runs fn in the run's scenario, alone in it (hostscenario.Host.In).
func (h *hostSide) in(ctx context.Context, key string, fn func(m *mirror) (any, error)) (any, error) {
	return h.host.In(ctx, key, func(m *hostscenario.Mirror) (any, error) { return fn(m.Data.(*mirror)) })
}

func (h *hostSide) registerApp(ctx context.Context, p registerParams) (any, error) {
	return h.in(ctx, p.Scenario, func(m *mirror) (any, error) {
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
		sd := &served{Device: d, files: map[string]core.Files{}}
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
			h.host.Tell("touch", toldTouch{Scenario: m.key, App: app, Touch: t})
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
	if _, err := h.host.Mirror(p.Scenario); err != nil {
		// The scenario ended here already, and its apps with it.
		return deviceAnswer{}, nil //nolint:nilerr // a scenario that ended has nothing to stop
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

// end stops the apps a scenario left running, as it ends in the host.
func (m *mirror) end() error {
	m.mu.Lock()
	devices := m.devices
	m.devices = map[string]*served{}
	m.mu.Unlock()
	var errs []error
	for name, d := range devices {
		m.forget(name)
		if err := d.Stop(false); err != nil {
			errs = append(errs, fmt.Errorf("the %s app: %w", name, err))
		}
	}
	return errors.Join(errs...)
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

func fileOp(h *hostSide, fn func(ctx context.Context, f core.Files, p filesParams) (any, error)) hostwire.Handler {
	return deviceOp(h, func(ctx context.Context, d *served, p filesParams) (any, error) {
		// The path is one in the app's own files, whoever asks.
		rel, err := sandboxPath("./" + p.Path)
		if err != nil {
			return nil, err
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
			h.host.Tell("arrived", toldArrival{Scenario: p.Scenario, App: p.App, Journey: p.Journey})
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
