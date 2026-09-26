// Package network is the web-network pack: what a web app's pages fetch,
// seen and changed from the browser. A third-party script that is down, an
// API that answers late, a slow connection: pages that call other services
// themselves, which a mock of the app's own dependencies does not cover. It
// builds on the web-core pack.
package network

import (
	"bytes"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// Name is the pack's name.
const Name = "web-network"

const since = "0.1.1"

// Pack returns the web-network pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{webcore.Name},
		ConfigSchema: []byte(configSchema), Steps: steps(),
		Hooks: []core.Hook{{ID: Name + ".listen", Phase: core.BeforeStep, Run: listen}},
	}
}

const packDoc = `See and change what a web app's pages fetch, from the browser: requests that fail, answer with an error or a file, or take their time, a slow connection, a recording of the pages' traffic to answer from, and the messages of their websockets. It builds on the ` + "`web-core`" + ` pack.

A request is named by its address, a path below the web app's ` + "`url`" + ` (` + "`/api/prices`" + `) or a whole URL (` + "`https://maps.example.com/tiles/**`" + `): ` + "`*`" + ` stands for any part of a path, ` + "`**`" + ` for any parts; the query does not count unless the address has one. What a step sets holds for every web app of the scenario, from the pages they open next: set it before the page opens.

The pages' own requests only: for the services your app calls from its server, mock them instead (the ` + "`mock`" + ` pack). ` + "`record: true`" + ` in ` + "`axx.yaml`" + ` records what each web app's pages fetch, for a scenario to answer from later.`

// configSchema is the pack's section of axx.yaml, packs.web-network.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "record": {"type": "boolean", "description": "Record what each scenario's pages fetch, a HAR file for each web app in .axx/web/recordings, for scenarios to answer from (default false)."}
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	Record bool `json:"record"`
}

func settingsFor(s *core.Suite) (*Config, error) {
	return core.Cached(s, Name+"/settings", func() (*Config, error) {
		var c Config
		return &c, s.PackConfig(Name, &c)
	})
}

// messages are the websocket messages a scenario's pages sent and received.
type messages struct {
	mu        sync.Mutex
	listening bool
	sent      []string
	received  []string
}

var scenarioMessages = core.NewStateKey(Name+"/messages", func(*core.Scenario) *messages { return &messages{} }, nil)

// listen listens to the websockets of the scenario's pages from its first
// step, and records their traffic when the settings say so. (A step hook,
// not a scenario one: those show in every scenario's report.)
func listen(sc *core.Scenario) error {
	ms := scenarioMessages.Of(sc)
	ms.mu.Lock()
	started := ms.listening
	ms.listening = true
	ms.mu.Unlock()
	if started {
		return nil
	}
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	return webcore.OnContext(sc, func(c webcore.Context) error {
		listen := func(p playwright.Page) {
			p.OnWebSocket(func(ws playwright.WebSocket) {
				ws.OnFrameSent(func(b []byte) { ms.add(&ms.sent, b) })
				ws.OnFrameReceived(func(b []byte) { ms.add(&ms.received, b) })
			})
		}
		c.OnPage(listen)
		for _, p := range c.Pages() {
			listen(p)
		}
		if cfg.Record {
			path := c.File(sc, "recordings", ".har")
			if err := c.RouteFromHAR(path, playwright.BrowserContextRouteFromHAROptions{
				Update: playwright.Bool(true), UpdateContent: playwright.RouteFromHarUpdateContentPolicyEmbed,
				UpdateMode: playwright.HarModeMinimal, NotFound: playwright.HarNotFoundFallback,
			}); err != nil {
				return fmt.Errorf("cannot record the %s web app's traffic: %w", c.App(), err)
			}
		}
		return nil
	})
}

func (ms *messages) add(to *[]string, b []byte) {
	ms.mu.Lock()
	*to = append(*to, string(b))
	ms.mu.Unlock()
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".fail", Keyword: "Given", Since: since,
			Expr: "the page's requests to {string} fail",
			Doc: "Make the pages' requests to an address fail, as when a service is down or out of reach: a path below " +
				"the web app's url, or a whole URL, with `*` and `**`.",
			Examples: []string{`Given the page's requests to "https://maps.example.com/**" fail`},
			Run:      route(func(r playwright.Route, _ core.Args) error { return r.Abort("failed") }),
		},
		{
			ID: Name + ".status", Keyword: "Given", Since: since,
			Expr: "the page's requests to {string} answer with status {int}",
			Doc:  "Answer the pages' requests to an address with a status and nothing else, like 503.",
			Examples: []string{
				`Given the page's requests to "/api/prices" answer with status 503`,
			},
			Run: route(func(r playwright.Route, a core.Args) error {
				return r.Fulfill(playwright.RouteFulfillOptions{Status: playwright.Int(a.Int(1))})
			}),
		},
		{
			ID: Name + ".file", Keyword: "Given", Since: since,
			Expr: "the page's requests to {string} answer with the {filepath} file",
			Doc:  "Answer the pages' requests to an address with a file of the project, with the type of its extension.",
			Examples: []string{
				`Given the page's requests to "/api/prices" answer with the network/prices-berlin.json file`,
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				path, err := sc.Suite().ResolvePath(a.String(1))
				if err != nil {
					return err
				}
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				kind := mime.TypeByExtension(filepath.Ext(path))
				if kind == "" {
					kind = "application/octet-stream"
				}
				return route(func(r playwright.Route, _ core.Args) error {
					return r.Fulfill(playwright.RouteFulfillOptions{Status: playwright.Int(200), ContentType: &kind, Body: bytes.Clone(body)})
				})(sc, a)
			},
		},
		{
			ID: Name + ".slow", Keyword: "Given", Since: since,
			Expr: "the page's requests to {string} take {duration}",
			Doc:  "Make the pages' requests to an address take that long before they go out, as a busy service does.",
			Examples: []string{
				`Given the page's requests to "/api/prices" take 3s`,
			},
			Run: route(func(r playwright.Route, a core.Args) error {
				time.Sleep(a.Value(1).(time.Duration))
				return r.Fallback()
			}),
		},
		{
			ID: Name + ".connection.slow", Keyword: "Given", Since: since,
			Expr: "the browser's connection is slow",
			Doc: "Make the browser's connection slow, as a phone's in a bad spot: 400 ms of latency and 500 kbit/s each way. " +
				"Chromium, Chrome and Edge only.",
			Examples: []string{"Given the browser's connection is slow"},
			Run: func(sc *core.Scenario, _ core.Args) error {
				// Before a page loads anything, or its first requests go out fast.
				return webcore.OnPage(sc, func(c webcore.Context, p playwright.Page) error {
					switch c.Engine() {
					case "chromium", "chrome", "msedge":
					default:
						return core.Failf("the browser's connection can be slowed in Chromium, Chrome and Edge only, not in %s", c.Engine())
					}
					s, err := c.NewCDPSession(p)
					if err != nil {
						return err
					}
					if _, err := s.Send("Network.enable", nil); err != nil {
						return err
					}
					_, err = s.Send("Network.emulateNetworkConditions", map[string]any{
						"offline": false, "latency": 400, "downloadThroughput": 500 * 1024 / 8, "uploadThroughput": 500 * 1024 / 8,
					})
					return err
				})
			},
		},
		{
			ID: Name + ".recording", Keyword: "Given", Since: since,
			Expr: "the page's requests are answered from the {filepath} recording",
			Doc: "Answer the pages' requests from a recording of what they fetched, a HAR file (`record: true` makes " +
				"them): the requests it has, it answers; the others go out, and so do websockets.",
			Examples: []string{`Given the page's requests are answered from the network/quote.har recording`},
			Run: func(sc *core.Scenario, a core.Args) error {
				path, err := sc.Suite().ResolvePath(a.String(0))
				if err != nil {
					return err
				}
				return webcore.OnContext(sc, func(c webcore.Context) error {
					if err := answering(c); err != nil {
						return err
					}
					// Answered from a recording, a websocket's handshake would
					// open nothing: websockets go out.
					return c.RouteFromHAR(path, playwright.BrowserContextRouteFromHAROptions{
						NotFound: playwright.HarNotFoundFallback,
						URL: func(u string) bool {
							return !strings.HasPrefix(u, "ws:") && !strings.HasPrefix(u, "wss:")
						},
					})
				})
			},
		},
		{
			ID: Name + ".ws.sent", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the page sent a websocket message containing {string}",
			Doc:      "Check that a page of the scenario sent a websocket message with that text in it. The check waits: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the page sent a websocket message containing "subscribe:PX-4101"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				return checkMessages(sc, a, "sent", func(ms *messages) []string { return ms.sent })
			},
		},
		{
			ID: Name + ".ws.received", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the page received a websocket message containing {string}",
			Doc:      "Check that a page of the scenario received a websocket message with that text in it. The check waits: 10 seconds, or `within {duration}`.",
			Examples: []string{`Then the page received a websocket message containing "IN_TRANSIT"`},
			Run: func(sc *core.Scenario, a core.Args) error {
				return checkMessages(sc, a, "received", func(ms *messages) []string { return ms.received })
			},
		},
	}
}

// route is a step that answers the pages' requests to the address of its
// first argument, in every web app of the scenario, with handle.
func route(handle func(playwright.Route, core.Args) error) core.StepFunc {
	return func(sc *core.Scenario, a core.Args) error {
		pattern := webcore.Text(sc, a, 0)
		return webcore.OnContext(sc, func(c webcore.Context) error {
			matches, err := matcher(c.URL(), pattern)
			if err != nil {
				return err
			}
			if err := answering(c); err != nil {
				return err
			}
			return c.Route(matches, func(r playwright.Route) {
				if err := handle(r, a); err != nil {
					sc.Log("the %s request to %s could not be answered: %v", r.Request().Method(), r.Request().URL(), err)
				}
			})
		})
	}
}

// answering readies a context for pages the pack answers: Chromium takes
// a page it did not get from the network for a page of the internet, which
// may not reach the machine's own addresses (Local Network Access), where
// the app's websockets and services are.
func answering(c webcore.Context) error {
	switch c.Engine() {
	case "chromium", "chrome", "msedge":
		return c.GrantPermissions([]string{"local-network-access"})
	}
	return nil
}

// matcher matches the addresses a pattern names: a path below the app's URL
// or a whole URL, * any part of a path, ** any parts, the query only when
// the pattern has one.
func matcher(appURL, pattern string) (func(string) bool, error) {
	if !strings.HasPrefix(pattern, "http://") && !strings.HasPrefix(pattern, "https://") {
		pattern = strings.TrimRight(appURL, "/") + "/" + strings.TrimLeft(pattern, "/")
	}
	withQuery := strings.Contains(pattern, "?")
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch {
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case pattern[i] == '*':
			b.WriteString("[^/?]*")
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("the address %q is not one a request can have: %w", pattern, err)
	}
	return func(address string) bool {
		if !withQuery {
			if u, err := url.Parse(address); err == nil {
				u.RawQuery, u.Fragment = "", ""
				address = u.String()
			}
		}
		return re.MatchString(address)
	}, nil
}

// checkMessages checks that a page sent or received a message with a text.
func checkMessages(sc *core.Scenario, a core.Args, what string, of func(*messages) []string) error {
	want := webcore.Text(sc, a, 1)
	ms := scenarioMessages.Of(sc)
	var seen []string
	ok, err := webcore.WaitUntil(sc, cloudstep.Wait(a, 0), func() (bool, error) {
		ms.mu.Lock()
		seen = append(seen[:0], of(ms)...)
		ms.mu.Unlock()
		for _, m := range seen {
			if strings.Contains(m, want) {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil || ok {
		return err
	}
	var shown []string
	for _, m := range seen {
		shown = append(shown, fmt.Sprintf("%q", m))
	}
	return core.Failf("No websocket message the pages %s contains %q; %s", what, want, cloudstep.Shown("messages "+what, shown, 10))
}
