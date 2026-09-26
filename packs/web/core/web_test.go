package webcore

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
)

func suite() *core.Suite {
	return core.NewSuite(core.SuiteOptions{Interpolate: func(s string) string {
		return os.Expand(s, func(k string) string {
			if k == "sys:portal.url" {
				return "http://parcels:8400"
			}
			return "${" + k + "}"
		})
	}})
}

func table(rows ...[]string) *core.Table { return &core.Table{Rows: rows} }

func TestRegisteringAWebApp(t *testing.T) {
	a, err := parseApp(suite().Interpolate, "portal", table(
		[]string{"url", "${sys:portal.url}/portal/"},
	))
	if err != nil {
		t.Fatal(err)
	}
	want := App{Name: "portal", URL: "http://parcels:8400/portal", Engine: "chromium"}
	if !reflect.DeepEqual(*a, want) {
		t.Errorf("got %+v, want %+v", *a, want)
	}
	a, err = parseApp(suite().Interpolate, "portal", table([]string{"url", "https://portal.example"}, []string{"engine", "webkit"}))
	if err != nil || a.Engine != "webkit" {
		t.Fatalf("%+v %v", a, err)
	}

	for _, tc := range []struct {
		rows [][]string
		want string
	}{
		{[][]string{{"engine", "webkit"}}, `"url" is required`},
		{[][]string{{"url", "parcels:8400"}}, "is not an http(s) URL"},
		{[][]string{{"url", "http://parcels:8400"}, {"engine", "safari"}}, "is not one of chromium, firefox, webkit, chrome, msedge"},
		{[][]string{{"url", "http://parcels:8400"}, {"browser", "ws://localhost:3000/"}}, `unknown web app property "browser"`},
	} {
		if _, err := parseApp(suite().Interpolate, "portal", table(tc.rows...)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: want %q, got %v", tc.rows, tc.want, err)
		}
	}
}

func TestPagesOfTheApp(t *testing.T) {
	a := &App{URL: "http://parcels:8400/portal"}
	for page, want := range map[string]string{
		"/quotes":                 "http://parcels:8400/portal/quotes",
		"quotes?weight=1200":      "http://parcels:8400/portal/quotes?weight=1200",
		"/":                       "http://parcels:8400/portal/",
		"https://auth.example/in": "https://auth.example/in",
	} {
		if got := a.pageURL(page); got != want {
			t.Errorf("pageURL(%q) = %q, want %q", page, got, want)
		}
	}
	for _, tc := range []struct{ address, want, path string }{
		{"http://parcels:8400/portal/quotes/Q-1", "/quotes/Q-1", "/quotes/Q-1"},
		{"http://parcels:8400/portal/quotes/Q-1?tab=price#top", "/quotes/Q-1", "/quotes/Q-1"},
		{"http://parcels:8400/portal/quotes?tab=price#top", "/quotes?tab=price", "/quotes?tab=price"},
		{"http://parcels:8400/portal", "/", "/"},
		{"http://parcels:8400/portalx/quotes", "/quotes", "http://parcels:8400/portalx/quotes"},
		{"https://auth.example/in", "/in", "https://auth.example/in"},
	} {
		if got := a.pagePath(tc.address, tc.want); got != tc.path {
			t.Errorf("pagePath(%q, %q) = %q, want %q", tc.address, tc.want, got, tc.path)
		}
	}
}

func TestTableRows(t *testing.T) {
	parcels := pageTable{
		Headers: []string{"Parcel", "Recipient", "Status"},
		Rows:    [][]string{{"PX-4101", "Anna Weber", "IN_TRANSIT"}, {"PX-4102", "Marc Petit", "DELIVERED"}},
	}
	quotes := pageTable{Headers: []string{"Zone", "Price"}, Rows: [][]string{{"DE-1", "6.90 EUR"}}}
	pairs := func(kv ...string) []core.Pair {
		var out []core.Pair
		for i := 0; i < len(kv); i += 2 {
			out = append(out, core.Pair{Key: kv[i], Value: kv[i+1]})
		}
		return out
	}

	if ok, why := matchRow([]pageTable{quotes, parcels}, pairs("Status", "DELIVERED", "Parcel", "PX-4102")); !ok {
		t.Errorf("no match: %s", why)
	}
	// Values compare as the page shows them, spaces collapsed.
	if ok, why := matchRow([]pageTable{parcels}, pairs("Recipient", "  Anna   Weber ")); !ok {
		t.Errorf("no match: %s", why)
	}
	for _, tc := range []struct {
		tables []pageTable
		want   []core.Pair
		why    string
	}{
		{[]pageTable{parcels}, pairs("Parcel", "PX-4101", "Status", "DELIVERED"), "the rows are:\n  Parcel=PX-4101, Status=IN_TRANSIT\n  Parcel=PX-4102, Status=DELIVERED"},
		{[]pageTable{quotes, parcels}, pairs("Carrier", "Kestrel"), `no table on the page has the columns Carrier; the tables have: "Zone | Price"; "Parcel | Recipient | Status"`},
		{nil, pairs("Parcel", "PX-4101"), "the page shows no table"},
		{[]pageTable{{Headers: []string{"Parcel"}}}, pairs("Parcel", "PX-4101"), "the tables with those columns have no rows"},
		// Case matters, in headers and values.
		{[]pageTable{parcels}, pairs("parcel", "PX-4101"), "no table on the page has the columns parcel"},
	} {
		ok, why := matchRow(tc.tables, tc.want)
		if ok || !strings.HasPrefix(why, tc.why) {
			t.Errorf("%v: got %v %q, want %q", tc.want, ok, why, tc.why)
		}
	}
}

func TestLaunchErrorsSayWhatToDo(t *testing.T) {
	for _, c := range []struct {
		engine, err, want string
	}{
		{
			"chrome", "BrowserType.launch: Chromium distribution 'chrome' is not found at /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\nRun \"npx playwright install chrome\"",
			"the chrome engine is the Google Chrome installed on this machine: install it, or use chromium",
		},
		{
			"webkit", "BrowserType.launch: \n╔══════╗\n║ Host system is missing dependencies to run browsers. ║",
			"install them with `sudo npx playwright@" + PlaywrightVersion + " install-deps webkit`",
		},
		{
			"chromium", "BrowserType.launch: Target page, context or browser has been closed\nLooks like you launched a headed browser without having a XServer running.",
			"this machine has no screen: run without packs.web-core.watch",
		},
	} {
		err := launchError(&App{Engine: c.engine}, errors.New(c.err))
		if !strings.HasPrefix(err.Error(), "cannot start the "+c.engine+" browser: BrowserType.launch:") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.engine, err)
		}
	}
}

// The pack pins the Playwright version playwright-go speaks: the driver it
// prepares must be that version.
func TestPinnedPlaywrightVersion(t *testing.T) {
	d, err := playwright.NewDriver(&playwright.RunOptions{DriverDirectory: t.TempDir(), SkipInstallBrowsers: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != PlaywrightVersion {
		t.Errorf("playwright-go drives Playwright %s, the pack pins %s: update packs/web/internal/driver", d.Version, PlaywrightVersion)
	}
}
