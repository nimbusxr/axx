package demo

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A film's subtitles are written with their times, as SRT: of the rows of an
// outline, each after the one before.
func TestTheSubtitlesWithTheirTimes(t *testing.T) {
	one := &Staged{Timeline: &Timeline{Duration: 10, Subtitles: []Subtitle{{Start: 3, End: 7.94, Text: "A shop registers a parcel with us."}, {Start: 7.94, End: 10, Text: "We answer that it is registered,"}}}}
	two := &Staged{Timeline: &Timeline{Duration: 5, Subtitles: []Subtitle{{Start: 0.5, End: 3661.25, Text: "Later."}}}}
	want := "1\n00:00:03,000 --> 00:00:07,940\nA shop registers a parcel with us.\n\n" +
		"2\n00:00:07,940 --> 00:00:10,000\nWe answer that it is registered,\n\n" +
		"3\n00:00:10,500 --> 01:01:11,250\nLater.\n\n"
	if got := srtOf([]*Staged{one, two}); got != want {
		t.Errorf("the subtitles:\n%s\nwant:\n%s", got, want)
	}
}

// The project's command is given the subtitles and writes the film's sound,
// which is made as long as the film.
func TestTheSoundtrackCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test's command is a POSIX shell's")
	}
	dir := t.TempDir()
	// What a model would write: half a second of sound, 16-bit PCM.
	var wav bytes.Buffer
	samples := make([]int16, 11025)
	for i := range samples {
		samples[i] = int16(i % 200)
	}
	wav.WriteString("RIFF")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(36+len(samples)*2))
	wav.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(22050), uint32(44100), uint16(2), uint16(16)} {
		_ = binary.Write(&wav, binary.LittleEndian, v)
	}
	wav.WriteString("data")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(len(samples)*2))
	_ = binary.Write(&wav, binary.LittleEndian, samples)
	if err := os.WriteFile(filepath.Join(dir, "spoken.wav"), wav.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	film, subtitles := filepath.Join(dir, "out", "hold.mp4"), filepath.Join(dir, "out", "hold.srt")
	if err := os.MkdirAll(filepath.Dir(film), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subtitles, []byte("1\n00:00:00,000 --> 00:00:01,000\nHello.\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// It is run in the project's folder, and told where everything is.
	command := `grep -q Hello. "$AXX_DEMO_SUBTITLES" && echo "$AXX_DEMO_SECONDS $AXX_DEMO_FILM" > told && cp spoken.wav "$AXX_DEMO_SOUNDTRACK"`
	var said bytes.Buffer
	sound, err := soundtrackOf(context.Background(), dir, Soundtrack{Command: command}, film, subtitles, 2*time.Second, &said)
	if err != nil {
		t.Fatal(err, said.String())
	}
	if sound.Rate != 22050 || sound.Channels != 1 || len(sound.Samples) != 44100 || sound.Samples[150] != 150 || sound.Samples[44099] != 0 {
		t.Errorf("the sound: %d Hz, %d channels, %d samples", sound.Rate, sound.Channels, len(sound.Samples))
	}
	told, _ := os.ReadFile(filepath.Join(dir, "told"))
	if got := strings.TrimSpace(string(told)); got != "2.000 "+film {
		t.Errorf("the command was told %q", got)
	}
	// A command that fails, and one that writes nothing, say so.
	if _, err := soundtrackOf(context.Background(), dir, Soundtrack{Command: "exit 3"}, film, subtitles, time.Second, &said); err == nil || !strings.Contains(err.Error(), "packs.demo.soundtrack.command") {
		t.Errorf("a command that fails: %v", err)
	}
	if _, err := soundtrackOf(context.Background(), dir, Soundtrack{Command: "true"}, film, subtitles, time.Second, &said); err == nil || !strings.Contains(err.Error(), "wrote no sound") {
		t.Errorf("a command that writes nothing: %v", err)
	}
}
