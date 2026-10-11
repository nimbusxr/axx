package mobileandroid

import (
	"context"
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
	if err := p.ensureServer(ctx, dev, logDir); err != nil {
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
		if _, err := d.sdk.run(ctx, serial, "install", "-r", "-t", a.apk); err != nil {
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
}

func (d *running) Session() *appium.Session { return d.session }

// revive starts the UiAutomator2 server again after it crashed under the scenario (its own
// process: the app is as it was), and a session on it, which the scenario's becomes. Its log
// keeps the device's crash log, which says why.
func (d *running) revive(ctx context.Context) error {
	// A step and the recorder may both find the server dead: the first starts it again, and the
	// second finds the session answering.
	d.reviving.Lock()
	defer d.reviving.Unlock()
	if d.session.Alive(ctx) {
		return nil
	}
	if err := d.pool.ensureServer(ctx, d.dev, d.logDir); err != nil {
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
	}
	// What the app reaches as localhost: the registration's host ports, and
	// none another scenario's registration gave.
	if _, err := d.sdk.run(ctx, d.dev.serial, "reverse", "--remove-all"); err != nil {
		return err
	}
	for _, port := range d.app.hostPorts {
		tcp := "tcp:" + strconv.Itoa(port)
		if _, err := d.sdk.run(ctx, d.dev.serial, "reverse", tcp, tcp); err != nil {
			return fmt.Errorf("the %s app cannot reach port %d of this machine: %w", d.app.name, port, err)
		}
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
	var err error
	if d.dev != nil {
		err = d.session.Command(ctx, http.MethodPost, "/appium/device/open_notifications", nil, nil)
	} else {
		err = d.session.Mobile(ctx, "openNotifications", nil, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot open the notification shade: %w", err)
	}
	return mobilecore.ScreenNotifications{Read: d.Screen, Hide: d.session.Back}, nil
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
	var bars map[string]struct {
		Visible             bool
		X, Y, Width, Height float64
	}
	var err error
	if d.dev != nil {
		err = d.session.Command(ctx, http.MethodGet, "/appium/device/system_bars", nil, &bars)
	} else {
		err = d.session.Mobile(ctx, "getSystemBars", nil, &bars)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read where the device's system bars are: %w", err)
	}
	var out []mobilecore.Rect
	for _, b := range bars {
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
	if d.pool != nil && d.dev != nil {
		d.pool.release(d.dev)
		d.dev = nil
	}
}
