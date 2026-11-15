package mobilecore

import (
	"strings"
	"testing"
)

// A row that went off the top as the screen scrolled down is looked for up first: Android lists
// nothing of a list past the rows it has made.
func TestWhatWentByIsLookedForUpFirst(t *testing.T) {
	screen := func(names ...string) *Screen {
		s := &Screen{}
		for _, name := range names {
			s.Nodes = append(s.Nodes, &Node{Text: name, Displayed: true})
		}
		return s
	}
	d := &Passed{}
	d.Read(screen("PX-MOB-9401", "PX-MOB-9402", "PX-MOB-9403"))
	if got := strings.Join(d.Toward("PX-MOB-9401"), " "); got != "down up" {
		t.Errorf("a row the screen shows is looked for %s", got)
	}
	d.Scrolled("down")
	d.Read(screen("PX-MOB-9403", "PX-MOB-9404", "PX-MOB-9405"))
	if got := strings.Join(d.Toward("PX-MOB-9401"), " "); got != "up down" {
		t.Errorf("a row that went off the top is looked for %s, want up first", got)
	}
	if got := strings.Join(d.Toward("PX-MOB-9409"), " "); got != "down up" {
		t.Errorf("a row never seen is looked for %s, want down first", got)
	}
	// Scrolled back up, the row shows again, and what left went off the bottom.
	d.Scrolled("up")
	d.Read(screen("PX-MOB-9401", "PX-MOB-9402", "PX-MOB-9403"))
	if got := strings.Join(d.Toward("PX-MOB-9405"), " "); got != "down up" {
		t.Errorf("a row that went off the bottom is looked for %s, want down first", got)
	}
	// A screen read with no scroll before it says nothing of where what it lacks went.
	d.Read(screen("Sign in"))
	if got := strings.Join(d.Toward("PX-MOB-9402"), " "); got != "down up" {
		t.Errorf("after another screen came, a row is looked for %s, want down first", got)
	}
}
