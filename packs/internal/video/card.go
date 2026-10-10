package video

import (
	"image"
	"image/color"
	"image/draw"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Card is a title between a run's scenarios, width by height: a heading, the
// lines under it, and a word with its color (passed, failed) under those.
type Card struct {
	Heading string
	Lines   []string
	Word    string
	Color   color.RGBA
}

var (
	cardBG    = color.RGBA{20, 22, 28, 255}
	cardText  = color.RGBA{236, 238, 243, 255}
	cardMuted = color.RGBA{154, 162, 178, 255}
	// Passed and Failed are the colors of a scenario's outcome on its card.
	Passed = color.RGBA{76, 175, 80, 255}
	Failed = color.RGBA{239, 83, 80, 255}
)

var (
	fontsOnce     sync.Once
	regular, bold *opentype.Font
)

func loadFonts() {
	regular, _ = opentype.Parse(goregular.TTF)
	bold, _ = opentype.Parse(gobold.TTF)
}

// Image draws the card, width by height.
func (c Card) Image(width, height int) *image.RGBA {
	fontsOnce.Do(loadFonts)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{cardBG}, image.Point{}, draw.Src)
	unit := float64(height) / 1080
	heading := face(bold, 54*unit)
	body := face(regular, 30*unit)
	word := face(bold, 36*unit)
	lines := wrap(c.Heading, heading, width*8/10)
	total := len(lines)*int(70*unit) + len(c.Lines)*int(46*unit)
	if c.Word != "" {
		total += int(80 * unit)
	}
	y := (height-total)/2 + int(54*unit)
	for _, l := range lines {
		text(img, l, heading, width, y, cardText)
		y += int(70 * unit)
	}
	y += int(10 * unit)
	for _, l := range c.Lines {
		text(img, l, body, width, y, cardMuted)
		y += int(46 * unit)
	}
	if c.Word != "" {
		text(img, c.Word, word, width, y+int(30*unit), c.Color)
	}
	return img
}

func face(f *opentype.Font, size float64) font.Face {
	if f == nil {
		return nil
	}
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: max(8, size), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return fc
}

// text draws s centred across width, its baseline at y.
func text(img *image.RGBA, s string, f font.Face, width, y int, c color.RGBA) {
	if f == nil {
		return
	}
	d := &font.Drawer{Dst: img, Src: &image.Uniform{c}, Face: f}
	w := d.MeasureString(s).Round()
	d.Dot = fixed.P((width-w)/2, y)
	d.DrawString(s)
}

// wrap splits s into lines no wider than width.
func wrap(s string, f font.Face, width int) []string {
	if f == nil {
		return []string{s}
	}
	var lines []string
	line := ""
	for _, w := range splitWords(s) {
		next := w
		if line != "" {
			next = line + " " + w
		}
		if line != "" && font.MeasureString(f, next).Round() > width {
			lines = append(lines, line)
			next = w
		}
		line = next
	}
	return append(lines, line)
}

func splitWords(s string) []string {
	var out []string
	word := ""
	for _, r := range s {
		if r == ' ' {
			if word != "" {
				out = append(out, word)
			}
			word = ""
			continue
		}
		word += string(r)
	}
	if word != "" {
		out = append(out, word)
	}
	return out
}
