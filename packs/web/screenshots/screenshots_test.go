package screenshots

import (
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettings(t *testing.T) {
	st, err := parseConfig(Config{}, "/work/acceptance")
	if err != nil {
		t.Fatal(err)
	}
	if st.folder != filepath.Join("/work/acceptance", "screenshots") || st.tolerance != 0 || st.update || len(st.platforms) != 0 {
		t.Errorf("defaults: %+v", st)
	}
	st, err = parseConfig(Config{Folder: "looks", Platforms: []string{"linux"}, Tolerance: 0.01, Update: true}, "/work/acceptance")
	if err != nil {
		t.Fatal(err)
	}
	if st.folder != filepath.Join("/work/acceptance", "looks") || st.tolerance != 0.01 || !st.update || st.platforms[0] != "linux" {
		t.Errorf("set: %+v", st)
	}
	if _, err := parseConfig(Config{Tolerance: 2}, "/p"); err == nil || !strings.Contains(err.Error(), "packs.web-screenshots.tolerance: 2 is not a share between 0 and 1") {
		t.Errorf("tolerance: %v", err)
	}
}

func TestScreenshotNames(t *testing.T) {
	for _, ok := range []string{"quote", "quote result", "parcel-PX-4101", "portal.home"} {
		if !screenshotName.MatchString(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "../quote", "quote/result", ".hidden", `a\b`} {
		if screenshotName.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestScreenshotsDifferByPixelsNotAntiAliasing(t *testing.T) {
	page := func(mark func(*image.RGBA)) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 40, 20))
		for y := range 20 {
			for x := range 40 {
				img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
		// A black bar, as text is.
		for y := 5; y < 15; y++ {
			for x := 5; x < 35; x++ {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
		if mark != nil {
			mark(img)
		}
		return img
	}
	if c := compare(page(nil), page(nil)); !c.sameSize || c.differ != 0 || c.pixels != 800 {
		t.Errorf("alike: %+v", c)
	}
	// A block of another color differs.
	c := compare(page(nil), page(func(img *image.RGBA) {
		for y := 16; y < 19; y++ {
			for x := 10; x < 14; x++ {
				img.SetRGBA(x, y, color.RGBA{220, 30, 30, 255})
			}
		}
	}))
	if c.differ != 12 {
		t.Errorf("a red block: %d pixels differ", c.differ)
	}
	// A slightly lighter edge pixel, as anti-aliasing draws it, does not.
	c = compare(page(nil), page(func(img *image.RGBA) { img.SetRGBA(5, 10, color.RGBA{90, 90, 90, 255}) }))
	if c.differ != 0 {
		t.Errorf("anti-aliasing: %d pixels differ", c.differ)
	}
	// A color close enough is the same color.
	c = compare(page(nil), page(func(img *image.RGBA) { img.SetRGBA(20, 2, color.RGBA{250, 250, 250, 255}) }))
	if c.differ != 0 {
		t.Errorf("a near color: %d pixels differ", c.differ)
	}
	if c := compare(page(nil), image.NewRGBA(image.Rect(0, 0, 40, 30))); c.sameSize || c.actualSize != "40x30" || c.expectedSize != "40x20" {
		t.Errorf("sizes: %+v", c)
	}
}
