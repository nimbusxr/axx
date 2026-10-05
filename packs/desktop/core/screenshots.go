package desktopcore

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
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
        "update": {"type": "boolean", "description": "Take every screenshot again, as the app looks now, instead of comparing."},
        "platforms": {"type": "array", "items": {"type": "string", "enum": ["linux", "darwin", "windows"]}, "description": "The platforms whose screenshots the project keeps: each compares its own; elsewhere the steps pass without comparing (default: every platform)."}
      }
    }
  }
}`

// Config is the pack's settings, packs.desktop-core in axx.yaml.
type Config struct {
	Screenshots struct {
		Folder    string   `json:"folder"`
		Tolerance float64  `json:"tolerance"`
		Update    bool     `json:"update"`
		Platforms []string `json:"platforms"`
	} `json:"screenshots"`
}

type settings struct {
	folder    string // absolute
	tolerance float64
	update    bool
	platforms []string
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
	return &settings{folder: folder, tolerance: sc.Tolerance, update: sc.Update, platforms: sc.Platforms}, nil
}

// platform is the OS screenshots are taken on, as the web pack names it.
var platform = runtime.GOOS

// screenshotFile is a screenshot's file: <name>.<platform>.png, with the
// display's scale when it is not 1 (register.darwin@2x.png).
func screenshotFile(folder, name string, scale float64) string {
	suffix := ""
	if scale > 0 && scale != 1 {
		suffix = "@" + strconv.FormatFloat(scale, 'f', -1, 64) + "x"
	}
	return filepath.Join(folder, name+"."+platform+suffix+".png")
}

// looksLike compares the app's front window, once it has settled, with its
// screenshot.
func looksLike(sc *core.Scenario, app string, p Process, name string, wait time.Duration) error {
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	if len(cfg.platforms) > 0 && !slices.Contains(cfg.platforms, platform) {
		sc.Log("the %q screenshot is compared on %s only, not on %s", name, strings.Join(cfg.platforms, ", "), platform)
		return nil
	}
	var (
		shot     image.Image
		path     string
		last     imagediff.Result
		expected image.Image
	)
	deadline := time.Now().Add(wait)
	for {
		a, _, err := p.Window()
		if err != nil {
			return err
		}
		// Settled: as it was half a second ago (a web view draws its images
		// as they load).
		time.Sleep(500 * time.Millisecond)
		b, scale, err := p.Window()
		if err != nil {
			return err
		}
		shot, path = b, screenshotFile(cfg.folder, name, scale)
		if imagediff.Compare(a, b, imagediff.Options{}).Differ == 0 {
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
			last = imagediff.Compare(expected, shot, imagediff.Options{})
			if last.SameSize && float64(last.Differ) <= cfg.tolerance*float64(last.Pixels) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-sc.Context().Done():
			return sc.Context().Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if expected == nil {
		return core.Failf("The %s app did not settle within %s: what it shows kept changing", app, wait)
	}
	sc.Attach("image/png", encodePNG(expected), "expected screenshot")
	sc.Attach("image/png", encodePNG(shot), "the "+app+" app")
	if !last.SameSize {
		return core.Fail(fmt.Sprintf("The %s app's window is not the size of its %q screenshot (%s)", app, name, path), last.ExpectedSize, last.ActualSize)
	}
	sc.Attach("image/png", encodePNG(last.Diff), "difference")
	return core.Failf("The %s app does not look like its %q screenshot: %d pixels of %d differ (%.2f%%; %.2f%% may)",
		app, name, last.Differ, last.Pixels, 100*float64(last.Differ)/float64(last.Pixels), 100*cfg.tolerance)
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
	return core.Failf("There was no %q screenshot to compare with, so it was taken: %s; look at it, keep it, and run again",
		strings.TrimSuffix(filepath.Base(path), ".png"), path)
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
