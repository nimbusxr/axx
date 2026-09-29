package mobileandroid

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// runner runs an app for the scenarios that registered it.
type runner struct{ app *app }

func (r runner) Kind() string { return "android" }

// Start leases a device for the scenario and starts a session of the app on
// it, reset: the app installed and its data cleared (Appium's reset), the
// device's time zone and location set, and the app's permissions exactly
// those the registration grants. The app is not launched yet.
func (r runner) Start(sc *core.Scenario) (mobilecore.Device, error) {
	a := r.app
	ctx := sc.Context()
	logDir := filepath.Join(sc.Suite().ProjectDir(), ".axx", "mobile")
	d := &running{app: a}
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
		d.pool, d.dev, d.sdk = p, dev, p.sdk
		client = dev.appium.Client
		if err := d.resetDevice(ctx); err != nil {
			p.release(dev)
			return nil, err
		}
	}
	s, err := client.NewSession(ctx, a.capabilities(d.dev))
	if err != nil {
		d.releaseDevice()
		return nil, fmt.Errorf("cannot start the %s app: %w", a.name, err)
	}
	d.session = s
	if d.dev != nil && a.apk != "" {
		d.dev.installed[a.apk] = true
	}
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
	}
	for k, v := range a.caps {
		caps[k] = v
	}
	return caps
}

// running is an app running on a device for a scenario.
type running struct {
	app     *app
	pool    *pool
	dev     *device
	sdk     *sdk
	session *appium.Session
	pkg     string
	resets  []string
}

func (d *running) Session() *appium.Session { return d.session }

// resetDevice sets what the app sees of the device: nothing a scenario
// before left open over it (the notification shade), its time zone and its
// location.
func (d *running) resetDevice(ctx context.Context) error {
	if _, err := d.sdk.shell(ctx, d.dev.serial, "cmd", "statusbar", "collapse"); err != nil {
		return err
	}
	if _, err := d.sdk.shell(ctx, d.dev.serial, "settings", "put", "global", "auto_time_zone", "0"); err != nil {
		return err
	}
	if _, err := d.sdk.shell(ctx, d.dev.serial, "cmd", "alarm", "set-timezone", d.app.timezone); err != nil {
		return err
	}
	d.resets = append(d.resets, "time zone "+d.app.timezone)
	if loc := d.app.location; loc != nil && d.dev.avd != "" {
		lon, lat := strconv.FormatFloat(loc[1], 'f', -1, 64), strconv.FormatFloat(loc[0], 'f', -1, 64)
		if _, err := d.sdk.run(ctx, d.dev.serial, "emu", "geo", "fix", lon, lat); err != nil {
			return err
		}
		d.resets = append(d.resets, "location "+lat+", "+lon)
	}
	return nil
}

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

func (d *running) Launch(ctx context.Context) error {
	return d.session.Mobile(ctx, "activateApp", map[string]any{"appId": d.pkg}, nil)
}

func (d *running) Restart(ctx context.Context) error {
	if err := d.session.Mobile(ctx, "terminateApp", map[string]any{"appId": d.pkg}, nil); err != nil {
		return err
	}
	return d.Launch(ctx)
}

func (d *running) Background(ctx context.Context) error {
	return d.session.Mobile(ctx, "backgroundApp", map[string]any{"seconds": -1}, nil)
}

func (d *running) OpenLink(ctx context.Context, url string) error {
	return d.session.Mobile(ctx, "deepLink", map[string]any{"url": url, "package": d.pkg, "waitForLaunch": true}, nil)
}

// Swipe swipes across the middle of the screen, as a finger does.
func (d *running) Swipe(ctx context.Context, direction string) error {
	w, err := d.session.Window(ctx)
	if err != nil {
		return err
	}
	return d.session.Mobile(ctx, "swipeGesture", map[string]any{
		"left": int(w.Width * 0.1), "top": int(w.Height * 0.25), "width": int(w.Width * 0.8), "height": int(w.Height * 0.5),
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
		b := n.Bounds
		var more bool
		err := d.session.Mobile(ctx, "scrollGesture", map[string]any{
			"left": int(b.X), "top": int(b.Y), "width": int(b.Width), "height": int(b.Height),
			"direction": direction, "percent": 0.8,
		}, &more)
		return more, err
	}
	return false, nil
}

// OpenNotifications opens the notification shade, which the app's screen
// then shows, and Back closes.
func (d *running) OpenNotifications(ctx context.Context) (func(context.Context) (*mobilecore.Screen, error), func(context.Context) error, error) {
	if err := d.session.Mobile(ctx, "openNotifications", nil, nil); err != nil {
		return nil, nil, fmt.Errorf("cannot open the notification shade: %w", err)
	}
	return d.Screen, d.session.Back, nil
}

// SystemBars are the status and navigation bars, as UiAutomator2 reads them.
func (d *running) SystemBars(ctx context.Context) ([]mobilecore.Rect, error) {
	var bars map[string]struct {
		Visible             bool
		X, Y, Width, Height float64
	}
	if err := d.session.Mobile(ctx, "getSystemBars", nil, &bars); err != nil {
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

// Stop ends the session and gives the device back to its pool.
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
