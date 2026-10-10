package video

import (
	"image"
	"image/draw"
)

// toI420 writes img into dst as I420, width by height: the luma plane, then
// both chroma planes at half the size, in BT.709's limited range, as players
// take HD video to be. A frame of another size is fitted in, centred on
// black.
func toI420(img image.Image, dst []byte, width, height int) {
	rgba := fit(img, width, height)
	ySize, cw := width*height, width/2
	yp, up, vp := dst[:ySize], dst[ySize:ySize+ySize/4], dst[ySize+ySize/4:]
	pix, stride := rgba.Pix, rgba.Stride
	for y := 0; y < height; y += 2 {
		row0 := pix[y*stride:]
		row1 := pix[(y+1)*stride:]
		for x := 0; x < width; x += 2 {
			var rs, gs, bs int
			for _, p := range [4][]byte{row0[x*4:], row0[x*4+4:], row1[x*4:], row1[x*4+4:]} {
				r, g, b := int(p[0]), int(p[1]), int(p[2])
				rs, gs, bs = rs+r, gs+g, bs+b
			}
			yp[y*width+x] = luma(row0[x*4:])
			yp[y*width+x+1] = luma(row0[x*4+4:])
			yp[(y+1)*width+x] = luma(row1[x*4:])
			yp[(y+1)*width+x+1] = luma(row1[x*4+4:])
			r, g, b := rs/4, gs/4, bs/4
			i := y/2*cw + x/2
			up[i] = clamp(((-26*r - 87*g + 112*b + 128) >> 8) + 128)
			vp[i] = clamp(((112*r - 102*g - 10*b + 128) >> 8) + 128)
		}
	}
}

func luma(p []byte) byte {
	r, g, b := int(p[0]), int(p[1]), int(p[2])
	return clamp(((47*r + 157*g + 16*b + 128) >> 8) + 16)
}

func clamp(v int) byte {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return byte(v)
}

// fit is img as an RGBA image width by height: as it is when it has that
// size, else scaled to fit (nearest pixel) and centred on black.
func fit(img image.Image, width, height int) *image.RGBA {
	b := img.Bounds()
	if r, ok := img.(*image.RGBA); ok && b.Dx() == width && b.Dy() == height && b.Min == (image.Point{}) {
		return r
	}
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
	if b.Dx() == width && b.Dy() == height {
		draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
		return out
	}
	scale := min(float64(width)/float64(b.Dx()), float64(height)/float64(b.Dy()))
	w, h := int(float64(b.Dx())*scale), int(float64(b.Dy())*scale)
	ox, oy := (width-w)/2, (height-h)/2
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	for y := range h {
		sy := min(b.Dy()-1, int(float64(y)/scale))
		for x := range w {
			sx := min(b.Dx()-1, int(float64(x)/scale))
			copy(out.Pix[(oy+y)*out.Stride+(ox+x)*4:][:4], src.Pix[sy*src.Stride+sx*4:][:4])
		}
	}
	return out
}
