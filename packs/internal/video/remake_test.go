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

func TestFramesReadsAVideoBack(t *testing.T) {
	e, err := NewEncoderOf(320, 240, 10, StreamQuality)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range frames(320, 240, 6) {
		if err := e.Encode(f, time.Duration(i)*100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	var in bytes.Buffer
	if err := WriteMP4(&in, []Clip{e.Finish(time.Second)}); err != nil {
		t.Fatal(err)
	}
	got, err := Frames(in.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Fatalf("%d frames, want 6", len(got))
	}
	for i, f := range got {
		if b := f.Bounds(); b.Dx() != 320 || b.Dy() != 240 {
			t.Errorf("frame %d is %dx%d, want 320x240", i, b.Dx(), b.Dy())
		}
	}
	if _, err := Frames([]byte("not a video")); err == nil {
		t.Error("what is no video was read as one")
	}
}

// A video's copy whose every frame is a key frame has the video's frames, at their times and
// of its size, and is shown the way up the video is: a phone held upright writes its frames on
// their side, and the turn a player gives them is kept. Any one frame of it decodes with none
// before it.
func TestKeyFrames(t *testing.T) {
	e, err := NewEncoderOf(320, 240, 10, StreamQuality)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range frames(320, 240, 10) {
		if err := e.Encode(f, time.Duration(i)*100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	clip := e.Finish(time.Second)
	// A quarter turn, as a phone held upright says of its video.
	clip.Matrix = concat(u32(0), u32(0x10000), u32(0), u32(0xffff0000), u32(0), u32(0), u32(240<<16), u32(0), u32(0x40000000))
	var in bytes.Buffer
	if err := WriteMP4(&in, []Clip{clip}); err != nil {
		t.Fatal(err)
	}
	source, err := ReadMP4(in.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	keys := 0
	for _, s := range source.Samples {
		if s.Key {
			keys++
		}
	}
	if keys != 1 {
		t.Fatalf("the video to copy has %d key frames of %d frames: the test wants one", keys, len(source.Samples))
	}
	var out bytes.Buffer
	if err := KeyFrames(in.Bytes(), &out, StreamQuality); err != nil {
		t.Fatal(err)
	}
	track, err := ReadMP4(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if track.Width != 320 || track.Height != 240 || len(track.Samples) != 10 {
		t.Fatalf("the copy is %dx%d of %d frames, not 320x240 of 10", track.Width, track.Height, len(track.Samples))
	}
	if !bytes.Equal(track.Matrix, clip.Matrix) {
		t.Errorf("the copy is turned %x, and the video %x", track.Matrix, clip.Matrix)
	}
	if track.Samples[7].At != 700*time.Millisecond || track.Duration != time.Second {
		t.Errorf("its 8th frame shows at %s, and it lasts %s: not 700ms and 1s", track.Samples[7].At, track.Duration)
	}
	var sets []byte
	for _, nal := range append(track.SPS, track.PPS...) {
		sets = append(append(sets, 0, 0, 0, 1), nal...)
	}
	// The 8th frame alone, by a decoder that has seen no other: its square is where the 8th
	// of the original is, x 171 to 271; red is Cr high.
	for i, s := range track.Samples {
		if !s.Key {
			t.Errorf("frame %d of the copy is no key frame", i+1)
		}
	}
	dec, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	if _, err := dec.Decode(sets); err != nil {
		t.Fatal(err)
	}
	au, err := annexB(track.Samples[7].Data, track.LengthSize)
	if err != nil {
		t.Fatal(err)
	}
	f, err := dec.Decode(au)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		if f, err = dec.Flush(); err != nil || f == nil {
			t.Fatalf("the 8th frame does not decode by itself: %v", err)
		}
	}
	if cr := f.Cr[f.COffset(220, 120)-f.COffset(0, 0)]; cr < 180 {
		t.Errorf("the 8th frame, decoded by itself, shows no red square where the original's is: Cr %d", cr)
	}
	if cr := f.Cr[f.COffset(60, 120)-f.COffset(0, 0)]; cr > 150 {
		t.Errorf("the 8th frame, decoded by itself, shows a red square where the original's first is: Cr %d", cr)
	}
}
