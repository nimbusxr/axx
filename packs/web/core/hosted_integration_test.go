//go:build integration

package webcore

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/packs/web/internal/relay"
)

// reachedFromTheRun makes the run's side of a hosted browser's network
// reach the portal by a name only it knows, and nothing else: not even the
// portal's own address on this machine, which the browser could reach
// itself if anything of its network went past the run.
func reachedFromTheRun(t *testing.T, portal string) *atomic.Int64 {
	t.Helper()
	u, err := url.Parse(portal)
	if err != nil {
		t.Fatal(err)
	}
	var refused atomic.Int64
	before := hostedNetwork
	t.Cleanup(func() { hostedNetwork = before })
	hostedNetwork = func(s *core.Suite) relay.Options {
		return relay.Options{
			Logger: s.Logger(),
			Proxy:  func(*url.URL) (*url.URL, error) { return nil, nil },
			Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, _ := net.SplitHostPort(addr)
				if host != "portal.parcels.test" {
					refused.Add(1)
					return nil, fmt.Errorf("the sandbox refuses %s", addr)
				}
				var d net.Dialer
				return d.DialContext(ctx, network, u.Host)
			},
		}
	}
	return &refused
}

// A browser the axx host started reaches a site through the run that drives
// it, in every engine: what the run cannot reach, the browser cannot,
// though it is on a machine that can.
func TestAHostedBrowsersNetworkGoesThroughTheRun(t *testing.T) {
	t.Setenv(cloudtest.HostedEnv, "1")
	for _, engine := range bundled {
		t.Run(engine, func(t *testing.T) {
			if why := notHosted(engine, runtime.GOOS); why != "" {
				t.Skip(why)
			}
			portal := environment(t)
			refused := reachedFromTheRun(t, portal)
			h := cloudtest.NewWith(t, nil, Pack())
			h.OK("the portal web app with the following properties:", [][]string{{"url", "http://portal.parcels.test"}, {"engine", engine}})
			h.OK(`the "/quote.html" page is opened`)
			h.OK(`the "Get a quote" button is clicked`)
			h.OK(`the page shows "Enter a weight"`)

			// The same portal, by its address on this machine: the run's
			// side refuses it, so the page does not load.
			h = cloudtest.NewWith(t, nil, Pack())
			h.OK("the portal web app with the following properties:", [][]string{{"url", portal}, {"engine", engine}})
			before := refused.Load()
			// The page that opens, if one does, is the run's answer that it
			// cannot reach the address: never the portal's.
			if err := h.Step(`the "/quote.html" page is opened`); err == nil {
				if err := h.Step(`the page shows "Get a quote"`); err == nil {
					t.Fatalf("the browser reached %s past the run", portal)
				}
				h.OK(`the page shows "the run cannot reach"`)
			}
			if refused.Load() == before {
				t.Errorf("the browser did not ask the run for %s", portal)
			}
		})
	}
}

// On a Mac, the host starts no WebKit: macOS would send its requests to the
// machine's own addresses past the run. The run starts its own, as it does
// without a host.
func TestTheHostStartsNoWebKitOnAMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("WebKit's network goes through the run here")
	}
	t.Setenv(cloudtest.HostedEnv, "1")
	portal := environment(t)
	refused := reachedFromTheRun(t, portal)
	h := cloudtest.NewWith(t, nil, Pack())
	h.OK("the portal web app with the following properties:", [][]string{{"url", portal}, {"engine", "webkit"}})
	h.OK(`the "/quote.html" page is opened`)
	h.OK(`the page shows "Get a quote"`)
	if refused.Load() != 0 {
		t.Error("a WebKit the run started asked the host's relay for its pages")
	}
	if isHosted(h.Suite, &App{Engine: "webkit"}) {
		t.Error("the host started a WebKit on a Mac")
	}
}

// A browser the host started for an agent's run has a clipboard of its
// own: what the person copied is not the run's to paste, and what a
// scenario copies does not replace it.
func TestAHostedBrowserKeepsToItsOwnClipboard(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the test reads and sets the Mac's clipboard (pbcopy, pbpaste)")
	}
	mine, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Skipf("no clipboard to read: %v", err)
	}
	copyToMac := func(text string) {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { copyToMac(string(mine)) })
	const secret = "depot-door-code-4471"
	copyToMac(secret)

	t.Setenv(cloudtest.HostedEnv, "1")
	for _, engine := range []string{"chromium"} {
		t.Run(engine, func(t *testing.T) {
			h, app := newHarness(t, engine)
			h.OK("the portal web app with the following properties:", app)
			h.OK(`the "/board.html" page is opened`)
			h.OK(`the Meta+V key is pressed in the "Scan a parcel" field`)
			if err := h.Step(`within 1s the "Scan a parcel" field has the value "` + secret + `"`); err == nil {
				t.Fatal("the run pasted what the person copied on the Mac")
			}
			h.OK(`the "Scan a parcel" field is filled with "PX-4101"`)
			h.OK(`the Meta+A key is pressed in the "Scan a parcel" field`)
			h.OK(`the Meta+C key is pressed in the "Scan a parcel" field`)
			got, err := exec.Command("pbpaste").Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != secret {
				t.Errorf("the Mac's clipboard is %q after the scenario copied: want it as the person left it", got)
			}
			// The browser's own clipboard has what the scenario copied.
			h.OK(`the "Scan a parcel" field is filled with '""'`)
			h.OK(`the Meta+V key is pressed in the "Scan a parcel" field`)
			h.OK(`the "Scan a parcel" field has the value "PX-4101"`)
			if !isHosted(h.Suite, &App{Engine: engine}) {
				t.Error("the browser was not the host's")
			}
		})
	}
}

// A tool that drives Chromium itself (Lighthouse) attaches to its remote
// debugging port, which takes every DevTools command: a browser the host
// started has none, and the scenario says so.
func TestOtherToolsCannotDriveAHostedBrowser(t *testing.T) {
	t.Setenv(cloudtest.HostedEnv, "1")
	h, app := newHarness(t, "chromium")
	RemoteDebugging(h.Suite)
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	c := &Current{s: scenarioPages.Of(h.SC).current, suite: h.Suite}
	_, err := c.DebuggingURL()
	if err == nil || !strings.Contains(err.Error(), "is one the axx host started for this run") || !strings.Contains(err.Error(), "outside the sandbox") {
		t.Errorf("the browser's debugging address: %v", err)
	}
}
