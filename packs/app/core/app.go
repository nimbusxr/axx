package appcore

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
)

// App is an app a scenario registered: its name, the platform it runs on,
// and the family of packs that run apps on that platform.
type App struct {
	Name string
	// Platform is what it runs on, as its registration names it: android,
	// ios, macos, windows or linux.
	Platform string
	// Family carries out the app's steps.
	Family Family
	// Data is the platform pack's own, like how to start the app.
	Data any
}

// Family carries out the steps of app-core for the apps of its platforms:
// mobile-core for phones, desktop-core for desktops. Each method gets the
// scenario and the app; a check gets how long it may wait, and fails with a
// core assertion.
type Family interface {
	// Launch starts the app, reset for the scenario, or brings it to the
	// front; Restart stops it and starts it again, keeping what it stored.
	Launch(sc *core.Scenario, app *App) error
	Restart(sc *core.Scenario, app *App) error
	// Press presses a control, found by the name people see, as a person
	// does: a tap on a phone, a click on a desktop.
	Press(sc *core.Scenario, app *App, k Kind, name string) error
	// Fill replaces what a field holds with text, as typing it would.
	Fill(sc *core.Scenario, app *App, field, text string) error
	// ScrollIntoView scrolls until the control shows.
	ScrollIntoView(sc *core.Scenario, app *App, k Kind, name string) error
	// Shows and DoesNotShow check the app's text; Shown that a control
	// shows; Enabled whether it can be used; Value what a field holds; and
	// LooksLike the app against its screenshot of that name.
	Shows(sc *core.Scenario, app *App, text string, wait time.Duration) error
	DoesNotShow(sc *core.Scenario, app *App, text string, wait time.Duration) error
	Shown(sc *core.Scenario, app *App, k Kind, name string, wait time.Duration) error
	Enabled(sc *core.Scenario, app *App, k Kind, name string, enabled bool, wait time.Duration) error
	Value(sc *core.Scenario, app *App, field, want string, wait time.Duration) error
	LooksLike(sc *core.Scenario, app *App, screenshot string, wait time.Duration) error
	// Files are the app's files at a path in its file context: "./" where
	// it keeps its data, "~/" its home (a folder of the files pack whose
	// owner is the app).
	Files(sc *core.Scenario, app *App, path string) (core.Files, error)
}

// Host is the platform of this machine, as registrations name desktops:
// macos, windows or linux.
func Host() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	default:
		return runtime.GOOS
	}
}

// registration is an app's name, and its registrations: the one this
// machine runs, and those for platforms it cannot run.
type registration struct {
	app    *App
	others []string
}

type registry struct {
	mu   sync.Mutex
	apps map[string]*registration
}

var apps = core.NewStateKey(Name+"/apps", func(*core.Scenario) *registry {
	return &registry{apps: map[string]*registration{}}
}, nil)

// Register adds an app its platform's pack registered (ADR 0012). runsHere
// says whether this machine can run the platform: a registration for one it
// cannot run does nothing here, so a feature lists a desktop app once for
// each OS. One app registered for two platforms this machine can run (an
// Android and an iOS app of one name, on a Mac) is an error.
func Register(sc *core.Scenario, app *App, runsHere bool) error {
	r := apps.Of(sc)
	r.mu.Lock()
	defer r.mu.Unlock()
	reg := r.apps[app.Name]
	if reg == nil {
		reg = &registration{}
		r.apps[app.Name] = reg
	}
	switch {
	case !runsHere:
		reg.others = append(reg.others, app.Platform)
	case reg.app != nil && reg.app.Platform == app.Platform:
		return fmt.Errorf("the %s app is registered for %s twice in this scenario", app.Name, app.Platform)
	case reg.app != nil:
		return fmt.Errorf("the %s app is registered for %s and for %s, and this machine can run both: "+
			"an app runs on one platform in a scenario (and its Background); register the other under another name, or in a feature of its own",
			app.Name, reg.app.Platform, app.Platform)
	default:
		reg.app = app
	}
	return nil
}

// Get is the scenario's app of the name, as this machine runs it.
func Get(sc *core.Scenario, name string) (*App, error) {
	r := apps.Of(sc)
	r.mu.Lock()
	defer r.mu.Unlock()
	reg := r.apps[name]
	switch {
	case reg == nil:
		var names []string
		for n := range r.apps {
			names = append(names, n)
		}
		slices.Sort(names)
		known := ""
		if len(names) > 0 {
			known = " (it registers " + strings.Join(names, ", ") + ")"
		}
		return nil, fmt.Errorf("no app named %q is registered in this scenario%s; register it with "+
			`"the %s <platform> app with the following properties:", its platform one of android, ios, macos, windows, linux`, name, known, name)
	case reg.app == nil:
		return nil, fmt.Errorf("the %s app is registered for %s, which this machine (%s) cannot run: register it for this machine too, "+
			`like "the %s %s app with the following properties:"`, name, strings.Join(reg.others, " and "), Host(), name, Host())
	}
	return reg.app, nil
}
