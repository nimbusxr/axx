package main

import (
	"testing"
	"time"
)

func TestPriceQuote(t *testing.T) {
	tests := []struct {
		zone, country, level string
		grams                int
		cents, days          int
	}{
		{"DE-1", "DE", "STANDARD", 500, 490, 2},
		{"DE-1", "DE", "STANDARD", 1200, 690, 2},
		{"DE-1", "DE", "STANDARD", 7500, 890, 2},
		{"DE-1", "DE", "STANDARD", 25000, 1290, 2},
		{"FR-1", "FR", "EXPRESS", 800, 1790, 2},
		{"DE-REMOTE", "DE", "STANDARD", 1200, 990, 3},
	}
	for _, tt := range tests {
		q := priceQuote(tt.zone, tt.country, tt.level, tt.grams)
		if q.PriceCents != tt.cents || q.DeliveryDays != tt.days || q.Currency != "EUR" {
			t.Errorf("%+v: got %d cents, %d days", tt, q.PriceCents, q.DeliveryDays)
		}
	}
}

func TestSummarize(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	scans := []scan{
		{ScanID: "s2", Status: "IN_TRANSIT", Location: "Hamburg hub", ScannedAt: at("2026-05-04T09:00:00Z")},
		{ScanID: "s1", Status: "PICKED_UP", Location: "Berlin depot", ScannedAt: at("2026-05-03T16:00:00Z")},
		{ScanID: "s2", Status: "IN_TRANSIT", Location: "Hamburg hub", ScannedAt: at("2026-05-04T09:00:00Z")},
	}
	v := summarize("PX-1", scans)
	if v.Status != "IN_TRANSIT" || v.ScanCount != 2 || v.Delivered || *v.LastLocation != "Hamburg hub" {
		t.Fatalf("summary = %+v", v)
	}
	if v := summarize("PX-2", nil); v.Status != "REGISTERED" || v.ScanCount != 0 || v.LastLocation != nil {
		t.Fatalf("empty summary = %+v", v)
	}
}

func TestLuhnCheckDigit(t *testing.T) {
	if got := luhnCheckDigit("7992739871"); got != 3 {
		t.Fatalf("check digit = %d, want 3", got)
	}
}
