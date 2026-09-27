package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestEstimateFor(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	tests := []struct {
		level, country, created, from, to string
	}{
		// Friday: two working days, over the weekend.
		{"STANDARD", "DE", "2026-09-25T15:00:00Z", "2026-09-29T09:00:00Z", "2026-09-29T18:00:00Z"},
		{"EXPRESS", "DE", "2026-09-24T08:00:00Z", "2026-09-25T09:00:00Z", "2026-09-25T12:00:00Z"},
		{"STANDARD", "FR", "2026-09-21T10:00:00Z", "2026-09-28T09:00:00Z", "2026-09-28T18:00:00Z"},
	}
	for _, tt := range tests {
		p := &Parcel{ServiceLevel: tt.level, Zone: tt.country + "-1", Recipient: Recipient{Country: tt.country}, WeightGrams: 800, CreatedAt: at(tt.created)}
		e := estimateFor(p)
		if !e.From.Equal(at(tt.from)) || !e.To.Equal(at(tt.to)) {
			t.Errorf("%+v: got %s to %s", tt, e.From, e.To)
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

func TestPortalPagesRender(t *testing.T) {
	q := priceQuote("DE-1", "DE", "STANDARD", 1200)
	p := &Parcel{
		Reference: "PX-WEB-1", Sender: "shop-example", Status: "REGISTERED", ServiceLevel: "EXPRESS", Zone: "DE-1",
		Recipient: Recipient{Name: "Anna Weber", Street: "Invalidenstr. 116", Postcode: "10115", City: "Berlin", Country: "DE"},
	}
	pages := map[string]portalPage{
		"quote":    {Title: "Get a quote", Quote: &q, Form: portalForm{Weight: "1200", Country: "DE", Postcode: "10115"}},
		"register": {Title: "Register a parcel", Error: "Enter the weight in grams", Form: portalForm{Service: "EXPRESS", Neighbour: true}},
		"parcel":   {Title: "Parcel PX-WEB-1 registered", Parcel: p},
		"parcels":  {Title: "Your parcels", Parcels: []*Parcel{p}},
	}
	want := map[string]string{
		"quote":    "Price: 6.90 EUR · delivered in 2 days",
		"register": `<p role="alert">Enter the weight in grams</p>`,
		"parcel":   "Leave with a neighbour: no",
		"parcels":  "<td>Anna Weber</td>",
	}
	for name, data := range pages {
		var b strings.Builder
		if err := portalPages[name].ExecuteTemplate(&b, name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(b.String(), want[name]) {
			t.Errorf("%s lacks %q:\n%s", name, want[name], b.String())
		}
	}
	for name, data := range map[string]any{
		"label": struct {
			*Parcel
			Barcode string
		}{p, "PX00000051013"},
		"track": struct{ Reference, Sender, Status string }{"PX-WEB-1", "shop-example", "in transit"},
	} {
		var b strings.Builder
		if err := portalPages[name].Execute(&b, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(b.String(), "PX-WEB-1") && !strings.Contains(b.String(), "Anna Weber") {
			t.Errorf("%s:\n%s", name, b.String())
		}
	}
	label := zpl(p, labeler{secret: []byte("example-label-secret")}.label(&Parcel{Reference: "PX-WEB-1", ServiceLevel: "EXPRESS", LabelNumber: 5101}))
	for _, want := range []string{"^XA\n", "^FDAnna Weber^FS", "^BCN,120,Y,N,N^FDPX0000005101", "^FDRef PX-WEB-1^FS", "^XZ\n"} {
		if !strings.Contains(label, want) {
			t.Errorf("the label lacks %q:\n%s", want, label)
		}
	}
	for v, want := range map[string]string{"": "Enter the weight in grams", "0": "Enter the weight in grams", "30001": "A parcel weighs at most 30 kg", "1200": ""} {
		if _, msg := portalWeight(v); msg != want {
			t.Errorf("portalWeight(%q) = %q, want %q", v, msg, want)
		}
	}
}

// The courier gets a JSON booking without the recipient's name or street,
// and a form for a pickup.
func TestCourierRequests(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.Path+" "+r.Header.Get("Content-Type")+" "+string(body))
		if q := r.URL.Query(); r.URL.Path == "/v1/collections" && (q.Get("slot") != "same-day" || len(q.Get("requestId")) != 16) {
			t.Errorf("the booking's query: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := &courierClient{base: srv.URL, http: srv.Client()}
	p := &Parcel{
		Reference: "PX-REG-1401", WeightGrams: 800, ServiceLevel: "EXPRESS",
		Recipient: Recipient{Name: "Ada Lovelace", Street: "Invalidenstrasse 116", Postcode: "10115", Country: "DE"},
	}
	if err := c.book(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := c.pickup(context.Background(), "PX-WEB-5401", "Friday"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`POST /v1/collections application/json {"reference":"PX-REG-1401","weightGrams":800,"deliverTo":{"postcode":"10115","country":"DE"}}`,
		"POST /v1/pickups application/x-www-form-urlencoded day=Friday&reference=PX-WEB-5401",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
