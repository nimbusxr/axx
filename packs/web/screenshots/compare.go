package screenshots

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

// The comparison of two screenshots is pixelmatch's
// (https://github.com/mapbox/pixelmatch), as Playwright compares them: a
// color distance in YIQ, and pixels of anti-aliased edges left out.

// threshold is how far apart two colors may be and still count as the same,
// in pixelmatch's terms (0 to 1); Playwright compares screenshots with 0.2.
const threshold = 0.2

func mustPNG(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func sizeOf(img image.Image) string {
	return fmt.Sprintf("%dx%d", img.Bounds().Dx(), img.Bounds().Dy())
}

// comparison is how two images differ.
type comparison struct {
	sameSize                 bool
	expectedSize, actualSize string
	pixels                   int
	differ                   int
	diff                     *image.RGBA // the differing pixels in red, on a faded copy of the expected image
}

// compare counts the pixels of two images that differ: whose colors are
// further apart than threshold, unless the difference is anti-aliasing.
//
// The color distance (YIQ) and the anti-aliasing test are pixelmatch's
// (https://github.com/mapbox/pixelmatch), as Playwright compares screenshots.
func compare(expected, actual image.Image) comparison {
	a, b := toRGBA(expected), toRGBA(actual)
	w, h := a.Rect.Dx(), a.Rect.Dy()
	c := comparison{expectedSize: sizeOf(expected), actualSize: sizeOf(actual), pixels: w * h}
	if w != b.Rect.Dx() || h != b.Rect.Dy() {
		return c
	}
	c.sameSize = true
	c.diff = image.NewRGBA(image.Rect(0, 0, w, h))
	maxDelta := 35215 * threshold * threshold
	for y := range h {
		for x := range w {
			pos := y*a.Stride + x*4
			delta := colorDelta(a.Pix, b.Pix, pos, pos, false)
			switch {
			case math.Abs(delta) <= maxDelta:
				v := uint8(blend(rgb2y(float64(a.Pix[pos]), float64(a.Pix[pos+1]), float64(a.Pix[pos+2])), 0.1*float64(a.Pix[pos+3])/255))
				c.diff.SetRGBA(x, y, color.RGBA{v, v, v, 255})
			case antialiased(a, x, y, w, h, b) || antialiased(b, x, y, w, h, a):
				c.diff.SetRGBA(x, y, color.RGBA{255, 255, 0, 255})
			default:
				c.diff.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
				c.differ++
			}
		}
	}
	return c
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) {
		return r
	}
	r := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(r, r.Rect, img, img.Bounds().Min, draw.Src)
	return r
}

// antialiased reports whether the pixel at x, y of img is likely part of
// anti-aliasing: few identical neighbours, and the darkest or brightest
// neighbour sitting in a flat area in both images.
func antialiased(img *image.RGBA, x1, y1, w, h int, other *image.RGBA) bool {
	x0, y0 := max(x1-1, 0), max(y1-1, 0)
	x2, y2 := min(x1+1, w-1), min(y1+1, h-1)
	pos := y1*img.Stride + x1*4
	zeroes := 0
	if x1 == x0 || x1 == x2 || y1 == y0 || y1 == y2 {
		zeroes = 1
	}
	var lo, hi float64
	var minX, minY, maxX, maxY int
	for x := x0; x <= x2; x++ {
		for y := y0; y <= y2; y++ {
			if x == x1 && y == y1 {
				continue
			}
			delta := colorDelta(img.Pix, img.Pix, pos, y*img.Stride+x*4, true)
			switch {
			case delta == 0:
				zeroes++
				if zeroes > 2 {
					return false
				}
			case delta < lo:
				lo, minX, minY = delta, x, y
			case delta > hi:
				hi, maxX, maxY = delta, x, y
			}
		}
	}
	if lo == 0 || hi == 0 {
		return false
	}
	return (manySiblings(img, minX, minY, w, h) && manySiblings(other, minX, minY, w, h)) ||
		(manySiblings(img, maxX, maxY, w, h) && manySiblings(other, maxX, maxY, w, h))
}

// manySiblings reports whether a pixel has more than two identical neighbours.
func manySiblings(img *image.RGBA, x1, y1, w, h int) bool {
	x0, y0 := max(x1-1, 0), max(y1-1, 0)
	x2, y2 := min(x1+1, w-1), min(y1+1, h-1)
	pos := y1*img.Stride + x1*4
	zeroes := 0
	if x1 == x0 || x1 == x2 || y1 == y0 || y1 == y2 {
		zeroes = 1
	}
	p := img.Pix
	for x := x0; x <= x2; x++ {
		for y := y0; y <= y2; y++ {
			if x == x1 && y == y1 {
				continue
			}
			q := y*img.Stride + x*4
			if p[pos] == p[q] && p[pos+1] == p[q+1] && p[pos+2] == p[q+2] && p[pos+3] == p[q+3] {
				zeroes++
			}
			if zeroes > 2 {
				return true
			}
		}
	}
	return false
}

// colorDelta is the squared YIQ distance between two pixels (negative when
// the first is lighter), or only the difference in brightness.
func colorDelta(p1, p2 []uint8, k, m int, yOnly bool) float64 {
	r1, g1, b1, a1 := float64(p1[k]), float64(p1[k+1]), float64(p1[k+2]), float64(p1[k+3])
	r2, g2, b2, a2 := float64(p2[m]), float64(p2[m+1]), float64(p2[m+2]), float64(p2[m+3])
	if a1 == a2 && r1 == r2 && g1 == g2 && b1 == b2 {
		return 0
	}
	if a1 < 255 {
		a1 /= 255
		r1, g1, b1 = blend(r1, a1), blend(g1, a1), blend(b1, a1)
	}
	if a2 < 255 {
		a2 /= 255
		r2, g2, b2 = blend(r2, a2), blend(g2, a2), blend(b2, a2)
	}
	y1, y2 := rgb2y(r1, g1, b1), rgb2y(r2, g2, b2)
	y := y1 - y2
	if yOnly {
		return y
	}
	i := rgb2i(r1, g1, b1) - rgb2i(r2, g2, b2)
	q := rgb2q(r1, g1, b1) - rgb2q(r2, g2, b2)
	delta := 0.5053*y*y + 0.299*i*i + 0.1957*q*q
	if y1 > y2 {
		return -delta
	}
	return delta
}

func rgb2y(r, g, b float64) float64 { return r*0.29889531 + g*0.58662247 + b*0.11448223 }
func rgb2i(r, g, b float64) float64 { return r*0.59597799 - g*0.27417610 - b*0.32180189 }
func rgb2q(r, g, b float64) float64 { return r*0.21147017 - g*0.52261711 + b*0.31114694 }

// blend blends a color with white by alpha.
func blend(c, a float64) float64 { return 255 + (c-255)*a }
