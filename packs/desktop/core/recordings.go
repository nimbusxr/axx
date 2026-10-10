package desktopcore

import (
	"image"
	"image/draw"
	"math"
	"time"

	"github.com/nimbusxr/axx/core"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	"github.com/nimbusxr/axx/packs/internal/recording"
)

// A scenario's desktop apps can be kept as the web pack keeps its pages
// (packs.desktop-core.traces and .videos): a trace, one HTML page with each
// app's window after each step, and its controls where the scenario failed;
// a video, the scenario's desktop as it ran (video.go). Package recording
// keeps them, as it keeps the mobile packs'.

// appRecording is what a scenario keeps of one app as it runs.
type appRecording struct {
	app   string
	trace []recording.TraceStep
}

// snapshotter is a Process that captures its window as a PNG as it shows,
// cursor and all, cheaper than Window: for a trace, which keeps it as it
// comes (macOS's screencapture writes one; decoding it, hiding the cursor
// and encoding it again took as long as the capture).
type snapshotter interface {
	Snapshot() (png []byte, scale float64, err error)
}

// recordingOf is the scenario's recording of the app, made on first use.
func (s *scenario) recordingOf(app string) *appRecording {
	if s.recordings == nil {
		s.recordings = map[string]*appRecording{}
	}
	r, ok := s.recordings[app]
	if !ok {
		r = &appRecording{app: app}
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
		st := recording.TraceStep{Keyword: step.Keyword, Text: step.Text, Line: step.Line, At: time.Since(sc.Started()), MIME: "image/png", Scale: 1}
		snap, ok := p.(snapshotter)
		if !ok {
			img, scale, err := p.Window()
			if err != nil {
				continue
			}
			st.Image = encodePNG(downscale(img, scale))
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
				r.trace[i].Image, r.trace[i].Scale = b, scale
				s.mu.Unlock()
			}()
		}
	}
	return nil
}

// finishRecordings keeps or drops what the scenario recorded of its apps, as
// it ends and before they stop.
func finishRecordings(sc *core.Scenario, s *scenario) {
	cfg, err := settingsFor(sc.Suite())
	if err != nil {
		return
	}
	failed := sc.Status() == "failed"
	finishVideo(sc, s, cfg, failed)
	for _, name := range s.order {
		r, ok := s.recordings[name]
		if !ok {
			continue
		}
		if len(r.trace) > 0 && recording.Keeps(cfg.traces, failed) {
			var controls string
			if p, ok := s.running[name]; ok && failed && !p.Exited() {
				if tree, err := p.Tree(); err == nil {
					controls = Outline(tree)
				}
			}
			recording.WriteTrace(sc, "desktop", name, r.trace, failed, controls)
		}
	}
	s.recordings = nil
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
func (t traced) Leave() error { t.s.tracing.Wait(); return t.Process.Leave() }
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

func (t traced) ClickAt(c Control, from Anchor, x, y float64) error {
	t.s.tracing.Wait()
	return t.Process.ClickAt(c, from, x, y)
}

func (t traced) Drag(c Control, from Anchor, x1, y1, x2, y2 float64) error {
	t.s.tracing.Wait()
	return t.Process.Drag(c, from, x1, y1, x2, y2)
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
