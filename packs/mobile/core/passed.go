package mobilecore

import (
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
)

// Passed is what a step remembers of the controls that went by as it scrolled a screen. A list
// that makes its rows as it scrolls has nothing of a row past the ones it has made, so a step
// that looks for one looks a page at a time, and knowing that a row went off the top says which
// way to look first. It only ever chooses the first direction: a memory that is out of date (the
// app shows another screen by now) costs a look the other way.
type Passed struct {
	mu      sync.Mutex
	toward  string         // the direction the last scroll went, until the screen is read again
	showing []string       // the names the screen showed when it was last read
	where   map[string]int // a name that went by: 1 above the screen, -1 below
}

// Scrolled notes that the screen was scrolled up or down: what its next reading no longer shows
// went by that way.
func (p *Passed) Scrolled(direction string) {
	if direction != "up" && direction != "down" {
		return
	}
	p.mu.Lock()
	p.toward = direction
	p.mu.Unlock()
}

// Read keeps what a reading of the screen shows.
func (p *Passed) Read(s *Screen) {
	var names []string
	for _, n := range s.Visible() {
		if name := n.Name(); name != "" {
			names = append(names, name)
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.where == nil || len(p.where) > 4000 {
		p.where = map[string]int{}
	}
	if p.toward != "" {
		side := 1 // scrolled down: what left went off the top
		if p.toward == "up" {
			side = -1
		}
		now := map[string]bool{}
		for _, name := range names {
			now[name] = true
		}
		for _, name := range p.showing {
			if !now[name] {
				p.where[name] = side
			}
		}
		p.toward = ""
	}
	for _, name := range names {
		delete(p.where, name)
	}
	p.showing = names
}

// Toward is the directions to look for a name in, in order: down and then up, as a person reads
// a screen, or up first for a name that went off the top.
func (p *Passed) Toward(name string) []string {
	want := clean(name)
	p.mu.Lock()
	defer p.mu.Unlock()
	for seen, side := range p.where {
		if side > 0 && strings.Contains(seen, want) {
			return []string{"up", "down"}
		}
	}
	return []string{"down", "up"}
}

// passedOn is what went by on each of a scenario's devices.
type passedOn struct {
	mu sync.Mutex
	by map[Device]*Passed
}

var scenarioPassed = core.NewStateKey(Name+"/passed", func(*core.Scenario) *passedOn { return &passedOn{by: map[Device]*Passed{}} }, nil)

// passedBy is what went by on a device, in the scenario.
func passedBy(sc *core.Scenario, d Device) *Passed {
	on := scenarioPassed.Of(sc)
	on.mu.Lock()
	defer on.mu.Unlock()
	if on.by[d] == nil {
		on.by[d] = &Passed{}
	}
	return on.by[d]
}
