package demo

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/proc"
	"github.com/nimbusxr/axx/packs/internal/video"
)

// A video's sound is made after its picture, by whatever the project says:
// a model that speaks, a person who records. The picture's time is the
// video's, and the sound follows it. Beside each video axx writes what it says
// with when (a .srt file: its subtitles, and its cards' texts), which is what
// a soundtrack is made from; a project's soundtrack command is given that file and writes one
// sound file as long as the video, which axx puts under the picture. Axx
// itself makes no sound and has no voice.

// Soundtrack is the project's way of making a video's sound.
type Soundtrack struct {
	// Command is a command of the project's, run through the shell in
	// axx.yaml's folder once the picture is made. It reads the video's
	// subtitles and writes the video's sound, a WAV file:
	//
	//	AXX_DEMO_SUBTITLES   the subtitles with their times, a .srt file
	//	AXX_DEMO_SOUNDTRACK  the file to write: a WAV file, as long as the video
	//	AXX_DEMO_SECONDS     how long the video is
	//	AXX_DEMO_VIDEO        the video, without sound
	Command string `json:"command,omitempty"`
}

// srtOf is what a video says, with when: its subtitles and the texts of its
// cards, as an SRT file, of each of its takes, one after another. A card's
// text is there so that a soundtrack says it too.
func srtOf(parts []*Staged) string {
	var said []Subtitle
	offset := 0.0
	for _, part := range parts {
		for _, seg := range part.Timeline.Segments {
			if seg.Kind == "card" && seg.Text != "" {
				said = append(said, Subtitle{Start: offset + seg.Start, End: offset + seg.End, Text: seg.Text})
			}
		}
		for _, s := range part.Timeline.Subtitles {
			said = append(said, Subtitle{Start: offset + s.Start, End: offset + s.End, Text: s.Text})
		}
		offset += part.Timeline.Duration
	}
	sort.SliceStable(said, func(i, j int) bool { return said[i].Start < said[j].Start })
	var b strings.Builder
	for i, s := range said {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(s.Start), srtTime(s.End), s.Text)
	}
	return b.String()
}

// srtTime is a moment as SRT writes it: 00:00:03,500.
func srtTime(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second)).Round(time.Millisecond)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60, d.Milliseconds()%1000)
}

// soundtrackOf runs the project's soundtrack command for a video, and returns
// the sound it wrote, made as long as the video.
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
		"AXX_DEMO_VIDEO="+film,
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
	// As long as the video: cut where it runs over, silent where it ends early.
	sound.Fit(length)
	return sound, nil
}
