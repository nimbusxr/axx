//go:build integration

package mobileios

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
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
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		// What the app showed, as the pack read it, while its session lasts.
		_ = mobilecore.OnDevice(h.SC, "courier", func(_ context.Context, d mobilecore.Device) error {
			// The scenario's context is over by now; the session is not.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			src, err := d.Session().Source(ctx)
			t.Logf("the courier app's page source at the failure (%v):\n%s", err, src)
			return nil
		})
		logs, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "mobile", "*.log"))
		for _, l := range logs {
			b, _ := os.ReadFile(l)
			lines := strings.Split(string(b), "\n")
			t.Logf("the end of %s:\n%s", filepath.Base(l), strings.Join(lines[max(0, len(lines)-80):], "\n"))
		}
	})
	return h
}

// warm makes the test's simulator before its scenario: setting up the
// simulator axx clones, on a runner that has none yet, and the first
// downloads of Appium and WebDriverAgent take longer than a scenario may.
// The scenario then leases the clone, and resets it as ever.
func warm(t *testing.T, h *cloudtest.Harness, device string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	p, err := poolFor(h.SC, device)
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.lease(ctx, filepath.Join(h.Dir, ".axx", "mobile"))
	if err != nil {
		t.Fatal(err)
	}
	p.release(d)
	// The scenario starts now, with all of its time.
	h.NewScenarioWithin(scenarioLimit)
}

// scenarioLimit is how long a scenario may take: on a hosted runner, a
// simulator is slow for minutes after it boots, and a journey of twenty-odd
// steps takes longer than the harness's usual five.
const scenarioLimit = 15 * time.Minute

func signIn(h *cloudtest.Harness) {
	h.OK(`the courier app is launched`)
	h.OK(`the "Sign in" button is disabled in the courier app`)
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with "${env:COURIER_PIN}"`)
	h.OK(`the "Sign in" button is tapped in the courier app`)
}

// A courier delivers a parcel on a simulator, with every step of the packs;
// the next scenario, on the same simulator, starts signed out (the first kept
// its sign-in in the keychain, which outlives the app), and iOS asks again
// for notifications. One simulator for both, as a run's worker has.
func TestCouriersOnASimulator(t *testing.T) {
	device := simulator(t)
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := harness(t)
	warm(t, h, device)
	h.OK("the courier ios app with the following properties:", [][]string{
		{"app", courierBuild(t)}, {"device", device}, {"timezone", "Europe/Berlin"}, {"locale", "en-GB"}, {"location", "51.3397, 12.3731"},
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
	// A runner reads SpringBoard's screen in seconds, not in a fraction of one.
	h.OK(`within 30s the courier app shows a notification "PX-MOB-9401 delivered"`)
	h.OK(`the "Back to deliveries" button is tapped in the courier app`)
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

	h.NewScenarioWithin(scenarioLimit)
	h.OK("the courier ios app with the following properties:", [][]string{{"app", courierBuild(t)}, {"device", device}})
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

// recordingsKept checks that the scenario kept a trace of the courier app,
// its screen after each step, and a video of its phone as it ran, with a
// chapter for each step; and that the run kept one more, of its scenarios.
func recordingsKept(t *testing.T, h *cloudtest.Harness) {
	t.Helper()
	traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "mobile", "traces", "*-courier-*.html"))
	videos, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "mobile", "videos", "test-*.mp4"))
	if len(traces) != 1 || len(videos) != 1 {
		t.Fatalf("traces %v, videos %v", traces, videos)
	}
	page, _ := os.ReadFile(traces[0])
	if n := strings.Count(string(page), "data:image/jpeg;base64,"); n < 5 {
		t.Errorf("the trace has %d screens, not one for each step from the app's start", n)
	}
	movie, _ := os.ReadFile(videos[0])
	if len(movie) < 8 || string(movie[4:8]) != "ftyp" || !bytes.Contains(movie, []byte("avcC")) {
		t.Errorf("the video is not H.264 in an MP4 file: %q", movie[:min(len(movie), 16)])
	}
	// The first step's chapter opens every video; steps that start in the same moment share one.
	for _, want := range []string{"chpl", "* the courier ios app with the following properties:"} {
		if !bytes.Contains(movie, []byte(want)) {
			t.Errorf("the video's chapters lack %q", want)
		}
	}
	if err := h.Suite.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("AXX_KEEP_RECORDINGS"); dir != "" {
		_ = os.CopyFS(dir, os.DirFS(filepath.Join(h.Dir, ".axx", "mobile")))
	}
	run, err := os.ReadFile(filepath.Join(h.Dir, ".axx", "mobile", "videos", "run.mp4"))
	if err != nil || len(run) <= len(movie) || !bytes.Contains(run, []byte("✓ "+h.SC.Name)) {
		t.Errorf("the run's video is not the scenario's after cards: %d bytes, the scenario's %d (%v)", len(run), len(movie), err)
	}
}

// A scenario keeps what the settings ask: a trace and a video of its phone.
func TestRecordingsOnASimulator(t *testing.T) {
	device := simulator(t)
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cloudtest.NewWith(t, map[string]any{"mobile-core": map[string]any{"traces": "always", "videos": "always"}}, appcore.Pack(), mobilecore.Pack(), Pack())
	warm(t, h, device)
	h.OK("the courier ios app with the following properties:", [][]string{{"app", courierBuild(t)}, {"device", device}})
	signIn(h)
	h.OK(`the courier app's dialog is accepted`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK("the courier app is swiped down")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	recordingsKept(t, h)
}
