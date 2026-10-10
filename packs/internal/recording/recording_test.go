package recording

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/video"
)

func TestTracePageShowsSnapshotsAtTheirScale(t *testing.T) {
	var shot bytes.Buffer
	_ = png.Encode(&shot, image.NewRGBA(image.Rect(0, 0, 1280, 1160)))
	page := string(TraceHTML(&core.Scenario{Name: "A clerk registers a parcel"}, "depot",
		[]TraceStep{{Keyword: "When", Text: "the depot app is launched", Image: shot.Bytes(), Scale: 2}}, false, ""))
	if !strings.Contains(page, `width="640" height="580"`) {
		t.Errorf("a 2x snapshot is not shown at its points' size: %.300s", page)
	}
	if !strings.Contains(page, "data:image/png;base64,") {
		t.Errorf("a capture with no type is a PNG: %.300s", page)
	}
	if w, h, ok := pngSize(shot.Bytes()); !ok || w != 1280 || h != 1160 {
		t.Errorf("pngSize: %d, %d, %v", w, h, ok)
	}
}

// A phone's screen comes as a JPEG, and shows as one.
func TestTracePageShowsJPEGs(t *testing.T) {
	page := string(TraceHTML(&core.Scenario{Name: "A courier delivers a parcel"}, "courier",
		[]TraceStep{{Keyword: "When", Text: "the courier app is launched", Image: []byte{0xff, 0xd8, 0xff}, MIME: "image/jpeg", Scale: 1}}, true, "button \"Deliver\""))
	for _, want := range []string{"data:image/jpeg;base64,", `class="failed"`, "button &#34;Deliver&#34;"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

// A video's chapters are its steps from when they ran, after its opening:
// the first from the start; one that ran in the same moment as the one
// before it, with it.
func TestVideoChapters(t *testing.T) {
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{Name: "a parcel arrives"}, nil, nil)
	sc.SetProgress([]core.StepProgress{
		{StepInfo: core.StepInfo{Keyword: "Given", Text: "the depot app is launched"}, Status: "passed"},
		{StepInfo: core.StepInfo{Keyword: "When", Text: "a parcel is registered"}, Status: "passed"},
		{StepInfo: core.StepInfo{Keyword: "Then", Text: "the parcel is listed"}, Status: "passed"},
	})
	r := &Recorder{sc: sc, marks: []mark{{0, 500 * time.Millisecond}, {1, 1200 * time.Millisecond}, {2, 1250 * time.Millisecond}}}
	got := r.chapters()
	want := []video.Chapter{{Title: "Given the depot app is launched", At: 0}, {Title: "When a parcel is registered", At: openingLength + 1200*time.Millisecond}}
	if !slices.Equal(got, want) {
		t.Errorf("chapters %+v, want %+v", got, want)
	}
}

func TestKeeps(t *testing.T) {
	for _, c := range []struct {
		policy string
		failed bool
		want   bool
	}{{"always", false, true}, {"failed", true, true}, {"failed", false, false}, {"never", true, false}} {
		if got := Keeps(c.policy, c.failed); got != c.want {
			t.Errorf("Keeps(%q, %v) = %v", c.policy, c.failed, got)
		}
	}
}
