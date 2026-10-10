//go:build integration

package mobileandroid

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/courierapi"
)

// The integration tests run the parcels example's couriers' app on an
// emulator axx starts: AXX_ANDROID_AVD, or parcels-pixel
// (../../../examples/parcels/courier/android/create-emulator.sh), with the
// app built (./gradlew assembleDebug there).

var courierAPK = filepath.Join("..", "..", "..", "examples", "parcels", "courier", "android", "app", "build", "outputs", "apk", "debug", "app-debug.apk")

func avd(t *testing.T) string {
	t.Helper()
	if _, err := findSDK(); err != nil {
		t.Skipf("no Android SDK: %v", err)
	}
	if name := os.Getenv("AXX_ANDROID_AVD"); name != "" {
		return name
	}
	return "parcels-pixel"
}

func apk(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(courierAPK)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the couriers' app is not built: run ./gradlew assembleDebug in examples/parcels/courier/android (%v)", err)
	}
	return p
}

// logsOnFailure prints, when the test fails, the end of the logs the pack keeps: the
// UiAutomator2 server's and the emulator's, which say why a device stopped answering.
func logsOnFailure(t *testing.T, h *cloudtest.Harness) {
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
		// What crashed on the devices: a process that died, the server's among them.
		s, err := findSDK()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		serials, _ := s.devices(ctx)
		for _, serial := range serials {
			out, _ := s.run(ctx, serial, "logcat", "-d", "-b", "crash")
			t.Logf("%s's crash log:\n%s", serial, out)
		}
	})
}

// near checks that the scenario's device was put at a latitude and longitude
// (emu geo fix), as its resets say.
func near(t *testing.T, h *cloudtest.Harness, lat, lon float64) {
	t.Helper()
	want := "location " + strconv.FormatFloat(lat, 'f', -1, 64) + ", " + strconv.FormatFloat(lon, 'f', -1, 64)
	_ = mobilecore.OnDevice(h.SC, "courier", func(_ context.Context, d mobilecore.Device) error {
		if resets, _ := d.Describe()["resets"].([]string); !slices.Contains(resets, want) {
			t.Errorf("the device's resets are %v, want %q among them", resets, want)
		}
		return nil
	})
}

func signIn(h *cloudtest.Harness) {
	h.OK(`the courier app is launched`)
	h.OK(`the "Sign in" button is disabled in the courier app`)
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with "${env:COURIER_PIN}"`)
	h.OK(`the "Sign in" button is tapped in the courier app`)
}

// A courier delivers a parcel on an emulator, with every step of the packs;
// the next scenario, on the same emulator, starts signed out, without the
// permission the first had, and Android asks for it. One emulator for both,
// as a run's worker has.
func TestCouriersOnAnEmulator(t *testing.T) {
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	logsOnFailure(t, h)
	h.OK("the courier android app with the following properties:", [][]string{
		{"apk", apk(t)},
		{"device", avd(t)},
		{"permissions", "POST_NOTIFICATIONS"},
		{"timezone", "Europe/Berlin"},
		{"locale", "en-GB"},
		{"location", "51.3397, 12.3731"},
		{"host ports", "8400"}, // the courier app calls localhost:8400, the stand-in on this machine
	})
	signIn(h)
	near(t, h, 51.3397, 12.3731)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Mark delivered" button is disabled in the courier app`)
	h.OK(`the "Signed by" field in the courier app is filled with "Jonas Weber"`)
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	h.OK(`the courier app shows "Mark PX-MOB-9401 delivered?"`)
	h.OK(`the "Confirm" button is tapped in the courier app`)
	h.OK(`the courier app shows "Delivered"`)
	h.OK(`the courier app shows a notification "PX-MOB-9401 delivered"`)
	h.OK(`the "Back to deliveries" button is tapped in the courier app`)
	h.OK(`the courier app does not show "PX-MOB-9401"`)
	h.OK("the courier app is swiped down")
	h.OK(`the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9402" link`)
	h.OK(`the courier app shows "Prager Str. 3, 04103 Leipzig"`)
	h.OK("the courier app's back button is pressed")
	h.OK(`the courier app shows "Today's deliveries"`)
	h.OK("the courier app is sent to the background")
	h.OK("the courier app is brought back")
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

	h.NewScenario()
	// What the app reached as localhost went with the scenario before: this
	// one names its own.
	h.OK("the courier android app with the following properties:", [][]string{{"apk", apk(t)}, {"device", avd(t)}, {"host ports", "8400"}})
	h.OK(`the courier app is launched`)
	// Not where the scenario before left it: where an emulator starts.
	near(t, h, defaultLocation[0], defaultLocation[1])
	h.OK(`the courier app shows "Sign in"`)
	h.OK(`the "Courier ID" field in the courier app has the value ""`)
	signIn(h)
	h.OK(`the courier app's dialog shows "send you notifications"`)
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
	for _, want := range []string{"chpl", "* the courier android app with the following properties:"} {
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
func TestRecordingsOnAnEmulator(t *testing.T) {
	courierapi.Start(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cloudtest.NewWith(t, map[string]any{"mobile-core": map[string]any{"traces": "always", "videos": "always"}}, appcore.Pack(), mobilecore.Pack(), Pack())
	logsOnFailure(t, h)
	h.OK("the courier android app with the following properties:", [][]string{
		{"apk", apk(t)}, {"device", avd(t)}, {"permissions", "POST_NOTIFICATIONS"}, {"host ports", "8400"},
	})
	signIn(h)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK("the courier app is swiped down")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	recordingsKept(t, h)
}
