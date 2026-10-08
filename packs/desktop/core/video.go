package desktopcore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/desktop/internal/video"
)

// A scenario's video is its desktop as it ran, the pointer drawn where it
// was: H.264 in an MP4 file, encoded with Cisco's OpenH264 (package video).
// A run keeps one more, run.mp4: its kept scenarios one after another, each
// after a card with its name and how it ended.

// frameRate is how often a video looks at the desktop, a second.
const frameRate = 10

// cardLength is how long a scenario's card shows in the run's video.
const cardLength = 2500 * time.Millisecond

// recorder records a scenario's desktop as it runs: a frame only when the
// desktop changed, so a still screen costs nothing.
type recorder struct {
	desk  Desktop
	start time.Time
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
	enc   *video.Encoder
	last  [32]byte
	err   error
}

// startRecording records the scenario's desktop from its first app's start,
// when the settings keep its video.
func startRecording(sc *core.Scenario, s *scenario) {
	cfg, err := settingsFor(sc.Suite())
	if err != nil || cfg.videos == "never" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recorder != nil || s.desktop == nil {
		return
	}
	r := &recorder{desk: s.desktop, start: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	s.recorder = r
	go r.run()
}

func (r *recorder) run() {
	defer close(r.done)
	t := time.NewTicker(time.Second / frameRate)
	defer t.Stop()
	for {
		r.look()
		select {
		case <-r.stop:
			r.look()
			return
		case <-t.C:
		}
	}
}

// look adds the desktop to the video when it changed.
func (r *recorder) look() {
	if r.err != nil {
		return
	}
	s, err := r.desk.Screen()
	if err != nil || s.Image == nil {
		return // no screen yet, or not this moment: the last frame shows on
	}
	img := downscale(s.Image, s.Scale)
	if s.HasPointer {
		f := math.Max(1, math.Round(s.Scale))
		video.DrawPointer(img, image.Pt(int(float64(s.Pointer.X)/f), int(float64(s.Pointer.Y)/f)), 1)
	}
	sum := sha256.Sum256(img.Pix)
	if r.enc != nil && sum == r.last {
		return
	}
	r.last = sum
	if r.enc == nil {
		b := img.Bounds()
		if r.enc, r.err = video.NewEncoder(b.Dx(), b.Dy(), frameRate); r.err != nil {
			return
		}
	}
	r.err = r.enc.Encode(img, time.Since(r.start))
}

// finish stops recording: the clip, when there is one.
func (r *recorder) finish() (video.Clip, bool) {
	r.once.Do(func() { close(r.stop) })
	<-r.done
	if r.enc == nil {
		return video.Clip{}, false
	}
	c := r.enc.Finish(time.Since(r.start))
	return c, len(c.Samples) > 0
}

// finishVideo keeps the scenario's video when the settings say, and adds it
// to the run's.
func finishVideo(sc *core.Scenario, s *scenario, cfg *settings, failed bool) {
	r := s.recorder
	if r == nil {
		return
	}
	s.recorder = nil
	clip, ok := r.finish()
	if !ok {
		if r.err != nil {
			sc.Log("no video of the scenario: %v", r.err)
		}
		return
	}
	if !keeps(cfg.videos, failed) {
		return
	}
	var b bytes.Buffer
	if err := video.WriteMP4(&b, []video.Clip{clip}); err != nil {
		sc.Log("no video of the scenario: %v", err)
		return
	}
	path := recordingPath(sc, "videos", "", ".mp4")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		sc.Log("no video of the scenario: %v", err)
		return
	}
	sc.Log("the scenario's video: %s (%s)", relative(sc, path), video.Credit)
	sc.Attach("video/mp4", b.Bytes(), "the scenario's video")
	announce(sc, "video", path)
	runVideoOf(sc.Suite()).add(sc, clip, failed)
}

// runVideo is a run's video: its kept scenarios' clips, each after its card.
type runVideo struct {
	mu             sync.Mutex
	clips          []video.Clip
	passed, failed int
	started        time.Time
	width, height  int
	where          string
}

var runVideos sync.Map // *core.Suite → *runVideo

// runVideoOf is the run's video, written as the run ends.
func runVideoOf(s *core.Suite) *runVideo {
	v, loaded := runVideos.LoadOrStore(s, &runVideo{started: time.Now()})
	rv := v.(*runVideo)
	if !loaded {
		rv.where = filepath.Join(s.ProjectDir(), ".axx", "desktop", "videos", "run.mp4")
		s.OnClose(func(context.Context) error {
			runVideos.Delete(s)
			return rv.write(s)
		})
	}
	return rv
}

// add adds a scenario's clip, after its card.
func (v *runVideo) add(sc *core.Scenario, clip video.Clip, failed bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.width == 0 {
		v.width, v.height = clip.Width, clip.Height
	}
	if clip.Width != v.width || clip.Height != v.height {
		return // a desktop of another size: its own video only
	}
	word, color := "passed", video.Passed
	if failed {
		word, color = "failed", video.Failed
		v.failed++
	} else {
		v.passed++
	}
	lines := []string{fmt.Sprintf("%s:%d", sc.URI, sc.Line)}
	card, err := cardClip(video.Card{Heading: sc.Name, Lines: lines, Word: word, Color: color}, v.width, v.height)
	if err != nil {
		return
	}
	v.clips = append(v.clips, card, clip)
}

// write writes the run's video, after a card that says what ran.
func (v *runVideo) write(s *core.Suite) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.clips) == 0 {
		return nil
	}
	n := v.passed + v.failed
	lines := []string{fmt.Sprintf("%d %s · %d passed · %d failed", n, plural(n, "scenario", "scenarios"), v.passed, v.failed), v.started.Format("2 January 2006, 15:04")}
	opening, err := cardClip(video.Card{Heading: filepath.Base(s.ProjectDir()), Lines: lines}, v.width, v.height)
	if err != nil {
		return nil //nolint:nilerr // the run's video is a convenience: a run does not fail for it
	}
	var b bytes.Buffer
	if err := video.WriteMP4(&b, append([]video.Clip{opening}, v.clips...)); err != nil {
		return nil //nolint:nilerr // as above
	}
	_ = os.MkdirAll(filepath.Dir(v.where), 0o755)
	if err := os.WriteFile(v.where, b.Bytes(), 0o644); err == nil {
		s.Announce("video", "path", v.where)
	}
	return nil
}

// cardClip is a card, encoded as a clip that shows it for cardLength.
func cardClip(c video.Card, width, height int) (video.Clip, error) {
	e, err := video.NewEncoder(width, height, frameRate)
	if err != nil {
		return video.Clip{}, err
	}
	if err := e.Encode(c.Image(width, height), 0); err != nil {
		e.Close()
		return video.Clip{}, err
	}
	return e.Finish(cardLength), nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
