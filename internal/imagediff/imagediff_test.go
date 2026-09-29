package imagediff

import (
	"image"
	"image/color"
	"testing"
)

func TestImagesDifferByPixelsNotAntiAliasing(t *testing.T) {
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
	if c := Compare(page(nil), page(nil), Options{}); !c.SameSize || c.Differ != 0 || c.Pixels != 800 {
		t.Errorf("alike: %+v", c)
	}
	// A block of another color differs.
	c := Compare(page(nil), page(func(img *image.RGBA) {
		for y := 16; y < 19; y++ {
			for x := 10; x < 14; x++ {
				img.SetRGBA(x, y, color.RGBA{220, 30, 30, 255})
			}
		}
	}), Options{})
	if c.Differ != 12 {
		t.Errorf("a red block: %d pixels differ", c.Differ)
	}
	// A masked element, a time, painted over one pixel wider in one of the
	// screenshots than in the other: left out where either is painted over.
	masks := func(x1 int) func(img *image.RGBA) {
		return func(img *image.RGBA) {
			for y := 4; y < 8; y++ {
				for x := 10; x < x1; x++ {
					img.SetRGBA(x, y, MaskColor)
				}
			}
		}
	}
	if c := Compare(page(masks(20)), page(masks(21)), Options{Masked: true}); c.Differ != 0 {
		t.Errorf("masks of two sizes: %d pixels differ", c.Differ)
	}
	if c := Compare(page(masks(20)), page(masks(21)), Options{}); c.Differ != 4 {
		t.Errorf("unmasked, the wider mask differs: %d pixels differ", c.Differ)
	}
	// A slightly lighter edge pixel, as anti-aliasing draws it, does not.
	c = Compare(page(nil), page(func(img *image.RGBA) { img.SetRGBA(5, 10, color.RGBA{90, 90, 90, 255}) }), Options{})
	if c.Differ != 0 {
		t.Errorf("anti-aliasing: %d pixels differ", c.Differ)
	}
	// A color close enough is the same color.
	c = Compare(page(nil), page(func(img *image.RGBA) { img.SetRGBA(20, 2, color.RGBA{250, 250, 250, 255}) }), Options{})
	if c.Differ != 0 {
		t.Errorf("a near color: %d pixels differ", c.Differ)
	}
	if c := Compare(page(nil), image.NewRGBA(image.Rect(0, 0, 40, 30)), Options{}); c.SameSize || c.ActualSize != "40x30" || c.ExpectedSize != "40x20" {
		t.Errorf("sizes: %+v", c)
	}
}

// The difference shows what differs in red on a faded copy of the expected
// image; images of any kind and origin compare by their pixels.
func TestTheDifferenceShowsWhereImagesDiffer(t *testing.T) {
	expected := image.NewRGBA(image.Rect(0, 0, 3, 3))
	actual := image.NewNRGBA(image.Rect(10, 10, 13, 13))
	for y := range 3 {
		for x := range 3 {
			expected.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			actual.SetNRGBA(10+x, 10+y, color.NRGBA{255, 255, 255, 255})
		}
	}
	if c := Compare(expected, actual, Options{}); !c.SameSize || c.Differ != 0 {
		t.Errorf("alike: %+v", c)
	}
	actual.SetNRGBA(11, 11, color.NRGBA{0, 0, 0, 255})
	c := Compare(expected, actual, Options{})
	if c.Differ != 1 || c.Diff.RGBAAt(1, 1) != (color.RGBA{255, 0, 0, 255}) || c.Diff.RGBAAt(0, 0) != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("%d differ; the difference: %v, %v", c.Differ, c.Diff.RGBAAt(1, 1), c.Diff.RGBAAt(0, 0))
	}
	if c := Compare(expected, image.NewRGBA(image.Rect(0, 0, 3, 4)), Options{}); c.SameSize || c.Diff != nil || c.Differ != 0 {
		t.Errorf("sizes: %+v", c)
	}
}
