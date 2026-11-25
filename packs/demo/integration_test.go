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
	script, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), Known{})
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
	each := DefaultPace.Exchange
	got := shown(t, s, req.Start+0.2*each)
	if !strings.HasPrefix(got["subtitle"], "Ms Okafor is away") || !strings.Contains(got["diagram"], "parcels") || !strings.Contains(got["diagram"], "shop") {
		t.Errorf("as the request goes out: %v", got)
	}
	if !strings.Contains(got["sent"], "PUT /api/parcels/PX-1/hold") || got["seen"] != "" || !strings.Contains(got["ticker"], "a PUT request to") {
		t.Errorf("as the request goes out, the messages: %v", got)
	}
	if got = shown(t, s, req.Start+0.95*each); !strings.Contains(got["seen"], "200 OK") || !strings.Contains(got["seen"], `"AT_DEPOT"`) {
		t.Errorf("once the answer is back: %v", got)
	}
	// The mock's call came from the system, and where the scenario talks to
	// one service, from that one.
	if got = shown(t, s, req.Start+each+0.3*each); !strings.Contains(got["sent"], "parcels → address") || !strings.Contains(got["sent"], "GET /v1/postcodes/DE/10115") {
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
	script, err := ParseScript("features/hold.demo.feature", []byte(src), st.known())
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

// A project's diagram, written in Mermaid, is the map the demo is drawn on,
// in characters: every box of it, the scenario's exchanges on its lines, the
// lines a cue lights, and another diagram where a cue goes to one. A cue that
// names what its diagram has not makes no film.
func TestAProjectsDiagrams(t *testing.T) {
	ctx := context.Background()
	system := "flowchart TD\n  shop([shop]) --> parcels\n  parcels --> parcels-db[(parcels-db)]\n  parcels -.-> address\n  parcels-db --> reports[Monthly reports]\n"
	other := "flowchart LR\n  parcels --> warehouse\n"
	src := strings.Replace(holdFeature, "# demo: [2s] The depot's", "# demo: [parcels-db --> reports] [2s] The depot's", 1)
	src = strings.Replace(src, "# demo: [speed 4x]", "# demo: [diagram other] [speed 4x]", 1)
	src = strings.Replace(src, "# demo: [action] Ms Okafor", "# demo: [action] [as shop] Ms Okafor", 1)
	var st Settings
	st.Diagrams = Diagrams{Names: []string{"system", "other"}, Files: map[string]string{"system": "system.mmd", "other": "other.mmd"}}
	var err error
	if st.mermaid, err = mermaidFile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	script, err := ParseScript("features/hold.demo.feature", []byte(src), st.known())
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	take := holdTake()
	tl := Cut(take, script, DefaultPace)
	diagrams := []Diagram{{Name: "system", Source: system}, {Name: "other", Source: other}}
	s, err := openStudio(ctx, t.TempDir(), st, &Staged{Take: take, Timeline: tl, Diagrams: diagrams})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	lit := func() string {
		got, err := s.page.Evaluate(`() => [...new Set([...document.querySelectorAll('.surface-diagram .grid span')].filter((e) => /k-(coord|ok|told)/.test(e.className) && /[─│>┌┐└┘├┤┬┴┼╌╎]/.test(e.textContent)).map((e) => e.className.match(/k-(coord|ok|told)/)[1]))].sort().join(',')`)
		if err != nil {
			t.Fatal(err)
		}
		return got.(string)
	}
	// The request: every box of the map is there, the one the scenario never
	// talks to among them, and the request goes along the shop's line.
	req := tl.Segments[1]
	got := shown(t, s, req.Start+0.2*DefaultPace.Exchange)
	for _, box := range []string{"shop", "parcels", "parcels-db", "address", "Monthly reports"} {
		if !strings.Contains(got["diagram"], box) {
			t.Errorf("the map has no %q:\n%s", box, got["diagram"])
		}
	}
	if lit() != "coord" {
		t.Errorf("as the request goes out, the lines lit: %q", lit())
	}
	// The selection: what the scenario found came along the system's own
	// line, and the cue's line is lit as told.
	sel := tl.Segments[3]
	shown(t, s, sel.Start+0.4)
	if lit() != "ok,told" {
		t.Errorf("as the selection plays, the lines lit: %q", lit())
	}
	// The cue went to the other diagram.
	if got = shown(t, s, tl.Segments[5].Start+0.1); !strings.Contains(got["diagram"], "warehouse") || strings.Contains(got["diagram"], "Monthly reports") {
		t.Errorf("after [diagram other]:\n%s", got["diagram"])
	}

	// What a cue names is in its diagram, and a diagram is a flowchart.
	bad := strings.Replace(src, "[parcels-db --> reports]", "[parcels-db --> nowhere]", 1)
	script, _ = ParseScript("features/hold.demo.feature", []byte(bad), st.known())
	if _, err := openStudio(ctx, t.TempDir(), st, &Staged{Take: take, Timeline: Cut(take, script, DefaultPace), Diagrams: diagrams}); err == nil || !strings.Contains(err.Error(), "the diagram system has no line from parcels-db to nowhere") {
		t.Errorf("a cue that lights a line there is not: %v", err)
	}
	if _, err := openStudio(ctx, t.TempDir(), st, &Staged{Take: take, Timeline: tl, Diagrams: []Diagram{{Name: "system", Source: "flowchart LR\n  a --> "}}}); err == nil || !strings.Contains(err.Error(), "the diagram system cannot be read") {
		t.Errorf("a diagram that is no Mermaid: %v", err)
	}
}
