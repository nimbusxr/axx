package courierapi

import (
	"testing"
	"time"
)

// The report of the route's end, a moment after the phone arrived, is the one checked.
func TestTheEndsReportIsWaitedFor(t *testing.T) {
	speed, course := 50/3.6, 90.0
	a := &API{}
	for _, lon := range []float64{12.3735, 12.3740, 12.3745, 12.3750, 12.3755} {
		a.locations = append(a.locations, Location{Latitude: 51.3397, Longitude: lon, Speed: &speed, Course: &course})
	}
	go func() {
		time.Sleep(time.Second)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.locations = append(a.locations, Location{Latitude: 51.3397, Longitude: 12.3760, Speed: &speed, Course: &course})
	}()
	a.TheVanTravelled(t, [2]float64{51.3397, 12.3731}, [2]float64{51.3397, 12.3760}, speed, true)
}
