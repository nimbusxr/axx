//go:build integration

package mobileandroid

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
)

// The integration tests run the parcels example's couriers' app on an
// emulator axx starts: AXX_ANDROID_AVD, or parcels-pixel
// (../../../examples/parcels/courier/android/create-emulator.sh), with the
// app built (./gradlew assembleDebug there).

var courierAPK = filepath.Join("..", "..", "..", "examples", "parcels", "courier", "android", "app", "build", "outputs", "apk", "debug", "app-debug.apk")

func avd(t *testing.T) string {
	t.Helper()
	if _, err := findSDK(); err != nil {
		t.Skipf("no Android SDK: %v", err)
	}
	if name := os.Getenv("AXX_ANDROID_AVD"); name != "" {
		return name
	}
	return "parcels-pixel"
}

func apk(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(courierAPK)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the couriers' app is not built: run ./gradlew assembleDebug in examples/parcels/courier/android (%v)", err)
	}
	return p
}

// couriers stands in for the parcels service's couriers' API, where the
// emulator finds the service: 127.0.0.1:8400 on the host.
func couriers(t *testing.T) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:8400")
	if err != nil {
		t.Fatalf("the couriers' API stands in on 127.0.0.1:8400, which is taken (is the parcels example running?): %v", err)
	}
	var mu sync.Mutex
	delivered := map[string]bool{}
	type recipient struct{ Name, Street, City, Postcode, Country string }
	parcels := map[string]recipient{
		"PX-MOB-9401": {"Jonas Weber", "Karl-Liebknecht-Str. 12", "Leipzig", "04107", "DE"},
		"PX-MOB-9402": {"Lena Vogel", "Prager Str. 3", "Leipzig", "04103", "DE"},
	}
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
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func signIn(h *cloudtest.Harness) {
	h.OK(`the courier app is launched`)
	h.OK(`the "Sign in" button is disabled in the courier app`)
	h.OK(`the "Courier ID" field in the courier app is filled with "CR-LEJ-12"`)
	h.OK(`the "PIN" field in the courier app is filled with "${env:COURIER_PIN}"`)
	h.OK(`the "Sign in" button is tapped in the courier app`)
}

// A courier delivers a parcel on a real emulator: every step of the packs.
func TestACourierDeliversOnAnEmulator(t *testing.T) {
	couriers(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cloudtest.New(t, mobilecore.Pack(), Pack())
	h.OK("the courier android app with the following properties:", [][]string{
		{"apk", apk(t)}, {"device", avd(t)}, {"permissions", "POST_NOTIFICATIONS"}, {"timezone", "Europe/Berlin"}, {"locale", "en-GB"},
	})
	signIn(h)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	h.OK(`the "PX-MOB-9401" list item is tapped in the courier app`)
	h.OK(`the "Mark delivered" button is disabled in the courier app`)
	h.OK(`the "Signed by" field in the courier app is filled with "Jonas Weber"`)
	h.OK(`the "Mark delivered" button is tapped in the courier app`)
	h.OK(`the courier app shows "Mark PX-MOB-9401 delivered?"`)
	h.OK(`the "Confirm" button is tapped in the courier app`)
	h.OK(`the courier app shows "Delivered"`)
	h.OK(`the courier app shows a notification "PX-MOB-9401 delivered"`)
	h.OK(`the courier app does not show "PX-MOB-9401"`)
	h.OK("the courier app is swiped down")
	h.OK(`the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9402" link`)
	h.OK(`the courier app shows "Prager Str. 3, 04103 Leipzig"`)
	h.OK("the courier app's back button is pressed")
	h.OK(`the courier app shows "Today's deliveries"`)
	h.OK("the courier app is sent to the background")
	h.OK("the courier app is brought back")
	h.OK("the courier app is restarted")
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	for _, l := range h.Sink.Logs {
		if strings.Contains(l, "4711") {
			t.Errorf("the PIN is in the logs: %s", l)
		}
	}
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}

// Two scenarios on one device: the second starts signed out, without the
// permission the first had, and Android asks for it.
func TestScenariosOnADeviceAreIsolated(t *testing.T) {
	couriers(t)
	t.Setenv("COURIER_PIN", "4711")
	h := cloudtest.New(t, mobilecore.Pack(), Pack())
	h.OK("the courier android app with the following properties:", [][]string{{"apk", apk(t)}, {"device", avd(t)}, {"permissions", "POST_NOTIFICATIONS"}})
	signIn(h)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}

	h.NewScenario()
	h.OK("the courier android app with the following properties:", [][]string{{"apk", apk(t)}, {"device", avd(t)}})
	h.OK(`the courier app is launched`)
	h.OK(`the courier app shows "Sign in"`)
	h.OK(`the "Courier ID" field in the courier app has the value ""`)
	signIn(h)
	h.OK(`the courier app's dialog shows "send you notifications"`)
	h.OK(`the courier app's dialog is dismissed`)
	h.OK(`the courier app shows "Hello, Hanna Wolf"`)
	if err := h.End("passed"); err != nil {
		t.Fatal(err)
	}
}
