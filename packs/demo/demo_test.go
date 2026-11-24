package demo

import (
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
