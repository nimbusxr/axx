package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClaimID(t *testing.T) {
	if got := claimID("PX-4101"); got != "CLM-4101" {
		t.Errorf("claimID = %s", got)
	}
}

func TestFilingValidatesTheRequest(t *testing.T) {
	s := &service{log: slog.New(slog.DiscardHandler), now: time.Now}
	for body, want := range map[string]int{
		`{}`:                                 http.StatusBadRequest,
		`{"parcel":"PX-1","reason":"BORED"}`: http.StatusBadRequest,
		`not json`:                           http.StatusBadRequest,
	} {
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/claims", strings.NewReader(body)))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", body, rec.Code, want)
		}
	}
}
