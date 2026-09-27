//go:build integration

package gcpfirestore

import (
	"context"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

func TestDocuments(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci-gcp:latest", "4588", "/health", nil)
	h := cloudtest.New(t, gcpcore.Pack(), Pack())
	h.OK("the billing gcp project with the following properties:", [][]string{
		{"project", "parcels-dev"}, {"endpoint", "http://" + addr},
	})
	h.File("seeds/shipments.yaml", `
shipments:
  SHP-1001: {carrier: KESTREL, weightKg: 2.5, agreedPrice: 3.38, route: {from: Leeds, to: York}}
  SHP-1002: {carrier: KESTREL, weightKg: 12, agreedPrice: 16.2, signedBy: null}
shipments/SHP-1001/scans:
  scan-1: {depot: LDS, at: "2026-09-24T09:30:00Z"}
`)
	h.OK("a seeds/shipments.yaml firestore seed")
	h.OK("the shipments/SHP-1001 firestore document has the following properties:", [][]string{
		{"carrier", "KESTREL"}, {"weightKg", "2.5"}, {"route.to", "York"}, {"signedBy", "undefined"},
	})
	h.OK("the shipments/SHP-1002 firestore document has the following properties:", [][]string{{"weightKg", "12"}, {"signedBy", "null"}})
	h.OK("the shipments/SHP-1001/scans firestore collection has a document where:", [][]string{{"depot", "LDS"}})
	h.OK("the shipments firestore collection has a document where:", [][]string{{"agreedPrice", "16.2"}})

	// A document the service writes later is waited for.
	c, err := client(h.SC)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(time.Second)
		_, _ = c.Doc("invoices/INV-1").Set(context.Background(), map[string]any{
			"status": "RECONCILED", "totals": map[string]any{"billed": 19.58, "disputed": 0},
			"reconciledAt": time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC),
		})
	}()
	h.OK("within 5s the invoices/INV-1 firestore document has the following properties:", [][]string{
		{"status", "RECONCILED"}, {"totals.billed", "19.58"}, {"reconciledAt", "2026-09-24T09:30:00Z"},
	})
	h.Fails("within 1s the invoices/INV-2 firestore document has the following properties:", "There is no invoices/INV-2 firestore document", [][]string{{"status", "RECONCILED"}})
	h.Fails("within 1s the invoices/INV-1 firestore document has the following properties:", "status", [][]string{{"status", "DISPUTED"}})
	err = h.Fails("within 1s the shipments firestore collection has a document where:", "did not have a document", [][]string{{"carrier", "HERON"}})
	if !strings.Contains(err.Error(), `"carrier":"KESTREL"`) {
		t.Errorf("the failure should show the documents: %v", err)
	}
}

// A seed's unquoted YAML dates are Firestore timestamps and its quoted ones
// strings, as a service reading the documents with the Firestore SDK sees
// them.
func TestSeedTimestamps(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci-gcp:latest", "4588", "/health", nil)
	h := cloudtest.New(t, gcpcore.Pack(), Pack())
	h.OK("the billing gcp project with the following properties:", [][]string{
		{"project", "parcels-dev"}, {"endpoint", "http://" + addr},
	})
	h.File("seeds/shipments.yaml", `
shipments:
  PX-5101:
    weighedAt: 2026-09-24T07:40:00Z
    carrierScan: "2026-09-24T07:40:00Z"
    pickedUpAt: !!timestamp 2026-09-24T09:40:00.25+02:00
    shipDate: 2026-09-24
    scans:
      - {depot: LDS, at: 2026-09-24T09:30:00Z}
`)
	h.OK("a seeds/shipments.yaml firestore seed")

	// The service reads the document as it would against Firestore.
	t.Setenv("FIRESTORE_EMULATOR_HOST", addr)
	ctx := context.Background()
	c, err := firestore.NewClient(ctx, "parcels-dev")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	snap, err := c.Doc("shipments/PX-5101").Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var shipment struct {
		WeighedAt   time.Time `firestore:"weighedAt"`
		CarrierScan string    `firestore:"carrierScan"`
		PickedUpAt  time.Time `firestore:"pickedUpAt"`
		ShipDate    time.Time `firestore:"shipDate"`
		Scans       []struct {
			At time.Time `firestore:"at"`
		} `firestore:"scans"`
	}
	if err := snap.DataTo(&shipment); err != nil {
		t.Fatalf("the service cannot read the seeded shipment: %v", err)
	}
	data := snap.Data()
	if _, ok := data["weighedAt"].(time.Time); !ok {
		t.Errorf("weighedAt is a %T, want a timestamp", data["weighedAt"])
	}
	if _, ok := data["carrierScan"].(string); !ok {
		t.Errorf("carrierScan is a %T, want a string", data["carrierScan"])
	}
	for name, ts := range map[string]struct{ got, want time.Time }{
		"weighedAt":   {shipment.WeighedAt, time.Date(2026, 9, 24, 7, 40, 0, 0, time.UTC)},
		"pickedUpAt":  {shipment.PickedUpAt, time.Date(2026, 9, 24, 7, 40, 0, 250e6, time.UTC)},
		"shipDate":    {shipment.ShipDate, time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)},
		"scans[0].at": {shipment.Scans[0].At, time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC)},
	} {
		if !ts.got.Equal(ts.want) {
			t.Errorf("%s = %v, want %v", name, ts.got, ts.want)
		}
	}
	if shipment.CarrierScan != "2026-09-24T07:40:00Z" {
		t.Errorf("carrierScan = %q", shipment.CarrierScan)
	}

	// Checks read the seeded timestamps back in RFC 3339, in UTC.
	h.OK("the shipments/PX-5101 firestore document has the following properties:", [][]string{
		{"weighedAt", "2026-09-24T07:40:00Z"},
		{"carrierScan", "2026-09-24T07:40:00Z"},
		{"pickedUpAt", "2026-09-24T07:40:00.25Z"},
		{"shipDate", "2026-09-24T00:00:00Z"},
		{"scans[0].at", "2026-09-24T09:30:00Z"},
	})
}
