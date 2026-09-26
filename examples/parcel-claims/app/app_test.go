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

// The shop's staff read what was approved and what will be refunded.
func TestLetterText(t *testing.T) {
	c := Claim{ID: "CLM-4101", Parcel: "PX-4101", Reason: Damaged, Amount: 89.5, Currency: "EUR"}
	letter := string(letterText(c, Parcel{Reference: "PX-4101", Shop: "ACME-TOYS"}))
	for _, want := range []string{
		"To ACME-TOYS\nClaim CLM-4101 for parcel PX-4101\n",
		"We have approved your claim for parcel PX-4101, which was damaged in transit.",
		"We will refund the declared value of 89.50 EUR.",
	} {
		if !strings.Contains(letter, want) {
			t.Errorf("the letter does not say %q:\n%s", want, letter)
		}
	}
	c.Reason = Lost
	if letter := string(letterText(c, Parcel{Shop: "ACME-TOYS"})); !strings.Contains(letter, "which was lost in transit.") {
		t.Errorf("a lost parcel's letter:\n%s", letter)
	}
}

// Finance books the amount, with the shop it is paid to and the carrier.
func TestSettlement(t *testing.T) {
	c := Claim{ID: "CLM-4103", Parcel: "PX-4103", Reason: Lost, Amount: 45, Currency: "EUR"}
	got := string(settlement(c, Parcel{Reference: "PX-4103", Shop: "NORTHWIND", Carrier: "HERON"}))
	want := "claim,parcel,shop,carrier,reason,amount,currency\nCLM-4103,PX-4103,NORTHWIND,HERON,LOST,45.00,EUR\n"
	if got != want {
		t.Errorf("settlement:\n%s\nwant:\n%s", got, want)
	}
}
