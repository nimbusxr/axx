// Package lighthouse is the web-lighthouse pack: a web app's pages audited
// with Lighthouse, Google's tool for the quality of web pages, in the
// browser the scenario already uses: their performance, accessibility, best
// practices and SEO scores, and how fast they load by the Core Web Vitals.
// The pack downloads Lighthouse the first time and runs it with the web
// pack's Node.js; Lighthouse drives the browser through its remote
// debugging port. It builds on the web-core pack.
package lighthouse

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// Name is the pack's name.
const Name = "web-lighthouse"

const since = "0.1.1"

// Pack returns the web-lighthouse pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{webcore.Name},
		ConfigSchema: []byte(configSchema), Steps: steps(),
		Hooks: []core.Hook{{ID: Name + ".debugging", Phase: core.BeforeStep, Run: debugging}},
	}
}

const packDoc = `Audit a web app's pages with [Lighthouse](https://developer.chrome.com/docs/lighthouse), Google's tool for the quality of web pages. It builds on the ` + "`web-core`" + ` pack.

- **Scores.** ` + "`the {string} page scores at least:`" + ` checks the page's Lighthouse scores, from 0 to 100: performance, accessibility, best practices and SEO. A score below its minimum fails the step, which says which audits cost the page the most.
- **Loading.** ` + "`the {string} page loads within:`" + ` checks how fast the page loads, by the Core Web Vitals and Lighthouse's other metrics: largest contentful paint, first contentful paint, total blocking time, speed index and cumulative layout shift.

Lighthouse loads the page afresh in a tab of its own, beside the scenario's, with the web app's cookies and storage: signed-in pages are audited signed in. As for a first visit, it clears the browser's cache and the site's service workers first, for the scenario's tabs too. It measures the page as a mid-range phone on a slow 4G connection loads it, or a desktop computer (` + "`device`" + `), simulating the device and the connection rather than slowing the browser down: an audit takes about 5 seconds. Performance varies with the machine's load, so the pack audits it three times and judges the median run, as Lighthouse recommends; still, leave some room. Each audit attaches Lighthouse's report, which the pack also keeps in ` + "`.axx/web/lighthouse`" + `.

Lighthouse audits pages in Chromium, Chrome and Edge. The pack downloads Lighthouse ` + lighthouseVersion + ` the first time, about 18 MB from npm's registry (` + "`npm_config_registry`" + ` points it at a mirror), and runs it with the web-core pack's Node.js.

` + "```yaml" + `
packs:
  web-lighthouse:
    device: desktop    # mobile (the default) or desktop
` + "```"

// configSchema is the pack's section of axx.yaml, packs.web-lighthouse.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "device": {"type": "string", "enum": ["mobile", "desktop"], "description": "The device Lighthouse audits pages as: mobile, a mid-range phone on a slow 4G connection (the default), or desktop, Lighthouse's desktop preset."}
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	Device string `json:"device"`
}

type settings struct {
	device string // mobile or desktop
}

var devices = []string{"mobile", "desktop"}

func settingsFor(s *core.Suite) (*settings, error) {
	return core.Cached(s, Name+"/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		return parseConfig(c)
	})
}

func parseConfig(c Config) (*settings, error) {
	st := &settings{device: c.Device}
	switch st.device {
	case "":
		st.device = "mobile"
	case "mobile", "desktop":
	default:
		return nil, fmt.Errorf("packs.%s.device: %q is not one of %s", Name, c.Device, strings.Join(devices, ", "))
	}
	return st, nil
}

// debugging has the run's Chromium browsers open their remote debugging
// port, which Lighthouse drives them through, before the first scenario
// launches one: a browser is launched once per run. (A step hook, not a
// scenario one: those show in every scenario's report.)
func debugging(sc *core.Scenario) error {
	_, err := core.Cached(sc.Suite(), Name+"/debugging", func() (bool, error) {
		webcore.RemoteDebugging(sc.Suite())
		return true, nil
	})
	return err
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".scores", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {string} page scores at least:",
			Doc: "Audit a page of the web app with Lighthouse, a path below its `url` or a whole URL, and check its scores.\n\n" +
				"- Lighthouse audits the categories listed only, as the `device` of `packs.web-lighthouse` (a phone by default).\n" +
				"- With performance among the categories, Lighthouse audits the page three times, and the step judges the median run.\n" +
				"- A score below its minimum fails the step, which lists the audits that cost the page the most.\n" +
				"- Lighthouse's report is attached.",
			Table: &core.TableDoc{
				Columns: []string{"category", "score"},
				Rows: []core.TableRow{
					{Name: "performance", Takes: "the lowest score it may have, from 0 to 100"},
					{Name: "accessibility", Takes: "the lowest score it may have, from 0 to 100"},
					{Name: "best practices", Takes: "the lowest score it may have, from 0 to 100"},
					{Name: "seo", Takes: "the lowest score it may have, from 0 to 100"},
				},
			},
			Examples: []string{"Then the \"/quote\" page scores at least:\n  | performance    | 90  |\n  | accessibility  | 100 |\n  | best practices | 100 |\n  | seo            | 100 |"},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, a core.Args) error {
				page := webcore.Text(sc, a, 0)
				mins, err := parseScores(func(v string) string { return webcore.Expand(sc, v) }, a.Table)
				if err != nil {
					return err
				}
				ids := make([]string, len(mins))
				for i, m := range mins {
					ids[i] = m.id
				}
				what, r, err := audit(sc, c, page, ids)
				if err != nil {
					return err
				}
				return checkScores(sc, what, r, mins)
			}),
		},
		{
			ID: Name + ".loads", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {string} page loads within:",
			Doc: "Audit how fast a page of the web app loads with Lighthouse, a path below its `url` or a whole URL, and check " +
				"its metrics.\n\n" +
				"- Lighthouse measures the page as the `device` of `packs.web-lighthouse` would load it (a phone on a slow 4G " +
				"connection by default).\n" +
				"- Lighthouse audits the page three times, and the step judges the median run: a load's metrics vary with the machine's load.\n" +
				"- A metric above its limit fails the step, which lists what would bring it down.\n" +
				"- Lighthouse's report is attached.",
			Table: &core.TableDoc{
				Columns: []string{"metric", "limit"},
				Rows: []core.TableRow{
					{Name: "largest contentful paint", Takes: "the longest it may take, like `2.5s` (a Core Web Vital)"},
					{Name: "cumulative layout shift", Takes: "the most it may be, a number like `0.1` (a Core Web Vital)"},
					{Name: "first contentful paint", Takes: "the longest it may take, like `1.8s`"},
					{Name: "total blocking time", Takes: "the longest it may be, like `200ms`"},
					{Name: "speed index", Takes: "the longest it may take, like `3.4s`"},
				},
			},
			Examples: []string{"Then the \"/quote\" page loads within:\n  | largest contentful paint | 2.5s  |\n  | total blocking time      | 200ms |\n  | cumulative layout shift  | 0.1   |"},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, a core.Args) error {
				page := webcore.Text(sc, a, 0)
				limits, err := parseLimits(func(v string) string { return webcore.Expand(sc, v) }, a.Table)
				if err != nil {
					return err
				}
				what, r, err := audit(sc, c, page, []string{"performance"})
				if err != nil {
					return err
				}
				return checkMetrics(sc, what, r, limits)
			}),
		},
	}
}
