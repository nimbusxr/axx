package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// Management requests go to the management endpoint, keeping their path.
func TestRelocate(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL + "/devstoreaccount1-servicebus")
	pl := runtime.NewPipeline("test", "v1", runtime.PipelineOptions{}, &policy.ClientOptions{PerRetryPolicies: []policy.Policy{relocate{base}}})
	req, err := runtime.NewRequest(t.Context(), http.MethodGet, "https://customs.servicebus.windows.net/customs-events")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := pl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got != "/devstoreaccount1-servicebus/customs-events" {
		t.Errorf("path %s", got)
	}
}

// Every line of the invoice owes duty on its value; the duties due are
// their total.
func TestAssessDuties(t *testing.T) {
	inv := Invoice{Currency: "EUR", Total: 480, Items: []Item{
		{Description: "Leather boots", HSCode: "640391", Quantity: 2, Value: 360},
		{Description: "Wool scarf", HSCode: "611710", Quantity: 1, Value: 120},
	}}
	lines, total := assessDuties(inv, 0.20)
	if total != 96 {
		t.Errorf("total %v, want 96", total)
	}
	want := "hs_code,description,quantity,value,duty,currency\n" +
		"640391,Leather boots,2,360.00,72.00,EUR\n" +
		"611710,Wool scarf,1,120.00,24.00,EUR\n"
	if got := string(dutiesCSV(lines, inv.Currency)); got != want {
		t.Errorf("breakdown:\n%s\nwant:\n%s", got, want)
	}
	if _, total := assessDuties(Invoice{Currency: "EUR", Total: 200}, 0.20); total != 40 {
		t.Errorf("an invoice without items: %v, want 40", total)
	}
}

func TestCertificateText(t *testing.T) {
	at := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	text := string(certificateText(Filing{Declaration: "DEC-7101", Parcel: "PX-7101"}, Invoice{Currency: "EUR", Total: 45}, 150, at))
	for _, want := range []string{
		"Declaration  DEC-7101\n",
		"Cleared at   2026-09-25T10:00:00Z\n",
		"Parcel PX-7101 is cleared for import without duties: its value of 45.00 EUR is within the de minimis limit of 150.00 EUR.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the certificate does not say %q:\n%s", want, text)
		}
	}
}
