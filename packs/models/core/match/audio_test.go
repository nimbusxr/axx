package match

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func tone(rate, n int, freqs ...float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		for _, f := range freqs {
			out[i] += 0.3 * math.Sin(2*math.Pi*f*float64(i)/float64(rate))
		}
	}
	return out
}

func soundInput(n, rate int) *contract.Tensor {
	return &contract.Tensor{Name: "samples", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: int64(n)}}, Audio: &contract.Audio{SampleRate: rate}}
}

// A sound's spectrum is much the same wherever the window begins, and
// however loud it is; another sound's is not.
func TestASoundsSpectrumIsItsOwn(t *testing.T) {
	r, err := NewReducer(soundInput(15600, 16000))
	if err != nil {
		t.Fatal(err)
	}
	if r.Frame != 260 || r.Bins() != 130 || r.Dims() != Bands {
		t.Fatalf("frame %d, bins %d, dims %d", r.Frame, r.Bins(), r.Dims())
	}
	bell := tone(16000, 30000, 660, 880)
	key, _ := r.Reduce(bell[:15600])
	later, _ := r.Reduce(bell[1234 : 1234+15600])
	quiet := make([]float64, 15600)
	for i := range quiet {
		quiet[i] = 0.2 * bell[i]
	}
	soft, _ := r.Reduce(quiet)
	hum, _ := r.Reduce(tone(16000, 15600, 120))
	if s := Similarity(key, later); s < 0.95 {
		t.Errorf("the sound later is %.3f alike", s)
	}
	if s := Similarity(key, soft); s < 0.999 {
		t.Errorf("the sound quieter is %.3f alike", s)
	}
	if s := Similarity(key, hum); s > 0.5 {
		t.Errorf("another sound is %.3f alike", s)
	}
}

func TestASoundsFrameDividesIt(t *testing.T) {
	for n, want := range map[int]int{15600: 260, 16000: 250, 44032: 256, 8000: 250} {
		if got := audioFrame(n); got != want {
			t.Errorf("%d samples: a frame of %d, want %d", n, got, want)
		}
	}
	if _, err := NewReducer(soundInput(15601, 16000)); err == nil {
		t.Error("a prime number of samples has a frame")
	}
}

func writeWAV(t *testing.T, format, channels, bits, rate int, data []byte) string {
	t.Helper()
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00")
	b = binary.LittleEndian.AppendUint16(b, uint16(format))
	b = binary.LittleEndian.AppendUint16(b, uint16(channels))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate*channels*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(channels*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(bits))
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	b = append(b, data...)
	path := filepath.Join(t.TempDir(), "sound.wav")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestASoundIsRead(t *testing.T) {
	// Two channels of 16 bits: their mean.
	var pcm []byte
	for _, v := range []int16{16384, -16384, 32767, 32767} {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(v))
	}
	got, err := ReadSound(writeWAV(t, 1, 2, 16, 8000, pcm), 8000)
	if err != nil || len(got) != 2 || got[0] != 0 || math.Abs(got[1]-32767.0/32768) > 1e-9 {
		t.Errorf("%v, %v", got, err)
	}
	// 32-bit floats, resampled from 8 kHz to 16 kHz: every other sample the
	// file's, the ones between them its sinc's (FFmpeg's, below).
	var fl []byte
	for _, v := range []float32{0, 1, 0, -1} {
		fl = binary.LittleEndian.AppendUint32(fl, math.Float32bits(v))
	}
	got, err = ReadSound(writeWAV(t, 3, 1, 32, 8000, fl), 16000)
	if err != nil || len(got) != 8 || math.Abs(got[2]-1) > 1e-12 || math.Abs(got[4]) > 1e-12 || got[1] <= 0 || got[1] >= 1 {
		t.Errorf("resampled %v, %v", got, err)
	}
	if _, err := ReadSound(writeWAV(t, 2, 1, 4, 8000, []byte{1, 2}), 8000); err == nil {
		t.Error("ADPCM is read")
	}
}

// A telephone's sound (8 kHz) is resampled to 16 kHz as FFmpeg 8's
// libswresample resamples it (testdata: half a second of a voicemail, and
// what `ffmpeg -ar 16000 -sample_fmt s16` makes of it), to its rounding to
// 16 bits.
func TestASoundIsResampledAsFFmpegDoes(t *testing.T) {
	ours, err := ReadSound("testdata/telephone-8k.wav", 16000)
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg, err := ReadSound("testdata/telephone-16k-ffmpeg.wav", 16000)
	if err != nil {
		t.Fatal(err)
	}
	if len(ours) != len(ffmpeg) {
		t.Fatalf("%d samples, FFmpeg's %d", len(ours), len(ffmpeg))
	}
	var noise, signal float64
	for i := range ours {
		if d := math.Abs(ours[i] - ffmpeg[i]); d > 2.0/32768 {
			t.Fatalf("sample %d is %v, FFmpeg's %v", i, ours[i], ffmpeg[i])
		}
		noise += (ours[i] - ffmpeg[i]) * (ours[i] - ffmpeg[i])
		signal += ffmpeg[i] * ffmpeg[i]
	}
	if snr := 10 * math.Log10(signal/noise); snr < 70 {
		t.Errorf("FFmpeg's sound to the difference: %.1f dB", snr)
	}
}
