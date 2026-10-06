package files

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // apps save captures as JPEG too
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

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
        "folder": {"type": "string", "description": "The folder of the images' screenshots, in the project (default screenshots)."},
        "tolerance": {"type": "number", "minimum": 0, "maximum": 1, "description": "The share of an image's pixels that may differ (default 0)."},
        "update": {"type": "boolean", "description": "Take every screenshot again, as the image is now, instead of comparing."},
        "platforms": {"type": "array", "items": {"type": "string", "enum": ["linux", "darwin", "windows"]}, "description": "The platforms whose screenshots the project keeps: each compares its own; elsewhere the step passes without comparing (default: every platform)."}
      }
    }
  }
}`

// Config is the pack's settings, packs.files in axx.yaml.
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
	return core.Cached(s, "files/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig("files", &c); err != nil {
			return nil, err
		}
		sc := c.Screenshots
		if sc.Tolerance < 0 || sc.Tolerance > 1 {
			return nil, fmt.Errorf("packs.files.screenshots.tolerance: %v is not a share between 0 and 1", sc.Tolerance)
		}
		folder := sc.Folder
		if folder == "" {
			folder = "screenshots"
		}
		if !filepath.IsAbs(folder) {
			folder = filepath.Join(s.ProjectDir(), filepath.FromSlash(folder))
		}
		return &settings{folder: folder, tolerance: sc.Tolerance, update: sc.Update, platforms: sc.Platforms}, nil
	})
}

// platform is the OS the screenshots are compared on: the apps that save
// images draw them their own way on each.
var platform = runtime.GOOS

func screenshotStep(o cloudstep.Objects) core.StepDef {
	return core.StepDef{
		ID: "files.screenshot", Keyword: "Then", Since: "0.2.1",
		Expr: "[[within {duration} ]]the {path} file in the {word} folder looks like the {string} screenshot",
		Doc: "Check that an image file looks like its screenshot, pixel by pixel (anti-aliasing aside): an image an app saved, like an annotated capture (PNG or JPEG). " +
			"The check waits for the file: 10 seconds, or `within {duration}`.\n\n" +
			"- Each platform draws images its own way, so each has its own screenshot, named after it (`checkout-annotated.darwin.png`), in the screenshots folder (`packs.files.screenshots`).\n" +
			"- Without one, the step takes it and fails: look at it, and keep it.\n" +
			"- When the image is different, the step attaches the screenshot, the image and their difference.",
		Examples: []string{`Then the "snap-*.png" file in the inbox folder looks like the "checkout-annotated" screenshot`},
		Run: func(sc *core.Scenario, a core.Args) error {
			cfg, err := settingsFor(sc.Suite())
			if err != nil {
				return err
			}
			name := a.String(3)
			if err := screenshotName(name); err != nil {
				return err
			}
			if len(cfg.platforms) > 0 && !slices.Contains(cfg.platforms, platform) {
				sc.Log("the %q screenshot is compared on %s only, not on %s", name, strings.Join(cfg.platforms, ", "), platform)
				return nil
			}
			path := filepath.Join(cfg.folder, name+"."+platform+".png")
			var (
				shot, expected image.Image
				last           imagediff.Result
				kept           bool
			)
			err = o.Await(sc, cloudstep.Wait(a, 0), a.String(2), a.String(1), func(b []byte) (bool, string, error) {
				img, why := decoded(b)
				if img == nil {
					return false, why, nil
				}
				shot = img
				if cfg.update {
					return true, "", nil
				}
				if expected == nil {
					expected, err = readPNG(path)
					if os.IsNotExist(err) {
						kept = true
						return true, "", nil
					}
					if err != nil {
						return false, "", err
					}
				}
				last = imagediff.Compare(expected, img, imagediff.Options{})
				if !last.SameSize {
					return false, fmt.Sprintf("it is %s, and its %q screenshot %s", last.ActualSize, name, last.ExpectedSize), nil
				}
				if float64(last.Differ) <= cfg.tolerance*float64(last.Pixels) {
					return true, "", nil
				}
				return false, fmt.Sprintf("%d pixels of %d differ from its %q screenshot (%.2f%%; %.2f%% may)",
					last.Differ, last.Pixels, name, 100*float64(last.Differ)/float64(last.Pixels), 100*cfg.tolerance), nil
			})
			if err != nil {
				if expected != nil && shot != nil {
					sc.Attach("image/png", encodePNG(expected), "expected screenshot")
					sc.Attach("image/png", encodePNG(shot), "the image")
					if last.Diff != nil {
						sc.Attach("image/png", encodePNG(last.Diff), "difference")
					}
				}
				return err
			}
			if cfg.update || kept {
				return keep(sc, path, shot, cfg.update)
			}
			return nil
		},
	}
}

// screenshotName is whether a screenshot's name names a file of the
// screenshots folder.
func screenshotName(name string) error {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return fmt.Errorf("a screenshot's name is a file name in the screenshots folder, like checkout-annotated: not %q", name)
	}
	return nil
}

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

// decoded is an image file's image, or why it is not one axx reads (a file
// still being written is not yet).
func decoded(b []byte) (image.Image, string) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, "it is not an image axx reads (PNG, JPEG): " + err.Error()
	}
	return img, ""
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
