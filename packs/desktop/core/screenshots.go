package desktopcore

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
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
    },
    "traces": {"type": "string", "enum": ["failed", "always", "never"], "description": "Which scenarios keep a trace of each app in .axx/desktop/traces: a page with its window after each step, and its controls where the scenario failed (default never)."},
    "videos": {"type": "string", "enum": ["failed", "always", "never"], "description": "Which scenarios keep a video of their desktop in .axx/desktop/videos, an MP4 file (H.264), and the run one of them all, run.mp4 (default never). OpenH264 Video Codec provided by Cisco Systems, Inc.: axx downloads it from Cisco when a video is first kept; never turns it off."}
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
	Traces string `json:"traces"`
	Videos string `json:"videos"`
}

type settings struct {
	folder    string // absolute
	tolerance float64
	update    bool
	platforms []string
	traces    string // failed, always or never
	videos    string
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
	traces, videos := c.Traces, c.Videos
	if traces == "" {
		traces = "never"
	}
	if videos == "" {
		videos = "never"
	}
	return &settings{folder: folder, tolerance: sc.Tolerance, update: sc.Update, platforms: sc.Platforms, traces: traces, videos: videos}, nil
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
// screenshot, the app in front and the pointer away from it.
func looksLike(sc *core.Scenario, app string, p Process, name string, wait time.Duration) error {
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	if len(cfg.platforms) > 0 && !slices.Contains(cfg.platforms, platform) {
		sc.Log("the %q screenshot is compared on %s only, not on %s", name, strings.Join(cfg.platforms, ", "), platform)
		return nil
	}
	// In front, as a person looks at it: an app in the background draws its
	// window as inactive (Flutter its field's line, AppKit its controls'
	// colors), and which one is in front is chance.
	if err := p.Front(); err != nil {
		return err
	}
	// And with the pointer away, wherever the last step left it: a control
	// under it draws itself hovered (Flutter's field, its line darker).
	if err := p.Away(); err != nil {
		return err
	}
	var (
		// shot is the settled look last compared with the screenshot, and
		// last how they compared.
		shot     image.Image
		path     string
		last     imagediff.Result
		expected image.Image
		// before and after are the last two looks that differed, and
		// change how.
		before, after image.Image
		change        imagediff.Result
		// caret is a text cursor seen blinking where the app does not say
		// it is (Flutter's on Linux): hidden in every look from then on.
		caret image.Rectangle
	)
	// look is the window, its cursor hidden.
	look := func() (image.Image, float64, error) {
		img, scale, err := p.Window()
		if err == nil && !caret.Empty() {
			img = HideCaret(img, caret)
		}
		return img, scale, err
	}
	deadline := time.Now().Add(wait)
	// Each look is held against the one before it: a window that settles
	// late is compared as soon as it has, not a look later.
	a, _, err := look()
	if err != nil {
		return err
	}
	for {
		// Settled: as it was half a second ago (a web view draws its images
		// as they load), but for a cursor's blink.
		select {
		case <-sc.Context().Done():
			return sc.Context().Err()
		case <-time.After(500 * time.Millisecond):
		}
		b, scale, err := look()
		if err != nil {
			return err
		}
		moved := imagediff.Compare(a, b, imagediff.Options{})
		if r, ok := blinked(a, b); moved.Differ != 0 && ok {
			caret = r
			a, b = HideCaret(a, caret), HideCaret(b, caret)
			moved = imagediff.Compare(a, b, imagediff.Options{})
		}
		// Taken, a look is looked at once more a blink later: a cursor that
		// blinks slower than the looks are taken shows only then.
		if moved.Differ == 0 && cfg.update && caret.Empty() {
			time.Sleep(700 * time.Millisecond)
			c, _, err := look()
			if err != nil {
				return err
			}
			if r, ok := blinked(b, c); ok {
				caret = r
				b = HideCaret(b, caret)
			} else if again := imagediff.Compare(b, c, imagediff.Options{}); again.Differ != 0 {
				a, b, moved = b, c, again
			}
		}
		path = screenshotFile(cfg.folder, name, scale)
		if moved.Differ != 0 {
			before, after, change = a, b, moved
		} else {
			if cfg.update {
				return keep(sc, path, b, true)
			}
			if expected == nil {
				expected, err = readPNG(path)
				if os.IsNotExist(err) {
					return keep(sc, path, b, false)
				}
				if err != nil {
					return err
				}
			}
			shot, last = b, imagediff.Compare(expected, b, imagediff.Options{})
			if last.SameSize && float64(last.Differ) <= cfg.tolerance*float64(last.Pixels) {
				return nil
			}
		}
		if time.Now().After(deadline) {
			break
		}
		a = b
	}
	if expected == nil {
		if before != nil && change.Diff != nil {
			sc.Attach("image/png", encodePNG(before), "the "+app+" app")
			sc.Attach("image/png", encodePNG(after), "the "+app+" app half a second later")
			sc.Attach("image/png", encodePNG(change.Diff), "what changed")
		}
		return core.Failf("The %s app did not settle within %s: what it shows kept changing (%d pixels in half a second)", app, wait, change.Differ)
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

// blinked is where a text cursor blinked between two looks at a window:
// all that changed is a column a few pixels wide and about a line high. It
// is a pixel wider each side and two longer each end, for the cursor's
// smoothed edges and ends.
func blinked(a, b image.Image) (image.Rectangle, bool) {
	if a.Bounds() != b.Bounds() {
		return image.Rectangle{}, false
	}
	var box image.Rectangle
	ra, okA := a.(*image.RGBA)
	rb, okB := b.(*image.RGBA)
	bd := a.Bounds()
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		for x := bd.Min.X; x < bd.Max.X; x++ {
			var same bool
			if okA && okB {
				same = ra.RGBAAt(x, y) == rb.RGBAAt(x, y)
			} else {
				same = a.At(x, y) == b.At(x, y)
			}
			if !same {
				box = box.Union(image.Rect(x, y, x+1, y+1))
				if box.Dx() > 6 {
					return image.Rectangle{}, false
				}
			}
		}
	}
	if box.Empty() || box.Dy() < 8 || box.Dy() > 120 || box.Dy() < 3*box.Dx() {
		return image.Rectangle{}, false
	}
	return image.Rect(box.Min.X-1, box.Min.Y-2, box.Max.X+1, box.Max.Y+2), true
}

// HideCaret hides a text cursor in a screenshot, at r in its pixels: each
// row of r takes the color most of the few pixels on either side of it
// have, as the field looks with the cursor off: its background, whether r
// starts at the last letter's smoothed edge or a pixel after it (a cursor's
// place is rounded, at a display's scale). Every screenshot of the field is
// then the same, whether its cursor was on or off.
func HideCaret(img image.Image, r image.Rectangle) image.Image {
	b := img.Bounds()
	if r = r.Intersect(b); r.Empty() || r.Dx() >= b.Dx() {
		return img
	}
	out := image.NewRGBA(b)
	draw.Draw(out, b, img, b.Min, draw.Src)
	const side = 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		// The right side's first: after a field's text, its background.
		var colors []color.RGBA
		count := map[color.RGBA]int{}
		for x := r.Max.X; x < min(r.Max.X+side, b.Max.X); x++ {
			colors = append(colors, out.RGBAAt(x, y))
		}
		for x := r.Min.X - 1; x >= max(r.Min.X-side, b.Min.X); x-- {
			colors = append(colors, out.RGBAAt(x, y))
		}
		fill := colors[0]
		for _, c := range colors {
			if count[c]++; count[c] > count[fill] {
				fill = c
			}
		}
		for x := r.Min.X; x < r.Max.X; x++ {
			out.SetRGBA(x, y, fill)
		}
	}
	return out
}

// AwaySpot is where the pointer goes to be off a window, on the screen: a
// little to its right, else to its left, under it or above it, else at the
// screen's right edge (a window that fills the screen), never in a corner of
// the screen (macOS runs what its hot corners do).
func AwaySpot(window, screen image.Rectangle) image.Point {
	const gap = 16
	mid := image.Pt(window.Min.X+window.Dx()/2, window.Min.Y+window.Dy()/2)
	mid.Y = min(max(mid.Y, screen.Min.Y+gap), screen.Max.Y-gap)
	mid.X = min(max(mid.X, screen.Min.X+gap), screen.Max.X-gap)
	for _, p := range []image.Point{
		{window.Max.X + gap, mid.Y}, {window.Min.X - gap, mid.Y}, {mid.X, window.Max.Y + gap}, {mid.X, window.Min.Y - gap},
	} {
		if p.In(screen.Inset(gap / 2)) {
			return p
		}
	}
	return image.Pt(screen.Max.X-2, screen.Min.Y+screen.Dy()/2)
}

// Flat is whether an image is one color all over, near enough: a window
// that has drawn nothing yet (a web view's, until its first frame).
func Flat(img image.Image) bool {
	b := img.Bounds()
	if b.Empty() {
		return true
	}
	r0, g0, b0, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += max(1, b.Dy()/32) {
		for x := b.Min.X; x < b.Max.X; x += max(1, b.Dx()/32) {
			r, g, bl, _ := img.At(x, y).RGBA()
			if diff(r, r0) > 0x0800 || diff(g, g0) > 0x0800 || diff(bl, b0) > 0x0800 {
				return false
			}
		}
	}
	return true
}

func diff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}
