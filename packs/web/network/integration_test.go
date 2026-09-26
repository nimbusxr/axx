//go:build integration

package network

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

var engines = []string{"chromium", "firefox", "webkit"}

// site is the test site: a parcel's tracking page, which asks /api/estimate
// when the parcel arrives and follows its status on the /live websocket.
type site struct {
	*httptest.Server
	arrives atomic.Value
}

func newSite(t *testing.T) *site {
	t.Helper()
	s := &site{}
	s.arrives.Store("Tuesday")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("testdata")))
	mux.HandleFunc("/api/estimate", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"arrives": %q}`, s.arrives.Load())
	})
	mux.HandleFunc("/live", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := r.Context()
		for {
			_, msg, err := c.Read(ctx)
			if err != nil {
				return
			}
			if strings.Contains(string(msg), `"follow":"PX-4101"`) {
				_ = c.Write(ctx, websocket.MessageText, []byte(`{"reference":"PX-4101","status":"IN_TRANSIT"}`))
			}
		}
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// newHarness is a scenario with the test site registered as the tracking
// web app, in an engine.
func newHarness(t *testing.T, s *site, engine string, cfg Config) *cloudtest.Harness {
	t.Helper()
	h := cloudtest.NewWith(t, map[string]any{Name: cfg}, webcore.Pack(), Pack())
	h.OK("the tracking web app with the following properties:", [][]string{{"url", s.URL}, {"engine", engine}})
	return h
}

func ended(t *testing.T, h *cloudtest.Harness) {
	t.Cleanup(func() { _ = h.End("passed") })
}

func TestARequestThatFailsIsOneThePageCannotMake(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			h := newHarness(t, newSite(t), engine, Config{})
			ended(t, h)
			h.OK(`the page's requests to "/api/estimate" fail`)
			h.OK(`the "/track.html" page is opened`)
			h.OK(`the page shows "No estimate: the service cannot be reached"`)
		})
	}
}

func TestARequestAnswersWithAStatus(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			h := newHarness(t, newSite(t), engine, Config{})
			ended(t, h)
			h.OK(`the page's requests to "/api/estimate" answer with status 503`)
			h.OK(`the "/track.html" page is opened`)
			h.OK(`the page shows "No estimate right now (503)"`)
		})
	}
}

func TestARequestAnswersWithAFile(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			h := newHarness(t, newSite(t), engine, Config{})
			ended(t, h)
			h.File("network/estimate-friday.json", `{"arrives": "Friday"}`)
			h.OK(`the page's requests to "/api/estimate" answer with the network/estimate-friday.json file`)
			h.OK(`the "/track.html" page is opened`)
			h.OK(`the page shows "Arrives on Friday"`)
		})
	}
}

func TestAFileMustBeThere(t *testing.T) {
	h := newHarness(t, newSite(t), "chromium", Config{})
	ended(t, h)
	_ = h.Fails(`the page's requests to "/api/estimate" answer with the network/nowhere.json file`, "not found")
}

func TestARequestTakesItsTime(t *testing.T) {
	h := newHarness(t, newSite(t), "chromium", Config{})
	ended(t, h)
	h.OK(`the page's requests to "/api/estimate" take 3s`)
	start := time.Now()
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the page shows "Working out when your parcel arrives"`)
	h.OK(`the page shows "Arrives on Tuesday"`)
	if took := time.Since(start); took < 3*time.Second {
		t.Errorf("the estimate came after %s, not 3s", took)
	}
}

func TestAddressesArePathsOrWholeURLsWithWildcards(t *testing.T) {
	s := newSite(t)
	for _, c := range []struct{ pattern, shows string }{
		{s.URL + "/api/*", "No estimate: the service cannot be reached"},
		{"/**", "No estimate: the service cannot be reached"},
		{"/api/estimate?ref=PX-4101", "No estimate: the service cannot be reached"},
		{"/api/estimate?ref=PX-9999", "Arrives on Tuesday"},
		{"/api", "Arrives on Tuesday"},
	} {
		t.Run(c.pattern, func(t *testing.T) {
			h := newHarness(t, s, "chromium", Config{})
			ended(t, h)
			h.OK(`the page's requests to "` + c.pattern + `" fail`)
			// The page itself (for /**) is one of the requests.
			if c.pattern == "/**" {
				_ = h.Fails(`the "/track.html" page is opened`, "")
				return
			}
			h.OK(`the "/track.html" page is opened`)
			h.OK(`the page shows "` + c.shows + `"`)
		})
	}
}

func TestAStepChangesTheRequestsOfAnOpenPage(t *testing.T) {
	h := newHarness(t, newSite(t), "chromium", Config{})
	ended(t, h)
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the page shows "Arrives on Tuesday"`)
	h.OK(`the page's requests to "/api/estimate" answer with status 502`)
	h.OK(`the "Ask again" button is clicked`)
	h.OK(`the page shows "No estimate right now (502)"`)
}

func TestTheLatestStepAnswersFirst(t *testing.T) {
	h := newHarness(t, newSite(t), "chromium", Config{})
	ended(t, h)
	h.OK(`the page's requests to "/api/estimate" answer with status 502`)
	h.OK(`the page's requests to "/api/estimate" answer with status 503`)
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the page shows "No estimate right now (503)"`)
}

func TestTheConnectionIsSlow(t *testing.T) {
	h := newHarness(t, newSite(t), "chromium", Config{})
	ended(t, h)
	h.OK(`the browser's connection is slow`)
	start := time.Now()
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the page shows "Arrives on Tuesday"`)
	// The page, then the estimate: two round trips of 400 ms at least.
	if took := time.Since(start); took < 800*time.Millisecond {
		t.Errorf("the page and its estimate came after %s, not 800ms", took)
	}
}

func TestOnlyChromiumSlowsItsConnection(t *testing.T) {
	h := newHarness(t, newSite(t), "firefox", Config{})
	ended(t, h)
	h.OK(`the browser's connection is slow`)
	_ = h.Fails(`the "/track.html" page is opened`, "Chromium, Chrome and Edge only, not in firefox")
}

func TestTrafficIsRecordedToBeAnsweredFrom(t *testing.T) {
	s := newSite(t)
	rec := newHarness(t, s, "chromium", Config{Record: true})
	rec.OK(`the "/track.html" page is opened`)
	rec.OK(`the page shows "Arrives on Tuesday"`)
	if err := rec.End("passed"); err != nil {
		t.Fatal(err)
	}
	hars, _ := filepath.Glob(filepath.Join(rec.Dir, ".axx", "web", "recordings", "*-tracking-*.har"))
	if len(hars) != 1 {
		t.Fatalf("recordings: %v", hars)
	}
	har, err := os.ReadFile(hars[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(har), "/api/estimate?ref=PX-4101") {
		t.Fatalf("the recording lacks the estimate:\n%s", har)
	}

	s.arrives.Store("Monday")
	h := newHarness(t, s, "chromium", Config{})
	ended(t, h)
	h.File("network/tracking.har", string(har))
	h.OK(`the page's requests are answered from the network/tracking.har recording`)
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the page shows "Arrives on Tuesday"`)
	// Websockets go out, and so does what the recording lacks.
	h.OK(`the page shows "Status: IN_TRANSIT"`)
	h.OK(`the "/estimate-friday.json" page is opened`)
	h.OK(`the page shows "Friday"`)
}

func TestWebsocketMessages(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			h := newHarness(t, newSite(t), engine, Config{})
			ended(t, h)
			h.OK(`the "/track.html" page is opened`)
			h.OK(`the page sent a websocket message containing "PX-4101"`)
			h.OK(`the page received a websocket message containing "IN_TRANSIT"`)
			h.OK(`the page shows "Status: IN_TRANSIT"`)
		})
	}
}

func TestAMessageThatDidNotComeSaysWhatDid(t *testing.T) {
	h := newHarness(t, newSite(t), "chromium", Config{})
	ended(t, h)
	h.OK(`the "/track.html" page is opened`)
	h.OK(`the page received a websocket message containing "IN_TRANSIT"`)
	_ = h.Fails(`within 1s the page sent a websocket message containing "PX-9999"`,
		`No websocket message the pages sent contains "PX-9999"; 1 messages sent:`+"\n"+`  "{\"follow\":\"PX-4101\"}"`)
	_ = h.Fails(`within 1s the page received a websocket message containing "DELIVERED"`, `"{\"reference\":\"PX-4101\",\"status\":\"IN_TRANSIT\"}"`)
}

func TestAPageTheStepsAnswerReachesTheAppsServices(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			h := newHarness(t, newSite(t), engine, Config{})
			ended(t, h)
			b, err := os.ReadFile("testdata/track.html")
			if err != nil {
				t.Fatal(err)
			}
			h.File("pages/track.html", string(b))
			h.OK(`the page's requests to "/track.html" answer with the pages/track.html file`)
			h.OK(`the "/track.html" page is opened`)
			h.OK(`the page shows "Status: IN_TRANSIT"`)
		})
	}
}
