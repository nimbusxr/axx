package demo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

const holdFeature = `# demo: [card 3s] Holding a parcel at the depot
Feature: Hold a parcel

  # a note of the author's own
  Scenario: A shop holds a parcel
    # demo: [cut]
    Given the parcels service with the following properties:
      | property | value                 |
      | url      | http://localhost:8080 |
    And a parcels database with the following properties:
      | property | value                             |
      | url      | postgres://localhost:5432/parcels |

    # demo: [action] Ms Okafor is away until Friday.
    #       Her shop asks us to hold her parcel.
    #  not a part of it
    When a PUT request to /api/parcels/PX-1/hold
    # demo: [2s] The depot's database has the hold at once.
    Then a selection of rows is retrieved from the parcels.holds table where:
      | column     | value |
      | parcel_ref | PX-1  |
    # demo: [speed 4x] [pause 2s] And nothing else changed.
    And the response status is 200
    # demo: That is the whole of it.
`

func TestScript(t *testing.T) {
	s, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), Known{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Problems) != 0 {
		t.Fatalf("problems: %+v", s.Problems)
	}
	type want struct {
		anchor int
		dirs   string
		text   string
	}
	wants := []want{
		{2, "card 3s", "Holding a parcel at the depot"},
		{7, "cut", ""},
		{17, "action", "Ms Okafor is away until Friday. Her shop asks us to hold her parcel."},
		{19, "lasts 2s", "The depot's database has the hold at once."},
		{23, "speed 4x|pause 2s", "And nothing else changed."},
		{0, "", "That is the whole of it."},
	}
	if len(s.Cues) != len(wants) {
		t.Fatalf("%d cues, want %d: %+v", len(s.Cues), len(wants), s.Cues)
	}
	for i, w := range wants {
		c := s.Cues[i]
		var dirs []string
		for _, d := range c.Directions {
			dirs = append(dirs, strings.TrimSpace(d.Name+" "+strings.Join(d.Args, " ")))
		}
		if c.Anchor != w.anchor || strings.Join(dirs, "|") != w.dirs || c.Text != w.text {
			t.Errorf("cue %d: anchor %d, directions %q, text %q; want %d, %q, %q", i, c.Anchor, strings.Join(dirs, "|"), c.Text, w.anchor, w.dirs, w.text)
		}
	}
}

func TestScriptProblems(t *testing.T) {
	src := `Feature: F
  Scenario: S
    # demo: [sped 2x] Faster.
    # demo: [speed fast] Faster.
    # demo: [show tv] A screen.
    # demo: [confetti] Ours.
    # demo: [pause 2s
    Given something
`
	s, err := ParseScript("f.demo.feature", []byte(src), Known{Directions: []string{"confetti"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range s.Problems {
		got = append(got, p.Message)
	}
	if len(got) != 4 {
		t.Fatalf("%d problems, want 4:\n%s", len(got), strings.Join(got, "\n"))
	}
	for i, part := range []string{`did you mean [speed]?`, `"fast" is no speed`, `show does not know "tv"`, "not closed"} {
		if !strings.Contains(got[i], part) {
			t.Errorf("problem %d is %q, want it to say %q", i, got[i], part)
		}
	}
	if len(s.Cues) != 5 || len(s.Cues[3].Directions) != 1 || s.Cues[3].Directions[0].Name != "confetti" {
		t.Errorf("the project's own direction was not kept: %+v", s.Cues)
	}
}

// holdTake is the take of the scenario of holdFeature, as a run would leave it.
func holdTake() *Take {
	sent := func(title, body string) *core.Message {
		return &core.Message{Title: title, MediaType: "application/json", Body: body}
	}
	return &Take{
		Version: TakeVersion, Feature: "features/hold.demo.feature", Scenario: "A shop holds a parcel", Line: 5, Passed: true,
		Peers: []Peer{{"parcels", "service"}, {"parcels-db", "database"}, {"address", "mock"}},
		Steps: []TakeStep{
			{
				Keyword: "Given", Text: "the parcels service with the following properties:", Line: 7, Pack: "rest", Status: "passed", End: 0.001,
				Exchanges: []core.Exchange{{Peer: "parcels", Kind: "service"}},
			},
			{
				Keyword: "And", Text: "a parcels database with the following properties:", Line: 10, Pack: "sql", Status: "passed", Start: 0.001, End: 0.01,
				Exchanges: []core.Exchange{{Peer: "parcels-db", Kind: "database"}, {Peer: "address", Kind: "mock"}},
			},
			{
				Keyword: "When", Text: "a PUT request to /api/parcels/PX-1/hold", Line: 17, Pack: "rest", Status: "passed", Start: 0.01, End: 0.05,
				Exchanges: []core.Exchange{
					{
						Peer: "parcels", Kind: "service", Address: "http://localhost:8080",
						Sent: sent("PUT /api/parcels/PX-1/hold", `{"until":"2026-11-27T18:00:00Z","reason":"Recipient is away"}`),
						Seen: sent("200 OK", `{"reference":"PX-1","status":"AT_DEPOT","hold":{"until":"2026-11-27T18:00:00Z","reason":"Recipient is away","placedAt":"2026-11-24T10:15:00Z"}}`),
					},
					{Peer: "address", Kind: "mock", From: core.FromSystem, Sent: &core.Message{Title: "GET /v1/postcodes/DE/10115"}},
				},
			},
			{
				Keyword: "Then", Text: "a selection of rows is retrieved from the parcels.holds table where:", Line: 19, Pack: "sql", Status: "passed", Start: 0.05, End: 0.06,
				Exchanges: []core.Exchange{{
					Peer: "parcels-db", Kind: "database",
					Sent: &core.Message{Title: "select parcels.holds where parcel_ref = PX-1"},
					Seen: sent("1 row", `[{"parcel_ref":"PX-1","until":"2026-11-27T18:00:00Z","reason":"Recipient is away"}]`),
				}},
			},
			{Keyword: "And", Text: "the response status is 200", Line: 23, Pack: "rest", Status: "passed", Start: 0.06, End: 0.061},
		},
	}
}

func TestCut(t *testing.T) {
	s, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), Known{})
	if err != nil {
		t.Fatal(err)
	}
	pace := DefaultPace
	tl := Cut(holdTake(), s, pace)
	var got []string
	for _, seg := range tl.Segments {
		got = append(got, seg.Kind)
	}
	// The card; the two steps under [cut] show nothing; the request, which
	// sent two things; a hold for its subtitle to be read; the selection, for
	// the 2 seconds its cue says; the check, four times as fast, with its
	// pause; the rest of its subtitle's time; the last subtitle; the end.
	want := "card step hold step hold step hold hold hold hold"
	if strings.Join(got, " ") != want {
		t.Fatalf("segments %q, want %q\n%+v", strings.Join(got, " "), want, tl.Segments)
	}
	seg := tl.Segments
	if seg[0].End != 3 || seg[0].Text != "Holding a parcel at the depot" {
		t.Errorf("the card: %+v", seg[0])
	}
	if d := seg[1].End - seg[1].Start; seg[1].Step != 2 || d < 2*pace.Exchange-0.001 || d > 2*pace.Exchange+0.001 {
		t.Errorf("the request: %+v", seg[1])
	}
	if d := seg[3].End - seg[3].Start; seg[3].Step != 3 || d < 1.999 || d > 2.001 {
		t.Errorf("the selection lasts %v, want the 2 seconds of its cue: %+v", d, seg[3])
	}
	if d := seg[5].End - seg[5].Start; d < pace.Check/4-0.001 || d > pace.Check/4+0.001 {
		t.Errorf("the check lasts %v, want a quarter of %v", d, pace.Check)
	}
	if d := seg[6].End - seg[6].Start; d < 1.999 || d > 2.001 {
		t.Errorf("the pause lasts %v, want 2 seconds", d)
	}
	if len(tl.Subtitles) != 4 {
		t.Fatalf("%d subtitles, want 4: %+v", len(tl.Subtitles), tl.Subtitles)
	}
	for i, sub := range tl.Subtitles {
		if least := readingTime(sub.Text, pace); sub.End-sub.Start < least-0.001 {
			t.Errorf("subtitle %d shows for %v, and takes %v to read", i, sub.End-sub.Start, least)
		}
		if i > 0 && sub.Start < tl.Subtitles[i-1].End {
			t.Errorf("subtitle %d starts before the one before it ends", i)
		}
	}
	if tl.Subtitles[0].Start != 3 {
		t.Errorf("the first subtitle starts at %v, want after the card", tl.Subtitles[0].Start)
	}
	if len(tl.Beats) != 5 {
		t.Errorf("%d beats, want the card and four subtitles: %v", len(tl.Beats), tl.Beats)
	}
	if last := seg[len(seg)-1]; tl.Duration != last.End {
		t.Errorf("the film is %v long and its last segment ends at %v", tl.Duration, last.End)
	}
}

func TestCutWithoutCues(t *testing.T) {
	tl := Cut(holdTake(), nil, DefaultPace)
	steps := 0
	for _, seg := range tl.Segments {
		if seg.Kind == "step" {
			steps++
		}
	}
	if steps != 5 || len(tl.Subtitles) != 0 || tl.Duration <= 0 {
		t.Errorf("a take with no cues plays every step: %d steps, %d subtitles, %v long", steps, len(tl.Subtitles), tl.Duration)
	}
}

// A step on a screen shows the screen, for at least the time a person needs
// to see what it did, up to where the screen last changed before the next
// step began; a step with no screen after it shows the diagram.
func TestCutOfScreens(t *testing.T) {
	take := &Take{
		Version: TakeVersion, Passed: true,
		Steps: []TakeStep{
			{Keyword: "When", Text: "the field is filled", Line: 3, Pack: "web-core", Status: "passed", Start: 1, End: 1.04},
			{Keyword: "And", Text: "the page is opened", Line: 4, Pack: "web-core", Status: "passed", Start: 1.5, End: 4},
			{Keyword: "Then", Text: "the rows are selected", Line: 5, Pack: "sql", Status: "passed", Start: 4.2, End: 4.3},
		},
		Footage: []Shot{
			{Surface: "browser", File: "a.jpg", At: 1.02},
			{Surface: "browser", File: "b.jpg", At: 1.3},
			{Surface: "browser", File: "c.jpg", At: 2},
			{Surface: "browser", File: "d.jpg", At: 3.6},
		},
	}
	pace := DefaultPace
	tl := Cut(take, nil, pace)
	seg := tl.Segments
	if len(seg) != 4 {
		t.Fatalf("%d segments: %+v", len(seg), seg)
	}
	if got := seg[0]; got.Show[0] != "browser" || got.From != 1 || got.To != 1.3 || got.End-got.Start != pace.Screen {
		t.Errorf("a quick step on a screen lasts %v, to the picture taken before the next step: %+v", pace.Screen, got)
	}
	if got := seg[1]; got.From != 1.5 || got.To != 3.6 || got.End-got.Start < 2.099 || got.End-got.Start > 2.101 {
		t.Errorf("a slow step on a screen lasts as it took, to its screen's last change: %+v", got)
	}
	if got := seg[2]; got.Show[0] != "diagram" {
		t.Errorf("a step with no screen shows the diagram: %+v", got)
	}
}

// The rows of a Scenario Outline are one film: a cue's <name> is its row's
// value, and what belongs to the Feature plays once, with the first row.
func TestCutOfAnOutlinesRows(t *testing.T) {
	src := `# demo: [card 3s] Prices
Feature: Prices

  Scenario Outline: A parcel of <grams> grams
    # demo: A parcel of <grams> grams costs <price> cents.
    When the price is asked

    Examples:
      | grams | price |
      | 500   | 490   |
      | 7500  | 890   |
`
	script, err := ParseScript("prices.demo.feature", []byte(src), Known{})
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	for row, want := range []struct {
		line  int
		cards int
		text  string
	}{{10, 1, "A parcel of 500 grams costs 490 cents."}, {11, 0, "A parcel of 7500 grams costs 890 cents."}} {
		take := &Take{Version: TakeVersion, Line: want.line, Passed: true, Steps: []TakeStep{{Keyword: "When", Text: "the price is asked", Line: 6, Status: "passed"}}}
		pace := DefaultPace
		pace.Row = row
		tl := Cut(take, script, pace)
		cards := 0
		for _, seg := range tl.Segments {
			if seg.Kind == "card" {
				cards++
			}
		}
		if cards != want.cards || len(tl.Subtitles) != 1 || tl.Subtitles[0].Text != want.text {
			t.Errorf("row %d: %d cards, subtitles %+v; want %d and %q", row, cards, tl.Subtitles, want.cards, want.text)
		}
	}
}

// A mistake in a cue is reported where it is, for lint, validate and editors.
func TestCuesCheck(t *testing.T) {
	dir := t.TempDir()
	src := "Feature: F\n\n  Scenario: S\n    # demo: [sped 2x] Faster.\n    Given something\n"
	if err := os.WriteFile(filepath.Join(dir, "f.demo.feature"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.feature"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	found := cuesCheck().Run(&core.FeatureCheckCall{
		Scenarios:  []core.ScenarioInfo{{URI: "f.demo.feature", Line: 3}, {URI: "plain.feature", Line: 3}},
		Settings:   func(any) error { return nil },
		ProjectDir: dir,
	})
	if len(found) != 1 || found[0].URI != "f.demo.feature" || found[0].At != 4 || found[0].Column != 13 || !strings.Contains(found[0].Message, "did you mean [speed]?") {
		t.Errorf("findings: %+v", found)
	}
}

// A cue goes to another of the project's diagrams, says who the scenario is
// on it, and lights its lines; the timeline carries all three to the stage.
func TestCuesOfTheDiagram(t *testing.T) {
	src := `Feature: F

  Scenario: S
    # demo: [as shop] [shop --> portal --> parcels] The shop registers a parcel.
    When the request is executed
    # demo: [diagram billing] [parcel-events -> billing] Billing hears of it.
    Then the event is consumed
    # demo: [diagram payments] There is none of that name.
    And nothing else
    # demo: [diagram system + aaa] [hold response] Two of them, side by side.
    And both are shown
`
	var st Settings
	if err := json.Unmarshal([]byte(`{"diagrams": {"system": "docs/system.mmd", "billing": "docs/billing.mmd", "aaa": "docs/a.mmd"}}`), &st); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(st.Diagrams.Names, ","); got != "system,billing,aaa" || st.Diagrams.Files["billing"] != "docs/billing.mmd" {
		t.Fatalf("the diagrams, in the order they are listed: %s, %v", got, st.Diagrams.Files)
	}
	script, err := ParseScript("f.demo.feature", []byte(src), st.known())
	if err != nil {
		t.Fatal(err)
	}
	if len(script.Problems) != 1 || !strings.Contains(script.Problems[0].Message, `no diagram "payments": packs.demo.diagrams has system, billing, aaa`) {
		t.Fatalf("problems: %+v", script.Problems)
	}
	take := &Take{Version: TakeVersion, Passed: true, Steps: []TakeStep{
		{Keyword: "When", Text: "the request is executed", Line: 5, Status: "passed"},
		{Keyword: "Then", Text: "the event is consumed", Line: 7, Status: "passed"},
		{Keyword: "And", Text: "nothing else", Line: 9, Status: "passed"},
		{Keyword: "And", Text: "both are shown", Line: 11, Status: "passed"},
	}}
	tl := Cut(take, script, DefaultPace)
	var steps []Segment
	for _, seg := range tl.Segments {
		if seg.Kind == "step" {
			steps = append(steps, seg)
		}
	}
	if len(steps) != 4 {
		t.Fatalf("segments: %+v", tl.Segments)
	}
	if s := steps[3]; strings.Join(s.Diagrams, "+") != "system+aaa" || !s.HoldResponse || steps[2].HoldResponse {
		t.Errorf("two diagrams side by side: %+v", s)
	}
	if s := steps[0]; s.As != "shop" || len(s.Diagrams) != 0 || len(s.Flows) != 2 || s.Flows[0] != [2]string{"shop", "portal"} || s.Flows[1] != [2]string{"portal", "parcels"} {
		t.Errorf("the first step: %+v", s)
	}
	// Who the scenario is on one diagram, it is not on the next.
	if s := steps[1]; s.As != "" || strings.Join(s.Diagrams, "+") != "billing" || len(s.Flows) != 1 || s.Flows[0] != [2]string{"parcel-events", "billing"} {
		t.Errorf("the second step: %+v", s)
	}
	if s := steps[2]; strings.Join(s.Diagrams, "+") != "billing" || len(s.Flows) != 0 {
		t.Errorf("the third step keeps the diagram and lights nothing: %+v", s)
	}
}

// A film is cut again after its file was edited: a take has the lines its
// steps had when it ran, and a cue still belongs to the step it is written
// above, though lines were added over it. A file whose scenario has other
// steps than the take is not the take's any more.
func TestCutAfterTheFilesLinesMoved(t *testing.T) {
	take := &Take{Version: TakeVersion, Scenario: "S", Line: 3, Passed: true, Steps: []TakeStep{
		{Keyword: "Given", Text: "the service", Line: 4, Status: "passed"},
		{Keyword: "When", Text: "the request is executed", Line: 5, Status: "passed"},
		{Keyword: "Then", Text: "the answer is checked", Line: 6, Status: "passed"},
	}}
	// The file as it is now: an introduction and a cue were added above.
	src := `Feature: F

  # demo: An introduction, two lines long,
  #       which moved everything below it.
  Scenario: S
    # demo: [cut]
    Given the service
    # demo: [action] The request.
    When the request is executed
    # demo: The check.
    Then the answer is checked
`
	script, err := ParseScript("f.demo.feature", []byte(src), Known{})
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	lines, _, ok := script.Placed(take, 0)
	if !ok || len(lines) != 3 || lines[0] != 7 || lines[2] != 11 {
		t.Fatalf("the take's steps in the file as it is: %v, %v", lines, ok)
	}
	tl := Cut(take, script, DefaultPace)
	var steps []int
	for _, seg := range tl.Segments {
		if seg.Kind == "step" {
			steps = append(steps, seg.Step)
		}
	}
	if len(steps) != 2 || steps[0] != 1 || steps[1] != 2 {
		t.Errorf("the steps on camera: %v, want the request and the check: %+v", steps, tl.Segments)
	}
	if len(tl.Subtitles) != 3 || tl.Subtitles[1].Text != "The request." || tl.Subtitles[1].Start != tl.Segments[1].Start {
		t.Errorf("the request's subtitle starts with its step: %+v\n%+v", tl.Subtitles, tl.Segments)
	}
	// The introduction shows before anything happens: the picture holds.
	if first := tl.Segments[0]; first.Kind != "hold" || first.Step != 0 || tl.Subtitles[0].End != first.End {
		t.Errorf("the introduction: %+v, %+v", first, tl.Subtitles[0])
	}
	// A scenario of other steps is not the take's.
	other, _ := ParseScript("f.demo.feature", []byte("Feature: F\n\n  Scenario: S\n    Given the service\n"), Known{})
	if _, _, ok := other.Placed(take, 0); ok {
		t.Error("a scenario of one step was taken for the take's, of three")
	}
}

// What a cue shows is a surface the pack has, one of a module's (the pack's
// terminal is one) or one of the project's own.
func TestShowNamesASurface(t *testing.T) {
	for src, want := range map[string]string{
		"[show terminal]":           "",
		"[show terminal + diagram]": "",
		"[show ledger]":             "",
		"[show ledger + browser]":   "",
		"[show nothing]":            `show does not know "nothing": it shows diagram, browser, phone, desktop, ledger, terminal, or auto; a project adds its own in packs.demo.surfaces`,
		"[show a + b + c]":          "show takes what to show",
	} {
		feature := strings.Replace(holdFeature, "# demo: [2s] The depot's", "# demo: "+src+" The depot's", 1)
		s, err := ParseScript("features/hold.demo.feature", []byte(feature), Settings{Surfaces: []string{"ledger"}}.known())
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if len(s.Problems) > 0 {
			got = s.Problems[0].Message
		}
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s: %q, want %q", src, got, want)
		}
	}
}

// The steps of apps are the same on a phone and on a desktop (app-core): they
// happen on the screen that is filmed, so a film shows the phone at them with
// nothing said, and they last as long as they took on it.
func TestAnAppsStepsAreOnTheScreenThatIsFilmed(t *testing.T) {
	for _, c := range []struct {
		pack   string
		filmed []string
		want   string
	}{
		{"app-core", []string{"phone"}, "phone"},
		{"app-core", []string{"desktop"}, "desktop"},
		{"app-core", []string{"browser", "desktop", "phone"}, "phone"},
		{"app-core", nil, ""},
		{"mobile-ios", nil, "phone"},
		{"web-core", []string{"phone"}, "browser"},
		{"rest", []string{"phone"}, ""},
	} {
		if got := surfaceOf(c.pack, c.filmed); got != c.want {
			t.Errorf("surfaceOf(%q, %v) = %q, want %q", c.pack, c.filmed, got, c.want)
		}
	}
	take := &Take{
		Version: TakeVersion, Line: 3, Passed: true,
		Steps: []TakeStep{
			{Keyword: "When", Text: `the "Sign in" button is tapped in the courier app`, Line: 4, Pack: "app-core", Status: "passed", Start: 1, End: 1.2},
			{Keyword: "Then", Text: `the courier app shows "Hello"`, Line: 5, Pack: "app-core", Status: "passed", Start: 6, End: 6.1},
		},
		Footage: []Shot{{Surface: "phone", File: "a.jpg", At: 0.5}, {Surface: "phone", File: "b.jpg", At: 4.5}},
	}
	tl := Cut(take, nil, DefaultPace)
	first := tl.Segments[0]
	if len(first.Show) != 1 || first.Show[0] != "phone" {
		t.Errorf("the step shows %v, want the phone", first.Show)
	}
	// The tap's doing showed until the next step began: the film plays it.
	if first.From != 1 || first.To != 4.5 || first.End-first.Start != 3.5 {
		t.Errorf("the tap plays from %v to %v, in %v seconds; want 1 to 4.5, in 3.5", first.From, first.To, first.End-first.Start)
	}
}
