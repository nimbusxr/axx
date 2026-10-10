package mobilecore

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/recording"
	"github.com/nimbusxr/axx/packs/internal/video"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// A scenario's apps can be kept as the desktop and web packs keep theirs
// (packs.mobile-core.traces and .videos): a trace, one HTML page with each
// app's screen after each step, and its controls where the scenario failed;
// a video, the scenario's phones as they ran, each touch drawn where it was,
// with its steps beside them. Package recording keeps them, as it keeps the
// desktop packs'. The screens come from Appium's streams of them (MJPEG):
// WebDriverAgent's on iOS, the UiAutomator2 server's on Android.

// A phone's place in a video: its screen, as big as fits, in the middle.
// Every phone has the same place, so a run's video has its iOS and Android
// scenarios alike.
const (
	slotWidth  = 592
	slotHeight = 1280
)

// tapLength is how long a tap shows; a finger that moved shows a little
// after it lifts.
const (
	tapLength  = 450 * time.Millisecond
	liftLength = 300 * time.Millisecond
)

var slotColor = color.RGBA{14, 16, 20, 255}

// phones are the mobile apps a scenario registered, in order: its video has
// a place for each, from its start.
type phones struct {
	mu    sync.Mutex
	names []string
}

var scenarioPhones = core.NewStateKey(Name+"/phones", func(*core.Scenario) *phones { return &phones{} }, nil)

func (p *phones) add(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, n := range p.names {
		if n == name {
			return
		}
	}
	p.names = append(p.names, name)
}

func (p *phones) list() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.names...)
}

// recordings are what a scenario keeps of its apps as it runs.
type recordings struct {
	mu       sync.Mutex
	feeds    map[string]*feed
	recorder *recording.Recorder
	traces   map[string][]recording.TraceStep
}

var scenarioRecordings = core.NewStateKey(Name+"/recordings", func(*core.Scenario) *recordings {
	return &recordings{feeds: map[string]*feed{}, traces: map[string][]recording.TraceStep{}}
}, nil)

// startRecording watches the app's screen from its start, when the settings
// keep a trace or a video.
func startRecording(sc *core.Scenario, app string, d Device) {
	cfg, err := settingsFor(sc.Suite())
	if err != nil || cfg.traces == "never" && cfg.videos == "never" {
		return
	}
	url := d.Stream()
	if url == "" {
		sc.Log("the %s app's device streams no screen: no trace or video shows it", app)
		return
	}
	f := watch(sc, d, url)
	rs := scenarioRecordings.Of(sc)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if old := rs.feeds[app]; old != nil {
		old.stop()
	}
	rs.feeds[app] = f
	if cfg.videos != "never" && rs.recorder == nil {
		rs.recorder = recording.Record(sc, func() *image.RGBA { return rs.look(sc) })
	}
}

// look is the scenario's phones as they are now: each in its place, in the
// order the scenario registered them; nil before any shows.
func (rs *recordings) look(sc *core.Scenario) *image.RGBA {
	names := scenarioPhones.Of(sc).list()
	rs.mu.Lock()
	feeds := make([]*feed, len(names))
	for i, n := range names {
		feeds[i] = rs.feeds[n]
	}
	rs.mu.Unlock()
	if len(names) == 0 {
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, slotWidth*len(names), slotHeight))
	xdraw.Draw(out, out.Bounds(), image.NewUniform(slotColor), image.Point{}, xdraw.Src)
	now, shown := time.Now(), false
	for i, f := range feeds {
		if f != nil && f.draw(out, image.Rect(i*slotWidth, 0, (i+1)*slotWidth, slotHeight), now) {
			shown = true
		}
	}
	if !shown {
		return nil
	}
	return out
}

// traceStepHook adds each running app's screen to its trace, after each
// step: the first frame its stream sends after the step ended.
func traceStepHook(sc *core.Scenario) error {
	cfg, err := settingsFor(sc.Suite())
	if err != nil || cfg.traces == "never" {
		return nil //nolint:nilerr // a setting the scenario fails on elsewhere
	}
	rs, ok := scenarioRecordings.Peek(sc)
	step := sc.Step()
	if !ok || step == nil {
		return nil
	}
	ended := time.Now()
	rs.mu.Lock()
	feeds := make(map[string]*feed, len(rs.feeds))
	for name, f := range rs.feeds {
		feeds[name] = f
	}
	rs.mu.Unlock()
	for name, f := range feeds {
		st := recording.TraceStep{Keyword: step.Keyword, Text: step.Text, Line: step.Line, At: time.Since(sc.Started()), MIME: "image/jpeg", Scale: 1}
		if b, w := f.after(ended, time.Second); b != nil {
			st.Image = b
			if w > traceWidth {
				st.Scale = float64(w) / traceWidth
			}
		}
		rs.mu.Lock()
		rs.traces[name] = append(rs.traces[name], st)
		rs.mu.Unlock()
	}
	return nil
}

// traceWidth is how wide a trace shows a phone's screen, in the page's
// points: about as wide as a phone is.
const traceWidth = 390

// finishRecordings keeps or drops what the scenario recorded of its apps,
// as it ends and before their devices go back.
func finishRecordings(sc *core.Scenario, r *running) {
	rs, ok := scenarioRecordings.Peek(sc)
	if !ok {
		return
	}
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return
	}
	failed := sc.Status() == "failed"
	rs.mu.Lock()
	rec := rs.recorder
	rs.recorder = nil
	rs.mu.Unlock()
	if rec != nil {
		recording.Keep(sc, "mobile", rec, cfg.videos, failed)
	}
	r.mu.Lock()
	devices := make(map[string]Device, len(r.devices))
	for name, d := range r.devices {
		devices[name] = d
	}
	r.mu.Unlock()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, name := range scenarioPhones.Of(sc).list() {
		steps := rs.traces[name]
		if len(steps) > 0 && recording.Keeps(cfg.traces, failed) {
			var controls string
			if d, ok := devices[name]; ok && failed {
				if s, err := d.Screen(context.WithoutCancel(sc.Context())); err == nil {
					controls = outline(s)
				}
			}
			recording.WriteTrace(sc, "mobile", name, steps, failed, controls)
		}
	}
	for _, f := range rs.feeds {
		f.stop()
	}
	rs.feeds, rs.traces = map[string]*feed{}, map[string][]recording.TraceStep{}
}

// outline is the screen's controls, as a trace shows them where a scenario
// failed: each shown one, inside its parent, with its name and state.
func outline(s *Screen) string {
	var b strings.Builder
	for _, n := range s.Nodes {
		if !n.Displayed {
			continue
		}
		depth := 0
		for p := n.Parent; p != nil; p = p.Parent {
			depth++
		}
		fmt.Fprintf(&b, "%s%s", strings.Repeat("  ", depth), roleName(n))
		if name := n.Name(); name != "" {
			fmt.Fprintf(&b, " name=%q", name)
		}
		if n.Role == RoleField && n.Text != "" && n.Text != n.Name() && !n.Password {
			fmt.Fprintf(&b, " value=%q", clean(n.Text))
		}
		if n.ID != "" {
			fmt.Fprintf(&b, " id=%q", n.ID)
		}
		if !n.Enabled {
			b.WriteString(` enabled="false"`)
		}
		if n.Checkable {
			fmt.Fprintf(&b, " checked=%q", strconv.FormatBool(n.Checked))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func roleName(n *Node) string {
	switch n.Role {
	case RoleButton:
		return "button"
	case RoleField:
		return "field"
	case RoleCheckbox:
		return "checkbox"
	case RoleSwitch:
		return "switch"
	case RoleTab:
		return "tab"
	case RoleListItem:
		return "list_item"
	case RoleImage:
		return "image"
	case RoleText:
		return "text"
	}
	if i := strings.LastIndexAny(n.Class, ".:"); i >= 0 {
		return n.Class[i+1:]
	}
	if n.Class != "" {
		return n.Class
	}
	return "element"
}

// feed is a device's screen as its stream sends it: the latest frame, and
// the touches the session made on it.
type feed struct {
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	frame   image.Image
	jpeg    []byte
	at      time.Time
	window  appium.Rect
	touches []appium.Touch
}

// watch starts reading a device's screen stream, and hears of its
// session's touches.
func watch(sc *core.Scenario, d Device, url string) *feed {
	ctx, cancel := context.WithCancel(context.WithoutCancel(sc.Context()))
	f := &feed{cancel: cancel, done: make(chan struct{})}
	s := d.Session()
	// A frame half the screen's size, ten a second: enough for a video, and
	// little work for the device.
	_ = s.Settings(ctx, map[string]any{"mjpegServerFramerate": recording.FrameRate, "mjpegScalingFactor": 50, "mjpegServerScreenshotQuality": 60})
	if w, err := s.Window(ctx); err == nil {
		f.window = w
	}
	s.Touched = f.touch
	go f.run(ctx, url)
	return f
}

func (f *feed) stop() {
	f.cancel()
	<-f.done
}

// run reads the stream until the feed stops, again when it breaks (the
// stream's server can restart with the app).
func (f *feed) run(ctx context.Context, url string) {
	defer close(f.done)
	client := &http.Client{}
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		if resp, err := client.Do(req); err == nil {
			_ = readMJPEG(resp.Body, f.add)
			_ = resp.Body.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// add keeps a frame the stream sent.
func (f *feed) add(b []byte) {
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil {
		return
	}
	f.mu.Lock()
	f.frame, f.jpeg, f.at = img, b, time.Now()
	f.mu.Unlock()
}

// touch keeps a touch the session made, for as long as it shows.
func (f *feed) touch(t appium.Touch) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touches = slices.DeleteFunc(f.touches, func(old appium.Touch) bool {
		return time.Since(old.At) >= old.Length+tapLength+liftLength
	})
	f.touches = append(f.touches, t)
}

// after is the first frame the stream sent after t, waiting for it as long
// as within, or else the last it sent; and how wide it is.
func (f *feed) after(t time.Time, within time.Duration) ([]byte, int) {
	deadline := time.Now().Add(within)
	for {
		f.mu.Lock()
		b, at, img := f.jpeg, f.at, f.frame
		f.mu.Unlock()
		if b != nil && at.After(t) || !time.Now().Before(deadline) {
			if img == nil {
				return nil, 0
			}
			return b, img.Bounds().Dx()
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// draw draws the device's screen in place, as big as fits, with the touches
// showing at now; it reports whether there was a screen to draw.
func (f *feed) draw(dst *image.RGBA, place image.Rectangle, now time.Time) bool {
	f.mu.Lock()
	img, window := f.frame, f.window
	touches := append([]appium.Touch(nil), f.touches...)
	f.mu.Unlock()
	if img == nil {
		return false
	}
	fb := img.Bounds()
	k := min(float64(place.Dx())/float64(fb.Dx()), float64(place.Dy())/float64(fb.Dy()))
	w, h := int(float64(fb.Dx())*k), int(float64(fb.Dy())*k)
	corner := image.Pt(place.Min.X+(place.Dx()-w)/2, place.Min.Y+(place.Dy()-h)/2)
	at := image.Rectangle{Min: corner, Max: corner.Add(image.Pt(w, h))}
	xdraw.ApproxBiLinear.Scale(dst, at, img, fb, xdraw.Src, nil)
	if window.Width <= 0 || window.Height <= 0 {
		return true
	}
	// A touch is in the window's coordinates; the frame shows the whole
	// window.
	sx, sy := float64(w)/window.Width, float64(h)/window.Height
	pt := func(p appium.Point) image.Point {
		return image.Pt(at.Min.X+int((p.X-window.X)*sx), at.Min.Y+int((p.Y-window.Y)*sy))
	}
	radius := float64(place.Dx()) * 0.034
	for _, t := range touches {
		since := now.Sub(t.At)
		switch {
		case since < 0:
		case t.From == t.To:
			if since < tapLength {
				video.DrawTouch(dst, pt(t.From), pt(t.To), radius, float64(since)/float64(tapLength))
			}
		case since < t.Length:
			p := float64(since) / float64(t.Length)
			mid := appium.Point{X: t.From.X + (t.To.X-t.From.X)*p, Y: t.From.Y + (t.To.Y-t.From.Y)*p}
			video.DrawTouch(dst, pt(t.From), pt(mid), radius, 0)
		case since < t.Length+liftLength:
			video.DrawTouch(dst, pt(t.From), pt(t.To), radius, float64(since-t.Length)/float64(liftLength))
		}
	}
	return true
}

// readMJPEG reads an MJPEG stream's frames, each a JPEG, until it ends: by
// each part's Content-Length, or else from the JPEG's start to its end.
func readMJPEG(r io.Reader, frame func([]byte)) error {
	br := bufio.NewReaderSize(r, 1<<16)
	tp := textproto.NewReader(br)
	for {
		// A part: its boundary line, its headers, a blank line, the JPEG.
		line, err := tp.ReadLine()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(line, "--") {
			continue
		}
		h, err := tp.ReadMIMEHeader()
		if err != nil && len(h) == 0 {
			return err
		}
		if n, err := strconv.Atoi(strings.TrimSpace(h.Get("Content-Length"))); err == nil && n > 0 {
			b := make([]byte, n)
			if _, err := io.ReadFull(br, b); err != nil {
				return err
			}
			frame(b)
			continue
		}
		b, err := readJPEG(br)
		if err != nil {
			return err
		}
		frame(b)
	}
}

// readJPEG reads a JPEG from its start (FF D8) to its end (FF D9).
func readJPEG(br *bufio.Reader) ([]byte, error) {
	var out []byte
	var prev byte
	started := false
	for {
		c, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		if !started {
			if prev == 0xff && c == 0xd8 {
				started, out = true, []byte{0xff, 0xd8}
			}
			prev = c
			continue
		}
		out = append(out, c)
		if prev == 0xff && c == 0xd9 {
			return out, nil
		}
		prev = c
	}
}
