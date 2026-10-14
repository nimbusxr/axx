package mobileandroid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// runner runs an app for the scenarios that registered it.
type runner struct{ app *app }

func (r runner) Kind() string { return "android" }

// RunsHere is whether this machine can run the app: an emulator or a device
// runs on every OS.
func (r runner) RunsHere() bool { return true }

// Start leases a device for the scenario and starts a session of the app on
// it, reset: the app installed and its data cleared, the device's time zone
// and location set, and the app's permissions exactly those the registration
// grants. The app is not launched yet. A device of the machine's runs the
// UiAutomator2 server axx keeps on it; a device an Appium server runs (the
// appium property: a farm's), a session of that server.
func (r runner) Start(sc *core.Scenario) (mobilecore.Device, error) {
	a := r.app
	ctx := sc.Context()
	d := &running{app: a}
	if a.server != "" {
		return d.startOnAppium(ctx)
	}
	logDir := filepath.Join(sc.Suite().ProjectDir(), ".axx", "mobile")
	p, err := poolFor(sc, a.device)
	if err != nil {
		return nil, err
	}
	// Waiting for a device another scenario has, or for one being made
	// ready (a first boot takes minutes), is not the step's own work.
	release := sc.Hold()
	dev, err := p.lease(ctx, logDir)
	release()
	if err != nil {
		return nil, err
	}
	d.pool, d.dev, d.sdk = p, dev, p.sdk
	if err := p.startServer(ctx, dev, logDir); err != nil {
		d.releaseDevice()
		return nil, err
	}
	if err := d.resetDevice(ctx); err != nil {
		d.releaseDevice()
		return nil, err
	}
	if err := d.resetApp(ctx); err != nil {
		d.releaseDevice()
		return nil, err
	}
	caps := map[string]any{"platformName": "Android"}
	for k, v := range a.caps {
		caps[k] = v
	}
	s, err := dev.server.client.NewSession(ctx, caps)
	if err != nil {
		d.releaseDevice()
		return nil, fmt.Errorf("cannot start the %s app: %w", a.name, err)
	}
	d.session, d.logDir, d.caps = s, logDir, caps
	s.Revive = d.revive
	if err := d.grant(ctx); err != nil {
		_ = d.Stop(true)
		return nil, err
	}
	return d, nil
}

// startOnAppium starts a session of the app on an Appium server, which
// resets the app and holds the device.
func (d *running) startOnAppium(ctx context.Context) (mobilecore.Device, error) {
	a := d.app
	s, err := (&appium.Client{URL: a.server}).NewSession(ctx, a.capabilities(nil))
	if err != nil {
		return nil, fmt.Errorf("cannot start the %s app: %w", a.name, err)
	}
	d.session = s
	d.pkg = a.pkg
	if d.pkg == "" {
		d.pkg, _ = s.Capabilities["appPackage"].(string)
	}
	if err := d.grant(ctx); err != nil {
		_ = d.Stop(true)
		return nil, err
	}
	return d, nil
}

// resetApp leaves the app as a scenario starts from it: installed (the APK
// the registration names, the first time the device runs it in a run, over
// any version it has), stopped, its data cleared (and with it the
// permissions it was granted), and in the registration's language.
func (d *running) resetApp(ctx context.Context) error {
	a, serial := d.app, d.dev.serial
	d.pkg = a.pkg
	if d.pkg == "" {
		pkg, err := apkPackage(a.apk)
		if err != nil {
			return fmt.Errorf("the %s app's package: %w", a.name, err)
		}
		d.pkg = pkg
	}
	if a.apk != "" && !d.dev.installed[a.apk] {
		if _, err := d.sdk.install(ctx, serial, a.apk, "-t"); err != nil {
			return fmt.Errorf("cannot install the %s app: %w", a.name, err)
		}
		d.dev.installed[a.apk] = true
	}
	if _, err := d.sdk.shell(ctx, serial, "am", "force-stop", d.pkg); err != nil {
		return err
	}
	out, err := d.sdk.shell(ctx, serial, "pm", "clear", d.pkg)
	if err != nil {
		return fmt.Errorf("cannot clear the %s app's data: %w", a.name, err)
	}
	if !strings.Contains(out, "Success") {
		return fmt.Errorf("cannot clear the %s app's data (is %s installed?): %s", a.name, d.pkg, out)
	}
	lang, country := a.language()
	tag := lang
	if country != "" {
		tag += "-" + country
	}
	// The app's own language (Android 13 and later), which leaves the
	// device's alone.
	if _, err := d.sdk.shell(ctx, serial, "cmd", "locale", "set-app-locales", d.pkg, "--locales", tag); err != nil {
		return fmt.Errorf("cannot set the %s app's language to %s (an app's own language needs Android 13 or later): %w", a.name, tag, err)
	}
	d.resets = append(d.resets, "language "+tag)
	return nil
}

// capabilities are what the session asks Appium for.
func (a *app) capabilities(dev *device) map[string]any {
	lang, country := a.language()
	caps := map[string]any{
		"platformName":                  "Android",
		"appium:automationName":         "UiAutomator2",
		"appium:noReset":                false, // the app's data is cleared
		"appium:fullReset":              false,
		"appium:autoLaunch":             false, // a step launches it, or opens a link
		"appium:autoGrantPermissions":   false, // the registration's permissions only
		"appium:disableWindowAnimation": true,
		"appium:newCommandTimeout":      0, // the scenario holds the session
		"appium:language":               lang,
	}
	if country != "" {
		caps["appium:locale"] = country
	}
	if a.apk != "" {
		caps["appium:app"] = a.apk
		// The APK the scenario names is the app it tests: installed the first
		// time the device runs it, whatever version the device has.
		caps["appium:enforceAppInstall"] = dev == nil || !dev.installed[a.apk]
	}
	if a.pkg != "" {
		caps["appium:appPackage"] = a.pkg
	}
	if a.activity != "" {
		caps["appium:appActivity"] = a.activity
	}
	if dev != nil {
		caps["appium:udid"] = dev.serial
		caps["appium:systemPort"] = dev.systemPort
		caps["appium:mjpegServerPort"] = dev.mjpegPort
	}
	for k, v := range a.caps {
		caps[k] = v
	}
	return caps
}

// running is an app running on a device for a scenario.
type running struct {
	// component is the activity a launch starts, found once.
	component string
	app       *app
	pool      *pool
	dev       *device
	sdk       *sdk
	session   *appium.Session
	pkg       string
	resets    []string
	// logDir and caps are where the server's log goes and what its sessions ask, to start
	// both again should the server crash under the scenario (revive).
	logDir   string
	caps     map[string]any
	reviving sync.Mutex
	// stopFeed ends the emulator's location fixes (feedLocation) and waits for them to stop.
	stopFeed func()
	// route is the route the phone travels, which the location fixes follow.
	route travelling
}

func (d *running) Session() *appium.Session { return d.session }

// revive starts the UiAutomator2 server again after it crashed under the scenario (its own
// process: the app is as it was), and a session on it, which the scenario's becomes. Its log
// keeps the device's crash log, which says why. A server also ends when adb loses the device for
// a moment, and the app's host ports with it: revive gives the app those again.
func (d *running) revive(ctx context.Context) error {
	// A step and the recorder may both find the server dead: the first starts it again, and the
	// second finds the session answering.
	d.reviving.Lock()
	defer d.reviving.Unlock()
	if d.session.Alive(ctx) {
		return nil
	}
	if err := d.pool.startServer(ctx, d.dev, d.logDir); err != nil {
		return err
	}
	if err := d.reachHostPorts(ctx); err != nil {
		return err
	}
	s, err := d.dev.server.client.NewSession(ctx, d.caps)
	if err != nil {
		return err
	}
	d.session.Renew(s.ID)
	d.resets = append(d.resets, "the UiAutomator2 server started again after it crashed")
	return nil
}

// reachHostPorts makes what the app reaches as localhost: the registration's host ports, and none
// another scenario's registration gave. adb keeps them only while it has the device: when it
// loses the device under a scenario (an emulator that stalls), they go with it, and revive makes
// them again with the server.
func (d *running) reachHostPorts(ctx context.Context) error {
	if _, err := d.sdk.run(ctx, d.dev.serial, "reverse", "--remove-all"); err != nil {
		return err
	}
	for _, port := range d.app.hostPorts {
		tcp := "tcp:" + strconv.Itoa(port)
		if _, err := d.sdk.run(ctx, d.dev.serial, "reverse", tcp, tcp); err != nil {
			return fmt.Errorf("the %s app cannot reach port %d of this machine: %w", d.app.name, port, err)
		}
	}
	return nil
}

// resetDevice sets what the app sees of the device: nothing a scenario
// before left open over it (the notification shade, a dialog of the
// system's), its time zone and its location.
func (d *running) resetDevice(ctx context.Context) error {
	if _, err := d.sdk.shell(ctx, d.dev.serial, "cmd", "statusbar", "collapse"); err != nil {
		return err
	}
	// Like the dialog of another app that is not responding, which a slow
	// machine shows over everything.
	if _, err := d.sdk.shell(ctx, d.dev.serial, "am", "broadcast", "-a", "android.intent.action.CLOSE_SYSTEM_DIALOGS"); err != nil {
		return err
	}
	if _, err := d.sdk.shell(ctx, d.dev.serial, "settings", "put", "global", "auto_time_zone", "0"); err != nil {
		return err
	}
	if _, err := d.sdk.shell(ctx, d.dev.serial, "cmd", "alarm", "set-timezone", d.app.timezone); err != nil {
		return err
	}
	d.resets = append(d.resets, "time zone "+d.app.timezone)
	if d.dev.avd != "" {
		// Where the scenario before left an emulator is not where this one starts:
		// the registration's location, or the emulator's own.
		loc := defaultLocation
		if d.app.location != nil {
			loc = *d.app.location
		}
		lon, lat := strconv.FormatFloat(loc[1], 'f', -1, 64), strconv.FormatFloat(loc[0], 'f', -1, 64)
		if _, err := d.sdk.run(ctx, d.dev.serial, "emu", "geo", "fix", lon, lat); err != nil {
			return err
		}
		d.resets = append(d.resets, "location "+lat+", "+lon)
		d.feedLocation(lon, lat)
	}
	if err := d.putPhotos(ctx); err != nil {
		return err
	}
	if err := d.putDocuments(ctx); err != nil {
		return err
	}
	if err := d.reachHostPorts(ctx); err != nil {
		return err
	}
	if len(d.app.hostPorts) > 0 {
		ports := make([]string, len(d.app.hostPorts))
		for i, p := range d.app.hostPorts {
			ports[i] = strconv.Itoa(p)
		}
		d.resets = append(d.resets, "host ports "+strings.Join(ports, ", "))
	}
	return nil
}

// feedLocation has the emulator report where it is every second until the scenario ends, as a
// phone's GPS does. A geo fix is a single report: an app that asks for its current location,
// not the last one the phone had, waits for the next report, and none came. A route the phone
// travels (Travel) moves the reports along it, with the speed and bearing the emulator's GPS
// takes; an emulator without it gets plain geo fixes.
func (d *running) feedLocation(lon, lat string) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	sdk, serial, gps := d.sdk, d.dev.serial, d.dev.gps
	la, _ := strconv.ParseFloat(lat, 64)
	lo, _ := strconv.ParseFloat(lon, 64)
	at := mobilecore.Fix{Point: mobilecore.Point{Lat: la, Lon: lo}}
	go func() {
		defer close(done)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if f := d.route.at(now); f != nil {
					at = *f
				}
				if gps != nil && gps.set(ctx, at.Lat, at.Lon, at.Speed, at.Course) == nil {
					continue
				}
				_, _ = sdk.run(ctx, serial, "emu", "geo", "fix", strconv.FormatFloat(at.Lon, 'f', -1, 64), strconv.FormatFloat(at.Lat, 'f', -1, 64))
			}
		}
	}()
	// The next scenario on the emulator gets no fix of this one's.
	d.stopFeed = func() { cancel(); <-done }
}

// Travel has the emulator report the phone moving along the route, every second, with its speed
// and bearing; at the last point, it stays.
func (d *running) Travel(_ context.Context, r mobilecore.Route) (<-chan struct{}, error) {
	if d.stopFeed == nil {
		return nil, fmt.Errorf("the %s app's device reports no location of axx's to move: only an emulator axx starts does", d.app.name)
	}
	return d.route.set(r, time.Now()), nil
}

// travelling is the route the phone is on, if it is on one: where it is at a moment.
type travelling struct {
	mu      sync.Mutex
	r       *mobilecore.Route
	start   time.Time
	arrived chan struct{}
}

func (t *travelling) set(r mobilecore.Route, start time.Time) <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.r, t.start, t.arrived = &r, start, make(chan struct{})
	return t.arrived
}

// at is where the route has the phone at now; nil when the phone is on no route.
func (t *travelling) at(now time.Time) *mobilecore.Fix {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.r == nil {
		return nil
	}
	f, done := t.r.At(now.Sub(t.start))
	if done && t.arrived != nil {
		close(t.arrived)
		t.arrived = nil
	}
	return &f
}

// photosDir is the folder of the device's photo library axx puts a scenario's photos in.
const photosDir = "/sdcard/DCIM/axx"

// putPhotos takes the photos an earlier scenario put in the library out of it, and puts the
// registration's in, where the media store indexes them as it does a camera's.
func (d *running) putPhotos(ctx context.Context) error {
	serial := d.dev.serial
	// Out of the media store, then off the device: the store forgets a file it is told is gone,
	// not one that just disappeared. One command each, which adb hands the device's shell whole.
	// The store has the files under their real path (/storage/emulated/0/...), not /sdcard's.
	where := "\"_data LIKE '%/DCIM/axx/%'\""
	if _, err := d.sdk.shell(ctx, serial, "content delete --uri content://media/external/file --where "+where); err != nil {
		return fmt.Errorf("cannot take the photos of the scenario before out of the photo library: %w", err)
	}
	if _, err := d.sdk.shell(ctx, serial, "rm", "-rf", photosDir); err != nil {
		return err
	}
	if len(d.app.photos) == 0 {
		return nil
	}
	if _, err := d.sdk.shell(ctx, serial, "mkdir", "-p", photosDir); err != nil {
		return err
	}
	names := make([]string, len(d.app.photos))
	for i, f := range d.app.photos {
		names[i] = filepath.Base(f)
		dst := photosDir + "/" + names[i]
		if _, err := d.sdk.run(ctx, serial, "push", f, dst); err != nil {
			return fmt.Errorf("cannot put %s in the %s app's photo library: %w", f, d.app.name, err)
		}
		if _, err := d.sdk.shell(ctx, serial, "am", "broadcast", "-a", "android.intent.action.MEDIA_SCANNER_SCAN_FILE", "-d", "file://"+dst); err != nil {
			return err
		}
	}
	// The scan is the media store's own work, after the broadcast: the app is not started before
	// it has every photo.
	count := "content query --uri content://media/external/file --projection _id --where " + where
	for deadline := time.Now().Add(30 * time.Second); ; {
		out, err := d.sdk.shell(ctx, serial, count)
		if err == nil && strings.Count(out, "Row:") >= len(names) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the photo library did not take the %s app's photos in 30s (%s)", d.app.name, strings.Join(names, ", "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	d.resets = append(d.resets, "photos: "+strings.Join(names, ", "))
	return nil
}

// defaultLocation is where an emulator says it is when nothing set it: the
// latitude and longitude it starts with.
var defaultLocation = [2]float64{37.4219983, -122.084}

// grant leaves the app the permissions its registration grants, and no
// other: clearing its data revoked those it had.
func (d *running) grant(ctx context.Context) error {
	d.resets = append(d.resets, "app data cleared")
	if d.sdk == nil {
		return nil
	}
	for _, perm := range d.app.permissions {
		if _, err := d.sdk.shell(ctx, d.dev.serial, "pm", "grant", d.pkg, perm); err != nil {
			return fmt.Errorf("cannot grant the %s app %s: %w", d.app.name, perm, err)
		}
	}
	if len(d.app.permissions) > 0 {
		d.resets = append(d.resets, "permissions: "+strings.Join(d.app.permissions, ", "))
	}
	return nil
}

func (d *running) Screen(ctx context.Context) (*mobilecore.Screen, error) {
	src, err := d.session.Source(ctx)
	// Now and then the UiAutomator2 server fails to read a screen that changes under it (it
	// "cannot set AccessibilityNodeInfo's field 'mSealed'"), even with Android's hidden APIs
	// allowed: as the file picker's list of places slid shut. Read a moment later, it reads.
	for range 3 {
		var ae *appium.Error
		if err == nil || !errors.As(err, &ae) || !strings.Contains(ae.Message, "AccessibilityNodeInfo's field 'mSealed'") {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
		src, err = d.session.Source(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the %s app's screen: %w", d.app.name, err)
	}
	return parseSource(src)
}

// Launch brings the app to the front, as its launcher icon does: started if
// it was not running, where it was if it was.
func (d *running) Launch(ctx context.Context) error {
	if d.dev == nil {
		return d.session.Mobile(ctx, "activateApp", map[string]any{"appId": d.pkg}, nil)
	}
	component, err := d.launcher(ctx)
	if err != nil {
		return err
	}
	return d.am(ctx, "start", "-W", "-n", component, "-a", "android.intent.action.MAIN", "-c", "android.intent.category.LAUNCHER")
}

func (d *running) Restart(ctx context.Context) error {
	if d.dev == nil {
		if err := d.session.Mobile(ctx, "terminateApp", map[string]any{"appId": d.pkg}, nil); err != nil {
			return err
		}
		return d.Launch(ctx)
	}
	if _, err := d.sdk.shell(ctx, d.dev.serial, "am", "force-stop", d.pkg); err != nil {
		return err
	}
	return d.Launch(ctx)
}

func (d *running) Background(ctx context.Context) error {
	if d.dev == nil {
		return d.session.Mobile(ctx, "backgroundApp", map[string]any{"seconds": -1}, nil)
	}
	if _, err := d.sdk.shell(ctx, d.dev.serial, "input", "keyevent", "KEYCODE_HOME"); err != nil {
		return err
	}
	// The key goes home in its own time: a step after this one, as bringing the app back, would
	// race it, and on a busy emulator lose (the home screen came in over the app brought back).
	for end := time.Now().Add(10 * time.Second); ; {
		out, err := d.sdk.shell(ctx, d.dev.serial, "dumpsys", "activity", "activities")
		if err != nil {
			return err
		}
		if resumedPackage(out) != d.pkg {
			return nil
		}
		if time.Now().After(end) {
			return fmt.Errorf("the %s app is still in front after the home key", d.app.name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// resumedPackage is the package of the activity in front, as dumpsys activity says: its
// topResumedActivity (Android 10 and later), or its mResumedActivity.
func resumedPackage(dumpsys string) string {
	for _, line := range strings.Split(dumpsys, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "topResumedActivity=") && !strings.HasPrefix(line, "mResumedActivity:") && !strings.HasPrefix(line, "ResumedActivity:") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if pkg, _, ok := strings.Cut(f, "/"); ok && strings.Contains(pkg, ".") {
				return pkg
			}
		}
	}
	return ""
}

func (d *running) OpenLink(ctx context.Context, url string) error {
	if d.dev == nil {
		return d.session.Mobile(ctx, "deepLink", map[string]any{"url": url, "package": d.pkg, "waitForLaunch": true}, nil)
	}
	return d.am(ctx, "start", "-W", "-a", "android.intent.action.VIEW", "-d", q(url), d.pkg)
}

// am runs the activity manager on the device: it says what went wrong in
// what it prints, and exits 0 all the same.
func (d *running) am(ctx context.Context, args ...string) error {
	out, err := d.sdk.shell(ctx, d.dev.serial, append([]string{"am"}, args...)...)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Error") || strings.Contains(line, "Exception") {
			return fmt.Errorf("the %s app: %s", d.app.name, out)
		}
	}
	return nil
}

// launcher is the activity a launch starts: the registration's, or the one
// the device's launcher starts for the app.
func (d *running) launcher(ctx context.Context) (string, error) {
	if d.component != "" {
		return d.component, nil
	}
	if act := d.app.activity; act != "" {
		if !strings.Contains(act, "/") {
			act = d.pkg + "/" + act
		}
		d.component = act
		return act, nil
	}
	out, err := d.sdk.shell(ctx, d.dev.serial, "cmd", "package", "resolve-activity", "--brief",
		"-a", "android.intent.action.MAIN", "-c", "android.intent.category.LAUNCHER", d.pkg)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); strings.HasPrefix(last, d.pkg+"/") {
		d.component = last
		return last, nil
	}
	return "", fmt.Errorf("the %s app (%s) has no activity the launcher starts: name one with the activity property", d.app.name, d.pkg)
}

// Swipe swipes across the middle of the screen, as a finger does.
func (d *running) Swipe(ctx context.Context, direction string) error {
	w, err := d.session.Window(ctx)
	if err != nil {
		return err
	}
	area := appium.Area{Left: w.Width * 0.1, Top: w.Height * 0.25, Width: w.Width * 0.8, Height: w.Height * 0.5}
	if d.dev != nil {
		return d.session.Swipe(ctx, area, direction, 0.75)
	}
	return d.session.Mobile(ctx, "swipeGesture", map[string]any{
		"left": int(area.Left), "top": int(area.Top), "width": int(area.Width), "height": int(area.Height),
		"direction": direction, "percent": 0.75,
	}, nil)
}

// Scroll scrolls the first thing on the screen that scrolls.
func (d *running) Scroll(ctx context.Context, direction string) (bool, error) {
	s, err := d.Screen(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range s.Visible() {
		if !n.Scrolling {
			continue
		}
		// The gesture keeps a little inside the view's edges: one that starts on its edge does
		// not scroll a Compose scroll view (DTCD's terms).
		b := n.Bounds
		in := 0.05
		area := appium.Area{Left: b.X + b.Width*in, Top: b.Y + b.Height*in, Width: b.Width * (1 - 2*in), Height: b.Height * (1 - 2*in)}
		var more bool
		var err error
		if d.dev != nil {
			more, err = d.session.Scroll(ctx, area, direction, 0.8)
		} else {
			err = d.session.Mobile(ctx, "scrollGesture", map[string]any{
				"left": int(area.Left), "top": int(area.Top), "width": int(area.Width), "height": int(area.Height),
				"direction": direction, "percent": 0.8,
			}, &more)
		}
		if err != nil || more {
			return more, err
		}
		// A Compose scroll view tells UiAutomator it can scroll no further after every gesture
		// that scrolled it (DTCD's terms): what the screen shows says whether it moved, once
		// Compose has told accessibility (a moment after the gesture).
		before := shown(s)
		for range 4 {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
			after, err := d.Screen(ctx)
			if err != nil {
				return false, err
			}
			if shown(after) != before {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

// shown is the names of what the screen shows, in order: two screens with the same are where a
// scroll left them.
func shown(s *mobilecore.Screen) string {
	var names []string
	for _, n := range s.Visible() {
		names = append(names, n.Name())
	}
	return strings.Join(names, "\n")
}

// OpenNotifications opens the notification shade, which the screen then
// shows, and Back closes.
func (d *running) OpenNotifications(ctx context.Context) (mobilecore.Notifications, error) {
	open := func() error {
		if d.dev != nil {
			_, err := d.sdk.shell(ctx, d.dev.serial, "cmd", "statusbar", "expand-notifications")
			return err
		}
		return d.session.Mobile(ctx, "openNotifications", nil, nil)
	}
	if err := open(); err != nil {
		return nil, fmt.Errorf("cannot open the notification shade: %w", err)
	}
	hide := d.session.Back
	if d.dev != nil {
		if err := d.shadeOpened(ctx, open); err != nil {
			return nil, err
		}
		// Collapsed, not backed out of: once a notification opened the app, the shade is gone, and
		// a back would take the app back a screen. It is shut before the next step goes on, which
		// may open it again.
		hide = d.collapseShade
	}
	tap := func(ctx context.Context, x, y float64) error { return d.session.Drag(ctx, x, y, x, y, 0) }
	return mobilecore.ScreenNotifications{Read: d.Screen, Hide: hide, Tap: tap}, nil
}

// shadeOpened waits for the notification shade, opened, to have the focus, and opens it again if it
// has not taken it, up to four times: the shade slides open after the command returns, and a
// command sent as the launcher still comes in (the app just sent to the background) is lost. The
// screen read meanwhile would be the home screen's.
func (d *running) shadeOpened(ctx context.Context, open func() error) error {
	for range 4 {
		if d.shadeFocused(ctx, true) {
			return nil
		}
		if err := open(); err != nil {
			return fmt.Errorf("cannot open the notification shade: %w", err)
		}
	}
	if d.shadeFocused(ctx, true) {
		return nil
	}
	return errors.New("the notification shade did not open")
}

// collapseShade shuts the notification shade, and waits for it to let go of the focus: a tap
// before then goes to the shade, not the app. On a busy emulator the shade can keep the focus
// longer than three seconds: it is collapsed again then, up to three times.
func (d *running) collapseShade(ctx context.Context) error {
	for range 3 {
		if _, err := d.sdk.shell(ctx, d.dev.serial, "cmd", "statusbar", "collapse"); err != nil {
			return err
		}
		if d.shadeFocused(ctx, false) {
			return nil
		}
	}
	return errors.New("the notification shade did not close")
}

// shadeFocused waits, up to three seconds, for the notification shade to have the focus (open)
// or not to (shut), and reports whether it came to that.
func (d *running) shadeFocused(ctx context.Context, open bool) bool {
	for deadline := time.Now().Add(3 * time.Second); ; {
		out, err := d.sdk.shell(ctx, d.dev.serial, "dumpsys", "window", "displays")
		if err == nil && shadeHasFocus(out) == open {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// shadeHasFocus is whether the window with the focus, as dumpsys window says, is the
// notification shade's.
func shadeHasFocus(dumpsys string) bool {
	for _, line := range strings.Split(dumpsys, "\n") {
		if _, focus, ok := strings.Cut(line, "mCurrentFocus="); ok {
			return strings.Contains(focus, "NotificationShade")
		}
	}
	return false
}

// ScrollToShow scrolls the screen's scrolling view down until the control of that name shows, as
// a person looks for something in a long text: Android does not list what a scroll view holds
// out of view, so it looks each time the view is at rest. It turns a page twice; a control
// further down is in a long text (DTCD's terms, which took a minute a page at a time), which it
// flings through, many screens at once; what a fling went past, it turns back to a page at a
// time.
func (d *running) ScrollToShow(ctx context.Context, _ *mobilecore.Node, name string) (bool, error) {
	for i := range 52 {
		s, err := d.Screen(ctx)
		if err != nil || s.Shows(name) {
			return err == nil, err
		}
		var moved bool
		if i < 2 {
			moved, err = d.Scroll(ctx, "down")
		} else {
			moved, err = d.fling(ctx, s, "down")
		}
		if err != nil {
			return false, err
		}
		if !moved {
			break // at the end
		}
	}
	for range 200 {
		s, err := d.Screen(ctx)
		if err != nil || s.Shows(name) {
			return err == nil, err
		}
		moved, err := d.Scroll(ctx, "up")
		if err != nil || !moved {
			return false, err
		}
	}
	return false, nil
}

// fling flings the screen's first scrolling view toward direction, as a quick finger does, and
// reports whether it moved, once the view has come to rest.
func (d *running) fling(ctx context.Context, s *mobilecore.Screen, direction string) (bool, error) {
	for _, n := range s.Visible() {
		if !n.Scrolling {
			continue
		}
		// Inside the view's edges, as Scroll keeps.
		b, in := n.Bounds, 0.05
		area := appium.Area{Left: b.X + b.Width*in, Top: b.Y + b.Height*in, Width: b.Width * (1 - 2*in), Height: b.Height * (1 - 2*in)}
		var err error
		if d.dev != nil {
			_, err = d.session.Fling(ctx, area, direction)
		} else {
			err = d.session.Mobile(ctx, "flingGesture", map[string]any{
				"left": int(area.Left), "top": int(area.Top), "width": int(area.Width), "height": int(area.Height),
				"direction": direction,
			}, nil)
		}
		if err != nil {
			return false, err
		}
		// The view coasts after the fling (and a Compose view says it can go no further after
		// every one): it is at rest once two reads in a row agree.
		before := shown(s)
		last, still := before, 0
		for range 20 {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(150 * time.Millisecond):
			}
			after, err := d.Screen(ctx)
			if err != nil {
				return false, err
			}
			now := shown(after)
			if now != last {
				last, still = now, 0
			} else if still++; still >= 2 {
				break
			}
		}
		return last != before, nil
	}
	return false, nil
}

// HideKeyboard closes the keyboard when it shows, as the device's back gesture does.
func (d *running) HideKeyboard(ctx context.Context) (bool, error) {
	if d.dev == nil {
		shown, err := d.session.KeyboardShown(ctx)
		if err != nil || !shown {
			return false, err
		}
		if err := d.session.Mobile(ctx, "hideKeyboard", nil, nil); err != nil {
			return false, err
		}
		return true, nil
	}
	shown, err := d.keyboardShown(ctx)
	if err != nil || !shown {
		return false, err
	}
	// Escape closes it in most keyboards; back, in the others.
	for _, key := range []string{"KEYCODE_ESCAPE", "KEYCODE_BACK"} {
		if _, err := d.sdk.shell(ctx, d.dev.serial, "input", "keyevent", key); err != nil {
			return false, err
		}
		if shown, err := d.keyboardShown(ctx); err != nil || !shown {
			return true, err
		}
	}
	return true, nil
}

// keyboardShown is whether the input method shows its keyboard, as the
// device's input method service says.
func (d *running) keyboardShown(ctx context.Context) (bool, error) {
	out, err := d.sdk.shell(ctx, d.dev.serial, "dumpsys", "input_method")
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "mInputShown=true") || strings.Contains(out, "mIsInputViewShown=true"), nil
}

func (d *running) SystemBars(ctx context.Context) ([]mobilecore.Rect, error) {
	var bars map[string]json.RawMessage
	var err error
	if d.dev != nil {
		err = d.session.Command(ctx, http.MethodGet, "/appium/device/system_bars", nil, &bars)
	} else {
		err = d.session.Mobile(ctx, "getSystemBars", nil, &bars)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read where the device's system bars are: %w", err)
	}
	var width float64
	if win, err := d.session.Window(ctx); err == nil {
		width = win.Width
	}
	out, err := systemBars(bars, width)
	if err != nil {
		return nil, fmt.Errorf("cannot read where the device's system bars are: %w", err)
	}
	return out, nil
}

// systemBars are the bars a device reports, in its screen's pixels. An Appium server reports each
// as where it is ({"statusBar": {"visible": true, "x": 0, ...}}); the UiAutomator2 server, which
// axx drives directly, reports only the status bar's height ({"statusBar": 63}), a bar across the
// top of a screen width wide.
func systemBars(bars map[string]json.RawMessage, width float64) ([]mobilecore.Rect, error) {
	var out []mobilecore.Rect
	for name, raw := range bars {
		var height float64
		if json.Unmarshal(raw, &height) == nil {
			if height > 0 && name == "statusBar" {
				out = append(out, mobilecore.Rect{Width: width, Height: height})
			}
			continue
		}
		var b struct {
			Visible             bool
			X, Y, Width, Height float64
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("the %s is %s", name, raw)
		}
		if b.Visible {
			out = append(out, mobilecore.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height})
		}
	}
	return out, nil
}

// Stream is the UiAutomator2 server's screen stream, which Appium forwards to the host; none
// for a farm's device.
func (d *running) Stream() string {
	if d.dev == nil || d.dev.mjpegPort == 0 {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(d.dev.mjpegPort)
}

// ScreenKey is android- and the emulator's device (AVD), or the device's
// serial or the farm's device name.
func (d *running) ScreenKey() string {
	name := d.app.device
	if d.dev != nil && d.dev.avd != "" {
		name = d.dev.avd
	}
	if name == "" {
		name, _ = d.session.Capabilities["deviceName"].(string)
	}
	return "android-" + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)
}

func (d *running) Describe() map[string]any {
	out := map[string]any{"app": d.pkg, "resets": d.resets}
	if d.dev != nil {
		out["device"] = d.dev.serial
		if d.dev.avd != "" {
			out["avd"] = d.dev.avd
		}
		out["lease"] = "this scenario's own"
	} else {
		out["appium"] = d.app.server
	}
	return out
}

// Stop ends the session and gives the device back to its pool. On a device of
// the pool, the session stays: ending one stops the UiAutomator2 server with
// it, and the next scenario's session replaces it.
func (d *running) Stop(bool) error {
	var err error
	if d.session != nil && d.dev == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err = d.session.Delete(ctx)
	}
	d.releaseDevice()
	return err
}

func (d *running) releaseDevice() {
	if d.stopFeed != nil {
		d.stopFeed()
		d.stopFeed = nil
	}
	if d.pool != nil && d.dev != nil {
		d.pool.release(d.dev)
		d.dev = nil
	}
}
