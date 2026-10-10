package recording

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/video"
)

// A scenario's video is its screen as it ran, and beside it the scenario's
// steps, each as it stands (to run, running, passed, failed, skipped): H.264
// in an MP4 file, encoded with Cisco's OpenH264 (package video). A run keeps
// one more, run.mp4: its kept scenarios one after another, each after a card
// with its name and how it ended.

// FrameRate is how often a video looks at the screen, a second.
const FrameRate = 10

// cardLength is how long a scenario's card shows in the run's video.
const cardLength = 2500 * time.Millisecond

// holdLength is how long a scenario's last frame shows, its steps as they
// ended: a failure's message can be read.
const holdLength = 2 * time.Second

// openingLength is how long a scenario's video opens on its first frame,
// its steps from the first: the whole scenario at a glance, those that ran
// before it had a screen (the apps it declares) among them.
const openingLength = 1500 * time.Millisecond

// Look is the screen as it is now, as the video shows it, or nil when there
// is none yet (the last frame shows on). Every frame of a video has the same
// size: the first's.
type Look func() *image.RGBA

// Recorder records a scenario's screen as it runs: a frame only when the
// screen changed, so a still screen costs nothing.
type Recorder struct {
	look    Look
	sc      *core.Scenario
	quality video.Quality
	start   time.Time
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	enc     *video.Encoder
	last    [32]byte
	err     error
	// panel is the steps panel last drawn, and what it showed.
	panel     *image.RGBA
	panelWhat string
	// marks are when each step was first seen to run, in the order they
	// ran: the video's chapters.
	marks []mark
	seen  map[int]bool
}

type mark struct {
	step int
	at   time.Duration
}

// Record starts recording the scenario's screen, as look sees it, at the
// quality that suits where it comes from (a desktop's screen, a phone's
// stream).
func Record(sc *core.Scenario, look Look, quality video.Quality) *Recorder {
	r := &Recorder{look: look, sc: sc, quality: quality, start: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	go r.run()
	return r
}

func (r *Recorder) run() {
	defer close(r.done)
	t := time.NewTicker(time.Second / FrameRate)
	defer t.Stop()
	for {
		r.add()
		select {
		case <-r.stop:
			r.add()
			return
		case <-t.C:
		}
	}
}

// add adds the screen to the video when it changed.
func (r *Recorder) add() {
	if r.err != nil {
		return
	}
	screen := r.look()
	if screen == nil {
		return // no screen yet, or not this moment: the last frame shows on
	}
	if r.enc != nil {
		if w, h := r.enc.Size(); w-video.PanelWidth(h) != screen.Bounds().Dx() || h != screen.Bounds().Dy() {
			return // not the size the video has
		}
	}
	steps := r.sc.Progress()
	r.mark(steps)
	img := r.withSteps(screen, steps, false)
	sum := sha256.Sum256(img.Pix)
	if r.enc != nil && sum == r.last {
		return
	}
	r.last = sum
	if r.enc == nil {
		b := img.Bounds()
		if r.enc, r.err = video.NewEncoderOf(b.Dx(), b.Dy(), FrameRate, r.quality); r.err != nil {
			return
		}
		if r.err = r.enc.Encode(r.withSteps(screen, steps, true), 0); r.err != nil {
			return
		}
	}
	r.err = r.enc.Encode(img, openingLength+time.Since(r.start))
}

// Started is when the recording started: what the video shows at a moment
// is at openingLength plus the time since.
func (r *Recorder) Started() time.Time { return r.start }

// finish stops recording: the clip, when there is one.
func (r *Recorder) finish() (video.Clip, bool) {
	r.once.Do(func() { close(r.stop) })
	<-r.done
	if r.enc == nil {
		return video.Clip{}, false
	}
	c := r.enc.Finish(openingLength + time.Since(r.start) + holdLength)
	return c, len(c.Samples) > 0
}

// mark notes when each step that ran was first seen to run.
func (r *Recorder) mark(steps []core.StepProgress) {
	if r.seen == nil {
		r.seen = map[int]bool{}
	}
	at := time.Since(r.start)
	for i, st := range steps {
		if st.Status != "" && st.Status != "skipped" && !r.seen[i] {
			r.seen[i] = true
			r.marks = append(r.marks, mark{i, at})
		}
	}
}

// chapters are the video's chapters: a step each, from when it ran, but
// one for the steps that ran in the same moment (those before the video's
// start among them).
func (r *Recorder) chapters() []video.Chapter {
	steps := r.sc.Progress()
	var out []video.Chapter
	for _, m := range r.marks {
		at := openingLength + m.at
		if m.step >= len(steps) || len(out) > 0 && at-out[len(out)-1].At < time.Second/FrameRate {
			continue
		}
		st := steps[m.step]
		out = append(out, video.Chapter{Title: st.Keyword + " " + sysRef.ReplaceAllStringFunc(st.Text, r.sc.Suite().Interpolate), At: at})
	}
	if len(out) > 0 {
		out[0].At = 0 // the opening's too
	}
	return out
}

// withSteps is the screen with the scenario's steps beside it: from the
// first (top) or the one in focus.
func (r *Recorder) withSteps(screen *image.RGBA, steps []core.StepProgress, top bool) *image.RGBA {
	b := screen.Bounds()
	w := video.PanelWidth(b.Dy())
	p := panelOf(r.sc, steps)
	p.Top = top
	if what := fmt.Sprintf("%+v", p); r.panel == nil || what != r.panelWhat || r.panel.Bounds().Dy() != b.Dy() {
		r.panel = image.NewRGBA(image.Rect(0, 0, w, b.Dy()))
		p.Draw(r.panel, r.panel.Bounds())
		r.panelWhat = what
	}
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()+w, b.Dy()))
	draw.Draw(out, image.Rect(0, 0, b.Dx(), b.Dy()), screen, b.Min, draw.Src)
	draw.Draw(out, image.Rect(b.Dx(), 0, b.Dx()+w, b.Dy()), r.panel, image.Point{}, draw.Src)
	return out
}

// sysRef is a reference to a property of the run's profile, which the
// panel shows filled in; others (${env:...}, which may hold a secret) show
// as written.
var sysRef = regexp.MustCompile(`\$\{sys:[^}]*\}`)

// panelOf is the scenario's steps panel, as they stand.
func panelOf(sc *core.Scenario, steps []core.StepProgress) video.Panel {
	p := video.Panel{Title: sc.Name, Where: fmt.Sprintf("%s:%d", sc.URI, sc.Line)}
	for _, st := range steps {
		p.Steps = append(p.Steps, video.Step{
			Keyword: st.Keyword, Text: sysRef.ReplaceAllStringFunc(st.Text, sc.Suite().Interpolate),
			Background: st.Background, Argument: st.Argument,
			State: stateOf(st.Status), Error: st.Error, Logs: st.Logs,
		})
	}
	return p
}

func stateOf(status string) video.State {
	switch status {
	case "":
		return video.StepWaiting
	case "running":
		return video.StepRunning
	case "passed":
		return video.StepPassed
	case "skipped":
		return video.StepSkipped
	}
	return video.StepFailed
}

// Keep stops the recorder and keeps the scenario's video in
// .axx/<area>/videos when the policy says, and adds it to the run's.
func Keep(sc *core.Scenario, area string, r *Recorder, policy string, failed bool) {
	clip, ok := r.finish()
	if !ok {
		if r.err != nil {
			sc.Log("no video of the scenario: %v", r.err)
		}
		return
	}
	if !Keeps(policy, failed) {
		return
	}
	var b bytes.Buffer
	if err := video.WriteMP4(&b, []video.Clip{clip}, r.chapters()...); err != nil {
		sc.Log("no video of the scenario: %v", err)
		return
	}
	path := Path(sc, area, "videos", "", ".mp4")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		sc.Log("no video of the scenario: %v", err)
		return
	}
	sc.Log("the scenario's video: %s (%s)", Relative(sc, path), video.Credit)
	if failed {
		// The report shows a failed scenario's video; the others are in the
		// folder, as a run keeps them all.
		sc.Attach("video/mp4", b.Bytes(), "the scenario's video")
	}
	Announce(sc, "video", path)
	runVideoOf(sc.Suite(), area).add(sc, clip, failed)
}

// runVideo is a run's video: its kept scenarios' clips, each after its card.
type runVideo struct {
	mu    sync.Mutex
	clips []video.Clip
	// titles are the clips' chapters: a scenario's on its card.
	titles         []string
	passed, failed int
	started        time.Time
	width, height  int
	where          string
}

type runKey struct {
	suite *core.Suite
	area  string
}

var runVideos sync.Map // runKey → *runVideo

// runVideoOf is the run's video of an area (desktop, mobile), written as the
// run ends.
func runVideoOf(s *core.Suite, area string) *runVideo {
	key := runKey{s, area}
	v, loaded := runVideos.LoadOrStore(key, &runVideo{started: time.Now()})
	rv := v.(*runVideo)
	if !loaded {
		rv.where = filepath.Join(s.ProjectDir(), ".axx", area, "videos", "run.mp4")
		s.OnClose(func(context.Context) error {
			runVideos.Delete(key)
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
		return // a screen of another size: its own video only
	}
	word, color, mark := "passed", video.Passed, "✓"
	if failed {
		word, color, mark = "failed", video.Failed, "✗"
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
	v.titles = append(v.titles, mark+" "+sc.Name, "")
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
	// A chapter for each scenario, from its card.
	clips := append([]video.Clip{opening}, v.clips...)
	titles := append([]string{filepath.Base(s.ProjectDir())}, v.titles...)
	var chapters []video.Chapter
	var at time.Duration
	for i, c := range clips {
		if titles[i] != "" {
			chapters = append(chapters, video.Chapter{Title: titles[i], At: at})
		}
		at += c.Length()
	}
	var b bytes.Buffer
	if err := video.WriteMP4(&b, clips, chapters...); err != nil {
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
	e, err := video.NewEncoder(width, height, FrameRate)
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
