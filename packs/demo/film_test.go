package demo

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStills writes pictures of the sample demo at moments of its film, and
// the film, into the folder AXX_DEMO_STILLS names: for a person to look at.
// It needs a browser, so it runs only when asked.
func TestStills(t *testing.T) {
	dir := os.Getenv("AXX_DEMO_STILLS")
	if dir == "" {
		t.Skip("AXX_DEMO_STILLS names no folder")
	}
	s, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), Known{})
	if err != nil {
		t.Fatal(err)
	}
	take := holdTake()
	tl := Cut(take, s, DefaultPace)
	st := Settings{Theme: Theme{Caller: "shop"}}
	staged := &Staged{Take: take, Timeline: tl, Theme: st.Theme}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	studio, err := openStudio(ctx, t.TempDir(), st, staged)
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []float64{1, 3.6, 4.4, 5.6, 6.3, 7.2, 9.2, 10.5, 14} {
		if _, err := studio.seek(time.Duration(at * float64(time.Second))); err != nil {
			t.Fatal(err)
		}
		img, err := studio.shot()
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("still-%04.1f.png", at)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	studio.close()
	start := time.Now()
	if err := Film(ctx, t.TempDir(), st, staged, filepath.Join(dir, "hold.mp4"), nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("a film of %.1f seconds made in %s", tl.Duration, time.Since(start).Round(time.Millisecond))
}
