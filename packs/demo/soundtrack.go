package demo

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/packs/internal/video"
)

// A film's sound is made after its picture, by whatever the project says:
// a model that speaks, a person who records. The picture's time is the
// film's, and the sound follows it. Beside each film axx writes its
// subtitles with their times (a .srt file), which is what a soundtrack is
// made from; a project's soundtrack command is given that file and writes one
// sound file as long as the film, which axx puts under the picture. Axx
// itself makes no sound and has no voice.

// Soundtrack is the project's way of making a film's sound.
type Soundtrack struct {
	// Command is a command of the project's, run through the shell in
	// axx.yaml's folder once the picture is made. It reads the film's
	// subtitles and writes the film's sound, a WAV file:
	//
	//	AXX_DEMO_SUBTITLES   the subtitles with their times, a .srt file
	//	AXX_DEMO_SOUNDTRACK  the file to write: a WAV file, as long as the film
	//	AXX_DEMO_SECONDS     how long the film is
	//	AXX_DEMO_FILM        the film, without sound
	Command string `json:"command,omitempty"`
}

// srtOf is a film's subtitles with their times, as an SRT file: of each of
// its takes, one after another.
func srtOf(parts []*Staged) string {
	var b strings.Builder
	n, offset := 0, 0.0
	for _, part := range parts {
		for _, s := range part.Timeline.Subtitles {
			n++
			fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", n, srtTime(offset+s.Start), srtTime(offset+s.End), s.Text)
		}
		offset += part.Timeline.Duration
	}
	return b.String()
}

// srtTime is a moment as SRT writes it: 00:00:03,500.
func srtTime(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second)).Round(time.Millisecond)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60, d.Milliseconds()%1000)
}

// soundtrackOf runs the project's soundtrack command for a film, and returns
// the sound it wrote, made as long as the film.
func soundtrackOf(ctx context.Context, projectDir string, st Soundtrack, film, subtitles string, length time.Duration, said io.Writer) (*video.Sound, error) {
	out := strings.TrimSuffix(film, filepath.Ext(film)) + ".wav"
	_ = os.Remove(out)
	argv := proc.ShellArgv(st.Command)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = projectDir
	cmd.Stdout, cmd.Stderr = said, said
	cmd.Env = append(os.Environ(),
		"AXX_DEMO_SUBTITLES="+subtitles,
		"AXX_DEMO_SOUNDTRACK="+out,
		fmt.Sprintf("AXX_DEMO_SECONDS=%.3f", length.Seconds()),
		"AXX_DEMO_FILM="+film,
	)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the soundtrack command (packs.demo.soundtrack.command) failed: %w", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("the soundtrack command wrote no sound: it is to write a WAV file to $AXX_DEMO_SOUNDTRACK (%s)", out)
	}
	sound, err := video.ReadWAV(data)
	if err != nil {
		return nil, fmt.Errorf("the soundtrack the command wrote (%s): %w", out, err)
	}
	// As long as the film: cut where it runs over, silent where it ends early.
	sound.Fit(length)
	return sound, nil
}
