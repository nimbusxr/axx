package video

import (
	"image"
	"image/color"
)

// The pointer, an arrow as desktops draw it, in points from its tip: a dark
// edge and a light body.
var (
	arrowEdge = []pt{{0, 0}, {0, 17}, {4.2, 13.2}, {7, 19.6}, {9.8, 18.4}, {7.1, 12.1}, {12.4, 12.1}}
	arrowBody = []pt{{1.2, 2.9}, {1.2, 14.2}, {4.6, 11.1}, {7.6, 18}, {8.2, 17.7}, {5.4, 11}, {9.6, 11}}
)

type pt struct{ x, y float64 }

// DrawPointer draws the pointer on img with its tip at at, scale times its
// size in points: a screen's capture has no pointer.
func DrawPointer(img *image.RGBA, at image.Point, scale float64) {
	fill(img, arrowEdge, at, scale, color.RGBA{20, 20, 24, 255})
	fill(img, arrowBody, at, scale, color.RGBA{250, 250, 250, 255})
}

// fill fills the polygon poly, scaled and moved to at, with c: each pixel
// as much as the polygon covers it, sampled 4 by 4.
func fill(img *image.RGBA, poly []pt, at image.Point, scale float64, c color.RGBA) {
	var maxX, maxY float64
	for _, p := range poly {
		maxX, maxY = max(maxX, p.x), max(maxY, p.y)
	}
	b := img.Bounds()
	for y := 0; y <= int(maxY*scale)+1; y++ {
		for x := 0; x <= int(maxX*scale)+1; x++ {
			px, py := at.X+x, at.Y+y
			if px < b.Min.X || py < b.Min.Y || px >= b.Max.X || py >= b.Max.Y {
				continue
			}
			hits := 0
			for sy := range 4 {
				for sx := range 4 {
					if inside(poly, (float64(x)+(float64(sx)+0.5)/4)/scale, (float64(y)+(float64(sy)+0.5)/4)/scale) {
						hits++
					}
				}
			}
			if hits == 0 {
				continue
			}
			i := img.PixOffset(px, py)
			a := uint32(hits) * 255 / 16
			for k, v := range [3]uint8{c.R, c.G, c.B} {
				img.Pix[i+k] = uint8((uint32(v)*a + uint32(img.Pix[i+k])*(255-a)) / 255) //nolint:gosec // a blend of two bytes
			}
			img.Pix[i+3] = 255
		}
	}
}

// inside is whether the point is inside the polygon (even-odd).
func inside(poly []pt, x, y float64) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.y > y) != (b.y > y) && x < (b.x-a.x)*(y-a.y)/(b.y-a.y)+a.x {
			in = !in
		}
	}
	return in
}
