//go:build integration

package mobileios

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/courierapi"
)

// The integration tests run the parcels example's couriers' app on
// simulators axx makes: of AXX_IOS_DEVICE, or iPhone 16, with the app built
// (xcodebuild in examples/parcels/courier/ios; see its README).

var courierApp = filepath.Join("..", "..", "..", "examples", "parcels", "courier", "ios", "build", "Debug-iphonesimulator", "Courier.app")

func simulator(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode: iOS simulators run on a Mac")
	}
	if name := os.Getenv("AXX_IOS_DEVICE"); name != "" {
		return name
	}
	return "iPhone 16"
}

func courierBuild(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(courierApp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p, "Info.plist")); err != nil {
		t.Fatalf("the couriers' app is not built: see examples/parcels/courier/ios/README.md (%v)", err)
	}
	return p
}

// harness runs the packs for a test; a failed one prints the end of its
// Appium's log.
func harness(t *testing.T) *cloudtest.Harness {
	t.Helper()
	h := cloudtest.New(t, mobilecore.Pack(), Pack())
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		logs, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "mobile", "*.log"))
		for _, l := range logs {
			b, _ := os.ReadFile(l)
			lines := strings.Split(string(b), "\n")
			t.Logf("the end of %s:\n%s", filepath.Base(l), strings.Join(lines[max(0, len(lines)-80):], "\n"))
		}
	})
	return h
}

func signIn(h *cloudtest.Harness) {
	h.OK(`the courier app is launched`)
	h.OK(`the "Sign in" button is disabled in the courier app`)
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with "${env:COURIER_PIN}"`)
	h.OK(`the "Sign in" button is tapped in the courier app`)
}

// A courier delivers a parcel on a simulator: every step of the packs.
func TestACourierDeliversOnASimulator(t *testing.T) {
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := harness(t)
	h.OK("the courier ios app with the following properties:", [][]string{
		{"app", courierBuild(t)}, {"device", simulator(t)}, {"timezone", "Europe/Berlin"}, {"locale", "en-GB"}, {"location", "51.3397, 12.3731"},
	})
	signIn(h)
	h.OK(`the courier app's dialog shows "Send You Notifications"`)
	h.OK(`the courier app's dialog is accepted`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Mark delivered" button is disabled in the courier app`)
	h.OK(`the "Signed by" field in the courier app is filled with "Jonas Weber"`)
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	h.OK(`the courier app shows "Mark PX-MOB-9401 delivered?"`)
	h.OK(`the "Confirm" button is tapped in the courier app`)
	h.OK(`the courier app shows "Delivered"`)
	h.OK(`the courier app shows a notification "PX-MOB-9401 delivered"`)
	h.OK(`the courier app does not show "PX-MOB-9401"`)
	h.OK("the courier app is swiped down")
	h.OK(`the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9402" link`)
	h.OK(`the courier app shows "Prager Str. 3, 04103 Leipzig"`)
	h.OK("the courier app is sent to the background")
	h.OK("the courier app is brought back")
	h.OK(`the courier app shows "Prager Str. 3, 04103 Leipzig"`)
	h.OK("the courier app is restarted")
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	for _, l := range h.Sink.Logs {
		if strings.Contains(l, "4711") {
			t.Errorf("the PIN is in the logs: %s", l)
		}
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// Two scenarios on one simulator: the second starts signed out (the first
// kept its sign-in in the keychain), and iOS asks again for notifications.
func TestScenariosOnASimulatorAreIsolated(t *testing.T) {
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := harness(t)
	h.OK("the courier ios app with the following properties:", [][]string{{"app", courierBuild(t)}, {"device", simulator(t)}})
	signIn(h)
	h.OK(`the courier app's dialog is accepted`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK("the courier app is restarted")
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	h.NewScenario()
	h.OK("the courier ios app with the following properties:", [][]string{{"app", courierBuild(t)}, {"device", simulator(t)}})
	h.OK(`the courier app is launched`)
	h.OK(`the courier app shows "Sign in"`)
	h.OK(`the "Courier ID" field in the courier app has the value ""`)
	signIn(h)
	h.OK(`the courier app's dialog shows "Send You Notifications"`)
	h.OK(`the courier app's dialog is dismissed`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
