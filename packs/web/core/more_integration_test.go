//go:build integration

package webcore

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// Everything a shop does on a parcel's page, in every engine: menus that
// open on hover, tabs, a menu, dialogs, keys, a frame, downloads, and a link
// that opens a new browser tab.
func TestAParcelPageInEveryEngine(t *testing.T) {
	for _, engine := range bundled {
		t.Run(engine, func(t *testing.T) {
			h, app := newHarness(t, engine)
			h.File("labels/PX-4101.zpl", zplLabel("PX-4101", "Anna Weber"))
			h.OK("the portal web app with the following properties:", app)
			h.OK(`the "/parcel.html" page is opened`)

			// What opens on hover.
			h.OK(`the page does not show "Sign out"`)
			h.OK(`the pointer is moved over "Account"`)
			h.OK(`the "Sign out" link is clicked`)
			h.OK(`the "/signout" page is shown`)
			h.OK(`the browser's back button is clicked`)
			h.OK(`the "/parcel.html" page is shown`)
			h.OK(`the pointer is moved over "About Express"`)
			h.OK(`the page shows "Delivered the next working day within Germany"`)

			// Tabs on the page, and keys.
			h.OK(`the "History" tab is clicked`)
			h.OK("the page shows a table row where:", [][]string{{"Date", "2026-09-25"}, {"Event", "In transit"}})
			h.OK(`the page does not show "Recipient: Anna Weber"`)
			h.OK(`the "Details" tab is clicked`)
			h.OK(`the page shows "Recipient: Anna Weber"`)
			h.OK(`the "Find a parcel" field is filled with "PX-4101"`)
			h.OK(`the Enter key is pressed in the "Find a parcel" field`)
			h.OK(`the page shows "Found PX-4101"`)
			h.OK(`the Escape key is pressed`)
			h.OK(`the "Find a parcel" field has the value ""`)

			// A menu, and its dialogs.
			h.OK(`the "Actions" button is clicked`)
			h.OK(`the "Cancel the parcel" menu item is clicked`)
			h.OK(`the dialog shows "Cancel parcel PX-4101?"`)
			h.OK(`the dialog is dismissed`)
			h.OK(`the page shows "Parcel PX-4101 kept"`)
			h.OK(`the "Actions" button is clicked`)
			h.OK(`the "Report a problem" menu item is clicked`)
			h.OK(`the dialog is answered with "The parcel arrived damaged"`)
			h.OK(`the page shows "Problem reported: The parcel arrived damaged"`)
			h.OK(`the "Actions" button is clicked`)
			h.OK(`the "Opening hours" menu item is clicked`)
			h.OK(`the dialog shows "8:00 to 18:00"`)
			h.OK(`the dialog is accepted`)
			h.OK(`the page shows "Opening hours shown"`)
			h.OK(`the "Actions" button is clicked`)
			h.OK(`the "Cancel the parcel" menu item is clicked`)
			h.OK(`the dialog is accepted`)
			h.OK(`the page shows "Parcel PX-4101 cancelled"`)
			h.OK(`the "Actions" button is clicked`)
			h.OK(`the "Notify me of changes" menu item has the aria-checked attribute "false"`)
			h.OK(`the "Notify me of changes" menu item is clicked`)
			h.OK(`the page shows "Changes will be sent to you"`)
			h.OK(`the "Notify me of changes" menu item has the aria-checked attribute "true"`)

			// A frame: fields, buttons, text and tables inside it.
			h.OK(`"Späti Torstr. 102" is chosen in the "Pickup point" field`)
			h.OK(`the "Choose this pickup point" button is clicked`)
			h.OK(`the page shows "Pickup point: Späti Torstr. 102"`)
			h.OK("the page shows a table row where:", [][]string{{"Pickup point", "Kiosk Rosenthaler Str. 40"}, {"Opening hours", "7:00 to 22:00"}})

			// Downloads.
			h.OK(`the "Download the label" link is clicked`)
			h.OK(`the "PX-4101-label.zpl" file is downloaded`)
			h.OK(`the downloaded "PX-4101-label.zpl" file is identical to the labels/PX-4101.zpl file`)
			h.OK(`the "Export as CSV" link is clicked`)
			h.OK(`the downloaded "parcels.csv" file contains "PX-4101,Anna Weber,IN_TRANSIT"`)
			h.OK(`the downloaded "parcels.csv" file has a row where:`, [][]string{{"Parcel", "PX-4101"}, {"Status", "IN_TRANSIT"}})
			h.OK(`the "Download the customs invoice" link is clicked`)
			h.OK(`the downloaded "INV-6001.pdf" file contains "Parcel: PX-CUS-6001"`)

			// A new browser tab, followed and closed.
			h.OK(`the "Track on the carrier's site" link is clicked`)
			h.OK(`the "/track.html?ref=PX-4101" page is shown`)
			h.OK(`the page shows "Parcel PX-4101: in transit to the depot"`)
			h.OK(`the browser tab is closed`)
			h.OK(`the "/parcel.html" page is shown`)
			h.OK(`the page is reloaded`)
			h.OK(`the page shows "Recipient: Anna Weber"`)

			if err := h.End("passed"); err != nil {
				t.Fatal(err)
			}
			if kept, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "downloads", "*", "*")); len(kept) != 0 {
				t.Errorf("a passing scenario kept its downloads: %v", kept)
			}
		})
	}
}

// While a dialog waits for an answer, the page does too: every other step
// says so.
func TestADialogWaitsForAnAnswer(t *testing.T) {
	defer func(d time.Duration) { actionTimeout = d }(actionTimeout)
	actionTimeout = 2 * time.Second
	h, app := newHarness(t, "chromium")
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/parcel.html" page is opened`)
	_ = h.Fails(`the dialog is accepted`, "No dialog is open")
	_ = h.Fails(`within 1s the dialog shows "Cancel"`, "No dialog is open")
	h.OK(`the "Actions" button is clicked`)
	h.OK(`the "Cancel the parcel" menu item is clicked`)
	want := `The page shows a dialog: "Cancel parcel PX-4101? The shop gets its postage back."; answer it first`
	_ = h.Fails(`the page shows "Parcel PX-4101 kept"`, want)
	_ = h.Fails(`the "Details" tab is clicked`, want)
	_ = h.Fails(`the dialog shows "Delete"`, `The dialog does not show "Delete"`)
	_ = h.Fails(`the dialog is answered with "no"`, `The dialog asks no question (it is a confirm`)
	h.OK(`the dialog is dismissed`)
	h.OK(`the page shows "Parcel PX-4101 kept"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadsAndTabsSayWhatThereIs(t *testing.T) {
	defer func(d time.Duration) { actionTimeout = d }(actionTimeout)
	actionTimeout = 2 * time.Second
	h, app := newHarness(t, "chromium")
	h.File("labels/PX-4101.zpl", "^XA^XZ\n")
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/parcel.html" page is opened`)
	_ = h.Fails(`within 1s the "PX-4101-label.zpl" file is downloaded`, `The browser downloaded no "PX-4101-label.zpl" file; no files downloaded`)
	h.OK(`the "Download the label" link is clicked`)
	_ = h.Fails(`within 1s the "label.pdf" file is downloaded`, `The browser downloaded no "label.pdf" file; 1 files downloaded:`+"\n  "+`"PX-4101-label.zpl"`)
	_ = h.Fails(`the downloaded "PX-4101-label.zpl" file contains "PX-9999"`, `The downloaded "PX-4101-label.zpl" file does not contain "PX-9999"`)
	_ = h.Fails(`the downloaded "PX-4101-label.zpl" file is identical to the labels/PX-4101.zpl file`, "differs from the labels/PX-4101.zpl file")
	_ = h.Fails(`the downloaded "PX-4101-label.zpl" file has a row where:`, `its table has no "Parcel" column`, [][]string{{"Parcel", "PX-4101"}})
	h.OK(`the "Export as CSV" link is clicked`)
	_ = h.Fails(`the downloaded "parcels.csv" file has a row where:`, `The downloaded "parcels.csv" file has no row where Parcel=PX-9999`, [][]string{{"Parcel", "PX-9999"}})
	_ = h.Fails(`the "Save" tab is clicked`, `No tab named "Save" on the page; 2 tabs:`)
	_ = h.Fails(`the "Save" menu item is clicked`, `No menu item named "Save" on the page; no menu items`)
	_ = h.Fails(`the pointer is moved over "Help"`, `No thing named "Help" on the page;`)
	_ = h.Fails(`the Entr key is pressed`, "cannot press the Entr key")
	h.OK(`the browser tab is closed`)
	_ = h.Fails(`the page shows "Parcel"`, "every browser tab of the portal web app is closed")
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	if kept, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "downloads", "*", "PX-4101-label.zpl")); len(kept) != 1 {
		t.Errorf("the failed scenario's downloads: %v", kept)
	}
}

// The web app's properties decide how the browser presents itself.
func TestTheBrowserPresentsItselfAsTheAppSays(t *testing.T) {
	h, app := newHarness(t, "webkit")
	h.OK("the phone web app with the following properties:", append(app,
		[]string{"device", "iPhone 15"}, []string{"locale", "de-DE"}, []string{"timezone", "Europe/Berlin"}))
	h.OK(`the "/browser.html" page is opened`)
	h.OK(`the page shows "Width: 393"`)
	h.OK(`the page shows "Device: iPhone"`)
	h.OK(`the page shows "Language: de-DE"`)
	h.OK(`the page shows "Time zone: Europe/Berlin"`)
	h.OK(`the page shows "Registered: 25.09.2026, 14:03"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	h, app = newHarness(t, "chromium")
	h.OK("the desk web app with the following properties:", append(app, []string{"viewport", "1024x600"}, []string{"timezone", "America/New_York"}))
	h.OK(`the "/browser.html" page is opened`)
	h.OK(`the page shows "Width: 1024, height: 600"`)
	h.OK(`the page shows "Device: computer"`)
	h.OK(`the page shows "Registered: Sep 25, 2026, 8:03"`)
	h.OK("the typo web app with the following properties:", append(app, []string{"device", "iPhone 99"}))
	_ = h.Fails(`the "/browser.html" page of the typo web app is opened`, `the typo web app's device "iPhone 99" is not one Playwright knows;`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// Watching shows the browser in a window, and nothing more: a failed
// scenario ends as it fails.
func TestWatchingShowsTheBrowser(t *testing.T) {
	needsScreen(t)
	h, app := newConfigured(t, "chromium", &Config{Watch: true, Slowdown: "20ms"})
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/browser.html" page is opened`)
	h.OK(`the page shows "Browser: on a screen"`)
	// The browser asks for the site's icon, which the site has.
	h.OK(`the page has no script errors`)
	_ = h.Fails(`within 1s the page shows "Browser: headless"`, `The page does not show "Browser: headless"`)
	start := time.Now()
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("the watched scenario paused where it failed: it ended after %s", waited)
	}
}

// A scenario that fails pauses where it failed, in Playwright's Inspector,
// with its browser shown, until it is resumed or its page closed.
func TestFailedScenariosPauseWhenAskedTo(t *testing.T) {
	needsScreen(t)
	h, app := newConfigured(t, "chromium", &Config{PauseOnFailure: true})
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/browser.html" page is opened`)
	h.OK(`the page shows "Browser: on a screen"`)
	_ = h.Fails(`within 1s the page shows "Browser: headless"`, `The page does not show "Browser: headless"`)

	// Someone looks at the page, and closes it.
	s, err := current(h.SC)
	if err != nil {
		t.Fatal(err)
	}
	pg, err := s.page()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(2 * time.Second)
		_ = pg.Close()
	}()
	start := time.Now()
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited < 2*time.Second {
		t.Errorf("the failed scenario did not pause: it ended after %s", waited)
	}
}

// What a scenario keeps: traces and videos as the settings say.
func TestWhatScenariosKeep(t *testing.T) {
	run := func(cfg *Config, status string) (traces, videos []string) {
		h, app := newConfigured(t, "chromium", cfg)
		h.OK("the portal web app with the following properties:", app)
		h.OK(`the "/parcel.html" page is opened`)
		h.OK(`the "Track on the carrier's site" link is clicked`)
		h.OK(`the page shows "in transit"`)
		if err := h.End(status); err != nil {
			t.Fatal(err)
		}
		traces, _ = filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "traces", "*.zip"))
		videos, _ = filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "videos", "*.webm"))
		return traces, videos
	}
	if traces, videos := run(&Config{Traces: "always"}, "passed"); len(traces) != 1 || len(videos) != 0 {
		t.Errorf("traces always, passed: %v %v", traces, videos)
	}
	if traces, videos := run(&Config{Traces: "never", Videos: "failed"}, "failed"); len(traces) != 0 || len(videos) != 2 {
		t.Errorf("videos failed, failed: %v %v", traces, videos)
	}
	if traces, videos := run(&Config{Videos: "failed"}, "passed"); len(traces) != 0 || len(videos) != 0 {
		t.Errorf("videos failed, passed: %v %v", traces, videos)
	}
	for _, v := range func() []string { _, v := run(&Config{Videos: "always"}, "passed"); return v }() {
		if st, err := os.Stat(v); err != nil || st.Size() == 0 {
			t.Errorf("empty video %s", v)
		}
	}
}

// A trace shows the scenario step by step: each Gherkin step is a group, at
// its line of the feature file, with its actions under it.
func TestTracesShowTheSteps(t *testing.T) {
	h, app := newConfigured(t, "chromium", &Config{Traces: "always"})
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	h.OK(`the "Weight (grams)" field is filled with "1200"`)
	h.OK(`the "Get a quote" button is clicked`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "traces", "*.zip"))
	if len(traces) != 1 {
		t.Fatalf("traces: %v", traces)
	}
	zr, err := zip.OpenReader(traces[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	type event struct {
		Type, CallID, Title, Method, ParentID string
		Stack                                 []struct {
			File string
			Line int
		}
	}
	groups := map[string]event{} // by call id
	var actions []event
	for _, f := range zr.File {
		if f.Name != "trace.trace" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(rc)
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.Type != "before" {
				continue
			}
			switch e.Method {
			case "tracingGroup":
				groups[e.CallID] = e
			case "fill", "click":
				actions = append(actions, e)
			}
		}
		_ = rc.Close()
	}
	feature := filepath.Join(h.Dir, "features", "test.feature")
	want := map[string]struct {
		step string
		line int
	}{"fill": {`* the "Weight (grams)" field is filled with "1200"`, 4}, "click": {`* the "Get a quote" button is clicked`, 5}}
	if len(actions) != 2 {
		t.Fatalf("actions: %+v", actions)
	}
	for _, a := range actions {
		g, ok := groups[a.ParentID]
		w := want[a.Method]
		if !ok || g.Title != w.step || len(g.Stack) != 1 || g.Stack[0].File != feature || g.Stack[0].Line != w.line {
			t.Errorf("%s is under %+v, want the step %q at line %d", a.Method, g, w.step, w.line)
		}
	}
}

// needsScreen skips a test that shows browser windows where there is no
// screen for them.
func needsScreen(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no screen to show a browser window on (run the tests under xvfb-run)")
	}
}

// resumeWhenPaused waits for the session's pause, resumes it, and says
// what the Inspector showed.
func resumeWhenPaused(t *testing.T, h *cloudtest.Harness) <-chan *playwright.PausedDetail {
	t.Helper()
	paused := make(chan *playwright.PausedDetail, 1)
	go func() {
		defer close(paused)
		for range 200 {
			time.Sleep(50 * time.Millisecond)
			s, err := current(h.SC)
			if err != nil {
				continue
			}
			d, err := s.ctx.Debugger()
			if err != nil {
				continue
			}
			if pd, err := d.PausedDetails(); err == nil && pd != nil {
				paused <- pd
				_ = d.Resume()
				return
			}
		}
	}()
	return paused
}

// A run pauses before the steps it is asked to, in the Inspector: before a
// step on the page, and before a step that opens the first page.
func TestPausingBeforeSteps(t *testing.T) {
	needsScreen(t)
	h, app := newHarness(t, "chromium")
	h.Suite.PauseAt(map[string][]int{"features/test.feature": {3, 5}})
	h.OK("the portal web app with the following properties:", app) // line 2
	paused := resumeWhenPaused(t, h)
	h.OK(`the "/quote.html" page is opened`) // line 3: paused as it opens the page
	if pd := <-paused; pd == nil {
		t.Fatal("the run did not pause before the step that opens the page")
	}
	h.OK(`the "Weight (grams)" field is filled with "1200"`) // line 4
	paused = resumeWhenPaused(t, h)
	h.OK(`the "Get a quote" button is clicked`) // line 5: paused before
	pd := <-paused
	if pd == nil {
		t.Fatal("the run did not pause before the step")
	}
	// The Inspector shows the feature file, at the step.
	if feature := filepath.Join(h.Dir, "features", "test.feature"); pd.Location == nil || pd.Location.File != feature || pd.Location.Line == nil || *pd.Location.Line != 5 {
		t.Errorf("paused at %+v, want %s:5", pd.Location, feature)
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// The Inspector shows a paused scenario's feature file as Gherkin, and names
// the element picked, and those of its call log, the way steps do (the
// patched Inspector page, sent what the driver sends it).
func TestTheInspectorSpeaksGherkin(t *testing.T) {
	h, _ := newHarness(t, "chromium")
	dir, err := driverDir(h.Suite)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(filepath.Join(dir, "package", "lib", "vite", "recorder"))))
	defer srv.Close()
	pw, err := playwrightFor(h.Suite)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pw.Chromium.Launch()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	pg, err := b.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.AddInitScript(playwright.Script{Content: playwright.String(`window.sendCommand = async () => {};`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Goto(srv.URL + "/index.html"); err != nil {
		t.Fatal(err)
	}
	const feature = "Feature: Shop portal\n\n  Scenario: A shop gets a quote\n    When the \"/quote\" page is opened\n    And the \"Register\" button is clicked\n"
	dispatch := func(method string, params any) {
		t.Helper()
		if _, err := pg.Evaluate(`([method, params]) => window.dispatch({ method, params })`, []any{method, params}); err != nil {
			t.Fatal(err)
		}
	}
	dispatch("sourcesUpdated", map[string]any{"sources": []any{map[string]any{
		"id": "/work/features/shop-portal.feature", "label": "shop-portal.feature", "isRecorded": false,
		"text": feature, "language": "gherkin", "highlight": []any{map[string]any{"line": 5, "type": "paused"}}, "revealLine": 5,
	}}})
	dispatch("sourceRevealRequested", map[string]any{"sourceId": "/work/features/shop-portal.feature"})
	dispatch("callLogsUpdated", map[string]any{"callLogs": []any{map[string]any{
		"id": "call@1", "title": "Click", "messages": []any{}, "status": "paused",
		"params": map[string]any{"selector": `internal:role=link[name="Track a parcel"s]`},
	}}})
	dispatch("elementPicked", map[string]any{"userGesture": true, "elementInfo": map[string]any{
		"selector": `internal:label="Weight (grams)"s`, "ariaSnapshot": "",
	}})
	var keywords []string
	ok, err := waitUntil(h.SC, 5*time.Second, func() (bool, error) {
		v, err := pg.Evaluate(`() => [...document.querySelectorAll(".cm-keyword")].map((e) => e.textContent.trim())`)
		if err != nil {
			return false, err
		}
		keywords = nil
		for _, k := range v.([]any) {
			keywords = append(keywords, k.(string))
		}
		return len(keywords) >= 4, nil
	})
	if err != nil || !ok || !slices.Contains(keywords, "Feature:") || !slices.Contains(keywords, "When") {
		t.Errorf("the feature file's keywords: %q %v", keywords, err)
	}
	var editors any
	ok, err = waitUntil(h.SC, 5*time.Second, func() (bool, error) {
		editors, err = pg.Evaluate(`() => [...document.querySelectorAll(".CodeMirror")].map((e) => e.CodeMirror.getValue())`)
		return err == nil && slices.Contains(editors.([]any), any(`the "Weight (grams)" field`)), err
	})
	if err != nil || !ok {
		t.Errorf("the picked element: %q %v", editors, err)
	}
	if err := pg.GetByText("Log", playwright.PageGetByTextOptions{Exact: playwright.Bool(true)}).Click(); err != nil {
		t.Fatal(err)
	}
	text, err := pg.Locator("body").InnerText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `the "Track a parcel" link`) || strings.Contains(text, `page.the`) {
		t.Errorf("the call log does not name the link as steps do:\n%s", text)
	}
}

// The trace viewer shows a scenario's steps, names its elements the way the
// steps do, and shows its feature file, which the trace keeps, as Gherkin.
func TestTheTraceViewerSpeaksGherkin(t *testing.T) {
	h, app := newConfigured(t, "chromium", &Config{Traces: "always"})
	// The harness's scenario is at line 1 of features/test.feature, its steps below.
	feature := "Scenario: A shop gets a quote\n  * the portal web app with the following properties:\n  * the \"/quote.html\" page is opened\n" +
		"  * the \"Weight (grams)\" field is filled with \"1200\"\n  * the \"Get a quote\" button is clicked\n"
	if err := os.MkdirAll(filepath.Join(h.Dir, "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.Dir, "features", "test.feature"), []byte(feature), 0o644); err != nil {
		t.Fatal(err)
	}
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	h.OK(`the "Weight (grams)" field is filled with "1200"`)
	h.OK(`the "Get a quote" button is clicked`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "traces", "*.zip"))
	if len(traces) != 1 {
		t.Fatalf("traces: %v", traces)
	}
	// The trace keeps the feature file.
	zr, err := zip.OpenReader(traces[0])
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "resources/src@") {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			kept = kept || string(b) == feature
		}
	}
	_ = zr.Close()
	if !kept {
		t.Error("the trace does not keep the feature file")
	}

	dir, err := driverDir(h.Suite)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(filepath.Join(dir, "package", "lib", "vite", "traceViewer"))))
	mux.HandleFunc("/trace.zip", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, traces[0]) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	pw, err := playwrightFor(h.Suite)
	if err != nil {
		t.Fatal(err)
	}
	b, err := pw.Chromium.Launch()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	pg, err := b.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Goto(srv.URL + "/index.html?trace=" + srv.URL + "/trace.zip"); err != nil {
		t.Fatal(err)
	}
	step := pg.GetByText(`* the "Get a quote" button is clicked`, playwright.PageGetByTextOptions{Exact: playwright.Bool(true)})
	if err := step.WaitFor(); err != nil {
		t.Fatal(err)
	}
	actions, err := pg.Locator("body").InnerText()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`* the "/quote.html" page is opened`, `* the "Weight (grams)" field is filled with "1200"`} {
		if !strings.Contains(actions, want) {
			t.Errorf("the trace viewer lacks the step %q:\n%s", want, actions)
		}
	}
	if strings.Contains(actions, "getByRole(") || strings.Contains(actions, "getByLabel(") {
		t.Errorf("the trace viewer names elements in JavaScript:\n%s", actions)
	}
	// The step's source: its feature file, as Gherkin.
	if err := step.Click(); err != nil {
		t.Fatal(err)
	}
	if err := pg.GetByRole("tab", playwright.PageGetByRoleOptions{Name: "Source"}).Click(); err != nil {
		t.Fatal(err)
	}
	var keywords []string
	ok, err := waitUntil(h.SC, 5*time.Second, func() (bool, error) {
		v, err := pg.Evaluate(`() => [...document.querySelectorAll(".cm-keyword")].map((e) => e.textContent.trim())`)
		if err != nil {
			return false, err
		}
		keywords = nil
		for _, k := range v.([]any) {
			keywords = append(keywords, k.(string))
		}
		return slices.Contains(keywords, "Scenario:"), nil
	})
	if err != nil || !ok {
		t.Errorf("the feature file's keywords: %q %v", keywords, err)
	}
}

// While a scenario is paused, an IDE highlights on the page what a step
// names, runs steps in the scenario and reads the steps recorded, at the
// address the run announces.
func TestAPausedScenarioTakesAnIDEsRequests(t *testing.T) {
	needsScreen(t)
	h, app := newHarness(t, "chromium")
	h.Suite.PauseAt(map[string][]int{"features/test.feature": {4}})
	h.OK("the portal web app with the following properties:", app) // line 2
	h.OK(`the "/quote.html" page is opened`)                       // line 3
	type answer struct {
		status int
		body   string
	}
	answers := make(chan []answer, 1)
	go func() {
		var got []answer
		defer func() { answers <- got }()
		var url string
		for range 200 {
			time.Sleep(50 * time.Millisecond)
			for _, l := range h.Sink.Lines() {
				if u, ok := strings.CutPrefix(l, "[AXX-IDE] paused url="); ok {
					url, _, _ = strings.Cut(u, " ")
				}
			}
			if url != "" {
				break
			}
		}
		if url == "" {
			return
		}
		ask := func(method, path, body string) {
			req, _ := http.NewRequest(method, url+path, strings.NewReader(body))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				got = append(got, answer{0, err.Error()})
				return
			}
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			got = append(got, answer{resp.StatusCode, string(b)})
		}
		ask("POST", "highlight", `And the "Get a quote" button is clicked`)
		ask("POST", "highlight", `And the "Send" button is clicked`)
		ask("POST", "run", `the "Weight (grams)" field is filled with "900"`)
		ask("POST", "run", `the "Weight (grams)" field has the value "900"`)
		ask("POST", "run", `within 1s the page shows "Price: 99.00 EUR"`)
		ask("GET", "recorded", "")
		// Someone clicks the Inspector's resume.
		if s, err := current(h.SC); err == nil {
			if d, err := s.ctx.Debugger(); err == nil {
				_ = d.Resume()
			}
		}
	}()
	h.OK(`the "Weight (grams)" field is filled with "1200"`) // line 4: paused before
	got := <-answers
	want := []struct {
		status int
		body   string
	}{
		{200, `the button named "Get a quote"`},
		{404, `No button named "Send" on the page;`},
		{200, "passed"},
		{200, "passed"},
		{422, `The page does not show "Price: 99.00 EUR"`},
		{200, ""},
	}
	if len(got) != len(want) {
		t.Fatalf("answers: %+v", got)
	}
	for i, w := range want {
		if got[i].status != w.status || !strings.HasPrefix(got[i].body, w.body) {
			t.Errorf("answer %d: %d %q, want %d %q", i, got[i].status, got[i].body, w.status, w.body)
		}
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// Packs that build on the web-core pack reach Chromium's remote debugging port,
// and open tabs of their own beside the scenario's.
func TestToolsDriveTheBrowserBeside(t *testing.T) {
	h, app := newHarness(t, "chromium")
	RemoteDebugging(h.Suite)
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	st := scenarioPages.Of(h.SC)
	c := &Current{s: st.current, suite: h.Suite}
	u, err := c.DebuggingURL()
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(u + "/json/version")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !strings.Contains(string(b), `"Browser"`) {
		t.Fatalf("%s/json/version: %s", u, b)
	}
	var hooked atomic.Int32
	if err := OnPage(h.SC, func(Context, playwright.Page) error { hooked.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	aside, err := c.AsideTab()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aside.Goto(app[0][1] + "/track.html"); err != nil {
		t.Fatal(err)
	}
	if _, err := aside.Evaluate(`console.error("an error of the tool's tab")`); err != nil {
		t.Fatal(err)
	}
	pg, _ := c.Page()
	if !strings.HasSuffix(pg.URL(), "/quote.html") {
		t.Errorf("the current tab is %s", pg.URL())
	}
	_ = aside.Close()
	h.OK(`the page shows "Get a quote"`)
	h.OK(`the page has no script errors`)
	// The hook ran on the open tab, and not on the tool's; the next tab is
	// the scenario's again.
	if n := hooked.Load(); n != 1 {
		t.Errorf("the page hooks ran %d times", n)
	}
	next, err := c.s.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := c.Page(); p != next {
		t.Error("a tab opened after the tool's is not the current tab")
	}
	_ = h.End("passed")

	h, app = newHarness(t, "firefox")
	RemoteDebugging(h.Suite)
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	c = &Current{s: scenarioPages.Of(h.SC).current, suite: h.Suite}
	if _, err := c.DebuggingURL(); err == nil || !strings.Contains(err.Error(), "only Chromium, Chrome and Edge") {
		t.Errorf("firefox: %v", err)
	}
	_ = h.End("passed")
}

// An agent's session shows it the page it is on: the elements steps can
// name, as they name them, what a screen reader reads, and a screenshot.
func TestAgentsLookAtThePage(t *testing.T) {
	h, app := newHarness(t, "chromium")
	look := tools()[0].Run
	_, err := look(&core.ToolCall{Scenario: h.SC, Input: json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "the session has no page open") {
		t.Fatalf("no page: %v", err)
	}
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	res, err := look(&core.ToolCall{Scenario: h.SC, Input: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	v := res.Data.(pageView)
	for _, want := range []string{`the "Get a quote" button`, `the "Weight (grams)" field`, `the "Destination country" field`,
		`the "Express" option`, `the "Insure this parcel" checkbox`, `the "Track a parcel" link`} {
		if !slices.Contains(v.Elements, want) {
			t.Errorf("the elements lack %s: %q", want, v.Elements)
		}
	}
	if v.App != "portal" || v.Title != "Get a quote" || !strings.HasSuffix(v.URL, "/quote.html") || v.Tabs != 1 {
		t.Errorf("%+v", v)
	}
	if !strings.Contains(v.Structure, `- heading "Get a quote" [level=1]`) {
		t.Errorf("structure:\n%s", v.Structure)
	}
	if len(res.Images) != 1 || res.Images[0].MediaType != "image/png" {
		t.Errorf("screenshot: %d images", len(res.Images))
	}
	res, err = look(&core.ToolCall{Scenario: h.SC, Input: json.RawMessage(`{"screenshot": false}`)})
	if err != nil || len(res.Images) != 0 {
		t.Errorf("without a screenshot: %v, %d images", err, len(res.Images))
	}
	if _, err := look(&core.ToolCall{Scenario: h.SC, Input: json.RawMessage(`{"app": "depot"}`)}); err == nil || !strings.Contains(err.Error(), "no page of the depot web app") {
		t.Errorf("another app: %v", err)
	}
	_ = h.End("passed")
}

// A CDP session a pack's page hook opens sets the page's media back to the
// screen: a web app printed stays printed, whenever the hook comes.
func TestPrintedPagesStayPrintedWhateverThePacksHooks(t *testing.T) {
	h, app := newHarness(t, "chromium")
	cdp := func(c Context, p playwright.Page) error {
		_, err := c.NewCDPSession(p)
		return err
	}
	if err := OnPage(h.SC, cdp); err != nil {
		t.Fatal(err)
	}
	h.OK("the portal web app with the following properties:", append(app, []string{"media", "print"}))
	h.OK(`the "/session.html" page is opened`)
	h.OK(`the page shows "Media: print"`)
	if err := OnPage(h.SC, cdp); err != nil {
		t.Fatal(err)
	}
	h.OK(`the "/quote.html" page is opened`)
	h.OK(`the "/session.html" page is opened`)
	h.OK(`the page shows "Media: print"`)
	_ = h.End("passed")
}
