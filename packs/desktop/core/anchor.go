package desktopcore

import (
	"fmt"

	"github.com/nimbusxr/axx/core"
)

// An Anchor is the point of a control that places on it are measured from,
// in parts of its width and height: 0, 0 is its top left, 0.5, 0.5 its
// middle, 1, 1 its bottom right.
type Anchor struct{ X, Y float64 }

// TopLeft is where places are measured from when a step names no anchor.
var TopLeft = Anchor{}

// anchors are the {anchor} words.
var anchors = map[string]Anchor{
	"middle":       {0.5, 0.5},
	"top left":     {0, 0},
	"top right":    {1, 0},
	"bottom left":  {0, 1},
	"bottom right": {1, 1},
}

var anchorParam = core.ParamType{
	Name:    "anchor",
	Regexps: []string{`(middle|top left|top right|bottom left|bottom right)`},
	Doc: "the point of a control that places on it are measured from: its `middle`, or a corner. Places run right and down from it, " +
		"so one left of or above it is negative",
	Values:   []string{"middle", "top left", "top right", "bottom left", "bottom right"},
	Examples: []string{"middle", "bottom right"},
	Transform: func(_ *core.Scenario, match string, _ []*string) (any, error) {
		a, ok := anchors[match]
		if !ok {
			return nil, fmt.Errorf("%q is no point of a control", match)
		}
		return a, nil
	},
}

// Place is the point at x, y from the anchor of a rectangle at left, top
// that is width wide and height high, all in one unit.
func (a Anchor) Place(left, top, width, height, x, y float64) (float64, float64) {
	return left + a.X*width + x, top + a.Y*height + y
}

// String is the anchor's word, as a step names it.
func (a Anchor) String() string {
	for word, at := range anchors {
		if at == a {
			return word
		}
	}
	return fmt.Sprintf("%g, %g", a.X, a.Y)
}

// Off is a driver's error for a place that is not on its control, where a
// click would reach something else: the control's size, in the places' unit.
type Off struct{ Width, Height float64 }

func (o *Off) Error() string {
	return fmt.Sprintf("the place is off the control, %.0f by %.0f", o.Width, o.Height)
}

// Lost is a driver's error for a control with no place on the screen as an
// action takes it, before any input: the app drew it again and gave up the
// control the step found (a web view makes its controls anew as it lays
// out). The step finds the control again.
type Lost struct{ Err error }

func (l *Lost) Error() string { return l.Err.Error() }
func (l *Lost) Unwrap() error { return l.Err }

// On returns an *Off when the point x, y from the anchor is not on a control
// width wide and height high (its edges are on it).
func (a Anchor) On(width, height, x, y float64) error {
	px, py := a.Place(0, 0, width, height, x, y)
	if px < 0 || py < 0 || px > width || py > height {
		return &Off{width, height}
	}
	return nil
}
