package webcore

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

func TestSettings(t *testing.T) {
	st, err := parseConfig(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if st.watch || st.pause || st.slowdown != 0 || st.traces != "failed" || st.videos != "never" || st.testIDAttribute != "data-testid" {
		t.Errorf("defaults: %+v", st)
	}
	st, err = parseConfig(Config{
		Watch: true, Slowdown: "500ms", PauseOnFailure: true, Traces: "always", Videos: "failed", TestIDAttribute: "data-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !st.watch || !st.pause || st.slowdown != 500*time.Millisecond || st.traces != "always" || st.videos != "failed" ||
		st.testIDAttribute != "data-test" {
		t.Errorf("set: %+v", st)
	}
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{Slowdown: "fast"}, `packs.web-core.slowdown: "fast" is not a duration`},
		{Config{Traces: "sometimes"}, `packs.web-core.traces: "sometimes" is not one of failed, always, never`},
		{Config{Videos: "all"}, `packs.web-core.videos: "all" is not one of`},
		{Config{ScriptErrors: "ignore"}, `packs.web-core.scriptErrors: "ignore" is not one of report, fail`},
	} {
		if _, err := parseConfig(c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c.cfg, err)
		}
	}
	if !keep("always", false) || !keep("failed", true) || keep("failed", false) || keep("never", true) {
		t.Error("keep")
	}
}

func TestHowTheBrowserPresentsItself(t *testing.T) {
	a, err := parseApp(suite().Interpolate, "portal", table(
		[]string{"url", "http://parcels:8400"},
		[]string{"engine", "webkit"},
		[]string{"device", "iPhone 15"},
		[]string{"viewport", "390 x 844"},
		[]string{"locale", "de-DE"},
		[]string{"timezone", "Europe/Berlin"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if a.Device != "iPhone 15" || *a.Viewport != (Size{390, 844}) || a.Locale != "de-DE" || a.Timezone != "Europe/Berlin" {
		t.Errorf("%+v", a)
	}
	a, err = parseApp(suite().Interpolate, "portal", table(
		[]string{"url", "https://portal.example:8443/app"},
		[]string{"engine", "chrome"},
		[]string{"color scheme", "dark"},
		[]string{"reduced motion", "reduce"},
		[]string{"media", "print"},
		[]string{"user agent", "ParcelsKiosk/2.1"},
		[]string{"location", "52.5200, 13.4050"},
		[]string{"permissions", "geolocation, notifications"},
		[]string{"tls.verify", "false"},
		[]string{"username", "portal"},
		[]string{"password", "p0rtal"},
		[]string{"cookie.session", "s-4101"},
		[]string{"header.Authorization", "Bearer t-4101"},
		[]string{"local storage.token", "t-4101"},
		[]string{"session storage.msal.idtoken", "i-4101"},
	))
	if err != nil {
		t.Fatal(err)
	}
	want := App{
		Name: "portal", URL: "https://portal.example:8443/app", Engine: "chrome",
		ColorScheme: "dark", ReducedMotion: "reduce", Media: "print", UserAgent: "ParcelsKiosk/2.1", Location: &Location{52.52, 13.405},
		Permissions: []string{"geolocation", "notifications"}, InsecureTLS: true, Username: "portal", Password: "p0rtal",
		Cookies: []Setting{{"session", "s-4101"}}, Headers: []Setting{{"Authorization", "Bearer t-4101"}},
		LocalStorage: []Setting{{"token", "t-4101"}}, SessionStorage: []Setting{{"msal.idtoken", "i-4101"}},
	}
	if !reflect.DeepEqual(*a, want) {
		t.Errorf("got %+v\nwant %+v", *a, want)
	}
	if a.origin() != "https://portal.example:8443" {
		t.Errorf("origin %s", a.origin())
	}
	for _, c := range []struct{ key, value, want string }{
		{"color scheme", "sepia", "is not one of light, dark, no-preference"},
		{"media", "paper", "is not one of screen, print"},
		{"location", "Berlin", "is not a latitude and a longitude"},
		{"tls.verify", "maybe", "is not true or false"},
		{"username", "portal", "needs both a username and a password"},
		{"cookie", "x", "unknown web app property"},
	} {
		_, err := parseApp(suite().Interpolate, "portal", table([]string{"url", "http://parcels:8400"}, []string{c.key, c.value}))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: %v", c.key, c.value, err)
		}
	}
	for _, v := range []string{"wide", "0x720", "1280"} {
		_, err := parseApp(suite().Interpolate, "portal", table([]string{"url", "http://parcels:8400"}, []string{"viewport", v}))
		if err == nil || !strings.Contains(err.Error(), "viewport") {
			t.Errorf("viewport %q: %v", v, err)
		}
	}
}

func TestDownloadsStayInTheirFolder(t *testing.T) {
	dir := t.TempDir()
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: "abc-123456789", Name: "Export parcels"},
		core.NewSuite(core.SuiteOptions{ProjectDir: dir}), nil)
	a := &App{Name: "portal"}
	want := filepath.Join(dir, ".axx", "web", "downloads", "export-parcels-portal-23456789")
	for name, file := range map[string]string{
		"parcels.csv":      "parcels.csv",
		"../../etc/passwd": "passwd",
		`..\..\boot.ini`:   "boot.ini",
		"":                 "download",
	} {
		if got := downloadPath(sc, a, name); got != filepath.Join(want, file) {
			t.Errorf("%q: %s", name, got)
		}
	}
}

// A failure shows the lines of the page most like the text it looked for.
func TestFailuresShowTheNearestLines(t *testing.T) {
	lines := []string{"Parcels Get a quote Register a parcel", "Get a quote", "Weight (grams)", "Price: 6.90 EUR · delivered in 2 days"}
	if got := nearest(lines, "Price: 99.00 EUR"); len(got) != 1 || got[0] != lines[3] {
		t.Errorf("nearest: %q", got)
	}
	if got := nearest(lines, "Customs invoice: INV-2041.pdf"); len(got) != 0 {
		t.Errorf("nothing like it: %q", got)
	}
	if n := commonRun("Price: 6.90 EUR", "Price: 99.00 EUR"); n != 7 {
		t.Errorf("commonRun: %d", n)
	}
}
