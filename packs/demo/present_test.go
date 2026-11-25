package demo

import (
	"context"
	"errors"
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
	sh := newShow(script, Settings{}, holdTake(), "hold")
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
