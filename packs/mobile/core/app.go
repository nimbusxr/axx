package mobilecore

import (
	"context"
	"fmt"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
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
	// Describe is the device and app for a failed scenario's context.
	Describe() map[string]any
	// Stop ends the session and gives the device back; failed tells whether
	// the scenario failed.
	Stop(failed bool) error
}

var apps = core.NewStateKey(Name+"/apps", func(*core.Scenario) *core.Services[*App] {
	return core.NewServices[*App]("mobile app", `No mobile app is registered in this scenario; register one with "the {word} android app with the following properties:"`)
}, nil)

// Register adds a platform's app to the scenario's apps.
func Register(sc *core.Scenario, app *App) error { return apps.Of(sc).Add(app.Name, app) }

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
	app, err := apps.Of(sc).Get(name)
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
