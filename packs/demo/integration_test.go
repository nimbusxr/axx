//go:build integration

package demo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/internal/video"
)

// shown is what the stage shows at a moment: the texts of its parts that are
// not hidden.
func shown(t *testing.T, s *studio, at float64) map[string]string {
	t.Helper()
	if _, err := s.seek(time.Duration(at * float64(time.Second))); err != nil {
		t.Fatal(err)
	}
	got, err := s.page.Evaluate(`() => {
		const text = (sel) => { const el = document.querySelector(sel); return el && !el.hidden && !el.closest('[hidden]') ? el.innerText : ''; };
		return {
			card: text('#stage .card'), subtitle: text('#stage .subtitle'), ticker: text('#stage .ticker'),
			diagram: text('.surface-diagram .grid'), sent: text('.msg.sent.on'), seen: text('.msg.seen.on'),
			own: text('.own-scene'), said: (window.saidDirections || []).join(','),
		};
	}`)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range got.(map[string]any) {
		out[k], _ = v.(string)
	}
	return out
}

// The stage, in a real browser, shows of each moment of a film what the
// timeline says: the card, then each step with its subtitle and what it sent
// and saw, and nothing of the steps the camera was off for.
func TestTheStageShowsEachMomentOfTheFilm(t *testing.T) {
	script, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), nil)
	if err != nil {
		t.Fatal(err)
	}
	take := holdTake()
	tl := Cut(take, script, DefaultPace)
	st := Settings{Theme: Theme{Caller: "shop"}}
	staged := &Staged{Take: take, Timeline: tl, Theme: st.Theme}
	ctx := context.Background()
	s, err := openStudio(ctx, t.TempDir(), st, staged)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	if got := shown(t, s, 1); got["card"] != "Holding a parcel at the depot" || got["subtitle"] != "" || got["diagram"] != "" {
		t.Errorf("during the card: %v", got)
	}
	// The request: its subtitle, the box of what it went to, what was sent
	// and, once the answer is back, what was seen.
	req := tl.Segments[1]
	got := shown(t, s, req.Start+0.3*2.2)
	if !strings.HasPrefix(got["subtitle"], "Ms Okafor is away") || !strings.Contains(got["diagram"], "parcels") || !strings.Contains(got["diagram"], "shop") {
		t.Errorf("as the request goes out: %v", got)
	}
	if !strings.Contains(got["sent"], "PUT /api/parcels/PX-1/hold") || got["seen"] != "" || !strings.Contains(got["ticker"], "a PUT request to") {
		t.Errorf("as the request goes out, the messages: %v", got)
	}
	if got = shown(t, s, req.Start+0.95*2.2); !strings.Contains(got["seen"], "200 OK") || !strings.Contains(got["seen"], `"AT_DEPOT"`) {
		t.Errorf("once the answer is back: %v", got)
	}
	// The mock's call came from the system, and where the scenario talks to
	// one service, from that one.
	if got = shown(t, s, req.Start+2.2+0.3*2.2); !strings.Contains(got["sent"], "parcels → address") || !strings.Contains(got["sent"], "GET /v1/postcodes/DE/10115") {
		t.Errorf("the call the mock received: %v", got)
	}
	sel := tl.Segments[3]
	if got = shown(t, s, sel.Start+1.9); got["subtitle"] != "The depot's database has the hold at once." || !strings.Contains(got["seen"], "1 row") {
		t.Errorf("the selection: %v", got)
	}
	if got = shown(t, s, tl.Duration); got["subtitle"] != "That is the whole of it." {
		t.Errorf("at the end: %v", got)
	}

	// The film is as long as its timeline, with a chapter for each subtitle.
	out := filepath.Join(t.TempDir(), "hold.mp4")
	if err := Film(ctx, t.TempDir(), st, staged, out, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	track, err := video.ReadMP4(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(track.Samples) < 20 {
		t.Errorf("the film has %d frames", len(track.Samples))
	}
	for _, sub := range tl.Subtitles {
		if !bytes.Contains(data, []byte(sub.Text)) {
			t.Errorf("the film has no chapter %q", sub.Text)
		}
	}
}

// A project's own scene replaces the pack's of its name, its stylesheet is
// loaded, and its own directions reach its scenes as the film comes to them.
func TestAProjectsOwnLook(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	scene := `axxStage.scene('diagram', {
	mount(el) { el.innerHTML = '<p class="own-scene"></p>'; },
	render(el, state) { el.querySelector('.own-scene').textContent = 'ours: ' + (state.step ? state.step.text : ''); },
});
window.saidDirections = [];
axxStage.on('direction', (d) => window.saidDirections.push(d.name + ' ' + d.args.join(' ')));`
	if err := os.WriteFile(filepath.Join(dir, "demo", "scenes.js"), []byte(scene), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo", "stage.css"), []byte(`.own-scene { color: rgb(1, 2, 3); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(holdFeature, "# demo: [2s] The depot's", "# demo: [confetti twice] [2s] The depot's", 1)
	st := Settings{Directions: []string{"confetti"}, Scenes: []string{"demo/scenes.js"}, Styles: []string{"demo/stage.css"}}
	script, err := ParseScript("features/hold.demo.feature", []byte(src), st.Directions)
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	take := holdTake()
	tl := Cut(take, script, DefaultPace)
	s, err := openStudio(context.Background(), dir, st, &Staged{Take: take, Timeline: tl})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if got := shown(t, s, tl.Segments[1].Start+0.5); got["own"] != "ours: a PUT request to /api/parcels/PX-1/hold" || got["diagram"] != "" || got["said"] != "" {
		t.Errorf("the project's own scene: %v", got)
	}
	if got := shown(t, s, tl.Segments[3].Start+0.5); got["said"] != "confetti twice" {
		t.Errorf("the project's own direction: %v", got)
	}
	color, err := s.page.Evaluate(`() => getComputedStyle(document.querySelector('.own-scene')).color`)
	if err != nil || color != "rgb(1, 2, 3)" {
		t.Errorf("the project's stylesheet: %v, %v", color, err)
	}
}
