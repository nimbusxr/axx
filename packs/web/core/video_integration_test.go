//go:build integration

package webcore

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/internal/recording"
	"github.com/nimbusxr/axx/packs/internal/video"
)

// A scenario's video is the tab it acts on with its steps beside it, H.264
// in an MP4 file, in every engine: the page shows in it, the trace keeps
// its own pictures, and the run keeps one video of its scenarios.
func TestAScenariosVideoShowsItsPagesWithItsStepsBeside(t *testing.T) {
	for _, engine := range bundled {
		t.Run(engine, func(t *testing.T) {
			h, app := newConfigured(t, engine, &Config{Videos: "always", Traces: "always"})
			h.OK("the portal web app with the following properties:", app)
			h.OK(`the "/quote.html" page is opened`)
			h.OK(`the "Get a quote" button is clicked`)
			h.OK(`the page shows "Enter a weight"`)
			// A scenario's steps take a moment each: the video looks ten
			// times a second.
			time.Sleep(3 * time.Second / recording.FrameRate)
			h.OK(`the "/parcel.html" page is opened`)
			h.OK(`the page shows "Anna Weber"`)
			time.Sleep(3 * time.Second / recording.FrameRate)
			if err := h.End("passed"); err != nil {
				t.Fatal(err)
			}

			videos, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "videos", "*.mp4"))
			if len(videos) != 1 {
				t.Fatalf("the scenario's videos: %v, want one", videos)
			}
			movie, err := os.ReadFile(videos[0])
			if err != nil {
				t.Fatal(err)
			}
			// A chapter a step: the steps that ran in one moment share one.
			for _, want := range []string{"avcC", "chpl", `* the "/parcel.html" page is opened`} {
				if !bytes.Contains(movie, []byte(want)) {
					t.Errorf("the video lacks %q: no H.264 in an MP4 file with a chapter a step", want)
				}
			}
			frames, err := video.Frames(movie)
			if err != nil {
				t.Fatal(err)
			}
			if len(frames) < 3 {
				t.Fatalf("the video has %d frames, want its opening and the pages as they changed", len(frames))
			}
			last := frames[len(frames)-1]
			panel := video.PanelWidth(videoHeight)
			if b := last.Bounds(); b.Dx() != videoWidth+panel || b.Dy() != videoHeight {
				t.Fatalf("the video is %dx%d, want the page's %dx%d and the steps' %d beside it", b.Dx(), b.Dy(), videoWidth, videoHeight, panel)
			}
			// The page: the portal's light background, with its text on it.
			// Beside it, the steps' dark panel.
			if y := last.YCbCrAt(videoWidth-40, videoHeight-40).Y; y < 200 {
				t.Errorf("the page's background shows as %d, want the portal's light one", y)
			}
			if dark := darkIn(last, image.Rect(0, 0, videoWidth/2, videoHeight/2)); dark < 400 {
				t.Errorf("the page shows %d dark pixels, want the text of the parcel's page", dark)
			}
			if y := last.YCbCrAt(videoWidth+panel/2, videoHeight-40).Y; y > 60 {
				t.Errorf("beside the page shows as %d, want the steps' dark panel", y)
			}

			if !slices.ContainsFunc(h.Sink.Logs, func(l string) bool { return strings.Contains(l, "the scenario's video: .axx/web/videos/") }) {
				t.Errorf("the scenario does not say where its video is: %v", h.Sink.Logs)
			}
			for _, a := range h.Sink.Attachments {
				if a.MediaType == "video/mp4" {
					t.Error("a passed scenario's video is in the report: it is in its folder only")
				}
			}

			// The trace takes its pictures of the tabs from the same frames.
			traces, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "traces", "*.zip"))
			if len(traces) != 1 {
				t.Fatalf("the scenario's traces: %v, want one", traces)
			}
			z, err := zip.OpenReader(traces[0])
			if err != nil {
				t.Fatal(err)
			}
			pictures := 0
			for _, f := range z.File {
				if strings.HasSuffix(f.Name, ".jpeg") {
					pictures++
				}
			}
			_ = z.Close()
			if pictures == 0 {
				t.Error("the trace has no picture of the page: the video took its frames away")
			}

			if err := h.Suite.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			run, err := os.ReadFile(filepath.Join(h.Dir, ".axx", "web", "videos", "run.mp4"))
			if err != nil || len(run) <= len(movie) {
				t.Errorf("the run's video is the scenario's after cards: %d bytes, the scenario's %d (%v)", len(run), len(movie), err)
			}
			if !bytes.Contains(run, []byte("✓ "+h.SC.Name)) {
				t.Errorf("the run's video has no chapter for its scenario %q", h.SC.Name)
			}
		})
	}
}

// darkIn counts a frame's dark pixels in a part of it: text, on a light page.
func darkIn(f *image.YCbCr, r image.Rectangle) int {
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if f.YCbCrAt(x, y).Y < 90 {
				n++
			}
		}
	}
	return n
}

// A scenario that keeps no video sends no frames for one.
func TestAScenarioWithoutAVideoRecordsNone(t *testing.T) {
	h, app := newHarness(t, "chromium")
	h.OK("the portal web app with the following properties:", app)
	h.OK(`the "/quote.html" page is opened`)
	st := scenarioPages.Of(h.SC)
	if st.video != nil || st.current.video != nil {
		t.Error("a scenario records a video its settings do not keep")
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
	if videos, _ := filepath.Glob(filepath.Join(h.Dir, ".axx", "web", "videos", "*")); len(videos) != 0 {
		t.Errorf("videos kept by default: %v", videos)
	}
}
