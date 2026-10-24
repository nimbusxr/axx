package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
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

// A 409 Conflict is an entity that exists only when the entity is there: the
// emulator also answers it while another operation on the entity is in
// progress, and the entity must still be created then.
func TestEnsureCreatesTheEntityUntilItExists(t *testing.T) {
	provisionRetry = time.Millisecond
	t.Cleanup(func() { provisionRetry = time.Second })
	conflict := errors.New("409 Conflict: another conflicting operation is in progress")

	created, creates := false, 0
	err := ensure(t.Context(), "queue customs-filings",
		func() error {
			creates++
			if creates < 3 {
				return conflict
			}
			created = true
			return nil
		},
		func() (bool, error) { return created, nil })
	if err != nil || !created || creates != 3 {
		t.Errorf("err %v, created %v after %d creates", err, created, creates)
	}

	// An entity that exists is kept.
	creates = 0
	err = ensure(t.Context(), "queue customs-filings",
		func() error { creates++; return conflict },
		func() (bool, error) { return true, nil })
	if err != nil || creates != 1 {
		t.Errorf("err %v after %d creates", err, creates)
	}

	// One that never gets created fails provisioning, with what it is.
	provisionTimeout = 20 * time.Millisecond
	t.Cleanup(func() { provisionTimeout = 3 * time.Minute })
	err = ensure(t.Context(), "queue customs-filings",
		func() error { return conflict },
		func() (bool, error) { return false, nil })
	if err == nil || !strings.Contains(err.Error(), "queue customs-filings: 409 Conflict") {
		t.Errorf("err %v", err)
	}
}

// An inspection photo is laid out as the classifier takes it: its middle
// square at 224×224, BGR, channels first, from 0 to 255.
func TestAnInspectionPhotoIsLaidOutAsTheModelTakesIt(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := range 480 {
		for x := range 640 {
			c := color.RGBA{R: 200, G: 100, B: 50, A: 255}
			if x < 80 || x >= 560 {
				c = color.RGBA{A: 255} // outside the middle square
			}
			img.SetRGBA(x, y, c)
		}
	}
	var photo bytes.Buffer
	if err := png.Encode(&photo, img); err != nil {
		t.Fatal(err)
	}
	pixels, err := modelInput(photo.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if b, g, r := pixels[0][0][0], pixels[1][112][112], pixels[2][223][223]; b != 50 || g != 100 || r != 200 {
		t.Errorf("BGR %v %v %v, want 50 100 200", b, g, r)
	}
	if _, err := modelInput([]byte("not a picture")); err == nil {
		t.Error("what is not a picture is laid out")
	}
}

// What an inspection photo shows is declared when an invoice's line is of
// a tariff chapter of its goods.
func TestAPhotoIsOfTheGoodsAnInvoiceDeclares(t *testing.T) {
	toys := Invoice{Items: []Item{{Description: "Plush teddy bear", HSCode: "950300"}}}
	for label, want := range map[string][2]bool{
		"teddy": {true, true}, "cellular_telephone": {false, true},
	} {
		if declared, known := declares(toys, label); declared != want[0] || known != want[1] {
			t.Errorf("%s: declared %v, known %v", label, declared, known)
		}
	}
	if _, known := declares(Invoice{Items: []Item{{HSCode: "490199"}}}, "teddy"); known {
		t.Error("books' chapter has classes")
	}
}
