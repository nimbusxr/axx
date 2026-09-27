//go:build integration

package lighthouse

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// newSite serves the parcels pages of testdata/site: /quote, quick and
// sound; /parcels/new, heavy, with a slow image, busy work and a notice
// that moves the page down; /track/PX-4101, with accessibility problems;
// and /account, for the signed in only (the session cookie), which sends
// the others to /login.
func newSite(t *testing.T) *httptest.Server {
	t.Helper()
	page := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeFile(w, r, filepath.Join("testdata", "site", file))
		}
	}
	hero := heroJPEG(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /quote", page("quote.html"))
	mux.HandleFunc("GET /parcels/new", page("new.html"))
	mux.HandleFunc("GET /track/PX-4101", page("track.html"))
	mux.HandleFunc("GET /login", page("login.html"))
	mux.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err != nil || c.Value != "parcels-7f3a" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		page("account.html")(w, r)
	})
	mux.HandleFunc("GET /van.svg", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join("testdata", "site", "van.svg"))
	})
	mux.HandleFunc("GET /hero.jpg", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond) // a busy image server
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(hero)
	})
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("User-agent: *\nAllow: /\n"))
	})
	site := httptest.NewServer(mux)
	t.Cleanup(site.Close)
	return site
}

// heroJPEG is a large photo-like image.
func heroJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	for y := range 900 {
		for x := range 1600 {
			img.Set(x, y, color.RGBA{uint8(x * 255 / 1600), uint8(y * 255 / 900), uint8((x ^ y) & 0xff), 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// newHarness is a scenario with the site registered as the shop web app,
// with more properties, and the pack's settings.
func newHarness(t *testing.T, cfg Config, props ...[]string) *cloudtest.Harness {
	t.Helper()
	site := newSite(t)
	h := cloudtest.NewWith(t, map[string]any{Name: cfg}, webcore.Pack(), Pack())
	h.OK("the shop web app with the following properties:", append([][]string{{"url", site.URL}}, props...))
	t.Cleanup(func() { _ = h.End("passed") })
	return h
}

func reports(t *testing.T, h *cloudtest.Harness) (attached []cloudtest.Attachment, kept []string) {
	t.Helper()
	for _, a := range h.Sink.Attachments {
		if a.MediaType == "text/html" && a.Name == "Lighthouse report" {
			attached = append(attached, a)
		}
	}
	kept, _ = filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "lighthouse", "*.html"))
	return attached, kept
}

func TestAQuickPageScoresWellAndLoadsWithinItsLimits(t *testing.T) {
	h := newHarness(t, Config{})
	h.OK(`the "/quote" page is opened`)
	// /quote has no scripts: what blocks it is the machine's own load, which
	// the mobile audit multiplies by 4 (306ms on a busy CI runner). The
	// limits leave room for that; /parcels/new's busy work is far beyond it.
	h.OK(`the "/quote" page scores at least:`, [][]string{
		{"performance", "80"}, {"accessibility", "100"}, {"best practices", "90"}, {"seo", "90"},
	})
	h.OK(`the "/quote" page loads within:`, [][]string{
		{"largest contentful paint", "2.5s"},
		{"first contentful paint", "1.8s"},
		{"total blocking time", "600ms"},
		{"speed index", "3.4s"},
		{"cumulative layout shift", "0.1"},
	})
	attached, kept := reports(t, h)
	if len(attached) != 2 || len(kept) != 2 {
		t.Fatalf("each audit has its report: attached %d, kept %v", len(attached), kept)
	}
	for _, a := range attached {
		if !bytes.Contains(a.Body, []byte(`"formFactor":"mobile"`)) || !bytes.Contains(a.Body, []byte("/quote")) {
			t.Errorf("the report is not the mobile audit of /quote: %.300s", a.Body)
		}
	}
	for _, want := range []string{"-quote.html", "-quote-2.html"} {
		if !strings.Contains(strings.Join(kept, " "), want) {
			t.Errorf("no report ends in %s: %v", want, kept)
		}
	}
	logs := strings.Join(h.Sink.Logs, "\n")
	for _, want := range []string{`The "/quote" page scores as it should (performance: `, `The "/quote" page loads within its limits (largest contentful paint: `, "the Lighthouse report of the \"/quote\" page: .axx/web/lighthouse/"} {
		if !strings.Contains(logs, want) {
			t.Errorf("the logs lack %q:\n%s", want, logs)
		}
	}
}

func TestAPageBelowItsScoresSaysWhatCostItTheMost(t *testing.T) {
	h := newHarness(t, Config{})
	h.OK(`the "/quote" page is opened`)
	err := h.Fails(`the "/track/PX-4101" page scores at least:`, `The "/track/PX-4101" page scores below what it should:`, [][]string{
		{"performance", "50"}, {"accessibility", "100"}, {"best practices", "90"}, {"seo", "80"},
	})
	for _, want := range []string{
		"\n  accessibility: ", ", at least 100\n",
		"\n    Image elements do not have `[alt]` attributes",
		"\n    Form elements do not have associated labels",
		"\n    Background and foreground colors do not have a sufficient contrast ratio.",
		"\n  (performance: ", ", best practices: ", ", seo: ",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure lacks %q:\n%v", want, err)
		}
	}
	// The scenario goes on in its own tab.
	h.OK(`the page title is "Get a quote - Parcels"`)
	if attached, _ := reports(t, h); len(attached) != 1 {
		t.Errorf("the report of a failed audit is attached too: %d", len(attached))
	}
}

func TestASlowPageSaysWhichLimitsItMisses(t *testing.T) {
	h := newHarness(t, Config{})
	h.OK(`the "/quote" page is opened`)
	// Limits whatever the machine: no page paints within 1ms, the page's
	// busy work blocks for longer than 200ms, and it shifts less than 1.
	err := h.Fails(`the "/parcels/new" page loads within:`, `The "/parcels/new" page does not load within its limits:`, [][]string{
		{"first contentful paint", "1ms"}, {"total blocking time", "200ms"}, {"cumulative layout shift", "1"},
	})
	for _, want := range []string{"\n  first contentful paint: ", ", at most 1ms", "\n  total blocking time: ", ", at most 200ms", "\n  (cumulative layout shift: "} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure lacks %q:\n%v", want, err)
		}
	}
	err = h.Fails(`the "/parcels/new" page scores at least:`, `The "/parcels/new" page scores below what it should:`, [][]string{
		{"performance", "90"},
	})
	if !strings.Contains(err.Error(), "\n    Total Blocking Time: ") {
		t.Errorf("the failure lacks the blocking time:\n%v", err)
	}
}

func TestASignedInPageIsAuditedSignedIn(t *testing.T) {
	h := newHarness(t, Config{}, []string{"cookie.session", "parcels-7f3a"})
	h.OK(`the "/quote" page is opened`)
	h.OK(`the "/account" page scores at least:`, [][]string{{"accessibility", "100"}})

	// Signed out, Lighthouse audits the sign-in page, which says so.
	h = newHarness(t, Config{})
	h.OK(`the "/quote" page is opened`)
	_ = h.Fails(`the "/account" page scores at least:`, `The "/account" page (redirected to /login) scores below what it should:
  accessibility: `, [][]string{{"accessibility", "100"}})
}

func TestTheDesktopPreset(t *testing.T) {
	h := newHarness(t, Config{Device: "desktop"})
	h.OK(`the "/quote" page is opened`)
	h.OK(`the "/quote" page loads within:`, [][]string{{"largest contentful paint", "1s"}})
	attached, _ := reports(t, h)
	if len(attached) != 1 || !bytes.Contains(attached[0].Body, []byte(`"formFactor":"desktop"`)) {
		t.Errorf("the audit is not a desktop one")
	}
}

func TestAPageThatAnswersWithAnErrorIsNotAudited(t *testing.T) {
	h := newHarness(t, Config{})
	h.OK(`the "/quote" page is opened`)
	err := h.Fails(`the "/parcels/PX-0000" page scores at least:`, `Lighthouse could not audit the "/parcels/PX-0000" page: `, [][]string{{"seo", "90"}})
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("the failure lacks the status:\n%v", err)
	}
}

func TestLighthouseAuditsChromiumOnly(t *testing.T) {
	h := newHarness(t, Config{}, []string{"engine", "firefox"})
	h.OK(`the "/quote" page is opened`)
	err := h.Step(`the "/quote" page scores at least:`, [][]string{{"performance", "90"}})
	if err == nil || err.Error() != "Lighthouse audits pages in Chromium, Chrome and Edge only; the shop web app runs in firefox" {
		t.Errorf("firefox: %v", err)
	}
}

// Scenarios run side by side in one browser: an audit keeps to its tab
// while other scenarios open theirs and close them as they end.
func TestAnAuditKeepsToItsTabWhileOtherScenariosRun(t *testing.T) {
	site := newSite(t)
	h := cloudtest.NewWith(t, map[string]any{Name: Config{}}, webcore.Pack(), Pack())
	app := [][]string{{"url", site.URL}}
	const others = 6
	stop, done := make(chan struct{}), make(chan error, others)
	for g := range others {
		go func() {
			for i := 0; ; i++ {
				select {
				case <-stop:
					done <- nil
					return
				default:
				}
				sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: fmt.Sprint("other-", g, "-", i), Name: "other", URI: "features/test.feature", Line: 1}, h.Suite, h.Sink)
				o := h.In(sc)
				for _, step := range []string{"the shop web app with the following properties:", `the "/quote" page is opened`} {
					var err error
					if strings.HasSuffix(step, ":") {
						err = o.Step(step, app)
					} else {
						err = o.Step(step)
					}
					if err != nil {
						done <- err
						return
					}
				}
				if err := o.End("passed"); err != nil {
					done <- err
					return
				}
			}
		}()
	}
	h.OK("the shop web app with the following properties:", app)
	h.OK(`the "/quote" page is opened`)
	for range 2 {
		h.OK(`the "/quote" page scores at least:`, [][]string{{"accessibility", "90"}})
	}
	close(stop)
	for range others {
		if err := <-done; err != nil {
			t.Fatalf("the other scenarios: %v", err)
		}
	}
	_ = h.End("passed")
}
