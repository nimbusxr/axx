package video

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// WriteMP4 writes clips, one after another, as one MP4 video: an H.264
// track whose index comes first, so a browser plays it as it loads. The
// clips have one size; a clip whose parameter sets differ from the first's
// carries its own in its first frame.
func WriteMP4(w io.Writer, clips []Clip) error {
	if len(clips) == 0 || len(clips[0].Samples) == 0 {
		return errors.New("a video has at least one frame")
	}
	first := clips[0]
	if len(first.SPS) < 4 || len(first.PPS) == 0 {
		return errors.New("the video's first clip has no parameter sets")
	}
	var samples []mp4Sample
	var length time.Duration
	for _, c := range clips {
		if c.Width != first.Width || c.Height != first.Height {
			return fmt.Errorf("a video's clips have one size: %dx%d, not %dx%d", first.Width, first.Height, c.Width, c.Height)
		}
		inband := !bytes.Equal(c.SPS, first.SPS) || !bytes.Equal(c.PPS, first.PPS)
		for i, s := range c.Samples {
			end := c.End
			if i+1 < len(c.Samples) {
				end = c.Samples[i+1].At
			}
			data := s.Data
			if i == 0 && inband {
				data = append(append(lengthPrefixed(c.SPS), lengthPrefixed(c.PPS)...), data...)
			}
			ms := max(1, (end - s.At).Milliseconds())
			samples = append(samples, mp4Sample{data: data, key: s.Key, ms: ms})
			length += time.Duration(ms) * time.Millisecond
		}
	}
	ftyp := box("ftyp", []byte("isom"), u32(0x200), []byte("isomiso2avc1mp41"))
	var mdatSize int
	for _, s := range samples {
		mdatSize += len(s.data)
	}
	moov := moovBox(first, samples, length, 0)
	offset := len(ftyp) + len(moov) + 8
	if offset+mdatSize > 1<<32-1 {
		return errors.New("a video is under 4 GB")
	}
	moov = moovBox(first, samples, length, offset)
	for _, b := range [][]byte{ftyp, moov, u32(uint32(8 + mdatSize)), []byte("mdat")} { //nolint:gosec // under 4 GB
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	for _, s := range samples {
		if _, err := w.Write(s.data); err != nil {
			return err
		}
	}
	return nil
}

type mp4Sample struct {
	data []byte
	key  bool
	ms   int64
}

func lengthPrefixed(nal []byte) []byte {
	return append(u32(uint32(len(nal))), nal...) //nolint:gosec // a NAL unit's size
}

// moovBox is the video's index; its samples' data starts at offset in the
// file.
func moovBox(c Clip, samples []mp4Sample, d time.Duration, offset int) []byte {
	dur := u32(uint32(d.Milliseconds())) //nolint:gosec // a run's length
	matrix := concat(u32(0x10000), u32(0), u32(0), u32(0), u32(0x10000), u32(0), u32(0), u32(0), u32(0x40000000))
	mvhd := fullBox("mvhd", 0, u32(0), u32(0), u32(1000), dur, u32(0x10000), u16(0x100), make([]byte, 10), matrix, make([]byte, 24), u32(2))
	w, h := uint32(c.Width), uint32(c.Height) //nolint:gosec // a screen's size
	tkhd := fullBox("tkhd", 3, u32(0), u32(0), u32(1), u32(0), dur, make([]byte, 8), u16(0), u16(0), u16(0), u16(0), matrix, u32(w<<16), u32(h<<16))
	mdhd := fullBox("mdhd", 0, u32(0), u32(0), u32(1000), dur, u16(0x55c4), u16(0)) // und
	hdlr := fullBox("hdlr", 0, u32(0), []byte("vide"), make([]byte, 12), []byte("VideoHandler\x00"))
	vmhd := fullBox("vmhd", 1, u16(0), make([]byte, 6))
	dinf := box("dinf", fullBox("dref", 0, u32(1), fullBox("url ", 1)))

	avcC := box("avcC", []byte{1, c.SPS[1], c.SPS[2], c.SPS[3], 0xff, 0xe1}, u16(uint16(len(c.SPS))), c.SPS, //nolint:gosec // a parameter set
		[]byte{1}, u16(uint16(len(c.PPS))), c.PPS) //nolint:gosec // a parameter set
	compressor := make([]byte, 32)
	avc1 := box("avc1", make([]byte, 6), u16(1), make([]byte, 16), u16(uint16(w)), u16(uint16(h)), //nolint:gosec // a screen's size
		u32(0x480000), u32(0x480000), u32(0), u16(1), compressor, u16(0x18), u16(0xffff), avcC)
	stsd := fullBox("stsd", 0, u32(1), avc1)

	var stts, stss, stsz, stco bytes.Buffer
	var runs, syncs uint32
	for i, s := range samples {
		if i == 0 || s.ms != samples[i-1].ms {
			stts.Write(u32(1))
			stts.Write(u32(uint32(s.ms))) //nolint:gosec // a frame's length
			runs++
		} else {
			n := stts.Bytes()
			binary.BigEndian.PutUint32(n[len(n)-8:], binary.BigEndian.Uint32(n[len(n)-8:])+1)
		}
		if s.key {
			stss.Write(u32(uint32(i + 1))) //nolint:gosec // a frame's number
			syncs++
		}
		stsz.Write(u32(uint32(len(s.data)))) //nolint:gosec // a frame's size
		stco.Write(u32(uint32(offset)))      //nolint:gosec // under 4 GB
		offset += len(s.data)
	}
	n := u32(uint32(len(samples))) //nolint:gosec // a video's frames
	stbl := box("stbl", stsd,
		fullBox("stts", 0, u32(runs), stts.Bytes()),
		fullBox("stss", 0, u32(syncs), stss.Bytes()),
		fullBox("stsc", 0, u32(1), u32(1), u32(1), u32(1)),
		fullBox("stsz", 0, u32(0), n, stsz.Bytes()),
		fullBox("stco", 0, n, stco.Bytes()))
	minf := box("minf", vmhd, dinf, stbl)
	mdia := box("mdia", mdhd, hdlr, minf)
	return box("moov", mvhd, box("trak", tkhd, mdia))
}

func box(kind string, parts ...[]byte) []byte {
	body := concat(parts...)
	return concat(u32(uint32(8+len(body))), []byte(kind), body) //nolint:gosec // a box's size
}

func fullBox(kind string, flags uint32, parts ...[]byte) []byte {
	return box(kind, append([]byte{0, byte(flags >> 16), byte(flags >> 8), byte(flags)}, concat(parts...)...))
}

func concat(parts ...[]byte) []byte {
	var b bytes.Buffer
	for _, p := range parts {
		b.Write(p)
	}
	return b.Bytes()
}

func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
