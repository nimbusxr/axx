package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"math"
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

// The photo check reads the model's logits: the likeliest class, and whether
// it is a parcel's and likely enough.
func TestThePhotoCheckJudgesTheModelsScores(t *testing.T) {
	labels := []string{"tabby", "carton", "web site", "packet"}
	for _, tc := range []struct {
		logits []float32
		want   verdict
	}{
		{[]float32{0, 3, 0, 0}, verdict{Label: "carton", Parcel: true}},
		{[]float32{0, 0, 3, 0}, verdict{Label: "web site"}},
		{[]float32{0, 0.2, 0, 0}, verdict{Label: "carton"}}, // a parcel's, but unsure
	} {
		got := judge(tc.logits, labels)
		if got.Label != tc.want.Label || got.Parcel != tc.want.Parcel {
			t.Errorf("%v: %+v, want %+v", tc.logits, got, tc.want)
		}
	}
	if v := judge([]float32{0, math.Float32frombits(0x40400000), 0, 0}, labels); math.Abs(v.Score-0.8702) > 1e-3 {
		t.Errorf("score %v: the softmax of the logits", v.Score)
	}
}

// The model takes the middle square of a photo at 224×224, its channels
// normalized as ImageNet's, channel by channel.
func TestThePhotoIsTheModelsInput(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := range 200 {
		for x := range 300 {
			c := color.RGBA{R: 255, A: 255} // red in the middle square
			if x < 50 || x >= 250 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	in, err := modelInput(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 3*224*224 {
		t.Fatalf("%d values", len(in))
	}
	at := func(c, y, x int) float32 { return in[(c*224+y)*224+x] }
	if r, b := at(0, 0, 0), at(2, 0, 0); math.Abs(float64(r-(1-0.485)/0.229)) > 1e-5 || math.Abs(float64(b-(0-0.406)/0.225)) > 1e-5 {
		t.Errorf("a red pixel: R %v, B %v", r, b)
	}
	if _, err := modelInput([]byte("not a photo")); err == nil {
		t.Error("a file that is not a photo is the model's input")
	}
}

// The triage reads the model's JSON, and nothing else.
func TestTheTriageReadsTheModelsAnswer(t *testing.T) {
	v, err := parseVerdict(`{"transitDamage":false,"summary":"The customer changed their mind."}`)
	if err != nil || v.TransitDamage || v.Summary != "The customer changed their mind." {
		t.Errorf("verdict %+v, %v", v, err)
	}
	for _, answer := range []string{`true`, `"The parcel arrived wet."`, `{"transitDamage":true,"note":"x"}`} {
		if _, err := parseVerdict(answer); err == nil {
			t.Errorf("%s is read as a verdict", answer)
		}
	}
}

// A voice note's samples are its PCM's, from -1 to 1, its channels mixed;
// a recording of another rate or format is refused, named.
func TestAVoiceNotesSamplesAreRead(t *testing.T) {
	wav := func(rate, channels int, pcm ...int16) []byte {
		var data []byte
		for _, v := range pcm {
			data = binary.LittleEndian.AppendUint16(data, uint16(v))
		}
		b := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
		b = binary.LittleEndian.AppendUint32(b, 16)
		b = binary.LittleEndian.AppendUint16(b, 1)
		b = binary.LittleEndian.AppendUint16(b, uint16(channels))
		b = binary.LittleEndian.AppendUint32(b, uint32(rate))
		b = binary.LittleEndian.AppendUint32(b, uint32(rate*2*channels))
		b = binary.LittleEndian.AppendUint16(b, uint16(2*channels))
		b = binary.LittleEndian.AppendUint16(b, 16)
		b = append(b, "data"...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
		return append(b, data...)
	}
	got, err := voiceSamples(wav(16000, 2, 16384, -16384, 32767, 32767))
	if err != nil || len(got) != 2 || got[0] != 0 || got[1] != 32767.0/32768 {
		t.Errorf("%v, %v", got, err)
	}
	if _, err := voiceSamples(wav(8000, 1, 1, 2)); err == nil || !strings.Contains(err.Error(), "at 8000 Hz") {
		t.Errorf("a recording at 8 kHz: %v", err)
	}
	if _, err := voiceSamples([]byte("ID3")); err == nil {
		t.Error("an MP3 is read")
	}
	if !isVoiceNote("claims/CLM-4113/voice-note.WAV") || isVoiceNote("claims/CLM-4113/crushed-box.png") {
		t.Error("voice notes are not told from photos")
	}
}
