//go:build integration

package webcore

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Selectors, gestures and checks on a depot's board, in every engine.
func TestADepotBoardInEveryEngine(t *testing.T) {
	for _, engine := range bundled {
		t.Run(engine, func(t *testing.T) {
			h, app := newHarness(t, engine)
			h.File("invoices/INV-2041.pdf", "%PDF-1.4 invoice")
			h.OK("the depot web app with the following properties:", app)
			h.OK(`the "/board.html" page is opened`)
			h.OK(`the page title is "Depot board"`)
			h.OK(`the "Scan a parcel" field has the focus`)

			// Selectors, where names are not enough.
			h.OK(`the "css=#scan" field is filled with "PX-4101"`)
			h.OK(`the Enter key is pressed in the "xpath=//input[@id='scan']" field`)
			h.OK(`the page shows "Scanned PX-4101"`)
			h.OK(`the "testid=depot-total" element shows "3 parcels"`)
			h.OK(`the "xpath=//h1" element shows "Depot board"`)
			h.OK(`the page shows 3 "css=#board .card" elements`)
			h.OK(`the page shows 2 "Remove" buttons`)
			h.OK(`the "Depot" field is disabled`)
			h.OK(`the "Scan a parcel" field is enabled`)

			// Gestures.
			h.OK(`the "Print the label" menu item is not shown`)
			h.OK(`the "PX-4101" element is right-clicked`)
			h.OK(`the "Print the label" menu item is shown`)
			h.OK(`the "Print the label" menu item is clicked`)
			h.OK(`the page shows "Label printed for PX-4101"`)
			h.OK(`the "Depot board" element is clicked`)
			h.OK(`the "PX-4102" element is double-clicked`)
			h.OK(`the page shows "Editing PX-4102"`)
			h.OK(`the "PX-4101" element is dragged onto the "Out for delivery" element`)
			h.OK(`the page shows "PX-4101 moved to Out for delivery"`)
			h.OK(`the following options are chosen in the "Delivery days" field:`, [][]string{{"Monday"}, {"Wednesday"}})
			h.OK(`the page shows "Days: Monday, Wednesday"`)
			h.OK(`the invoices/INV-2041.pdf file is uploaded in the "Choose a customs invoice" field`)
			h.OK(`the page shows "Invoice: INV-2041.pdf"`)
			h.OK(`the page does not show "PX-5041"`)
			h.OK(`the "css=#more" element is scrolled into view`)
			h.OK(`the page shows "PX-5041"`)
			h.OK(`the page is scrolled to the bottom`)
			h.OK(`the page shows "PX-5081"`)
			h.OK(`the page is scrolled to the top`)

			// Requests and script errors.
			h.OK(`the "Refresh the board" button is clicked`)
			h.OK(`the browser sent a POST request to "/api/board"`)
			h.OK(`the page shows "Board refreshed"`)
			h.OK(`the page has no script errors`)
			if err := h.End("passed"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// State checks: checkboxes, options, attributes, counts of rows.
func TestTheStateOfThings(t *testing.T) {
	defer func(d time.Duration) { actionTimeout = d }(actionTimeout)
	actionTimeout = 2 * time.Second
	h, app := newHarness(t, "chromium")
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	h.OK(`the "Leave with a neighbour" checkbox is ticked`)
	h.OK(`the "Insure this parcel" checkbox is not ticked`)
	h.OK(`the "Standard" option is selected`)
	h.OK(`the "Express" option is not selected`)
	_ = h.Fails(`within 1s the "Insure this parcel" checkbox is ticked`, `The "Insure this parcel" checkbox is not ticked`)
	_ = h.Fails(`within 1s the "Express" option is selected`, `The "Express" option is not selected`)
	_ = h.Fails(`within 1s the "Weight (grams)" field is disabled`, `The "Weight (grams)" field is enabled`)
	_ = h.Fails(`within 1s the page title is "Quote"`, "The page has another title")
	_ = h.Fails(`within 1s the "Get a quote" button is not shown`, `The page shows 1 button named "Get a quote"`)
	_ = h.Fails(`within 1s the "Save" button is shown`, `No button named "Save" on the page; 1 buttons:`)
	_ = h.Fails(`within 1s the "css=.missing" element is shown`, `No element on the page matches "css=.missing"`)
	_ = h.Fails(`within 1s the page shows 2 "Get a quote" buttons`, "expected: 2\n  actual:   1")
	_ = h.Fails(`within 1s the "Get a quote" button shows "Send"`, `The "Get a quote" button does not show "Send"`)
	_ = h.Fails(`within 1s the "Get a quote" button has the focus`, `The "Get a quote" button does not have the focus`)

	h.OK(`the "/parcel.html" page is opened`)
	h.OK(`the "Actions" button has the aria-expanded attribute "false"`)
	h.OK(`the "Actions" button is clicked`)
	h.OK(`the "Actions" button has the aria-expanded attribute "true"`)
	_ = h.Fails(`within 1s the "Actions" button has the aria-busy attribute "true"`, `actual:   "no aria-busy attribute"`)
	h.OK(`the "Opening hours" menu item is clicked`)
	_ = h.Fails(`the page has no script errors`, "The page shows a dialog")
	h.OK(`the dialog is accepted`)

	h.OK(`the "/parcels" page is opened`)
	h.OK("the page shows 1 table row where:", [][]string{{"Parcel", "PX-4101"}})
	_ = h.Fails("within 1s the page shows 2 table rows where:", "The page shows another number of table rows where Status=IN_TRANSIT; the rows are:",
		[][]string{{"Status", "IN_TRANSIT"}})
	_ = h.Fails(`within 1s the browser sent a DELETE request to "/parcels"`, "The browser sent no DELETE request to \"/parcels\"; ")

	h.OK(`the "/board.html" page is opened`)
	h.OK(`the "Break the board" button is clicked`)
	err := h.Fails(`the page has no script errors`, "The portal web app's pages had 2 script errors:")
	for _, want := range []string{"columns is undefined", "the board lost its columns"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the script errors lack %q: %v", want, err)
		}
	}
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	if logs := strings.Join(h.Sink.Logs, "\n"); !strings.Contains(logs, "the portal web app's pages had 2 script errors") {
		t.Errorf("the failed scenario does not list its script errors: %s", logs)
	}
}

// Script errors can fail a scenario on their own.
func TestScriptErrorsCanFailScenarios(t *testing.T) {
	h, app := newConfigured(t, "chromium", &Config{ScriptErrors: "fail"})
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/board.html" page is opened`)
	h.OK(`the "Break the board" button is clicked`)
	err := h.End("passed")
	if err == nil || !strings.Contains(err.Error(), "The portal web app's pages had 2 script errors") {
		t.Errorf("the scenario did not fail: %v", err)
	}
}

// The browser's clock and connection.
func TestTheClockAndTheConnection(t *testing.T) {
	h, app := newHarness(t, "chromium")
	h.OK("the depot web app with the following properties:", app)
	h.OK(`the browser's clock is set to "2026-09-25T14:00:00+02:00"`)
	h.OK(`the "/board.html" page is opened`)
	h.OK(`the page shows "Today: 2026-09-25"`)
	h.OK(`the page shows "Session ends in 30 minutes"`)
	h.OK(`the browser's clock is moved forward by 31m`)
	h.OK(`the page shows "Session expired"`)
	h.OK(`the browser is offline`)
	h.OK(`the page shows "You are offline"`)
	h.OK(`the "Refresh the board" button is clicked`)
	h.OK(`the page shows "Offline: the board keeps its changes on this device"`)
	h.OK(`the browser is online`)
	h.OK(`the page shows "Back online"`)
	_ = h.Fails(`the browser's clock is set to "next Tuesday"`, `"next Tuesday" is not a time like 2026-09-25T14:00:00+02:00`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

func TestTappingNeedsATouchScreen(t *testing.T) {
	h, app := newHarness(t, "webkit")
	h.OK("the phone web app with the following properties:", append(app, []string{"device", "iPhone 15"}))
	h.OK(`the "/board.html" page is opened`)
	h.OK(`the "PX-4101" element is tapped`)
	h.OK(`the page shows "Tapped PX-4101"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	h, app = newHarness(t, "chromium")
	h.OK("the desk web app with the following properties:", app)
	h.OK(`the "/board.html" page is opened`)
	_ = h.Fails(`the "PX-4101" element is tapped`, "tapping needs a touch screen: give the desk web app a device that has one")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// A web app can start with a session: its cookies, headers, storage and
// HTTP credentials, and with how the browser presents itself.
func TestStartingWithASession(t *testing.T) {
	t.Setenv("SHOP_TOKEN", "t-4101-secret")
	h, app := newHarness(t, "chromium")
	// Pages learn where the browser is on secure addresses only: the portal over HTTPS.
	secure := append([][]string{{"url", fmt.Sprintf("https://127.0.0.1:%d", env.tlsPort)}}, app[1:]...)
	h.OK("the portal web app with the following properties:", append(secure,
		[]string{"tls.verify", "false"},
		[]string{"cookie.session", "s-4101"},
		[]string{"header.Authorization", "Bearer ${env:SHOP_TOKEN}"},
		[]string{"local storage.token", "${env:SHOP_TOKEN}"},
		[]string{"session storage.msal.idtoken", "i-4101"},
		[]string{"color scheme", "dark"},
		[]string{"reduced motion", "reduce"},
		[]string{"media", "print"},
		[]string{"user agent", "ParcelsKiosk/2.1"},
		[]string{"location", "52.5200, 13.4050"},
		[]string{"permissions", "notifications"},
	))
	h.OK(`the "/session.html" page is opened`)
	h.OK(`the page shows "Cookie: s-4101; Authorization: Bearer ${env:SHOP_TOKEN}; User: none"`)
	h.OK(`the page shows "Token: ${env:SHOP_TOKEN}; ID token: i-4101"`)
	h.OK(`the page shows "Color scheme: dark"`)
	h.OK(`the page shows "Motion: reduced"`)
	h.OK(`the page shows "Media: print"`)
	h.OK(`the page shows "Agent: ParcelsKiosk/2.1"`)
	h.OK(`the page shows "Location: 52.5200, 13.4050"`)
	h.OK(`the page shows "Notifications: granted"`)
	err := h.Fails(`within 1s the page shows "Token: t-9999"`, `The page does not show "Token: t-9999"`)
	if !strings.Contains(err.Error(), "Token: ********") || strings.Contains(err.Error(), "t-4101-secret") {
		t.Errorf("the failure does not mask the token: %v", err)
	}
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "traces", "*.zip"))
	if len(traces) != 1 {
		t.Fatalf("traces: %v", traces)
	}
	for name, b := range readZip(t, traces[0]) {
		if strings.Contains(string(b), "t-4101-secret") {
			t.Errorf("the trace's %s holds the token", name)
		}
	}

	// HTTP authentication, for the app's address.
	h, app = newHarness(t, "chromium")
	h.OK("the depot web app with the following properties:", append(app, []string{"username", "depot"}, []string{"password", "depot-staff"}))
	h.OK(`the "/protected" page is opened`)
	h.OK(`the page shows "Welcome to the depot"`)
	h.OK("the stranger web app with the following properties:", app)
	_ = h.Fails(`the "/protected" page of the stranger web app is opened`, "asks for a username and a password (HTTP authentication)")
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// An app with a certificate no one trusts, as in many test environments.
func TestUntrustedCertificates(t *testing.T) {
	environment(t)
	h, app := newHarness(t, "chromium")
	app[0][1] = fmt.Sprintf("https://127.0.0.1:%d", env.tlsPort)
	h.OK("the strict web app with the following properties:", app)
	_ = h.Fails(`the "/quote.html" page is opened`, "ERR_CERT")
	h.OK("the lenient web app with the following properties:", append(app, []string{"tls.verify", "false"}))
	h.OK(`the "/quote.html" page of the lenient web app is opened`)
	h.OK(`the page shows "Get a quote"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// Traces and videos go with the failed step to the reports, and to the
// IDE running axx.
func TestFailuresAttachTheirTraceAndVideo(t *testing.T) {
	h, app := newConfigured(t, "chromium", &Config{Videos: "failed", Watch: false})
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	_ = h.Fails(`within 1s the page shows "Price: 99.00 EUR"`, "does not show")
	if err := h.End("failed"); err != nil {
		t.Fatal(err)
	}
	types := map[string]int{}
	for _, a := range h.Sink.Attachments {
		types[a.MediaType]++
	}
	if types["application/zip"] != 1 || types["video/webm"] != 1 {
		t.Errorf("attachments: %v", types)
	}
	announced := strings.Join(h.Sink.Announced, "\n")
	for _, want := range []string{"[AXX-IDE] trace-viewer dir=/", "/package/lib/vite/traceViewer", "[AXX-IDE] trace path=/", "[AXX-IDE] video path=/"} {
		if !strings.Contains(announced, want) {
			t.Errorf("announcements lack %q:\n%s", want, announced)
		}
	}
}

// Google Chrome and Microsoft Edge, where the machine has them.
func TestBrandedBrowsers(t *testing.T) {
	for _, engine := range []string{"chrome", "msedge"} {
		t.Run(engine, func(t *testing.T) {
			h, app := newHarness(t, engine)
			h.OK("the portal web app with the following properties:", app)
			if err := h.Step(`the "/quote.html" page is opened`); err != nil {
				if strings.Contains(err.Error(), "installed on this machine: install it") {
					t.Skipf("this machine has no %s: %v", engine, err)
				}
				t.Fatal(err)
			}
			h.OK(`the page shows "Get a quote"`)
			if err := h.End("passed"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
