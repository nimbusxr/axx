//go:build linux

package desktoplinux

import (
	"testing"

	"github.com/nimbusxr/axx/packs/desktop/internal/atspi"
)

// The Qt 5 depot desk's Register button and what it is in, as the desk gives them once its form
// has scrolled: the form's scroll area is a filler with a scroll bar, and has no area's role.
func qtForm(buttonY int32) (atspi.Rect, []around, atspi.Rect) {
	no, yes := func() bool { return false }, func() bool { return true }
	button := atspi.Rect{X: 21, Y: buttonY, Width: 584, Height: 23}
	in := []around{
		{role: "filler", at: atspi.Rect{X: 12, Y: buttonY - 140, Width: 602, Height: 670}, bar: no}, // the form
		{role: "filler", at: atspi.Rect{X: 12, Y: 164, Width: 602, Height: 441}, bar: no},           // the scroll area's view
		{role: "filler", at: atspi.Rect{X: 11, Y: 163, Width: 618, Height: 443}, bar: yes},          // the scroll area
		{role: "layered pane", at: atspi.Rect{X: 11, Y: 163, Width: 618, Height: 443}, bar: no},
		{role: "filler", at: atspi.Rect{X: 9, Y: 136, Width: 622, Height: 472}, bar: no}, // the tabs
		{role: "filler", at: atspi.Rect{X: 0, Y: 57, Width: 640, Height: 560}, bar: no},
	}
	return button, in, atspi.Rect{X: 0, Y: 37, Width: 640, Height: 580}
}

// A control scrolled out of an area that has no area's role (Qt's scroll area) does not show:
// its place is then over what is above the area, where a click reaches something else.
func TestAControlScrolledOutOfAnAreaWithoutARoleDoesNotShow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		buttonY int32
		shows   bool
	}{
		{"in the area", 195, true},
		{"scrolled out, over the tabs' strip", 135, false},
		{"scrolled out, over the picture above the tabs", 75, false},
		{"partly out of the area, its middle in it", 155, true},
		{"partly out of the area, its middle out of it", 145, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			button, in, win := qtForm(tc.buttonY)
			v := showing(button, in, win)
			x, y := button.X+button.Width/2, button.Y+button.Height/2
			middle := x >= v.X && x < v.X+v.Width && y >= v.Y && y < v.Y+v.Height
			if middle != tc.shows {
				t.Errorf("the button at %+v shows %+v of itself: its middle shows = %v, want %v", button, v, middle, tc.shows)
			}
		})
	}
}

// An area with an area's role shows part of what it holds whether it has a scroll bar or not,
// and an element with no place of its own is as far as its area shows.
func TestAnAreaShowsPartOfWhatItHolds(t *testing.T) {
	never := func() bool { t.Error("an area's role says it all: its scroll bar is not asked for"); return false }
	pane := around{role: "scroll pane", at: atspi.Rect{X: 10, Y: 100, Width: 300, Height: 200}, bar: never}
	win := atspi.Rect{X: 0, Y: 0, Width: 640, Height: 480}
	if got, want := showing(atspi.Rect{X: 20, Y: 280, Width: 100, Height: 40}, []around{pane}, win), (atspi.Rect{X: 20, Y: 280, Width: 100, Height: 20}); got != want {
		t.Errorf("a row at the area's end shows %+v, want %+v", got, want)
	}
	if got := showing(atspi.Rect{X: -2147483648, Y: -2147483648}, []around{pane}, win); got != pane.at {
		t.Errorf("an element with no place shows %+v, want its area's %+v", got, pane.at)
	}
	// What holds all of the element is not asked for a scroll bar either: nothing of it is cut.
	holds := around{role: "filler", at: atspi.Rect{X: 0, Y: 0, Width: 640, Height: 480}, bar: never}
	row := atspi.Rect{X: 20, Y: 120, Width: 100, Height: 40}
	if got := showing(row, []around{holds}, win); got != row {
		t.Errorf("a row its filler holds shows %+v, want all of it", got)
	}
}
