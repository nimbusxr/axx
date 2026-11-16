//go:build integration

package mobileandroid

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// reportsAPK builds the web view tests' app: one web view that shows the page the iOS pack's
// tests show (../ios/testdata/webview/report.html), a courier's report of a delivery problem,
// longer than the screen. It is built with the SDK's own tools, as the test's emulator has them.
func reportsAPK(t *testing.T) string {
	t.Helper()
	return pageAPK(t, filepath.Join("..", "ios", "testdata", "webview", "report.html"))
}

// pageAPK builds that app with a page of the test's for its web view to show.
func pageAPK(t *testing.T, file string) string {
	t.Helper()
	home := os.Getenv("ANDROID_HOME")
	if home == "" {
		home = os.Getenv("ANDROID_SDK_ROOT")
	}
	tools, _ := filepath.Glob(filepath.Join(home, "build-tools", "*"))
	platforms, _ := filepath.Glob(filepath.Join(home, "platforms", "android-*", "android.jar"))
	if len(tools) == 0 || len(platforms) == 0 {
		t.Skip("the Android SDK has no build-tools or no platform to build the web view tests' app with")
	}
	slices.Sort(tools)
	slices.Sort(platforms)
	tool, android := tools[len(tools)-1], platforms[len(platforms)-1]
	work := t.TempDir()
	run := func(name string, args ...string) {
		t.Helper()
		if out, err := exec.Command(name, args...).CombinedOutput(); err != nil { //nolint:gosec // the test's own build
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
	}
	classes, assets := filepath.Join(work, "classes"), filepath.Join(work, "assets")
	for _, d := range []string{classes, assets} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	page, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "page.html"), page, 0o644); err != nil {
		t.Fatal(err)
	}
	run("javac", "--release", "8", "-classpath", android, "-d", classes, filepath.Join("testdata", "webview", "Reports.java"))
	run(filepath.Join(tool, "d8"), "--min-api", "26", "--lib", android, "--output", work, filepath.Join(classes, "example", "parcels", "reports", "Reports.class"))
	unsigned := filepath.Join(work, "unsigned.apk")
	run(filepath.Join(tool, "aapt2"), "link", "--debug-mode", "-I", android, "--manifest", filepath.Join("testdata", "webview", "AndroidManifest.xml"), "-A", assets, "-o", unsigned)
	// The app's code goes into the package aapt2 made.
	linked, err := os.ReadFile(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(linked), int64(len(linked)))
	if err != nil {
		t.Fatal(err)
	}
	var apk bytes.Buffer
	writer := zip.NewWriter(&apk)
	for _, f := range reader.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		w, err := writer.CreateHeader(&f.FileHeader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(w, r); err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
	}
	dex, err := os.ReadFile(filepath.Join(work, "classes.dex"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := writer.Create("classes.dex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(dex); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unsigned, apk.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	keystore, signed := filepath.Join(work, "debug.jks"), filepath.Join(work, "reports.apk")
	run("keytool", "-genkeypair", "-keystore", keystore, "-storepass", "android", "-keypass", "android", "-alias", "debug",
		"-dname", "CN=axx web view tests", "-keyalg", "RSA", "-validity", "3650")
	run(filepath.Join(tool, "apksigner"), "sign", "--ks", keystore, "--ks-pass", "pass:android", "--out", signed, unsigned)
	return signed
}

// A courier reports a delivery problem in a form the app shows in a web view, longer than the
// screen. The steps scroll to what is out of view themselves, by asking the page (axx's scroll
// service): a field found by its name is shown and filled, and so is a control only a selector
// finds, down the page and back up it, with nothing swiped. A number field is typed into, which
// brings the keyboard up: a control the page will not stay on with the keyboard up has the
// keyboard closed, and is scrolled to again. The feature reads as the iOS one does.
func TestACourierReportsAProblemInAWebViewOnAnEmulator(t *testing.T) {
	device := avd(t)
	app := reportsAPK(t)
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	logsOnFailure(t, h)
	h.Start(&core.Plan{})
	h.OK("the reports android app with the following properties:", [][]string{{"apk", app}, {"device", device}})
	h.OK("the reports app is launched")
	h.OK(`within 30s the reports app shows "Did you ring the bell?"`)
	// A question's radio button, inside the group the page makes of the question. (The first one
	// after the question's text is another question's when the page is scrolled so that the text
	// is just off the screen and the group still on it: Android lists only what the screen shows.)
	radio := func(question, answer string) string {
		return `the 'xpath=//*[@text="` + question + `"]//android.widget.RadioButton[@text="` + answer + `"]' element is tapped in the reports app`
	}
	// What the first screen shows.
	h.OK(radio("Did you ring the bell?", "Yes"))
	// A field further down, found by its name: the step scrolls until it shows, with no scroll step.
	h.OK(`the "Who did you speak to?" field in the reports app is filled with "Mara Lindqvist"`)
	// A control only a selector finds, back up the page and further down it: the step pages for it.
	h.OK(radio("Did you call the recipient?", "No"))
	h.OK(radio("Is the parcel back in the van?", "Yes"))
	// The scroll step takes a selector too.
	h.OK(`the 'xpath=//android.widget.EditText[@hint="What did the neighbour say?"]' field is scrolled into view in the reports app`)
	h.OK(`the "What did the neighbour say?" field in the reports app is filled with "Nobody home since Monday"`)
	// A number field, found by its label. Chrome presents it as a control with a range: its
	// label comes as its text, with no hint, and UiAutomator2 will not set its text, so the step
	// taps it and types, as a person does.
	h.OK(`the "How many parcels are still in the van?" field in the reports app is filled with '"3"'`)
	// The keyboard is up now, and the page keeps the field being typed in on the screen: asked
	// to show a control back up the page, it goes back to the field a moment later. The step
	// closes the keyboard, with one Back, which Android gives to the keyboard, and scrolls
	// again. The answer changes, so a tap that missed would show.
	h.OK(radio("Did you ring the bell?", "No"))
	// The filled number field is still found by its label.
	h.OK(`the "How many parcels are still in the van?" field is tapped in the reports app`)
	// A button at the page's end, found by its name.
	h.OK(`the "Send report" button is tapped in the reports app`)
	h.OK(`within 10s the reports app shows "Report sent"`)
	for _, answer := range []string{
		"Did you ring the bell? No", "Did you call the recipient? No", "What did the neighbour say? Nobody home since Monday",
		"Is the parcel back in the van? Yes", "Who did you speak to? Mara Lindqvist", "How many parcels are still in the van? 3",
	} {
		h.OK(`the reports app shows "` + answer + `"`)
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// A courier checks the van before a round, in a form the app shows in a web view: twenty-four
// questions, each answered Yes or No, four screens of them. A step taps an answer by a selector,
// which finds it wherever it is on the page, and has the page show it. Out of view, every "No"
// below the screen has the same place (the web view cuts it to the screen's edge), and so has
// every one above it: the step takes the one the selector found, by what the driver says of it,
// not by where it is, and the scroll service is told which of those alike it is. Taken by its
// place, the page showed another question's answer, and scrolled past the one asked for.
func TestACourierChecksTheVanInAWebViewOnAnEmulator(t *testing.T) {
	device := avd(t)
	app := pageAPK(t, filepath.Join("testdata", "webview", "check.html"))
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	logsOnFailure(t, h)
	h.Start(&core.Plan{})
	h.OK("the checks android app with the following properties:", [][]string{{"apk", app}, {"device", device}})
	h.OK("the checks app is launched")
	h.OK(`within 30s the checks app shows "Are the tyres in good condition?"`)
	radio := func(question, answer string) string {
		return `the 'xpath=//*[@text="` + question + `"]//android.widget.RadioButton[@text="` + answer + `"]' element is tapped in the checks app`
	}
	// What the first screen shows.
	h.OK(radio("Are the tyres in good condition?", "Yes"))
	// Below the screen, with five more questions after it, whose answers have its place.
	h.OK(radio("Does the horn work?", "No"))
	// Further down, where the page gives no answer an ID: six alike come before it.
	h.OK(radio("Is the first aid kit on board?", "No"))
	// Back up the page, past ten questions above the screen.
	h.OK(radio("Do all the lights work?", "Yes"))
	// The page's last question, and one five up from it, among those with no ID.
	h.OK(radio("Is there damage to report?", "Yes"))
	h.OK(radio("Is the label printer loaded?", "No"))
	h.OK(`the "Send check" button is tapped in the checks app`)
	h.OK(`within 10s the checks app shows "Check sent"`)
	// What the page took: each answer is its own question's.
	for _, answer := range []string{
		"Are the tyres in good condition? Yes", "Do all the lights work? Yes", "Does the horn work? No",
		"Is the label printer loaded? No", "Is the first aid kit on board? No", "Is there damage to report? Yes",
	} {
		h.OK(`the checks app shows "` + answer + `"`)
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
