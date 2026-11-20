package courierapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// Each call the app makes is kept with the status it was answered with, for a failed test to
// tell: the status a handler sets, and 200 for one that only writes.
func TestTheCallsTheAppMadeAreKept(t *testing.T) {
	a := &API{}
	if got := a.asked(); got != " nothing" {
		t.Errorf("an API no app called was asked%s", got)
	}
	h := a.recorded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/couriers/sign-in" {
			w.WriteHeader(http.StatusUnauthorized)
		}
		_, _ = w.Write([]byte("{}"))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/couriers/sign-in", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/couriers/CR-LEJ-12/deliveries?day=today", nil))
	requests := a.Requests()
	if len(requests) != 2 {
		t.Fatalf("kept %d calls, want 2: %v", len(requests), requests)
	}
	for i, want := range []Request{
		{Method: "POST", Path: "/api/couriers/sign-in", Status: 401},
		{Method: "GET", Path: "/api/couriers/CR-LEJ-12/deliveries", Status: 200},
	} {
		if got := requests[i]; got.Method != want.Method || got.Path != want.Path || got.Status != want.Status || got.At.IsZero() {
			t.Errorf("call %d is kept as %+v, want %+v at the time it came", i+1, got, want)
		}
	}
	at := requests[0].At.Format("15:04:05.000")
	if got := a.asked(); !strings.HasPrefix(got, "\n  "+at+" POST /api/couriers/sign-in: 401\n  ") || !strings.HasSuffix(got, " GET /api/couriers/CR-LEJ-12/deliveries: 200") {
		t.Errorf("the calls are told as%s", got)
	}
	// An app that reports the van's location every second: the last 60 calls are told.
	for range 70 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/couriers/CR-LEJ-12/location", nil))
	}
	got := a.asked()
	if !strings.HasPrefix(got, "\n  (12 calls before these)\n") || strings.Count(got, "/location: 200") != 60 || strings.Contains(got, "sign-in") {
		t.Errorf("72 calls are told as%s", got)
	}
}
