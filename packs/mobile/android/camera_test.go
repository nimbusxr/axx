package mobileandroid

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

// rotationXYZ is Rx·Ry·Rz of the angles in degrees, by rows.
func rotationXYZ(x, y, z float64) [3][3]float64 {
	rad := math.Pi / 180
	cx, sx, cy, sy, cz, sz := math.Cos(x*rad), math.Sin(x*rad), math.Cos(y*rad), math.Sin(y*rad), math.Cos(z*rad), math.Sin(z*rad)
	rx := [3][3]float64{{1, 0, 0}, {0, cx, -sx}, {0, sx, cx}}
	ry := [3][3]float64{{cy, 0, sy}, {0, 1, 0}, {-sy, 0, cy}}
	rz := [3][3]float64{{cz, -sz, 0}, {sz, cz, 0}, {0, 0, 1}}
	mul := func(a, b [3][3]float64) (m [3][3]float64) {
		for i := range 3 {
			for j := range 3 {
				for k := range 3 {
					m[i][j] += a[i][k] * b[k][j]
				}
			}
		}
		return m
	}
	return mul(mul(rx, ry), rz)
}

// The phone the emulator places has its camera where the pose says, turned as it says: the
// emulator's Euler angles about X, Y and Z (its R = Rx·Ry·Rz), and the lens off its middle.
func TestPhonePlacement(t *testing.T) {
	for _, c := range []struct{ x, y, z float64 }{{0, 0, 0}, {20, 30, 10}, {-40, 0, 0}, {0, -75, 0}, {10, 45, -170}} {
		r := rotationXYZ(c.x, c.y, c.z)
		cam := [3]float64{0.3, 1.4, -2}
		var pose scene.Mat4
		for i := range 3 {
			for j := range 3 {
				pose[j*4+i] = float32(r[i][j])
			}
			pose[12+i] = float32(cam[i])
		}
		pose[15] = 1
		turn, at := phonePlacement(pose)
		for i, want := range []float64{c.x, c.y, c.z} {
			if math.Abs(turn[i]-want) > 1e-3 {
				t.Errorf("%v: turned %v, not %v", c, turn, []float64{c.x, c.y, c.z})
				break
			}
		}
		for i := range 3 {
			lensAt := at[i] + r[i][0]*lens[0] + r[i][1]*lens[1] + r[i][2]*lens[2]
			if math.Abs(lensAt-cam[i]) > 1e-5 {
				t.Errorf("%v: the camera is at %v + R·lens, not %v", c, at, cam)
				break
			}
		}
	}
}

func TestCover(t *testing.T) {
	for _, c := range []struct {
		in   image.Rectangle
		want image.Rectangle
	}{
		{image.Rect(0, 0, 1080, 1920), image.Rect(0, 240, 1080, 1680)}, // 9:16: its middle 3:4
		{image.Rect(0, 0, 1920, 1080), image.Rect(555, 0, 1365, 1080)}, // 16:9: its middle 3:4
		{image.Rect(0, 0, 900, 1200), image.Rect(0, 0, 900, 1200)},     // 3:4 already
	} {
		if got := cover(c.in, 3, 4); got != c.want {
			t.Errorf("cover(%v) is %v, not %v", c.in, got, c.want)
		}
	}
}

// A picture laid out for the emulator's camera is 4:3, the picture aspect-filled at its left,
// 3:4: what the emulator shows of it.
func TestFramedImage(t *testing.T) {
	dir := t.TempDir()
	// 9:16, red above, blue below, green in its middle third.
	src := image.NewRGBA(image.Rect(0, 0, 540, 960))
	for y := range 960 {
		for x := range 540 {
			c := color.RGBA{255, 0, 0, 255}
			switch {
			case y >= 640:
				c = color.RGBA{0, 0, 255, 255}
			case y >= 320:
				c = color.RGBA{0, 255, 0, 255}
			}
			src.Set(x, y, c)
		}
	}
	in := filepath.Join(dir, "label.png")
	var b bytes.Buffer
	if err := png.Encode(&b, src); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "framed.png")
	if err := framedImage(in, out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, frameW, frameH) {
		t.Fatalf("the picture is %v, not %dx%d", img.Bounds(), frameW, frameH)
	}
	// The middle 3:4 of the 9:16 picture spans y 120 to 840 of 960: red to 320, green to 640.
	for _, p := range []struct {
		x, y int
		want color.RGBA
	}{
		{uprightW / 2, 5, color.RGBA{255, 0, 0, 255}},
		{uprightW / 2, frameH / 2, color.RGBA{0, 255, 0, 255}},
		{uprightW / 2, frameH - 5, color.RGBA{0, 0, 255, 255}},
		{uprightW + 20, frameH / 2, color.RGBA{0, 0, 0, 255}},
	} {
		r, g, bl, _ := img.At(p.x, p.y).RGBA()
		if got := (color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), 255}); got != p.want {
			t.Errorf("at %d,%d: %v, not %v", p.x, p.y, got, p.want)
		}
	}
	// red stops 1/9 of the way down the frame: (320-120)/720 of it
	r, _, _, _ := img.At(uprightW/2, frameH*200/720-8).RGBA()
	_, g, _, _ := img.At(uprightW/2, frameH*200/720+8).RGBA()
	if r>>8 < 200 || g>>8 < 200 {
		t.Errorf("the picture is not aspect-filled: red stops elsewhere than %d", frameH*200/720)
	}
}

// A JPEG's EXIF orientation turns it upright: 6 is a photo taken with the phone upright.
func TestJPEGOrientation(t *testing.T) {
	// FF D8, then an APP1 Exif segment with a little-endian TIFF header whose first directory
	// has one entry: Orientation (0x0112), SHORT, 1, 6.
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	seg := append([]byte("Exif\x00\x00"), tiff...)
	data := append([]byte{0xff, 0xd8, 0xff, 0xe1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}, seg...)
	data = append(data, 0xff, 0xda, 0, 2)
	if o := jpegOrientation(data); o != 6 {
		t.Errorf("orientation %d, not 6", o)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255}) // the photo's top left
	up := upright(img, 6)
	if up.Bounds().Dx() != 2 || up.Bounds().Dy() != 4 {
		t.Fatalf("turned upright, a 4x2 photo is %v", up.Bounds())
	}
	if r, _, _, _ := up.At(1, 0).RGBA(); r>>8 != 255 {
		t.Error("orientation 6 does not turn the photo a quarter clockwise: its top left is not at the top right")
	}
}
