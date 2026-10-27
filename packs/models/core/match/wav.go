package match

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/nimbusxr/axx/packs/models/core/internal/detmath"
)

// ReadSound reads a WAV file's samples (PCM of 8, 16, 24 or 32 bits, or
// 32-bit floats), its channels mixed, from −1 to 1, at the rate given:
// resampled from the file's as the runtimes resample a file they read
// (Resample).
func ReadSound(path string, rate int) ([]float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	samples, fileRate, err := decodeWAV(b)
	if err != nil {
		return nil, fmt.Errorf("%s is not a sound axx reads (a WAV file of PCM or 32-bit floats): %w", path, err)
	}
	return Resample(samples, fileRate, rate), nil
}

// DecodeSound decodes a WAV file's bytes (PCM or 32-bit floats) at its own sample rate, as one
// channel, from -1 to 1.
func DecodeSound(b []byte) ([]float64, int, error) { return decodeWAV(b) }

// ReadSoundRate reads a sound file (a WAV of PCM or 32-bit floats) at its
// own sample rate, as one channel, from -1 to 1.
func ReadSoundRate(path string) ([]float64, int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	samples, rate, err := decodeWAV(b)
	if err != nil {
		return nil, 0, fmt.Errorf("%s is not a sound axx reads (a WAV file of PCM or 32-bit floats): %w", path, err)
	}
	return samples, rate, nil
}

// Resample resamples sound from one rate to another as FFmpeg's
// libswresample does by default, which the runtimes that read sound files
// resample with (faster-whisper's PyAV, OpenAI's whisper's and transformers'
// ffmpeg): each sample a Kaiser-windowed sinc's (β 9) sum of the samples
// around it, of 32 taps at the lower rate, its cutoff the input's Nyquist
// frequency when it raises the rate (a telephone's 8 kHz to 16 kHz: no
// images above 4 kHz, which a linear interpolation's would have), and 0.97
// of the output's when it lowers it. Past its ends, the sound is mirrored,
// as libswresample's is.
func Resample(samples []float64, from, to int) []float64 {
	if from == to || len(samples) == 0 {
		return samples
	}
	factor := min(float64(to)*0.97/float64(from), 1)
	taps := int(math.Ceil(32 / factor))
	center := (taps - 1) / 2
	kernel := func(d float64) float64 {
		x := float64(float64(math.Pi*d) * factor)
		y := 1.0
		if x != 0 {
			y = detmath.Sin(x) / x
		}
		w := 2 * x / (factor * float64(taps) * math.Pi)
		return float64(y * besselI0(kaiserBeta*math.Sqrt(max(1-float64(w*w), 0))))
	}
	var norm float64
	for i := range taps {
		norm += float64(kernel(float64(i - center)))
	}
	// An output sample k is at the input's k·from/to: whole samples and a
	// phase of the reduced rates' ratio, each phase's filter worked out once.
	g := gcd(from, to)
	step, phases := from/g, to/g
	filters := map[int][]float64{}
	filter := func(ph int) []float64 {
		if f, ok := filters[ph]; ok {
			return f
		}
		f := make([]float64, taps)
		frac := float64(ph) / float64(phases)
		for i := range f {
			f[i] = kernel(float64(i-center)-frac) / norm
		}
		filters[ph] = f
		return f
	}
	out := make([]float64, int(math.Round(float64(len(samples))*float64(to)/float64(from))))
	for k := range out {
		pos := k * step
		index, f := pos/phases, filter(pos%phases)
		var v float64
		for i, c := range f {
			v += float64(samples[mirrored(index+i-center, len(samples))] * c)
		}
		out[k] = v
	}
	return out
}

// mirrored is the sample a resampling's filter reads at j, of a sound of n
// samples, as libswresample reads past its ends: the sound mirrored at its
// first sample, and after its last.
func mirrored(j, n int) int {
	for j < 0 || j >= n {
		if j < 0 {
			j = -j
		}
		if j >= n {
			j = 2*n - 1 - j
		}
	}
	return j
}

// kaiserBeta is libswresample's Kaiser window's β.
const kaiserBeta = 9

// besselI0 is the modified Bessel function of the first kind, of order 0:
// the sum of ((x/2)^k / k!)².
func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; term > 1e-17*sum; k++ {
		term = float64(term * float64((x/2/float64(k))*(x/2/float64(k))))
		sum += term
	}
	return sum
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func decodeWAV(b []byte) ([]float64, int, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, errors.New("no RIFF WAVE header")
	}
	var format, channels, bits int
	var rate int
	var data []byte
	for p := 12; p+8 <= len(b); {
		id, size := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:p+8]))
		body := b[p+8 : min(p+8+size, len(b))]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, 0, errors.New("a short fmt chunk")
			}
			format = int(binary.LittleEndian.Uint16(body[0:2]))
			channels = int(binary.LittleEndian.Uint16(body[2:4]))
			rate = int(binary.LittleEndian.Uint32(body[4:8]))
			bits = int(binary.LittleEndian.Uint16(body[14:16]))
			if format == 0xfffe && len(body) >= 26 { // WAVE_FORMAT_EXTENSIBLE: the subformat's
				format = int(binary.LittleEndian.Uint16(body[24:26]))
			}
		case "data":
			data = body
		}
		p += 8 + size + size%2
	}
	if channels == 0 || data == nil {
		return nil, 0, errors.New("no fmt or data chunk")
	}
	width := bits / 8
	if width == 0 || (format == 1 && width > 4) || (format == 3 && width != 4) || (format != 1 && format != 3) {
		return nil, 0, fmt.Errorf("samples of format %d and %d bits", format, bits)
	}
	frames := len(data) / (width * channels)
	out := make([]float64, frames)
	for f := range frames {
		var sum float64
		for c := range channels {
			s := data[(f*channels+c)*width:]
			var v float64
			switch {
			case format == 3:
				v = float64(math.Float32frombits(binary.LittleEndian.Uint32(s)))
			case width == 1:
				v = (float64(s[0]) - 128) / 128
			case width == 2:
				v = float64(int16(binary.LittleEndian.Uint16(s))) / 32768
			case width == 3:
				v = float64(int32(uint32(s[0])<<8|uint32(s[1])<<16|uint32(s[2])<<24)>>8) / 8388608
			case width == 4:
				v = float64(int32(binary.LittleEndian.Uint32(s))) / 2147483648
			}
			sum += v
		}
		out[f] = sum / float64(channels)
	}
	return out, rate, nil
}
