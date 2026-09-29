package mobileios

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// Start leases a simulator for the scenario and starts a session of the app
// on it, reset: the app installed afresh, its keychain and permissions
// reset, the permissions the registration grants granted, and the
// simulator's location set. The app is not launched yet.
func (r runner) Start(sc *core.Scenario) (mobilecore.Device, error) {
	a := r.app
	ctx := sc.Context()
	logDir := filepath.Join(sc.Suite().ProjectDir(), ".axx", "mobile")
	d := &running{app: a, bundleID: a.bundleID}
	client := &appium.Client{URL: a.server}
	if a.server == "" {
		p, err := poolFor(sc, a.device)
		if err != nil {
			return nil, err
		}
		dev, err := p.lease(ctx, logDir)
		if err != nil {
			return nil, err
		}
		d.pool, d.dev, d.key = p, dev, p.key
		client = dev.appium.Client
		if err := d.reset(ctx, sc.Suite()); err != nil {
			p.release(dev)
			return nil, err
		}
	}
	s, err := client.NewSession(ctx, a.capabilities(d.dev, d.bundleID))
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
	return d, nil
}

// capabilities are what the session asks Appium for. On a simulator axx
// runs, axx reset the app before the session starts, so Appium leaves it as
// it is; another Appium server installs the app afresh itself.
func (a *app) capabilities(dev *device, bundleID string) map[string]any {
	caps := map[string]any{
		"platformName":             "iOS",
		"appium:automationName":    "XCUITest",
		"appium:noReset":           dev != nil,
		"appium:autoLaunch":        false,
		"appium:newCommandTimeout": 0, // the scenario holds the session
	}
	if dev == nil && a.path != "" {
		caps["appium:app"] = a.path
	}
	if bundleID != "" {
		caps["appium:bundleId"] = bundleID
	}
	if dev != nil {
		caps["appium:udid"] = dev.udid
		caps["appium:platformVersion"] = dev.version
		if dev.set != "" {
			caps["appium:simulatorDevicesSetPath"] = string(dev.set)
		}
		caps["appium:usePreinstalledWDA"] = true
		caps["appium:wdaLocalPort"] = dev.wdaPort
		caps["appium:mjpegServerPort"] = dev.mjpegPort
		caps["appium:reduceMotion"] = true // a screen settles at once, and screenshots compare
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
}

func (d *running) Session() *appium.Session { return d.session }

// reset leaves the simulator as the scenario starts from: the app installed
// afresh (with it go its data and its notifications), its keychain and
// permissions reset, the permissions the registration grants granted, and
// the simulator's location set, or none.
func (d *running) reset(ctx context.Context, s *core.Suite) error {
	udid, set := d.dev.udid, d.dev.set
	path := ""
	if d.app.path != "" {
		var err error
		path, err = appBundle(s, d.app.path)
		if err != nil {
			return fmt.Errorf("the %s app: %w", d.app.name, err)
		}
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
		return nil, fmt.Errorf("cannot read the %s app's screen: %w", d.app.name, err)
	}
	return parseSource(src)
}

// runningSuspended is the first state XCUITest reports of a running app:
// 1 is not running, 2 suspended, 3 in the background, 4 in the foreground.
const runningSuspended = 2

func (d *running) state(ctx context.Context) (int, error) {
	var state int
	err := d.session.Mobile(ctx, "queryAppState", map[string]any{"bundleId": d.bundleID}, &state)
	return state, err
}

// Launch brings the app to the front; one that is not running starts in the
// registration's language, region and time zone.
func (d *running) Launch(ctx context.Context) error {
	state, err := d.state(ctx)
	if err != nil {
		return err
	}
	if state >= runningSuspended {
		return d.session.Mobile(ctx, "activateApp", map[string]any{"bundleId": d.bundleID}, nil)
	}
	return d.start(ctx)
}

func (d *running) start(ctx context.Context) error {
	return d.session.Mobile(ctx, "launchApp", map[string]any{
		"bundleId":    d.bundleID,
		"arguments":   d.app.launchArguments(),
		"environment": map[string]string{"TZ": d.app.timezone},
	}, nil)
}

func (d *running) Restart(ctx context.Context) error {
	if err := d.session.Mobile(ctx, "terminateApp", map[string]any{"bundleId": d.bundleID}, nil); err != nil {
		return err
	}
	return d.start(ctx)
}

func (d *running) Background(ctx context.Context) error {
	return d.session.Mobile(ctx, "backgroundApp", map[string]any{"seconds": -1}, nil)
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
	return d.session.Mobile(ctx, "deepLink", map[string]any{"url": url, "bundleId": d.bundleID}, nil)
}

// Swipe swipes across the screen, as a finger does.
func (d *running) Swipe(ctx context.Context, direction string) error {
	return d.session.Mobile(ctx, "swipe", map[string]any{"direction": direction}, nil)
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
	for _, n := range s.Visible() {
		if !n.Scrolling {
			continue
		}
		el, err := d.session.Find(ctx, n.Using, n.Value)
		if err != nil {
			return false, err
		}
		if err := d.session.Mobile(ctx, "scroll", map[string]any{"elementId": el.ID, "direction": direction}, nil); err != nil {
			return false, err
		}
		after, err := d.session.Source(ctx)
		return after != before, err
	}
	return false, nil
}

// OpenNotifications opens Notification Center with a finger from the top
// edge, and reads it from SpringBoard, which shows it; activating the app
// closes it again.
func (d *running) OpenNotifications(ctx context.Context) (func(context.Context) (*mobilecore.Screen, error), func(context.Context) error, error) {
	w, err := d.session.Window(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := d.session.Drag(ctx, w.Width*0.2, 0, w.Width*0.2, w.Height*0.7, 400*time.Millisecond); err != nil {
		return nil, nil, fmt.Errorf("cannot open Notification Center: %w", err)
	}
	if err := d.session.Settings(ctx, map[string]any{"defaultActiveApplication": springboard}); err != nil {
		return nil, nil, err
	}
	closeThem := func(ctx context.Context) error {
		if err := d.session.Settings(ctx, map[string]any{"defaultActiveApplication": d.bundleID}); err != nil {
			return err
		}
		return d.session.Mobile(ctx, "activateApp", map[string]any{"bundleId": d.bundleID}, nil)
	}
	return d.Screen, closeThem, nil
}

// springboard is iOS's home screen app, which shows notifications.
const springboard = "com.apple.springboard"

// SystemBars is the status bar, with its clock, in the screenshot's pixels.
func (d *running) SystemBars(ctx context.Context) ([]mobilecore.Rect, error) {
	var info struct {
		StatusBarSize struct{ Width, Height float64 } `json:"statusBarSize"`
		Scale         float64                         `json:"scale"`
	}
	if err := d.session.Mobile(ctx, "deviceScreenInfo", nil, &info); err != nil {
		return nil, fmt.Errorf("cannot read where the simulator's status bar is: %w", err)
	}
	if info.Scale == 0 {
		info.Scale = 1
	}
	return []mobilecore.Rect{{Width: info.StatusBarSize.Width * info.Scale, Height: info.StatusBarSize.Height * info.Scale}}, nil
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
