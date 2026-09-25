//go:build integration

package gcpbigquery

import (
	"context"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

func TestRows(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci-gcp:latest", "4588", "/health", nil)
	h := cloudtest.New(t, gcpcore.Pack(), Pack())
	h.OK("the billing gcp project with the following properties:", [][]string{
		{"project", "parcels-dev"}, {"endpoint", "http://" + addr},
	})
	c, p, err := client(h.SC)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ds := c.DatasetInProject(p.ID, "billing")
	if err := ds.Create(ctx, nil); err != nil {
		t.Fatal(err)
	}
	rates := bigquery.Schema{
		{Name: "carrier", Type: bigquery.StringFieldType},
		{Name: "service", Type: bigquery.StringFieldType},
		{Name: "price_per_kg", Type: bigquery.NumericFieldType},
		{Name: "since", Type: bigquery.DateFieldType},
	}
	lines := bigquery.Schema{
		{Name: "invoice", Type: bigquery.StringFieldType},
		{Name: "parcel", Type: bigquery.StringFieldType},
		{Name: "billed", Type: bigquery.FloatFieldType},
		{Name: "status", Type: bigquery.StringFieldType},
		{Name: "reconciled_at", Type: bigquery.TimestampFieldType},
		{Name: "route", Type: bigquery.RecordFieldType, Schema: bigquery.Schema{
			{Name: "from", Type: bigquery.StringFieldType}, {Name: "to", Type: bigquery.StringFieldType},
		}},
	}
	for name, schema := range map[string]bigquery.Schema{"carrier_rates": rates, "invoice_lines": lines} {
		if err := ds.Table(name).Create(ctx, &bigquery.TableMetadata{Schema: schema}); err != nil {
			t.Fatal(err)
		}
	}

	h.File("seeds/rates.yaml", `
billing.carrier_rates:
  - {carrier: KESTREL, service: express, price_per_kg: 1.35, since: "2026-01-01"}
  - {carrier: KESTREL, service: economy, price_per_kg: 0.80, since: "2026-01-01"}
  - {carrier: HERON, service: express, price_per_kg: 1.10, since: "2026-03-01"}
`)
	h.OK("a seeds/rates.yaml bigquery seed")
	h.OK("the billing.carrier_rates bigquery table has a row where:", [][]string{
		{"carrier", "KESTREL"}, {"service", "express"}, {"price_per_kg", "1.35"}, {"since", "2026-01-01"},
	})
	h.OK("the billing.carrier_rates bigquery table has 2 rows where:", [][]string{{"carrier", "KESTREL"}})
	h.OK("the parcels-dev.billing.carrier_rates bigquery table has 1 row where:", [][]string{{"carrier", "HERON"}, {"price_per_kg", "1.1"}})

	// Rows the service writes later are waited for.
	go func() {
		time.Sleep(time.Second)
		_ = ds.Table("invoice_lines").Inserter().Put(ctx, []bigquery.ValueSaver{row{
			"invoice": "INV-1", "parcel": "PX-1", "billed": 12.5, "status": "OVERCHARGED",
			"reconciled_at": time.Date(2026, 9, 24, 9, 30, 0, 0, time.UTC),
			"route":         map[string]bigquery.Value{"from": "Leeds", "to": "York"},
		}})
	}()
	h.OK("within 10s the billing.invoice_lines bigquery table has a row where:", [][]string{
		{"invoice", "INV-1"},
		{"billed", "12.5"},
		{"status", "OVERCHARGED"},
		{"route.to", "York"},
	})
	err = h.Fails("within 1s the billing.invoice_lines bigquery table has a row where:", "did not have a row", [][]string{{"status", "MATCHED"}})
	if !strings.Contains(err.Error(), `It has 1 row:
  {"status":"OVERCHARGED"}`) {
		t.Errorf("the failure should show the rows: %v", err)
	}
	h.Fails("the invoice_lines bigquery table has a row where:", "dataset.table", [][]string{{"status", "MATCHED"}})
}
