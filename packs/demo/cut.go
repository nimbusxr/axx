package demo

import (
	"math"
	"slices"
	"strings"
)

// The cut is the video's timeline: what the picture shows at each moment, made
// from a take and the script's cues. The video's time is its own. A step that
// took the system 20 milliseconds gets the time a person needs to see what it
// did; a subtitle stays as long as it takes to read; and the directions
// change both: how long a step lasts, how fast it plays, where the camera
// stops and starts.

// Timeline is a video's cut.
type Timeline struct {
	// Duration is the video's length, in seconds.
	Duration  float64    `json:"duration"`
	Segments  []Segment  `json:"segments"`
	Subtitles []Subtitle `json:"subtitles"`
	// Beats are where a presenter stops: the start of each subtitle and
	// each card.
	Beats []float64 `json:"beats"`
	// Marks are what cues write in the diagram's boxes, each from its
	// moment on.
	Marks []Mark `json:"marks,omitempty"`
}

// Mark is something a cue writes in a box of the diagram ([mark billing ✓]),
// from the moment the cue's step is over.
type Mark struct {
	At   float64 `json:"at"`
	Box  string  `json:"box"`
	Text string  `json:"text"`
}

// Segment is a stretch of the video.
type Segment struct {
	// Kind is "step" (a step plays), "hold" (the picture stays as the step
	// left it) or "card" (a card fills the picture).
	Kind  string  `json:"kind"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	// Step is the take's step that plays or is held: -1 on a card before
	// the first.
	Step int `json:"step"`
	// From and To are the stretch of the run the segment shows, in the
	// take's seconds: a step's own time, less what of it nothing happened
	// in on a screen; a hold's and a card's are the moment they stay at.
	From float64 `json:"from"`
	To   float64 `json:"to"`
	// Text is a card's.
	Text string `json:"text,omitempty"`
	// Show is what the picture shows: one surface, or two side by side.
	Show []string `json:"show,omitempty"`
	// Diagrams are the project's diagrams the picture shows, by name: one,
	// or two side by side; none is the first of the project's. As is who
	// the scenario is in them. Flows are the lines a cue lights while the
	// step plays: each a pair of boxes.
	Diagrams []string    `json:"diagrams,omitempty"`
	As       string      `json:"as,omitempty"`
	Flows    [][2]string `json:"flows,omitempty"`
	// HoldResponse is whether the step shows only what it sent, what came
	// back being shown with the step after it.
	HoldResponse bool `json:"holdResponse,omitempty"`
	// Directions are the project's own directions on the step's cues, which
	// its stage acts on.
	Directions []Direction `json:"directions,omitempty"`
}

// Subtitle is a subtitle and when it shows.
type Subtitle struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Pace says how long things last in a video that no direction speaks of.
type Pace struct {
	// Exchange is how long a step lasts for each thing it sent or saw, and
	// Most the most a step lasts for them all.
	Exchange, Most float64
	// Meet is a step that only names what the scenario talks to, Check any
	// other step with nothing to send, and Failed a step that failed.
	Meet, Check, Failed float64
	// Card is a card that says no time.
	Card float64
	// Read is how long a subtitle of no words stays, and Word what each of
	// its words adds; End is how long the last picture stays.
	Read, Word, End float64
	// Screen is the least a step on a screen lasts: what it did stays that
	// long to be seen.
	Screen float64
	// Live cuts a show before an audience, whose time is the presenter's:
	// the directions that set a video's time do nothing, nothing is held to
	// be read, and the timeline goes as far as the run has come. Entered is
	// how many beats (cards and subtitles) the presenter has gone on to; a
	// timeline made again as the run goes on only grows.
	Live    bool
	Entered int
	// Row is which row of a Scenario Outline the take is, from 0, in a video
	// of them all.
	Row int
}

// DefaultPace is the pace of a video whose project sets none: slow enough to
// follow for someone who sees the system for the first time. What a step
// sent has time to be read before its answer comes, and the answer before
// the next step.
var DefaultPace = Pace{Exchange: 3.4, Most: 8, Meet: 0.9, Check: 1.8, Failed: 4, Card: 3, Read: 2, Word: 0.42, End: 2.5, Screen: 1.8}

// Cut makes the timeline of a take with a script's cues.
func Cut(take *Take, script *Script, pace Pace) *Timeline {
	tl := &Timeline{Segments: []Segment{}, Subtitles: []Subtitle{}, Beats: []float64{}}
	// The cues of each step: a cue belongs to the first step at or below
	// its anchor, and one below the last step to the end.
	cues := make([][]Cue, len(take.Steps)+1)
	if script != nil {
		// Where the take's steps are in the file as it is now: a video is cut
		// again after the file was edited, and its lines moved.
		lines, row, placed := script.Placed(take, pace.Row)
		if !placed {
			lines, row = nil, script.Rows[take.Line]
			for _, st := range take.Steps {
				lines = append(lines, st.Line)
			}
		}
		for _, c := range script.Cues {
			// What belongs to the Feature is the whole video's: of the rows
			// of a Scenario Outline, the first plays it.
			if pace.Row > 0 && c.Anchor == script.FeatureLine {
				continue
			}
			if row != nil {
				c.Text = fill(c.Text, row)
			}
			i := len(take.Steps)
			if c.Anchor != 0 {
				for j, line := range lines {
					if line >= c.Anchor {
						i = j
						break
					}
				}
			}
			cues[i] = append(cues[i], c)
		}
	}
	// Where the camera first comes on, and what the picture is of there:
	// before an audience, an introduction is read over that picture.
	opening := struct {
		step          int
		show, diagram []string
		as            string
	}{step: -1}
	for i, on := 0, true; i < len(take.Steps) && opening.step < 0; i++ {
		for _, c := range cues[i] {
			for _, d := range c.Directions {
				switch d.Name {
				case "cut":
					on = false
				case "action":
					on = true
				case "show":
					if opening.show = showSurfaces(d.Args); len(opening.show) == 1 && opening.show[0] == "auto" {
						opening.show = nil
					}
				case "diagram":
					opening.diagram, opening.as = showSurfaces(d.Args), ""
				case "as":
					opening.as = d.Args[0]
				}
			}
		}
		if on {
			opening.step = i
		}
	}
	t, camera := 0.0, true
	var show []string    // what a direction says to show; nil follows the steps
	var diagram []string // the diagrams a cue went to
	as := ""             // who the scenario is in them
	open := -1           // the subtitle that shows, in tl.Subtitles
	last := -1           // the last step that ran so far
	// over is set where a live show's timeline ends for now: at a beat the
	// presenter has not gone on to, or a step that has not ended.
	over := false
	hold := func(d float64) {
		if d <= 0 || pace.Live {
			return
		}
		// The picture stays as the step left it: a screen's picture of what
		// a step did is taken after the step, up to where the next began.
		at := 0.0
		if last >= 0 {
			at = take.Steps[last].End
			if last+1 < len(take.Steps) && take.Steps[last+1].Start > at {
				at = take.Steps[last+1].Start
			}
		}
		tl.Segments = append(tl.Segments, Segment{Kind: "hold", Start: t, End: t + d, Step: last, From: at, To: at, Show: surfaces(show, take, last), Diagrams: slices.Clone(diagram), As: as})
		t += d
	}
	// closeSubtitle ends the subtitle that shows, once it had its time.
	closeSubtitle := func() {
		if open < 0 {
			return
		}
		s := &tl.Subtitles[open]
		hold(s.Start + readingTime(s.Text, pace) - t)
		s.End = t
		open = -1
	}
	play := func(i int, cs []Cue) {
		lasts, speed, pause := -1.0, 1.0, 0.0
		var cards []Segment
		var own []Direction
		var flows [][2]string
		var marks []Mark
		holdResponse := false
		subtitle := ""
		saidFirst, any := false, false // whether what is said comes before the first card
		for _, c := range cs {
			text := false
			for _, d := range c.Directions {
				switch d.Name {
				case "cut":
					camera = false
				case "action":
					camera = true
				case "show":
					show = showSurfaces(d.Args)
					if len(show) == 1 && show[0] == "auto" {
						show = nil
					}
				case "diagram":
					// Another map: who the scenario is on it is said again.
					diagram, as = showSurfaces(d.Args), ""
				case "as":
					as = d.Args[0]
				case "hold":
					holdResponse = true
				case "mark":
					marks = append(marks, Mark{Box: d.Args[0], Text: strings.Join(d.Args[1:], " ")})
				case "flow":
					for k := 0; k+1 < len(d.Args); k++ {
						flows = append(flows, [2]string{d.Args[k], d.Args[k+1]})
					}
				case "lasts":
					v, _ := parseDuration(d.Args[0])
					lasts = v.Seconds()
				case "speed":
					speed, _ = parseSpeed(d.Args[0])
				case "pause":
					pause = pace.Read
					if len(d.Args) == 1 {
						v, _ := parseDuration(d.Args[0])
						pause = v.Seconds()
					}
				case "card":
					dur := pace.Card
					if len(d.Args) == 1 {
						v, _ := parseDuration(d.Args[0])
						dur = v.Seconds()
					}
					cards = append(cards, Segment{Kind: "card", End: dur, Text: c.Text})
					text = true
				default:
					own = append(own, d)
				}
			}
			if !any && (text || c.Text != "") {
				saidFirst, any = !text, true
			}
			if !text && c.Text != "" {
				subtitle = strings.TrimSpace(subtitle + " " + c.Text)
			}
		}
		playCards := func() bool {
			for _, c := range cards {
				if pace.Live && len(tl.Beats) >= pace.Entered {
					over = true
					return false
				}
				closeSubtitle()
				if pace.Live {
					c.End = pace.Card
				}
				c.Start, c.End, c.Step = t, t+c.End, last
				if last >= 0 {
					c.From, c.To = take.Steps[last].End, take.Steps[last].End
				}
				tl.Beats = append(tl.Beats, t)
				tl.Segments = append(tl.Segments, c)
				t = c.End
			}
			return true
		}
		playSubtitle := func() bool {
			if subtitle != "" {
				if pace.Live && len(tl.Beats) >= pace.Entered {
					over = true
					return false
				}
				closeSubtitle()
				tl.Subtitles = append(tl.Subtitles, Subtitle{Start: t, End: t, Text: subtitle})
				tl.Beats = append(tl.Beats, t)
				open = len(tl.Subtitles) - 1
				if n := len(tl.Segments); pace.Live && (n == 0 || tl.Segments[n-1].Kind == "card") {
					// Before an audience a subtitle shows before its steps ran, over
					// the picture as it stands. Where that is a card, or nothing yet
					// (a demo's introduction), it is the picture its step begins on.
					seg := Segment{Kind: "hold", Start: t, End: t + 0.1, Step: last, Show: surfaces(show, take, last), Diagrams: slices.Clone(diagram), As: as}
					if last >= 0 {
						seg.From = shownUntil(take, last, true)
					}
					// Where the camera has not come on yet and the steps before it
					// ran (a show runs what a demo sets up before its first beat),
					// the picture is the one the camera comes on to.
					if k := opening.step; k > last && k > 0 && take.Steps[k-1].Status == "passed" {
						seg.Step, seg.From = k-1, shownUntil(take, k-1, true)
						seg.Show, seg.Diagrams, seg.As = surfaces(opening.show, take, k), slices.Clone(opening.diagram), opening.as
					}
					seg.To = seg.From
					tl.Segments = append(tl.Segments, seg)
					t += 0.1
				}
			}
			return true
		}
		// The cards of a step come before what is said at it. Below the last
		// step there is no step to wait for: its cues play in the order they
		// are written, so that a demo can end on a card.
		order := []func() bool{playCards, playSubtitle}
		if i >= len(take.Steps) && saidFirst {
			order = []func() bool{playSubtitle, playCards}
		}
		for _, part := range order {
			if !part() {
				return
			}
		}
		if i >= len(take.Steps) {
			return
		}
		st := take.Steps[i]
		// A step that failed before an audience is not shown: the presenter
		// decides what the show does next.
		if pace.Live && st.Status != "passed" {
			over = true
			return
		}
		if pace.Live {
			lasts, speed, pause = -1, 1, 0
		}
		last = i
		shows := surfaces(show, take, i)
		from, to, dur := st.Start, st.End, 0.0
		if camera {
			dur = stepTime(st, pace)
			// A step on a screen lasts as long as it took, to where its
			// screen last changed: the video plays what happened.
			if on := onScreen(shows); len(on) > 0 && surfaceOf(st.Pack, filmedOf(take)) != "" && st.Status == "passed" {
				// What the screen showed until the next step began is this
				// step's doing.
				to = lastChange(take, on, st.Start, shownUntil(take, i, pace.Live))
				dur = max(to-from, pace.Screen)
			}
			dur /= speed
			if lasts >= 0 {
				dur = lasts
			}
		}
		if dur > 0 {
			tl.Segments = append(tl.Segments, Segment{Kind: "step", Start: t, End: t + dur, Step: i, From: from, To: to, Show: shows, Directions: own, Diagrams: slices.Clone(diagram), As: as, Flows: flows, HoldResponse: holdResponse})
			t += dur
		}
		// What the step's cues write in the boxes is there once the step is over.
		for _, m := range marks {
			m.At = t
			tl.Marks = append(tl.Marks, m)
		}
		if camera {
			hold(pause)
		}
	}
	for i := range take.Steps {
		if play(i, cues[i]); over {
			break
		}
	}
	if !over {
		play(len(take.Steps), cues[len(take.Steps)])
	}
	// The last subtitle stays to the end of the video, and the last picture a
	// moment longer. A demo that ends on a card ends on it: the card had its
	// time, and the picture before it does not come back.
	ending := open
	closeSubtitle()
	if n := len(tl.Segments); t > 0 && tl.Segments[n-1].Kind != "card" {
		hold(pace.End)
	}
	if ending >= 0 {
		tl.Subtitles[ending].End = t
	}
	tl.Duration = round(t)
	for i := range tl.Segments {
		tl.Segments[i].Start, tl.Segments[i].End = round(tl.Segments[i].Start), round(tl.Segments[i].End)
	}
	for i := range tl.Subtitles {
		tl.Subtitles[i].Start, tl.Subtitles[i].End = round(tl.Subtitles[i].Start), round(tl.Subtitles[i].End)
	}
	for i := range tl.Beats {
		tl.Beats[i] = round(tl.Beats[i])
	}
	for i := range tl.Marks {
		tl.Marks[i].At = round(tl.Marks[i].At)
	}
	return tl
}

// round keeps a time to the millisecond, so that a timeline reads the same
// wherever it is made.
func round(v float64) float64 { return math.Round(v*1000) / 1000 }

// stepTime is how long a step lasts in a video that says nothing of it.
func stepTime(st TakeStep, pace Pace) float64 {
	if st.Status != "passed" {
		return pace.Failed
	}
	said := 0
	for _, x := range st.Exchanges {
		if x.Sent != nil || x.Seen != nil {
			said++
		}
	}
	switch {
	case said > 0:
		return min(float64(said)*pace.Exchange, pace.Most)
	case len(st.Exchanges) > 0:
		return pace.Meet
	}
	return pace.Check
}

// shownUntil is how long a screen showed what a step did: until the camera had
// its picture, and in a video until the next step began. Before an audience
// the next step begins when the presenter says, and what a screen did while
// it waited (a clock, a cursor) is no part of the step.
func shownUntil(take *Take, i int, live bool) float64 {
	st := take.Steps[i]
	until := max(st.End, st.Settled)
	if !live && i+1 < len(take.Steps) && take.Steps[i+1].Start > until {
		until = take.Steps[i+1].Start
	}
	return until
}

// onScreen are the screens among the surfaces a step shows.
func onScreen(shows []string) []string {
	var out []string
	for _, s := range shows {
		for _, screen := range screens {
			if s == screen {
				out = append(out, s)
			}
		}
	}
	return out
}

// lastChange is when the screens a step shows last changed between two
// moments of the run: where the video leaves the step. With no change it is
// the first of the two.
func lastChange(take *Take, on []string, from, until float64) float64 {
	last := -1.0
	for _, shot := range take.Footage {
		if shot.At >= from && shot.At <= until && slices.Contains(on, shot.Surface) {
			last = shot.At
		}
	}
	if last < 0 {
		return from
	}
	return last
}

// readingTime is how long a subtitle stays at the least.
func readingTime(text string, pace Pace) float64 {
	return pace.Read + float64(len(strings.Fields(text)))*pace.Word
}

// screens are the packs whose steps happen on a screen, and the surface that
// shows it.
var screens = map[string]string{"web": "browser", "mobile": "phone", "desktop": "desktop"}

// surfaces is what the picture shows at a step: what a direction said, or
// what the step's pack works on.
func surfaces(show []string, take *Take, step int) []string {
	if len(show) > 0 {
		return slices.Clone(show)
	}
	if step >= 0 && step < len(take.Steps) {
		if s := surfaceOf(take.Steps[step].Pack, filmedOf(take)); s != "" {
			return []string{s}
		}
	}
	return []string{"diagram"}
}

// surfaceOf is the screen a pack's steps happen on, or "" for a pack with
// none. The steps of apps (app-core) are the same on a phone and on a
// desktop: they happen on the one that is recorded, a phone before a desktop.
func surfaceOf(pack string, filmed []string) string {
	for prefix, surface := range screens {
		if pack == prefix || strings.HasPrefix(pack, prefix+"-") {
			return surface
		}
	}
	if pack == "app" || strings.HasPrefix(pack, "app-") {
		for _, surface := range []string{"phone", "desktop"} {
			if slices.Contains(filmed, surface) {
				return surface
			}
		}
	}
	return ""
}

// filmedOf are the screens a take has pictures of.
func filmedOf(take *Take) []string {
	var out []string
	for _, shot := range take.Footage {
		if !slices.Contains(out, shot.Surface) {
			out = append(out, shot.Surface)
		}
	}
	return out
}
