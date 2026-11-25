package demo

import (
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
	s, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), nil)
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
	s, err := ParseScript("f.demo.feature", []byte(src), []string{"confetti"})
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
						Peer: "parcels", Kind: "service",
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
	s, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), nil)
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
	if seg[1].Step != 2 || seg[1].End-seg[1].Start != 2*pace.Exchange {
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
	script, err := ParseScript("prices.demo.feature", []byte(src), nil)
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
