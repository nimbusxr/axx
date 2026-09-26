//go:build integration

package gcpstorage

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

func TestObjects(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci-gcp:latest", "4588", "/health", nil)
	h := cloudtest.New(t, gcpcore.Pack(), Pack())
	h.OK("the billing gcp project with the following properties:", [][]string{
		{"project", "parcels-dev"}, {"endpoint", "http://" + addr},
	})
	st, err := store(h.SC)
	if err != nil {
		t.Fatal(err)
	}
	c := st.(bucketStore).c
	ctx := context.Background()
	if err := c.Bucket("carrier-invoices").Create(ctx, "parcels-dev", nil); err != nil {
		t.Fatal(err)
	}

	h.File("invoices/kestrel.json", `{"invoice":"INV-1","lines":[{"parcel":"PX-1","amount":12.5}]}`)
	h.OK("the invoices/kestrel.json file is uploaded to the carrier-invoices gcs bucket as incoming/kestrel.json")
	h.OK("the carrier-invoices gcs bucket has an object named incoming/kestrel.json")
	h.OK("the incoming/kestrel.json object in the carrier-invoices gcs bucket is identical to the invoices/kestrel.json file")
	h.OK("the incoming/kestrel.json object in the carrier-invoices gcs bucket has the following properties:", [][]string{
		{"invoice", "INV-1"}, {"lines[0].amount", "12.5"},
	})

	// The text of a PDF and the rows of a CSV file.
	pdf, err := os.ReadFile("../../../internal/filecontent/testdata/customs-invoice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Put(ctx, "carrier-invoices", "invoices/CI-2026-0925-001.pdf", pdf, "application/pdf"); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(ctx, "carrier-invoices", "disputes/INV-0.csv", []byte("parcel;billed;reason\nPX-1;12.50;weight differs\n"), "text/csv"); err != nil {
		t.Fatal(err)
	}
	h.OK(`the invoices/CI-2026-0925-001.pdf object in the carrier-invoices gcs bucket contains "Hiking boots 6403.91 1 129.00 EUR"`)
	h.OK("the disputes/INV-0.csv object in the carrier-invoices gcs bucket has a row where:", [][]string{{"parcel", "PX-1"}, {"reason", "weight differs"}})

	go func() {
		time.Sleep(time.Second)
		_ = st.Put(ctx, "carrier-invoices", "disputes/INV-1.csv", []byte("parcel,billed\n"), "text/csv")
	}()
	h.OK("within 5s the carrier-invoices gcs bucket has an object named disputes/INV-1.csv")
	err = h.Fails("within 1s the carrier-invoices gcs bucket has an object named disputes/INV-2.csv", "has no object named disputes/INV-2.csv")
	if !strings.Contains(err.Error(), "incoming/kestrel.json") {
		t.Errorf("the failure should list the bucket: %v", err)
	}
}
