package cloudstep_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// readSeed reads a seed file with the given content.
func readSeed(t *testing.T, name, content string, o cloudstep.SeedOptions) (*cloudstep.Seed, error) {
	t.Helper()
	h := cloudtest.New(t)
	h.File(name, content)
	return cloudstep.ReadSeedWith(h.SC, name, o)
}

// shipment reads a seed with timestamps and returns the fields of its
// shipments/PX-5101 document.
func shipment(t *testing.T, content string) map[string]any {
	t.Helper()
	s, err := readSeed(t, "seeds/shipments.yaml", content, cloudstep.SeedOptions{Timestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	docs, _ := s.Items["shipments"].(map[string]any)
	fields, ok := docs["PX-5101"].(map[string]any)
	if !ok {
		t.Fatalf("no PX-5101 shipment in %v", s.Items)
	}
	return fields
}

func wantTime(t *testing.T, name string, got any, want time.Time) {
	t.Helper()
	at, ok := got.(time.Time)
	if !ok {
		t.Errorf("%s is %T %v, want the time %v", name, got, got, want)
		return
	}
	if !at.Equal(want) {
		t.Errorf("%s = %v, want %v", name, at, want)
	}
}

func wantString(t *testing.T, name string, got any, want string) {
	t.Helper()
	if s, ok := got.(string); !ok || s != want {
		t.Errorf("%s is %T %v, want the string %q", name, got, got, want)
	}
}

func TestReadSeedKeepsDatesAsText(t *testing.T) {
	s, err := readSeed(t, "seeds/claims.yaml", "claims:\n  - {id: CLM-4101, filedAt: 2026-09-01T10:00:00Z}\n", cloudstep.SeedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	claim := s.Items["claims"].([]any)[0].(map[string]any)
	wantString(t, "filedAt", claim["filedAt"], "2026-09-01T10:00:00Z")
}

func TestSeedUnquotedTimestamp(t *testing.T) {
	f := shipment(t, "shipments:\n  PX-5101:\n    weighedAt: 2026-09-24T07:40:00Z\n")
	wantTime(t, "weighedAt", f["weighedAt"], time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC))
}

func TestSeedUnquotedDateIsMidnightUTC(t *testing.T) {
	f := shipment(t, "shipments:\n  PX-5101:\n    shipDate: 2026-09-24\n")
	wantTime(t, "shipDate", f["shipDate"], time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC))
}

func TestSeedTimestampTag(t *testing.T) {
	f := shipment(t, `shipments:
  PX-5101:
    weighedAt: !!timestamp 2026-09-24T07:40:00Z
    shipDate: !!timestamp "2026-09-24"
    pickedUpAt: !<tag:yaml.org,2002:timestamp> 2026-09-24T09:15:00Z
`)
	wantTime(t, "weighedAt", f["weighedAt"], time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC))
	wantTime(t, "shipDate", f["shipDate"], time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC))
	wantTime(t, "pickedUpAt", f["pickedUpAt"], time.Date(2026, 9, 24, 9, 15, 0, 0, time.UTC))
}

func TestSeedQuotedDatesAreStrings(t *testing.T) {
	f := shipment(t, `shipments:
  PX-5101:
    carrierScan: "2026-09-24T07:40:00Z"
    manifest: '2026-09-24'
    tagged: !!str 2026-09-24
    note: |
      2026-09-24
`)
	wantString(t, "carrierScan", f["carrierScan"], "2026-09-24T07:40:00Z")
	wantString(t, "manifest", f["manifest"], "2026-09-24")
	wantString(t, "tagged", f["tagged"], "2026-09-24")
	wantString(t, "note", f["note"], "2026-09-24\n")
}

func TestSeedTimestampsInMapsAndLists(t *testing.T) {
	f := shipment(t, `shipments:
  PX-5101:
    route: {from: Leeds, departedAt: 2026-09-24T08:00:00Z, eta: {at: 2026-09-24T12:00:00Z, window: "2026-09-24T12:00:00Z"}}
    scans:
      - depot: LDS
        at: 2026-09-24T09:30:00Z
      - {depot: YRK, at: 2026-09-24T11:05:00Z}
    deliveryDays: [2026-09-25, "2026-09-26"]
`)
	route := f["route"].(map[string]any)
	wantTime(t, "route.departedAt", route["departedAt"], time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC))
	eta := route["eta"].(map[string]any)
	wantTime(t, "route.eta.at", eta["at"], time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	wantString(t, "route.eta.window", eta["window"], "2026-09-24T12:00:00Z")
	scans := f["scans"].([]any)
	wantTime(t, "scans[0].at", scans[0].(map[string]any)["at"], time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC))
	wantTime(t, "scans[1].at", scans[1].(map[string]any)["at"], time.Date(2026, 9, 24, 11, 5, 0, 0, time.UTC))
	days := f["deliveryDays"].([]any)
	wantTime(t, "deliveryDays[0]", days[0], time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))
	wantString(t, "deliveryDays[1]", days[1], "2026-09-26")
}

func TestSeedTimestampsThroughAliases(t *testing.T) {
	f := shipment(t, `shipments:
  PX-5101:
    weighedAt: &weighed 2026-09-24T07:40:00Z
    labelledAt: *weighed
    scan: &scan {depot: LDS, at: 2026-09-24T09:30:00Z}
    firstScan: *scan
`)
	want := time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC)
	wantTime(t, "weighedAt", f["weighedAt"], want)
	wantTime(t, "labelledAt", f["labelledAt"], want)
	wantTime(t, "firstScan.at", f["firstScan"].(map[string]any)["at"], time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC))
}

func TestSeedRepeatedFieldKeepsItsLastValue(t *testing.T) {
	f := shipment(t, `shipments:
  PX-5101:
    weighedAt: 2026-09-24T07:40:00Z
    weighedAt: "2026-09-24T07:40:00Z"
    pickedUpAt: "2026-09-24T09:15:00Z"
    pickedUpAt: 2026-09-24T09:15:00Z
`)
	wantString(t, "weighedAt", f["weighedAt"], "2026-09-24T07:40:00Z")
	wantTime(t, "pickedUpAt", f["pickedUpAt"], time.Date(2026, 9, 24, 9, 15, 0, 0, time.UTC))
}

func TestSeedDateKeysAreStrings(t *testing.T) {
	s, err := readSeed(t, "seeds/manifests.yaml", "manifests:\n  2026-09-24: {closedAt: 2026-09-24T18:00:00Z}\n", cloudstep.SeedOptions{Timestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	day, ok := s.Items["manifests"].(map[string]any)["2026-09-24"].(map[string]any)
	if !ok {
		t.Fatalf("no 2026-09-24 manifest in %v", s.Items)
	}
	wantTime(t, "closedAt", day["closedAt"], time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC))
}

func TestSeedTimestampZones(t *testing.T) {
	for text, want := range map[string]time.Time{
		"2026-09-24T07:40:00Z":       time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24T09:40:00+02:00":  time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24T02:40:00-05:00":  time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24T13:10:00+05:30":  time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24T02:40:00-5":      time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24 02:40:00 -05:00": time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24t07:40:00Z":       time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24T07:40:00":        time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24 07:40:00":        time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-9-4T7:40:00Z":          time.Date(2026, 9, 4, 7, 40, 0, 0, time.UTC),
	} {
		f := shipment(t, "shipments:\n  PX-5101:\n    weighedAt: "+text+"\n")
		wantTime(t, text, f["weighedAt"], want)
	}
}

func TestSeedTimestampFractions(t *testing.T) {
	for text, want := range map[string]time.Time{
		"2026-09-24T07:40:00.5Z":           time.Date(2026, 9, 24, 7, 40, 0, 500_000_000, time.UTC),
		"2026-09-24T07:40:00.250Z":         time.Date(2026, 9, 24, 7, 40, 0, 250_000_000, time.UTC),
		"2026-09-24T07:40:00.123456Z":      time.Date(2026, 9, 24, 7, 40, 0, 123_456_000, time.UTC),
		"2026-09-24T07:40:00.123456789Z":   time.Date(2026, 9, 24, 7, 40, 0, 123_456_789, time.UTC),
		"2026-09-24T07:40:00.1234567891Z":  time.Date(2026, 9, 24, 7, 40, 0, 123_456_789, time.UTC),
		"2026-09-24T09:40:00.75+02:00":     time.Date(2026, 9, 24, 7, 40, 0, 750_000_000, time.UTC),
		"2026-09-24T07:40:00.Z":            time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC),
		"2026-09-24 07:40:00.10 Z":         time.Date(2026, 9, 24, 7, 40, 0, 100_000_000, time.UTC),
		"2026-09-24T07:40:00.000000001Z":   time.Date(2026, 9, 24, 7, 40, 0, 1, time.UTC),
		"2026-09-24T07:40:00.999999999-01": time.Date(2026, 9, 24, 8, 40, 0, 999_999_999, time.UTC),
	} {
		f := shipment(t, "shipments:\n  PX-5101:\n    weighedAt: "+text+"\n")
		wantTime(t, text, f["weighedAt"], want)
	}
}

// Text YAML does not take for a timestamp stays text, even unquoted.
func TestSeedDateLikeTextIsString(t *testing.T) {
	for _, text := range []string{
		"2026-9-24",            // a date alone has two-digit months and days
		"2026-09-24T07:40Z",    // a time has seconds
		"2026-09-24T07:40:00z", // the zone is Z
		"2026-09",
		"24/09/2026",
		"2026-09-24T07:40:00Z and later",
	} {
		f := shipment(t, "shipments:\n  PX-5101:\n    weighedAt: "+text+"\n")
		wantString(t, text, f["weighedAt"], text)
	}
}

func TestSeedInvalidDateFails(t *testing.T) {
	_, err := readSeed(t, "seeds/shipments.yaml", "shipments:\n  PX-5101:\n    shipDate: 2026-02-30\n", cloudstep.SeedOptions{Timestamps: true})
	if err == nil || !strings.Contains(err.Error(), "seeds/shipments.yaml: line 3, column 15: 2026-02-30 is not a valid date (quote it to keep it a string)") {
		t.Fatalf("got %v", err)
	}
}

func TestSeedInvalidTimestampTagFails(t *testing.T) {
	_, err := readSeed(t, "seeds/shipments.yaml", "shipments:\n  PX-5101:\n    shipDate: !!timestamp tomorrow\n", cloudstep.SeedOptions{Timestamps: true})
	if err == nil || !strings.Contains(err.Error(), "seeds/shipments.yaml: line 3, column 15: !!timestamp tomorrow is not a YAML timestamp") {
		t.Fatalf("got %v", err)
	}
}

// JSON has no date type: a JSON seed's dates are strings.
func TestJSONSeedDatesAreStrings(t *testing.T) {
	s, err := readSeed(t, "seeds/shipments.json", `{"shipments": {"PX-5101": {"weighedAt": "2026-09-24T07:40:00Z", "scans": [{"at": "2026-09-24T09:30:00Z"}]}}}`, cloudstep.SeedOptions{Timestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	f := s.Items["shipments"].(map[string]any)["PX-5101"].(map[string]any)
	wantString(t, "weighedAt", f["weighedAt"], "2026-09-24T07:40:00Z")
	wantString(t, "scans[0].at", f["scans"].([]any)[0].(map[string]any)["at"], "2026-09-24T09:30:00Z")
}
