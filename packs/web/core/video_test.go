package webcore

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// fakeTab stands in for a browser's tab: the video knows tabs apart, and asks
// them nothing.
type fakeTab struct{ playwright.Page }

// frameOf is a frame a tab sends: a JPEG of one colour, of a viewport.
func frameOf(t *testing.T, w, h int, c color.Color, viewport image.Point) playwright.OnFrame {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return playwright.OnFrame{Data: b.Bytes(), ViewportWidth: viewport.X, ViewportHeight: viewport.Y}
}

// videoOf is a video of sessions, each with one tab that sends frames, with
// no recorder: the test looks as the recorder would.
func videoOf(apps ...string) (*webVideo, map[string]*session, map[string]*tabFeed) {
	v := &webVideo{feeds: map[playwright.Page]*tabFeed{}}
	sessions, feeds := map[string]*session{}, map[string]*tabFeed{}
	for _, name := range apps {
		p := &fakeTab{}
		f := &tabFeed{}
		v.feeds[p] = f
		sessions[name] = &session{app: &App{Name: name}, tabs: []playwright.Page{p}, video: v}
		feeds[name] = f
	}
	return v, sessions, feeds
}

func near(got color.Color, want color.RGBA, within int) bool {
	r, g, b, _ := got.RGBA()
	d := func(a uint32, b uint8) int {
		n := int(a>>8) - int(b)
		if n < 0 {
			return -n
		}
		return n
	}
	return d(r, want.R) <= within && d(g, want.G) <= within && d(b, want.B) <= within
}

var (
	green = color.RGBA{30, 160, 90, 255}
	rust  = color.RGBA{190, 80, 40, 255}
)

func TestAVideoShowsTheTabTheScenarioActsOn(t *testing.T) {
	v, sessions, feeds := videoOf("portal", "depot")
	if v.look() != nil {
		t.Fatal("a picture before any app is in use")
	}
	v.shown.Store(sessions["portal"])
	if v.look() != nil {
		t.Fatal("a picture before the tab sent a frame")
	}
	feeds["portal"].frame(frameOf(t, 1280, 720, green, image.Pt(1280, 720)))
	feeds["depot"].frame(frameOf(t, 1280, 720, rust, image.Pt(1280, 720)))

	got := v.look()
	if got == nil {
		t.Fatal("no picture of the tab that sent a frame")
	}
	if b := got.Bounds(); b.Dx() != videoWidth || b.Dy() != videoHeight {
		t.Fatalf("the picture is %dx%d, want %dx%d", b.Dx(), b.Dy(), videoWidth, videoHeight)
	}
	if !near(got.At(640, 360), green, 6) {
		t.Errorf("the portal's tab shows %v, want its green", got.At(640, 360))
	}
	if again := v.look(); again != got {
		t.Error("the same frame was drawn again")
	}

	// The scenario acts on the other app: its tab shows.
	v.shown.Store(sessions["depot"])
	if got := v.look(); got == nil || !near(got.At(640, 360), rust, 6) {
		t.Errorf("the depot's tab does not show once the scenario acts on it")
	}

	// A tab the app opened is its current one: nothing of it yet, so the
	// last picture stays.
	sessions["depot"].tabs = append(sessions["depot"].tabs, &fakeTab{})
	if v.look() != nil {
		t.Error("a picture of a tab that sent no frame")
	}
}

func TestAVideoFitsEveryTabInOnePicture(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		area image.Rectangle
	}{
		{"a frame of the picture's size", 1280, 720, image.Rect(0, 0, 1280, 720)},
		{"a small frame of its shape, as a tab a page opened sends", 800, 450, image.Rect(0, 0, 1280, 720)},
		{"a phone's", 370, 800, image.Rect(473, 0, 806, 720)},
		{"a wide one", 1280, 400, image.Rect(0, 160, 1280, 560)},
	} {
		t.Run(c.name, func(t *testing.T) {
			frame := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
			draw.Draw(frame, frame.Bounds(), image.NewUniform(green), image.Point{}, draw.Src)
			got, area := fitted(frame)
			if area != c.area {
				t.Fatalf("the frame is at %v, want %v", area, c.area)
			}
			if b := got.Bounds(); b.Dx() != videoWidth || b.Dy() != videoHeight {
				t.Fatalf("the picture is %dx%d", b.Dx(), b.Dy())
			}
			mid := image.Pt((area.Min.X+area.Max.X)/2, (area.Min.Y+area.Max.Y)/2)
			if !near(got.At(mid.X, mid.Y), green, 2) {
				t.Errorf("the frame's middle shows %v", got.At(mid.X, mid.Y))
			}
			for _, outside := range []image.Point{{area.Min.X - 3, mid.Y}, {area.Max.X + 2, mid.Y}, {mid.X, area.Min.Y - 3}, {mid.X, area.Max.Y + 2}} {
				if outside.In(got.Bounds()) && !near(got.At(outside.X, outside.Y), videoBackdrop, 0) {
					t.Errorf("beside the frame, at %v, shows %v, want the backdrop", outside, got.At(outside.X, outside.Y))
				}
			}
		})
	}
}

func TestAVideoDrawsThePointerWhereAStepActed(t *testing.T) {
	v, sessions, feeds := videoOf("portal")
	p := sessions["portal"].tabs[0]
	v.shown.Store(sessions["portal"])
	// A frame half the viewport's size: the page's pixels are not the
	// picture's.
	feeds["portal"].frame(frameOf(t, 640, 360, green, image.Pt(1280, 720)))
	plain := v.look()

	v.pointAt(p, image.Pt(320, 180), true)
	pressed := v.look()
	if pressed == plain {
		t.Fatal("the pointer was drawn on the frame's own picture")
	}
	// The pointer's tip is at the point, in the picture: the page's (320,
	// 180) of 1280 by 720, here the same. Its body is light, just inside.
	if got := pressed.At(322, 186); !near(got, color.RGBA{250, 250, 250, 255}, 40) {
		t.Errorf("no pointer where the step acted: %v", got)
	}
	// The press shows around it, and not far from it.
	if near(pressed.At(308, 180), green, 6) {
		t.Error("no mark of the press beside the pointer")
	}
	if !near(pressed.At(700, 400), green, 6) {
		t.Errorf("the page away from the pointer changed: %v", pressed.At(700, 400))
	}
	if !near(plain.At(322, 186), green, 6) {
		t.Error("the frame's own picture has the pointer now")
	}

	// The press fades; the pointer stays where it is.
	feeds["portal"].mu.Lock()
	feeds["portal"].pressed = time.Now().Add(-2 * pressLength)
	feeds["portal"].mu.Unlock()
	rested := v.look()
	if !near(rested.At(308, 180), green, 6) {
		t.Errorf("the press still shows after it faded: %v", rested.At(308, 180))
	}
	if got := rested.At(322, 186); !near(got, color.RGBA{250, 250, 250, 255}, 40) {
		t.Errorf("the pointer left with the press: %v", got)
	}

	// A step that only moves the pointer presses nothing.
	v.pointAt(p, image.Pt(900, 500), false)
	feeds["portal"].mu.Lock()
	pressedAt := feeds["portal"].pressed
	feeds["portal"].mu.Unlock()
	if time.Since(pressedAt) < pressLength {
		t.Error("moving the pointer pressed")
	}
	if got := v.look().At(902, 506); !near(got, color.RGBA{250, 250, 250, 255}, 40) {
		t.Errorf("the pointer did not move: %v", got)
	}

	// A tab the video does not know moves no pointer.
	v.pointAt(&fakeTab{}, image.Pt(1, 1), true)
}

// TestATabSendsNoFasterThanTheVideoShows: the browser sends a tab's next
// frame when this one is taken, which a frame after another waits for.
func TestATabSendsNoFasterThanTheVideoShows(t *testing.T) {
	f := &tabFeed{}
	fr := frameOf(t, 16, 16, green, image.Pt(16, 16))
	start := time.Now()
	f.frame(fr)
	if first := time.Since(start); first > videoRate/2 {
		t.Errorf("the first frame waited %s", first)
	}
	f.frame(fr)
	if both := time.Since(start); both < videoRate*8/10 {
		t.Errorf("two frames were taken in %s, want about %s between them", both, videoRate)
	}
	if f.seq != 2 {
		t.Errorf("%d frames kept count of, want 2", f.seq)
	}
}
