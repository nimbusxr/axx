//go:build integration

package a11y

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

var engines = []string{"chromium", "firefox", "webkit"}

// newHarness is a scenario with the test site registered as the portal web
// app, in an engine.
func newHarness(t *testing.T, engine string, cfg Config) *cloudtest.Harness {
	t.Helper()
	site := httptest.NewServer(http.FileServer(http.Dir("testdata")))
	t.Cleanup(site.Close)
	h := cloudtest.NewWith(t, map[string]any{Name: cfg}, webcore.Pack(), Pack())
	h.OK("the portal web app with the following properties:", [][]string{{"url", site.URL}, {"engine", engine}})
	t.Cleanup(func() { _ = h.End("passed") })
	return h
}

func TestAnAccessiblePageHasNoViolations(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			h := newHarness(t, engine, Config{})
			h.OK(`the "/quote.html" page is opened`)
			h.OK(`the page has no accessibility violations`)
		})
	}
}

func TestViolationsSayWhatAndWhere(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			h := newHarness(t, engine, Config{})
			h.OK(`the "/track.html" page is opened`)
			err := h.Fails(`the page has no accessibility violations`, "The page has 3 accessibility violations (wcag21aa):")
			for _, want := range []string{"label (critical)", "#tracking", "image-alt (critical)", "#van", "color-contrast (serious)", "#note", "dequeuniversity.com"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the failure lacks %q:\n%v", want, err)
				}
			}
			attached := false
			for _, a := range h.Sink.Attachments {
				attached = attached || (a.Name == "accessibility violations" && a.MediaType == "application/json")
			}
			if !attached {
				t.Error("the violations are not attached")
			}
		})
	}
}

func TestAnAuditCanLeaveRulesOut(t *testing.T) {
	h := newHarness(t, "chromium", Config{Ignore: []string{"color-contrast", "image-alt"}})
	h.OK(`the "/track.html" page is opened`)
	_ = h.Fails(`the page has no accessibility violations`, "The page has 1 accessibility violation (wcag21aa):\n  label (critical)")
}

func TestAPartOfThePageIsAuditedAlone(t *testing.T) {
	h := newHarness(t, "chromium", Config{})
	h.OK(`the "/track.html" page is opened`)
	_ = h.Fails(`the "css=#track" element has no accessibility violations`, `The "css=#track" element has 1 accessibility violation`)
}

func TestFramesAreAuditedToo(t *testing.T) {
	h := newHarness(t, "chromium", Config{})
	h.OK(`the "/framed.html" page is opened`)
	err := h.Fails(`the page has no accessibility violations`, "label (critical)")
	if !strings.Contains(err.Error(), "#street") {
		t.Errorf("the frame's field is not named:\n%v", err)
	}
}

func TestThePageReadsAsItsStructureSays(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			h := newHarness(t, engine, Config{})
			h.OK(`the "/quote.html" page is opened`)
			h.OK(`the page's accessible structure is:`, `- heading "Get a quote" [level=1]
- button "Get a quote"
- paragraph: "/Price: \\d+\\.\\d+ EUR/"`)
			h.OK(`the "css=form" element's accessible structure is:`, `- spinbutton "Weight (grams)"
- combobox "Destination country"`)
		})
	}
}

func TestAStructureThatDiffersSaysWhatThePageReads(t *testing.T) {
	h := newHarness(t, "chromium", Config{})
	h.OK(`the "/quote.html" page is opened`)
	err := h.Fails(`within 1s the page's accessible structure is:`, `The page's accessible structure is not as expected`, `- heading "Register a parcel" [level=1]`)
	if !strings.Contains(err.Error(), `heading "Get a quote"`) {
		t.Errorf("the failure lacks what the page reads:\n%v", err)
	}
}

func TestAStructureMustBeAnARIASnapshot(t *testing.T) {
	h := newHarness(t, "chromium", Config{})
	h.OK(`the "/quote.html" page is opened`)
	_ = h.Fails(`the page's accessible structure is:`, `the accessible structure is no ARIA snapshot`, `- paragraph: /Price: \d+ EUR/`)
}
