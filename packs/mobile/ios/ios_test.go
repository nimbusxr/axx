package mobileios

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium/appiumtest"
)

// screens are the courier app's screens, as Appium's XCUITest driver reads
// them (captured from the app on a simulator); notifications is
// SpringBoard's Notification Center over it.
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

// at is the locator the pack finds the control of a role named name on a
// screen by: the outermost one whose texts include it.
func at(t *testing.T, screens map[string]string, screen string, role mobilecore.Role, name string) string {
	t.Helper()
	s, err := parseSource(screens[screen])
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range s.Visible() {
		if n.Role == role && (n.Name() == name || slices.Contains(n.Texts(), name)) {
			return n.Value
		}
	}
	t.Fatalf("no %q on %s", name, screen)
	return ""
}

// On iOS 27 a list's row is a cell holding the row's button inside wrappers
// the driver calls not visible; the row is still the list item its texts name.
func TestListItemsOnIOS27(t *testing.T) {
	b, err := os.ReadFile("testdata/ios27/deliveries.xml")
	if err != nil {
		t.Fatal(err)
	}
	at(t, map[string]string{"deliveries": string(b)}, "deliveries", mobilecore.RoleListItem, "PX-MOB-9401")
	at(t, map[string]string{"deliveries": string(b)}, "deliveries", mobilecore.RoleListItem, "PX-MOB-9402")
}

// courier is the fake courier app: its screens, and where taps lead.
func courier(t *testing.T) *appiumtest.Server {
	t.Helper()
	sc := screens(t)
	return appiumtest.Start(t, appiumtest.App{
		Screens: sc,
		Start:   "sign-in",
		Taps: map[string]string{
			at(t, sc, "sign-in-filled", mobilecore.RoleButton, "Sign in"): "permission",
			"accept permission":  "deliveries",
			"dismiss permission": "deliveries",
			at(t, sc, "deliveries", mobilecore.RoleListItem, "PX-MOB-9401"):    "delivery",
			at(t, sc, "delivery", mobilecore.RoleButton, "Mark delivered"):     "confirm",
			at(t, sc, "confirm", mobilecore.RoleButton, "Confirm"):             "delivered",
			at(t, sc, "confirm", mobilecore.RoleButton, "Cancel"):              "delivery",
			at(t, sc, "delivery", mobilecore.RoleButton, "Today's deliveries"): "deliveries",
		},
		Links:         map[string]string{"parcels-courier://deliveries/PX-MOB-9401": "delivery"},
		Alerts:        map[string]string{"permission": "“Parcels Courier” Would Like to Send You Notifications\nNotifications may include alerts, sounds, and icon badges."},
		Notifications: "notifications",
	})
}

func register(h *cloudtest.Harness, url string, rows ...[]string) {
	h.OK("the courier ios app with the following properties:", append([][]string{{"bundle id", "example.parcels.courier"}, {"appium", url}}, rows...))
}

// A courier signs in, allows notifications and marks a parcel delivered:
// every step of the packs, on the app's own screens.
func TestACourierDelivers(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL, []string{"locale", "de-DE"}, []string{"timezone", "Europe/Berlin"})
	h.OK(`the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9401" link`)
	h.OK(`the courier app shows "Karl-Liebknecht-Str. 12, 04107 Leipzig"`)
	h.OK("the courier app is restarted")
	app.Show("sign-in")
	h.OK(`the courier app shows "Sign in"`)
	h.OK(`the "Sign in" button is disabled in the courier app`)
	h.OK(`the "Courier ID" field in the courier app has the value '""'`)
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with '"4711"'`)
	app.Show("sign-in-filled")
	h.OK(`the "Courier ID" field in the courier app has the value "CR-LEJ-12"`)
	h.OK(`the "Sign in" button is enabled in the courier app`)
	h.OK(`the "Sign in" button is tapped in the courier app`)
	h.OK(`the courier app's dialog shows "Send You Notifications"`)
	h.OK(`the courier app's dialog is accepted`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK(`the "PX-MOB-9401" list item is shown in the courier app`)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Mark delivered" button is disabled in the courier app`)
	h.OK(`the "Signed by" field in the courier app is filled with "Jonas Weber"`)
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	h.OK(`the courier app shows "Mark PX-MOB-9401 delivered?"`)
	h.OK(`the "Confirm" button is tapped in the courier app`)
	h.OK(`the courier app shows "Delivered"`)
	// The notification slides as it is read, and the driver loses it: the step looks again.
	app.Stale(1)
	h.OK(`the courier app shows a notification "PX-MOB-9401 delivered"`)
	h.OK(`the courier app does not show "Mark delivered"`)
	h.OK("the courier app is swiped down")
	h.OK("the courier app is sent to the background")
	h.OK("the courier app is brought back")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	cmds := strings.Join(app.Commands(), "\n")
	for _, want := range []string{
		// A link opens the app first, in the registration's language and time zone.
		`mobile: queryAppState [{"bundleId":"example.parcels.courier"}]`,
		`mobile: launchApp [{"arguments":["-AppleLanguages","(de-DE)","-AppleLocale","de_DE"],"bundleId":"example.parcels.courier","environment":{"TZ":"Europe/Berlin"}}]`,
		`mobile: deepLink [{"bundleId":"example.parcels.courier","url":"parcels-courier://deliveries/PX-MOB-9401"}]`,
		`mobile: terminateApp [{"bundleId":"example.parcels.courier"}]`,
		`settings {"defaultActiveApplication":"example.parcels.courier"}`,
		"type CR-LEJ-12", "type 4711", "accept alert", "type Jonas Weber",
		"drag", `settings {"defaultActiveApplication":"com.apple.springboard"}`, `mobile: activateApp [{"bundleId":"example.parcels.courier"}]`,
		"held 300ms", `mobile: backgroundApp [{"seconds":-1}]`, "end session",
	} {
		if !strings.Contains(cmds, want) {
			t.Errorf("no %s in:\n%s", want, cmds)
		}
	}
	if app.Screen() != "delivered" {
		t.Errorf("Notification Center stayed open over the app: %s", app.Screen())
	}
	caps := app.Capabilities()
	for key, want := range map[string]any{
		"platformName": "iOS", "appium:automationName": "XCUITest", "appium:bundleId": "example.parcels.courier",
		"appium:noReset": false, "appium:autoLaunch": false,
	} {
		if caps[key] != want {
			t.Errorf("capability %s: %v, want %v", key, caps[key], want)
		}
	}
}

// What a failed step says, and what is not the app's: the keyboard, the
// scroll indicators.
func TestFailures(t *testing.T) {
	app := courier(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	t.Cleanup(mobilecore.SetActionTimeout(300 * time.Millisecond))
	_ = h.Fails(`the "Log in" button is tapped in the courier app`, `No button named "Log in" in the courier app`)
	_ = h.Fails(`within 1s the courier app shows "Today's deliveries"`, `The courier app does not show "Today's deliveries"`)
	// What the screen shows, which says where the text is.
	if err := h.Fails(`within 1s the courier app does not show "Sign in"`, `The courier app shows "Sign in"`); !strings.Contains(err.Error(), "Parcels Courier") {
		t.Errorf("the failure does not say what the screen shows: %v", err)
	}
	_ = h.Fails(`the courier app's dialog is accepted`, "The courier app shows no dialog")
	_ = h.Fails(`within 1s the courier app shows a notification "PX-MOB-9402 delivered"`, `The device of the courier app shows no notification "PX-MOB-9402 delivered"`)
	if app.Screen() != "sign-in" {
		t.Errorf("Notification Center stayed open over the app: %s", app.Screen())
	}
	app.Show("sign-in-filled")
	err := h.Fails(`within 1s the "PIN" field in the courier app has the value "${env:COURIER_PIN}"`, "does not have the value")
	if strings.Contains(err.Error(), "4711") {
		t.Errorf("the secret is in the failure: %v", err)
	}
	for _, hidden := range []string{"0%", "Vertical scroll bar", "Delete"} {
		_ = h.Fails(`within 1s the courier app shows "`+hidden+`"`, "does not show")
	}
	_ = h.End("failed")
}

// XCUITest's element types tell the roles: a cell is a list item, a button
// in an alert a button, a field its placeholder's.
func TestRoles(t *testing.T) {
	sc := screens(t)
	for screen, want := range map[string]map[string]mobilecore.Role{
		"sign-in":    {"Sign in": mobilecore.RoleButton, "Courier ID": mobilecore.RoleField, "PIN": mobilecore.RoleField},
		"deliveries": {"Sign out": mobilecore.RoleButton, "PX-MOB-9401": mobilecore.RoleListItem, "Hello, Hanna Wolf": mobilecore.RoleText},
		"delivery":   {"Signed by": mobilecore.RoleField, "Today's deliveries": mobilecore.RoleButton},
		"confirm":    {"Confirm": mobilecore.RoleButton, "Cancel": mobilecore.RoleButton},
	} {
		s, err := parseSource(sc[screen])
		if err != nil {
			t.Fatal(err)
		}
		for name, role := range want {
			found := false
			for _, n := range s.Visible() {
				if n.Role == role && (n.Name() == name || n.Hint == name || slices.Contains(n.Texts(), name)) {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: no %q of role %d", screen, name, role)
			}
		}
		if s.Size.Width != 393 || s.Size.Height != 852 {
			t.Errorf("%s: the screen is %v points", screen, s.Size)
		}
	}
	s, _ := parseSource(sc["sign-in-filled"])
	for _, n := range s.Nodes {
		if n.Displayed && n.Class == "XCUIElementTypeKey" {
			t.Errorf("the keyboard's %q key is shown as the app's", n.Name())
		}
		if n.Password && n.Text == "4711" {
			t.Error("a PIN field shows its PIN")
		}
	}
}

// A screenshot leaves the status bar out, in the screenshot's pixels.
func TestScreenshots(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL, []string{"device", "iPhone 16"})
	h.OK("the courier app is launched")
	_ = h.Fails(`the courier app looks like the "sign in" screenshot`, `There was no "sign in.ios-iPhone-16" screenshot to compare with, so it was taken`)
	if _, err := os.Stat(filepath.Join(h.Dir, "screenshots", "sign in.ios-iPhone-16.png")); err != nil {
		t.Fatal(err)
	}
	h.OK(`the courier app looks like the "sign in" screenshot`)
	app.Show("deliveries")
	_ = h.Fails(`within 1s the courier app looks like the "sign in" screenshot`, `The courier app does not look like its "sign in" screenshot`)
	_ = h.End("failed")
}

// A swipe waits for the screen to stop moving, as a person does: iOS drops the touches of one
// made while a sheet still slides away, and a pull to refresh is lost with them.
func TestASwipeWaitsForTheScreenToStopMoving(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	app.Sliding(4)
	h.OK("the courier app is swiped down")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	cmds := strings.Join(app.Commands(), "\n")
	if !strings.Contains(cmds, "held 300ms") || strings.Contains(cmds, "the swipe was lost") {
		t.Errorf("the swipe did not wait for the screen to stop moving:\n%s", cmds)
	}
}

// A swipe down is a pull, as a person pulls a list to refresh it: from high on the screen to low,
// held there a moment. XCUITest's own swipe down never refreshed a SwiftUI List.
func TestASwipeDownPulls(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK("the courier app is swiped down")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	cmds := strings.Join(app.Commands(), "\n")
	if strings.Contains(cmds, "mobile: swipe") || !strings.Contains(cmds, "drag\nheld 300ms") {
		t.Errorf("no pull:\n%s", cmds)
	}
}

// A dialog the scenario left unanswered is dismissed as it ends, before
// its session does: the next scenario's app, installed afresh, asks again.
func TestADialogLeftOpenIsDismissed(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with '"4711"'`)
	app.Show("sign-in-filled")
	h.OK(`the "Sign in" button is tapped in the courier app`)
	h.OK(`the courier app's dialog shows "Send You Notifications"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	cmds := app.Commands()
	dismissed, ended := slices.Index(cmds, "dismiss alert"), slices.Index(cmds, "end session")
	if dismissed < 0 || ended < dismissed {
		t.Errorf("the dialog left open is not dismissed before the session ends:\n%s", strings.Join(cmds, "\n"))
	}
	if app.Screen() != "deliveries" {
		t.Errorf("the dialog is still shown: %s", app.Screen())
	}
}

// iOS 27 says what an action sheet shows is not visible: its buttons are on
// the screen, in front of the app, and are what a person taps.
func TestAnActionSheetShowsItsButtons(t *testing.T) {
	src := `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Mark delivered" label="Mark delivered" x="20" y="700" width="353" height="50" visible="false"/>
<XCUIElementTypeSheet type="XCUIElementTypeSheet" name="Hand over" label="Hand over" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeOther type="XCUIElementTypeOther" x="76" y="515" width="240" height="80" visible="false">
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Hand over now" label="Hand over now" x="92" y="531" width="208" height="48" visible="false"/>
<XCUIElementTypeOther type="XCUIElementTypeOther" name="Vertical scroll bar, 1 page" label="Vertical scroll bar, 1 page" x="283" y="515" width="30" height="46" visible="false"/>
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Below" label="Below" x="92" y="900" width="208" height="48" visible="false"/>
</XCUIElementTypeOther></XCUIElementTypeSheet></XCUIElementTypeApplication></AppiumAUT>`
	s, err := parseSource(src)
	if err != nil {
		t.Fatal(err)
	}
	shown := map[string]bool{}
	for _, n := range s.Visible() {
		shown[n.Name()] = true
	}
	if !shown["Hand over now"] {
		t.Error("the sheet's button on the screen is not shown")
	}
	for _, n := range s.Visible() {
		if n.Name() == "Hand over now" && !n.ByPoint {
			t.Error("the sheet's button is not tapped where it is: iOS 27's own tap of it does nothing")
		}
	}
	for _, hidden := range []string{"Mark delivered", "Vertical scroll bar, 1 page", "Below"} {
		if shown[hidden] {
			t.Errorf("%q is shown: the app behind the sheet, a scroll bar, or off the screen", hidden)
		}
	}
}

// A long text's scroll bar says how many pages it holds, which says where a scrub lands.
func TestScrollBarPages(t *testing.T) {
	for label, want := range map[string]string{"Vertical scroll bar, 25 pages": "25", "Vertical scroll bar, 1 page": "1", "Horizontal scroll bar, 3 pages": ""} {
		got := ""
		if m := pagesRE.FindStringSubmatch(label); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q: %q pages, want %q", label, got, want)
		}
	}
}

// With the keyboard up, a scroll view's drags and its shown controls are in the part above the
// keyboard: a finger on the keys types, and a control under them does not show.
func TestTheKeyboardCoversTheScrollViewsBottom(t *testing.T) {
	src := func(keyboard string) string {
		return `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="DTCD" label="DTCD" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeWindow type="XCUIElementTypeWindow" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeScrollView type="XCUIElementTypeScrollView" x="0" y="100" width="393" height="752" visible="true">
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Save Changes" label="Save Changes" x="16" y="1900" width="361" height="54" visible="false"/>
</XCUIElementTypeScrollView></XCUIElementTypeWindow>` + keyboard + `</XCUIElementTypeApplication></AppiumAUT>`
	}
	for name, c := range map[string]struct {
		keyboard string
		height   float64
	}{
		"keyboard up": {`<XCUIElementTypeKeyboard type="XCUIElementTypeKeyboard" x="0" y="516" width="393" height="336" visible="true"/>`, 416},
		"no keyboard": {"", 752},
	} {
		s, err := parseSource(src(c.keyboard))
		if err != nil {
			t.Fatal(err)
		}
		var view *mobilecore.Node
		for _, n := range s.Nodes {
			if n.Scrolling {
				view = n
			}
		}
		if view == nil {
			t.Fatal("no scroll view")
		}
		if v := uncovered(view); v.Y != 100 || v.Height != c.height {
			t.Errorf("%s: the view shows %v, want y 100 and height %v", name, v, c.height)
		}
	}
}

// A form whose rows are built as they scroll in has nothing to aim for: the step scrolls a page at
// a time. With the keyboard up, those scrolls drag above it rather than swipe on the keys.
func TestScrollingAFormWithTheKeyboardUpDragsAboveIt(t *testing.T) {
	form := `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeCollectionView type="XCUIElementTypeCollectionView" x="0" y="100" width="393" height="752" visible="true">
<XCUIElementTypeTextField type="XCUIElementTypeTextField" name="Parcel ID" label="Parcel ID" value="PX-MOB-9401" x="16" y="400" width="361" height="44" visible="true"/>
</XCUIElementTypeCollectionView>
<XCUIElementTypeKeyboard type="XCUIElementTypeKeyboard" x="0" y="516" width="393" height="336" visible="true"/>
</XCUIElementTypeApplication></AppiumAUT>`
	app := appiumtest.Start(t, appiumtest.App{Screens: map[string]string{"form": form}, Start: "form"})
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	_ = h.Fails(`the "Hand Over" button is scrolled into view in the courier app`, `No button named "Hand Over"`)
	drags := 0
	for _, c := range app.Commands() {
		if strings.HasPrefix(c, "mobile: scroll") {
			t.Errorf("the platform's scroll, which swipes on the keys: %s", c)
		}
		if c == "drag" {
			drags++
		}
	}
	if drags == 0 {
		t.Error("no drag above the keyboard")
	}
}

// A sheet that takes no notice of a tap looks just as it did a moment later: its button is
// tapped again, once. A tap it hears shows, and is not repeated.
func TestASheetTapItIgnoredIsTappedAgain(t *testing.T) {
	sheet := `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeSheet type="XCUIElementTypeSheet" name="Save Password?" label="Save Password?" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Not Now" label="Not Now" x="92" y="531" width="208" height="48" visible="false"/>
</XCUIElementTypeSheet></XCUIElementTypeApplication></AppiumAUT>`
	drags := func(app *appiumtest.Server) int {
		n := 0
		for _, c := range app.Commands() {
			if c == "drag" {
				n++
			}
		}
		return n
	}
	for _, c := range []struct {
		name  string
		app   appiumtest.App
		drags int
	}{
		{"ignored", appiumtest.App{Screens: map[string]string{"sheet": sheet}, Start: "sheet"}, 2},
		{"heard", appiumtest.App{Screens: map[string]string{"sheet": sheet, "gone": "<AppiumAUT/>"}, Start: "sheet", Taps: map[string]string{"tap sheet": "gone"}}, 1},
	} {
		app := appiumtest.Start(t, c.app)
		h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
		register(h, app.URL)
		h.OK("the courier app is launched")
		before := drags(app)
		h.OK(`the "Not Now" button is tapped in the courier app`)
		if got := drags(app) - before; got != c.drags {
			t.Errorf("%s: %d taps, want %d", c.name, got, c.drags)
		}
	}
}

// XCTest gives up on a launch that takes long on a busy Mac: an app up all the same is
// launched; one that is not is launched once more.
func TestALaunchXCTestGaveUpOn(t *testing.T) {
	launches := func(app *appiumtest.Server) int {
		n := 0
		for _, c := range app.Commands() {
			if strings.HasPrefix(c, "mobile: launchApp") {
				n++
			}
		}
		return n
	}
	for _, c := range []struct {
		name     string
		up       bool
		launches int
	}{{"up all the same", true, 1}, {"not up", false, 2}} {
		app := courier(t)
		app.LaunchTimeouts(1, c.up)
		h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
		register(h, app.URL)
		h.OK("the courier app is launched")
		h.OK(`the courier app shows "Sign in"`)
		if got := launches(app); got != c.launches {
			t.Errorf("%s: %d launches, want %d", c.name, got, c.launches)
		}
	}
}

// An app installed a moment before is not known to SpringBoard yet, which refuses to launch it:
// the launch is tried again until SpringBoard knows it.
func TestAnAppJustInstalledIsLaunchedOnceSpringBoardKnowsIt(t *testing.T) {
	app := courier(t)
	app.LaunchesUnknown(2)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK(`the courier app shows "Sign in"`)
	n := 0
	for _, c := range app.Commands() {
		if strings.HasPrefix(c, "mobile: launchApp") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("%d launches, want 3: two refused, then the app's", n)
	}
}

// A sheet's button iOS says is visible is tapped where it is too: XCTest's own tap of it does
// nothing on iOS 27.
func TestASheetsVisibleButtonIsTappedByPoint(t *testing.T) {
	src := `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeSheet type="XCUIElementTypeSheet" name="Save Password?" label="Save Password?" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Not Now" label="Not Now" x="92" y="531" width="208" height="48" visible="true"/>
</XCUIElementTypeSheet></XCUIElementTypeApplication></AppiumAUT>`
	s, err := parseSource(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range s.Visible() {
		if n.Name() == "Not Now" && !n.ByPoint {
			t.Error("the sheet's visible button is not tapped where it is")
		}
	}
}

// A SwiftUI Toggle (the courier app's "Share van location", as WebDriverAgent reads it on iOS 27)
// is a switch as wide as its row, with the switch control in it: a tap goes to the control,
// since XCTest's tap of the row lands on its label, which does not toggle it.
func TestATogglesTapGoesToItsSwitch(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "swiftui", "toggle.xml"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseSource(string(b))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range s.Visible() {
		if n.Name() == "Share van location" {
			if !n.ByPoint || n.Bounds != (mobilecore.Rect{X: 300, Y: 207, Width: 63, Height: 28}) {
				t.Errorf("the toggle is tapped at %+v (by point: %v), want its switch at 300, 207", n.Bounds, n.ByPoint)
			}
			return
		}
	}
	t.Error("no Share van location")
}

// iOS asks for a location with three buttons, the one that declines last: accepted, the dialog
// is answered with the one before it, which allows for good. WebDriverAgent's own accept would
// press the last.
func TestALocationRequestIsAllowedWhileUsingTheApp(t *testing.T) {
	sc := screens(t)
	app := appiumtest.Start(t, appiumtest.App{
		Screens:      sc,
		Start:        "deliveries",
		Alerts:       map[string]string{"deliveries": "Allow “Parcels Courier” to use your location?"},
		AlertButtons: map[string][]string{"deliveries": {"Allow Once", "Allow While Using App", "Don’t Allow"}},
		Taps:         map[string]string{"accept with Allow While Using App deliveries": "delivery"},
	})
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK(`the courier app's dialog is accepted`)
	if cmds := strings.Join(app.Commands(), "\n"); !strings.Contains(cmds, "accept with Allow While Using App alert") {
		t.Errorf("the dialog was not allowed while using the app:\n%s", cmds)
	}
}

// A keyboard still coming in (on a busy machine, a while) has no keys yet, and WebDriverAgent,
// asked to close it, knows no way to: the step closes it once its keys are there, rather than
// failing as it did on a 2-core VM sized as GitHub's small runner.
func TestAKeyboardStillComingInIsClosedOnceItCanBe(t *testing.T) {
	button := `<XCUIElementTypeButton type="XCUIElementTypeButton" name="Mark delivered" label="Mark delivered" x="16" y="700" width="361" height="44" visible="%s"/>`
	screen := func(keyboard bool) string {
		visible, keys := "true", ""
		if keyboard {
			visible, keys = "false", `<XCUIElementTypeKeyboard type="XCUIElementTypeKeyboard" x="0" y="516" width="393" height="336" visible="true"/>`
		}
		return `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">` +
			fmt.Sprintf(button, visible) + keys + `</XCUIElementTypeApplication></AppiumAUT>`
	}
	screens := map[string]string{"typing": screen(true), "typed": screen(false), "confirm": confirmScreen}
	app := appiumtest.Start(t, appiumtest.App{
		Screens: screens,
		Start:   "typing",
		// The button, once the keyboard is closed, opens the confirmation, as the app's does.
		Taps: map[string]string{"hide keyboard typing": "typed", at(t, screens, "typed", mobilecore.RoleButton, "Mark delivered"): "confirm"},
	})
	app.KeyboardComingIn(1)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	if app.Screen() != "confirm" {
		t.Errorf("the keyboard was not closed, or the button not tapped: %s", app.Screen())
	}
}

// confirmScreen is what the courier app shows once "Mark delivered" is tapped.
const confirmScreen = `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">` +
	`<XCUIElementTypeStaticText type="XCUIElementTypeStaticText" name="Mark PX-MOB-9401 delivered?" label="Mark PX-MOB-9401 delivered?" x="16" y="300" width="361" height="44" visible="true"/>` +
	`</XCUIElementTypeApplication></AppiumAUT>`

// A keyboard slow to go, which WebDriverAgent says it did not know how to close, is given time to
// go: its key is tapped once.
func TestAKeyboardSlowToGoIsClosedOnce(t *testing.T) {
	button := `<XCUIElementTypeButton type="XCUIElementTypeButton" name="Mark delivered" label="Mark delivered" x="16" y="700" width="361" height="44" visible="%s"/>`
	keyboard := `<XCUIElementTypeKeyboard type="XCUIElementTypeKeyboard" x="0" y="516" width="393" height="336" visible="true"/>`
	screens := map[string]string{
		"typing":  `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">` + fmt.Sprintf(button, "false") + keyboard + `</XCUIElementTypeApplication></AppiumAUT>`,
		"typed":   `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">` + fmt.Sprintf(button, "true") + `</XCUIElementTypeApplication></AppiumAUT>`,
		"confirm": confirmScreen,
	}
	app := appiumtest.Start(t, appiumtest.App{
		Screens: screens,
		Start:   "typing",
		Taps:    map[string]string{"hide keyboard typing": "typed", at(t, screens, "typed", mobilecore.RoleButton, "Mark delivered"): "confirm"},
	})
	app.KeyboardSlowToGo(time.Second)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	if app.Screen() != "confirm" {
		t.Errorf("the keyboard was not closed, or the button not tapped: %s", app.Screen())
	}
	if n := app.KeyboardClosings(); n != 1 {
		t.Errorf("the keyboard's key was tapped %d times, want once", n)
	}
}

// A control in a row that scrolls sideways, past its end, is not listed until it shows: the step
// looks along the row, as a person does, and finds it there.
func TestAControlPastTheEndOfARowIsScrolledIntoView(t *testing.T) {
	form := func(chips ...string) string {
		var b strings.Builder
		for i, c := range chips {
			fmt.Fprintf(&b, `<XCUIElementTypeButton type="XCUIElementTypeButton" name="%s" label="%s" x="%d" y="400" width="120" height="32" visible="true"/>`, c, c, 16+i*128)
		}
		return `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Couriers" label="Couriers" x="0" y="0" width="393" height="852" visible="true">
<XCUIElementTypeStaticText type="XCUIElementTypeStaticText" name="Restrictions" label="Restrictions" x="16" y="370" width="200" height="20" visible="true"/>
<XCUIElementTypeScrollView type="XCUIElementTypeScrollView" x="0" y="396" width="393" height="40" visible="true">` + b.String() + `</XCUIElementTypeScrollView>
</XCUIElementTypeApplication></AppiumAUT>`
	}
	app := appiumtest.Start(t, appiumtest.App{
		Screens: map[string]string{"start": form("No trucks", "HOV 3+", "No parking"), "along": form("Reduced width", "Reduced height")},
		Start:   "start",
		Taps:    map[string]string{"drag left start": "along"},
	})
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK(`the "Reduced width" button is scrolled into view in the courier app`)
	h.OK(`the "Reduced width" button is tapped in the courier app`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// webForm is a web page an app shows in a web view, as WebDriverAgent reads it: the page's
// controls are the web view's, which has no scroll view among its elements, and a radio button is
// an element with the label beside it. The field further down the page is not on the screen.
const webForm = `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Reports" label="Reports" x="0" y="0" width="402" height="874" visible="true">
<XCUIElementTypeWindow type="XCUIElementTypeWindow" x="0" y="0" width="402" height="874" visible="true">
<XCUIElementTypeWebView type="XCUIElementTypeWebView" x="0" y="62" width="402" height="778" visible="true">
<XCUIElementTypeOther type="XCUIElementTypeOther" name="Report a delivery problem" label="Report a delivery problem" x="0" y="62" width="402" height="1700" visible="true">
<XCUIElementTypeOther type="XCUIElementTypeOther" name="Can the parcel go to a neighbour?" label="Can the parcel go to a neighbour?" x="16" y="393" width="370" height="138" visible="true">
<XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Can the parcel go to a neighbour?" name="Can the parcel go to a neighbour?" label="Can the parcel go to a neighbour?" x="16" y="395" width="260" height="21" visible="true"/>
<XCUIElementTypeOther type="XCUIElementTypeOther" value="0" name="Leave it next door" label="Leave it next door" x="28" y="428" width="356" height="45" visible="true" traits="ToggleButton"/>
<XCUIElementTypeOther type="XCUIElementTypeOther" value="0" name="Take it back" label="Take it back" x="28" y="480" width="356" height="45" visible="true" traits="ToggleButton"/>
</XCUIElementTypeOther>
<XCUIElementTypeTextField type="XCUIElementTypeTextField" name="Who did you speak to?" label="Who did you speak to?" x="16" y="1525" width="370" height="45" visible="false"/>
</XCUIElementTypeOther></XCUIElementTypeWebView></XCUIElementTypeWindow></XCUIElementTypeApplication></AppiumAUT>`

// A web view scrolls its page: a control further down the page is one the screen has out of
// view, in something that scrolls, so a step scrolls to it.
func TestAWebViewScrollsItsPage(t *testing.T) {
	s, err := parseSource(webForm)
	if err != nil {
		t.Fatal(err)
	}
	var field *mobilecore.Node
	for _, n := range s.Nodes {
		if n.Name() == "Who did you speak to?" {
			field = n
		}
	}
	if field == nil || field.Displayed {
		t.Fatalf("the field further down the page: %+v", field)
	}
	view := field.Parent
	for view != nil && !view.Scrolling {
		view = view.Parent
	}
	if view == nil || view.Class != "XCUIElementTypeWebView" {
		t.Errorf("the field is in nothing that scrolls: the web view is %+v", view)
	}
}

// XCTest cannot place its own tap on some controls: a web page's radio button past the page's
// first screen is one. WebDriverAgent's tap of it taps nothing and answers that it tapped. It
// does say the control is not hittable, so the step asks, and taps such a control where it is on
// the screen, as a finger does; a control it can hit keeps WebDriverAgent's own tap.
func TestAControlTheDriverCannotPlaceIsTappedWhereItIs(t *testing.T) {
	const next, back = `//XCUIElementTypeOther[@label="Leave it next door"]`, `//XCUIElementTypeOther[@label="Take it back"]`
	chosen := strings.Replace(webForm, `value="0" name="Leave it next door"`, `value="1" name="Leave it next door"`, 1)
	app := appiumtest.Start(t, appiumtest.App{
		Screens:  map[string]string{"form": webForm, "chosen": chosen},
		Start:    "form",
		Taps:     map[string]string{"tap form": "chosen", back: "form"},
		Unplaced: []string{next},
	})
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	count := func(prefix string) int {
		n := 0
		for _, c := range app.Commands() {
			if c == prefix || strings.HasPrefix(c, prefix+" ") {
				n++
			}
		}
		return n
	}
	h.OK(`the 'xpath=` + next + `' element is tapped in the courier app`)
	if app.Screen() != "chosen" || count("drag") != 1 || count("lost tap") != 0 {
		t.Errorf("the radio button the driver cannot place: the app shows %s after %v", app.Screen(), app.Commands())
	}
	if !slices.ContainsFunc(h.Sink.Logs, func(l string) bool { return strings.Contains(l, "it is tapped where it is, at 206, 450") }) {
		t.Errorf("the step does not say where it tapped: %v", h.Sink.Logs)
	}
	// One the driver can hit is the driver's to tap.
	h.OK(`the 'xpath=` + back + `' element is tapped in the courier app`)
	if app.Screen() != "form" || count("drag") != 1 || count("tap") != 1 {
		t.Errorf("a control the driver can hit: the app shows %s after %v", app.Screen(), app.Commands())
	}
}

// padScreen is a screen with a number pad up over a button, as WebDriverAgent reads a web form's
// phone field being typed in: the pad has no key that closes it, and over it is the form's bar,
// its buttons some views deep, the last one closing the keyboard. done names that button.
func padScreen(done string) string {
	return `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Reports" label="Reports" x="0" y="0" width="402" height="874" visible="true">
<XCUIElementTypeWindow type="XCUIElementTypeWindow" x="0" y="0" width="402" height="874" visible="true">
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Send report" label="Send report" x="16" y="727" width="370" height="48" visible="false"/>
</XCUIElementTypeWindow>
<XCUIElementTypeWindow type="XCUIElementTypeWindow" x="0" y="0" width="402" height="874" visible="false">
<XCUIElementTypeToolbar type="XCUIElementTypeToolbar" name="Toolbar" label="Toolbar" x="0" y="514" width="402" height="48" visible="true">
<XCUIElementTypeOther type="XCUIElementTypeOther" x="0" y="514" width="402" height="48" visible="true">
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Previous" label="Previous" x="21" y="519" width="40" height="38" visible="true"/>
<XCUIElementTypeButton type="XCUIElementTypeButton" name="Next" label="Next" x="75" y="519" width="40" height="38" visible="false"/>
<XCUIElementTypeButton type="XCUIElementTypeButton" name="` + done + `" label="` + done + `" x="341" y="519" width="40" height="38" visible="false"/>
</XCUIElementTypeOther></XCUIElementTypeToolbar></XCUIElementTypeWindow>
<XCUIElementTypeWindow type="XCUIElementTypeWindow" x="0" y="0" width="402" height="874" visible="true">
<XCUIElementTypeKeyboard type="XCUIElementTypeKeyboard" x="0" y="589" width="402" height="227" visible="true">
<XCUIElementTypeKey type="XCUIElementTypeKey" name="0" label="0" x="137" y="758" width="133" height="54" visible="true"/>
</XCUIElementTypeKeyboard></XCUIElementTypeWindow>
</XCUIElementTypeApplication></AppiumAUT>`
}

// The bar over a keyboard closes it with its Done, which does nothing else: the bar is the
// toolbar on the keyboard's top edge, and no other. A bar in another language has a last button
// all the same, for a keyboard no key closes.
func TestTheBarOverAKeyboardClosesIt(t *testing.T) {
	read := func(src string) *mobilecore.Screen {
		t.Helper()
		s, err := parseSource(src)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if b := barDone(read(padScreen("Done")), false); b == nil || b.Bounds != (mobilecore.Rect{X: 341, Y: 519, Width: 40, Height: 38}) {
		t.Errorf("the bar's Done: %+v", b)
	}
	german := read(padScreen("Fertig"))
	if b := barDone(german, false); b != nil {
		t.Errorf("a button not named Done was taken for it: %+v", b)
	}
	if b := barDone(german, true); b == nil || b.Name() != "Fertig" {
		t.Errorf("the bar's last button: %+v", b)
	}
	// An app's own toolbar, away from the keyboard, is not the keyboard's bar; nor is any bar
	// one with no keyboard on the screen.
	away := read(strings.Replace(padScreen("Done"), `label="Toolbar" x="0" y="514"`, `label="Toolbar" x="0" y="100"`, 1))
	if b := barDone(away, true); b != nil {
		t.Errorf("a toolbar away from the keyboard: %+v", b)
	}
	closed := read(strings.ReplaceAll(padScreen("Done"), "XCUIElementTypeKeyboard", "XCUIElementTypeOther"))
	if b := barDone(closed, true); b != nil {
		t.Errorf("a bar with no keyboard: %+v", b)
	}
}

// A number pad has no key that closes it: WebDriverAgent says it does not know how to close the
// keyboard, and the step failed on that. The bar over the pad closes it, so a control under the
// pad is reached as a person reaches it: Done, then the control.
func TestAControlUnderANumberPadIsReachedThroughTheBar(t *testing.T) {
	open := strings.ReplaceAll(strings.Replace(padScreen("Done"), `y="727" width="370" height="48" visible="false"`, `y="727" width="370" height="48" visible="true"`, 1),
		"XCUIElementTypeKeyboard", "XCUIElementTypeOther")
	sent := `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Reports" label="Reports" x="0" y="0" width="402" height="874" visible="true">` +
		`<XCUIElementTypeStaticText type="XCUIElementTypeStaticText" name="Report sent" label="Report sent" x="16" y="300" width="361" height="44" visible="true"/>` +
		`</XCUIElementTypeApplication></AppiumAUT>`
	screens := map[string]string{"pad": padScreen("Done"), "open": open, "sent": sent}
	app := appiumtest.Start(t, appiumtest.App{
		Screens: screens,
		Start:   "pad",
		// A finger on the bar's Done closes the pad; the button, in view by then, sends the report.
		Taps: map[string]string{"tap pad": "open", at(t, screens, "open", mobilecore.RoleButton, "Send report"): "sent"},
	})
	// WebDriverAgent never learns how to close this keyboard.
	app.KeyboardComingIn(100)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK(`the "Send report" button is tapped in the courier app`)
	if app.Screen() != "sent" {
		t.Errorf("the button under the pad was not reached: the app shows %s after %v", app.Screen(), app.Commands())
	}
	if n := app.KeyboardClosings(); n != 0 {
		t.Errorf("the pad was closed with a key %d times: it has none", n)
	}
}
