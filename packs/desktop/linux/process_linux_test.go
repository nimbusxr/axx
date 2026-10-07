//go:build linux

package desktoplinux

import (
	"image"
	"image/color"
	"strings"
	"testing"

	appcore "github.com/nimbusxr/axx/packs/app/core"
	desktopcore "github.com/nimbusxr/axx/packs/desktop/core"
)

// A window of one color all over has drawn nothing yet; one that shows
// anything else has.
func TestFlat(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for y := range 400 {
		for x := range 640 {
			img.Set(x, y, color.Black)
		}
	}
	if !flat(img) {
		t.Error("a black window shows nothing")
	}
	for y := 180; y < 220; y++ {
		for x := 300; x < 340; x++ {
			img.Set(x, y, color.RGBA{250, 247, 240, 255})
		}
	}
	if flat(img) {
		t.Error("a window with a picture in it shows something")
	}
	if !flat(image.NewRGBA(image.Rectangle{})) {
		t.Error("an empty window shows nothing")
	}
}

// The system's app is read as another app starts it on the scenario's
// desktop: before then, it has no window to show, and its steps say so
// rather than fail on a desktop that is not there yet (a trace takes its
// window after every step).
func TestWatchedBeforeItStarts(t *testing.T) {
	p, err := (&desk{}).Watch(nil, &desktopcore.App{Name: "files", App: "nautilus", System: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Window(); err == nil || !strings.Contains(err.Error(), "not on the scenario's desktop yet") {
		t.Errorf("a window before the app is there: %v", err)
	}
	for name, step := range map[string]func() error{"Key": func() error { return p.Key("Enter") }, "Type": func() error { return p.Type("x") }, "Away": p.Away} {
		if err := step(); err == nil {
			t.Errorf("%s before the app is there succeeded", name)
		}
	}
	if p.Exited() {
		t.Error("the system's app has not exited while it is watched")
	}
	if err := p.Stop(); err != nil {
		t.Errorf("the system's app is not stopped: %v", err)
	}
	if found, err := p.Find(appcore.Kind{Noun: "text"}, ".snap", true); err != nil || len(found) != 0 {
		t.Errorf("nothing is found before the app is there: %v %v", found, err)
	}
}
