package desktopcore

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"html"
	"image"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
)

// A scenario's desktop apps can be kept as the web pack keeps its pages
// (packs.desktop-core.traces and .videos): a trace, one HTML page with the
// app's window after each step, and its controls where the scenario failed;
// a video, the app's window as it changed, an animated PNG the report plays.
// Both show the app's window only, never the rest of the screen.

// keeps is whether a policy (failed, always, never) keeps a scenario's file.
func keeps(policy string, failed bool) bool {
	return policy == "always" || policy == "failed" && failed
}

// recording is what a scenario keeps of one app as it runs.
type recording struct {
	app   string
	trace []traceStep
	video *video
}

type traceStep struct {
	keyword, text string
	line          int
	at            time.Duration
	png           []byte
	scale         float64 // the display's, which the page shows the window at
}

// snapshotter is a Process that captures its window as a PNG as it shows,
// cursor and all, cheaper than Window: for a trace, which keeps it as it
// comes (macOS's screencapture writes one; decoding it, hiding the cursor
// and encoding it again took as long as the capture).
type snapshotter interface {
	Snapshot() (png []byte, scale float64, err error)
}

// recordingOf is the scenario's recording of the app, made on first use.
func (s *scenario) recordingOf(app string) *recording {
	if s.recordings == nil {
		s.recordings = map[string]*recording{}
	}
	r, ok := s.recordings[app]
	if !ok {
		r = &recording{app: app}
		s.recordings[app] = r
	}
	return r
}

// traceStepHook adds the window of each app the scenario runs to its trace,
// after each step.
func traceStepHook(sc *core.Scenario) error {
	cfg, err := settingsFor(sc.Suite())
	if err != nil || cfg.traces == "never" {
		return nil //nolint:nilerr // a setting the scenario fails on elsewhere
	}
	s, ok := scenarios.Peek(sc)
	if !ok {
		return nil
	}
	step := sc.Step()
	if step == nil {
		return nil
	}
	s.mu.Lock()
	running := make(map[string]Process, len(s.running))
	for name, p := range s.running {
		running[name] = p
	}
	s.mu.Unlock()
	for name, p := range running {
		if p.Exited() {
			continue
		}
		st := traceStep{keyword: step.Keyword, text: step.Text, line: step.Line, at: time.Since(sc.Started()), scale: 1}
		snap, ok := p.(snapshotter)
		if !ok {
			img, scale, err := p.Window()
			if err != nil {
				continue
			}
			st.png = encodePNG(downscale(img, scale))
		}
		s.mu.Lock()
		r := s.recordingOf(name)
		i := len(r.trace)
		r.trace = append(r.trace, st)
		s.mu.Unlock()
		if ok {
			// Taken while the next step finds what it acts on: it acts once
			// the capture is done (traced), and the capture shows the window
			// as this step left it.
			s.tracing.Add(1)
			go func() {
				defer s.tracing.Done()
				b, scale, err := snap.Snapshot()
				if err != nil {
					return
				}
				s.mu.Lock()
				r.trace[i].png, r.trace[i].scale = b, scale
				s.mu.Unlock()
			}()
		}
	}
	return nil
}

// startVideo records the app's window while it runs, when the settings keep
// videos.
func startVideo(sc *core.Scenario, s *scenario, name string, p Process) {
	cfg, err := settingsFor(sc.Suite())
	if err != nil || cfg.videos == "never" {
		return
	}
	v := &video{stop: make(chan struct{}), done: make(chan struct{})}
	s.mu.Lock()
	r := s.recordingOf(name)
	if r.video != nil {
		r.video.end()
		r.video.joinAfter(v)
	}
	r.video = v
	s.mu.Unlock()
	go v.record(p)
}

// finishRecordings keeps or drops what the scenario recorded of its apps, as
// it ends and before they stop.
func finishRecordings(sc *core.Scenario, s *scenario) {
	if len(s.recordings) == 0 {
		return
	}
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return
	}
	failed := sc.Status() == "failed"
	for _, name := range s.order {
		r, ok := s.recordings[name]
		if !ok {
			continue
		}
		if r.video != nil {
			r.video.end()
			if keeps(cfg.videos, failed) && len(r.video.frames) > 1 {
				path := recordingPath(sc, "videos", name, ".png")
				if err := os.WriteFile(path, r.video.apng(), 0o644); err == nil {
					sc.Log("the %s app's video: %s", name, relative(sc, path))
					sc.Attach("image/png", mustRead(path), "the "+name+" app's video")
					announce(sc, "video", path)
				}
			}
		}
		if len(r.trace) > 0 && keeps(cfg.traces, failed) {
			var controls string
			if p, ok := s.running[name]; ok && failed && !p.Exited() {
				if tree, err := p.Tree(); err == nil {
					controls = Outline(tree)
				}
			}
			path := recordingPath(sc, "traces", name, ".html")
			if err := os.WriteFile(path, traceHTML(sc, name, r.trace, failed, controls), 0o644); err == nil {
				sc.Log("the %s app's trace: %s (open it in a browser)", name, relative(sc, path))
				sc.Attach("text/html", mustRead(path), "the "+name+" app's trace")
				announce(sc, "trace", path)
			}
		}
	}
	s.recordings = nil
}

// traceHTML is a trace's page: each step, and the window after it.
func traceHTML(sc *core.Scenario, app string, steps []traceStep, failed bool, controls string) []byte {
	var b strings.Builder
	esc := html.EscapeString
	fmt.Fprintf(&b, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>%s · %s</title><style>
:root{color-scheme:light dark;--fg:#1d2330;--muted:#5b6475;--bg:#f6f7f9;--card:#fff;--line:#d9dde5;--bad:#c62828}
@media (prefers-color-scheme:dark){:root{--fg:#e6e8ee;--muted:#9aa3b5;--bg:#14171d;--card:#1d222b;--line:#2c333f;--bad:#ef5350}}
body{margin:0;padding:24px 16px;background:var(--bg);color:var(--fg);font:14px/1.5 system-ui,sans-serif}
main{max-width:1100px;margin:0 auto;display:grid;gap:16px}
h1{font-size:20px;margin:0}p{margin:0;color:var(--muted)}
section{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:12px 16px;display:grid;gap:8px}
section.failed{border-color:var(--bad)}
h2{font-size:15px;margin:0;font-weight:600}h2 b{font-weight:700}h2 span{color:var(--muted);font-weight:400;font-size:13px}
img{max-width:100%%;height:auto;border:1px solid var(--line);border-radius:4px}
pre{margin:0;overflow:auto;font-size:12px;max-height:480px}
</style></head><body><main><h1>%s</h1><p>The %s app after each step of %s (%s:%d).</p>`,
		esc(sc.Name), esc(app), esc(sc.Name), esc(app), esc(sc.Name), esc(sc.URI), sc.Line)
	for i, st := range steps {
		class, status := "", ""
		if failed && i == len(steps)-1 {
			class, status = ` class="failed"`, " · failed"
		}
		if len(st.png) == 0 {
			fmt.Fprintf(&b, `<section%s><h2><b>%s</b> %s <span>line %d · %.1fs%s</span></h2><p>No capture of the window.</p></section>`,
				class, esc(st.keyword), esc(st.text), st.line, st.at.Seconds(), status)
			continue
		}
		size := ""
		if w, h, ok := pngSize(st.png); ok && st.scale > 1 {
			size = fmt.Sprintf(` width="%d" height="%d"`, int(float64(w)/st.scale), int(float64(h)/st.scale))
		}
		fmt.Fprintf(&b, `<section%s><h2><b>%s</b> %s <span>line %d · %.1fs%s</span></h2><img alt="the %s app after this step"%s src="data:image/png;base64,%s"></section>`,
			class, esc(st.keyword), esc(st.text), st.line, st.at.Seconds(), status, esc(app), size, base64.StdEncoding.EncodeToString(st.png))
	}
	if controls != "" {
		fmt.Fprintf(&b, `<section class="failed"><h2>The %s app's controls, as the scenario failed</h2><pre>%s</pre></section>`, esc(app), esc(controls))
	}
	b.WriteString("</main></body></html>\n")
	return []byte(b.String())
}

// traced is an app's process that waits for the window's trace captures
// before it changes the window: a capture shows the window as the step
// before left it, while the next step finds what it acts on.
type traced struct {
	Process
	s *scenario
}

func (t traced) Front() error { t.s.tracing.Wait(); return t.Process.Front() }
func (t traced) Away() error  { t.s.tracing.Wait(); return t.Process.Away() }
func (t traced) ScrollIntoView(c Control) error {
	t.s.tracing.Wait()
	return t.Process.ScrollIntoView(c)
}
func (t traced) Click(c Control) error  { t.s.tracing.Wait(); return t.Process.Click(c) }
func (t traced) Key(spec string) error  { t.s.tracing.Wait(); return t.Process.Key(spec) }
func (t traced) Type(text string) error { t.s.tracing.Wait(); return t.Process.Type(text) }
func (t traced) Stop() error            { t.s.tracing.Wait(); return t.Process.Stop() }

func (t traced) ScrollTo(k appcore.Kind, name string) (Control, error) {
	t.s.tracing.Wait()
	return t.Process.ScrollTo(k, name)
}

func (t traced) ClickAt(c Control, x, y float64) error {
	t.s.tracing.Wait()
	return t.Process.ClickAt(c, x, y)
}

func (t traced) Drag(c Control, x1, y1, x2, y2 float64) error {
	t.s.tracing.Wait()
	return t.Process.Drag(c, x1, y1, x2, y2)
}

// pngSize is a PNG's width and height, from its header.
func pngSize(b []byte) (w, h int, ok bool) {
	if len(b) < 24 || string(b[1:4]) != "PNG" || string(b[12:16]) != "IHDR" {
		return 0, 0, false
	}
	return int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24])), true
}

// video is an app's window as it changed: each frame compressed as it
// comes, a frame the same as the last only lengthening it.
type video struct {
	mu     sync.Mutex
	frames []videoFrame
	width  int
	height int
	last   [32]byte
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

type videoFrame struct {
	at   time.Time
	data []byte // the frame's zlib-compressed scanlines
}

// frameEvery is how often a video looks at the window.
const frameEvery = 250 * time.Millisecond

func (v *video) record(p Process) {
	defer close(v.done)
	t := time.NewTicker(frameEvery)
	defer t.Stop()
	for {
		if img, scale, err := p.Window(); err == nil {
			v.add(downscale(img, scale), time.Now())
		}
		select {
		case <-v.stop:
			return
		case <-t.C:
		}
	}
}

func (v *video) end() {
	v.once.Do(func() { close(v.stop) })
	<-v.done
}

// joinAfter keeps an earlier video's frames (the app before a restart) at
// the start of this one.
func (v *video) joinAfter(next *video) {
	next.frames, next.width, next.height = v.frames, v.width, v.height
}

func (v *video) add(img *image.RGBA, at time.Time) {
	b := img.Bounds()
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.frames) == 0 {
		v.width, v.height = b.Dx(), b.Dy()
	}
	if b.Dx() != v.width || b.Dy() != v.height {
		img = fitTo(img, v.width, v.height) // the window was resized
	}
	sum := sha256.Sum256(img.Pix)
	if len(v.frames) > 0 && sum == v.last {
		return
	}
	v.last = sum
	v.frames = append(v.frames, videoFrame{at: at, data: scanlines(img)})
}

// apng is the video as an animated PNG, each frame shown until the next.
func (v *video) apng() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(v.width))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(v.height))
	ihdr[8], ihdr[9] = 8, 6 // 8 bits, RGBA
	chunk(&out, "IHDR", ihdr)
	actl := make([]byte, 8)
	binary.BigEndian.PutUint32(actl[0:], uint32(len(v.frames)))
	chunk(&out, "acTL", actl) // played over and over, as a GIF is
	seq := uint32(0)
	for i, f := range v.frames {
		delay := time.Second
		if i+1 < len(v.frames) {
			delay = v.frames[i+1].at.Sub(f.at)
		}
		ms := uint16(min(math.MaxUint16, max(1, delay.Milliseconds())))
		fctl := make([]byte, 26)
		binary.BigEndian.PutUint32(fctl[0:], seq)
		binary.BigEndian.PutUint32(fctl[4:], uint32(v.width))
		binary.BigEndian.PutUint32(fctl[8:], uint32(v.height))
		binary.BigEndian.PutUint16(fctl[20:], ms)
		binary.BigEndian.PutUint16(fctl[22:], 1000)
		chunk(&out, "fcTL", fctl)
		seq++
		if i == 0 {
			chunk(&out, "IDAT", f.data)
			continue
		}
		fdat := make([]byte, 4, 4+len(f.data))
		binary.BigEndian.PutUint32(fdat, seq)
		chunk(&out, "fdAT", append(fdat, f.data...))
		seq++
	}
	chunk(&out, "IEND", nil)
	return out.Bytes()
}

func chunk(w *bytes.Buffer, kind string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	w.Write(n[:])
	crc := crc32.NewIEEE()
	crc.Write([]byte(kind))
	crc.Write(data)
	w.WriteString(kind)
	w.Write(data)
	binary.BigEndian.PutUint32(n[:], crc.Sum32())
	w.Write(n[:])
}

// scanlines is an RGBA image's rows as PNG data: each unfiltered,
// compressed.
func scanlines(img *image.RGBA) []byte {
	var b bytes.Buffer
	z, _ := zlib.NewWriterLevel(&b, zlib.BestSpeed)
	w := img.Bounds().Dx() * 4
	for y := range img.Bounds().Dy() {
		_, _ = z.Write([]byte{0})
		_, _ = z.Write(img.Pix[y*img.Stride : y*img.Stride+w])
	}
	_ = z.Close()
	return b.Bytes()
}

// downscale is a window's capture at a scale of 1: a display at twice the
// scale shows each point as 2 by 2 pixels, more than a trace needs.
func downscale(img image.Image, scale float64) *image.RGBA {
	f := int(math.Round(scale))
	b := img.Bounds()
	if f < 2 {
		out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
		return out
	}
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	w, h := b.Dx()/f, b.Dy()/f
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			var sum [4]int
			for dy := range f {
				i := (y*f+dy)*src.Stride + x*f*4
				for dx := range f {
					for c := range 4 {
						sum[c] += int(src.Pix[i+dx*4+c])
					}
				}
			}
			o := y*out.Stride + x*4
			for c := range 4 {
				out.Pix[o+c] = uint8(sum[c] / (f * f))
			}
		}
	}
	return out
}

// fitTo is the image on a canvas of the size, cut or padded.
func fitTo(img *image.RGBA, w, h int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9]+`)

// recordingPath is where a scenario keeps a file of an app, in
// .axx/desktop/<kind>, as the web pack keeps its pages'.
func recordingPath(sc *core.Scenario, kind, app, ext string) string {
	dir := filepath.Join(sc.Suite().ProjectDir(), ".axx", "desktop", kind)
	_ = os.MkdirAll(dir, 0o755)
	name := strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(sc.Name), "-"), "-")
	if len(name) > 60 {
		name = name[:60]
	}
	id := unsafeName.ReplaceAllString(sc.ID, "")
	if len(id) > 8 {
		id = id[len(id)-8:]
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s-%s%s", name, app, id, ext))
}

func relative(sc *core.Scenario, path string) string {
	if rel, err := filepath.Rel(sc.Suite().ProjectDir(), path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

func announce(sc *core.Scenario, kind, path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sc.Suite().Announce(kind, "path", abs, "location", fmt.Sprintf("%s:%d", sc.URI, sc.Line))
}

func mustRead(path string) []byte {
	b, _ := os.ReadFile(path)
	return b
}
