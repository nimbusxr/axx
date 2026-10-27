package match

import (
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func imageInput(layout string, dims ...int64) *contract.Tensor {
	t := &contract.Tensor{Name: "pixels", Type: "float32", Image: &contract.Image{Layout: layout}}
	for _, d := range dims {
		t.Shape = append(t.Shape, contract.Dim{Size: d})
	}
	return t
}

// parcel is a picture of a brown box on a gray porch, the box at (x, y).
func parcel(w, h, x, y int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for py := range h {
		for px := range w {
			c := color.RGBA{R: 120, G: 120, B: 125, A: 255}
			if px >= x && px < x+w/3 && py >= y && py < y+h/3 {
				c = color.RGBA{R: 160, G: 110, B: 60, A: 255}
			}
			img.SetRGBA(px, py, c)
		}
	}
	return img
}

func TestAnImageIsPooledToAGridOfAtMost16(t *testing.T) {
	for _, tc := range []struct {
		h, w           int64
		kh, kw, ph, pw int
	}{
		{224, 224, 14, 14, 16, 16},
		{300, 200, 18, 12, 16, 16},
		{8, 8, 1, 1, 8, 8},
		{64, 32, 4, 2, 16, 16},
	} {
		r, err := NewReducer(imageInput("NCHW", 1, 3, tc.h, tc.w))
		if err != nil {
			t.Fatal(err)
		}
		if r.KH != tc.kh || r.KW != tc.kw || r.PH != tc.ph || r.PW != tc.pw || r.Dims() != 3*tc.ph*tc.pw {
			t.Errorf("%dx%d: kernel %dx%d, grid %dx%d", tc.h, tc.w, r.KH, r.KW, r.PH, r.PW)
		}
	}
}

// An app scales and shifts each channel as its model expects; the reduced
// input is the same.
func TestHowAnAppScalesAPicturesChannelsMakesNoDifference(t *testing.T) {
	in := imageInput("NCHW", 1, 3, 64, 64)
	r, err := NewReducer(in)
	if err != nil {
		t.Fatal(err)
	}
	raw := Picture(r, in, parcel(200, 150, 40, 30))
	mean, sd := [3]float64{0.485, 0.456, 0.406}, [3]float64{0.229, 0.224, 0.225}
	normalized := make([]float64, len(raw))
	for i, v := range raw {
		c := i / (64 * 64)
		normalized[i] = (v/255 - mean[c]) / sd[c]
	}
	a, _ := r.Reduce(raw)
	b, _ := r.Reduce(normalized)
	if s := Similarity(a, b); math.Abs(s-1) > 1e-9 {
		t.Errorf("similarity %v, want 1", s)
	}
}

// An app that keeps the middle of a picture (torchvision's evaluation
// transform: the shorter side resized to 256, its middle 224) gives the
// model the input a contract of that crop lays a key out as.
func TestACropKeepsThePicturesMiddle(t *testing.T) {
	in := imageInput("NCHW", 1, 3, 64, 64)
	r, _ := NewReducer(in)
	pic := parcel(256, 256, 70, 90)
	// What the app gives the model: the middle 224 of 256, at 64×64.
	app := image.NewRGBA(image.Rect(0, 0, 224, 224))
	for y := range 224 {
		for x := range 224 {
			app.Set(x, y, pic.At(x+16, y+16))
		}
	}
	given, _ := r.Reduce(Picture(r, in, app))
	whole, _ := r.Reduce(Picture(r, in, pic))
	in.Image.Crop = 0.875
	cropped, _ := r.Reduce(Picture(r, in, pic))
	if s := Similarity(given, cropped); s < 0.999 {
		t.Errorf("the key, cropped: similarity %v", s)
	}
	if s := Similarity(given, whole); s > Similarity(given, cropped)-0.01 {
		t.Errorf("the key, whole: similarity %v, as alike as cropped", s)
	}
}

func TestALayoutIsReadAsItIs(t *testing.T) {
	nchw, nhwc := imageInput("NCHW", 1, 3, 32, 48), imageInput("NHWC", 1, 32, 48, 3)
	rc, _ := NewReducer(nchw)
	rh, _ := NewReducer(nhwc)
	pic := parcel(96, 64, 10, 20)
	a, _ := rc.Reduce(Picture(rc, nchw, pic))
	b, _ := rh.Reduce(Picture(rh, nhwc, pic))
	if s := Similarity(a, b); math.Abs(s-1) > 1e-9 {
		t.Errorf("NCHW and NHWC of a picture: similarity %v, want 1", s)
	}

	bgr := imageInput("NCHW", 1, 3, 32, 48)
	bgr.Image.Channels = "BGR"
	c, _ := rc.Reduce(Picture(rc, bgr, pic))
	// The same picture in another channel order is a different input.
	if s := Similarity(a, c); s > 0.99 {
		t.Errorf("RGB and BGR: similarity %v", s)
	}
}

func TestPicturesAreAlikeAsTheyLookAlike(t *testing.T) {
	in := imageInput("NCHW", 1, 3, 64, 64)
	r, _ := NewReducer(in)
	reduce := func(img image.Image) []float64 {
		v, err := r.Reduce(Picture(r, in, img))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	box := reduce(parcel(300, 300, 100, 100))
	// The same scene, a little noisy: a phone's camera.
	noisy := image.NewRGBA(image.Rect(0, 0, 300, 300))
	src := parcel(300, 300, 100, 100)
	rnd := rand.New(rand.NewPCG(1, 2))
	for y := range 300 {
		for x := range 300 {
			c := src.At(x, y).(color.RGBA)
			n := func(v uint8) uint8 { return uint8(max(0, min(255, int(v)+rnd.IntN(21)-10))) }
			noisy.SetRGBA(x, y, color.RGBA{R: n(c.R), G: n(c.G), B: n(c.B), A: 255})
		}
	}
	elsewhere := reduce(parcel(300, 300, 10, 190))
	if s := Similarity(box, reduce(noisy)); s < 0.95 {
		t.Errorf("a noisy picture of the box: similarity %v", s)
	}
	if s := Similarity(box, elsewhere); s > 0.5 {
		t.Errorf("the box elsewhere: similarity %v", s)
	}
}

func TestTheNearestKeyAnswersWhenItIsAlikeEnough(t *testing.T) {
	tb := Table{Keys: [][]float64{{1, -1}, {-1, 1}, {1, -1}}, Similarity: 0.9}
	for _, tc := range []struct {
		in   []float64
		want int
	}{
		{[]float64{1, -1}, 0},        // the first of two equal keys
		{[]float64{-1, 1}, 1},        // exactly
		{[]float64{0, 0}, 3},         // like neither: the default
		{[]float64{0.9, -0.9}, 0},    // as alike as the threshold: the key
		{[]float64{0.89, -0.89}, 3},  // less: the default
		{[]float64{-0.95, 0.95}, 1},  // near enough
		{[]float64{-1.0, 1.0001}, 1}, // a key's other side
	} {
		if got := tb.Nearest(tc.in); got != tc.want {
			t.Errorf("Nearest(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestATensorIsStandardizedAsAWhole(t *testing.T) {
	in := &contract.Tensor{Name: "features", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Size: 4}}}
	r, err := NewReducer(in)
	if err != nil || r.Image || r.Dims() != 4 {
		t.Fatalf("%+v %v", r, err)
	}
	a, _ := r.Reduce([]float64{1, 2, 3, 4})
	b, _ := r.Reduce([]float64{10, 20, 30, 40})
	if s := Similarity(a, b); math.Abs(s-1) > 1e-9 {
		t.Errorf("similarity %v", s)
	}
	if _, err := NewReducer(&contract.Tensor{Name: "x", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "n"}}}); err == nil {
		t.Error("an input of an open size has a reducer")
	}
}
