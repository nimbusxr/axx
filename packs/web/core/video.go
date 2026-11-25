package webcore

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mxschmitt/playwright-go"
	xdraw "golang.org/x/image/draw"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/recording"
	"github.com/nimbusxr/axx/packs/internal/video"
)

// A scenario's video is the tab its steps act on, as it ran, the pointer
// drawn where a step acted, with the scenario's steps beside it (package
// recording): one MP4 file a scenario, and the run's, run.mp4.
//
// The tabs' pictures are Playwright's screencast of each: frames of the
// page as the browser paints it, in every engine, which a browser the axx
// host started sends the run as any other. Nothing is added to the page:
// the pointer is drawn on the video's frames, where the step's element is.

// The video's picture of the tab: every frame has this size, whatever the
// tabs' own, each drawn as large as fits.
const (
	videoWidth  = 1280
	videoHeight = 720
)

// videoRate is how often a tab sends a frame, at most: the video's own
// rate. A tab that sent faster would fill the trace it shares its
// screencast with.
const videoRate = time.Second / recording.FrameRate

// pressLength is how long the mark of a press shows where the pointer is.
const pressLength = 600 * time.Millisecond

var videoBackdrop = color.RGBA{14, 16, 20, 255}

// webVideo is a scenario's video: its tabs' frames, and which app's tab
// shows.
type webVideo struct {
	recorder *recording.Recorder
	// shown is the session whose current tab the video shows: the one the
	// scenario acted on last.
	shown atomic.Pointer[session]

	mu    sync.Mutex
	feeds map[playwright.Page]*tabFeed
	// drawn is the picture last made of a tab's frame, and of which.
	drawn    *image.RGBA
	drawnOf  *tabFeed
	drawnSeq int
	area     image.Rectangle // where the frame is in the picture
}

// tabFeed is a tab's frames as its screencast sends them: the latest, and
// where the pointer was put on it.
type tabFeed struct {
	mu      sync.Mutex
	jpeg    []byte
	seq     int
	arrived time.Time
	// viewport is the tab's size in the page's own pixels, as of the frame.
	viewport image.Point
	// pointer is where a step last acted, in the page's pixels; pressed,
	// when it pressed there.
	pointer *image.Point
	pressed time.Time
}

// newVideo starts the scenario's video, of the session's tabs first.
// The video itself is recorded when the settings keep one (record); what
// watches the scenario's screens (a demo) is shown the same picture.
func newVideo(sc *core.Scenario, record bool) *webVideo {
	v := &webVideo{feeds: map[playwright.Page]*tabFeed{}}
	if record {
		v.recorder = recording.Record(sc, v.look, video.StreamQuality)
	}
	recording.Screen(sc, "browser", v.look)
	return v
}

// watch has the tab send its frames. A tab that cannot (a browser that
// closed) shows nothing: the video goes on with the others.
func (v *webVideo) watch(p playwright.Page) {
	v.mu.Lock()
	if v.feeds[p] != nil {
		v.mu.Unlock()
		return // its frames come already
	}
	f := &tabFeed{}
	v.feeds[p] = f
	v.mu.Unlock()
	cast, err := p.Screencast()
	if err != nil {
		return
	}
	_ = cast.Start(playwright.ScreencastStartOptions{
		Size:    &playwright.Size{Width: videoWidth, Height: videoHeight},
		OnFrame: f.frame,
	})
}

// frame takes a frame of the tab. The browser sends the next once this
// one is acknowledged, which is when frame returns: no sooner than the
// video's rate.
func (f *tabFeed) frame(fr playwright.OnFrame) {
	now := time.Now()
	f.mu.Lock()
	since := now.Sub(f.arrived)
	f.jpeg, f.seq, f.arrived = fr.Data, f.seq+1, now
	if fr.ViewportWidth > 0 && fr.ViewportHeight > 0 {
		f.viewport = image.Pt(fr.ViewportWidth, fr.ViewportHeight)
	}
	f.mu.Unlock()
	if since < videoRate {
		time.Sleep(videoRate - since)
	}
}

// pointAt puts the pointer on the tab, at a point of the page, pressing
// there or not.
func (v *webVideo) pointAt(p playwright.Page, at image.Point, press bool) {
	v.mu.Lock()
	f := v.feeds[p]
	v.mu.Unlock()
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pointer = &at
	if press {
		f.pressed = time.Now()
	}
}

// look is the tab the scenario acts on, as it is now: nil before it sent a
// frame (the video's last picture shows on).
func (v *webVideo) look() *image.RGBA {
	s := v.shown.Load()
	if s == nil {
		return nil
	}
	p, err := s.page()
	if err != nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	f := v.feeds[p]
	if f == nil {
		return nil
	}
	f.mu.Lock()
	data, seq, viewport := f.jpeg, f.seq, f.viewport
	pointer, pressed := f.pointer, f.pressed
	f.mu.Unlock()
	if data == nil {
		return nil
	}
	if v.drawn == nil || v.drawnOf != f || v.drawnSeq != seq {
		frame, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		v.drawn, v.area = fitted(frame)
		v.drawnOf, v.drawnSeq = f, seq
	}
	if pointer == nil || viewport.X == 0 || viewport.Y == 0 {
		return v.drawn
	}
	// The pointer, on a copy: the picture of the frame is kept for the next
	// look at the same frame.
	out := image.NewRGBA(v.drawn.Bounds())
	copy(out.Pix, v.drawn.Pix)
	place := func(pt image.Point) image.Point {
		return image.Pt(
			v.area.Min.X+int(math.Round(float64(pt.X)*float64(v.area.Dx())/float64(viewport.X))),
			v.area.Min.Y+int(math.Round(float64(pt.Y)*float64(v.area.Dy())/float64(viewport.Y))))
	}
	at := place(*pointer)
	if since := time.Since(pressed); since < pressLength {
		video.DrawTouch(out, at, at, 14, float64(since)/float64(pressLength))
	}
	video.DrawPointer(out, at, 1.4)
	return out
}

// fitted is a frame as the video shows it: as large as fits its picture,
// in the middle of it, and where it is there.
func fitted(frame image.Image) (*image.RGBA, image.Rectangle) {
	out := image.NewRGBA(image.Rect(0, 0, videoWidth, videoHeight))
	xdraw.Draw(out, out.Bounds(), image.NewUniform(videoBackdrop), image.Point{}, xdraw.Src)
	b := frame.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return out, out.Bounds()
	}
	scale := math.Min(float64(videoWidth)/float64(b.Dx()), float64(videoHeight)/float64(b.Dy()))
	w, h := int(math.Round(float64(b.Dx())*scale)), int(math.Round(float64(b.Dy())*scale))
	area := image.Rect((videoWidth-w)/2, (videoHeight-h)/2, (videoWidth-w)/2+w, (videoHeight-h)/2+h)
	if w == b.Dx() && h == b.Dy() {
		xdraw.Draw(out, area, frame, b.Min, xdraw.Src)
	} else {
		xdraw.CatmullRom.Scale(out, area, frame, b, xdraw.Src, nil)
	}
	return out, area
}

// keep ends the video, and keeps it when the settings say.
func (v *webVideo) keep(sc *core.Scenario, policy string, failed bool) {
	if v.recorder != nil {
		recording.Keep(sc, "web", v.recorder, policy, failed)
	}
}

// pointAt puts the video's pointer on the element a step acts on, in the
// middle of it, when the scenario keeps a video. An element that cannot be
// measured (gone, or in a tab that closed) moves no pointer: the step goes
// on to act, and says what it could not.
func (s *session) pointAt(l playwright.Locator, verb string) {
	if s.video == nil {
		return
	}
	p, err := s.page()
	if err != nil {
		return
	}
	wait := playwright.Float(1000)
	if err := l.ScrollIntoViewIfNeeded(playwright.LocatorScrollIntoViewIfNeededOptions{Timeout: wait}); err != nil {
		return
	}
	box, err := l.BoundingBox(playwright.LocatorBoundingBoxOptions{Timeout: wait})
	if err != nil || box == nil {
		return
	}
	at := image.Pt(int(math.Round(box.X+box.Width/2)), int(math.Round(box.Y+box.Height/2)))
	s.video.pointAt(p, at, !strings.HasPrefix(verb, "move the pointer"))
}
