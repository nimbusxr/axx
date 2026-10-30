package video

import (
	"bytes"
	"image"
	"testing"
	"time"
)

// A video read back frame by frame and drawn anew keeps its frames and their times: each one,
// decoded, shows the red square where the original did.
func TestRemake(t *testing.T) {
	e, err := NewEncoderOf(320, 240, 10, StreamQuality)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range frames(320, 240, 10) {
		if err := e.Encode(f, time.Duration(i)*100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	var in bytes.Buffer
	if err := WriteMP4(&in, []Clip{e.Finish(time.Second)}); err != nil {
		t.Fatal(err)
	}
	// Each frame on the left of one twice as wide, the rest black.
	var out bytes.Buffer
	err = Remake(in.Bytes(), &out, 640, 240, StreamQuality, func(dst, src *image.YCbCr) {
		for i := range dst.Y {
			dst.Y[i] = 16
		}
		for i := range dst.Cb {
			dst.Cb[i], dst.Cr[i] = 128, 128
		}
		for y := range 240 {
			copy(dst.Y[y*dst.YStride:], src.Y[y*src.YStride:y*src.YStride+320])
		}
		for y := range 120 {
			copy(dst.Cb[y*dst.CStride:], src.Cb[y*src.CStride:y*src.CStride+160])
			copy(dst.Cr[y*dst.CStride:], src.Cr[y*src.CStride:y*src.CStride+160])
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	track, err := ReadMP4(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if track.Width != 640 || track.Height != 240 || len(track.Samples) != 10 {
		t.Fatalf("the video remade is %dx%d of %d frames, not 640x240 of 10", track.Width, track.Height, len(track.Samples))
	}
	if track.Samples[3].At != 300*time.Millisecond || track.Duration != time.Second {
		t.Errorf("its 4th frame shows at %s, and it lasts %s: not 300ms and 1s", track.Samples[3].At, track.Duration)
	}
	dec, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	var sets []byte
	for _, nal := range append(track.SPS, track.PPS...) {
		sets = append(append(sets, 0, 0, 0, 1), nal...)
	}
	if _, err := dec.Decode(sets); err != nil {
		t.Fatal(err)
	}
	var last *image.YCbCr
	for _, s := range track.Samples {
		au, err := annexB(s.Data, track.LengthSize)
		if err != nil {
			t.Fatal(err)
		}
		if f, err := dec.Decode(au); err != nil {
			t.Fatal(err)
		} else if f != nil {
			last = f
		}
	}
	if last == nil {
		t.Fatal("no frame of the video remade decodes")
	}
	// The last frame's square is at the right of the original, x 220 to 320; red is Cr high.
	if cr := last.Cr[last.COffset(270, 120)-last.COffset(0, 0)]; cr < 180 {
		t.Errorf("the last frame shows no red square where the original did: Cr %d", cr)
	}
	if y := last.Y[last.YOffset(480, 120)]; y > 24 {
		t.Errorf("the right half is not black: Y %d", y)
	}
}
