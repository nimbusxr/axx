package mobilecore

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/imagediff"
)

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "screenshots": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "folder": {"type": "string", "description": "The folder of the apps' screenshots, in the project (default screenshots)."},
        "tolerance": {"type": "number", "minimum": 0, "maximum": 1, "description": "The share of a screenshot's pixels that may differ (default 0)."},
        "update": {"type": "boolean", "description": "Take every screenshot again, as the app looks now, instead of comparing."}
      }
    }
  }
}`

// Config is the pack's settings, packs.mobile-core in axx.yaml.
type Config struct {
	Screenshots struct {
		Folder    string  `json:"folder"`
		Tolerance float64 `json:"tolerance"`
		Update    bool    `json:"update"`
	} `json:"screenshots"`
}

type settings struct {
	folder    string // absolute
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
	sc := c.Screenshots
	if sc.Tolerance < 0 || sc.Tolerance > 1 {
		return nil, fmt.Errorf("packs.%s.screenshots.tolerance: %v is not a share between 0 and 1", Name, sc.Tolerance)
	}
	folder := sc.Folder
	if folder == "" {
		folder = "screenshots"
	}
	if !filepath.IsAbs(folder) {
		folder = filepath.Join(projectDir, filepath.FromSlash(folder))
	}
	return &settings{folder: folder, tolerance: sc.Tolerance, update: sc.Update}, nil
}

var screenshotName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]*$`)

func screenshotSteps() []core.StepDef {
	return []core.StepDef{{
		ID: "mobile-core.screenshot", Keyword: "Then", Since: since,
		Expr: "[[within {duration} ]]the {word} app looks like the {string} screenshot",
		Doc: "Check that the app looks like its screenshot, pixel by pixel (anti-aliasing aside); the device's status and navigation bars, its clock among them, are left out.\n\n" +
			"- The screenshot is `<name>.<device>.png`, like `delivered.android-parcels-pixel.png`, in the project's `screenshots` folder: " +
			"each device has its own. Without one, the step takes it and fails: look at it, and keep it.\n" +
			"- When the app looks different, the step attaches the screenshot, the app and their difference.\n" +
			"- The check waits for the app to settle and look right: 10 seconds, or `within {duration}`.\n" +
			"- `packs.mobile-core.screenshots` in axx.yaml sets the `folder`, the `tolerance` (the share of pixels that may differ) " +
			"and `update: true`, which takes every screenshot again.",
		Examples: []string{`Then the courier app looks like the "delivered" screenshot`},
		Run: func(sc *core.Scenario, a core.Args) error {
			wait, app, name := cloudstep.Wait(a, 0), a.String(1), a.String(2)
			if !screenshotName.MatchString(name) {
				return fmt.Errorf("a screenshot's name is letters, digits, dots, dashes, underscores and spaces, not %q", name)
			}
			return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
				return looksLike(sc, ctx, d, app, name, wait)
			})
		},
	}}
}

// looksLike compares what the app shows, once it has settled, with its
// screenshot.
func looksLike(sc *core.Scenario, ctx context.Context, d Device, app, name string, wait time.Duration) error {
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	path := filepath.Join(cfg.folder, name+"."+d.ScreenKey()+".png")
	var shot *image.RGBA
	var last imagediff.Result
	var expected image.Image
	deadline := time.Now().Add(wait)
	for {
		a, err := maskedShot(ctx, d)
		if err != nil {
			return err
		}
		b, err := maskedShot(ctx, d)
		if err != nil {
			return err
		}
		settled := imagediff.Compare(a, b, imagediff.Options{Masked: true}).Differ == 0
		shot = b
		if settled {
			if cfg.update {
				return keep(sc, path, shot, true)
			}
			if expected == nil {
				expected, err = readPNG(path)
				if os.IsNotExist(err) {
					return keep(sc, path, shot, false)
				}
				if err != nil {
					return err
				}
			}
			last = imagediff.Compare(expected, shot, imagediff.Options{Masked: true})
			if last.SameSize && float64(last.Differ) <= cfg.tolerance*float64(last.Pixels) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if expected == nil {
		return core.Failf("The %s app did not settle within %s: what it shows kept changing", app, wait)
	}
	sc.Attach("image/png", encodePNG(expected), "expected screenshot")
	sc.Attach("image/png", encodePNG(shot), "the "+app+" app")
	if !last.SameSize {
		return core.Fail(fmt.Sprintf("The %s app is not the size of its %q screenshot (%s)", app, name, path), last.ExpectedSize, last.ActualSize)
	}
	sc.Attach("image/png", encodePNG(last.Diff), "difference")
	return core.Failf("The %s app does not look like its %q screenshot: %d pixels of %d differ (%.2f%%; %.2f%% may)",
		app, name, last.Differ, last.Pixels, 100*float64(last.Differ)/float64(last.Pixels), 100*cfg.tolerance)
}

// maskedShot is a screenshot with the device's system bars masked.
func maskedShot(ctx context.Context, d Device) (*image.RGBA, error) {
	b, err := d.Session().Screenshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot take a screenshot: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("the screenshot is not a PNG: %w", err)
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	bars, err := d.SystemBars(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range bars {
		rect := image.Rect(int(r.X), int(r.Y), int(r.X+r.Width), int(r.Y+r.Height)).Add(out.Bounds().Min)
		draw.Draw(out, rect, image.NewUniform(imagediff.MaskColor), image.Point{}, draw.Src)
	}
	return out, nil
}

// keep writes a screenshot the project did not have, or takes it again.
func keep(sc *core.Scenario, path string, shot image.Image, update bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b := encodePNG(shot)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	sc.Attach("image/png", b, "screenshot taken")
	if update {
		sc.Log("took the screenshot %s again", path)
		return nil
	}
	return core.Failf("There was no %q screenshot to compare with, so it was taken: %s; look at it, keep it, and run again", strings.TrimSuffix(filepath.Base(path), ".png"), path)
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func encodePNG(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
