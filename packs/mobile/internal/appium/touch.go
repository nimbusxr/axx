package appium

import (
	"context"
	"time"
)

// Touch is a finger on the screen, as the session moved it, in the window's
// coordinates (Window's): a tap where From is To, a swipe or a drag from
// From to To over Length.
type Touch struct {
	From, To Point
	At       time.Time
	Length   time.Duration
}

// Point is a point of the window.
type Point struct{ X, Y float64 }

// touched reports a touch to the session's Touched, if it has one.
func (s *Session) touched(t Touch) {
	if f := s.Touched; f != nil {
		f(t)
	}
}

// gesture is the path of one of Appium's mobile: gestures, when it is one
// whose path the session knows: the finger, as it moves.
func (s *Session) gesture(ctx context.Context, command string, args map[string]any) (Touch, bool) {
	num := func(k string) float64 {
		switch v := args[k].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
		return 0
	}
	dir, _ := args["direction"].(string)
	area := Rect{X: num("left"), Y: num("top"), Width: num("width"), Height: num("height")}
	share := 0.5
	switch command {
	case "swipeGesture", "scrollGesture": // UiAutomator2's: an area, a direction and a share of it
		if p := num("percent"); p > 0 {
			share = p
		}
	case "swipe", "scroll": // XCUITest's: the whole screen, or an element
		if id, _ := args["elementId"].(string); id != "" {
			r, err := (&Element{s: s, ID: id}).Rect(ctx)
			if err != nil {
				return Touch{}, false
			}
			area = r
		} else {
			r, err := s.window(ctx)
			if err != nil {
				return Touch{}, false
			}
			area = r
		}
	default:
		return Touch{}, false
	}
	if area.Width <= 0 || area.Height <= 0 {
		return Touch{}, false
	}
	// A swipe moves the finger the way it names; a scroll shows what is that
	// way, the finger moving the other.
	if command == "scrollGesture" || command == "scroll" {
		dir = map[string]string{"up": "down", "down": "up", "left": "right", "right": "left"}[dir]
	}
	cx, cy := area.X+area.Width/2, area.Y+area.Height/2
	dx, dy := area.Width*share/2, area.Height*share/2
	var from, to Point
	switch dir {
	case "up":
		from, to = Point{cx, cy + dy}, Point{cx, cy - dy}
	case "down":
		from, to = Point{cx, cy - dy}, Point{cx, cy + dy}
	case "left":
		from, to = Point{cx + dx, cy}, Point{cx - dx, cy}
	case "right":
		from, to = Point{cx - dx, cy}, Point{cx + dx, cy}
	default:
		return Touch{}, false
	}
	return Touch{From: from, To: to, Length: 300 * time.Millisecond}, true
}

// window is the window's rectangle, read once.
func (s *Session) window(ctx context.Context) (Rect, error) {
	s.mu.Lock()
	r, ok := s.win, s.winRead
	s.mu.Unlock()
	if ok {
		return r, nil
	}
	r, err := s.Window(ctx)
	if err != nil {
		return Rect{}, err
	}
	s.mu.Lock()
	s.win, s.winRead = r, true
	s.mu.Unlock()
	return r, nil
}
