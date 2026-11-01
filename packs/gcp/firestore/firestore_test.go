package gcpfirestore

import (
	"math"
	"strings"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/type/latlng"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/jsonassert"
)

// A seed's timestamps reach Firestore as time.Time values, which it stores
// as timestamps.
func TestStoredKeepsTimestamps(t *testing.T) {
	at := time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC)
	got := stored(map[string]any{"weighedAt": at, "scans": []any{map[string]any{"at": at}}}).(map[string]any)
	if v, ok := got["weighedAt"].(time.Time); !ok || !v.Equal(at) {
		t.Errorf("weighedAt = %T %v", got["weighedAt"], got["weighedAt"])
	}
	scan := got["scans"].([]any)[0].(map[string]any)
	if v, ok := scan["at"].(time.Time); !ok || !v.Equal(at) {
		t.Errorf("scans[0].at = %T %v", scan["at"], scan["at"])
	}
}

// A document's numbers keep their types (ADR 0014): a seed's `12` is an
// integer and its `12.0` a double, which reads back as a decimal.
func TestNumbersKeepTheirTypes(t *testing.T) {
	h := cloudtest.New(t)
	h.File("seeds/shipments.yaml", "shipments:\n  PX-5101: {pieces: 12, weightKg: 12.0, agreedPrice: 3.38, depot: {lat: 52, lng: 13.4}}\n")
	s, err := cloudstep.ReadSeedWith(h.SC, "seeds/shipments.yaml", seedOptions)
	if err != nil {
		t.Fatal(err)
	}
	fields := stored(s.Items["shipments"].(map[string]any)["PX-5101"]).(map[string]any)
	if _, ok := fields["pieces"].(int64); !ok {
		t.Errorf("pieces is a %T, want an integer", fields["pieces"])
	}
	if _, ok := fields["weightKg"].(float64); !ok {
		t.Errorf("weightKg is a %T, want a double", fields["weightKg"])
	}
	fields["at"] = &latlng.LatLng{Latitude: 52, Longitude: 13.4}
	fields["ratio"] = math.Inf(1)
	if got, want := docJSON(fields), `{"agreedPrice":3.38,"at":{"lat":52.0,"lng":13.4},"depot":{"lat":52,"lng":13.4},"pieces":12,"ratio":"Infinity","weightKg":12.0}`; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	err = jsonassert.Properties(docJSON(fields), &core.Table{Rows: [][]string{{"weightKg", "12"}}}, false)
	if !core.IsAssertion(err) || !strings.Contains(err.Error(), "weightKg is the decimal 12.0: write 12.0") {
		t.Errorf("a whole double should not match an integer: %v", err)
	}
}

// The checks read a seeded timestamp back in RFC 3339, in UTC: as the seed
// wrote it when it is in UTC.
func TestSeededTimestampsReadBack(t *testing.T) {
	h := cloudtest.New(t)
	h.File("seeds/shipments.yaml", `shipments:
  PX-5101:
    weighedAt: 2026-09-24T07:40:00Z
    carrierScan: "2026-09-24T07:40:00Z"
    pickedUpAt: 2026-09-24T09:40:00.250+02:00
    shipDate: 2026-09-24
`)
	s, err := cloudstep.ReadSeedWith(h.SC, "seeds/shipments.yaml", seedOptions)
	if err != nil {
		t.Fatal(err)
	}
	fields := stored(s.Items["shipments"].(map[string]any)["PX-5101"]).(map[string]any)
	if _, ok := fields["weighedAt"].(time.Time); !ok {
		t.Fatalf("weighedAt is a %T, want a timestamp", fields["weighedAt"])
	}
	err = jsonassert.Properties(docJSON(fields), &core.Table{Rows: [][]string{
		{"weighedAt", "2026-09-24T07:40:00Z"},
		{"carrierScan", "2026-09-24T07:40:00Z"},
		{"pickedUpAt", "2026-09-24T07:40:00.25Z"},
		{"shipDate", "2026-09-24T00:00:00Z"},
	}}, false)
	if err != nil {
		t.Error(err)
	}
}
