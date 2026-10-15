package mobilecore

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// A Route is where a phone travels, as its GPS reports it: legs from point to point, each at its
// own speed. Its points come from a scenario's table or a GPX file.
type Route struct {
	Legs []Leg
}

// A Leg is one stretch of a route, at a steady speed, or a stop where the phone stands still
// (From is To, and its Speed 0).
type Leg struct {
	From, To Point
	// Speed is in metres a second.
	Speed float64
	// Time is how long the leg takes.
	Time time.Duration
}

// leg is the stretch from one point to another at a speed.
func leg(from, to Point, metresPerSecond float64) Leg {
	return Leg{From: from, To: to, Speed: metresPerSecond, Time: time.Duration(distance(from, to) / metresPerSecond * float64(time.Second))}
}

// A Point is a latitude and a longitude, in degrees.
type Point struct{ Lat, Lon float64 }

// A Fix is where a phone is at a moment of its route, and how it moves: its speed in metres a
// second and its course in degrees clockwise from north.
type Fix struct {
	Point
	Speed, Course float64
}

// Traveler is a device that can report a phone travelling along a route.
type Traveler interface {
	// Travel has the phone set off along the route from its first point, and returns at once;
	// arrived is closed when the phone reaches the last one, where it then stays.
	Travel(ctx context.Context, r Route) (arrived <-chan struct{}, err error)
}

// Duration is how long the phone takes to travel the route.
func (r Route) Duration() time.Duration {
	var d time.Duration
	for _, l := range r.Legs {
		d += l.Time
	}
	return d
}

// At is where the phone is a while after it set off; done tells whether it has arrived.
func (r Route) At(elapsed time.Duration) (f Fix, done bool) {
	for i, l := range r.Legs {
		d := l.Time
		if elapsed < d {
			if l.Speed == 0 {
				return Fix{Point: l.From, Course: r.courseBefore(i)}, false
			}
			t := float64(elapsed) / float64(d)
			return Fix{Point: Point{l.From.Lat + (l.To.Lat-l.From.Lat)*t, l.From.Lon + (l.To.Lon-l.From.Lon)*t}, Speed: l.Speed, Course: course(l.From, l.To)}, false
		}
		elapsed -= d
	}
	return Fix{Point: r.Legs[len(r.Legs)-1].To, Course: r.courseBefore(len(r.Legs))}, true
}

// courseBefore is the way the phone faced on the last leg before leg i that moved it.
func (r Route) courseBefore(i int) float64 {
	for j := min(i, len(r.Legs)) - 1; j >= 0; j-- {
		if l := r.Legs[j]; l.Speed > 0 {
			return course(l.From, l.To)
		}
	}
	return 0
}

const earthRadius = 6371008.8 // metres, the mean radius

// distance is the great-circle distance between two points, in metres.
func distance(a, b Point) float64 {
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dla, dlo := la2-la1, (b.Lon-a.Lon)*math.Pi/180
	h := math.Sin(dla/2)*math.Sin(dla/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dlo/2)*math.Sin(dlo/2)
	return 2 * earthRadius * math.Asin(math.Min(1, math.Sqrt(h)))
}

// course is the initial bearing from a to b, in degrees clockwise from north.
func course(a, b Point) float64 {
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dlo := (b.Lon - a.Lon) * math.Pi / 180
	y := math.Sin(dlo) * math.Cos(la2)
	x := math.Cos(la1)*math.Sin(la2) - math.Sin(la1)*math.Cos(la2)*math.Cos(dlo)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

// A speed is how fast a phone travels, or how much faster than a GPX file's own times.
type speed struct {
	metresPerSecond float64 // 0 when it is a pace
	pace            float64 // times the file's own pace; 0 when it is a speed
}

var speedPattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)(?: ?(mph|km/h|m/s)|(x))$`)

func parseSpeed(s string) (speed, error) {
	m := speedPattern.FindStringSubmatch(s)
	if m == nil {
		return speed{}, fmt.Errorf("%q is not a speed, like 25 mph, 40 km/h, 11 m/s or 4x", s)
	}
	n, _ := strconv.ParseFloat(m[1], 64)
	if n <= 0 {
		return speed{}, fmt.Errorf("the speed %q is not more than nothing", s)
	}
	switch m[2] {
	case "mph":
		return speed{metresPerSecond: n * 0.44704}, nil
	case "km/h":
		return speed{metresPerSecond: n / 3.6}, nil
	case "m/s":
		return speed{metresPerSecond: n}, nil
	}
	return speed{pace: n}, nil
}

var speedParam = core.ParamType{
	Name:     "speed",
	Regexps:  []string{`\d+(?:\.\d+)? ?(?:mph|km/h|m/s)|\d+(?:\.\d+)?x`},
	Doc:      "how fast a phone travels: `25 mph`, `40 km/h` or `11 m/s`; or, for a GPX file with times, how many times its own pace: `4x`",
	Examples: []string{"25 mph", "40 km/h", "4x"},
}

var travelParam = core.ParamType{
	Name:     "travel",
	Regexps:  []string{`travels|starts traveling`},
	Doc:      "whether the step waits for the phone to arrive (`travels`), or lets the scenario go on while it moves (`starts traveling`)",
	Values:   []string{"travels", "starts traveling"},
	Examples: []string{"travels", "starts traveling"},
}

// tableRoute is the route through a table's points, each a latitude and a longitude.
func tableRoute(t *core.Table, s speed) (Route, error) {
	if s.pace > 0 {
		return Route{}, errors.New("a route through a table's points has no times to go faster than: give a speed, like 25 mph")
	}
	if t == nil || len(t.Rows) < 2 {
		return Route{}, errors.New("a route needs at least two points: one row each, a latitude and a longitude, like | 41.5868 | -93.6510 |")
	}
	var points []Point
	for i, row := range t.Rows {
		if len(row) != 2 {
			return Route{}, fmt.Errorf("row %d has %d cells: a point is a latitude and a longitude, like | 41.5868 | -93.6510 |", i+1, len(row))
		}
		p, err := parsePoint(row[0], row[1])
		if err != nil {
			return Route{}, fmt.Errorf("row %d: %w", i+1, err)
		}
		points = append(points, p)
	}
	return steady(points, s.metresPerSecond), nil
}

func parsePoint(lat, lon string) (Point, error) {
	la, err1 := strconv.ParseFloat(strings.TrimSpace(lat), 64)
	lo, err2 := strconv.ParseFloat(strings.TrimSpace(lon), 64)
	if err1 != nil || err2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
		return Point{}, fmt.Errorf("%q, %q is not a latitude and a longitude, like 41.5868, -93.6510", lat, lon)
	}
	return Point{la, lo}, nil
}

// steady is the route through points at one speed. Points that repeat the one before add no leg.
func steady(points []Point, metresPerSecond float64) Route {
	var r Route
	for i := 1; i < len(points); i++ {
		if distance(points[i-1], points[i]) > 0 {
			r.Legs = append(r.Legs, leg(points[i-1], points[i], metresPerSecond))
		}
	}
	return r
}

// gpx is what axx reads of a GPX file: its tracks' points, else its routes', else its waypoints.
type gpx struct {
	Tracks []struct {
		Segments []struct {
			Points []gpxPoint `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
	Routes []struct {
		Points []gpxPoint `xml:"rtept"`
	} `xml:"rte"`
	Waypoints []gpxPoint `xml:"wpt"`
}

type gpxPoint struct {
	Lat  string `xml:"lat,attr"`
	Lon  string `xml:"lon,attr"`
	Time string `xml:"time"`
}

// gpxRoute is the route a GPX file records. At a speed, the phone keeps to it; otherwise it keeps
// to the file's times, as fast as they say or a pace times faster.
func gpxRoute(path string, s *speed) (Route, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Route{}, fmt.Errorf("cannot read the route: %w", err)
	}
	var doc gpx
	if err := xml.Unmarshal(b, &doc); err != nil {
		return Route{}, fmt.Errorf("%s is not a GPX file: %w", path, err)
	}
	var raw []gpxPoint
	for _, t := range doc.Tracks {
		for _, seg := range t.Segments {
			raw = append(raw, seg.Points...)
		}
	}
	if len(raw) == 0 {
		for _, r := range doc.Routes {
			raw = append(raw, r.Points...)
		}
	}
	if len(raw) == 0 {
		raw = doc.Waypoints
	}
	if len(raw) < 2 {
		return Route{}, fmt.Errorf("%s has %d points: a route needs at least two (track points, route points or waypoints)", path, len(raw))
	}
	points := make([]Point, len(raw))
	for i, p := range raw {
		if points[i], err = parsePoint(p.Lat, p.Lon); err != nil {
			return Route{}, fmt.Errorf("%s, point %d: %w", path, i+1, err)
		}
	}
	if s != nil && s.metresPerSecond > 0 {
		return steady(points, s.metresPerSecond), nil
	}
	pace := 1.0
	if s != nil {
		pace = s.pace
	}
	times := make([]time.Time, len(raw))
	for i, p := range raw {
		if times[i], err = time.Parse(time.RFC3339, strings.TrimSpace(p.Time)); err != nil {
			return Route{}, fmt.Errorf("%s, point %d has no time (%q): give a speed, like at 25 mph", path, i+1, p.Time)
		}
	}
	var r Route
	for i := 1; i < len(points); i++ {
		dist, dt := distance(points[i-1], points[i]), times[i].Sub(times[i-1]).Seconds()/pace
		if dt < 0 {
			return Route{}, fmt.Errorf("%s, point %d is earlier than the point before it", path, i+1)
		}
		switch {
		case dist == 0 && dt == 0:
			continue
		case dist == 0: // a stop, where the phone stands still for a while
			r.Legs = append(r.Legs, Leg{From: points[i-1], To: points[i], Time: time.Duration(dt * float64(time.Second))})
		case dt == 0:
			return Route{}, fmt.Errorf("%s, points %d and %d are apart at the same moment", path, i, i+1)
		default:
			r.Legs = append(r.Legs, leg(points[i-1], points[i], dist/dt))
		}
	}
	if r.length() == 0 {
		return Route{}, fmt.Errorf("%s never moves: its points are all one place", path)
	}
	return r, nil
}

// A journey is a phone on its route, or on a path through the scene its camera sees: when it
// should arrive, and when it has.
type journey struct {
	arrived <-chan struct{}
	due     time.Time
	// path is the scene's path the phone travels, if it travels one; failed, once it has
	// arrived, is why it stopped short.
	path   string
	failed *error
}

// journeys are the routes a scenario's phones are on, by app.
var journeys = core.NewStateKey("mobile-core.journeys", func(*core.Scenario) *map[string]journey {
	m := map[string]journey{}
	return &m
}, nil)

// travel has the app's phone travel the route, and waits for it to arrive unless the step lets
// the scenario go on.
func travel(sc *core.Scenario, app string, r Route, wait bool) error {
	return onDevice(sc, app, false, func(ctx context.Context, d Device) error {
		t, ok := d.(Traveler)
		if !ok {
			return fmt.Errorf("the %s app's device cannot travel a route", app)
		}
		arrived, err := t.Travel(ctx, r)
		if err != nil {
			return err
		}
		(*journeys.Of(sc))[app] = journey{arrived: arrived, due: time.Now().Add(r.Duration())}
		sc.Log("the %s app's phone travels %.0f m in %s", app, r.length(), r.Duration().Round(time.Second))
		if !wait {
			return nil
		}
		select {
		case <-arrived:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

func (r Route) length() float64 {
	var m float64
	for _, l := range r.Legs {
		m += distance(l.From, l.To)
	}
	return m
}

func routeSteps() []core.StepDef {
	return []core.StepDef{{
		ID: "mobile-core.travel-through", Keyword: "When", Arg: core.ArgTable, Since: since,
		Expr: "the {word} app's phone {travel} at {speed} through:",
		Doc: "Move the phone along a route, as its GPS reports a person walking or driving it: from the first point of the table " +
			"to the last, each a latitude and a longitude, at a steady speed. The app sees where the phone is, how fast it moves " +
			"and which way. `travels` waits for the phone to arrive; `starts traveling` lets the scenario go on while it moves, " +
			"and a later step waits for it to reach the end. The phone stays at the last point.",
		Table: &core.TableDoc{Columns: []string{"latitude", "longitude"}, Note: "A row is a point the route goes through, in order."},
		Examples: []string{"When the courier app's phone travels at 30 km/h through:\n" +
			"  | 51.3397 | 12.3731 |\n" +
			"  | 51.3397 | 12.3760 |"},
		Run: func(sc *core.Scenario, a core.Args) error {
			s, err := parseSpeed(a.String(2))
			if err != nil {
				return err
			}
			r, err := tableRoute(a.Table, s)
			if err != nil {
				return err
			}
			return travel(sc, a.String(0), r, a.String(1) == "travels")
		},
	}, {
		ID: "mobile-core.travel-route", Keyword: "When", Since: since,
		Expr: "the {word} app's phone {travel} the {filepath} route[[ at {speed}]]",
		Doc: "Move the phone along a GPX file's route, as its GPS reports it: its track, else its route, else its waypoints. " +
			"With no speed, the phone keeps to the file's own times; `at 4x` goes four times as fast; `at 25 mph` keeps one " +
			"speed throughout. `travels` waits for the phone to arrive; `starts traveling` lets the scenario go on while it moves.",
		Examples: []string{"When the courier app's phone travels the routes/grimmaische-strasse.gpx route", "When the courier app's phone starts traveling the routes/grimmaische-strasse.gpx route at 4x"},
		Run: func(sc *core.Scenario, a core.Args) error {
			path, err := sc.ResolvePath(a.String(2))
			if err != nil {
				return err
			}
			var s *speed
			if a.Present(3) {
				v, err := parseSpeed(a.String(3))
				if err != nil {
					return err
				}
				s = &v
			}
			r, err := gpxRoute(path, s)
			if err != nil {
				return err
			}
			return travel(sc, a.String(0), r, a.String(1) == "travels")
		},
	}, {
		ID: "mobile-core.route-end", Keyword: "Then", Since: since,
		Expr: "[[within {duration} ]]the {word} app's phone reaches the end of its route",
		Doc: "Wait for the phone a `starts traveling` step set off to arrive at the last point of its route, or the end " +
			"of the scene's path it travels (the route's own time and 10 seconds, or `within`).",
		Examples: []string{"Then within 2m the courier app's phone reaches the end of its route"},
		Run: func(sc *core.Scenario, a core.Args) error {
			app := a.String(1)
			j, ok := (*journeys.Of(sc))[app]
			if !ok {
				return fmt.Errorf("the %s app's phone is on no route: a step like \"the %s app's phone starts traveling ...\" sets it off", app, app)
			}
			wait := time.Until(j.due) + cloudstep.DefaultWait
			if a.Present(0) {
				wait = a.Value(0).(time.Duration)
			}
			select {
			case <-j.arrived:
				if j.failed != nil && *j.failed != nil {
					return fmt.Errorf("the %s app's phone stopped short of the end of the scene's %q path: %w", app, j.path, *j.failed)
				}
				return nil
			case <-time.After(wait):
				return core.Failf("The %s app's phone did not reach the end of its route within %s", app, wait.Round(time.Second))
			case <-sc.Context().Done():
				return sc.Context().Err()
			}
		},
	}}
}
