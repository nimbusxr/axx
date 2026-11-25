package demo

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

// until waits for the show to be as wanted.
func until(t *testing.T, sh *show, what string, ok func(l *Live) bool) *Staged {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := sh.staged()
		if ok(st.Live) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("the show never was %s: %+v", what, st.Live)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// stage plays a scenario of holdFeature's steps as a run would, for a show:
// each step waits at its gate, runs, and is shown. fail names a step's line
// that fails until the presenter has it tried again that many times.
func stage(t *testing.T, sh *show, fail, failures int) (done chan bool) {
	t.Helper()
	take := holdTake()
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{URI: take.Feature, Line: take.Line}, core.NewSuite(core.SuiteOptions{}), nil)
	var steps []core.StepProgress
	for _, st := range take.Steps {
		steps = append(steps, core.StepProgress{StepInfo: core.StepInfo{Keyword: st.Keyword, Text: st.Text, Line: st.Line, Pack: st.Pack}})
	}
	sc.SetProgress(steps)
	done = make(chan bool, 1)
	go func() {
		sh.start()
		passed := true
		for i, st := range take.Steps {
			sc.SetStep(&steps[i].StepInfo)
			sh.update(sc, nil)
			sh.gate(sc)
			for _, x := range st.Exchanges {
				sc.Exchanged(x)
			}
			for {
				failed := st.Line == fail && failures > 0
				sc.UpdateProgress(i, func(p *core.StepProgress) {
					p.Started, p.Ended, p.Status, p.Error = time.Now(), time.Now(), "passed", ""
					if failed {
						p.Status, p.Error = "failed", "no answer"
					}
				})
				if !failed {
					break
				}
				failures--
				if !errors.Is(sh.failed(sc), core.ErrRetryStep) {
					passed = false
					break
				}
			}
			if !passed {
				break
			}
			sh.update(sc, nil)
		}
		sh.ended(passed)
		done <- passed
	}()
	return done
}

func TestAShowGoesOnABeatAtATime(t *testing.T) {
	script, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), Known{})
	if err != nil {
		t.Fatal(err)
	}
	sh := newShow(script, Settings{}, nil, "hold")
	// Before anything ran, the stage has the demo's first card.
	if st := sh.staged(); st.Timeline.Segments[0].Text != "Holding a parcel at the depot" || st.Live.Cursor != 0 {
		t.Fatalf("the opening: %+v, %+v", st.Timeline.Segments, st.Live)
	}
	done := stage(t, sh, 0, 0)
	// The steps under [cut] ran with nobody asked; the run waits before the
	// first subtitle, and the stage still shows the card.
	st := until(t, sh, "waiting at the first subtitle", func(l *Live) bool { return l.Waiting && l.Entered == 1 })
	if len(st.Timeline.Subtitles) != 0 || len(st.Live.Beats) != 5 {
		t.Errorf("before the presenter went on: %+v, beats %v", st.Timeline, st.Live.Beats)
	}
	sh.say("forward")
	st = until(t, sh, "waiting at the second subtitle", func(l *Live) bool { return l.Waiting && l.Entered == 2 })
	if st.Live.Cursor != 1 || len(st.Timeline.Subtitles) != 1 || st.Timeline.Segments[len(st.Timeline.Segments)-1].Step != 2 {
		t.Errorf("after the first forward: %+v, %+v", st.Live, st.Timeline.Segments)
	}
	// What was played stays where it was as the timeline grows.
	firstBeat := st.Timeline.Beats[1]
	sh.say("forward")
	st = until(t, sh, "waiting at the third subtitle", func(l *Live) bool { return l.Waiting && l.Entered == 3 })
	if st.Timeline.Beats[1] != firstBeat || st.Live.Cursor != 2 {
		t.Errorf("after the second forward: %+v, beats %v", st.Live, st.Timeline.Beats)
	}
	// Back shows again what was shown, and the run stays where it is; forward
	// then goes on through what was played before it asks the run for more.
	sh.say("back")
	sh.say("back")
	if st = sh.staged(); st.Live.Cursor != 0 || st.Live.Entered != 3 || !st.Live.Waiting {
		t.Errorf("after going back twice: %+v", st.Live)
	}
	sh.say("forward")
	sh.say("forward")
	if st = sh.staged(); st.Live.Cursor != 2 || st.Live.Entered != 3 {
		t.Errorf("after going forward over what was played: %+v", st.Live)
	}
	sh.say("forward")
	until(t, sh, "done", func(l *Live) bool { return l.Mode == "done" })
	if passed := <-done; !passed {
		t.Error("the scenario did not pass")
	}
	if st = sh.staged(); len(st.Timeline.Beats) != 5 || st.Live.Entered != 5 {
		t.Errorf("at the end: %d beats in the timeline, %+v", len(st.Timeline.Beats), st.Live)
	}
}

func TestAShowWhoseStepFails(t *testing.T) {
	script, err := ParseScript("features/hold.demo.feature", []byte(holdFeature), Known{})
	if err != nil {
		t.Fatal(err)
	}
	// The step fails twice: the presenter has it tried again, then plays on
	// from the last take.
	sh := newShow(script, Settings{}, []*Take{holdTake()}, "hold")
	done := stage(t, sh, 19, 2)
	until(t, sh, "waiting", func(l *Live) bool { return l.Waiting })
	sh.say("forward")
	until(t, sh, "waiting", func(l *Live) bool { return l.Waiting && l.Entered == 2 })
	sh.say("forward")
	st := until(t, sh, "failed", func(l *Live) bool { return l.Failure != nil })
	if st.Live.Failure.Error != "no answer" || !st.Live.Fallback {
		t.Errorf("the failure: %+v", st.Live)
	}
	// The audience sees nothing of the step that failed.
	for _, seg := range st.Timeline.Segments {
		if seg.Step == 3 {
			t.Errorf("the stage shows the step that failed: %+v", seg)
		}
	}
	// Forward does nothing while the presenter has to decide.
	sh.say("forward")
	version := sh.staged().Live.Version
	sh.say("retry")
	until(t, sh, "failed again", func(l *Live) bool { return l.Failure != nil && l.Version > version })
	sh.say("take")
	st = until(t, sh, "playing the take", func(l *Live) bool { return l.Mode == "recorded" && l.Failure == nil })
	if st.Mark != "recorded" || len(st.Timeline.Beats) != 5 || st.Live.Entered != 5 || st.Live.Cursor != 2 {
		t.Errorf("playing on from the take: mark %q, %d beats, %+v", st.Mark, len(st.Timeline.Beats), st.Live)
	}
	<-done
	sh.say("forward")
	if st = sh.staged(); st.Live.Cursor != 3 || st.Live.Mode != "recorded" {
		t.Errorf("forward in the take: %+v", st.Live)
	}

	// From the top: the run starts again and goes on by itself to where the
	// show was, behind a card.
	sh = newShow(script, Settings{}, nil, "hold")
	done = stage(t, sh, 19, 1)
	until(t, sh, "waiting", func(l *Live) bool { return l.Waiting })
	sh.say("forward")
	until(t, sh, "waiting", func(l *Live) bool { return l.Waiting && l.Entered == 2 })
	sh.say("forward")
	until(t, sh, "failed", func(l *Live) bool { return l.Failure != nil })
	sh.say("take") // there is none: nothing happens
	if st = sh.staged(); st.Live.Failure == nil {
		t.Error("playing on from a take there is not ended the failure")
	}
	sh.say("top")
	<-done
	sh.mu.Lock()
	decision, auto := sh.decision, sh.auto
	sh.mu.Unlock()
	if decision != "top" || auto != 3 {
		t.Fatalf("from the top: decision %q, %d beats to play again", decision, auto)
	}
	done = stage(t, sh, 0, 0)
	st = until(t, sh, "live again, at the next subtitle", func(l *Live) bool { return l.Mode == "live" && l.Waiting })
	if st.Live.Entered != 3 || st.Live.Cursor != 2 {
		t.Errorf("after running again from the top: %+v", st.Live)
	}
	sh.say("forward")
	sh.say("forward")
	until(t, sh, "done", func(l *Live) bool { return l.Mode == "done" })
	<-done
}

const pricesFeature = `# demo: [card 3s] Prices
Feature: Prices

  Scenario Outline: A parcel of <grams> grams
    # demo: A parcel of <grams> grams costs <price> cents.
    When the price is asked

    Examples:
      | grams | price |
      | 500   | 490   |
      | 7500  | 890   |
`

// The rows of a Scenario Outline play one after another before an audience:
// each has its own beats, with its row's values, the Feature's card plays
// once, and between two rows the run waits for the presenter, who is still
// showing the row before.
func TestAShowOfAnOutlinesRows(t *testing.T) {
	script, err := ParseScript("prices.demo.feature", []byte(pricesFeature), Known{})
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	sh := newShow(script, Settings{}, nil, "prices")
	if st := sh.staged(); fmt.Sprint(st.Live.Beats) != "[Prices A parcel of 500 grams costs 490 cents.]" {
		t.Fatalf("the first row's beats: %v", st.Live.Beats)
	}
	done := make(chan bool, 1)
	go func() {
		sh.start()
		for _, row := range []struct {
			line  int
			grams string
		}{{10, "500"}, {11, "7500"}} {
			sc := core.NewScenario(context.Background(), core.ScenarioInfo{URI: "prices.demo.feature", Line: row.line, Name: "A parcel of " + row.grams + " grams"}, core.NewSuite(core.SuiteOptions{}), nil)
			steps := []core.StepProgress{{StepInfo: core.StepInfo{Keyword: "When", Text: "the price is asked", Line: 6}}}
			sc.SetProgress(steps)
			sc.SetStep(&steps[0].StepInfo)
			if !sh.arrive(sc) {
				done <- false
				return
			}
			sh.update(sc, nil)
			sh.gate(sc)
			sc.UpdateProgress(0, func(p *core.StepProgress) { p.Started, p.Ended, p.Status = time.Now(), time.Now(), "passed" })
			sh.update(sc, nil)
		}
		sh.ended(true)
		done <- true
	}()
	until(t, sh, "waiting at the first row's subtitle", func(l *Live) bool { return l.Waiting && l.Row == 0 && l.Entered == 1 })
	sh.say("forward")
	// The row ran, and the run waits before the next: the audience still
	// reads this one.
	st := until(t, sh, "waiting at the first row's end", func(l *Live) bool { return l.Waiting && l.Row == 0 && l.Entered == 2 })
	if st.Live.Cursor != 1 || len(st.Timeline.Subtitles) != 1 || st.Timeline.Subtitles[0].Text != "A parcel of 500 grams costs 490 cents." {
		t.Errorf("the first row, played: %+v, %+v", st.Live, st.Timeline.Subtitles)
	}
	sh.say("forward")
	// The next row begins on a card of its name, with its own beats: the
	// Feature's card is not one of them.
	st = until(t, sh, "waiting at the second row's subtitle", func(l *Live) bool { return l.Waiting && l.Row == 1 })
	if fmt.Sprint(st.Live.Beats) != "[A parcel of 7500 grams costs 890 cents.]" || st.Live.Cursor != 0 || st.Live.Entered != 0 {
		t.Errorf("the second row's beats: %+v", st.Live)
	}
	if len(st.Timeline.Segments) != 1 || st.Timeline.Segments[0].Kind != "card" || st.Timeline.Segments[0].Text != "A parcel of 7500 grams" {
		t.Errorf("the second row begins on a card of its name: %+v", st.Timeline.Segments)
	}
	sh.say("forward")
	st = until(t, sh, "done", func(l *Live) bool { return l.Mode == "done" })
	if passed := <-done; !passed {
		t.Error("the rows did not pass")
	}
	if st.Live.Row != 1 || len(st.Timeline.Subtitles) != 1 || st.Timeline.Subtitles[0].Text != "A parcel of 7500 grams costs 890 cents." {
		t.Errorf("the second row, played: %+v, %+v", st.Live, st.Timeline.Subtitles)
	}
	for _, seg := range st.Timeline.Segments {
		if seg.Kind == "card" {
			t.Errorf("the Feature's card played again with the second row: %+v", seg)
		}
	}
}

// A recorded show plays the rows' takes one after another: forward at a
// row's end goes to the next row, and back at its start to the row before.
func TestARecordedShowOfAnOutlinesRows(t *testing.T) {
	script, err := ParseScript("prices.demo.feature", []byte(pricesFeature), Known{})
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	take := func(line int) *Take {
		return &Take{Version: TakeVersion, Line: line, Passed: true, Steps: []TakeStep{{Keyword: "When", Text: "the price is asked", Line: 6, Status: "passed"}}}
	}
	sh := newShow(script, Settings{}, []*Take{take(10), take(11)}, "prices")
	sh.mu.Lock()
	sh.playFallback()
	sh.mu.Unlock()
	st := sh.staged()
	if st.Live.Mode != "recorded" || st.Live.Row != 0 || !st.Live.More || st.Live.Earlier || len(st.Live.Beats) != 2 {
		t.Fatalf("the first row's take: %+v", st.Live)
	}
	sh.say("forward")
	sh.say("forward")
	st = sh.staged()
	if st.Live.Row != 1 || st.Live.Cursor != 0 || st.Live.More || !st.Live.Earlier || fmt.Sprint(st.Live.Beats) != "[A parcel of 7500 grams costs 890 cents.]" || st.Take.Line != 11 {
		t.Errorf("forward at the first row's end: %+v", st.Live)
	}
	sh.say("forward") // the last row's last beat: nothing more
	sh.say("back")
	if st = sh.staged(); st.Live.Row != 0 || st.Live.Cursor != 1 || st.Take.Line != 10 {
		t.Errorf("back at the second row's start: %+v", st.Live)
	}
}

// Before an audience a demo's introduction, a cue above its Scenario, shows
// when the presenter goes on to it, before any step is on camera: over the
// picture the demo begins on, not over the card before it.
func TestAShowsIntroduction(t *testing.T) {
	src := `# demo: [card 3s] Prices
Feature: Prices

  # demo: This is how a price is asked.
  Scenario: A parcel
    # demo: [cut]
    Given the prices service
    # demo: [action] It costs 490 cents.
    When the price is asked
`
	script, err := ParseScript("prices.demo.feature", []byte(src), Known{})
	if err != nil || len(script.Problems) > 0 {
		t.Fatal(err, script.Problems)
	}
	// The run waits before its first step: nothing ran yet.
	take := &Take{Version: TakeVersion, Line: 5, Steps: []TakeStep{{Keyword: "Given", Text: "the prices service", Line: 7}, {Keyword: "When", Text: "the price is asked", Line: 9}}}
	pace := DefaultPace
	pace.Live, pace.Card, pace.Entered = true, 0.6, 2
	tl := Cut(take, script, pace)
	if len(tl.Segments) != 2 || tl.Segments[0].Kind != "card" || tl.Segments[1].Kind != "hold" {
		t.Fatalf("the card, then the picture the introduction is read over: %+v", tl.Segments)
	}
	if len(tl.Subtitles) != 1 || tl.Subtitles[0].Text != "This is how a price is asked." || tl.Subtitles[0].Start != tl.Segments[1].Start || tl.Beats[1] != tl.Segments[1].Start {
		t.Errorf("the introduction over it: %+v, beats %v", tl.Subtitles, tl.Beats)
	}
}
