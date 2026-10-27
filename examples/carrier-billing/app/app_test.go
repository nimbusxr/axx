package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"
)

func TestInvoiceName(t *testing.T) {
	for object, want := range map[string][3]string{
		"kestrel/INV-2026-09-KES.csv": {"KESTREL", "INV-2026-09-KES", "ok"},
		"INV-1.csv":                   {"", "", ""},
		"kestrel/2026/INV-1.csv":      {"", "", ""},
		"kestrel/INV-1.pdf":           {"", "", ""},
	} {
		carrier, invoice, ok := invoiceName(object)
		if carrier != want[0] || invoice != want[1] || ok != (want[2] == "ok") {
			t.Errorf("%s: %s %s %v", object, carrier, invoice, ok)
		}
	}
}

func TestMoney(t *testing.T) {
	if got := money(cents(338)); got != "3.38" {
		t.Errorf("money = %s", got)
	}
}

// The letter names every disputed line, why it is disputed, and the credit
// note the carrier owes.
func TestDisputeLetter(t *testing.T) {
	letter := string(disputeLetter("INV-2026-09-KES2", "KESTREL", []Line{
		{Parcel: "PX-5103", Service: "express", Billed: 2.5, Expected: 1.35, Status: Overcharged},
		{Parcel: "PX-5199", Service: "express", Billed: 4.1, Status: UnknownShipment},
		{Parcel: "PX-5198", Service: "same-day", Billed: 6, Status: NoRate},
	}, 1125))
	for _, want := range []string{
		"To KESTREL, about invoice INV-2026-09-KES2",
		"we dispute 3 of its lines, 11.25 in all:",
		"PX-5103  billed 2.50, the agreed rate for the weight we measured is 1.35",
		"PX-5199  billed 4.10, a parcel we did not ship with you",
		"PX-5198  billed 6.00, we agreed no rate for the same-day service",
		"Please send us a credit note for 11.25.",
	} {
		if !strings.Contains(letter, want) {
			t.Errorf("the letter does not say %q:\n%s", want, letter)
		}
	}
}

// A repack's photo is laid out as the classifier takes it (its middle
// square at 224×224, 0..1, NHWC), and the most likely class of its logits,
// with their softmax, is what the photo shows.
func TestARepackPhotoIsLaidOutAsTheModelTakesIt(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := range 480 {
		for x := range 640 {
			c := color.RGBA{R: 255, G: 51, B: 0, A: 255}
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
	if len(pixels) != 224 || len(pixels[0]) != 224 || pixels[0][0] != [3]float32{1, 0.2, 0} || pixels[223][223] != [3]float32{1, 0.2, 0} {
		t.Errorf("pixels %v .. %v", pixels[0][0], pixels[223][223])
	}
	logits := make([]float64, 1001)
	logits[479] = 5 // carton
	if best, score := top(logits); best != 479 || math.Abs(score-math.Exp(5)/(math.Exp(5)+1000)) > 1e-9 {
		t.Errorf("class %d at %v", best, score)
	}
}
