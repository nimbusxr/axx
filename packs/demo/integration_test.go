//go:build integration

package demo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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
			diagram: [...document.querySelectorAll('.surface-diagram .grid')].filter((e) => !e.hidden && !e.closest('[hidden]')).map((e) => e.innerText).join('\n'), sent: text('.msg.sent.on'), seen: text('.msg.seen.on'),
			own: text('.own-scene'), said: (window.saidDirections || []).join(','),
			terminal: text('.surface-terminal .term-lines'), ledger: text('.surface-ledger'), probe: text('.surface-probe'),
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

	// Two diagrams side by side: both are drawn, and what a step found is on
	// the one that has its box.
	two := strings.Replace(src, "[diagram other] [speed 4x]", "[diagram system + other] [speed 4x]", 1)
	script, err = ParseScript("features/hold.demo.feature", []byte(two), st.known())
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	tl2 := Cut(take, script, DefaultPace)
	s2, err := openStudio(ctx, t.TempDir(), st, &Staged{Take: take, Timeline: tl2, Diagrams: diagrams})
	if err != nil {
		t.Fatal(err)
	}
	got = shown(t, s2, tl2.Segments[5].Start+0.1)
	s2.close()
	if !strings.Contains(got["diagram"], "warehouse") || !strings.Contains(got["diagram"], "Monthly reports") {
		t.Errorf("after [diagram system + other], both diagrams:\n%s", got["diagram"])
	}
	// The picture shows two things at the most: two diagrams beside a screen
	// make no film.
	three := strings.Replace(two, "[diagram system + other]", "[show browser + diagram] [diagram system + other]", 1)
	script, _ = ParseScript("features/hold.demo.feature", []byte(three), st.known())
	if _, err := openStudio(ctx, t.TempDir(), st, &Staged{Take: take, Timeline: Cut(take, script, DefaultPace), Diagrams: diagrams}); err == nil || !strings.Contains(err.Error(), "two things at the most") {
		t.Errorf("two diagrams beside a browser: %v", err)
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

// A project's sequence diagram is drawn too: who takes part, each with its
// line, and the messages in the diagram's order. A step lights the message
// that fits what it sent or found; a message the film has not come to is dim,
// and one it has shown stays.
func TestAProjectsSequenceDiagram(t *testing.T) {
	ctx := context.Background()
	seq := "sequenceDiagram\n  actor shop\n  participant parcels\n  participant db as parcels-db\n  participant address\n" +
		"  shop->>parcels: hold the parcel\n  parcels->>address: where is it going?\n  parcels->>db: keep the hold\n  Note over parcels: a note is not drawn\n  parcels-->>shop: the parcel, held\n  db-)parcels: never shown by this demo\n"
	src := strings.Replace(holdFeature, "# demo: [action] Ms Okafor", "# demo: [action] [as shop] Ms Okafor", 1)
	var st Settings
	st.Diagrams = Diagrams{Names: []string{"holding"}, Files: map[string]string{"holding": "holding.mmd"}}
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
	s, err := openStudio(ctx, t.TempDir(), st, &Staged{Take: take, Timeline: tl, Diagrams: []Diagram{{Name: "holding", Source: seq}}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	// The text of the diagram's characters of a kind: what is lit, what is dim.
	of := func(kind string) string {
		got, err := s.page.Evaluate(`(kind) => [...document.querySelectorAll('.surface-diagram .grid span.k-' + kind)].map((e) => e.textContent).join('').replace(/[^A-Za-z?,' ]+/g, ' ').replace(/ +/g, ' ').trim()`, kind)
		if err != nil {
			t.Fatal(err)
		}
		return got.(string)
	}
	each := DefaultPace.Exchange
	req := tl.Segments[1]
	got := shown(t, s, req.Start+0.1*each)
	for _, who := range []string{"shop", "parcels", "parcels-db", "address"} {
		if !strings.Contains(got["diagram"], who) {
			t.Errorf("the sequence diagram has no %q:\n%s", who, got["diagram"])
		}
	}
	if strings.Contains(got["diagram"], "a note is not drawn") {
		t.Errorf("a note was drawn:\n%s", got["diagram"])
	}
	// The request goes out; nothing else has happened yet.
	if lit, dim := of("coord"), of("dim"); !strings.Contains(lit, "hold the parcel") || !strings.Contains(dim, "keep the hold") || !strings.Contains(dim, "the parcel, held") {
		t.Errorf("as the request goes out: lit %q, dim %q", lit, dim)
	}
	// Its answer comes back along the message that answers it.
	shown(t, s, req.Start+0.5*each)
	if ok := of("ok"); !strings.Contains(ok, "the parcel, held") {
		t.Errorf("as the answer comes back: %q", ok)
	}
	// The call the mock received is the message to it.
	shown(t, s, req.Start+each+0.2*each)
	if ok := of("ok"); !strings.Contains(ok, "where is it going?") {
		t.Errorf("the call the mock received: %q", ok)
	}
	// The selection found the row: the message to the database; the messages
	// shown before it stay, and the one the demo never shows stays dim.
	sel := tl.Segments[3]
	shown(t, s, sel.Start+0.4)
	if ok, past, dim := of("ok"), of("sub"), of("dim"); !strings.Contains(ok, "keep the hold") || !strings.Contains(past, "hold the parcel") || !strings.Contains(dim, "never shown by this demo") {
		t.Errorf("as the selection plays: lit %q, shown before %q, dim %q", ok, past, dim)
	}
}

// [hold response]: a step shows what it sent, and what came back shows with
// the step after it, which is where its cue says what the answer is.
func TestAResponseIsHeldForTheNextStep(t *testing.T) {
	src := strings.Replace(holdFeature, "# demo: [2s] The depot's", "# demo: [hold response] The depot's", 1)
	script, err := ParseScript("features/hold.demo.feature", []byte(src), Known{})
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	take := holdTake()
	tl := Cut(take, script, DefaultPace)
	var sel, check Segment
	for _, seg := range tl.Segments {
		if seg.Kind == "step" && seg.Step == 3 {
			sel = seg
		}
		if seg.Kind == "step" && seg.Step == 4 {
			check = seg
		}
	}
	if !sel.HoldResponse || check.End == 0 {
		t.Fatalf("the selection holds its response, and the check follows it: %+v, %+v", sel, check)
	}
	s, err := openStudio(context.Background(), t.TempDir(), Settings{}, &Staged{Take: take, Timeline: tl})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	// To the end of the selection, and while the picture holds after it: what
	// was asked, and nothing of what was found.
	for _, at := range []float64{sel.Start + 0.9*(sel.End-sel.Start), (sel.End + check.Start) / 2, check.Start + 0.05*(check.End-check.Start)} {
		got := shown(t, s, at)
		if !strings.Contains(got["sent"], "select parcels.holds") || got["seen"] != "" || strings.Contains(got["diagram"], "1 row") {
			t.Errorf("at %.2f, before the answer is shown: %v", at, got)
		}
	}
	// With the check, it is shown.
	if got := shown(t, s, check.Start+0.9*(check.End-check.Start)); !strings.Contains(got["seen"], "1 row") {
		t.Errorf("with the step after it: %v", got)
	}
	if got := shown(t, s, tl.Duration); !strings.Contains(got["seen"], "1 row") || !strings.Contains(got["diagram"], "1 row") {
		t.Errorf("at the end: %v", got)
	}
}

// [mark box text]: a cue writes in a box the scenario does not talk to, once
// its step is over, and what it wrote stays. A box its diagram has not makes
// no film.
func TestACueWritesInABox(t *testing.T) {
	ctx := context.Background()
	system := "flowchart LR\n  shop --> parcels\n  parcels --> parcels-db[(parcels-db)]\n  parcels-db -.-> reports\n"
	src := strings.Replace(holdFeature, "# demo: [2s] The depot's", "# demo: [mark reports ✅ ✓ sent] [2s] The depot's", 1)
	var st Settings
	st.Diagrams = Diagrams{Names: []string{"system"}, Files: map[string]string{"system": "system.mmd"}}
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
	sel := tl.Segments[3]
	if len(tl.Marks) != 1 || tl.Marks[0].Box != "reports" || tl.Marks[0].Text != "✅ ✓ sent" || tl.Marks[0].At != sel.End {
		t.Fatalf("the mark is there when its step is over: %+v, the step ends at %v", tl.Marks, sel.End)
	}
	diagrams := []Diagram{{Name: "system", Source: system}}
	s, err := openStudio(ctx, t.TempDir(), st, &Staged{Take: take, Timeline: tl, Diagrams: diagrams})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if got := shown(t, s, sel.Start+0.5); strings.Contains(got["diagram"], "sent") {
		t.Errorf("while its step plays, the box has nothing yet:\n%s", got["diagram"])
	}
	if got := shown(t, s, tl.Duration); !strings.Contains(got["diagram"], "✅") || !strings.Contains(got["diagram"], "✓ sent") {
		t.Errorf("after its step, to the end:\n%s", got["diagram"])
	}
	// An emoji takes two of the grid's cells: the lines of a box with one in
	// it are as long as the others'.
	width, err := s.page.Evaluate(`() => { const r = document.createRange(), pre = document.querySelector('.surface-diagram .grid'); const lines = pre.innerText.split('\n'); const w = (text) => { const span = document.createElement('span'); span.style.cssText = 'font:inherit;white-space:pre;visibility:hidden;position:absolute'; span.innerHTML = text; pre.appendChild(span); const v = span.getBoundingClientRect().width; span.remove(); return v; }; const html = pre.innerHTML.split('\n'); const i = html.findIndex((l) => l.includes('wide')); return [w(html[i]), w(html[i - 1])]; }`)
	if err != nil {
		t.Fatal(err)
	}
	if w, ok := width.([]any); !ok || len(w) != 2 || w[0] != w[1] {
		t.Errorf("the line with an emoji is %v wide, and the one above it %v", w[0], w[1])
	}
	bad := strings.Replace(src, "[mark reports", "[mark warehouse", 1)
	script, _ = ParseScript("features/hold.demo.feature", []byte(bad), st.known())
	if _, err := openStudio(ctx, t.TempDir(), st, &Staged{Take: take, Timeline: Cut(take, script, DefaultPace), Diagrams: diagrams}); err == nil || !strings.Contains(err.Error(), "the diagram system has no box warehouse") {
		t.Errorf("a mark in a box there is not: %v", err)
	}
}

// The terminal, a module of the pack's: a request is the curl command that
// makes it, typed as the step plays, and what came back is printed when the
// film shows it, so a held response stays out of it too.
func TestTheTerminalTypesTheSteps(t *testing.T) {
	src := strings.Replace(holdFeature, "# demo: [action] Ms Okafor", "# demo: [action] [show terminal + diagram] [hold response] Ms Okafor", 1)
	script, err := ParseScript("features/hold.demo.feature", []byte(src), Settings{}.known())
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	take := holdTake()
	tl := Cut(take, script, DefaultPace)
	var put, sel Segment
	for _, seg := range tl.Segments {
		if seg.Kind == "step" && seg.Step == 2 {
			put = seg
		}
		if seg.Kind == "step" && seg.Step == 3 {
			sel = seg
		}
	}
	s, err := openStudio(context.Background(), t.TempDir(), Settings{}, &Staged{Take: take, Timeline: tl})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	command := "$ curl -X PUT 'http://localhost:8080/api/parcels/PX-1/hold' \\\n    -H 'Content-Type: application/json' \\\n    -d '{\"until\":\"2026-11-27T18:00:00Z\",\"reason\":\"Recipient is away\"}'"
	// As the step begins, the command is being typed.
	if got := shown(t, s, put.Start+0.02*(put.End-put.Start)); !strings.HasPrefix(got["terminal"], "$ curl") || strings.Contains(got["terminal"], "hold'") || got["diagram"] == "" {
		t.Errorf("the command, as it is typed, beside the diagram: %v", got)
	}
	// Typed, and what came back held.
	if got := shown(t, s, put.Start+0.45*(put.End-put.Start)); !strings.Contains(got["terminal"], command) || strings.Contains(got["terminal"], "HTTP/1.1") {
		t.Errorf("the command, and no answer yet:\n%s", got["terminal"])
	}
	// The call the system made to a mock is a remark.
	if got := shown(t, s, put.Start+0.95*(put.End-put.Start)); !strings.Contains(got["terminal"], "# address: GET /v1/postcodes/DE/10115") || strings.Contains(got["terminal"], "HTTP/1.1") {
		t.Errorf("what the system called, and still no answer:\n%s", got["terminal"])
	}
	// With the step after it, what came back is printed, and what the
	// scenario looked at is a remark with what was there.
	got := shown(t, s, sel.Start+0.34*(sel.End-sel.Start))
	for _, want := range []string{command + "\nHTTP/1.1 200 OK\n{\n  \"reference\": \"PX-1\",", "# parcels-db: select parcels.holds where parcel_ref = PX-1"} {
		if !strings.Contains(got["terminal"], want) {
			t.Errorf("with the step after it, the terminal has no %q:\n%s", want, got["terminal"])
		}
	}
	if strings.Contains(got["terminal"], "1 row") {
		t.Errorf("what the selection found, before the film shows it:\n%s", got["terminal"])
	}
	// A terminal scrolls: its last lines are the ones it shows.
	got = shown(t, s, sel.Start+0.9*(sel.End-sel.Start))
	if !strings.Contains(got["terminal"], "1 row\n[\n  {\n    \"parcel_ref\": \"PX-1\",") || strings.HasPrefix(got["terminal"], "$ curl") {
		t.Errorf("what the selection found, at the terminal's end:\n%s", got["terminal"])
	}
}

// A project's own surface: a name in packs.demo.surfaces, and a scene one of
// its scripts registers under it, which [show ...] puts on the stage, alone or
// beside another. A surface no scene draws makes no film.
func TestAProjectsOwnSurface(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	scene := `axxStage.scene('ledger', {
	mount(el) { el.innerHTML = '<p class="rows"></p>'; },
	render(el, state) { el.querySelector('.rows').textContent = 'ledger: ' + (state.now.sent ? state.now.sent.title : '') + ' / ' + (state.now.seen ? state.now.seen.title : ''); },
});`
	if err := os.WriteFile(filepath.Join(dir, "demo", "ledger.js"), []byte(scene), 0o644); err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(holdFeature, "# demo: [2s] The depot's", "# demo: [show ledger + diagram] [2s] The depot's", 1)
	st := Settings{Surfaces: []string{"ledger"}, Scenes: []string{"demo/ledger.js"}}
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
	sel := tl.Segments[3]
	if got := shown(t, s, tl.Segments[1].Start+0.5); got["ledger"] != "" || got["diagram"] == "" {
		t.Errorf("before the cue, the diagram alone: %v", got)
	}
	if got := shown(t, s, sel.Start+0.1*(sel.End-sel.Start)); got["ledger"] != "ledger: select parcels.holds where parcel_ref = PX-1 /" || got["diagram"] == "" {
		t.Errorf("the project's surface beside the diagram, as the step asks: %v", got)
	}
	if got := shown(t, s, sel.Start+0.9*(sel.End-sel.Start)); got["ledger"] != "ledger: select parcels.holds where parcel_ref = PX-1 / 1 row" {
		t.Errorf("and with what the step found: %v", got)
	}
	// Named in axx.yaml, and drawn by no scene.
	st.Scenes = nil
	if _, err := openStudio(context.Background(), dir, st, &Staged{Take: take, Timeline: tl}); err == nil || !strings.Contains(err.Error(), "[show ledger]: no scene draws ledger") {
		t.Errorf("a surface no scene draws: %v", err)
	}
}

// A pack that builds on this one adds a module: its files, served to the
// stage, with the surfaces and the directions cues can then name.
func TestAPacksModule(t *testing.T) {
	RegisterModule(Module{
		Name: "probe", Surfaces: []string{"probe"}, Directions: []string{"flash"},
		Files: fstest.MapFS{
			"probe.js": {Data: []byte(`axxStage.scene('probe', {
	mount(el) { el.innerHTML = '<p class="probe"></p>'; },
	render(el, state) { el.querySelector('.probe').textContent = 'probe: ' + (state.step ? state.step.text : ''); },
});
window.saidDirections = [];
axxStage.on('direction', (d) => window.saidDirections.push(d.name + ' ' + d.args.join(' ')));`)},
			"probe.css": {Data: []byte(`.probe { color: rgb(4, 5, 6); }`)},
		},
		Scripts: []string{"probe.js"}, Styles: []string{"probe.css"},
	})
	t.Cleanup(func() {
		modulesMu.Lock()
		delete(modules, "probe")
		modulesMu.Unlock()
	})
	src := strings.Replace(holdFeature, "# demo: [2s] The depot's", "# demo: [show probe] [flash red] [2s] The depot's", 1)
	script, err := ParseScript("features/hold.demo.feature", []byte(src), Settings{}.known())
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	take := holdTake()
	tl := Cut(take, script, DefaultPace)
	s, err := openStudio(context.Background(), t.TempDir(), Settings{}, &Staged{Take: take, Timeline: tl})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if got := shown(t, s, tl.Segments[3].Start+0.5); !strings.HasPrefix(got["probe"], "probe: a selection of rows") || got["diagram"] != "" || got["said"] != "flash red" {
		t.Errorf("the module's surface and direction: %v", got)
	}
	color, err := s.page.Evaluate(`() => getComputedStyle(document.querySelector('.probe')).color`)
	if err != nil || color != "rgb(4, 5, 6)" {
		t.Errorf("the module's stylesheet: %v, %v", color, err)
	}
}

// Before an audience the step after a held response has not run yet: what
// came back stays held until the presenter goes on to it.
func TestAResponseIsHeldBeforeAnAudience(t *testing.T) {
	src := strings.Replace(holdFeature, "# demo: [2s] The depot's", "# demo: [hold response] The depot's", 1)
	script, err := ParseScript("features/hold.demo.feature", []byte(src), Known{})
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	// The run as it stands after the selection: the check has not run.
	take := holdTake()
	take.Steps[4].Status = ""
	pace := DefaultPace
	pace.Live, pace.Card, pace.Entered = true, 0.6, 3
	tl := Cut(take, script, pace)
	if last := tl.Segments[len(tl.Segments)-1]; last.Step != 3 || !last.HoldResponse {
		t.Fatalf("the show is at the selection: %+v", tl.Segments)
	}
	live := &Live{Mode: "live", Cursor: 2, Entered: 3, Waiting: true, Beats: []string{}}
	s, err := openStudio(context.Background(), t.TempDir(), Settings{}, &Staged{Take: take, Timeline: tl, Live: live})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if got := shown(t, s, tl.Duration); !strings.Contains(got["sent"], "select parcels.holds") || got["seen"] != "" || strings.Contains(got["diagram"], "1 row") {
		t.Errorf("at the selection's end, before the presenter goes on: %v", got)
	}
}
