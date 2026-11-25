package video

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// wav is a sound as a WAV file of 16-bit PCM.
func wav(s *Sound) []byte {
	var b bytes.Buffer
	data := len(s.Samples) * 2
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(s.Channels), uint32(s.Rate), uint32(s.Rate * s.Channels * 2), uint16(s.Channels * 2), uint16(16)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(data))
	_ = binary.Write(&b, binary.LittleEndian, s.Samples)
	return b.Bytes()
}

// voice is a sound like a voice track: a tone that swells, noise, and silence
// between, of a length that is no whole number of frames.
func voice(rate, channels int) *Sound {
	s := &Sound{Rate: rate, Channels: channels}
	seed := uint32(7)
	for i := range rate*2 + 1234 {
		for c := range channels {
			var v float64
			switch {
			case i < rate/2:
				v = math.Sin(float64(i)*2*math.Pi*220/float64(rate)) * float64(i) / float64(rate/2) * 12000
			case i < rate:
				v = 0 // silence
			case i < rate*3/2:
				seed = seed*1664525 + 1013904223
				v = float64(int32(seed)>>18) * float64(c+1) / 2
			default:
				v = math.Sin(float64(i)*2*math.Pi*880/float64(rate))*30000 + float64(c*100)
			}
			s.Samples = append(s.Samples, int16(v))
		}
	}
	return s
}

func TestReadWAV(t *testing.T) {
	want := voice(22050, 2)
	got, err := ReadWAV(wav(want))
	if err != nil {
		t.Fatal(err)
	}
	if got.Rate != 22050 || got.Channels != 2 || len(got.Samples) != len(want.Samples) || got.Samples[4321] != want.Samples[4321] {
		t.Errorf("read %d Hz, %d channels, %d samples", got.Rate, got.Channels, len(got.Samples))
	}
	// As long as the picture: cut, or silent to its end.
	got.Fit(time.Second)
	if len(got.Samples) != 22050*2 {
		t.Errorf("cut to a second: %d samples", len(got.Samples))
	}
	got.Fit(3 * time.Second)
	if len(got.Samples) != 22050*2*3 || got.Samples[len(got.Samples)-1] != 0 || got.Length() != 3*time.Second {
		t.Errorf("made three seconds long: %d samples, %s", len(got.Samples), got.Length())
	}
	if _, err := ReadWAV([]byte("ID3 not a wav file at all")); err == nil {
		t.Error("something that is no WAV file was read")
	}
}

// The sound track axx writes is FLAC that another decoder reads back sample
// for sample: ffmpeg, where the machine has it.
func TestASoundTrackIsReadBack(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg on this machine to read the sound back with")
	}
	for _, channels := range []int{1, 2} {
		sound := voice(22050, channels)
		// A picture of one frame that nothing decodes here: the sound is what is read.
		clip := Clip{Width: 16, Height: 16, SPS: []byte{0x67, 0x42, 0xc0, 0x0a}, PPS: []byte{0x68, 0xce}, Samples: []Sample{{Data: []byte{0, 0, 0, 1, 0x65}, Key: true}}, End: sound.Length()}
		var film bytes.Buffer
		if err := WriteMP4Sound(&film, []Clip{clip}, sound, Chapter{Title: "One"}); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		in, out := filepath.Join(dir, "film.mp4"), filepath.Join(dir, "sound.raw")
		if err := os.WriteFile(in, film.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(context.Background(), ffmpeg, "-loglevel", "error", "-y", "-i", in, "-vn", "-map", "0:a:0", "-f", "s16le", "-acodec", "pcm_s16le", out)
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg cannot read the sound: %v\n%s", err, msg)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]int16, len(raw)/2)
		_ = binary.Read(bytes.NewReader(raw), binary.LittleEndian, got)
		if len(got) != len(sound.Samples) {
			t.Fatalf("%d channels: read back %d samples of %d", channels, len(got), len(sound.Samples))
		}
		for i := range got {
			if got[i] != sound.Samples[i] {
				t.Fatalf("%d channels: sample %d is %d, was %d", channels, i, got[i], sound.Samples[i])
			}
		}
	}
}
