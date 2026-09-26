// Package a11y is the web-a11y pack: pages people can use whatever their
// abilities. It audits pages with axe-core, the accessibility engine of
// Deque, which it downloads the first time, and checks their structure as
// assistive technology reads it, with Playwright. It builds on the web-core pack.
package a11y

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	webcore "github.com/nimbusxr/axx/packs/web/core"
	"github.com/nimbusxr/axx/packs/web/internal/driver"
)

// Name is the pack's name.
const Name = "web-a11y"

const since = "0.1.1"

// axeCore is the axe-core the pack injects into pages (MPL-2.0, Deque
// Systems): downloaded, never distributed with axx.
var axeCore = driver.Package{
	Name: "axe-core", Version: "4.13.0",
	Integrity: "sha512-UzGt8zg7Ny8djbYMhxl2zuEevVa7r2gJjYY5Lwr1xM7+XU2nd6CkIWFTVcCIbAP63vSz71NaVyyuSk9lHKcy0A==",
	File:      "axe.min.js",
}

// Pack returns the web-a11y pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{webcore.Name},
		ConfigSchema: []byte(configSchema), Steps: steps(),
	}
}

const packDoc = `Check that pages are usable whatever the abilities of the people who use them. It builds on the ` + "`web-core`" + ` pack.

- **Audits.** ` + "`the page has no accessibility violations`" + ` audits the page with [axe-core](https://github.com/dequelabs/axe-core), the accessibility engine of Deque that Lighthouse runs too, against the WCAG 2.1 AA rules by default: fields without labels, text without enough contrast, images without alternative text... The pack downloads axe-core ` + axeVersionDoc + ` the first time. A violation fails the step, which says which rule, how serious, and which elements.
- **Structure.** ` + "`the page's accessible structure is:`" + ` checks what assistive technology reads: the headings, fields, buttons and texts, and their names, as Playwright's ARIA snapshots describe them. List what matters; the rest of the page may be anything.

` + "```yaml" + `
packs:
  web-a11y:
    standard: wcag21aa     # wcag2a, wcag2aa, wcag21a, wcag21aa or wcag22aa
    ignore: [region]       # rules to leave out, by their axe-core id
` + "```"

const axeVersionDoc = "4.13.0"

// configSchema is the pack's section of axx.yaml, packs.web-a11y.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "standard": {"type": "string", "enum": ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"], "description": "The WCAG level pages are audited against (default wcag21aa)."},
    "ignore": {"type": "array", "items": {"type": "string"}, "description": "Rules the audits leave out, by their axe-core id, like color-contrast."}
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	Standard string   `json:"standard"`
	Ignore   []string `json:"ignore"`
}

type settings struct {
	standard string
	tags     []string // axe-core's tags of the standard, and those it includes
	ignore   []string
}

var standards = []string{"wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"}

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
	st := &settings{standard: c.Standard, ignore: c.Ignore}
	if st.standard == "" {
		st.standard = "wcag21aa"
	}
	i := slices.Index(standards, st.standard)
	if i < 0 {
		return nil, fmt.Errorf("packs.%s.standard: %q is not one of %s", Name, c.Standard, strings.Join(standards, ", "))
	}
	// A level includes those below it: 2.1 AA is 2.0 A and AA, and 2.1 A and AA.
	for _, s := range standards[:i+1] {
		if strings.HasSuffix(st.standard, "aa") || !strings.HasSuffix(s, "aa") {
			st.tags = append(st.tags, s)
		}
	}
	return st, nil
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".page", Keyword: "Then", Since: since,
			Expr: "the page has no accessibility violations",
			Doc: "Audit the page, in every frame, with axe-core against the WCAG rules of `packs.web-a11y.standard` " +
				"(2.1 AA by default): the step fails with each rule violated, how serious it is, and the elements that violate it.",
			Examples: []string{"Then the page has no accessibility violations"},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, _ core.Args) error {
				return audit(sc, c, nil, "The page")
			}),
		},
		{
			ID: Name + ".element", Keyword: "Then", Since: since,
			Expr: "the {string} {element} has no accessibility violations",
			Doc:  "Audit one part of the page, by its name or a selector, with axe-core, as `the page has no accessibility violations` does.",
			Examples: []string{
				`Then the "css=form" element has no accessibility violations`,
			},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, a core.Args) error {
				name := webcore.Text(sc, a, 0)
				k := a.Value(1).(webcore.Kind)
				loc, err := c.Find(sc, k, name, cloudstep.DefaultWait)
				if err != nil {
					return err
				}
				return audit(sc, c, loc, fmt.Sprintf("The %q %s", name, k.Noun()))
			}),
		},
		{
			ID: Name + ".structure", Keyword: "Then", Arg: core.ArgDocString, Since: since,
			Expr: "[[within {duration} ]]the page's accessible structure is:",
			Doc: "Check what assistive technology reads on the page, in Playwright's ARIA snapshot format.\n\n" +
				"- A line is a role and a name, like `- heading \"Get a quote\" [level=1]` or `- button \"Get a quote\"`, " +
				"nested for what is inside.\n" +
				"- Only what is listed is checked, in its order.\n" +
				"- A name between slashes is a regular expression.\n" +
				"- The check waits for the page to match: 10 seconds, or `within {duration}`.",
			Examples: []string{"Then the page's accessible structure is:"},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, a core.Args) error {
				pg, err := c.Page()
				if err != nil {
					return err
				}
				return structure(sc, a, "The page", pg.Locator("body"))
			}),
		},
		{
			ID: Name + ".element.structure", Keyword: "Then", Arg: core.ArgDocString, Since: since,
			Expr: "[[within {duration} ]]the {string} {element}'s accessible structure is:",
			Doc:  "Check what assistive technology reads in one part of the page, by its name or a selector, as `the page's accessible structure is:` does.",
			Examples: []string{
				`Then the "css=form" element's accessible structure is:`,
			},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, a core.Args) error {
				name := webcore.Text(sc, a, 1)
				k := a.Value(2).(webcore.Kind)
				loc, err := c.Find(sc, k, name, cloudstep.Wait(a, 0))
				if err != nil {
					return err
				}
				return structure(sc, a, fmt.Sprintf("The %q %s", name, k.Noun()), loc)
			}),
		},
	}
}

// violation is one rule axe-core found violated.
type violation struct {
	ID     string `json:"id"`
	Impact string `json:"impact"`
	Help   string `json:"help"`
	URL    string `json:"helpUrl"`
	Nodes  []struct {
		Target []any `json:"target"`
	} `json:"nodes"`
}

// audit runs axe-core on the page, or on one element of it.
func audit(sc *core.Scenario, c *webcore.Current, loc playwright.Locator, what string) error {
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	pg, err := c.Page()
	if err != nil {
		return err
	}
	if err := inject(sc, pg); err != nil {
		return err
	}
	rules := map[string]any{}
	for _, id := range cfg.ignore {
		rules[id] = map[string]any{"enabled": false}
	}
	opts := map[string]any{
		"runOnly":     map[string]any{"type": "tag", "values": cfg.tags},
		"rules":       rules,
		"resultTypes": []string{"violations"},
	}
	var raw any
	if loc == nil {
		raw, err = pg.Evaluate(`async (opts) => (await axe.run(document, opts)).violations`, opts)
	} else {
		raw, err = loc.Evaluate(`async (el, opts) => (await axe.run(el, opts)).violations`, opts)
	}
	if err != nil {
		return fmt.Errorf("cannot audit the page: %s", firstLine(err))
	}
	b, _ := json.Marshal(raw)
	var found []violation
	if err := json.Unmarshal(b, &found); err != nil {
		return err
	}
	if len(found) == 0 {
		return nil
	}
	sc.Attach("application/json", b, "accessibility violations")
	var lines []string
	for _, v := range found {
		var targets []string
		for _, n := range v.Nodes {
			targets = append(targets, target(n.Target))
		}
		shown := targets
		if len(shown) > 5 {
			shown = append(shown[:5:5], fmt.Sprintf("and %d more", len(targets)-5))
		}
		lines = append(lines, fmt.Sprintf("  %s (%s): %s; %d %s: %s\n    %s", v.ID, v.Impact, v.Help, len(v.Nodes),
			plural(len(v.Nodes), "element", "elements"), strings.Join(shown, ", "), v.URL))
	}
	return core.Failf("%s has %d accessibility %s (%s):\n%s", what, len(found), plural(len(found), "violation", "violations"),
		cfg.standard, strings.Join(lines, "\n"))
}

// inject puts axe-core in each frame of the page that has not got it: it
// checks only the frames it is in.
func inject(sc *core.Scenario, pg playwright.Page) error {
	path, err := core.Cached(sc.Suite(), Name+"/axe-core", func() (string, error) {
		p, err := axeCore.Fetch(context.Background(), driver.Options{})
		if err != nil {
			return "", fmt.Errorf("cannot download axe-core: %w", err)
		}
		return p, nil
	})
	if err != nil {
		return err
	}
	source, err := core.Cached(sc.Suite(), Name+"/axe-source", func() (string, error) {
		b, err := os.ReadFile(path)
		return string(b), err
	})
	if err != nil {
		return err
	}
	for _, f := range pg.Frames() {
		has, err := f.Evaluate(`() => typeof window.axe !== 'undefined'`)
		if err != nil || has == true {
			continue
		}
		// Evaluated rather than added as a script: a page's Content-Security-Policy
		// does not stop it.
		if _, err := f.Evaluate(source + "\n;axe.configure({branding: {application: 'axx'}});undefined"); err != nil && f == pg.MainFrame() {
			return fmt.Errorf("cannot put axe-core in the page: %s", firstLine(err))
		}
	}
	return nil
}

// target is how axe-core points at an element: a selector, or selectors
// through frames and shadow roots.
func target(t []any) string {
	var parts []string
	for _, p := range t {
		switch v := p.(type) {
		case string:
			parts = append(parts, v)
		case []any:
			parts = append(parts, target(v))
		}
	}
	return strings.Join(parts, " > ")
}

// structure checks a part of the page's accessible structure against the
// step's doc string.
func structure(sc *core.Scenario, a core.Args, what string, loc playwright.Locator) error {
	want := strings.TrimSpace(webcore.Expand(sc, a.DocString.Content))
	var parsed any
	if err := yaml.Unmarshal([]byte(want), &parsed); err != nil {
		return fmt.Errorf("the accessible structure is no ARIA snapshot: %s\n  a name or a text with \": \" in it needs quotes, like \"/Price: \\\\d+ EUR/\"",
			strings.TrimSpace(err.Error()))
	}
	wait := cloudstep.Wait(a, 0)
	err := playwright.NewPlaywrightAssertions(float64(wait.Milliseconds())).Locator(loc).ToMatchAriaSnapshot(want)
	if err == nil {
		return nil
	}
	got, serr := loc.AriaSnapshot()
	if serr != nil {
		return errors.Join(err, serr)
	}
	return core.Failf("%s's accessible structure is not as expected; it reads:\n  %s", what, strings.ReplaceAll(strings.TrimSpace(got), "\n", "\n  "))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func firstLine(err error) string {
	s, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSpace(s)
}
