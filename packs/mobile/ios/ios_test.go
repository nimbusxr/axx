package mobileios

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
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
	h := cloudtest.New(t, mobilecore.Pack(), Pack())
	register(h, app.URL, []string{"locale", "de-DE"}, []string{"timezone", "Europe/Berlin"})
	h.OK(`the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9401" link`)
	h.OK(`the courier app shows "Karl-Liebknecht-Str. 12, 04107 Leipzig"`)
	h.OK("the courier app is restarted")
	app.Show("sign-in")
	h.OK(`the courier app shows "Sign in"`)
	h.OK(`the "Sign in" button is disabled in the courier app`)
	h.OK(`the "Courier ID" field in the courier app has the value ""`)
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with "4711"`)
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
		`mobile: swipe [{"direction":"down"}]`, `mobile: backgroundApp [{"seconds":-1}]`, "end session",
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
	h := cloudtest.New(t, mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	t.Cleanup(mobilecore.SetActionTimeout(300 * time.Millisecond))
	_ = h.Fails(`the "Log in" button is tapped in the courier app`, `No button named "Log in" in the courier app`)
	_ = h.Fails(`within 1s the courier app shows "Today's deliveries"`, `The courier app does not show "Today's deliveries"`)
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
	h := cloudtest.New(t, mobilecore.Pack(), Pack())
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

// A dialog the scenario left unanswered is dismissed as it ends, before
// its session does: the next scenario's app, installed afresh, asks again.
func TestADialogLeftOpenIsDismissed(t *testing.T) {
	app := courier(t)
	h := cloudtest.New(t, mobilecore.Pack(), Pack())
	register(h, app.URL)
	h.OK("the courier app is launched")
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with "4711"`)
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
