package video

import (
	"image"
	"image/color"
	"math"
)

// touchColor is a finger's touch, as phones show touches: blue.
var touchColor = color.RGBA{41, 121, 255, 255}

// DrawTouch draws a finger's touch on img: a disc of radius where it is,
// and for a finger that moved, the path behind it from from. fade is how far
// the touch has faded, from 0 (just touched) to 1 (gone); the disc grows a
// little as it fades, a ripple.
func DrawTouch(img *image.RGBA, from, at image.Point, radius, fade float64) {
	if fade >= 1 || radius <= 0 {
		return
	}
	fade = max(0, fade)
	if from != at {
		capsule(img, from, at, radius*0.45, 0.28*(1-fade))
	}
	r := radius * (1 + 0.35*fade)
	disc(img, at, r, 0.42*(1-fade))
	ring(img, at, r, max(1.5, radius*0.12), 0.75*(1-fade))
}

// disc fills a circle, each pixel as much as the circle covers it.
func disc(img *image.RGBA, c image.Point, r, alpha float64) {
	shade(img, c, r+1, alpha, func(d float64) float64 { return clamp01(r + 0.5 - d) })
}

// ring strokes a circle's edge, width wide inside it.
func ring(img *image.RGBA, c image.Point, r, width, alpha float64) {
	shade(img, c, r+1, alpha, func(d float64) float64 {
		return clamp01(r+0.5-d) * clamp01(d-(r-width)+0.5)
	})
}

// shade blends touchColor over the pixels within reach of c, as much as
// cover says of each pixel's distance from c.
func shade(img *image.RGBA, c image.Point, reach, alpha float64, cover func(d float64) float64) {
	b := img.Bounds()
	x0, x1 := max(b.Min.X, c.X-int(reach)-1), min(b.Max.X, c.X+int(reach)+2)
	y0, y1 := max(b.Min.Y, c.Y-int(reach)-1), min(b.Max.Y, c.Y+int(reach)+2)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			d := math.Hypot(float64(x-c.X)+0.5, float64(y-c.Y)+0.5)
			blend(img, x, y, alpha*cover(d))
		}
	}
}

// capsule fills the segment from a to b, half wide either side, rounded.
func capsule(img *image.RGBA, a, b image.Point, half, alpha float64) {
	bounds := img.Bounds()
	x0, x1 := max(bounds.Min.X, min(a.X, b.X)-int(half)-1), min(bounds.Max.X, max(a.X, b.X)+int(half)+2)
	y0, y1 := max(bounds.Min.Y, min(a.Y, b.Y)-int(half)-1), min(bounds.Max.Y, max(a.Y, b.Y)+int(half)+2)
	ax, ay, bx, by := float64(a.X), float64(a.Y), float64(b.X), float64(b.Y)
	vx, vy := bx-ax, by-ay
	l2 := vx*vx + vy*vy
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			t := 0.0
			if l2 > 0 {
				t = clamp01(((px-ax)*vx + (py-ay)*vy) / l2)
			}
			d := math.Hypot(px-(ax+t*vx), py-(ay+t*vy))
			blend(img, x, y, alpha*clamp01(half+0.5-d))
		}
	}
}

// blend lays touchColor over the pixel at x, y with the opacity a.
func blend(img *image.RGBA, x, y int, a float64) {
	if a <= 0 {
		return
	}
	i := img.PixOffset(x, y)
	p := img.Pix[i : i+3 : i+3]
	for k, v := range []uint8{touchColor.R, touchColor.G, touchColor.B} {
		p[k] = uint8(math.Round(float64(p[k])*(1-a) + float64(v)*a))
	}
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }
