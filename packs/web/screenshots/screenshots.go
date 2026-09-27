// Package screenshots is the web-screenshots pack: pages that look as
// designed, compared with screenshots taken before, pixel by pixel. It builds
// on the web-core pack.
//
// Browsers draw the same page differently on each operating system (fonts
// first), so screenshots are kept per engine and operating system, each
// compared on its own platform, and a project compares on the platforms it
// keeps screenshots for only.
package screenshots

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	webcore "github.com/nimbusxr/axx/packs/web/core"
)

// Name is the pack's name.
const Name = "web-screenshots"

const since = "0.1.1"

// Pack returns the web-screenshots pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Requires: []string{webcore.Name},
		ConfigSchema: []byte(configSchema), Steps: steps(),
	}
}

const packDoc = `Check that pages look as designed: the page, or one element of it, compared with a screenshot taken before, pixel by pixel, as Playwright compares them (small differences of color and anti-aliased edges aside). It builds on the ` + "`web-core`" + ` pack.

The screenshots are files of the project, ` + "`screenshots/<name>.<engine>-<platform>.png`" + `: browsers draw pages differently on each operating system, so each engine and platform has its own, compared on that platform only. The first time, a step takes its screenshot and fails: look at it, and keep it.

` + "```yaml" + `
packs:
  web-screenshots:
    platforms: [linux, darwin] # the platforms whose screenshots you keep
` + "```" + `

With ` + "`platforms`" + `, the steps compare on those platforms (` + "`linux`" + `, ` + "`darwin`" + `, ` + "`windows`" + `), each with its own screenshots, and pass elsewhere, saying so. ` + "`update: true`" + ` takes the screenshots of the platform it runs on again.`

// configSchema is the pack's section of axx.yaml, packs.web-screenshots.
const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "folder": {"type": "string", "description": "The folder of the screenshots, in the project (default screenshots)."},
    "platforms": {"type": "array", "items": {"type": "string", "enum": ["linux", "darwin", "windows"]}, "description": "The platforms whose screenshots the project keeps: each compares its own; elsewhere the steps pass without comparing (default: every platform)."},
    "tolerance": {"type": "number", "minimum": 0, "maximum": 1, "description": "The share of a screenshot's pixels that may differ (default 0)."},
    "update": {"type": "boolean", "description": "Take every screenshot again, as the page looks now, instead of comparing."}
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	Folder    string   `json:"folder"`
	Platforms []string `json:"platforms"`
	Tolerance float64  `json:"tolerance"`
	Update    bool     `json:"update"`
}

type settings struct {
	folder    string // absolute
	platforms []string
	tolerance float64
	update    bool
}

func settingsFor(s *core.Suite) (*settings, error) {
	return core.Cached(s, Name+"/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		return parseConfig(c, s.ProjectDir())
	})
}

func parseConfig(c Config, projectDir string) (*settings, error) {
	if c.Tolerance < 0 || c.Tolerance > 1 {
		return nil, fmt.Errorf("packs.%s.tolerance: %v is not a share between 0 and 1", Name, c.Tolerance)
	}
	folder := c.Folder
	if folder == "" {
		folder = "screenshots"
	}
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(projectDir, filepath.FromSlash(folder))
	}
	return &settings{folder: folder, platforms: c.Platforms, tolerance: c.Tolerance, update: c.Update}, nil
}

// platform is the operating system screenshots are taken on here.
var platform = runtime.GOOS

var screenshotName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]*$`)

const stepDoc = "\n\n" +
	"- The screenshot is `<name>.<engine>-<platform>.png`, in the project's `screenshots` folder. Without one, the step " +
	"takes it and fails: look at it, and keep it.\n" +
	"- When the page looks different, the step attaches the screenshot, the page and their difference.\n" +
	"- The check waits for the page to settle and look right: 10 seconds, or `within {duration}`.\n" +
	"- On a platform `platforms` leaves out, the step passes without comparing."

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".page", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the page looks like the {string} screenshot",
			Doc:      "Check that the page looks like its screenshot, pixel by pixel (anti-aliasing aside)." + stepDoc,
			Examples: []string{`Then the page looks like the "quote" screenshot`},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, a core.Args) error {
				return looksLike(sc, c, webcore.Text(sc, a, 1), cloudstep.Wait(a, 0), false, func(pg playwright.Page) ([]byte, error) {
					return pg.Screenshot(pageShot(nil))
				})
			}),
		},
		{
			ID: Name + ".page.masked", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the page looks like the {string} screenshot, apart from:",
			Doc: "Check that the page looks like its screenshot, apart from what changes from run to run, such as a date, " +
				"painted over in both." + stepDoc,
			Table:    &core.TableDoc{Columns: []string{"element"}, Note: "An element a row: by its text or label, or a selector like `css=time`."},
			Examples: []string{"Then the page looks like the \"parcel\" screenshot, apart from:\n  | css=time |"},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, a core.Args) error {
				var masks []string
				for _, row := range a.Table.Rows {
					for _, cell := range row {
						if cell = strings.TrimSpace(webcore.Expand(sc, cell)); cell != "" {
							masks = append(masks, cell)
						}
					}
				}
				return looksLike(sc, c, webcore.Text(sc, a, 1), cloudstep.Wait(a, 0), true, func(pg playwright.Page) ([]byte, error) {
					var locs []playwright.Locator
					for _, m := range masks {
						found, err := c.Anything(m)
						if err != nil {
							return nil, err
						}
						locs = append(locs, found...)
					}
					return pg.Screenshot(pageShot(locs))
				})
			}),
		},
		{
			ID: Name + ".element", Keyword: "Then", Since: since,
			Expr:     "[[within {duration} ]]the {string} {element} looks like the {string} screenshot",
			Doc:      "Check that one element, by its name or a selector, looks like its screenshot." + stepDoc,
			Examples: []string{`Then the "css=.quote-card" element looks like the "quote card" screenshot`},
			Run: webcore.Check(func(sc *core.Scenario, c *webcore.Current, a core.Args) error {
				loc, err := c.Find(sc, a.Value(2).(webcore.Kind), webcore.Text(sc, a, 1), cloudstep.Wait(a, 0))
				if err != nil {
					return err
				}
				return looksLike(sc, c, webcore.Text(sc, a, 3), cloudstep.Wait(a, 0), false, func(playwright.Page) ([]byte, error) {
					return loc.Screenshot(playwright.LocatorScreenshotOptions{
						Animations: playwright.ScreenshotAnimationsDisabled,
						Caret:      playwright.ScreenshotCaretHide,
					})
				})
			}),
		},
	}
}

func pageShot(mask []playwright.Locator) playwright.PageScreenshotOptions {
	return playwright.PageScreenshotOptions{
		FullPage:   playwright.Bool(true),
		Animations: playwright.ScreenshotAnimationsDisabled,
		Caret:      playwright.ScreenshotCaretHide,
		Mask:       mask,
		MaskColor:  playwright.String(fmt.Sprintf("#%02X%02X%02X", maskColor.R, maskColor.G, maskColor.B)),
	}
}

// looksLike compares what shoot takes with the screenshot name, once it
// settles: two screenshots in a row alike. With masked, what shoot painted
// over, in either screenshot, is left out.
func looksLike(sc *core.Scenario, c *webcore.Current, name string, wait time.Duration, masked bool, shoot func(playwright.Page) ([]byte, error)) error {
	if !screenshotName.MatchString(name) {
		return fmt.Errorf("the screenshot name %q is not a file name: use letters, digits, spaces, dots and dashes", name)
	}
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	if len(cfg.platforms) > 0 && !slices.Contains(cfg.platforms, platform) {
		sc.Log("the %q screenshot is compared on %s only, not on %s", name, strings.Join(cfg.platforms, ", "), platform)
		return nil
	}
	path := filepath.Join(cfg.folder, name+"."+c.Engine()+"-"+platform+".png")
	var expected []byte
	if !cfg.update {
		expected, err = os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	var prev, shot []byte
	var result comparison
	ok, err := webcore.WaitUntil(sc, wait, func() (bool, error) {
		pg, err := c.Page()
		if err != nil {
			return false, err
		}
		b, err := shoot(pg)
		if err != nil {
			return false, err
		}
		settled := bytes.Equal(b, prev)
		prev = b
		if !settled {
			return false, nil
		}
		shot = b
		if expected == nil {
			return true, nil
		}
		want, err := png.Decode(bytes.NewReader(expected))
		if err != nil {
			return false, fmt.Errorf("%s: %w", webcore.Relative(sc, path), err)
		}
		actual, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return false, err
		}
		result = compare(want, actual, masked)
		return result.sameSize && float64(result.differ) <= cfg.tolerance*float64(result.pixels), nil
	})
	if err != nil {
		return err
	}
	if shot == nil {
		shot = prev
	}
	if expected == nil {
		if shot == nil {
			return errors.New("cannot take a screenshot of the page")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, shot, 0o644); err != nil {
			return err
		}
		sc.Attach("image/png", shot, "screenshot taken")
		if cfg.update {
			sc.Log("the %q screenshot is taken again: %s", name, webcore.Relative(sc, path))
			return nil
		}
		return core.Failf("There was no %q screenshot to compare the page with: the page, as it is, is now %s; look at it, keep it, and run again",
			name, webcore.Relative(sc, path))
	}
	if ok {
		return nil
	}
	actualPath := c.File(sc, "screenshots", "-"+name+".png")
	if shot != nil {
		_ = os.WriteFile(actualPath, shot, 0o644)
		sc.Attach("image/png", expected, "expected screenshot")
		sc.Attach("image/png", shot, "the page")
	}
	if !result.sameSize {
		return core.Fail(fmt.Sprintf("The page does not look like the %q screenshot: its size differs", name),
			result.expectedSize, result.actualSize)
	}
	sc.Attach("image/png", mustPNG(result.diff), "difference")
	return core.Failf("The page does not look like the %q screenshot: %d of its %d pixels differ (%.2f%%, %.2f%% allowed); the page is %s",
		name, result.differ, result.pixels, 100*float64(result.differ)/float64(result.pixels), 100*cfg.tolerance, webcore.Relative(sc, actualPath))
}
