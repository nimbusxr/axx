package mobileandroid

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium/appiumtest"
)

// screens are the courier app's screens, as Appium's UiAutomator2 driver
// reads them (captured from the app on an emulator).
func screens(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	files, _ := filepath.Glob("testdata/courier/*.xml")
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".xml")] = string(b)
	}
	return out
}

// at is the locator the pack finds the control named name on a screen by:
// the outermost control whose texts include it.
func at(t *testing.T, screens map[string]string, screen, name string) string {
	t.Helper()
	s, err := parseSource(screens[screen])
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range s.Visible() {
		if n.Clickable && slices.Contains(n.Texts(), name) {
			return n.Value
		}
	}
	t.Fatalf("no control %q on %s", name, screen)
	return ""
}

// named is the locator of the control whose own name is name.
func named(t *testing.T, screen, name string) string {
	t.Helper()
	s, err := parseSource(screens(t)[screen])
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range s.Visible() {
		if n.Name() == name {
			return n.Value
		}
	}
	t.Fatalf("no control %q on %s", name, screen)
	return ""
}

// courier is the fake courier app: its screens, and where taps lead.
func courier(t *testing.T) *appiumtest.Server {
	t.Helper()
	sc := screens(t)
	return appiumtest.Start(t, appiumtest.App{
		Screens: sc,
		Start:   "sign-in",
		Taps: map[string]string{
			at(t, sc, "sign-in-filled", "Sign in"):  "permission",
			"accept permission":                     "deliveries",
			"dismiss permission":                    "deliveries",
			at(t, sc, "deliveries", "PX-MOB-9401"):  "delivery",
			at(t, sc, "delivery", "Mark delivered"): "confirm",
			at(t, sc, "confirm", "Confirm"):         "delivered",
			at(t, sc, "confirm", "Cancel"):          "delivery",
		},
		Links:         map[string]string{"parcels-courier://deliveries/PX-MOB-9401": "delivery"},
		Alerts:        map[string]string{"permission": "Allow Parcels Courier to send you notifications?"},
		Notifications: "notifications",
	})
}

func register(h *cloudtest.Harness, url string, rows ...[]string) {
	h.OK("the courier android app with the following properties:", append([][]string{{"package", "example.parcels.courier"}, {"appium", url}}, rows...))
}

// A courier signs in, allows notifications and marks a parcel delivered:
// every step of the pack, on the app's own screens.
func TestACourierDelivers(t *testing.T) {
	app := courier(t)
	// The dialog's button moves in: the screen read shows it before it is there.
	app.Moving(at(t, screens(t), "confirm", "Confirm"), 2)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL, []string{"locale", "de-DE"})
	h.OK("the courier app is launched")
	h.OK(`the courier app shows "Sign in"`)
	h.OK(`the "Sign in" button is disabled in the courier app`)
	h.OK(`the "Courier ID" field in the courier app has the value ""`)
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with "4711"`)
	app.Show("sign-in-filled")
	h.OK(`the "Courier ID" field in the courier app has the value "CR-LEJ-12"`)
	h.OK(`the "Sign in" button is enabled in the courier app`)
	h.OK(`the "Sign in" button is tapped in the courier app`)
	h.OK(`the courier app's dialog shows "send you notifications"`)
	h.OK(`the courier app's dialog is accepted`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK(`the "PX-MOB-9401" list item is shown in the courier app`)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Signed by" field in the courier app is filled with "Jonas Weber"`)
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	h.OK(`the courier app shows "Mark PX-MOB-9401 delivered?"`)
	h.OK(`the "Confirm" button is tapped in the courier app`)
	h.OK(`the courier app shows "Delivered"`)
	h.OK(`the courier app shows a notification "PX-MOB-9401 delivered"`)
	h.OK(`the courier app does not show "Mark delivered"`)
	h.OK("the courier app's back button is pressed")
	h.OK(`the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9401" link`)
	h.OK(`the courier app shows "Karl-Liebknecht-Str. 12, 04107 Leipzig"`)
	h.OK("the courier app is swiped down")
	h.OK("the courier app is sent to the background")
	h.OK("the courier app is brought back")
	h.OK("the courier app is restarted")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	caps := app.Capabilities()
	for key, want := range map[string]any{
		"platformName": "Android", "appium:automationName": "UiAutomator2", "appium:appPackage": "example.parcels.courier",
		"appium:noReset": false, "appium:autoLaunch": false, "appium:autoGrantPermissions": false,
		"appium:language": "de", "appium:locale": "DE",
	} {
		if caps[key] != want {
			t.Errorf("capability %s: %v, want %v", key, caps[key], want)
		}
	}
	cmds := strings.Join(app.Commands(), "\n")
	for _, want := range []string{
		`mobile: activateApp [{"appId":"example.parcels.courier"}]`,
		"type CR-LEJ-12", "type 4711", "accept alert", "type Jonas Weber", "mobile: openNotifications", "back",
		`mobile: deepLink [{"package":"example.parcels.courier","url":"parcels-courier://deliveries/PX-MOB-9401","waitForLaunch":true}]`,
		`"direction":"down"`, `mobile: backgroundApp [{"seconds":-1}]`, `mobile: terminateApp`, "end session",
	} {
		if !strings.Contains(cmds, want) {
			t.Errorf("no %s in:\n%s", want, cmds)
		}
	}
}

// A dialog still coming in has no text yet, and an answer then is lost: the
// dialog is answered once its text shows.
func TestADialogComingIn(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	app.Show("permission")
	app.Arriving(1)
	h.OK("the courier app's dialog is dismissed")
	if app.Screen() != "deliveries" {
		t.Errorf("the dialog is still shown: the app is on %s", app.Screen())
	}
	_ = h.End("passed")
}

// What a failed step says: the controls there are, a value, a dialog there
// is not. Each attaches what the app showed.
func TestFailures(t *testing.T) {
	app := courier(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	_ = h.Fails(`the "Sign in" button is tapped in the courier app`, "the courier app is not running: launch it")
	h.OK("the courier app is launched")
	withShortWaits(t)
	_ = h.Fails(`the "Log in" button is tapped in the courier app`, `No button named "Log in" in the courier app; 1 buttons:`)
	_ = h.Fails(`the "Courier" field in the courier app is filled with "x"`, "Courier ID")
	_ = h.Fails(`within 1s the courier app shows "Today's deliveries"`, `The courier app does not show "Today's deliveries"`)
	_ = h.Fails(`within 1s the courier app does not show "Sign in"`, `The courier app shows "Sign in"`)
	_ = h.Fails(`within 1s the "Sign in" button is enabled in the courier app`, `The "Sign in" button in the courier app is not enabled`)
	_ = h.Fails(`the courier app's dialog is accepted`, "The courier app shows no dialog")
	_ = h.Fails(`within 1s the courier app shows a notification "PX-MOB-9402 delivered"`, `The device of the courier app shows no notification "PX-MOB-9402 delivered"`)
	if app.Screen() != "sign-in" {
		t.Errorf("the notifications stayed open over the app: %s", app.Screen())
	}
	app.Show("sign-in-filled")
	err := h.Fails(`within 1s the "Courier ID" field in the courier app has the value "${env:COURIER_PIN}"`, "does not have the value")
	if strings.Contains(err.Error(), "4711") {
		t.Errorf("the secret is in the failure: %v", err)
	}
	app.Show("permission")
	h.OK(`the "Don't allow" button is shown in the courier app`) // a typographic apostrophe on the screen
	var shots int
	for _, a := range h.Sink.Attachments {
		if a.MediaType == "image/png" && a.Name == "the courier app" {
			shots++
		}
	}
	if shots == 0 {
		t.Error("a failed step attaches what the app showed")
	}
	_ = h.End("failed")
}

// A control no name tells apart is found by its id or by an XPath.
func TestSelectors(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	app.Show("permission")
	h.OK(`the "id=com.android.permissioncontroller:id/permission_deny_button" button is tapped in the courier app`)
	h.OK(`the "xpath=` + strings.ReplaceAll(named(t, "permission", "Allow"), `"`, `'`) + `" element is shown in the courier app`)
	withShortWaits(t)
	_ = h.Fails(`the "id=nothing" button is tapped in the courier app`, `No button in the courier app matches "id=nothing"`)
	_ = h.End("passed")
}

func withShortWaits(t *testing.T) {
	t.Helper()
	t.Cleanup(mobilecore.SetActionTimeout(300 * time.Millisecond))
}

func TestRegistrationErrors(t *testing.T) {
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	for _, c := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"device", "Pixel_9"}}, "needs its apk, or the package of an app the device has"},
		{[][]string{{"package", "p"}}, "needs a device"},
		{[][]string{{"package", "p"}, {"device", "d"}, {"locale", "German"}}, "is not a language and region"},
		{[][]string{{"package", "p"}, {"device", "d"}, {"timezone", "Berlin/Mitte"}}, "is not a time zone"},
		{[][]string{{"package", "p"}, {"device", "d"}, {"location", "Leipzig"}}, "is not a latitude and a longitude"},
		{[][]string{{"package", "p"}, {"device", "d"}, {"permissions", "notify me"}}, "is not a permission"},
		{[][]string{{"package", "p"}, {"appium", "farm.example"}}, "is not an http(s) URL"},
		{[][]string{{"package", "p"}, {"device", "d"}, {"capability.bstack:options", "{}"}}, "are for its appium server"},
		{[][]string{{"package", "p"}, {"device", "d"}, {"screen", "big"}}, `unknown android app property "screen"`},
		{[][]string{{"package", "p"}, {"device", "d"}, {"host ports", "5500, web"}}, `host port "web" is not a port`},
		{[][]string{{"package", "p"}, {"appium", "http://farm.example"}, {"host ports", "5500"}}, "host ports are for a device axx runs"},
	} {
		_ = h.Fails("the courier android app with the following properties:", c.want, c.rows)
	}
}

func TestHostPorts(t *testing.T) {
	sc := core.NewScenario(t.Context(), core.ScenarioInfo{}, nil, nil)
	a, err := parseApp(sc, func(s string) string { return s }, "wallet", &core.Table{Rows: [][]string{{"package", "p"}, {"device", "d"}, {"host ports", "5500, 8090,8089"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.hostPorts, []int{5500, 8090, 8089}) {
		t.Errorf("host ports: %v", a.hostPorts)
	}
}

// Chrome's command line reaches the device whole: adb joins a command's
// arguments with spaces, so the shell script is one argument.
func TestSkipBrowserWelcome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	adb := filepath.Join(dir, "adb")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s|' \"$a\"; done >> " + log + "\necho >> " + log + "\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	skipBrowserWelcome(t.Context(), &sdk{adb: adb}, "emulator-5554")
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "-s|emulator-5554|shell|echo '_ --disable-fre --no-default-browser-check --no-first-run' > /data/local/tmp/chrome-command-line|"
	if !slices.Contains(strings.Split(string(b), "\n"), want) {
		t.Errorf("adb calls:\n%s\nwant the line %s", b, want)
	}
}

// A device farm's options are JSON; other capabilities text or numbers.
func TestCapabilityValues(t *testing.T) {
	a := &app{locale: "en-US", caps: map[string]any{"bstack:options": capabilityValue(`{"deviceName": "Pixel 8"}`), "appium:newCommandTimeout": capabilityValue("120")}}
	caps := a.capabilities(nil)
	b, _ := json.Marshal(caps["bstack:options"])
	if string(b) != `{"deviceName":"Pixel 8"}` || caps["appium:newCommandTimeout"] != float64(120) {
		t.Errorf("caps: %v", caps)
	}
	if _, ok := caps["appium:locale"]; !ok {
		t.Error("the locale's region is a capability")
	}
}

// Emulators that start at once each get a console port of their own, and a
// port an emulator no longer holds is free again.
func TestConsolePorts(t *testing.T) {
	var mu sync.Mutex
	seen := map[int]bool{}
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			port, err := consolePort()
			mu.Lock()
			defer mu.Unlock()
			if err != nil || seen[port] || port%2 != 0 {
				t.Errorf("port %d: %v (taken: %v)", port, err, seen)
			}
			seen[port] = true
		}()
	}
	wg.Wait()
	for port := range seen {
		releasePort(port)
	}
	port, err := consolePort()
	if err != nil || !seen[port] {
		t.Errorf("a released port is free again: %d %v", port, err)
	}
	releasePort(port)
}

func TestConfig(t *testing.T) {
	s, err := parseConfig(Config{Devices: 2, BootTimeout: "90s", EmulatorArgs: []string{"-gpu", "swiftshader_indirect"}})
	if err != nil || s.devices != 2 || s.bootTimeout.Seconds() != 90 || len(s.emulatorArgs) != 2 {
		t.Errorf("settings %+v, %v", s, err)
	}
	if _, err := parseConfig(Config{BootTimeout: "soon"}); err == nil || !strings.Contains(err.Error(), "not a duration") {
		t.Errorf("err %v", err)
	}
	if s, _ := parseConfig(Config{}); s.devices != 1 {
		t.Errorf("one device by default: %d", s.devices)
	}
}

// Compose marks a control's role by a child of its class; a tappable row is
// a list item; Android's own views say it by their class.
func TestRoles(t *testing.T) {
	sc := screens(t)
	for screen, want := range map[string]map[string]mobilecore.Role{
		"sign-in":    {"Sign in": mobilecore.RoleButton, "Courier ID": mobilecore.RoleField, "PIN": mobilecore.RoleField},
		"deliveries": {"Sign out": mobilecore.RoleButton, "PX-MOB-9401": mobilecore.RoleListItem},
		"permission": {"Allow": mobilecore.RoleButton},
	} {
		s, err := parseSource(sc[screen])
		if err != nil {
			t.Fatal(err)
		}
		for name, role := range want {
			found := false
			for _, n := range s.Visible() {
				if n.Role == role && (n.Name() == name || slices.Contains(n.Texts(), name)) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: no %q of role %d", screen, name, role)
			}
		}
	}
	_ = core.Failf
}

// A screenshot is taken the first time, and compared after: the status bar,
// whose clock changes, is left out.
func TestScreenshots(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL, []string{"device", "Pixel_9"})
	h.OK("the courier app is launched")
	_ = h.Fails(`the courier app looks like the "sign in" screenshot`, `There was no "sign in.android-Pixel_9" screenshot to compare with, so it was taken`)
	if _, err := os.Stat(filepath.Join(h.Dir, "screenshots", "sign in.android-Pixel_9.png")); err != nil {
		t.Fatal(err)
	}
	h.OK(`the courier app looks like the "sign in" screenshot`)
	app.Show("deliveries")
	_ = h.Fails(`within 1s the courier app looks like the "sign in" screenshot`, `The courier app does not look like its "sign in" screenshot`)
	var names []string
	for _, a := range h.Sink.Attachments {
		names = append(names, a.Name)
	}
	for _, want := range []string{"screenshot taken", "expected screenshot", "difference"} {
		if !slices.Contains(names, want) {
			t.Errorf("no %q attachment in %v", want, names)
		}
	}
	_ = h.Fails(`the courier app looks like the "../elsewhere" screenshot`, "a screenshot's name is letters")
	_ = h.End("failed")

	u := cloudtest.NewWith(t, map[string]any{mobilecore.Name: map[string]any{"screenshots": map[string]any{"update": true}}}, appcore.Pack(), mobilecore.Pack(), Pack())
	register(u, app.URL, []string{"device", "Pixel_9"})
	u.OK("the courier app is launched")
	u.OK(`the courier app looks like the "deliveries" screenshot`)
	_ = u.End("passed")
}

// The activity in front, as dumpsys activity says it on Android 10 and later, and before.
func TestResumedPackage(t *testing.T) {
	for out, want := range map[string]string{
		"  topResumedActivity=ActivityRecord{8a3c1f2 u0 example.parcels.courier/.MainActivity t42}":                     "example.parcels.courier",
		"    mResumedActivity: ActivityRecord{5d2e u0 com.google.android.apps.nexuslauncher/.NexusLauncherActivity t1}": "com.google.android.apps.nexuslauncher",
		"  ResumedActivity: ActivityRecord{1 u0 com.android.settings/.Settings t9}":                                     "com.android.settings",
		"nothing in front": "",
	} {
		if got := resumedPackage(out); got != want {
			t.Errorf("%q: %q, want %q", out, got, want)
		}
	}
}
