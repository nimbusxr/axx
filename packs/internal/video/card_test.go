package video

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// A card draws its text on its background, and the pointer its arrow.
func TestCardAndPointer(t *testing.T) {
	img := Card{Heading: "A thick yellow stroke underlines a word", Lines: []string{"Mark what matters on the screen", "features/desktop/mark.feature:75"}, Word: "passed", Color: Passed}.Image(1920, 1080)
	lit := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 200 {
			lit++
		}
	}
	if lit < 1000 {
		t.Errorf("a card shows its text: %d light pixels", lit)
	}
	screen := image.NewRGBA(image.Rect(0, 0, 200, 120))
	draw.Draw(screen, screen.Bounds(), &image.Uniform{color.RGBA{120, 140, 200, 255}}, image.Point{}, draw.Src)
	DrawPointer(screen, image.Pt(60, 40), 2)
	if c := screen.RGBAAt(64, 60); c.R < 200 || c.G < 200 {
		t.Errorf("the pointer's body is light: %v", c)
	}
	if c := screen.RGBAAt(20, 20); c != (color.RGBA{120, 140, 200, 255}) {
		t.Errorf("away from the pointer the screen is as it was: %v", c)
	}
	if dir := os.Getenv("AXX_VIDEO_OUT"); dir != "" {
		for name, im := range map[string]image.Image{"card.png": img, "pointer.png": screen} {
			f, _ := os.Create(filepath.Join(dir, name))
			_ = png.Encode(f, im)
			_ = f.Close()
		}
	}
}
