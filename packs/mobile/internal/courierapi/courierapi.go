// Package courierapi stands in for the parcels service's couriers' API in
// the mobile packs' integration tests, where the courier apps find it:
// 127.0.0.1:8400 on the host.
package courierapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// A Location is where the courier's app said the van was.
type Location struct {
	Latitude, Longitude float64
	Speed, Course       *float64 // metres a second, degrees; nil when the app did not know
}

// A Document is a file the courier's app attached to a delivery.
type Document struct {
	Reference, Name, ContentType string
	Content                      []byte
}

// API is the couriers' API a test serves.
type API struct {
	mu        sync.Mutex
	locations []Location
	documents []Document
	lists     int
}

// Documents are the files the courier's app attached to deliveries, in order.
func (a *API) Documents() []Document {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Document(nil), a.documents...)
}

// SafePlaceAuthorization is a document a recipient sends their courier: Jonas Weber's leave to
// put PX-MOB-9401 in his garage, a PDF.
func SafePlaceAuthorization(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "testdata", "safe-place-PX-MOB-9401.pdf")
}

// Label is a photo of PX-MOB-9402's label, as the phone held over the parcel takes it: its QR
// code above its reference (make-labels.swift in the parcels example writes it).
func Label(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "testdata", "label-PX-MOB-9402.png")
}

// TornLabel is a video of PX-MOB-9401's label, its code torn off, as the courier lifts the parcel
// into the phone's view: 3 seconds.
func TornLabel(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "testdata", "torn-label-PX-MOB-9401.mp4")
}

// Lists tells how many times the courier's app asked for the day's deliveries.
func (a *API) Lists() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lists
}

// Locations are where the courier's app said the van was, in order.
func (a *API) Locations() []Location {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Location(nil), a.locations...)
}

// Start serves the couriers' API for a test: the courier CR-LEJ-12 (PIN
// 4711) signs in, delivers PX-MOB-9401 and PX-MOB-9402, attaches documents
// to them, and reports where the van is. The apps call 127.0.0.1:8400, and only one test at a time can
// serve it: the Android and the iOS packs' tests, run at once, take turns.
func Start(t *testing.T) *API {
	t.Helper()
	var l net.Listener
	var err error
	for deadline := time.Now().Add(5 * time.Minute); ; time.Sleep(time.Second) {
		l, err = (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:8400")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the couriers' API stands in on 127.0.0.1:8400, which is taken (is the parcels example running?): %v", err)
		}
	}
	var mu sync.Mutex
	delivered := map[string]bool{}
	type recipient struct{ Name, Street, City, Postcode, Country string }
	parcels := map[string]recipient{
		"PX-MOB-9401": {"Jonas Weber", "Karl-Liebknecht-Str. 12", "Leipzig", "04107", "DE"},
		"PX-MOB-9402": {"Lena Vogel", "Prager Str. 3", "Leipzig", "04103", "DE"},
	}
	api := &API{}
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("POST /api/couriers/sign-in", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Courier, PIN string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Courier != "CR-LEJ-12" || in.PIN != "4711" {
			reply(w, http.StatusUnauthorized, map[string]string{"detail": "the courier ID or the PIN is wrong"})
			return
		}
		reply(w, http.StatusOK, map[string]any{"token": "t", "courier": in.Courier, "name": "Hanna Wolf", "expiresIn": 43200})
	})
	mux.HandleFunc("GET /api/couriers/{courier}/deliveries", func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		api.lists++
		api.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		out := []map[string]any{}
		for _, ref := range []string{"PX-MOB-9401", "PX-MOB-9402"} {
			if !delivered[ref] {
				p := parcels[ref]
				out = append(out, map[string]any{"reference": ref, "serviceLevel": "STANDARD", "recipient": map[string]string{
					"name": p.Name, "street": p.Street, "city": p.City, "postcode": p.Postcode, "country": p.Country,
				}})
			}
		}
		reply(w, http.StatusOK, map[string]any{"courier": r.PathValue("courier"), "deliveries": out})
	})
	mux.HandleFunc("POST /api/couriers/{courier}/deliveries/{reference}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		ref := r.PathValue("reference")
		if delivered[ref] {
			reply(w, http.StatusConflict, map[string]string{"detail": ref + " is not out for delivery"})
			return
		}
		delivered[ref] = true
		reply(w, http.StatusOK, map[string]string{"reference": ref, "status": "DELIVERED", "signedBy": "Jonas Weber", "location": parcels[ref].City})
	})
	mux.HandleFunc("POST /api/couriers/{courier}/location", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Latitude, Longitude *float64
			Speed, Course       *float64
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Latitude == nil || in.Longitude == nil {
			reply(w, http.StatusBadRequest, map[string]string{"detail": "a location is a latitude and a longitude"})
			return
		}
		api.mu.Lock()
		api.locations = append(api.locations, Location{*in.Latitude, *in.Longitude, in.Speed, in.Course})
		api.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /api/couriers/{courier}/deliveries/{reference}/documents/{name}", func(w http.ResponseWriter, r *http.Request) {
		content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
		if err != nil || len(content) == 0 {
			reply(w, http.StatusBadRequest, map[string]string{"detail": "a document is a file of at most 10 MB"})
			return
		}
		ref, name := r.PathValue("reference"), r.PathValue("name")
		api.mu.Lock()
		api.documents = append(api.documents, Document{ref, name, r.Header.Get("Content-Type"), content})
		api.mu.Unlock()
		reply(w, http.StatusCreated, map[string]any{"reference": ref, "name": name, "size": len(content)})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return api
}

// TheVanTravelled checks the courier's app reported the van travelling the route a test gave
// the phone: from one point to another, eastward or westward along a street, at a speed in
// metres a second. While it moved, the app said how fast and, if course, which way; it ended at
// the end. The report of the end comes a moment after the phone arrives (the fix reaches the app,
// and the app reports it), and longer on a busy machine: it is waited for, up to ten seconds.
func (a *API) TheVanTravelled(t *testing.T, from, to [2]float64, speed float64, course bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if locs := a.Locations(); len(locs) > 0 && at(locs[len(locs)-1], to) {
			break
		}
	}
	locs := a.Locations()
	heading := 90.0
	if to[1] < from[1] {
		heading = 270
	}
	moving := 0
	for _, l := range locs {
		if l.Speed == nil || *l.Speed < speed*0.9 || *l.Speed > speed*1.1 {
			continue
		}
		if l.Course != nil && math.Abs(*l.Course-heading) > 5 || course && l.Course == nil {
			t.Errorf("the van moved at %.1f m/s with a course of %v, want %.0f°", *l.Speed, l.Course, heading)
			continue
		}
		moving++
	}
	if moving < 5 {
		t.Errorf("the app reported the van moving at %.1f m/s %d times, want 5 or more: %s", speed, moving, describe(locs))
	}
	if len(locs) == 0 {
		return
	}
	if last := locs[len(locs)-1]; !at(last, to) {
		t.Errorf("the van ended at %.5f, %.5f, want %.5f, %.5f", last.Latitude, last.Longitude, to[0], to[1])
	}
}

// at is whether a location is within about 15 metres of a point: an emulator keeps a location at
// float32's precision.
func at(l Location, p [2]float64) bool {
	return math.Abs(l.Latitude-p[0]) <= 1.5e-4 && math.Abs(l.Longitude-p[1]) <= 2e-4
}

func describe(locs []Location) string {
	var b strings.Builder
	for _, l := range locs {
		fmt.Fprintf(&b, "\n  %.5f, %.5f", l.Latitude, l.Longitude)
		if l.Speed != nil {
			fmt.Fprintf(&b, " at %.1f m/s", *l.Speed)
		}
		if l.Course != nil {
			fmt.Fprintf(&b, " toward %.0f°", *l.Course)
		}
	}
	return b.String()
}
