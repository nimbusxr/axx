package video

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

// A video's sound is one track of FLAC, which axx writes itself: lossless,
// with no codec to download, and read by the players that read the video
// (QuickTime, the browsers, VLC). It comes from a WAV file, which is what
// tools that make sound write.

// Sound is a sound track: its samples, 16 bits each, channel after channel
// for each moment (left, right, left, right for stereo).
type Sound struct {
	Rate     int // samples a second, of each channel
	Channels int // 1 or 2
	Samples  []int16
}

// Length is how long the sound lasts.
func (s *Sound) Length() time.Duration {
	if s.Rate == 0 || s.Channels == 0 {
		return 0
	}
	return time.Duration(len(s.Samples)/s.Channels) * time.Second / time.Duration(s.Rate)
}

// Fit makes the sound as long as d: cut where it is longer, silent to the
// end where it is shorter.
func (s *Sound) Fit(d time.Duration) {
	n := int(d.Seconds()*float64(s.Rate)+0.5) * s.Channels
	if n <= len(s.Samples) {
		s.Samples = s.Samples[:n]
		return
	}
	s.Samples = append(s.Samples, make([]int16, n-len(s.Samples))...)
}

// ReadWAV reads a WAV file's sound: PCM of 8, 16, 24 or 32 bits, or 32-bit
// floats, of one or two channels.
func ReadWAV(data []byte) (*Sound, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("it is no WAV file")
	}
	var format, bits int
	s := &Sound{}
	for at := 12; at+8 <= len(data); {
		id, size := string(data[at:at+4]), int(binary.LittleEndian.Uint32(data[at+4:]))
		body := data[at+8:]
		if size > len(body) {
			size = len(body) // a file written as a stream says a size it does not have
		}
		body = body[:size]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, errors.New("the WAV file's format is cut short")
			}
			format = int(binary.LittleEndian.Uint16(body))
			s.Channels = int(binary.LittleEndian.Uint16(body[2:]))
			s.Rate = int(binary.LittleEndian.Uint32(body[4:]))
			bits = int(binary.LittleEndian.Uint16(body[14:]))
			if format == 0xfffe && len(body) >= 26 {
				format = int(binary.LittleEndian.Uint16(body[24:])) // the format an extensible one stands for
			}
		case "data":
			if s.Rate == 0 {
				return nil, errors.New("the WAV file's sound comes before its format")
			}
			if s.Channels < 1 || s.Channels > 2 {
				return nil, fmt.Errorf("the WAV file has %d channels: a video's sound has one or two", s.Channels)
			}
			if s.Rate < 8000 || s.Rate > 65535 {
				return nil, fmt.Errorf("the WAV file has %d samples a second: a video's sound has from 8000 to 65535 (44100 and 48000 are the common ones)", s.Rate)
			}
			width := bits / 8
			if width == 0 {
				return nil, fmt.Errorf("the WAV file's samples are %d bits", bits)
			}
			for i := 0; i+width <= len(body); i += width {
				b := body[i:]
				var v int16
				switch {
				case format == 1 && bits == 8:
					v = int16(int(b[0])-128) << 8
				case format == 1 && bits == 16:
					v = int16(binary.LittleEndian.Uint16(b)) //nolint:gosec // a sample
				case format == 1 && bits == 24:
					v = int16(binary.LittleEndian.Uint16(b[1:])) //nolint:gosec // its top 16 bits
				case format == 1 && bits == 32:
					v = int16(binary.LittleEndian.Uint16(b[2:])) //nolint:gosec // its top 16 bits
				case format == 3 && bits == 32:
					f := float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
					v = int16(math.Round(max(-1, min(1, f)) * 32767))
				default:
					return nil, fmt.Errorf("the WAV file's samples are of a kind a video's sound is not made from (format %d, %d bits): write 16-bit PCM", format, bits)
				}
				s.Samples = append(s.Samples, v)
			}
			s.Samples = s.Samples[:len(s.Samples)/s.Channels*s.Channels]
			return s, nil
		}
		at += 8 + size + size%2
	}
	return nil, errors.New("the WAV file has no sound")
}

// flacBlock is how many samples of each channel a FLAC frame has.
const flacBlock = 4096

// flacFrames encodes the sound as FLAC frames, and their STREAMINFO, the
// block that says what they are.
func flacFrames(s *Sound) (frames [][]byte, info []byte) {
	total := len(s.Samples) / s.Channels
	for n, at := 0, 0; at < total; n, at = n+1, at+flacBlock {
		size := min(flacBlock, total-at)
		w := &bitWriter{}
		w.bits(0b11111111111110, 14) // the frame's mark
		w.bits(0, 2)                 // every frame has one size, but the last
		if size == flacBlock {
			w.bits(0b1100, 4) // 4096 samples
		} else {
			w.bits(0b0111, 4) // as the header's end says, in 16 bits
		}
		rateCode, rateBits := flacRate(s.Rate)
		w.bits(rateCode, 4)                     // the rate, said in every frame: players that take a frame by itself need it
		w.bits(uint64(s.Channels-1), 4)         //nolint:gosec // each channel by itself
		w.bits(0b100, 3)                        // 16 bits a sample
		w.bits(0, 1)                            // reserved
		w.buf = append(w.buf, utf8Number(n)...) // the frame's number
		if size != flacBlock {
			w.bits(uint64(size-1), 16) //nolint:gosec // a block's size
		}
		if rateBits {
			w.bits(uint64(s.Rate), 16) //nolint:gosec // 8000 to 65535
		}
		w.buf = append(w.buf, crc8(w.buf))
		block := make([]int64, size)
		for c := range s.Channels {
			for i := range size {
				block[i] = int64(s.Samples[(at+i)*s.Channels+c])
			}
			subframe(w, block)
		}
		w.flush()
		crc := crc16(w.buf)
		w.buf = append(w.buf, byte(crc>>8), byte(crc))
		frames = append(frames, w.buf)
	}
	i := &bitWriter{}
	i.bits(flacBlock, 16)
	i.bits(flacBlock, 16)
	i.bits(0, 24)                              // the smallest frame: not said
	i.bits(0, 24)                              // the largest frame: not said
	i.bits(uint64(s.Rate), 20)                 //nolint:gosec // 8000 to 65535
	i.bits(uint64(s.Channels-1), 3)            //nolint:gosec // 1 or 2
	i.bits(15, 5)                              // 16 bits a sample
	i.bits(uint64(total), 36)                  //nolint:gosec // the sound's samples
	i.buf = append(i.buf, make([]byte, 16)...) // the samples' MD5: not said
	return frames, i.buf
}

// flacRate is a frame header's code of a rate: one of the rates FLAC has a
// code for, or the code that says the rate follows at the header's end, in
// hertz and 16 bits.
func flacRate(rate int) (code uint64, follows bool) {
	codes := map[int]uint64{8000: 4, 16000: 5, 22050: 6, 24000: 7, 32000: 8, 44100: 9, 48000: 10}
	if c, ok := codes[rate]; ok {
		return c, false
	}
	return 13, true
}

// subframe writes one channel's block: one value where it is all the same
// (silence), otherwise what is left of each sample once it is guessed from
// those before it, in Rice's code.
func subframe(w *bitWriter, block []int64) {
	same := true
	for _, v := range block {
		if v != block[0] {
			same = false
			break
		}
	}
	if same {
		w.bits(0, 8)                        // a constant
		w.bits(uint64(block[0])&0xffff, 16) //nolint:gosec // a sample
		return
	}
	// The best of the fixed guesses: from none of the samples before to four.
	best, bestSum := 0, int64(math.MaxInt64)
	var residuals [5][]int64
	for order := 0; order <= 4 && order < len(block); order++ {
		r := make([]int64, 0, len(block)-order)
		var sum int64
		for i := order; i < len(block); i++ {
			var guess int64
			switch order {
			case 1:
				guess = block[i-1]
			case 2:
				guess = 2*block[i-1] - block[i-2]
			case 3:
				guess = 3*block[i-1] - 3*block[i-2] + block[i-3]
			case 4:
				guess = 4*block[i-1] - 6*block[i-2] + 4*block[i-3] - block[i-4]
			}
			d := block[i] - guess
			r = append(r, d)
			if d < 0 {
				d = -d
			}
			sum += d
		}
		residuals[order] = r
		if sum < bestSum {
			best, bestSum = order, sum
		}
	}
	r := residuals[best]
	// Rice's parameter that takes the fewest bits.
	k, bits := 0, int64(math.MaxInt64)
	for p := 0; p <= 14; p++ {
		var n int64
		for _, d := range r {
			n += int64(fold(d)>>p) + 1 + int64(p) //nolint:gosec // a count of bits
		}
		if n < bits {
			k, bits = p, n
		}
	}
	w.bits(uint64(0b001000|best)<<1, 8) //nolint:gosec // a fixed guess of that order
	for i := range best {
		w.bits(uint64(block[i])&0xffff, 16) //nolint:gosec // a sample
	}
	w.bits(0, 2)         // Rice's code, with parameters of 4 bits
	w.bits(0, 4)         // one partition
	w.bits(uint64(k), 4) //nolint:gosec // 0 to 14
	for _, d := range r {
		u := fold(d)
		for q := u >> k; q > 0; q-- { //nolint:gosec // 0 to 14
			w.bits(0, 1)
		}
		w.bits(1, 1)
		w.bits(u&(1<<k-1), k) //nolint:gosec // 0 to 14
	}
}

// fold puts the negative numbers between the positive ones: 0, -1, 1, -2...
func fold(d int64) uint64 {
	return uint64(d<<1) ^ uint64(d>>63) //nolint:gosec // the fold
}

// utf8Number is a frame's number as FLAC writes it: as UTF-8 writes a
// character of that number.
func utf8Number(n int) []byte {
	switch {
	case n < 0x80:
		return []byte{byte(n)}
	case n < 0x800:
		return []byte{0xc0 | byte(n>>6), 0x80 | byte(n&0x3f)}
	case n < 0x10000:
		return []byte{0xe0 | byte(n>>12), 0x80 | byte(n>>6&0x3f), 0x80 | byte(n&0x3f)}
	}
	return []byte{0xf0 | byte(n>>18), 0x80 | byte(n>>12&0x3f), 0x80 | byte(n>>6&0x3f), 0x80 | byte(n&0x3f)}
}

type bitWriter struct {
	buf  []byte
	cur  uint64
	have int
}

// bits writes the n lowest bits of v, the highest first.
func (w *bitWriter) bits(v uint64, n int) {
	for n > 0 {
		take := min(n, 8-w.have)
		w.cur = w.cur<<take | v>>(n-take)&(1<<take-1)
		w.have += take
		n -= take
		if w.have == 8 {
			w.buf = append(w.buf, byte(w.cur))
			w.cur, w.have = 0, 0
		}
	}
}

// flush fills the last byte with zeros.
func (w *bitWriter) flush() {
	if w.have > 0 {
		w.bits(0, 8-w.have)
	}
}

func crc8(b []byte) byte {
	var crc byte
	for _, v := range b {
		crc ^= v
		for range 8 {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func crc16(b []byte) uint16 {
	var crc uint16
	for _, v := range b {
		crc ^= uint16(v) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x8005
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
