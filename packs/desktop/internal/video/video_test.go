package video

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

// frames are a red square crossing a grey screen.
func frames(w, h, n int) []*image.RGBA {
	var out []*image.RGBA
	for i := range n {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{200, 200, 205, 255}}, image.Point{}, draw.Src)
		x := i * (w - 100) / max(1, n-1)
		draw.Draw(img, image.Rect(x, h/2-50, x+100, h/2+50), &image.Uniform{color.RGBA{220, 40, 40, 255}}, image.Point{}, draw.Src)
		out = append(out, img)
	}
	return out
}

// Frames encode as H.264 and make an MP4 file, its frames as long as they
// showed, clips one after another.
func TestEncodeMP4(t *testing.T) {
	var clips []Clip
	for range 2 {
		e, err := NewEncoder(640, 400, 10)
		if err != nil {
			t.Fatal(err)
		}
		for i, f := range frames(640, 400, 20) {
			if err := e.Encode(f, time.Duration(i)*100*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		c := e.Finish(2500 * time.Millisecond)
		if len(c.SPS) == 0 || len(c.PPS) == 0 || len(c.Samples) == 0 || !c.Samples[0].Key {
			t.Fatalf("a clip has parameter sets and starts on a key frame: %d sps, %d pps, %d samples", len(c.SPS), len(c.PPS), len(c.Samples))
		}
		clips = append(clips, c)
	}
	if !bytes.Equal(clips[0].SPS, clips[1].SPS) || !bytes.Equal(clips[0].PPS, clips[1].PPS) {
		t.Errorf("encoders with one setting make one parameter set: %x %x", clips[0].SPS, clips[1].SPS)
	}
	var out bytes.Buffer
	if err := WriteMP4(&out, clips); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	if string(b[4:8]) != "ftyp" || !bytes.Contains(b, []byte("moov")) || !bytes.Contains(b, []byte("avcC")) {
		t.Fatalf("an MP4 file: %q", b[:16])
	}
	if dir := os.Getenv("AXX_VIDEO_OUT"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "encode-test.mp4"), b, 0o644)
	}
	t.Logf("%d frames in %d bytes", len(clips[0].Samples)*2, len(b))
}

// The structures OpenH264 reads are laid out as its C header has them.
func TestLayouts(t *testing.T) {
	for name, c := range map[string][2]uintptr{
		"SEncParamExt":        {unsafe.Sizeof(encParamExt{}), 924},
		"SSpatialLayerConfig": {unsafe.Sizeof(spatialLayerConfig{}), 200},
		"SSourcePicture":      {unsafe.Sizeof(sourcePicture{}), 80},
		"SLayerBSInfo":        {unsafe.Sizeof(layerBSInfo{}), 56},
		"SFrameBSInfo":        {unsafe.Sizeof(frameBSInfo{}), 7192},
	} {
		if c[0] != c[1] {
			t.Errorf("%s is %d bytes, not %d", name, c[0], c[1])
		}
	}
	var p encParamExt
	if off := unsafe.Offsetof(p.idrBitrateRatio); off != 832+84 {
		t.Errorf("SEncParamExt.iIdrBitrateRatio is at %d, not %d", off, 832+84)
	}
}

// Every key frame refers to the one parameter set the file keeps: a video
// whose picture changes all at once decodes from its first frame.
func TestKeyFramesShareParameters(t *testing.T) {
	e, err := NewEncoder(320, 240, 10)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 6 {
		img := image.NewRGBA(image.Rect(0, 0, 320, 240))
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{uint8(i * 40), uint8(255 - i*40), 90, 255}}, image.Point{}, draw.Src)
		if err := e.Encode(img, time.Duration(i)*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	c := e.Finish(6 * time.Second)
	// The PPS's id is its first exp-Golomb number: 0 is the bit 1.
	if len(c.PPS) < 2 || c.PPS[1]&0x80 == 0 {
		t.Errorf("the picture parameter set's id is not 0: %x", c.PPS)
	}
	var out bytes.Buffer
	if err := WriteMP4(&out, []Clip{c}); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("AXX_VIDEO_OUT"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "keyframes-test.mp4"), out.Bytes(), 0o644)
	}
}

// A video's chapters are in a Nero chapter list and in a QuickTime chapter
// track the video track names: each title from its place.
func TestChapters(t *testing.T) {
	e, err := NewEncoder(320, 240, 10)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range frames(320, 240, 30) {
		if err := e.Encode(f, time.Duration(i)*100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	c := e.Finish(3 * time.Second)
	want := []Chapter{{"✓ The parcel arrives", 0}, {"✗ The parcel is lost", 1200 * time.Millisecond}, {"When the depot desk is opened", 2500 * time.Millisecond}}
	var out bytes.Buffer
	if err := WriteMP4(&out, []Clip{c}, want...); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	if dir := os.Getenv("AXX_VIDEO_OUT"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "chapters-test.mp4"), b, 0o644)
	}
	moov := child(b, "moov")
	// Nero's list.
	chpl := child(child(moov, "udta"), "chpl")
	if len(chpl) < 9 || chpl[0] != 1 || int(chpl[8]) != len(want) {
		t.Fatalf("chpl: %x", chpl[:min(len(chpl), 16)])
	}
	p := chpl[9:]
	for _, w := range want {
		at := time.Duration(binary.BigEndian.Uint64(p)) * 100
		n := int(p[8])
		if title := string(p[9 : 9+n]); title != w.Title || at != w.At {
			t.Errorf("chpl: %q at %s, want %q at %s", title, at, w.Title, w.At)
		}
		p = p[9+n:]
	}
	// QuickTime's track: the video track names track 2, a text track whose
	// samples are the titles.
	traks := children(moov, "trak")
	if len(traks) != 2 {
		t.Fatalf("%d tracks, want the video's and the chapters'", len(traks))
	}
	if chap := child(child(traks[0], "tref"), "chap"); len(chap) != 4 || binary.BigEndian.Uint32(chap) != 2 {
		t.Errorf("the video track's chapters: %x", chap)
	}
	mdia := child(traks[1], "mdia")
	if hdlr := child(mdia, "hdlr"); len(hdlr) < 8 || string(hdlr[4:8]) != "text" {
		t.Errorf("the chapter track's handler: %q", hdlr)
	}
	stbl := child(child(mdia, "minf"), "stbl")
	sizes, offsets := child(stbl, "stsz")[8:], child(stbl, "stco")[4:]
	for i, w := range want {
		at := binary.BigEndian.Uint32(offsets[4*i:])
		n := binary.BigEndian.Uint16(b[at:])
		if title := string(b[at+2 : at+2+uint32(n)]); title != w.Title {
			t.Errorf("chapter %d: %q, want %q", i, title, w.Title)
		}
		if size := binary.BigEndian.Uint32(sizes[4*i:]); size != uint32(2+n+12) {
			t.Errorf("chapter %d: %d bytes, want its title and its encd box", i, size)
		}
	}
}

// child is the body of the first box of the kind in b's boxes; children,
// every one's.
func child(b []byte, kind string) []byte {
	if c := children(b, kind); len(c) > 0 {
		return c[0]
	}
	return nil
}

func children(b []byte, kind string) [][]byte {
	var out [][]byte
	for len(b) >= 8 {
		size := int(binary.BigEndian.Uint32(b))
		if size < 8 || size > len(b) {
			break
		}
		if string(b[4:8]) == kind {
			body := b[8:size]
			// A full box's version and flags come first in these.
			if kind == "hdlr" || kind == "stsz" || kind == "stco" || kind == "mvhd" {
				body = body[4:]
			}
			out = append(out, body)
		}
		b = b[size:]
	}
	return out
}
