package mobileandroid

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

// A screen UiAutomator2 fails to read as it changes ("cannot set AccessibilityNodeInfo's field
// 'mSealed'") is read again a moment later.
func TestAScreenReadAsItChangesIsReadAgain(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	app.Unsealed(2)
	err := mobilecore.OnDevice(h.SC, "courier", func(ctx context.Context, d mobilecore.Device) error {
		_, err := d.Screen(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("a screen read twice as it changed: %v", err)
	}
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
	// The dialog's button drawn again between finding it and tapping it, as on a slow machine: the
	// server no longer has it, and the step finds it again.
	app.Expiring(1)
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

// A dialog drawn again as it is read (UiAutomator2's stale element, on a busy runner) is read
// again, and answered.
func TestADialogDrawnAgain(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	app.Show("permission")
	app.StaleAlerts(2)
	h.OK("the courier app's dialog is accepted")
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
	// What the screen shows, which says where the text is.
	if err := h.Fails(`within 1s the courier app does not show "Sign in"`, `The courier app shows "Sign in"`); !strings.Contains(err.Error(), "Courier ID") {
		t.Errorf("the failure does not say what the screen shows: %v", err)
	}
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
	document, err := filepath.Abs("android_test.go")
	if err != nil {
		t.Fatal(err)
	}
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
		{[][]string{{"package", "p"}, {"device", "d"}, {"photos", "fixtures/no-such-photo.jpg"}}, `the courier android app's photo "fixtures/no-such-photo.jpg"`},
		{[][]string{{"package", "p"}, {"device", "d"}, {"documents", "fixtures/no-such-document.pdf"}}, `the courier android app's documents: "fixtures/no-such-document.pdf"`},
		{[][]string{{"package", "p"}, {"appium", "http://farm.example"}, {"documents", document}}, "documents are put on devices axx runs"},
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

// An emulator reports where it is every second while its scenario runs, as a phone's GPS does,
// and stops when the scenario gives it back: the next scenario gets no fix of this one's.
func TestTheEmulatorKeepsReportingWhereItIs(t *testing.T) {
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
	fixes := func() int {
		b, _ := os.ReadFile(log)
		return strings.Count(string(b), "-s|emulator-5554|emu|geo|fix|12.3731|51.3397|\n")
	}
	d := &running{sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554"}}
	d.feedLocation("12.3731", "51.3397")
	time.Sleep(2500 * time.Millisecond)
	d.releaseDevice()
	sent := fixes()
	if sent < 2 {
		t.Errorf("%d fixes in 2.5 seconds, want one a second", sent)
	}
	time.Sleep(1500 * time.Millisecond)
	if after := fixes(); after != sent {
		t.Errorf("%d fixes after the scenario gave the emulator back", after-sent)
	}
}

// A scenario's photos go in the library's DCIM/axx folder, indexed before the app starts; and
// every reset first takes the photos an earlier scenario put there out of the media store and
// off the device.
func TestPhotosGoInTheLibraryAndOutAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	adb := filepath.Join(dir, "adb")
	// The media store has both photos once they are pushed.
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s|' \"$a\"; done >> " + log + "\necho >> " + log +
		"\ncase \"$*\" in *'content query'*) printf 'Row: 0 _id=1\\nRow: 1 _id=2\\n';; esac\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	begin := filepath.Join(dir, "begin.jpg")
	end := filepath.Join(dir, "end.jpg")
	for _, f := range []string{begin, end} {
		if err := os.WriteFile(f, []byte("jpeg"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	d := &running{app: &app{name: "dtcd", photos: []string{begin, end}}, sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554"}}
	if err := d.putPhotos(t.Context()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := []string{
		`-s|emulator-5554|shell|content delete --uri content://media/external/file --where "_data LIKE '%/DCIM/axx/%'"|`,
		"-s|emulator-5554|shell|rm|-rf|/sdcard/DCIM/axx|",
		"-s|emulator-5554|shell|mkdir|-p|/sdcard/DCIM/axx|",
		"-s|emulator-5554|push|" + begin + "|/sdcard/DCIM/axx/begin.jpg|",
		"-s|emulator-5554|shell|am|broadcast|-a|android.intent.action.MEDIA_SCANNER_SCAN_FILE|-d|file:///sdcard/DCIM/axx/begin.jpg|",
		"-s|emulator-5554|push|" + end + "|/sdcard/DCIM/axx/end.jpg|",
		"-s|emulator-5554|shell|am|broadcast|-a|android.intent.action.MEDIA_SCANNER_SCAN_FILE|-d|file:///sdcard/DCIM/axx/end.jpg|",
	}
	if len(calls) < len(want) || !slices.Equal(calls[:len(want)], want) {
		t.Errorf("adb calls:\n%s\nwant them to start:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	if !slices.Contains(d.resets, "photos: begin.jpg, end.jpg") {
		t.Errorf("resets %v", d.resets)
	}
}

// A scenario's documents go in the device's Downloads, indexed before the app starts. On an
// emulator axx started, every reset first empties Downloads (out of the media store, then off the
// device) and starts the system's file picker afresh; on a device of the project's, it takes out
// only the documents axx put there, which it lists on the device.
func TestDocumentsGoInDownloadsAndOutAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	adb := filepath.Join(dir, "adb")
	// Downloads has a document of a scenario before, the device has Google's file picker, and the
	// media store has the new document once it is pushed.
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s|' \"$a\"; done >> " + log + "\necho >> " + log +
		"\ncase \"$*\" in *'ls -1A'*|*'cat '*) printf 'pod-PX-MOB-9402.jpg\\n';; *'pm list packages'*) printf 'package:com.google.android.documentsui\\n';; *'content query'*) printf 'Row: 0 _id=1\\n';; esac\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(dir, "safe-place PX-MOB-9401.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := func() []string {
		b, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(log)
		return strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	takeOut := `content delete --uri content://media/external/file --where '_data LIKE '\''%/Download/pod-PX-MOB-9402.jpg'\'' OR _data LIKE '\''%/Download/pod-PX-MOB-9402.jpg/%'\'''`

	d := &running{app: &app{name: "courier", documents: []string{pdf}}, sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554", avd: "parcels-pixel"}}
	if err := d.putDocuments(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-s|emulator-5554|shell|ls -1A '/sdcard/Download' 2>/dev/null || true|",
		"-s|emulator-5554|shell|pm list packages documentsui|",
		"-s|emulator-5554|shell|pm clear 'com.google.android.documentsui'|",
		"-s|emulator-5554|shell|" + takeOut + "|",
		"-s|emulator-5554|shell|rm -rf '/sdcard/Download/pod-PX-MOB-9402.jpg'|",
		"-s|emulator-5554|shell|mkdir -p '/sdcard/Download'|",
		"-s|emulator-5554|push|" + pdf + "|/sdcard/Download/safe-place PX-MOB-9401.pdf|",
		"-s|emulator-5554|shell|am broadcast -a android.intent.action.MEDIA_SCANNER_SCAN_FILE -d 'file:///sdcard/Download/safe-place PX-MOB-9401.pdf'|",
	}
	if got := calls(); len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
		t.Errorf("adb calls on an emulator axx started:\n%s\nwant them to start:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !slices.Contains(d.resets, "documents in Downloads: safe-place PX-MOB-9401.pdf") {
		t.Errorf("resets %v", d.resets)
	}

	// A device of the project's: only what axx listed goes, and its file picker is left as it is.
	d = &running{app: &app{name: "courier"}, sdk: &sdk{adb: adb}, dev: &device{serial: "R5CT1234"}}
	if err := d.putDocuments(t.Context()); err != nil {
		t.Fatal(err)
	}
	want = []string{
		"-s|R5CT1234|shell|cat '/data/local/tmp/axx-documents' 2>/dev/null || true|",
		"-s|R5CT1234|shell|" + takeOut + "|",
		"-s|R5CT1234|shell|rm -rf '/sdcard/Download/pod-PX-MOB-9402.jpg'|",
		"-s|R5CT1234|shell|rm -f '/data/local/tmp/axx-documents'|",
	}
	if got := calls(); !slices.Equal(got, want) {
		t.Errorf("adb calls on a device of the project's:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// adb can lose a device for a moment (as a crashing UiAutomator2 server takes it down): axx waits
// for it to be back before starting the server again, and gives up after a while.
func TestWaitingForADeviceAdbLostForAMoment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	adb := filepath.Join(dir, "adb")
	// Offline for the first two asks, then back.
	script := "#!/bin/sh\nn=$(cat " + count + " 2>/dev/null || echo 0); echo $((n+1)) > " + count +
		"\nif [ $n -lt 2 ]; then echo 'error: device offline' >&2; exit 1; fi\necho device\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &sdk{adb: adb}
	if err := s.waitOnline(t.Context(), "emulator-5554", 10*time.Second); err != nil {
		t.Fatalf("a device back after two asks: %v", err)
	}
	if b, _ := os.ReadFile(count); strings.TrimSpace(string(b)) != "3" {
		t.Errorf("asked %s times, want 3", strings.TrimSpace(string(b)))
	}
	if err := os.WriteFile(count, []byte("-100"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.waitOnline(t.Context(), "emulator-5554", time.Second); err == nil {
		t.Error("a device still offline after the wait: no error")
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

// An adb takes --no-incremental from 30.0.1, by what adb version prints.
func TestKnowsNoIncremental(t *testing.T) {
	for out, want := range map[string]bool{
		"Android Debug Bridge version 1.0.41\nVersion 37.0.1-15733141\nInstalled as /sdk/platform-tools/adb": true,
		"Android Debug Bridge version 1.0.41\nVersion 30.0.1-6435776\n":                                      true,
		"Android Debug Bridge version 1.0.41\nVersion 30.0.0-6374843\n":                                      false,
		"Android Debug Bridge version 1.0.40\nVersion 4986621\n":                                             false,
	} {
		if got := knowsNoIncremental(out); got != want {
			t.Errorf("%q: %v, want %v", out, got, want)
		}
	}
}

// Jetpack Compose says a switch only in its role description, which the UiAutomator2 server
// leaves out: the courier app's "Share van location" row (Modifier.toggleable, Role.Switch), as
// the server reads it, is a switch.
func TestAComposeSwitchRowIsASwitch(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "compose", "switch-row.xml"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseSource(string(b))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range s.Visible() {
		if slices.Contains(n.Texts(), "Share van location") && n.Clickable {
			if n.Role != mobilecore.RoleSwitch {
				t.Errorf("the row's role is %d, want a switch", n.Role)
			}
			return
		}
	}
	t.Error("no tappable row shows Share van location")
}

// Android folds an app's notifications into a group, whose rows are not tappable until it is
// opened out (as two delivered parcels' notifications are, read from an emulator): opening one
// taps the group's expand button first, then the notification.
func TestANotificationFoldedInAGroupIsOpenedOutFirst(t *testing.T) {
	var reads []*mobilecore.Screen
	for _, f := range []string{"grouped.xml", "opened.xml"} {
		b, err := os.ReadFile(filepath.Join("testdata", "shade", f))
		if err != nil {
			t.Fatal(err)
		}
		s, err := parseSource(string(b))
		if err != nil {
			t.Fatal(err)
		}
		reads = append(reads, s)
	}
	var taps [][2]float64
	n := mobilecore.ScreenNotifications{
		Read: func(context.Context) (*mobilecore.Screen, error) {
			s := reads[min(len(taps), len(reads)-1)]
			return s, nil
		},
		Tap: func(_ context.Context, x, y float64) error { taps = append(taps, [2]float64{x, y}); return nil },
	}
	if err := n.Open(context.Background(), "PX-MOB-9402 delivered"); err != nil {
		t.Fatal(err)
	}
	if len(taps) != 2 || taps[0] != [2]float64{945, 647.5} {
		t.Fatalf("taps %v: want the group's expand button (945, 647.5), then the notification", taps)
	}
	var row *mobilecore.Node
	for _, nd := range reads[1].Nodes {
		if nd.Text == "PX-MOB-9402 delivered" {
			for p := nd; p != nil; p = p.Parent {
				if strings.HasSuffix(p.ID, ":id/expandableNotificationRow") {
					row = p
					break
				}
			}
		}
	}
	if x, y := row.Bounds.Center(); row == nil || !row.Clickable || taps[1] != [2]float64{x, y} {
		t.Errorf("the notification was tapped at %v, want its row in the opened group", taps[1])
	}
}

// adb runs with mDNS discovery off, so a server it starts has it off (on some networks adb's
// mDNS bridge aborts the server); an ADB_MDNS the user sets wins.
func TestAdbRunsWithMDNSOff(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	adb := filepath.Join(t.TempDir(), "adb")
	if err := os.WriteFile(adb, []byte("#!/bin/sh\necho \"mdns=${ADB_MDNS-unset}\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &sdk{adb: adb}
	if out, err := s.run(t.Context(), "", "devices"); err != nil || out != "mdns=0" {
		t.Errorf("by default: %q %v", out, err)
	}
	t.Setenv("ADB_MDNS", "1")
	if out, err := s.run(t.Context(), "", "devices"); err != nil || out != "mdns=1" {
		t.Errorf("the user's ADB_MDNS: %q %v", out, err)
	}
}

// A command that failed because adb's server had stopped says so, and how to keep it up.
func TestAnAdbServerThatStoppedIsExplained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	adb := filepath.Join(t.TempDir(), "adb")
	script := "#!/bin/sh\necho '* daemon not running; starting now at tcp:5037'\necho 'adb: device offline'\nexit 1\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := (&sdk{adb: adb}).run(t.Context(), "emulator-5554", "install", "x.apk")
	if err == nil || !strings.Contains(err.Error(), "adb's server had stopped and was started again") || !strings.Contains(err.Error(), "ADB_MDNS=0") {
		t.Errorf("err = %v", err)
	}
	if serverRestarted("Success") != "" {
		t.Error("an output without the server starting again adds nothing")
	}
}

// The bars a device reports: an Appium server says where each is; the UiAutomator2 server, which
// axx drives directly, says only the status bar's height, a bar across the top.
func TestSystemBars(t *testing.T) {
	for _, c := range []struct {
		name string
		bars string
		want []mobilecore.Rect
	}{
		{"the UiAutomator2 server", `{"statusBar": 63}`, []mobilecore.Rect{{Width: 1080, Height: 63}}},
		{"an Appium server", `{"statusBar": {"visible": true, "x": 0, "y": 0, "width": 1080, "height": 63}, "navigationBar": {"visible": false, "x": 0, "y": 2337, "width": 1080, "height": 63}}`, []mobilecore.Rect{{Width: 1080, Height: 63}}},
	} {
		var bars map[string]json.RawMessage
		if err := json.Unmarshal([]byte(c.bars), &bars); err != nil {
			t.Fatal(err)
		}
		got, err := systemBars(bars, 1080)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

// adb prints the host port it picked for a forward to tcp:0; anything else is not a port.
func TestForwardedPort(t *testing.T) {
	if port, err := forwardedPort("43119\n"); err != nil || port != 43119 {
		t.Errorf("forwardedPort(43119) = %d, %v", port, err)
	}
	for _, out := range []string{"", "error: cannot bind listener", "0", "70000"} {
		if _, err := forwardedPort(out); err == nil {
			t.Errorf("forwardedPort(%q) took it for a port", out)
		}
	}
}

// The notification shade is open when its window has the focus, as dumpsys window says (API 35).
func TestTheShadeIsOpenWhenItHasTheFocus(t *testing.T) {
	open := "  mCurrentFocus=Window{caffb71 u0 NotificationShade}\n  mFocusedApp=ActivityRecord{ef499bf u0 com.google.android.apps.nexuslauncher/.NexusLauncherActivity t7}\n"
	shut := "  mCurrentFocus=Window{1013a10 u0 com.google.android.apps.nexuslauncher/com.google.android.apps.nexuslauncher.NexusLauncherActivity}\n"
	if !shadeHasFocus(open) {
		t.Error("the shade's window has the focus, and the shade is not taken for open")
	}
	if shadeHasFocus(shut) || shadeHasFocus("") {
		t.Error("the launcher has the focus, and the shade is taken for open")
	}
}

// A shade that does not open, as the launcher comes in after the app went to the background, is
// opened again: the screen read is then the shade's, not the home screen's.
func TestAShadeThatDoesNotOpenIsOpenedAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	count := filepath.Join(dir, "expands")
	adb := filepath.Join(dir, "adb")
	// The first three expands are lost: the shade has the focus after the fourth.
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"*'statusbar expand-notifications'*) n=$(cat " + count + " 2>/dev/null || echo 0); echo $((n+1)) > " + count + " ;;\n" +
		"*'dumpsys window displays'*) if [ \"$(cat " + count + " 2>/dev/null || echo 0)\" -ge 4 ]; then echo '  mCurrentFocus=Window{caffb71 u0 NotificationShade}'; " +
		"else echo '  mCurrentFocus=Window{1013a10 u0 com.google.android.apps.nexuslauncher/com.google.android.apps.nexuslauncher.NexusLauncherActivity}'; fi ;;\n" +
		"esac\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d := &running{sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554"}}
	open := func() error {
		_, err := d.sdk.shell(t.Context(), d.dev.serial, "cmd", "statusbar", "expand-notifications")
		return err
	}
	if err := open(); err != nil {
		t.Fatal(err)
	}
	if err := d.shadeOpened(t.Context(), open); err != nil {
		t.Fatalf("a shade open on the fourth try: %v", err)
	}
	if b, _ := os.ReadFile(count); strings.TrimSpace(string(b)) != "4" {
		t.Errorf("opened %s times, want 4", strings.TrimSpace(string(b)))
	}
}

// A device is set up to settle at once and to take a slow tap for a tap: animations off, and a
// long press only once a touch is held for a second and a half.
func TestADeviceSettles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "adb.log")
	adb := filepath.Join(dir, "adb")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s ' \"$a\"; done >> " + log + "\necho >> " + log + "\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := settle(t.Context(), &sdk{adb: adb}, "emulator-5554"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{
		"-s emulator-5554 shell settings put global animator_duration_scale 0",
		"-s emulator-5554 shell settings put secure long_press_timeout 1500",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("adb was not asked %q; it was asked:\n%s", want, b)
		}
	}
}

// A shade that keeps the focus after it is collapsed, as on a busy emulator, is collapsed again:
// the next step's tap goes to the app, not the shade.
func TestAShadeSlowToCloseIsCollapsedAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	count := filepath.Join(dir, "collapses")
	adb := filepath.Join(dir, "adb")
	// The shade has the focus until it is collapsed a second time.
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"*'statusbar collapse'*) n=$(cat " + count + " 2>/dev/null || echo 0); echo $((n+1)) > " + count + " ;;\n" +
		"*'dumpsys window displays'*) if [ \"$(cat " + count + " 2>/dev/null || echo 0)\" -lt 2 ]; then echo '  mCurrentFocus=Window{caffb71 u0 NotificationShade}'; " +
		"else echo '  mCurrentFocus=Window{1013a10 u0 example.parcels.courier/example.parcels.courier.MainActivity}'; fi ;;\n" +
		"esac\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d := &running{sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554"}}
	if err := d.collapseShade(t.Context()); err != nil {
		t.Fatalf("a shade shut by its second collapse: %v", err)
	}
	if b, _ := os.ReadFile(count); strings.TrimSpace(string(b)) != "2" {
		t.Errorf("collapsed %s times, want 2", strings.TrimSpace(string(b)))
	}
}

// A UiAutomator2 server that ended by itself leaves in its log what the device says of its end:
// here the low-memory killer took it, which its crash log does not show but its process events
// and its system log do; the rest of the device's log stays out.
func TestTheServersEndIsInItsLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	adb := filepath.Join(dir, "adb")
	script := `#!/bin/sh
case "$*" in
*"-b crash"*) ;;
*"-b events"*) printf '%s\n' \
  '10-14 22:50:41.100  1 2 I am_kill : [0,4211,io.appium.uiautomator2.server,-800,lmk]' \
  '10-14 22:50:41.200  1 2 I am_proc_start: [0,4300,10123,com.google.android.gms]' \
  '10-14 22:50:41.300  1 2 I am_proc_died: [0,4211,io.appium.uiautomator2.server,915,3]';;
*"main,system"*) printf '%s\n' \
  '10-14 22:50:40.900 500 500 I lowmemorykiller: Kill io.appium.uiautomator2.server (4211), rss 412MB' \
  '10-14 22:50:40.950 600 600 D WifiService: scan done' \
  '10-14 22:50:41.000 300 300 I ActivityManager: Process io.appium.uiautomator2.server (pid 4211) has died';;
esac
`
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &pool{sdk: &sdk{adb: adb}}
	end := p.endOf(t.Context(), "emulator-5554")
	for _, want := range []string{
		"the device's crash log: nothing",
		"am_kill : [0,4211,io.appium.uiautomator2.server,-800,lmk]",
		"am_proc_died: [0,4211,io.appium.uiautomator2.server,915,3]",
		"lowmemorykiller: Kill io.appium.uiautomator2.server (4211), rss 412MB",
		"Process io.appium.uiautomator2.server (pid 4211) has died",
	} {
		if !strings.Contains(end, want) {
			t.Errorf("the server's end:\n%s\nwants %q", end, want)
		}
	}
	for _, noise := range []string{"com.google.android.gms", "WifiService"} {
		if strings.Contains(end, noise) {
			t.Errorf("the server's end:\n%s\nhas %q", end, noise)
		}
	}
}

// A failed scenario's app log: the app's own lines, by its user id, since the scenario reset it,
// then the system's lines that name the app or are of where the phone is (here, a location
// request that GNSS never answered); the rest of the device's log stays out.
func TestTheAppsLogSinceItsReset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	adb := filepath.Join(dir, "adb")
	calls := filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "$*" >> ` + calls + `
case "$*" in
*"pm list packages -U"*) printf 'package:com.purposebuilt.dtcd uid:10190\npackage:com.purposebuilt.dtcd.test uid:10191\n';;
*"--uid=10190"*) printf '%s\n' '10-14 19:20:40.100  4211  4211 I CrewSafety: intrusion logged, waiting for a fix';;
*"main,system,crash"*) printf '%s\n' \
  '10-14 19:20:40.200   600   700 I LocationManagerService: request from com.purposebuilt.dtcd: gps, interval 1000' \
  '10-14 19:20:40.300   600   700 D WifiService: scan done' \
  '10-14 19:20:41.000   500   500 W GnssLocationProvider: no fix in 1000ms';;
esac
`
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	reset := time.Unix(1792023640, 0)
	d := &running{app: &app{name: "dtcd"}, pkg: "com.purposebuilt.dtcd", sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554"}, resetAt: reset}
	log := d.AppLog(t.Context())
	for _, want := range []string{
		"the dtcd app's own log (com.purposebuilt.dtcd, uid 10190)",
		"CrewSafety: intrusion logged, waiting for a fix",
		"LocationManagerService: request from com.purposebuilt.dtcd",
		"GnssLocationProvider: no fix in 1000ms",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the app's log:\n%s\nwants %q", log, want)
		}
	}
	if strings.Contains(log, "WifiService") {
		t.Errorf("the app's log has the rest of the device's:\n%s", log)
	}
	b, _ := os.ReadFile(calls)
	if !strings.Contains(string(b), "-T 1792023640.000") {
		t.Errorf("the log is not read from the reset on: %s", b)
	}
}

// The app ends with its scenario: left running, it would go on with what it was doing, and take
// the machine from the scenarios running beside it, until the next scenario on its emulator.
func TestTheAppEndsWithItsScenario(t *testing.T) {
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
	d := &running{app: &app{name: "courier"}, pkg: "example.parcels.courier", sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554"}}
	if err := d.Stop(false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "-s|emulator-5554|shell|am|force-stop|example.parcels.courier|") {
		t.Errorf("adb calls:\n%s\nwant the app stopped", b)
	}
}
