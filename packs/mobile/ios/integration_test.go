//go:build integration

package mobileios

import (
	"bytes"
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// harness runs the packs for a test; a failed one prints the screen it read
// and the end of its logs.
func harness(t *testing.T) *cloudtest.Harness {
	t.Helper()
	return harnessWith(t, nil)
}

// harnessWith is harness with the packs' settings. The tests share one simulator, kept booted
// from one test to the next (packs.mobile-ios.keep), as a run's worker has one for its scenarios:
// each scenario still has it to itself, and its app reset. Setting up a simulator takes minutes
// on a hosted runner, and it is done once. Like any kept simulator, it stays booted after the
// tests, for the next run.
func harnessWith(t *testing.T, config map[string]any) *cloudtest.Harness {
	t.Helper()
	cfg := map[string]any{Name: map[string]any{"keep": true}}
	maps.Copy(cfg, config)
	h := cloudtest.NewWith(t, cfg, appcore.Pack(), mobilecore.Pack(), Pack())
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
			// What the simulator's own log says, in the last minutes, of the app, of the document
			// picker and the files it shows, and of each tap XCTest made (where, and when), with
			// the errors of each of those processes; all of what iOS's resolver of documents
			// for apps and its store of document ids say; and what SpringBoard, iOS's service of
			// notifications and RunningBoard say of the app (its notifications, its priority).
			if r, ok := d.(*running); ok && r.dev != nil {
				out, err := exec.CommandContext(ctx, "xcrun", "simctl", "spawn", r.dev.udid, "log", "show", "--last", "2m", "--style", "compact", "--predicate", //nolint:gosec // the test's own simulator
					`subsystem BEGINSWITH "com.apple.DocumentManager" OR subsystem BEGINSWITH "com.apple.FileProvider" OR subsystem BEGINSWITH "example.parcels"`+
						` OR (process == "testmanagerd" AND eventMessage BEGINSWITH "Synthesizing event")`+
						` OR ((process == "Courier" OR process CONTAINS[c] "document" OR process CONTAINS[c] "fileprovider") AND messageType IN {16, 17})`+
						` OR process == "ResolverService" OR process == "revisiond"`+
						` OR ((process == "SpringBoard" OR process == "usernotificationsd" OR process == "runningboardd") AND eventMessage CONTAINS[c] "courier")`).CombinedOutput()
				lines := strings.Split(string(out), "\n")
				t.Logf("the simulator's log (%v):\n%s", err, strings.Join(lines[max(0, len(lines)-1500):], "\n"))
			}
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
// download of WebDriverAgent take longer than a scenario may.
// The scenario then leases the clone, and resets it as ever.
func warm(t *testing.T, h *cloudtest.Harness, device string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	p, err := poolFor(h.SC, device)
	if err != nil {
		t.Fatal(err)
	}
	d, err := p.lease(ctx)
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
	api := courierapi.Start(t)
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
	// A notification that never came: the failure shows what the simulator logged of the app's
	// notifications, the app posting the one that did come among them.
	_ = h.Fails(`within 1s the courier app shows a notification "PX-MOB-0000 delivered"`, `shows no notification "PX-MOB-0000 delivered"`)
	if !slices.ContainsFunc(h.Sink.Logs, func(l string) bool {
		return strings.Contains(l, "logged of its notifications") && strings.Contains(l, "notified PX-MOB-9401 delivered")
	}) {
		t.Errorf("the failure shows nothing the app logged of its notifications: %q", h.Sink.Logs)
	}
	h.OK(`the "Back to deliveries" button is tapped in the courier app`)
	h.OK(`the courier app does not show "PX-MOB-9401"`)
	// The pull refreshes the list: the app asks for the day's deliveries again.
	lists := api.Lists()
	h.OK("the courier app is swiped down")
	for deadline := time.Now().Add(5 * time.Second); api.Lists() == lists; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Error("the swipe down did not refresh the deliveries")
			break
		}
	}
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
		t.Fatalf("traces %v, videos %v; the scenario's logs:\n%s", traces, videos, strings.Join(h.Sink.Logs, "\n"))
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
	h := harnessWith(t, map[string]any{"mobile-core": map[string]any{"traces": "always", "videos": "always"}})
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

// A courier shares the van's location and drives east along Grimmaische Straße at 50 km/h: the
// app hears the phone move, with its speed and course, and reports it; the step waits for the
// phone to arrive.
func TestACourierSharesTheVanOnASimulator(t *testing.T) {
	device := simulator(t)
	api := courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := harness(t)
	warm(t, h, device)
	h.OK("the courier ios app with the following properties:", [][]string{
		{"app", courierBuild(t)}, {"device", device}, {"timezone", "Europe/Berlin"}, {"location", "51.3397, 12.3731"},
	})
	signIn(h)
	h.OK(`the courier app's dialog is dismissed`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK(`the "Share van location" switch is tapped in the courier app`)
	h.OK(`the courier app's dialog shows "use your location"`)
	h.OK(`the courier app's dialog is accepted`)
	h.OK("the courier app's phone travels at 50 km/h through:", [][]string{{"51.3397", "12.3731"}, {"51.3397", "12.3760"}})
	// Its course, where CoreLocation gives one: the simulators of older Xcodes report none.
	api.TheVanTravelled(t, [2]float64{51.3397, 12.3731}, [2]float64{51.3397, 12.3760}, 50/3.6, false)
}

// A courier delivers with the app in the background: the delivered notification's Next delivery
// action (in the menu a long press shows) opens the next parcel, and the notification of that
// one, tapped, today's deliveries.
func TestACourierFollowsNotificationsOnASimulator(t *testing.T) {
	device := simulator(t)
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := harness(t)
	warm(t, h, device)
	h.OK("the courier ios app with the following properties:", [][]string{
		{"app", courierBuild(t)}, {"device", device}, {"timezone", "Europe/Berlin"},
	})
	signIn(h)
	h.OK(`the courier app's dialog is accepted`)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Signed by" field in the courier app is filled with "Jonas Weber"`)
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	h.OK(`the "Confirm" button is tapped in the courier app`)
	h.OK(`the courier app shows "Delivered"`)
	h.OK("the courier app is sent to the background")
	h.OK(`the "Next delivery" action of the "PX-MOB-9401 delivered" notification is tapped in the courier app`)
	h.OK(`the courier app shows "Prager Str. 3, 04103 Leipzig"`)
	h.OK(`the "Signed by" field in the courier app is filled with "Lena Vogel"`)
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	h.OK(`the "Confirm" button is tapped in the courier app`)
	h.OK(`the courier app shows "Delivered"`)
	h.OK("the courier app is sent to the background")
	h.OK(`the "PX-MOB-9402 delivered" notification is tapped in the courier app`)
	h.OK(`the courier app shows "No deliveries left today."`)
}

// parcelScene is the parcels example's scene of a parcel on a porch, 60 cm long, its front corners
// in guards.
var parcelScene = filepath.Join("..", "..", "..", "examples", "parcels", "acceptance", "features", "courier-app-measure", "scenes", "parcel-on-porch.glb")

// A courier measures a parcel with the app's AR view, whose camera sees a scene: a tap on each
// of the parcel's front corners (on the guard over it, which lands on the corner), and the app
// measures them exactly 60 cm apart. A scene object the scene has not got is named with those it
// has; and a running app whose camera is given a scene starts again, to see it.
func TestACourierMeasuresAParcelOnASimulator(t *testing.T) {
	device := simulator(t)
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := harness(t)
	warm(t, h, device)
	scene, err := filepath.Abs(parcelScene)
	if err != nil {
		t.Fatal(err)
	}
	h.OK("the courier ios app with the following properties:", [][]string{
		{"app", courierBuild(t)}, {"device", device}, {"timezone", "Europe/Berlin"},
	})
	signIn(h)
	h.OK(`the courier app's dialog is dismissed`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK("the courier app's camera sees " + scene)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`) // started again, the courier still signed in
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Measure parcel" button is tapped in the courier app`)
	h.OK(`the courier app shows "Tap one end of the parcel, then the other"`)
	_ = h.Fails(`the "parcel" in the courier app's camera view is tapped`, `its objects are "front left corner", "front right corner"`)
	h.OK(`the "front left corner" in the courier app's camera view is tapped`)
	h.OK(`the "front right corner" in the courier app's camera view is tapped`)
	h.OK(`the courier app shows "60 cm"`)
}

// A courier attaches a document from the phone's files to a delivery: the recipient's leave to
// put the parcel in his garage, which the documents property put On My iPhone, picked in the
// system's document picker as a courier picks it.
func TestACourierAttachesADocumentOnASimulator(t *testing.T) {
	device := simulator(t)
	api := courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := harness(t)
	warm(t, h, device)
	pdf := courierapi.SafePlaceAuthorization(t)
	h.OK("the courier ios app with the following properties:", [][]string{
		{"app", courierBuild(t)}, {"device", device}, {"documents", pdf},
	})
	signIn(h)
	h.OK(`the courier app's dialog is dismissed`)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Attach document" button is tapped in the courier app`)
	h.OK(`the "Browse" button is tapped in the courier app`)
	h.OK(`the "On My iPhone" text is tapped in the courier app`)
	h.OK(`the "safe-place-PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the courier app shows "safe-place-PX-MOB-9401.pdf attached"`)
	want, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatal(err)
	}
	docs := api.Documents()
	if len(docs) != 1 || docs[0].Reference != "PX-MOB-9401" || docs[0].Name != "safe-place-PX-MOB-9401.pdf" ||
		docs[0].ContentType != "application/pdf" || !bytes.Equal(docs[0].Content, want) {
		t.Errorf("the service has %+v, want PX-MOB-9401's safe-place-PX-MOB-9401.pdf (application/pdf, %d bytes)", docs, len(want))
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	// The next scenario on the simulator finds nothing a scenario before put in its files.
	h.NewScenarioWithin(scenarioLimit)
	h.OK("the courier ios app with the following properties:", [][]string{{"app", courierBuild(t)}, {"device", device}})
	signIn(h)
	h.OK(`the courier app's dialog is dismissed`)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Attach document" button is tapped in the courier app`)
	h.OK(`the "Browse" button is tapped in the courier app`)
	h.OK(`the "On My iPhone" text is tapped in the courier app`)
	h.OK(`the courier app does not show "safe-place-PX-MOB-9401"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// The round reminder, a background task iOS runs now and then, runs now: the courier, in another
// app, is told how many deliveries are left today. The app is suspended by then, as iOS suspends an
// app in the background, and SpringBoard wakes it for the task. A task runs once for each time the
// app schedules it, and the app schedules the next as it runs: once the courier delivers one, it
// runs again, and tells of the one left. A name the app does not permit fails, naming those it does.
func TestACourierIsRemindedOfTheRoundOnASimulator(t *testing.T) {
	device := simulator(t)
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := harness(t)
	warm(t, h, device)
	h.OK("the courier ios app with the following properties:", [][]string{
		{"app", courierBuild(t)}, {"device", device}, {"timezone", "Europe/Berlin"},
	})
	signIn(h)
	h.OK(`the courier app's dialog is accepted`)
	h.OK(`the courier app shows "PX-MOB-9401"`)
	h.OK("the courier app is sent to the background")
	time.Sleep(15 * time.Second) // iOS suspends the app a few seconds into the background
	_ = h.Fails(`the "example.parcels.courier.round-reminders" background task is run in the courier app`, `its background tasks are "example.parcels.courier.round-reminder"`)
	h.OK(`the "example.parcels.courier.round-reminder" background task is run in the courier app`)
	h.OK(`within 30s the courier app shows a notification "2 deliveries left today"`)
	h.OK(`the courier app shows a notification "Next: PX-MOB-9401"`)
	h.OK("the courier app is brought back")
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Signed by" field in the courier app is filled with "Jonas Weber"`)
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	h.OK(`the "Confirm" button is tapped in the courier app`)
	h.OK(`the courier app shows "Delivered"`)
	h.OK("the courier app is sent to the background")
	h.OK(`the "example.parcels.courier.round-reminder" background task is run in the courier app`)
	h.OK(`within 30s the courier app shows a notification "1 delivery left today"`)
}
