//go:build linux

package desktoplinux

import (
	"image"
	"image/color"
	"testing"
)

// A window of one color all over has drawn nothing yet; one that shows
// anything else has.
func TestFlat(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for y := range 400 {
		for x := range 640 {
			img.Set(x, y, color.Black)
		}
	}
	if !flat(img) {
		t.Error("a black window shows nothing")
	}
	for y := 180; y < 220; y++ {
		for x := 300; x < 340; x++ {
			img.Set(x, y, color.RGBA{250, 247, 240, 255})
		}
	}
	if flat(img) {
		t.Error("a window with a picture in it shows something")
	}
	if !flat(image.NewRGBA(image.Rectangle{})) {
		t.Error("an empty window shows nothing")
	}
}
