package mobilecore

import (
	"context"
	"fmt"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// App is a mobile app a scenario registered: an Android or an iOS app, which
// its platform's pack runs.
type App struct {
	Name     string
	Platform Platform
}

// Platform runs an app: the Android or iOS pack.
type Platform interface {
	// Kind is the platform, as registrations name it: android or ios.
	Kind() string
	// RunsHere is whether this machine can run the app: an iOS simulator
	// needs a Mac, a device farm does not.
	RunsHere() bool
	// Start leases a device for the scenario, resets the app and what it can
	// see on it, and starts a session of the app, which the scenario then has
	// until it ends.
	Start(sc *core.Scenario) (Device, error)
}

// Device is an app running on the device its scenario leased.
type Device interface {
	Session() *appium.Session
	// Screen reads what the app shows.
	Screen(ctx context.Context) (*Screen, error)
	// Launch brings the app to the front, started afresh if it was not
	// running; Restart stops and starts it; Background sends it to the
	// background.
	Launch(ctx context.Context) error
	Restart(ctx context.Context) error
	Background(ctx context.Context) error
	// OpenLink opens a link in the app, as tapping it elsewhere would.
	OpenLink(ctx context.Context, url string) error
	// HideKeyboard closes the on-screen keyboard when it shows, as a person does to reach what
	// it covers, and reports whether it did.
	HideKeyboard(ctx context.Context) (bool, error)
	// ScrollToShow scrolls until the control of that name shows: n is the node the screen has
	// for it, out of view, or nil when the screen does not list what it does not show. It reports
	// whether it could.
	ScrollToShow(ctx context.Context, n *Node, name string) (bool, error)
	// Swipe swipes the whole screen in a direction: up, down, left or right.
	Swipe(ctx context.Context, direction string) error
	// Scroll scrolls the screen's scrollable content one screenful toward
	// direction, and reports whether it moved.
	Scroll(ctx context.Context, direction string) (bool, error)
	// OpenNotifications shows the device's notifications over the app, until
	// they are closed again.
	OpenNotifications(ctx context.Context) (Notifications, error)
	// SystemBars are where the device's own bars are, which its screenshots
	// leave out: the status bar with its clock, the navigation bar.
	SystemBars(ctx context.Context) ([]Rect, error)
	// ScreenKey tells screenshots of this device apart from others', like
	// android-parcels-pixel.
	ScreenKey() string
	// Files are the app's files at a path in its sandbox, on the device.
	Files(ctx context.Context, path string) (core.Files, error)
	// Describe is the device and app for a failed scenario's context.
	Describe() map[string]any
	// Stop ends the session and gives the device back; failed tells whether
	// the scenario failed.
	Stop(failed bool) error
}

// Register adds a platform's app to the scenario's apps, which app-core
// keeps: one app runs on one platform in a scenario.
func Register(sc *core.Scenario, app *App) error {
	app.Name = appcore.Named(sc, app.Name)
	return appcore.Register(sc, &appcore.App{Name: app.Name, Platform: app.Platform.Kind(), Family: family{}, Data: app},
		app.Platform.RunsHere())
}

// mobileApp is the scenario's app of the name, which must be a mobile app.
func mobileApp(sc *core.Scenario, name string) (*App, error) {
	a, err := appcore.Get(sc, name)
	if err != nil {
		return nil, err
	}
	m, ok := a.Data.(*App)
	if !ok {
		return nil, fmt.Errorf("the %s app runs on %s: this step is for apps on phones", name, a.Platform)
	}
	return m, nil
}

// running is the scenario's apps that run, each on its device.
type running struct {
	mu      sync.Mutex
	devices map[string]Device
	order   []string
}

var scenarioRunning = core.NewStateKey(Name+"/running", func(sc *core.Scenario) *running {
	r := &running{devices: map[string]Device{}}
	sc.Describe(Name, func() any { return r.describe(sc) })
	return r
}, stopAll)

func (r *running) describe(sc *core.Scenario) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]any{}
	for name, d := range r.devices {
		out[name] = d.Describe()
	}
	return secrets.Mask(sc, fmt.Sprint(out))
}

func stopAll(sc *core.Scenario, r *running) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error
	for _, name := range r.order {
		if err := r.devices[name].Stop(sc.Status() == "failed"); err != nil {
			errs = append(errs, fmt.Errorf("the %s app: %w", name, err))
		}
	}
	r.devices = map[string]Device{}
	r.order = nil
	if len(errs) > 0 {
		return fmt.Errorf("stopping the mobile apps: %v", errs)
	}
	return nil
}

// device is the named app's device: started if start is true and it is not
// running yet.
func device(sc *core.Scenario, name string, start bool) (Device, error) {
	app, err := mobileApp(sc, name)
	if err != nil {
		return nil, err
	}
	r := scenarioRunning.Of(sc)
	r.mu.Lock()
	d, ok := r.devices[name]
	r.mu.Unlock()
	if ok {
		return d, nil
	}
	if !start {
		return nil, fmt.Errorf("the %s app is not running: launch it with \"the %s app is launched\", or open it with a link", name, name)
	}
	d, err = app.Platform.Start(sc)
	if err != nil {
		return nil, secrets.Hide(sc, err)
	}
	r.mu.Lock()
	r.devices[name] = d
	r.order = append(r.order, name)
	r.mu.Unlock()
	return d, nil
}
