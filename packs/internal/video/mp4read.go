package video

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Track is the H.264 video track of an MP4 (or QuickTime) file, as ReadMP4
// reads it.
type Track struct {
	Width, Height int
	// SPS and PPS are its parameter sets, as its avcC box keeps them.
	SPS, PPS [][]byte
	// LengthSize is how many bytes a NAL unit's length takes in a sample.
	LengthSize int
	// Samples are its frames, in the order they decode.
	Samples []TrackSample
	// Duration is how long it lasts.
	Duration time.Duration
}

// TrackSample is a frame of a Track: its NAL units, each after its length.
type TrackSample struct {
	Data []byte
	// At is when it shows, from the track's first frame on.
	At  time.Duration
	Key bool
}

// ReadMP4 reads the first video track of an MP4 or QuickTime file, which is
// to be H.264.
func ReadMP4(data []byte) (*Track, error) {
	moov, ok := mp4Child(data, "moov")
	if !ok {
		return nil, errors.New("it is no MP4 or QuickTime video: it has no moov box")
	}
	for _, trak := range mp4Children(moov, "trak") {
		mdia, _ := mp4Child(trak, "mdia")
		if hdlr, ok := mp4Child(mdia, "hdlr"); !ok || len(hdlr) < 12 || string(hdlr[8:12]) != "vide" {
			continue
		}
		return readTrack(data, mdia)
	}
	return nil, errors.New("it has no video track")
}

func readTrack(data, mdia []byte) (*Track, error) {
	timescale, err := timeScale(mdia)
	if err != nil {
		return nil, err
	}
	minf, _ := mp4Child(mdia, "minf")
	stbl, ok := mp4Child(minf, "stbl")
	if !ok {
		return nil, errors.New("its video track has no sample table")
	}
	t := &Track{}
	if err := t.readEntry(stbl); err != nil {
		return nil, err
	}
	sizes, err := sampleSizes(stbl)
	if err != nil {
		return nil, err
	}
	offsets, err := sampleOffsets(stbl, len(sizes))
	if err != nil {
		return nil, err
	}
	deltas := runs(stbl, "stts", len(sizes))
	shifts := runs(stbl, "ctts", len(sizes))
	stss, synced := mp4Child(stbl, "stss")
	keys := map[int]bool{}
	if synced {
		for _, n := range table(stss, 4) {
			keys[int(binary.BigEndian.Uint32(n))-1] = true
		}
	}
	var dts, first, end int64
	first = -1
	pts := make([]int64, len(sizes))
	for i, size := range sizes {
		o := offsets[i]
		if o < 0 || o+int64(size) > int64(len(data)) {
			return nil, fmt.Errorf("its frame %d is outside the file", i+1)
		}
		// ctts's offsets are signed in its version 1, and in practice in 0 too.
		pts[i] = dts + int64(int32(shifts[i])) //nolint:gosec // a signed offset
		if first < 0 || pts[i] < first {
			first = pts[i]
		}
		end = max(end, pts[i]+int64(deltas[i]))
		t.Samples = append(t.Samples, TrackSample{Data: data[o : o+int64(size)], Key: !synced || keys[i]})
		dts += int64(deltas[i])
	}
	scale := func(v int64) time.Duration { return time.Duration(v) * time.Second / time.Duration(timescale) }
	for i := range t.Samples {
		t.Samples[i].At = scale(pts[i] - first)
	}
	t.Duration = scale(end - first)
	return t, nil
}

// timeScale is the units a second the track's times count, from its mdhd box.
func timeScale(mdia []byte) (uint32, error) {
	mdhd, ok := mp4Child(mdia, "mdhd")
	at := 12
	if ok && len(mdhd) > 0 && mdhd[0] == 1 {
		at = 20
	}
	if !ok || len(mdhd) < at+4 || binary.BigEndian.Uint32(mdhd[at:]) == 0 {
		return 0, errors.New("its video track has no time scale")
	}
	return binary.BigEndian.Uint32(mdhd[at:]), nil
}

// readEntry reads the track's sample entry: H.264's avc1 (or avc3), its size
// and its avcC parameter sets.
func (t *Track) readEntry(stbl []byte) error {
	stsd, ok := mp4Child(stbl, "stsd")
	if !ok || len(stsd) < 16 {
		return errors.New("its video track describes no frames")
	}
	entry := stsd[8:]
	size, kind := binary.BigEndian.Uint32(entry), string(entry[4:8])
	switch kind {
	case "avc1", "avc3":
	case "hvc1", "hev1":
		return errors.New("its video is HEVC (H.265): axx reads H.264 videos")
	default:
		return fmt.Errorf("its video is %q, not H.264", kind)
	}
	// A VisualSampleEntry: 8 bytes of box header, 24 of fields, width and height, 50 more;
	// then its boxes.
	if int(size) > len(entry) || size < 86 {
		return errors.New("its H.264 sample entry is short")
	}
	t.Width, t.Height = int(binary.BigEndian.Uint16(entry[32:])), int(binary.BigEndian.Uint16(entry[34:]))
	avcc, ok := mp4Child(entry[86:size], "avcC")
	if !ok || len(avcc) < 7 {
		return errors.New("its H.264 video has no avcC box")
	}
	t.LengthSize = int(avcc[4]&3) + 1
	rest := avcc[5:]
	for i, sets := range []*[][]byte{&t.SPS, &t.PPS} {
		if len(rest) < 1 {
			return errors.New("its avcC box is short")
		}
		n := int(rest[0])
		if i == 0 {
			n &= 0x1f
		}
		rest = rest[1:]
		for range n {
			if len(rest) < 2 || len(rest) < 2+int(binary.BigEndian.Uint16(rest)) {
				return errors.New("its avcC box is short")
			}
			l := int(binary.BigEndian.Uint16(rest))
			*sets = append(*sets, rest[2:2+l])
			rest = rest[2+l:]
		}
	}
	if len(t.SPS) == 0 || len(t.PPS) == 0 {
		return errors.New("its avcC box has no parameter sets")
	}
	return nil
}

// sampleSizes are the frames' sizes, from the stsz box.
func sampleSizes(stbl []byte) ([]uint32, error) {
	stsz, ok := mp4Child(stbl, "stsz")
	if !ok || len(stsz) < 12 {
		return nil, errors.New("its video track gives no frame sizes")
	}
	fixed, n := binary.BigEndian.Uint32(stsz[4:]), int(binary.BigEndian.Uint32(stsz[8:]))
	sizes := make([]uint32, n)
	for i := range sizes {
		if fixed != 0 {
			sizes[i] = fixed
			continue
		}
		o := 12 + 4*i
		if o+4 > len(stsz) {
			return nil, errors.New("its stsz box is short")
		}
		sizes[i] = binary.BigEndian.Uint32(stsz[o:])
	}
	return sizes, nil
}

// sampleOffsets are where in the file each of n frames is: the chunks' offsets (stco or co64)
// and how many frames each chunk holds (stsc).
func sampleOffsets(stbl []byte, n int) ([]int64, error) {
	var chunks []int64
	if stco, ok := mp4Child(stbl, "stco"); ok {
		for _, e := range table(stco, 4) {
			chunks = append(chunks, int64(binary.BigEndian.Uint32(e)))
		}
	} else if co64, ok := mp4Child(stbl, "co64"); ok {
		for _, e := range table(co64, 8) {
			chunks = append(chunks, int64(binary.BigEndian.Uint64(e))) //nolint:gosec // a file offset
		}
	} else {
		return nil, errors.New("its video track gives no chunk offsets")
	}
	stsc, ok := mp4Child(stbl, "stsc")
	if !ok {
		return nil, errors.New("its video track gives no chunks' frames")
	}
	entries := table(stsc, 12)
	sizes, err := sampleSizes(stbl)
	if err != nil {
		return nil, err
	}
	offsets := make([]int64, 0, n)
	for e, entry := range entries {
		firstChunk := int(binary.BigEndian.Uint32(entry)) - 1
		perChunk := int(binary.BigEndian.Uint32(entry[4:]))
		lastChunk := len(chunks)
		if e+1 < len(entries) {
			lastChunk = int(binary.BigEndian.Uint32(entries[e+1])) - 1
		}
		for c := firstChunk; c < lastChunk && c < len(chunks); c++ {
			o := chunks[c]
			for range perChunk {
				if len(offsets) == n {
					return offsets, nil
				}
				offsets = append(offsets, o)
				o += int64(sizes[len(offsets)-1])
			}
		}
	}
	if len(offsets) < n {
		return nil, fmt.Errorf("its chunks hold %d of its %d frames", len(offsets), n)
	}
	return offsets, nil
}

// runs expands a box of (count, value) runs, stts's or ctts's, into a value for each of n
// frames; zero where the box is missing or ends.
func runs(stbl []byte, kind string, n int) []uint32 {
	out := make([]uint32, 0, n)
	if box, ok := mp4Child(stbl, kind); ok {
		for _, e := range table(box, 8) {
			for range binary.BigEndian.Uint32(e) {
				if len(out) == n {
					return out
				}
				out = append(out, binary.BigEndian.Uint32(e[4:]))
			}
		}
	}
	for len(out) < n {
		out = append(out, 0)
	}
	return out
}

// table is a full box's entries of size bytes each, after its entry count.
func table(box []byte, size int) [][]byte {
	if len(box) < 8 {
		return nil
	}
	n := int(binary.BigEndian.Uint32(box[4:]))
	out := make([][]byte, 0, n)
	for i := range n {
		o := 8 + size*i
		if o+size > len(box) {
			break
		}
		out = append(out, box[o:o+size])
	}
	return out
}

// mp4Child is the content of the first box of that kind in data.
func mp4Child(data []byte, kind string) ([]byte, bool) {
	for b := range boxes(data) {
		if b.kind == kind {
			return b.body, true
		}
	}
	return nil, false
}

// mp4Children are the contents of the boxes of that kind in data.
func mp4Children(data []byte, kind string) [][]byte {
	var out [][]byte
	for b := range boxes(data) {
		if b.kind == kind {
			out = append(out, b.body)
		}
	}
	return out
}

type mp4Box struct {
	kind string
	body []byte
}

// boxes are the boxes in data, one after another.
func boxes(data []byte) func(func(mp4Box) bool) {
	return func(yield func(mp4Box) bool) {
		for len(data) >= 8 {
			size, head := uint64(binary.BigEndian.Uint32(data)), 8
			switch size {
			case 0:
				size = uint64(len(data))
			case 1:
				if len(data) < 16 {
					return
				}
				size, head = binary.BigEndian.Uint64(data[8:]), 16
			}
			if size < uint64(head) || size > uint64(len(data)) {
				return
			}
			if !yield(mp4Box{kind: string(data[4:8]), body: data[head:size]}) {
				return
			}
			data = data[size:]
		}
	}
}
