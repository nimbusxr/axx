package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A courier signs in with their ID and PIN, and their token opens their own
// deliveries only.
func TestCourierSignIn(t *testing.T) {
	s := &service{
		courierTokenKey: []byte("test-courier-token-key"),
		couriers:        map[string]courierAccount{"CR-LEJ-12": {name: "Hanna Wolf", pin: "4711"}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/couriers/sign-in", s.courierSignIn)
	mux.HandleFunc("GET /api/couriers/{courier}/check", func(w http.ResponseWriter, r *http.Request) {
		if courier, ok := s.courierOf(w, r); ok {
			_, _ = w.Write([]byte(courier))
		}
	})
	signIn := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/couriers/sign-in", strings.NewReader(body)))
		return rec
	}
	for body, want := range map[string]int{
		`{"courier": "CR-LEJ-12", "pin": "0000"}`: http.StatusUnauthorized,
		`{"courier": "CR-XXX-99", "pin": "4711"}`: http.StatusUnauthorized,
		`{"courier": "CR-LEJ-12"}`:                http.StatusBadRequest,
	} {
		if rec := signIn(body); rec.Code != want {
			t.Errorf("%s: %d, want %d", body, rec.Code, want)
		}
	}
	rec := signIn(`{"courier": "CR-LEJ-12", "pin": "4711"}`)
	var session struct{ Token, Courier, Name string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &session) != nil || session.Name != "Hanna Wolf" || session.Token == "" {
		t.Fatalf("sign-in: %d %s", rec.Code, rec.Body)
	}
	check := func(courier, token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/couriers/"+courier+"/check", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := check("CR-LEJ-12", session.Token); got != http.StatusOK {
		t.Errorf("own deliveries: %d", got)
	}
	if got := check("CR-DRS-07", session.Token); got != http.StatusForbidden {
		t.Errorf("another courier's deliveries: %d", got)
	}
	if got := check("CR-LEJ-12", ""); got != http.StatusUnauthorized {
		t.Errorf("no token: %d", got)
	}
}

// The courier's app reports where the van is: rounded to about 11 metres, its speed in km/h
// and its heading in degrees; the courier's token only.
func TestCourierLocation(t *testing.T) {
	s := &service{
		courierTokenKey: []byte("test-courier-token-key"),
		couriers:        map[string]courierAccount{"CR-LEJ-12": {name: "Hanna Wolf", pin: "4711"}},
	}
	mux := http.NewServeMux()
	s.courierRoutes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/couriers/sign-in", strings.NewReader(`{"courier": "CR-LEJ-12", "pin": "4711"}`)))
	var session struct{ Token string }
	_ = json.Unmarshal(rec.Body.Bytes(), &session)
	report := func(courier, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/couriers/"+courier+"/location", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+session.Token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := report("CR-LEJ-12", `{"latitude": 51.33969879, "longitude": 12.38000011, "speed": 8.3333, "course": 89.6}`); got != http.StatusNoContent {
		t.Fatalf("a location: %d", got)
	}
	pos, ok := s.vans.get("CR-LEJ-12")
	if !ok || pos.Latitude != 51.3397 || pos.Longitude != 12.38 || pos.SpeedKmh != 30 || pos.Heading != 90 {
		t.Errorf("the van: %+v %v", pos, ok)
	}
	for body, want := range map[string]int{
		`{"longitude": 12.38}`:                                  http.StatusBadRequest,
		`{"latitude": 91, "longitude": 12.38}`:                  http.StatusBadRequest,
		`{"latitude": 51.3, "longitude": 12.38, "course": 360}`: http.StatusBadRequest,
	} {
		if got := report("CR-LEJ-12", body); got != want {
			t.Errorf("%s: %d, want %d", body, got, want)
		}
	}
	if got := report("CR-DRS-07", `{"latitude": 51.0504, "longitude": 13.7373}`); got != http.StatusForbidden {
		t.Errorf("another courier's van: %d", got)
	}
}
