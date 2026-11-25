package demo

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/nimbusxr/axx/core"
)

// A show is a demo before an audience: the scenario runs as the presenter
// goes on, a beat at a time. A beat is a card, or a subtitle with the steps
// under it. The run waits before each beat for the presenter (the gate); the
// stage plays the take as far as the run has come, and the presenter's place
// in it, the cursor, moves forward and back over the beats that were played:
// back shows again what was shown, and undoes nothing.
type show struct {
	script *Script
	st     Settings
	pace   Pace
	// fallbacks are the last good takes of the demo's scenarios (the rows of
	// a Scenario Outline), in the file's order: what the show plays on from
	// when a step fails and the presenter says so, and what a recorded show
	// plays. rec is the one that plays.
	fallbacks []*Take
	rec       int
	// diagrams are the project's diagrams.
	diagrams []Diagram

	mu sync.Mutex
	// row is which of the run's scenarios the show is in, from 0: the rows
	// of a Scenario Outline play one after another, each with its own beats,
	// and the presenter goes on from one to the next. line is its line.
	row, line int
	// skip is how many scenarios a run from the top passes without a stop,
	// to come back to the one the show was in.
	skip int
	// take is the run so far, and timeline its cut; nil before the first
	// step.
	take     *Take
	timeline *Timeline
	// opening is what the stage shows before the run has a step: the
	// demo's first card, or its name.
	opening string
	// mode is "live", "recorded" (the fallback plays), "restarting" (the
	// scenario runs again from the top, to where the show was) or "done".
	mode string
	// reached counts the beats the run came to, entered those the presenter
	// went on to (the run is at most one ahead, waiting), and cursor is the
	// beat the audience sees.
	reached, entered, cursor int
	// auto is how many beats the run goes on to by itself: those the show
	// had already played, when it runs again from the top.
	auto int
	// waiting is whether the run waits at a gate.
	waiting bool
	// failure is the step that failed and waits for the presenter.
	failure *Failure
	// decision is what the presenter said to do about a failure: "top"
	// once the run is to start again.
	decision string
	// gates are how many beats each step has before it, by its line.
	gates map[int]int
	// firstCard is whether the demo's first beat is a card: it shows from
	// the start, with nothing to wait for.
	firstCard bool

	version int
	wake    chan struct{} // closed and replaced at each change
	word    chan string   // the presenter's word to a waiting run
	quit    chan struct{} // closed when the presenter ends the show
	once    sync.Once
}

// Failure is a step that failed before an audience.
type Failure struct {
	Step  string `json:"step"`
	Error string `json:"error"`
}

// Live is the state of a show, for the stage and the presenter's page.
type Live struct {
	Mode    string `json:"mode"`
	Cursor  int    `json:"cursor"`
	Entered int    `json:"entered"`
	Waiting bool   `json:"waiting"`
	// Beats are the demo's beats, in order: what the presenter's page lists.
	Beats []string `json:"beats"`
	// Row is which of the demo's scenarios shows, from 0: the rows of a
	// Scenario Outline play one after another. More and Earlier are whether
	// forward and back have another row of a recorded show to go to.
	Row     int      `json:"row"`
	More    bool     `json:"more,omitempty"`
	Earlier bool     `json:"earlier,omitempty"`
	Failure *Failure `json:"failure,omitempty"`
	// Fallback is whether there is a take to play on from.
	Fallback bool `json:"fallback"`
	Version  int  `json:"version"`
}

func newShow(script *Script, st Settings, fallbacks []*Take, title string) *show {
	pace := DefaultPace
	pace.Live, pace.Card = true, 0.6
	sh := &show{
		script: script, st: st, pace: pace, fallbacks: fallbacks, mode: "live", opening: title,
		wake: make(chan struct{}), word: make(chan string, 1), quit: make(chan struct{}),
	}
	if beats := sh.beats(); len(beats) > 0 && beats[0].card {
		sh.firstCard, sh.opening, sh.entered = true, beats[0].text, 1
	}
	return sh
}

// beat is a card or a subtitle of a script, in the order they play.
type beat struct {
	card   bool
	text   string
	anchor int
}

// beatsOf are a script's beats: for each line of the feature its cues belong
// to, the cards, then the subtitle its other cues make together.
func beatsOf(script *Script) []beat {
	var out []beat
	if script == nil {
		return out
	}
	flush := func(anchor int, cards []string, subtitle string) {
		for _, c := range cards {
			out = append(out, beat{card: true, text: c, anchor: anchor})
		}
		if subtitle != "" {
			out = append(out, beat{text: subtitle, anchor: anchor})
		}
	}
	anchor, started := 0, false
	var cards []string
	subtitle := ""
	for _, c := range script.Cues {
		if started && c.Anchor != anchor {
			flush(anchor, cards, subtitle)
			cards, subtitle = nil, ""
		}
		anchor, started = c.Anchor, true
		isCard := false
		for _, d := range c.Directions {
			if d.Name == "card" {
				cards, isCard = append(cards, c.Text), true
			}
		}
		if !isCard && c.Text != "" {
			if subtitle != "" {
				subtitle += " "
			}
			subtitle += c.Text
		}
	}
	if started {
		flush(anchor, cards, subtitle)
	}
	return out
}

// beats are the beats of the scenario the show is in: its own cues, with the
// values of its row, and the Feature's, which play once, with the first. The
// caller holds mu, or nobody else has the show yet.
func (sh *show) beats() []beat {
	if sh.script == nil {
		return nil
	}
	row, line := sh.row, sh.line
	if sh.mode == "recorded" && sh.take != nil {
		row, line = sh.rec, sh.take.Line
	}
	// The scenario of that line, or of that place among the file's.
	var of *ScriptScenario
	n := 0
	for i := range sh.script.Scenarios {
		sc := &sh.script.Scenarios[i]
		rows := max(1, len(sc.Rows))
		if sc.Line == line || slices.Contains(sc.Rows, line) || of == nil && line == 0 && row < n+rows {
			of = sc
			if line == 0 && len(sc.Rows) > 0 {
				line = sc.Rows[row-n]
			}
			break
		}
		n += rows
	}
	var out []beat
	for _, b := range beatsOf(sh.script) {
		switch {
		case b.anchor != 0 && b.anchor <= sh.script.FeatureLine:
			if row > 0 {
				continue
			}
		case of != nil && b.anchor != 0 && b.anchor != of.Line && !slices.Contains(of.Steps, b.anchor):
			continue // another scenario's
		}
		b.text = fill(b.text, sh.script.Rows[line])
		out = append(out, b)
	}
	return out
}

// fallbackOf is the last good take of the scenario the show is in, or nil.
// The caller holds mu.
func (sh *show) fallbackOf() (*Take, int) {
	for i, t := range sh.fallbacks {
		if t.Line == sh.line {
			return t, i
		}
	}
	// Before the first step, and where the file's lines moved since.
	if sh.row < len(sh.fallbacks) {
		return sh.fallbacks[sh.row], sh.row
	}
	return nil, 0
}

// changed tells whoever follows the show that it changed. The caller holds mu.
func (sh *show) changed() {
	sh.version++
	close(sh.wake)
	sh.wake = make(chan struct{})
}

// watch returns the show's version and a channel closed at its next change.
func (sh *show) watch() (int, <-chan struct{}) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.version, sh.wake
}

// start begins a run of the scenario: the first, or one from the top.
func (sh *show) start() {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.reached, sh.gates, sh.failure, sh.decision, sh.waiting = 0, nil, nil, "", false
	sh.row, sh.line = 0, 0
	if sh.auto > 0 {
		sh.mode, sh.entered, sh.take, sh.timeline = "restarting", sh.auto, nil, nil
	}
	sh.changed()
}

// arrive is told, before each step, which scenario runs. At the first step
// of the one after another (the next row of a Scenario Outline) it waits for
// the presenter to leave the row before, whose last picture the audience is
// still reading, and begins the row. It reports false when the show ended
// instead.
func (sh *show) arrive(sc *core.Scenario) bool {
	sh.mu.Lock()
	if sh.mode == "recorded" || sh.line == sc.Line {
		sh.mu.Unlock()
		return true
	}
	if sh.line == 0 {
		sh.line = sc.Line
		sh.mu.Unlock()
		return true
	}
	if passing := sh.mode == "restarting" && sh.row < sh.skip; !passing {
		// What cues the row has below its last step play now.
		sh.entered = 1 << 30
		sh.recut()
		if sh.timeline != nil {
			sh.entered = len(sh.timeline.Beats)
		}
		sh.waiting = true
		sh.changed()
		sh.mu.Unlock()
		release := sc.Hold()
		select {
		case <-sh.word:
		case <-sh.quit:
			release()
			return false
		case <-sc.Context().Done():
			release()
			return false
		}
		release()
		sh.mu.Lock()
		sh.waiting = false
		if sh.mode == "recorded" {
			sh.mu.Unlock()
			return true
		}
		sh.cursor = 0
	}
	sh.row++
	sh.line = sc.Line
	sh.reached, sh.gates, sh.entered = 0, nil, 0
	sh.take, sh.timeline, sh.opening = nil, nil, sc.Name
	sh.changed()
	sh.mu.Unlock()
	return true
}

// gate waits, before a step, for the presenter to go on to each beat the
// step has before it.
func (sh *show) gate(sc *core.Scenario) {
	st := sc.Step()
	if st == nil {
		return
	}
	sh.mu.Lock()
	if sh.mode == "recorded" {
		sh.mu.Unlock()
		return // the show plays takes: the run goes on unseen
	}
	if sh.gates == nil {
		// Each beat belongs to the first step at or below its anchor.
		sh.gates = map[int]int{}
		steps := sc.Progress()
		for _, b := range sh.beats() {
			if b.anchor == 0 {
				continue
			}
			for _, p := range steps {
				if p.Line >= b.anchor {
					sh.gates[p.Line]++
					break
				}
			}
		}
	}
	n := sh.gates[st.Line]
	sh.mu.Unlock()
	for range n {
		if !sh.enter(sc) {
			return
		}
	}
}

// enter goes on to the next beat: at once for the demo's opening card and
// for the beats a run from the top plays again, otherwise when the presenter
// says. It reports false when the show ended instead.
func (sh *show) enter(sc *core.Scenario) bool {
	sh.mu.Lock()
	index := sh.reached
	sh.reached++
	again := sh.mode == "restarting" && (sh.row < sh.skip || index < sh.auto)
	if again || sh.row == 0 && index == 0 && sh.firstCard && sh.entered <= 1 {
		if sh.entered < index+1 {
			sh.entered = index + 1
		}
		if sh.mode == "restarting" && sh.row >= sh.skip && sh.reached >= sh.auto {
			sh.mode = "live"
		}
		sh.changed()
		sh.mu.Unlock()
		return true
	}
	if sh.mode == "restarting" {
		sh.mode = "live"
	}
	sh.waiting = true
	sh.changed()
	sh.mu.Unlock()
	// The presenter's time is not the step's.
	release := sc.Hold()
	defer release()
	select {
	case <-sh.word:
	case <-sh.quit:
		return false
	case <-sc.Context().Done():
		return false
	}
	sh.mu.Lock()
	sh.waiting = false
	sh.entered = index + 1
	sh.cursor = index
	// The beat's subtitle or card shows at once, before its steps ran.
	sh.recut()
	sh.changed()
	sh.mu.Unlock()
	return true
}

// update takes the run as it stands into the show.
func (sh *show) update(sc *core.Scenario, cams []*camera) {
	take := takeOf(sc)
	for _, c := range cams {
		take.Footage = append(take.Footage, c.sofar()...)
	}
	sortFootage(take)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.mode == "recorded" {
		return // the show plays a take: what the run still does is not shown
	}
	sh.take = take
	sh.recut()
	sh.changed()
}

// recut cuts the run so far again. The caller holds mu.
func (sh *show) recut() {
	if sh.take == nil {
		return
	}
	pace := sh.pace
	pace.Entered, pace.Row = sh.entered, sh.row
	sh.timeline = Cut(sh.take, sh.script, pace)
}

// failed is told of a step that failed, and waits for the presenter: it
// returns core.ErrRetryStep to run the step once more.
func (sh *show) failed(sc *core.Scenario) error {
	st := sc.Step()
	if st == nil {
		return nil
	}
	f := &Failure{Step: st.Keyword + " " + st.Text}
	for _, p := range sc.Progress() {
		if p.Line == st.Line {
			f.Error = p.Error
		}
	}
	sh.mu.Lock()
	if sh.mode == "recorded" {
		sh.mu.Unlock()
		return nil // the show plays takes: the run goes on unseen
	}
	sh.failure = f
	sh.changed()
	sh.mu.Unlock()
	release := sc.Hold()
	defer release()
	var said string
	select {
	case said = <-sh.word:
	case <-sh.quit:
	case <-sc.Context().Done():
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.failure = nil
	switch said {
	case "retry":
		sh.changed()
		return core.ErrRetryStep
	case "take":
		sh.playFallback()
	case "top":
		sh.decision, sh.auto, sh.skip = "top", sh.cursor+1, sh.row
	}
	sh.changed()
	return nil
}

// playFallback has the show play on from the last good take. The caller
// holds mu.
func (sh *show) playFallback() {
	take, at := sh.fallbackOf()
	if take == nil {
		return
	}
	sh.rec = at
	sh.playRecorded()
}

// playRecorded plays the take the recorded show is at. The caller holds mu.
func (sh *show) playRecorded() {
	take := sh.fallbacks[sh.rec]
	pace := sh.pace
	pace.Entered, pace.Row = 1<<30, sh.rec
	sh.mode, sh.take = "recorded", take
	sh.timeline = Cut(take, sh.script, pace)
	sh.entered = len(sh.timeline.Beats)
}

// ended is told that the run of the scenario ended, and whether it passed.
func (sh *show) ended(passed bool) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.mode == "live" && sh.decision == "" {
		if passed {
			sh.mode = "done"
			// What cues the demo has below its last step plays now.
			sh.entered = 1 << 30
			sh.recut()
			sh.entered = len(sh.timeline.Beats)
		} else {
			// A run that ended otherwise than by a step the presenter decided on.
			sh.playFallback()
		}
	}
	sh.changed()
}

// say takes a word from the stage or the presenter's page: forward, back,
// retry, take, top or quit.
func (sh *show) say(word string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	switch word {
	case "forward":
		switch {
		case sh.cursor < sh.entered-1:
			sh.cursor++
		case sh.mode == "recorded" && sh.rec+1 < len(sh.fallbacks):
			// The next row's take.
			sh.rec++
			sh.playRecorded()
			sh.cursor = 0
		case sh.waiting && sh.failure == nil:
			select {
			case sh.word <- "forward":
			default:
			}
			return
		}
	case "back":
		switch {
		case sh.cursor > 0:
			sh.cursor--
		case sh.mode == "recorded" && sh.rec > 0:
			// The row before's take, at its end.
			sh.rec--
			sh.playRecorded()
			sh.cursor = max(0, sh.entered-1)
		}
	case "retry", "take", "top":
		if take, _ := sh.fallbackOf(); sh.failure == nil || word == "take" && take == nil {
			return
		}
		select {
		case sh.word <- word:
		default:
		}
		return
	case "quit":
		sh.once.Do(func() { close(sh.quit) })
	}
	sh.changed()
}

// staged is what the stage plays now.
func (sh *show) staged() *Staged {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	fallback, _ := sh.fallbackOf()
	live := &Live{
		Mode: sh.mode, Cursor: sh.cursor, Entered: sh.entered, Waiting: sh.waiting, Row: sh.row,
		Beats: []string{}, Failure: sh.failure, Fallback: fallback != nil, Version: sh.version,
	}
	if sh.mode == "recorded" {
		live.Row, live.More, live.Earlier = sh.rec, sh.rec+1 < len(sh.fallbacks), sh.rec > 0
	}
	for _, b := range sh.beats() {
		live.Beats = append(live.Beats, b.text)
	}
	out := &Staged{Take: sh.take, Timeline: sh.timeline, Theme: sh.st.Theme, Live: live, Diagrams: sh.diagrams}
	if out.Take == nil || out.Timeline == nil || len(out.Timeline.Segments) == 0 || sh.mode == "restarting" {
		// Before the run has a step to show, and while it runs again from
		// the top: a card.
		text := sh.opening
		if sh.mode == "restarting" {
			text = "One moment"
		}
		out.Take = &Take{Version: TakeVersion, Peers: []Peer{}, Steps: []TakeStep{}}
		out.Timeline = &Timeline{
			Duration: 0.1, Subtitles: []Subtitle{}, Beats: []float64{0},
			Segments: []Segment{{Kind: "card", End: 0.1, Step: -1, Text: text}},
		}
	}
	if sh.mode == "recorded" {
		out.Mark = "recorded"
	}
	return out
}

// wait returns when the presenter ends the show, or ctx is done.
func (sh *show) wait(ctx context.Context) {
	select {
	case <-sh.quit:
	case <-ctx.Done():
	}
}

// sortFootage puts a take's pictures in the order they were taken.
func sortFootage(t *Take) {
	sort.SliceStable(t.Footage, func(i, j int) bool { return t.Footage[i].At < t.Footage[j].At })
}
