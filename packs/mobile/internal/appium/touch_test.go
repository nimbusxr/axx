package appium

import (
	"context"
	"testing"
	"time"
)

// A gesture is drawn where the finger went: a fling up a view for a fling down it,
// and a press-and-drag from its point to its point, after the press.
func TestGestureTouches(t *testing.T) {
	s := &Session{}
	for _, c := range []struct {
		command string
		args    map[string]any
		want    Touch
	}{
		{
			"flingGesture",
			map[string]any{"left": 0, "top": 100, "width": 200, "height": 1000, "direction": "down"},
			Touch{From: Point{100, 1000}, To: Point{100, 200}, Length: 100 * time.Millisecond},
		},
		{
			"scrollGesture",
			map[string]any{"left": 0, "top": 100, "width": 200, "height": 1000, "direction": "up", "percent": 0.5},
			Touch{From: Point{100, 350}, To: Point{100, 850}, Length: 300 * time.Millisecond},
		},
		{
			"dragFromToForDuration",
			map[string]any{"fromX": 380.0, "fromY": 200.0, "toX": 380.0, "toY": 700.0, "duration": 0.6},
			Touch{From: Point{380, 200}, To: Point{380, 700}, Length: 700 * time.Millisecond},
		},
	} {
		got, ok := s.gesture(context.Background(), c.command, c.args)
		if !ok || got != c.want {
			t.Errorf("%s: %+v (%v), want %+v", c.command, got, ok, c.want)
		}
	}
	if _, ok := s.gesture(context.Background(), "pinchGesture", map[string]any{}); ok {
		t.Error("a gesture the session does not know is drawn")
	}
}
