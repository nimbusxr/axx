package mobileios

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/npm"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// runner runs an app for the scenarios that registered it.
type runner struct{ app *app }

func (r runner) Kind() string { return "ios" }

// RunsHere is whether this machine can run the app: a simulator needs a Mac,
// a device farm does not.
func (r runner) RunsHere() bool { return r.app.server != "" || runtime.GOOS == "darwin" }

// Start leases a simulator for the scenario and starts a session of the app
// on it, reset: the app installed afresh, its keychain and permissions
// reset, the permissions the registration grants granted, and the
// simulator's location set. The app is not launched yet.
func (r runner) Start(sc *core.Scenario) (mobilecore.Device, error) {
	a := r.app
	ctx := sc.Context()
	d := &running{app: a, bundleID: a.bundleID}
	client := &appium.Client{URL: a.server}
	caps := wdaSession
	if a.server == "" {
		p, err := poolFor(sc, a.device)
		if err != nil {
			return nil, err
		}
		// Waiting for a device another scenario has, or for one being made
		// ready (a first boot takes minutes), is not the step's own work.
		release := sc.Hold()
		dev, err := p.lease(ctx)
		release()
		if err != nil {
			return nil, err
		}
		d.pool, d.dev, d.key = p, dev, p.key
		// WebDriverAgent itself: the session is a request to it, the app not
		// launched yet (a step launches it, or opens a link).
		client = &appium.Client{URL: dev.wdaURL(), HTTP: &http.Client{}}
		if err := d.reset(ctx, sc.Suite()); err != nil {
			p.release(dev)
			return nil, err
		}
		if err := dev.ensureWDA(ctx); err != nil {
			p.release(dev)
			return nil, err
		}
	}
	if d.dev == nil {
		caps = a.capabilities(d.bundleID)
	}
	s, err := client.NewSession(ctx, caps)
	if err != nil {
		d.releaseDevice()
		return nil, fmt.Errorf("cannot start the %s app: %w", a.name, err)
	}
	d.session = s
	// The app's screen is the app's, even while a banner or the status bar
	// of SpringBoard's is in front.
	if err := s.Settings(ctx, map[string]any{"defaultActiveApplication": d.bundleID}); err != nil {
		_ = d.Stop(true)
		return nil, fmt.Errorf("cannot start the %s app: %w", a.name, err)
	}
	if d.dev != nil {
		// A scenario answers what it leaves unanswered as it ends (Stop); one
		// stopped short (a killed run on a kept simulator) leaves its dialog,
		// which outlives the app it asked for and would take this scenario's
		// taps. The app has not started yet, so a dialog now is left over.
		// The answer stays with the app installed afresh: it is reset again.
		left, err := dismissLeftDialogs(ctx, s)
		if err == nil && left > 0 {
			d.resets = nil
			err = d.reset(ctx, sc.Suite())
			d.resets = append(d.resets, "a dialog left open dismissed")
		}
		if err != nil {
			_ = d.Stop(true)
			return nil, fmt.Errorf("cannot start the %s app: a dialog an earlier scenario left open: %w", a.name, err)
		}
	}
	return d, nil
}

// dismissLeftDialogs dismisses the dialogs the system shows, one after
// another, and says how many there were.
func dismissLeftDialogs(ctx context.Context, s *appium.Session) (int, error) {
	n := 0
	for range 5 {
		text, err := s.AlertText(ctx)
		if noAlert(err) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		if err := s.DismissAlert(ctx); err != nil && !noAlert(err) {
			// A dialog without a button that declines.
			if err := s.AcceptAlert(ctx); err != nil && !noAlert(err) {
				return n, err
			}
		}
		n++
		// The dialog takes a moment to go, and another can follow it.
		for end := time.Now().Add(2 * time.Second); time.Now().Before(end); {
			t, err := s.AlertText(ctx)
			if noAlert(err) || (err == nil && t != text) {
				break
			}
			select {
			case <-ctx.Done():
				return n, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	return n, fmt.Errorf("dialogs keep showing")
}

func noAlert(err error) bool {
	var ae *appium.Error
	return errors.As(err, &ae) && ae.Code == "no such alert"
}

// wdaSession is what a session asks WebDriverAgent for, as Appium's XCUITest driver asks it
// for a simulator whose app it leaves to axx: no app launched or ended with the session, and
// every action waits for the app to be still (a tap on a dialog still coming in is lost
// otherwise, as the Save Password dialog after a sign-in). The software keyboard shows, as on
// a phone, rather than the Mac's keyboard standing in for it.
var wdaSession = map[string]any{
	"shouldWaitForQuiescence":                true,
	"eventloopIdleDelaySec":                  0,
	"maxTypingFrequency":                     60,
	"shouldUseSingletonTestManager":          true,
	"shouldTerminateApp":                     false,
	"forceAppLaunch":                         false,
	"useNativeCachingStrategy":               true,
	"forceSimulatorSoftwareKeyboardPresence": true,
}

// capabilities are what a session asks an Appium server of the project's own
// or a device farm's for: it installs the app afresh itself.
func (a *app) capabilities(bundleID string) map[string]any {
	caps := map[string]any{
		"platformName":             "iOS",
		"appium:automationName":    "XCUITest",
		"appium:noReset":           false,
		"appium:autoLaunch":        false,
		"appium:newCommandTimeout": 0, // the scenario holds the session
	}
	if a.path != "" {
		caps["appium:app"] = a.path
	}
	if bundleID != "" {
		caps["appium:bundleId"] = bundleID
	}
	for k, v := range a.caps {
		caps[k] = v
	}
	return caps
}

// running is an app running on a simulator for a scenario.
type running struct {
	app      *app
	pool     *pool
	dev      *device
	key      string
	bundleID string
	session  *appium.Session
	resets   []string
	// resetAt is when the scenario reset the simulator, and appDir the app's bundle (Courier.app):
	// what the simulator logged of the app since, a failure shows.
	resetAt time.Time
	appDir  string
	// stopTravel ends the route the phone is on (Travel), and waits for it to stop.
	stopTravel func()
}

func (d *running) Session() *appium.Session { return d.session }

// Travel has the simulator report the phone moving along the route, as CoreLocation reports a
// person walking or driving: each run of legs at one speed is a simctl location scenario, which
// moves the phone and gives its speed and course, every second. The phone stays at the last point.
func (d *running) Travel(_ context.Context, r mobilecore.Route) (<-chan struct{}, error) {
	if d.stopTravel != nil {
		d.stopTravel() // a new route replaces the one before
	}
	ctx, cancel := context.WithCancel(context.Background())
	arrived, done := make(chan struct{}), make(chan struct{})
	udid, set := d.dev.udid, d.dev.set
	go func() {
		defer close(done)
		for _, run := range runs(r) {
			// A stop: the phone stands where it is.
			args := []string{"location", udid, "set", waypoint(run.Legs[0].From)}
			if run.Legs[0].Speed > 0 {
				args = []string{"location", udid, "start", "--speed=" + strconv.FormatFloat(run.Legs[0].Speed, 'f', 3, 64), "--interval=1", waypoint(run.Legs[0].From)}
				for _, l := range run.Legs {
					args = append(args, waypoint(l.To))
				}
			}
			if _, err := set.simctl(ctx, args...); err != nil {
				return
			}
			select {
			case <-time.After(run.Duration()):
			case <-ctx.Done():
				return
			}
		}
		last := r.Legs[len(r.Legs)-1].To
		_, _ = set.simctl(ctx, "location", udid, "set", waypoint(last))
		close(arrived)
	}()
	d.stopTravel = func() { cancel(); <-done; d.stopTravel = nil }
	return arrived, nil
}

// runs are a route's legs, the ones in a row at one speed together.
func runs(r mobilecore.Route) []mobilecore.Route {
	var out []mobilecore.Route
	for _, l := range r.Legs {
		if n := len(out); n > 0 && out[n-1].Legs[len(out[n-1].Legs)-1].Speed == l.Speed {
			out[n-1].Legs = append(out[n-1].Legs, l)
			continue
		}
		out = append(out, mobilecore.Route{Legs: []mobilecore.Leg{l}})
	}
	return out
}

func waypoint(p mobilecore.Point) string {
	return strconv.FormatFloat(p.Lat, 'f', -1, 64) + "," + strconv.FormatFloat(p.Lon, 'f', -1, 64)
}

// reset leaves the simulator as the scenario starts from: the app installed
// afresh (with it go its data and its notifications), its keychain and
// permissions reset, the permissions the registration grants granted, and
// the simulator's location set, or none.
func (d *running) reset(ctx context.Context, s *core.Suite) error {
	udid, set := d.dev.udid, d.dev.set
	d.resetAt = time.Now()
	path := ""
	if d.app.path != "" {
		var err error
		path, err = appBundle(s, d.app.path)
		if err != nil {
			return fmt.Errorf("the %s app: %w", d.app.name, err)
		}
		d.appDir = filepath.Base(path)
		if d.bundleID == "" {
			if d.bundleID, err = bundleIDOf(ctx, path); err != nil {
				return fmt.Errorf("the %s app: %w", d.app.name, err)
			}
		}
	}
	_, _ = set.simctl(ctx, "terminate", udid, d.bundleID) // not running is fine
	if path != "" {
		if err := uninstall(ctx, set, udid, d.bundleID); err != nil {
			return err
		}
	}
	if _, err := set.simctl(ctx, "keychain", udid, "reset"); err != nil {
		return err
	}
	if _, err := set.simctl(ctx, "privacy", udid, "reset", "all", d.bundleID); err != nil {
		return err
	}
	if path != "" {
		if _, err := set.simctl(ctx, "install", udid, path); err != nil {
			return fmt.Errorf("cannot install the %s app: %w", d.app.name, err)
		}
		d.resets = append(d.resets, "app installed afresh")
	}
	d.resets = append(d.resets, "keychain reset", "permissions reset")
	for _, service := range d.app.permissions {
		if _, err := set.simctl(ctx, "privacy", udid, "grant", service, d.bundleID); err != nil {
			return fmt.Errorf("cannot grant the %s app %s (xcrun simctl help privacy lists what a simulator grants): %w", d.app.name, service, err)
		}
	}
	if len(d.app.permissions) > 0 {
		d.resets = append(d.resets, "permissions: "+strings.Join(d.app.permissions, ", "))
	}
	if loc := d.app.location; loc != nil {
		lat, lon := strconv.FormatFloat(loc[0], 'f', -1, 64), strconv.FormatFloat(loc[1], 'f', -1, 64)
		if _, err := set.simctl(ctx, "location", udid, "set", lat+","+lon); err != nil {
			return err
		}
		d.resets = append(d.resets, "location "+lat+", "+lon)
	} else if _, err := set.simctl(ctx, "location", udid, "clear"); err != nil {
		return err
	}
	if len(d.app.photos) > 0 {
		if _, err := set.simctl(ctx, append([]string{"addmedia", udid}, d.app.photos...)...); err != nil {
			return fmt.Errorf("cannot put the %s app's photos in the simulator's library: %w", d.app.name, err)
		}
		// The library cannot be emptied: the next scenario gets another simulator.
		d.dev.media = true
		d.resets = append(d.resets, "photos: "+strings.Join(d.app.photos, ", "))
	}
	if err := d.putDocuments(ctx); err != nil {
		return err
	}
	d.resets = append(d.resets, "language "+d.app.locale, "time zone "+d.app.timezone)
	return nil
}

// uninstall removes the app from the simulator, if it has it.
func uninstall(ctx context.Context, set simSet, udid, bundleID string) error {
	if _, err := set.simctl(ctx, "get_app_container", udid, bundleID); err != nil {
		return nil //nolint:nilerr // the app has no container there: it is not installed
	}
	_, err := set.simctl(ctx, "uninstall", udid, bundleID)
	return err
}

// appBundle is the app a registration names: a simulator build (.app), or
// it zipped, unzipped once for the run.
func appBundle(s *core.Suite, path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		if !strings.HasSuffix(path, ".app") {
			return "", fmt.Errorf("%s is not an app: a simulator build is a folder named <name>.app", path)
		}
		return path, nil
	}
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		return "", fmt.Errorf("%s is not an app: a simulator build is a folder named <name>.app, or it zipped", path)
	}
	return core.Cached(s, Name+"/unzipped/"+path, func() (string, error) {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		dir, err := os.MkdirTemp("", "axx-ios-app-")
		if err != nil {
			return "", err
		}
		s.OnClose(func(context.Context) error { return os.RemoveAll(dir) })
		if err := npm.Unzip(body, dir); err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		apps, _ := filepath.Glob(filepath.Join(dir, "*.app"))
		if len(apps) != 1 {
			return "", fmt.Errorf("%s holds %d apps (<name>.app) at its top, not one", path, len(apps))
		}
		return apps[0], nil
	})
}

// bundleIDOf reads an app's bundle identifier from its Info.plist.
func bundleIDOf(ctx context.Context, app string) (string, error) {
	out, err := exec.CommandContext(ctx, "plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", filepath.Join(app, "Info.plist")).Output() //nolint:gosec // macOS's plutil
	if err != nil {
		return "", fmt.Errorf("cannot read the bundle identifier of %s: %w", app, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *running) Screen(ctx context.Context) (*mobilecore.Screen, error) {
	src, err := d.session.Source(ctx)
	if err != nil {
		// An app that is not running shows nothing: it quit, or a step closed it.
		if state, serr := d.state(ctx); serr == nil && state < runningSuspended {
			return &mobilecore.Screen{}, nil
		}
		return nil, fmt.Errorf("cannot read the %s app's screen: %w", d.app.name, err)
	}
	return parseSource(src)
}

// runningSuspended is the first state XCUITest reports of a running app:
// 1 is not running, 2 suspended, 3 in the background, 4 in the foreground.
const runningSuspended = 2

// runningForeground is the state XCUITest reports of the app in front.
const runningForeground = 4

func (d *running) state(ctx context.Context) (int, error) {
	var state int
	err := d.appCommand(ctx, "queryAppState", "state", map[string]any{"bundleId": d.bundleID}, &state)
	return state, err
}

// appCommand runs one of the app commands: WebDriverAgent's own (/wda/apps/<path>)
// on a simulator axx runs, Appium's mobile: command (command) on another
// server.
func (d *running) appCommand(ctx context.Context, command, path string, args map[string]any, out any) error {
	if d.dev == nil {
		return d.session.Mobile(ctx, command, args, out)
	}
	return d.session.Command(ctx, http.MethodPost, "/wda/apps/"+path, args, out)
}

// Launch brings the app to the front; one that is not running starts in the
// registration's language, region and time zone.
func (d *running) Launch(ctx context.Context) error {
	state, err := d.state(ctx)
	if err != nil {
		return err
	}
	if state >= runningSuspended {
		return d.appCommand(ctx, "activateApp", "activate", map[string]any{"bundleId": d.bundleID}, nil)
	}
	return d.start(ctx)
}

func (d *running) start(ctx context.Context) error {
	env := map[string]string{"TZ": d.app.timezone}
	if d.app.camera != nil {
		for k, v := range d.app.camera.env() {
			env[k] = v
		}
	}
	args := map[string]any{
		"bundleId":    d.bundleID,
		"arguments":   d.app.launchArguments(),
		"environment": env,
	}
	err := d.appCommand(ctx, "launchApp", "launch", args, nil)
	// The app is installed afresh before each scenario, and SpringBoard learns of it a moment
	// after simctl does: a launch in between is refused as an app it does not know.
	for deadline := time.Now().Add(30 * time.Second); err != nil && strings.Contains(err.Error(), "is unknown to FrontBoard") && time.Now().Before(deadline); {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
		err = d.appCommand(ctx, "launchApp", "launch", args, nil)
	}
	if err == nil || !strings.Contains(err.Error(), "Timed out attempting to launch app") {
		return err
	}
	// XCTest gives up on a launch that takes long on a busy Mac, though the app may be up all the
	// same: one in front is launched; else it is launched once more.
	if state, serr := d.state(ctx); serr == nil && state == runningForeground {
		return nil
	}
	return d.appCommand(ctx, "launchApp", "launch", args, nil)
}

func (d *running) Restart(ctx context.Context) error {
	if err := d.appCommand(ctx, "terminateApp", "terminate", map[string]any{"bundleId": d.bundleID}, nil); err != nil {
		return err
	}
	return d.start(ctx)
}

// Background sends the app to the background, as the home gesture does.
func (d *running) Background(ctx context.Context) error {
	if d.dev == nil {
		return d.session.Mobile(ctx, "backgroundApp", map[string]any{"seconds": -1}, nil)
	}
	return d.session.ServerCommand(ctx, http.MethodPost, "/wda/homescreen", nil, nil)
}

// OpenLink opens a link in the app, which starts first when it is not
// running, so that it has the registration's language and time zone.
func (d *running) OpenLink(ctx context.Context, url string) error {
	state, err := d.state(ctx)
	if err != nil {
		return err
	}
	if state < runningSuspended {
		if err := d.start(ctx); err != nil {
			return err
		}
	}
	args := map[string]any{"url": url, "bundleId": d.bundleID}
	if d.dev == nil {
		return d.session.Mobile(ctx, "deepLink", args, nil)
	}
	// XCTest opens it in the app itself, as the app's own link: simctl openurl
	// hands it to the system, which may not pass it on (DTCD's permit links).
	return d.session.Command(ctx, http.MethodPost, "/url", args, nil)
}

// Swipe swipes across the screen, as a finger does. Down pulls, as a person pulls a list to
// refresh it: from high on the screen to low, held there a moment. XCUITest's own swipe down is a
// flick too short for that: it never refreshed a SwiftUI List, on an idle Mac or a busy one.
func (d *running) Swipe(ctx context.Context, direction string) error {
	if direction == "down" {
		w, err := d.session.Window(ctx)
		if err != nil {
			return err
		}
		x := w.Width / 2
		return d.session.DragAndHold(ctx, x, w.Height*0.3, x, w.Height*0.75, 500*time.Millisecond, 300*time.Millisecond)
	}
	args := map[string]any{"direction": direction}
	if d.dev == nil {
		return d.session.Mobile(ctx, "swipe", args, nil)
	}
	return d.session.Gesture(ctx, "/wda/swipe", "swipe", args)
}

// Scroll scrolls the first thing on the screen that scrolls, and tells
// whether the screen changed.
func (d *running) Scroll(ctx context.Context, direction string) (bool, error) {
	before, err := d.session.Source(ctx)
	if err != nil {
		return false, err
	}
	s, err := parseSource(before)
	if err != nil {
		return false, err
	}
	// The scroll view the screen shows; else one on the screen that iOS 27 says is not visible
	// (a SwiftUI List's collection view).
	var views []*mobilecore.Node
	for _, n := range s.Nodes {
		if n.Scrolling && n.Displayed {
			views = append(views, n)
		}
	}
	if len(views) == 0 {
		for _, n := range s.Nodes {
			if n.Scrolling && onScreen(n, s.Size) {
				views = append(views, n)
			}
		}
	}
	if len(views) == 0 {
		return false, nil
	}
	// With the keyboard up, the finger goes in the part of the view above it: the platform's own
	// scroll swipes low in the view, on the keys, and types.
	if v := uncovered(views[0]); v.Height > 0 && v.Height < views[0].Bounds.Height {
		x, top, bottom := v.X+v.Width/2, v.Y+v.Height*0.2, v.Y+v.Height*0.8
		from, to := bottom, top // down: the finger pushes the view up
		if direction == "up" {
			from, to = top, bottom
		}
		if err := d.session.Drag(ctx, x, from, x, to, 500*time.Millisecond); err != nil {
			return false, err
		}
		after, err := d.session.Source(ctx)
		return after != before, err
	}
	el, err := d.session.Find(ctx, views[0].Using, views[0].Value)
	if err != nil {
		return false, err
	}
	args := map[string]any{"elementId": el.ID, "direction": direction}
	if d.dev == nil {
		err = d.session.Mobile(ctx, "scroll", args, nil)
	} else {
		err = d.session.Gesture(ctx, "/wda/element/"+el.ID+"/scroll", "scroll", args)
	}
	if err != nil {
		return false, err
	}
	after, err := d.session.Source(ctx)
	return after != before, err
}

// ScrollRow scrolls a row that scrolls sideways by most of its width, and tells whether the
// screen changed: a finger drags along it, the other way.
func (d *running) ScrollRow(ctx context.Context, row *mobilecore.Node, direction string) (bool, error) {
	before, err := d.session.Source(ctx)
	if err != nil {
		return false, err
	}
	b := row.Bounds
	y, from, to := b.Y+b.Height/2, b.X+b.Width*0.85, b.X+b.Width*0.15
	if direction == "left" {
		from, to = to, from
	}
	if err := d.session.Drag(ctx, from, y, to, y, 500*time.Millisecond); err != nil {
		return false, err
	}
	after, err := d.session.Source(ctx)
	return after != before, err
}

// OpenNotifications opens Notification Center with a finger from the top
// edge; SpringBoard shows it, and activating the app closes it again.
func (d *running) OpenNotifications(ctx context.Context) (mobilecore.Notifications, error) {
	w, err := d.session.Window(ctx)
	if err != nil {
		return nil, err
	}
	if err := d.session.Settings(ctx, map[string]any{"defaultActiveApplication": springboard}); err != nil {
		return nil, err
	}
	// A banner iOS shows of a notification (the app's own, while it was in front) stays at the top
	// for a few seconds, where a finger pulling Notification Center down takes hold of it instead,
	// and a pull that comes as the app is still leaving the screen opens nothing. So Notification
	// Center is pulled down once no banner shows, and again, as a person does, until it shows. A
	// banner can come in as the finger pulls (on GitHub's runner, seconds after the app posted its
	// notification): the pull then opens nothing, and the banner is waited out before the next.
	for pull := 0; ; pull++ {
		open, err := d.bannersGone(ctx)
		if err != nil {
			return nil, err
		}
		if open {
			return notificationCenter{d}, nil
		}
		if err := d.session.Drag(ctx, w.Width*0.2, 0, w.Width*0.2, w.Height*0.7, 400*time.Millisecond); err != nil {
			return nil, fmt.Errorf("cannot open Notification Center: %w", err)
		}
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
			if d.notificationCenterShows(ctx) {
				return notificationCenter{d}, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		if pull == 2 {
			return nil, errors.New("the notifications did not open: Notification Center was pulled down from the top of the screen three times")
		}
	}
}

// notificationRows finds Notification Center's rows, each of a notification (or the stack of an
// app's), labeled with all its texts. A banner shows a notification as a row does, but is in no
// row: so a banner is never taken for Notification Center, nor for one of its notifications.
const notificationRows = "name == 'ListCell'"

// notificationCenterShows reports whether Notification Center shows: its date, or its rows.
func (d *running) notificationCenterShows(ctx context.Context) bool {
	els, err := d.session.FindAll(ctx, d.predicate(), "name == 'lockscreen-date-view' OR "+notificationRows)
	return err == nil && len(els) > 0
}

// bannersGone waits, up to 12 seconds, until no banner shows, and reports whether Notification
// Center shows (a pull may have opened it after all): while it is shut, a notification that shows
// is a banner.
func (d *running) bannersGone(ctx context.Context) (bool, error) {
	for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); {
		if d.notificationCenterShows(ctx) {
			return true, nil
		}
		banners, err := d.session.FindAll(ctx, d.predicate(), "name == 'NotificationShortLookView'")
		if err != nil || len(banners) == 0 {
			return false, nil //nolint:nilerr // no banner found: none to wait for
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return false, nil
}

// notificationCenter looks for notifications by asking for them, not by
// reading SpringBoard's whole screen: that takes a minute on a busy machine.
type notificationCenter struct{ d *running }

func (n notificationCenter) Shows(ctx context.Context, text string) (bool, error) {
	q := predicateString(text)
	els, err := n.d.session.FindAll(ctx, n.d.predicate(), "label CONTAINS[c] "+q+" OR value CONTAINS[c] "+q)
	if appium.IsNoSuchElement(err) || err == nil && len(els) == 0 {
		return false, n.unfold(ctx)
	}
	return len(els) > 0, err
}

// unfold shows the notifications iOS folded away: those already seen (as a banner, say) wait
// behind a "1 Notification" pill at the bottom of Notification Center, which a person taps.
func (n notificationCenter) unfold(ctx context.Context) error {
	pills, err := n.d.session.FindAll(ctx, n.d.predicate(), "type == 'XCUIElementTypeButton' AND label MATCHES '^[0-9]+ Notifications?$'")
	if err != nil || len(pills) == 0 {
		return nil //nolint:nilerr // nothing folded: nothing to unfold
	}
	return pills[0].Click(ctx)
}

// Texts are the notifications' texts: SpringBoard labels each with all of
// them, like "PARCELS COURIER, now, PX-MOB-9401 delivered, Signed by ...".
func (n notificationCenter) Texts(ctx context.Context) []string {
	els, err := n.d.session.FindAll(ctx, n.d.predicate(), "name == 'NotificationShortLookView'")
	if err != nil {
		return nil
	}
	var out []string
	for _, el := range els {
		if label, err := el.Attribute(ctx, "label"); err == nil && label != "" {
			out = append(out, label)
		}
	}
	return out
}

// NotificationLog is what the simulator logged of the app's notifications since the scenario
// reset it: the app's own lines (by its bundle identifier, as apps name their log) and its
// background tasks', and what iOS's notification service did with its requests. The last 40.
func (n notificationCenter) NotificationLog(ctx context.Context) string {
	d := n.d
	if d.dev == nil || d.resetAt.IsZero() {
		return ""
	}
	app := fmt.Sprintf("subsystem == %q", d.bundleID)
	if d.appDir != "" {
		app = fmt.Sprintf(`processImagePath CONTAINS %q AND (subsystem == %q OR subsystem BEGINSWITH "com.apple.BackgroundTasks" OR subsystem == "com.apple.UserNotifications")`, "/"+d.appDir+"/", d.bundleID)
	}
	// What became of the app's notification requests is in the daemon that takes them and in
	// SpringBoard, which runs them through its pipeline to Notification Center (iOS 27 runs it
	// there): their UserNotifications lines that name the app.
	predicate := fmt.Sprintf(`(%s) OR ((process == "usernotificationsd" OR process == "SpringBoard") AND `+
		`(subsystem BEGINSWITH "com.apple.UserNotifications" OR subsystem BEGINSWITH "com.apple.usernotificationsd") AND composedMessage CONTAINS %q)`, app, d.bundleID)
	out, err := d.dev.set.simctl(ctx, "spawn", d.dev.udid, "log", "show", "--start", d.resetAt.Format("2006-01-02 15:04:05"), "--style", "compact", "--predicate", predicate)
	if err != nil {
		return ""
	}
	return lastLines(out, 400, 300)
}

// AppLog is what the simulator logged of the app since the scenario reset it: its own process's
// lines, and the system's UserNotifications and BackgroundTasks lines that name it.
func (d *running) AppLog(ctx context.Context) string {
	if d.dev == nil || d.resetAt.IsZero() || d.appDir == "" {
		return ""
	}
	predicate := fmt.Sprintf(`processImagePath CONTAINS %q OR ((process == "usernotificationsd" OR process == "SpringBoard" OR process == "dasd") AND composedMessage CONTAINS %q)`,
		"/"+d.appDir+"/", d.bundleID)
	out, err := d.dev.set.simctl(ctx, "spawn", d.dev.udid, "log", "show", "--start", d.resetAt.Format("2006-01-02 15:04:05"), "--style", "compact", "--predicate", predicate)
	if err != nil {
		return ""
	}
	return lastLines(out, 3000, 500)
}

// lastLines are a log's last lines (its header and blank lines left out), each cut to width.
func lastLines(log string, n, width int) string {
	var lines []string
	for _, l := range strings.Split(log, "\n") {
		if l = strings.TrimSpace(l); l == "" || strings.HasPrefix(l, "Timestamp ") || strings.HasPrefix(l, "Filtering the log data") || strings.HasPrefix(l, "getpwuid_r") {
			continue
		}
		if len(l) > width {
			l = strings.ToValidUTF8(l[:width], "") + "…"
		}
		lines = append(lines, l)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// find is the row of the notification that shows a text in Notification Center, where it rests:
// it is taken once it stands still, as Notification Center slides in. A banner iOS shows of the
// notification is in no row, so it is never held or tapped in the row's place: on GitHub's
// runner, a banner that came in as Notification Center was pulled down was held as it went, and
// iOS opened the app for it. One already seen (as a banner) waits below the screen, where a
// person swipes up to bring it into view.
func (n notificationCenter) find(ctx context.Context, text string) (*appium.Element, appium.Rect, error) {
	q := predicateString(text)
	w, err := n.d.session.Window(ctx)
	if err != nil {
		return nil, appium.Rect{}, err
	}
	var last appium.Rect
	swipes, missing, moving := 0, 0, 0
	for {
		els, err := n.d.session.FindAll(ctx, n.d.predicate(), notificationRows+" AND (label CONTAINS[c] "+q+" OR value CONTAINS[c] "+q+")")
		if err != nil {
			return nil, appium.Rect{}, err
		}
		var el *appium.Element
		var at appium.Rect
		for _, e := range els {
			if r, err := e.Rect(ctx); err == nil {
				el, at = e, r
				break
			}
		}
		switch {
		case el == nil && missing == 10:
			return nil, appium.Rect{}, fmt.Errorf("no notification shows %q", text)
		case el == nil:
			// Notification Center shows its date as it opens, before its rows.
			missing++
		case at.Y >= w.Height && swipes < 2:
			swipes++
			if err := n.d.session.Drag(ctx, w.Width/2, w.Height*0.6, w.Width/2, w.Height*0.25, 300*time.Millisecond); err != nil {
				return nil, appium.Rect{}, fmt.Errorf("cannot bring the notifications below the screen into view: %w", err)
			}
			last = appium.Rect{}
			continue
		case at == last || moving == 10:
			return el, at, nil
		default:
			last = at
			moving++
		}
		select {
		case <-ctx.Done():
			return nil, appium.Rect{}, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Open taps the notification, as a person opens it, until SpringBoard opens the app where the
// notification leads. iOS answers a tap in one of three ways: it opens the app; on a phone
// without Face ID unlock, as a simulator is, it slides the notification aside to show an Open
// button on its row, which a person then taps; or, when the notification tops a stack of the
// app's notifications, it spreads the stack out, and a person taps the notification again.
func (n notificationCenter) Open(ctx context.Context, text string) error {
	q := predicateString(text)
	for tap := 0; tap < 3; tap++ {
		_, at, err := n.find(ctx, text)
		if err != nil {
			return err
		}
		// The notification's own button, on its row (a banner has one too), tapped as XCTest taps
		// a control: a finger's tap Notification Center can take for the start of a slide.
		buttons, err := n.d.session.FindAll(ctx, n.d.predicate(), "type == 'XCUIElementTypeButton' AND (name == 'ListCell' OR name BEGINSWITH 'ShortLook.Platter') AND label CONTAINS[c] "+q)
		if err != nil {
			return err
		}
		var button *appium.Element
		for _, b := range buttons {
			if r, err := b.Rect(ctx); err == nil && r.Y < at.Y+at.Height && r.Y+r.Height > at.Y {
				button = b
				break
			}
		}
		if button == nil {
			return fmt.Errorf("the notification %q has no button to open it", text)
		}
		if err := button.Click(ctx); err != nil {
			return err
		}
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			if n.appInFront(ctx) {
				return nil
			}
			if open, ok := n.openButton(ctx, at); ok {
				// A finger's tap where it shows: XCTest's tap of the button loses it as iOS
				// slides the row.
				x, y := open.X+open.Width/2, open.Y+open.Height/2
				if err := n.d.session.Drag(ctx, x, y, x, y, 0); err != nil {
					return err
				}
				deadline = time.Now().Add(5 * time.Second)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	return fmt.Errorf("the notification %q did not open the app: it was tapped three times", text)
}

// appInFront reports whether SpringBoard has brought the app to the front.
func (n notificationCenter) appInFront(ctx context.Context) bool {
	state, err := n.d.state(ctx)
	return err == nil && state == runningForeground
}

// openButton is where the Open button iOS shows beside a notification it slid aside is, on the
// notification's row; false while there is none.
func (n notificationCenter) openButton(ctx context.Context, row appium.Rect) (appium.Rect, bool) {
	buttons, err := n.d.session.FindAll(ctx, n.d.predicate(), "type == 'XCUIElementTypeButton' AND name == 'swipe-action-button-identifier'")
	if err != nil {
		return appium.Rect{}, false
	}
	for _, b := range buttons {
		if r, err := b.Rect(ctx); err == nil && r.Y < row.Y+row.Height && r.Y+r.Height > row.Y {
			return r, true
		}
	}
	return appium.Rect{}, false
}

// Act holds the notification, as a person does to see its actions, and taps the one of that
// name in the menu iOS shows. A hold that shows no menu (Notification Center still sliding in) is
// held again, once, as a person does. iOS shows the menu in Notification Center only on a phone
// with a proximity sensor: axx's library gives the simulator's SpringBoard one (springboard.go).
func (n notificationCenter) Act(ctx context.Context, text, action string) error {
	q := predicateString(action)
	for hold := 0; ; hold++ {
		el, _, err := n.find(ctx, text)
		if err != nil {
			return err
		}
		if err := el.TouchAndHold(ctx, 1500*time.Millisecond); err != nil {
			return fmt.Errorf("cannot hold the notification: %w", err)
		}
		for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline); {
			buttons, err := n.d.session.FindAll(ctx, n.d.predicate(), "type == 'XCUIElementTypeButton' AND label ==[c] "+q)
			if err == nil && len(buttons) > 0 {
				return buttons[0].Click(ctx)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		if hold == 1 {
			break
		}
	}
	var names []string
	all, _ := n.d.session.FindAll(ctx, n.d.predicate(), "type == 'XCUIElementTypeButton'")
	for _, b := range all {
		if label, err := b.Attribute(ctx, "label"); err == nil && label != "" {
			names = append(names, label)
		}
	}
	err := fmt.Errorf("the notification's menu has no action %q: %s", action, strings.Join(names, ", "))
	switch status := springboardStatus(n.d.dev.udid); status {
	case "ok":
		return err
	case "":
		status = "it runs without axx's library"
		fallthrough
	default:
		return fmt.Errorf("%w (iOS shows a notification's actions in Notification Center only on a phone with a proximity sensor, which axx gives the simulator's SpringBoard: %s)", err, status)
	}
}

// Close brings the app back. activateApp returns as Notification Center starts
// to slide away, and a tap before it has gone lands on it, not on the app: so
// it waits, up to five seconds, for SpringBoard to stop showing notifications.
func (n notificationCenter) Close(ctx context.Context) error {
	if err := n.d.appCommand(ctx, "activateApp", "activate", map[string]any{"bundleId": n.d.bundleID}, nil); err != nil {
		return err
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		els, err := n.d.session.FindAll(ctx, n.d.predicate(), "name == 'NotificationShortLookView'")
		if err != nil || len(els) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return n.d.session.Settings(ctx, map[string]any{"defaultActiveApplication": n.d.bundleID})
}

// predicate is the locator strategy of an NSPredicate: WebDriverAgent's name for it, or Appium's
// on an Appium server.
func (d *running) predicate() string {
	if d.dev == nil {
		return "-ios predicate string"
	}
	return "predicate string"
}

// predicateString is a text as a string of an NSPredicate.
func predicateString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// springboard is iOS's home screen app, which shows notifications.
const springboard = "com.apple.springboard"

// SystemBars is the status bar, with its clock, in the screenshot's pixels.
// ScrollToShow drags the node's scroll view until the node shows, as a finger does: steady
// drags (no flick), each as far as the node still is from the view's middle, at most most of the
// view's height, watching where the node is. XCUITest's own scroll to an element works through
// table cells only; a SwiftUI scroll view of plain text has none.
func (d *running) ScrollToShow(ctx context.Context, n *mobilecore.Node, _ string) (bool, error) {
	if n == nil {
		return false, nil
	}
	view := n.Parent
	for view != nil && !view.Scrolling {
		view = view.Parent
	}
	if view == nil || n.Using == "" || view.Bounds.Height <= 0 {
		return false, nil
	}
	el, err := d.session.Find(ctx, n.Using, n.Value)
	if err != nil {
		return false, err
	}
	v := uncovered(view)
	x, middle, reach := v.X+v.Width/2, v.Y+v.Height/2, v.Height*0.6
	if err := d.scrub(ctx, el, view, v); err != nil {
		return false, err
	}
	last := math.NaN()
	for range 200 {
		r, err := el.Rect(ctx)
		if err != nil {
			return false, err
		}
		if r.Y >= v.Y && r.Y+r.Height <= v.Y+v.Height {
			return true, nil
		}
		if r.Y == last { // the view does not move: at its end, or it does not scroll so
			return false, nil
		}
		last = r.Y
		far := math.Max(-reach, math.Min(reach, r.Y+r.Height/2-middle))
		from := middle + far/2
		if err := d.session.Drag(ctx, x, from, x, from-far, 500*time.Millisecond); err != nil {
			return false, err
		}
	}
	return false, nil
}

// uncovered is the part of a scroll view the keyboard leaves showing: a finger that lands on the
// keyboard types, and a control under it does not show.
func uncovered(view *mobilecore.Node) mobilecore.Rect {
	v := view.Bounds
	root := view
	for root.Parent != nil {
		root = root.Parent
	}
	if k := keyboardOf(root); k != nil && k.Bounds.Height > 0 && k.Bounds.Y > v.Y && k.Bounds.Y < v.Y+v.Height {
		v.Height = k.Bounds.Y - v.Y
	}
	return v
}

// keyboardOf is the keyboard on the screen n is the top of, or nil.
func keyboardOf(n *mobilecore.Node) *mobilecore.Node {
	if n.Class == "XCUIElementTypeKeyboard" {
		return n
	}
	for _, c := range n.Children {
		if k := keyboardOf(c); k != nil {
			return k
		}
	}
	return nil
}

// pagesRE reads a scroll bar's label, like "Vertical scroll bar, 25 pages".
var pagesRE = regexp.MustCompile(`^Vertical scroll bar, (\d+) pages?$`)

// scrub takes a long scroll view to the control in one move, when it is more than a screen and
// a half away: as a person does in a long text, it grabs the scroll bar (shown by a nudge of the
// text: it shows only while the view moves) and drags it to where the control is. The drags after
// it take the view the last stretch. A view with no scroll bar, or a short one, is left as it is.
// v is the part of the view the keyboard leaves showing, where the nudge goes.
func (d *running) scrub(ctx context.Context, el *appium.Element, view *mobilecore.Node, v mobilecore.Rect) error {
	var bar *mobilecore.Node
	pages := 0.0
	for _, c := range view.Children {
		if m := pagesRE.FindStringSubmatch(c.Label); m != nil {
			bar = c
			pages, _ = strconv.ParseFloat(m[1], 64)
			break
		}
	}
	if bar == nil || pages < 3 || bar.Bounds.Height <= 0 {
		return nil
	}
	r, err := el.Rect(ctx)
	if err != nil {
		return err
	}
	t := bar.Bounds
	off := r.Y + r.Height/2 - (v.Y + v.Height/2)
	if math.Abs(off) < 1.5*v.Height {
		return nil
	}
	// Where the view is, and where the control is, as shares of how far it scrolls.
	at, _ := strconv.ParseFloat(strings.TrimSuffix(bar.Text, "%"), 64)
	from := at / 100
	to := math.Max(0, math.Min(1, from+off/((pages-1)*v.Height)))
	thumb := math.Max(t.Height/pages, 24)
	y := func(share float64) float64 { return t.Y + share*(t.Height-thumb) + thumb/2 }
	gx := t.X + t.Width*0.85 // the bar, drawn at the view's edge
	nudge := 40.0
	if to < from {
		nudge = -nudge
	}
	if err := d.pressDrag(ctx, v.X+v.Width/2, v.Y+v.Height/2+nudge/2, v.X+v.Width/2, v.Y+v.Height/2-nudge/2, 0.05); err != nil {
		return err
	}
	return d.pressDrag(ctx, gx, y(from), gx, y(to), 0.6)
}

// pressDrag presses a point for hold seconds and drags it to another, with WebDriverAgent's own
// gesture, which goes on at once: the W3C actions wait two seconds for the view to settle, by when
// the scroll bar has faded and a finger on it drags the text instead.
func (d *running) pressDrag(ctx context.Context, fromX, fromY, toX, toY, hold float64) error {
	args := map[string]any{"fromX": fromX, "fromY": fromY, "toX": toX, "toY": toY, "duration": hold}
	if d.dev == nil {
		return d.session.Mobile(ctx, "dragFromToForDuration", args, nil)
	}
	return d.session.Gesture(ctx, "/wda/dragfromtoforduration", "dragFromToForDuration", args)
}

// HideKeyboard closes the keyboard when it shows: an iPhone's keyboard has no key that only
// hides it, so it presses return (or done), as a person does to end the editing; never go, send
// or search, which would act.
func (d *running) HideKeyboard(ctx context.Context) (bool, error) {
	shown, err := d.keyboardShown(ctx)
	if err != nil || !shown {
		return false, err
	}
	keys := []string{"return", "Return", "done", "Done"}
	// WebDriverAgent taps one of these keys and gives the keyboard three seconds to go. When the
	// keyboard is still there, it says it knows no way to close it: the keyboard may be still
	// going, or still coming in, with no keys to tap yet. On a busy machine (a small CI runner) it
	// takes longer either way: it is given time to go, and closed again once it can be.
	for deadline := time.Now().Add(15 * time.Second); ; {
		if d.dev == nil {
			err = d.session.Mobile(ctx, "hideKeyboard", map[string]any{"keys": keys}, nil)
		} else {
			err = d.session.Command(ctx, http.MethodPost, "/wda/keyboard/dismiss", map[string]any{"keyNames": keys}, nil)
		}
		var ae *appium.Error
		if err == nil || !errors.As(err, &ae) || !strings.Contains(ae.Message, "Did not know how to dismiss the keyboard") || !time.Now().Before(deadline) {
			break
		}
		gone, gerr := d.keyboardGone(ctx, 2*time.Second)
		if gerr != nil {
			return false, gerr
		}
		if gone {
			return true, nil
		}
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// keyboardGone waits, up to within, for the keyboard to go, and reports whether it did.
func (d *running) keyboardGone(ctx context.Context, within time.Duration) (bool, error) {
	for deadline := time.Now().Add(within); ; {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
		if shown, err := d.keyboardShown(ctx); err == nil && !shown {
			return true, nil
		}
		if !time.Now().Before(deadline) {
			return false, nil
		}
	}
}

// keyboardShown is whether the keyboard shows: the screen has it.
func (d *running) keyboardShown(ctx context.Context) (bool, error) {
	if d.dev == nil {
		return d.session.KeyboardShown(ctx)
	}
	_, err := d.session.Find(ctx, "class name", "XCUIElementTypeKeyboard")
	if appium.IsNoSuchElement(err) {
		return false, nil
	}
	return err == nil, err
}

func (d *running) SystemBars(ctx context.Context) ([]mobilecore.Rect, error) {
	var info struct {
		StatusBarSize struct{ Width, Height float64 } `json:"statusBarSize"`
		Scale         float64                         `json:"scale"`
	}
	var err error
	if d.dev == nil {
		err = d.session.Mobile(ctx, "deviceScreenInfo", nil, &info)
	} else {
		err = d.session.Command(ctx, http.MethodGet, "/wda/screen", nil, &info)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read where the simulator's status bar is: %w", err)
	}
	if info.Scale == 0 {
		info.Scale = 1
	}
	return []mobilecore.Rect{{Width: info.StatusBarSize.Width * info.Scale, Height: info.StatusBarSize.Height * info.Scale}}, nil
}

// Stream is WebDriverAgent's screen stream; none for a farm's device.
func (d *running) Stream() string {
	if d.dev == nil || d.dev.mjpegPort == 0 {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(d.dev.mjpegPort)
}

// ScreenKey is ios- and the device type and iOS version, or the simulator
// the pool clones or uses, or the farm's device name.
func (d *running) ScreenKey() string {
	name := d.key
	if name == "" {
		name = d.app.device
	}
	if name == "" {
		name, _ = d.session.Capabilities["deviceName"].(string)
	}
	return "ios-" + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
}

func (d *running) Describe() map[string]any {
	out := map[string]any{"app": d.bundleID, "resets": d.resets}
	if d.dev != nil {
		out["simulator"] = d.dev.name
		out["udid"] = d.dev.udid
		if d.dev.source != "" {
			out["cloned from"] = d.dev.source
		}
		out["lease"] = "this scenario's own"
	} else {
		out["appium"] = d.app.server
	}
	return out
}

// Stop ends the session and gives the simulator back to its pool.
func (d *running) Stop(bool) error {
	var err error
	if d.session != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		// A dialog the scenario left unanswered outlives the app it asked
		// for. Answered now, what the answer records goes with the app when
		// the next scenario installs it afresh.
		_, derr := dismissLeftDialogs(ctx, d.session)
		err = errors.Join(derr, d.session.Delete(ctx))
	}
	d.releaseDevice()
	return err
}

func (d *running) releaseDevice() {
	if d.stopTravel != nil {
		d.stopTravel()
	}
	if d.pool != nil && d.dev != nil {
		d.pool.release(d.dev)
		d.dev = nil
	}
}
