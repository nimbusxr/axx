//go:build integration

package screenshots

import (
	"bytes"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// newHarness is a scenario with the look.html page's site registered as the
// quote web app, and the pack's settings.
func newHarness(t *testing.T, cfg Config) *cloudtest.Harness {
	t.Helper()
	site := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	t.Cleanup(site.Close)
	h := cloudtest.NewWith(t, map[string]any{Name: cfg}, webcore.Pack(), Pack())
	h.OK("the quote web app with the following properties:", [][]string{{"url", site.URL}, {"engine", "chromium"}})
	t.Cleanup(func() { _ = h.End("passed") })
	return h
}

func baseline(h *cloudtest.Harness, name string) string {
	return filepath.Join(h.Dir, "looks", name+".chromium-"+platform+".png")
}

func TestAMissingScreenshotIsTakenAndFails(t *testing.T) {
	h := newHarness(t, Config{Folder: "looks"})
	h.OK(`the "/look.html" page is opened`)
	_ = h.Fails(`the page looks like the "quote" screenshot`,
		`There was no "quote" screenshot to compare the page with: the page, as it is, is now looks/quote.chromium-`+platform+`.png`)
	if st, err := os.Stat(baseline(h, "quote")); err != nil || st.Size() == 0 {
		t.Errorf("the screenshot taken: %v", err)
	}
}

func TestAPageLooksLikeItsScreenshot(t *testing.T) {
	h := newHarness(t, Config{Folder: "looks"})
	h.OK(`the "/look.html" page is opened`)
	_ = h.Fails(`the page looks like the "quote" screenshot`, `There was no "quote" screenshot`)
	h.OK(`the page looks like the "quote" screenshot`)
}

func TestAChangedPageDoesNotLookLikeItsScreenshot(t *testing.T) {
	h := newHarness(t, Config{Folder: "looks"})
	h.OK(`the "/look.html" page is opened`)
	_ = h.Fails(`the page looks like the "quote" screenshot`, `There was no "quote" screenshot`)
	h.OK(`the "/look.html?late" page is opened`)
	h.Sink.Reset()
	_ = h.Fails(`within 1s the page looks like the "quote" screenshot`, `The page does not look like the "quote" screenshot:`)
	var names []string
	for _, a := range h.Sink.Attachments {
		names = append(names, a.Name)
	}
	for _, want := range []string{"expected screenshot", "the page", "difference"} {
		if !strings.Contains(strings.Join(names, ","), want) {
			t.Errorf("attachments %v lack %q", names, want)
		}
	}
	if kept, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "screenshots", "*-quote.png")); len(kept) != 1 {
		t.Errorf("the page as it looked: %v", kept)
	}
}

func TestScreenshotsAreComparedOnTheirPlatformsOnly(t *testing.T) {
	elsewhere := "windows"
	if platform == "windows" {
		elsewhere = "linux"
	}
	h := newHarness(t, Config{Folder: "looks", Platforms: []string{elsewhere}})
	h.OK(`the "/look.html" page is opened`)
	h.Sink.Reset()
	h.OK(`the page looks like the "quote" screenshot`)
	if _, err := os.Stat(baseline(h, "quote")); err == nil {
		t.Error("a screenshot was taken on a platform screenshots are not compared on")
	}
	if logs := strings.Join(h.Sink.Logs, "\n"); !strings.Contains(logs, `the "quote" screenshot is compared on `+elsewhere+` only, not on `+platform) {
		t.Errorf("logs: %s", logs)
	}
}

func TestUpdateTakesTheScreenshotsAgain(t *testing.T) {
	h := newHarness(t, Config{Folder: "looks", Update: true})
	old := mustPNG(image.NewRGBA(image.Rect(0, 0, 4, 4)))
	if err := os.MkdirAll(filepath.Dir(baseline(h, "quote")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baseline(h, "quote"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	h.OK(`the "/look.html" page is opened`)
	h.OK(`the page looks like the "quote" screenshot`)
	if now, _ := os.ReadFile(baseline(h, "quote")); bytes.Equal(now, old) || len(now) == 0 {
		t.Error("the screenshot was not taken again")
	}
}

func TestAnElementLooksLikeItsScreenshot(t *testing.T) {
	h := newHarness(t, Config{Folder: "looks"})
	h.OK(`the "/look.html" page is opened`)
	_ = h.Fails(`the "css=.card" element looks like the "quote card" screenshot`, `There was no "quote card" screenshot`)
	h.OK(`the "css=.card" element looks like the "quote card" screenshot`)
	h.OK(`the "/look.html?late" page is opened`)
	_ = h.Fails(`within 1s the "css=.card" element looks like the "quote card" screenshot`, `The page does not look like the "quote card" screenshot`)
}

func TestWhatChangesFromRunToRunIsPaintedOver(t *testing.T) {
	h := newHarness(t, Config{Folder: "looks"})
	h.OK(`the "/look.html?at" page is opened`)
	masked := [][]string{{"css=#now"}}
	_ = h.Fails(`the page looks like the "quote at" screenshot, apart from:`, `There was no "quote at" screenshot`, masked)
	h.OK(`the page is reloaded`)
	h.OK(`the page looks like the "quote at" screenshot, apart from:`, masked)
}

func TestAScreenshotNameIsAFileName(t *testing.T) {
	h := newHarness(t, Config{Folder: "looks"})
	h.OK(`the "/look.html" page is opened`)
	_ = h.Fails(`the page looks like the "../quote" screenshot`, "is not a file name")
}
