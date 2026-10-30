package mobileandroid

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	_ "image/jpeg" // the camera sees JPEG photos
	"image/png"
	"os"

	"golang.org/x/image/draw"

	"github.com/nimbusxr/axx/packs/internal/video"
)

// The emulator's camera shows a picture of its environment (scene.mode imagefile: or
// videofile:) oddly: it crops the picture to the middle 4:3 of it, as if the picture were the
// sensor's, landscape, then shows the left 3:4 of that, upright (emulator 37). So axx lays out
// what the camera is to see, as the phone held upright sees it (aspect fill, as an iPhone's
// camera does), at the left of a 4:3 picture: the emulator crops nothing of it, and shows that
// part, whole.
const (
	// frameW and frameH are the picture axx gives the emulator, 4:3; uprightW by frameH, at its
	// left, is what the phone shows, 3:4, as the sensor's 1280 by 960 is held upright.
	frameW, frameH = 1920, 1440
	uprightW       = frameH * 3 / 4
)

// framedImage is the image at path laid out for the emulator's camera, as a PNG file.
func framedImage(path, out string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	img = upright(img, jpegOrientation(data))
	dst := image.NewRGBA(image.Rect(0, 0, frameW, frameH))
	draw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, image.Rect(0, 0, uprightW, frameH), img, cover(img.Bounds(), uprightW, frameH), draw.Src, nil)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(f, dst); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// framedVideo is the H.264 video at path laid out for the emulator's camera, frame by frame, as
// an MP4 file.
func framedVideo(path, out string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	err = video.Remake(data, f, frameW, frameH, video.StreamQuality, func(dst, src *image.YCbCr) {
		sr := cover(src.Rect, uprightW, frameH)
		for i, p := range [3]struct {
			dst, src []byte
			ds, ss   int
			black    byte
			half     int
		}{{dst.Y, src.Y, dst.YStride, src.YStride, 16, 1}, {dst.Cb, src.Cb, dst.CStride, src.CStride, 128, 2}, {dst.Cr, src.Cr, dst.CStride, src.CStride, 128, 2}} {
			w, h := frameW/p.half, frameH/p.half
			d := &image.Gray{Pix: p.dst, Stride: p.ds, Rect: image.Rect(0, 0, w, h)}
			draw.Draw(d, d.Rect, image.NewUniform(color.Gray{Y: p.black}), image.Point{}, draw.Src)
			sw, sh := src.Rect.Dx(), src.Rect.Dy()
			if i > 0 {
				sw, sh = (sw+1)/2, (sh+1)/2
			}
			s := &image.Gray{Pix: p.src, Stride: p.ss, Rect: image.Rect(0, 0, sw, sh)}
			r := image.Rect(sr.Min.X/p.half, sr.Min.Y/p.half, sr.Max.X/p.half, sr.Max.Y/p.half)
			draw.BiLinear.Scale(d, image.Rect(0, 0, uprightW/p.half, h), s, r, draw.Src, nil)
		}
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// cover is the part of r that fills a w by h picture, aspect fill: all of r one way, its middle
// the other.
func cover(r image.Rectangle, w, h int) image.Rectangle {
	sw, sh := r.Dx(), r.Dy()
	if sw*h > sh*w { // wider than w:h: its middle, as wide as its height allows
		cw := sh * w / h
		x := r.Min.X + (sw-cw)/2
		return image.Rect(x, r.Min.Y, x+cw, r.Max.Y)
	}
	ch := sw * h / w
	y := r.Min.Y + (sh-ch)/2
	return image.Rect(r.Min.X, y, r.Max.X, y+ch)
}

// jpegOrientation is a JPEG photo's EXIF orientation (1 to 8; 1, as it is, for none): how a
// phone's photo is to be turned to show upright.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for i := 2; i+4 <= len(data) && data[i] == 0xff; {
		marker, size := data[i+1], int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xda || i+2+size > len(data) { // the image data: no EXIF before it
			return 1
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xe1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

// tiffOrientation is the Orientation tag (0x0112) of a TIFF header's first directory.
func tiffOrientation(t []byte) int {
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int(order.Uint32(t[4:]))
	if ifd+2 > len(t) {
		return 1
	}
	for n, e := int(order.Uint16(t[ifd:])), ifd+2; n > 0 && e+12 <= len(t); n, e = n-1, e+12 {
		if order.Uint16(t[e:]) == 0x0112 {
			if o := int(order.Uint16(t[e+8:])); o >= 1 && o <= 8 {
				return o
			}
		}
	}
	return 1
}

// upright is img turned as its EXIF orientation says.
func upright(img image.Image, orientation int) image.Image {
	if orientation <= 1 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if orientation >= 5 {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			out.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}
