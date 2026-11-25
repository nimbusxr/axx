package desktopcore

import (
	"image"
	"math"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/recording"
	"github.com/nimbusxr/axx/packs/internal/video"
)

// A scenario's video is its desktop as it ran, the pointer drawn where it
// was, and beside it the scenario's steps, each as it stands (to run,
// running, passed, failed, skipped): package recording keeps it, as it keeps
// the mobile packs'. A run keeps one more, run.mp4: its kept scenarios one
// after another, each after a card with its name and how it ended.

// startRecording records the scenario's desktop from its first app's start,
// when the settings keep its video.
func startRecording(sc *core.Scenario, s *scenario) {
	cfg, err := settingsFor(sc.Suite())
	// What watches the scenario's screens (a demo) is shown the desktop
	// whatever the settings keep.
	watched := recording.Watched(sc)
	if err != nil || cfg.videos == "never" && !watched {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desktop == nil || (s.recorder != nil || cfg.videos == "never") && (s.shown || !watched) {
		return
	}
	desk := s.desktop
	look := func() *image.RGBA {
		sh, err := desk.Screen()
		if err != nil || sh.Image == nil {
			return nil
		}
		screen := downscale(sh.Image, sh.Scale)
		if sh.HasPointer {
			f := math.Max(1, math.Round(sh.Scale))
			video.DrawPointer(screen, image.Pt(int(float64(sh.Pointer.X)/f), int(float64(sh.Pointer.Y)/f)), 1)
		}
		return screen
	}
	if s.recorder == nil && cfg.videos != "never" {
		s.recorder = recording.Record(sc, look, video.ScreenQuality)
	}
	if watched && !s.shown {
		s.shown = true
		recording.Screen(sc, "desktop", look)
	}
}

// finishVideo keeps the scenario's video when the settings say, and adds it
// to the run's.
func finishVideo(sc *core.Scenario, s *scenario, cfg *settings, failed bool) {
	r := s.recorder
	if r == nil {
		return
	}
	s.recorder = nil
	recording.Keep(sc, "desktop", r, cfg.videos, failed)
}
