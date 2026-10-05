//go:build linux

package atspi

import (
	"fmt"
	"image"

	"github.com/jezek/xgb/xproto"
)

// Screenshot is what the display shows, as the X server has it: the whole
// screen, which on Xvfb is the app's alone.
func (in *Input) Screenshot() (*image.RGBA, error) {
	screen := xproto.Setup(in.x).DefaultScreen(in.x)
	w, h := screen.WidthInPixels, screen.HeightInPixels
	const allPlanes = 0xffffffff
	img, err := xproto.GetImage(in.x, xproto.ImageFormatZPixmap, xproto.Drawable(in.root), 0, 0, w, h, allPlanes).Reply()
	if err != nil {
		return nil, fmt.Errorf("reading the screen: %w", err)
	}
	if screen.RootDepth != 24 && screen.RootDepth != 32 || len(img.Data) < int(w)*int(h)*4 {
		return nil, fmt.Errorf("the screen is %d bits deep: only 24 and 32 are read", screen.RootDepth)
	}
	out := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < int(w)*int(h); i++ { // blue, green, red, nothing → red, green, blue, opaque
		out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = img.Data[i*4+2], img.Data[i*4+1], img.Data[i*4], 0xff
	}
	return out, nil
}
