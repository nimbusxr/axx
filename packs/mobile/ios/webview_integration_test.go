//go:build integration

package mobileios

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// reportsApp builds the web view tests' app for the simulator: one web view that shows
// testdata/webview/report.html, a courier's report of a delivery problem, as a web page an app
// shows. Its questions run past the first screen, as a form's do.
func reportsApp(t *testing.T) string {
	t.Helper()
	app := filepath.Join(t.TempDir(), "Reports.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Info.plist", "report.html"} {
		b, err := os.ReadFile(filepath.Join("testdata", "webview", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	arch := "arm64"
	if out, err := exec.Command("uname", "-m").Output(); err == nil && string(out) == "x86_64\n" {
		arch = "x86_64"
	}
	for _, c := range [][]string{
		{
			"xcrun", "-sdk", "iphonesimulator", "swiftc", "-parse-as-library", "-target", arch + "-apple-ios17.0-simulator", "-O",
			filepath.Join("testdata", "webview", "main.swift"), "-o", filepath.Join(app, "Reports"),
		},
		{"codesign", "-s", "-", "--force", app},
	} {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil { //nolint:gosec // the test's own build
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
	return app
}

// A courier reports a delivery problem in a form the app shows in a web view, longer than the
// screen. The steps reach what is further down the page themselves: a field found by its name is
// scrolled to and filled, and a radio button past the first screen is tapped where it is. iOS's
// test framework cannot place its own tap on such a radio button (it scrolls to it and taps
// nothing, and WebDriverAgent says it tapped), with the keyboard up or not. The scroll step
// takes a selector, as the other steps do. A control under the keyboard is scrolled above it,
// and where no scroll frees it, the keyboard is closed with the bar over it.
func TestACourierReportsAProblemInAWebViewOnASimulator(t *testing.T) {
	device := simulator(t)
	app := reportsApp(t)
	h := harness(t)
	warm(t, h, device)
	h.OK("the reports ios app with the following properties:", [][]string{{"app", app}, {"device", device}})
	h.OK("the reports app is launched")
	h.OK(`within 30s the reports app shows "Did you ring the bell?"`)
	radio := func(question, answer string) string {
		return `the 'xpath=(//*[@label="` + question + `"]/following::*[@label="` + answer + `"])[1]' element is tapped in the reports app`
	}
	// On the first screen, the framework's own tap lands.
	h.OK(radio("Did you ring the bell?", "Yes"))
	// A field further down, found by its name: no scroll step.
	h.OK(`the "What did the neighbour say?" field in the reports app is filled with "Nobody home since Monday"`)
	// The next field is under the keyboard: the step scrolls it above the keyboard and types. It
	// does not close the keyboard to reach it, which would press return, and send the form.
	h.OK(`the "Anything the depot should know?" field in the reports app is filled with "Ring twice"`)
	// Radio buttons past the first screen, right after typing, the keyboard still up.
	h.OK(radio("Can the parcel go to a neighbour?", "No"))
	h.OK(radio("Is the parcel back in the van?", "Yes"))
	// The scroll step takes a selector, up the page and back down it.
	h.OK(`the 'xpath=//XCUIElementTypeStaticText[@label="Did you call the recipient?"]' text is scrolled into view in the reports app`)
	h.OK(radio("Did you call the recipient?", "No"))
	h.OK(`the 'xpath=//XCUIElementTypeTextField[@label="What number can the depot call?"]' field is scrolled into view in the reports app`)
	// The phone number is typed last, on the number pad, which has no key that closes it. (A
	// number that is text is written in both quotes: ADR 0014.)
	h.OK(`the "What number can the depot call?" field in the reports app is filled with '"03415550142"'`)
	// A finger on the field brings the pad up, over the button at the page's end. The page is at
	// its end and scrolls no further, so the step closes the pad to reach the button: with the
	// Done of the bar over it, the only thing that closes a number pad.
	h.OK(`the "What number can the depot call?" field is tapped in the reports app`)
	h.OK(`the "Send report" button is tapped in the reports app`)
	h.OK(`within 10s the reports app shows "Report sent"`)
	for _, answer := range []string{
		"Did you ring the bell? Yes", "Did you call the recipient? No", "What did the neighbour say? Nobody home since Monday",
		"Can the parcel go to a neighbour? No", "Is the parcel back in the van? Yes", "Anything the depot should know? Ring twice",
		"What number can the depot call? 03415550142",
	} {
		h.OK(`the reports app shows "` + answer + `"`)
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
