package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/nimbusxr/axx/evals/internal/mutant"
)

func TestLuhnCheckDigit(t *testing.T) {
	// Well-known Luhn examples: payload -> check digit.
	for payload, want := range map[string]int{
		"7992739871": 3,
		"0000000000": 0,
		"0100000001": 6,
		"1234567890": 3,
	} {
		if got := luhnCheckDigit(payload); got != want {
			t.Errorf("luhnCheckDigit(%s) = %d, want %d", payload, got, want)
		}
	}
}

func TestLabel(t *testing.T) {
	p := &Parcel{Reference: "PX-LBL-1", ServiceLevel: "STANDARD", LabelNumber: 100000001}
	l := labeler{secret: []byte("evals-label-secret")}
	got := l.label(p)
	if got.Barcode != "PX01000000016" {
		t.Fatalf("barcode = %s", got.Barcode)
	}
	mac := hmac.New(sha256.New, []byte("evals-label-secret"))
	mac.Write([]byte("PX-LBL-1|PX01000000016"))
	if got.Signature != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("signature = %s", got.Signature)
	}
}

func TestAvro(t *testing.T) {
	e := parcelRegistered{Reference: "A", Sender: "s", WeightGrams: 1200, ServiceLevel: "STANDARD", Zone: "DE-1", Source: "api", RegisteredAt: 1}
	b := e.avro()
	want := []byte{2, 'A', 2, 's', 0xe0, 0x12, 16, 'S', 'T', 'A', 'N', 'D', 'A', 'R', 'D', 8, 'D', 'E', '-', '1', 6, 'a', 'p', 'i', 2}
	if !bytes.Equal(b, want) {
		t.Fatalf("avro = %v, want %v", b, want)
	}
	if got := avroLong(nil, -1); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("zigzag(-1) = %v", got)
	}
}

func TestSummarize(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 9, 1, 10, m, 0, 0, time.UTC) }
	scans := []scan{
		{ScanID: "s2", Status: "IN_TRANSIT", Location: "hub", ScannedAt: at(20)},
		{ScanID: "s1", Status: "PICKED_UP", Location: "shop", ScannedAt: at(10)},
		{ScanID: "s3", Status: "DELIVERED", Location: "door", ScannedAt: at(30)},
		{ScanID: "s2", Status: "IN_TRANSIT", Location: "hub", ScannedAt: at(20)},
	}
	v := summarize("R", scans, mutant.Set{})
	if v.Status != "DELIVERED" || *v.LastLocation != "door" || v.ScanCount != 3 || !v.Delivered {
		t.Fatalf("summary = %+v", v)
	}
	if v := summarize("R", scans, mutant.Set{"tracking-status-from-first-scan": true}); v.Status != "PICKED_UP" {
		t.Fatalf("first-scan mutant = %+v", v)
	}
	if v := summarize("R", scans, mutant.Set{"tracking-counts-duplicate-scans": true}); v.ScanCount != 4 {
		t.Fatalf("duplicate mutant = %+v", v)
	}
	if v := summarize("R", nil, mutant.Set{}); v.Status != "REGISTERED" || v.ScanCount != 0 {
		t.Fatalf("empty = %+v", v)
	}
}

func TestOpenAPIParses(t *testing.T) {
	var doc map[string]any
	if err := yaml.Unmarshal(openapiYAML, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %v", doc["openapi"])
	}
}

func TestMutantParse(t *testing.T) {
	s, unknown := mutant.Parse("no-weight-limit, bogus,")
	if !s.On("no-weight-limit") || len(unknown) != 1 || unknown[0] != "bogus" {
		t.Fatalf("parse = %v %v", s, unknown)
	}
}
