package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// labelPreview draws a parcel's label as the shop's staff check it before
// printing, 4 by 6 inches at 100 dots an inch: its service level in a band,
// the reference, the recipient, the sender and the weight.
func labelPreview(l documentLine) ([]byte, error) {
	const w, h = 400, 600
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	ink := color.Black
	for _, r := range []image.Rectangle{ // the frame
		image.Rect(8, 8, w-8, 12), image.Rect(8, h-12, w-8, h-8), image.Rect(8, 8, 12, h-8), image.Rect(w-12, 8, w-8, h-8),
	} {
		draw.Draw(img, r, image.NewUniform(ink), image.Point{}, draw.Src)
	}
	draw.Draw(img, image.Rect(8, 8, w-8, 96), image.NewUniform(ink), image.Point{}, draw.Src)
	write(img, l.ServiceLevel, 26, 4, color.White)
	write(img, l.Reference, 130, 4, ink)
	write(img, "TO", 230, 2, ink)
	write(img, l.Recipient.Name, 264, 2, ink)
	write(img, l.Recipient.Postcode+" "+l.Recipient.City, 298, 2, ink)
	write(img, l.Recipient.Country, 332, 2, ink)
	write(img, "FROM", 430, 2, ink)
	write(img, l.Sender, 464, 2, ink)
	write(img, fmt.Sprintf("%d g", l.WeightGrams), 530, 3, ink)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// labelMargin is where a label's lines start, from its left edge.
const labelMargin = 24

// write draws a line of text with its top at y, from the label's margin, in
// the 7 by 13 bitmap font, each dot scale pixels wide, as a label printer
// prints it.
func write(dst *image.RGBA, text string, y, scale int, c color.Color) {
	face := basicfont.Face7x13
	small := image.NewRGBA(image.Rect(0, 0, 7*len(text), 13))
	d := font.Drawer{Dst: small, Src: image.NewUniform(c), Face: face, Dot: fixed.P(0, face.Ascent)}
	d.DrawString(text)
	at := image.Rect(labelMargin, y, labelMargin+small.Bounds().Dx()*scale, y+13*scale)
	xdraw.NearestNeighbor.Scale(dst, at, small, small.Bounds(), xdraw.Over, nil)
}
