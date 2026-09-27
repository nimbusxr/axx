package gcpfirestore

import (
	"testing"
	"time"

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
