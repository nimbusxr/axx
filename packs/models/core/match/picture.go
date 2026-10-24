package match

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/gif" // pictures a mapping's key may be
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"

	_ "golang.org/x/image/webp"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// ReadPicture reads a picture: PNG, JPEG, GIF or WebP.
func ReadPicture(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s is not a picture axx reads (PNG, JPEG, GIF or WebP): %w", path, err)
	}
	return img, nil
}

// Picture lays a picture out as an input of t, without its batch, as an app
// does: fitted to its size as the contract says (the middle of it at the
// input's proportions, or stretched), the share of its middle the contract's
// crop keeps, its channels in the contract's order, from 0 to 255. How the
// app scales them after makes no difference to matching.
func Picture(r *Reducer, t *contract.Tensor, img image.Image) []float64 {
	src := img.Bounds()
	if t.Image.Resize != "stretch" {
		src = centerCrop(src, r.W, r.H)
	}
	if c := t.Image.Crop; c > 0 && c < 1 {
		src = middle(src, c)
	}
	rgba := image.NewRGBA(src)
	draw.Draw(rgba, src, img, src.Min, draw.Src)

	dims := [3]int{}
	dims[r.layout[0]], dims[r.layout[1]], dims[r.layout[2]] = r.C, r.H, r.W
	out := make([]float64, r.ItemSize())
	for y := range r.H {
		for x := range r.W {
			rgb := bilinear(rgba, src, r.W, r.H, x, y)
			for c := range r.C {
				var v float64
				switch {
				case t.Image.Channels == "gray" || r.C == 1:
					v = float64(0.299*rgb[0]) + float64(0.587*rgb[1]) + float64(0.114*rgb[2])
				case t.Image.Channels == "BGR" && c < 3:
					v = rgb[2-c]
				case c < 3:
					v = rgb[c]
				default:
					v = 255 // alpha
				}
				idx := [3]int{}
				idx[r.layout[0]], idx[r.layout[1]], idx[r.layout[2]] = c, y, x
				out[(idx[0]*dims[1]+idx[1])*dims[2]+idx[2]] = v
			}
		}
	}
	return out
}

// bilinear is the color at (x, y) of the picture's part src scaled to w×h:
// the four pixels around the point it falls on, weighed by their nearness,
// each product converted before it is added, so that every machine computes
// the same bits (see standardize).
func bilinear(img *image.RGBA, src image.Rectangle, w, h, x, y int) [3]float64 {
	sx := float64(float64(x)+0.5)*float64(src.Dx())/float64(w) - 0.5
	sy := float64(float64(y)+0.5)*float64(src.Dy())/float64(h) - 0.5
	x0, y0 := math.Floor(sx), math.Floor(sy)
	fx, fy := sx-x0, sy-y0
	at := func(px, py float64) [3]float64 {
		cx := min(max(int(px), 0), src.Dx()-1) + src.Min.X
		cy := min(max(int(py), 0), src.Dy()-1) + src.Min.Y
		p := img.RGBAAt(cx, cy)
		return [3]float64{float64(p.R), float64(p.G), float64(p.B)}
	}
	a, b, c, d := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
	var out [3]float64
	for i := range out {
		top := float64(a[i]*float64(1-fx)) + float64(b[i]*fx)
		bottom := float64(c[i]*float64(1-fx)) + float64(d[i]*fx)
		out[i] = float64(top*float64(1-fy)) + float64(bottom*fy)
	}
	return out
}

// centerCrop is the middle of b at the proportions of w×h.
// middle is the share of b's middle, of each side.
func middle(b image.Rectangle, share float64) image.Rectangle {
	w, h := int(math.Round(float64(b.Dx())*share)), int(math.Round(float64(b.Dy())*share))
	x, y := b.Min.X+(b.Dx()-w)/2, b.Min.Y+(b.Dy()-h)/2
	return image.Rect(x, y, x+w, y+h)
}

func centerCrop(b image.Rectangle, w, h int) image.Rectangle {
	bw, bh := b.Dx(), b.Dy()
	if bw*h > bh*w { // wider than the input: crop the sides
		cw := bh * w / h
		x := b.Min.X + (bw-cw)/2
		return image.Rect(x, b.Min.Y, x+cw, b.Max.Y)
	}
	ch := bw * h / w
	y := b.Min.Y + (bh-ch)/2
	return image.Rect(b.Min.X, y, b.Max.X, y+ch)
}
