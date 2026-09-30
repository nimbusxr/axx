// Package courierapi stands in for the parcels service's couriers' API in
// the mobile packs' integration tests, where the courier apps find it:
// 127.0.0.1:8400 on the host.
package courierapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Start serves the couriers' API for a test: the courier CR-LEJ-12 (PIN
// 4711) signs in, and delivers PX-MOB-9401 and PX-MOB-9402. The apps call
// 127.0.0.1:8400, and only one test at a time can serve it: the Android and
// the iOS packs' tests, run at once, take turns.
func Start(t *testing.T) {
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
