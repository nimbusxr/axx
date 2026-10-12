package mobilecore

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

func TestSpeeds(t *testing.T) {
	for in, want := range map[string]speed{
		"25 mph": {metresPerSecond: 11.176}, "40 km/h": {metresPerSecond: 40 / 3.6}, "11 m/s": {metresPerSecond: 11},
		"4x": {pace: 4}, "0.5x": {pace: 0.5}, "25mph": {metresPerSecond: 11.176},
	} {
		got, err := parseSpeed(in)
		if err != nil || math.Abs(got.metresPerSecond-want.metresPerSecond) > 1e-9 || got.pace != want.pace {
			t.Errorf("%s: got %+v %v, want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{"0 mph", "fast", "25 knots"} {
		if _, err := parseSpeed(in); err == nil {
			t.Errorf("%s is not a speed", in)
		}
	}
}

// Ingersoll Ave, westbound: 0.009 degrees of longitude at 41.5868 N is 749 m.
var ingersoll = &core.Table{Rows: [][]string{{"41.5868", "-93.6420"}, {"41.5868", "-93.6510"}}}

func TestARouteThroughATablesPoints(t *testing.T) {
	r, err := tableRoute(ingersoll, speed{metresPerSecond: 11.176})
	if err != nil {
		t.Fatal(err)
	}
	if m := r.length(); math.Abs(m-749) > 2 {
		t.Errorf("the route is %.1f m long, want 749", m)
	}
	if d := r.Duration(); d < 66*time.Second || d > 68*time.Second {
		t.Errorf("at 25 mph it takes %s, want 67s", d)
	}
	// Halfway, the phone is halfway, westbound (270°) at 25 mph.
	f, done := r.At(r.Duration() / 2)
	if done || math.Abs(f.Lon+93.6465) > 1e-6 || math.Abs(f.Lat-41.5868) > 1e-9 || math.Abs(f.Course-270) > 0.1 || f.Speed != 11.176 {
		t.Errorf("halfway: %+v done=%v", f, done)
	}
	// At the end it stays at the last point, still facing west, not moving.
	f, done = r.At(time.Hour)
	if !done || f.Point != (Point{41.5868, -93.6510}) || f.Speed != 0 || math.Abs(f.Course-270) > 0.1 {
		t.Errorf("after: %+v done=%v", f, done)
	}
	for _, bad := range []*core.Table{{Rows: [][]string{{"41.5868", "-93.6420"}}}, {Rows: [][]string{{"41.5868", "-93.6420"}, {"north", "-93.6510"}}}, {Rows: [][]string{{"41.5868"}, {"41.5868"}}}} {
		if _, err := tableRoute(bad, speed{metresPerSecond: 1}); err == nil {
			t.Errorf("%v is no route", bad.Rows)
		}
	}
	if _, err := tableRoute(ingersoll, speed{pace: 4}); err == nil || !strings.Contains(err.Error(), "give a speed") {
		t.Errorf("a table has no times to go faster than: %v", err)
	}
}

func writeGPX(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "route.gpx")
	if err := os.WriteFile(p, []byte(`<?xml version="1.0"?><gpx version="1.1" creator="axx" xmlns="http://www.topografix.com/GPX/1/1">`+body+`</gpx>`), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A striping truck on Fleur Dr: northbound 1 km in 2 minutes, a minute's stop, then 500 m in a
// minute.
const fleur = `<trk><name>Fleur Dr</name><trkseg>
<trkpt lat="41.5600" lon="-93.6420"><time>2026-10-05T12:00:00Z</time></trkpt>
<trkpt lat="41.56899" lon="-93.6420"><time>2026-10-05T12:02:00Z</time></trkpt>
<trkpt lat="41.56899" lon="-93.6420"><time>2026-10-05T12:03:00Z</time></trkpt>
<trkpt lat="41.573487" lon="-93.6420"><time>2026-10-05T12:04:00Z</time></trkpt>
</trkseg></trk>`

func TestAGPXRouteKeepsToItsTimes(t *testing.T) {
	path := writeGPX(t, fleur)
	r, err := gpxRoute(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Legs) != 3 || math.Abs(r.Legs[0].Speed-1000.0/120) > 0.05 || r.Legs[1].Speed != 0 || math.Abs(r.Legs[2].Speed-500.0/60) > 0.05 {
		t.Fatalf("legs: %+v", r.Legs)
	}
	if d := r.Duration(); d != 4*time.Minute {
		t.Errorf("the route takes %s, as long as the file says: 4m", d)
	}
	if f, _ := r.At(time.Minute); math.Abs(f.Course) > 0.1 || f.Speed == 0 {
		t.Errorf("northbound: %+v", f)
	}
	// At the stop the phone stands still, facing the way it came.
	if f, done := r.At(150 * time.Second); done || f.Speed != 0 || f.Point != (Point{41.56899, -93.6420}) || math.Abs(f.Course) > 0.1 {
		t.Errorf("stopped: %+v done=%v", f, done)
	}
	// Four times its pace: the whole of it, stop and all, in a minute.
	fast, err := gpxRoute(path, &speed{pace: 4})
	if err != nil || fast.Duration() != time.Minute {
		t.Errorf("at 4x: %s %v", fast.Duration(), err)
	}
	// At one speed, the phone keeps it throughout, and does not stop.
	steadyRoute, err := gpxRoute(path, &speed{metresPerSecond: 15})
	if err != nil || len(steadyRoute.Legs) != 2 || steadyRoute.Legs[0].Speed != 15 || steadyRoute.Legs[1].Speed != 15 {
		t.Errorf("at 15 m/s: %+v %v", steadyRoute.Legs, err)
	}
}

func TestGPXRoutesWaypointsAndTheirFaults(t *testing.T) {
	r, err := gpxRoute(writeGPX(t, `<rte><rtept lat="41.5868" lon="-93.6420"/><rtept lat="41.5868" lon="-93.6510"/></rte>`), &speed{metresPerSecond: 10})
	if err != nil || len(r.Legs) != 1 {
		t.Errorf("route points: %+v %v", r, err)
	}
	if _, err := gpxRoute(writeGPX(t, `<wpt lat="41.5868" lon="-93.6420"/><wpt lat="41.5868" lon="-93.6510"/>`), nil); err == nil || !strings.Contains(err.Error(), "give a speed") {
		t.Errorf("waypoints without times need a speed: %v", err)
	}
	for body, want := range map[string]string{
		`<wpt lat="41.5868" lon="-93.6420"/>`: "at least two",
		`<trk><trkseg><trkpt lat="41.5868" lon="-93.6420"><time>2026-10-05T12:01:00Z</time></trkpt><trkpt lat="41.5869" lon="-93.6420"><time>2026-10-05T12:00:00Z</time></trkpt></trkseg></trk>`: "earlier than",
		`<trk><trkseg><trkpt lat="41.5868" lon="-93.6420"><time>2026-10-05T12:00:00Z</time></trkpt><trkpt lat="41.5868" lon="-93.6420"><time>2026-10-05T12:01:00Z</time></trkpt></trkseg></trk>`: "never moves",
		`<trk><trkseg><trkpt lat="95" lon="-93.6420"/><trkpt lat="41.5868" lon="-93.6420"/></trkseg></trk>`:                                                                                      "not a latitude",
	} {
		if _, err := gpxRoute(writeGPX(t, body), nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", body, want, err)
		}
	}
	if _, err := gpxRoute(filepath.Join(t.TempDir(), "none.gpx"), nil); err == nil {
		t.Error("a missing file")
	}
}
