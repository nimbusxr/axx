//go:build integration

package gcpfirestore

import (
	"context"
	"strings"
	"testing"
	"time"

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
