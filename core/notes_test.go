package core

import (
	"context"
	"testing"
)

// What a pack notes of a scenario is read back by its name, and all of it
// is what a pack carries to its other side: a copy, which later notes do
// not change.
func TestAScenariosNotes(t *testing.T) {
	sc := NewScenario(context.Background(), ScenarioInfo{}, nil, nil)
	if v, ok := sc.Noted("models/microphone"); ok || v != "" {
		t.Errorf("nothing was noted: %q, %v", v, ok)
	}
	if n := sc.Notes(); len(n) != 0 {
		t.Errorf("no notes: %v", n)
	}
	sc.Note("models/microphone", "true")
	sc.Note("models/fed/courier", "")
	all := sc.Notes()
	sc.Note("models/microphone", "false")
	if v, ok := sc.Noted("models/microphone"); !ok || v != "false" {
		t.Errorf("the note, noted again: %q, %v", v, ok)
	}
	if v, ok := sc.Noted("models/fed/courier"); !ok || v != "" {
		t.Errorf("an empty note is one: %q, %v", v, ok)
	}
	if len(all) != 2 || all["models/microphone"] != "true" {
		t.Errorf("the notes as they were: %v", all)
	}
}
