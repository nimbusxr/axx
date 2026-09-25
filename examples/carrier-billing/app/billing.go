package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"path"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Line statuses.
const (
	Matched         = "MATCHED"
	Overcharged     = "OVERCHARGED"
	UnknownShipment = "UNKNOWN_SHIPMENT"
	NoRate          = "NO_RATE"
)

// Shipment is what the parcels platform knows about a parcel it handed to a
// carrier.
type Shipment struct {
	Parcel   string  `firestore:"parcel" json:"parcel"`
	Carrier  string  `firestore:"carrier" json:"carrier"`
	Service  string  `firestore:"service" json:"service"`
	WeightKg float64 `firestore:"weightKg" json:"weightKg"`
}

// Line is a reconciled invoice line, as stored in BigQuery.
type Line struct {
	Invoice      string    `bigquery:"invoice"`
	Carrier      string    `bigquery:"carrier"`
	Parcel       string    `bigquery:"parcel"`
	Service      string    `bigquery:"service"`
	Billed       float64   `bigquery:"billed"`
	Expected     float64   `bigquery:"expected"`
	Status       string    `bigquery:"status"`
	ReconciledAt time.Time `bigquery:"reconciled_at"`
}

type service struct {
	gcp   *clients
	names names
	log   *slog.Logger
	now   func() time.Time
}

// reconcile prices every line of an uploaded invoice: the carrier's rate
// for the service, times the weight the parcels platform measured. Lines
// billed above that, or for parcels the platform never shipped, are
// disputed.
func (s *service) reconcile(ctx context.Context, bucket, object string) error {
	carrier, invoice, ok := invoiceName(object)
	if !ok {
		s.log.Warn("upload ignored: not <carrier>/<invoice>.csv", "object", object)
		return nil
	}
	doc := s.gcp.fs.Collection(s.names.InvoiceDocs).Doc(invoice)
	if _, err := doc.Get(ctx); err == nil {
		s.log.Info("invoice already reconciled", "invoice", invoice)
		return nil
	} else if status.Code(err) != codes.NotFound {
		return err
	}
	rows, err := s.readInvoice(ctx, bucket, object)
	if err != nil {
		return err
	}
	rates, err := s.rates(ctx, carrier)
	if err != nil {
		return err
	}
	var lines []Line
	var disputed []Line
	var billed, expected, overcharge int64
	for _, r := range rows {
		l := Line{Invoice: invoice, Carrier: carrier, Parcel: r.parcel, Service: r.service, Billed: cents(r.billed), ReconciledAt: s.now().UTC()}
		sh, found, err := s.shipment(ctx, r.parcel)
		if err != nil {
			return err
		}
		rate, rated := rates[r.service]
		switch {
		case !found:
			l.Status = UnknownShipment
		case !rated:
			l.Status = NoRate
		default:
			exp := int64(math.Round(rate * sh.WeightKg * 100))
			l.Expected = cents(exp)
			l.Status = Matched
			if r.billed > exp {
				l.Status = Overcharged
				overcharge += r.billed - exp
			}
			expected += exp
		}
		billed += r.billed
		if l.Status != Matched {
			disputed = append(disputed, l)
			if l.Status != Overcharged {
				overcharge += r.billed
			}
		}
		lines = append(lines, l)
	}
	if err := s.gcp.bq.Dataset(s.names.Dataset).Table(s.names.Lines).Inserter().Put(ctx, lines); err != nil {
		return fmt.Errorf("store the lines of %s: %w", invoice, err)
	}
	state := "RECONCILED"
	if len(disputed) > 0 {
		state = "DISPUTED"
		if err := s.writeDisputes(ctx, invoice, carrier, disputed, overcharge); err != nil {
			return err
		}
	}
	summary := map[string]any{
		"carrier": carrier, "status": state, "lines": len(lines), "source": "gs://" + bucket + "/" + object,
		"totals": map[string]any{"billed": cents(billed), "expected": cents(expected), "disputed": cents(overcharge)},
	}
	if _, err := doc.Set(ctx, summary); err != nil {
		return err
	}
	eventType := "InvoiceReconciled"
	if state == "DISPUTED" {
		eventType = "InvoiceDisputed"
	}
	event, _ := json.Marshal(map[string]any{"invoice": invoice, "carrier": carrier, "status": state, "lines": len(lines), "disputed": cents(overcharge)})
	res := s.gcp.ps.Publisher(s.names.InvoiceEvents).Publish(ctx, &pubsub.Message{Data: event, Attributes: map[string]string{"eventType": eventType}})
	if _, err := res.Get(ctx); err != nil {
		return err
	}
	s.log.Info("invoice reconciled", "invoice", invoice, "status", state, "lines", len(lines), "disputed", cents(overcharge))
	return nil
}

// invoiceName reads carrier and invoice from "kestrel/INV-2026-09-KES.csv".
func invoiceName(object string) (carrier, invoice string, ok bool) {
	dir, file := path.Split(object)
	dir = strings.Trim(dir, "/")
	if dir == "" || strings.Contains(dir, "/") || path.Ext(file) != ".csv" {
		return "", "", false
	}
	return strings.ToUpper(dir), strings.TrimSuffix(file, ".csv"), true
}

type invoiceRow struct {
	parcel, service string
	billed          int64 // cents
}

// readInvoice reads the CSV lines: parcel,service,weight_kg,billed.
func (s *service) readInvoice(ctx context.Context, bucket, object string) ([]invoiceRow, error) {
	r, err := s.gcp.gcs.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", object, err)
	}
	defer r.Close()
	recs, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s is not CSV: %w", object, err)
	}
	var out []invoiceRow
	for i, rec := range recs {
		if i == 0 || len(rec) < 4 {
			continue // the header
		}
		amount, err := strconv.ParseFloat(strings.TrimSpace(rec[3]), 64)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: billed %q", object, i+1, rec[3])
		}
		out = append(out, invoiceRow{parcel: strings.TrimSpace(rec[0]), service: strings.TrimSpace(rec[1]), billed: int64(math.Round(amount * 100))})
	}
	return out, nil
}

func (s *service) shipment(ctx context.Context, parcel string) (Shipment, bool, error) {
	snap, err := s.gcp.fs.Collection(s.names.Shipments).Doc(parcel).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Shipment{}, false, nil
	}
	if err != nil {
		return Shipment{}, false, err
	}
	var sh Shipment
	return sh, true, snap.DataTo(&sh)
}

// rates are the carrier's prices per kg, by service.
func (s *service) rates(ctx context.Context, carrier string) (map[string]float64, error) {
	q := s.gcp.bq.Query(fmt.Sprintf("SELECT carrier, service, price_per_kg FROM `%s.%s.%s`", s.gcp.project, s.names.Dataset, s.names.Rates))
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the rates: %w", err)
	}
	out := map[string]float64{}
	for {
		var row struct {
			Carrier    string   `bigquery:"carrier"`
			Service    string   `bigquery:"service"`
			PricePerKg *big.Rat `bigquery:"price_per_kg"`
		}
		err := it.Next(&row)
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if row.Carrier == carrier && row.PricePerKg != nil {
			out[row.Service], _ = row.PricePerKg.Float64()
		}
	}
}

// writeDisputes writes the lines the carrier must answer for, as CSV for the
// carrier and as a JSON summary for the finance team.
func (s *service) writeDisputes(ctx context.Context, invoice, carrier string, lines []Line, total int64) error {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"parcel", "status", "billed", "expected"})
	for _, l := range lines {
		_ = w.Write([]string{l.Parcel, l.Status, money(l.Billed), money(l.Expected)})
	}
	w.Flush()
	if err := s.put(ctx, invoice+".csv", "text/csv", b.Bytes()); err != nil {
		return err
	}
	summary, _ := json.MarshalIndent(map[string]any{
		"invoice": invoice, "carrier": carrier, "disputedLines": len(lines), "disputedAmount": cents(total),
	}, "", "  ")
	return s.put(ctx, invoice+".json", "application/json", summary)
}

func (s *service) put(ctx context.Context, name, contentType string, body []byte) error {
	w := s.gcp.gcs.Bucket(s.names.Disputes).Object(name).NewWriter(ctx)
	w.ContentType = contentType
	if _, err := io.Copy(w, bytes.NewReader(body)); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func cents(c int64) float64 { return float64(c) / 100 }

func money(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// ---- subscribers ----

func (s *service) startSubscribers(ctx context.Context) {
	go s.receive(ctx, s.names.UploadsSub, s.invoiceUploaded)
	go s.receive(ctx, s.names.ShipmentsSub, s.shipmentEvent)
}

// receive handles a subscription's messages; a message whose handling
// fails is redelivered.
func (s *service) receive(ctx context.Context, sub string, handle func(context.Context, *pubsub.Message) error) {
	for ctx.Err() == nil {
		err := s.gcp.ps.Subscriber(sub).Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
			if err := handle(ctx, m); err != nil {
				s.log.Error("message failed; it will be redelivered", "subscription", sub, "error", err)
				m.Nack()
				return
			}
			m.Ack()
		})
		if err != nil && ctx.Err() == nil {
			s.log.Warn("subscription stream broke; reopening", "subscription", sub, "error", err)
			time.Sleep(time.Second)
		}
	}
}

// invoiceUploaded handles the carrier-invoices bucket's notifications.
func (s *service) invoiceUploaded(ctx context.Context, m *pubsub.Message) error {
	if m.Attributes["eventType"] != "OBJECT_FINALIZE" {
		return nil
	}
	return s.reconcile(ctx, m.Attributes["bucketId"], m.Attributes["objectId"])
}

// shipmentEvent records the weights the parcels platform measures.
func (s *service) shipmentEvent(ctx context.Context, m *pubsub.Message) error {
	var e Shipment
	_ = json.Unmarshal(m.Data, &e)
	eventType, ok := m.Attributes["eventType"]
	if !ok {
		s.log.Warn("shipment event ignored: no eventType", "parcel", e.Parcel)
		return nil
	}
	if eventType != "ShipmentWeighed" || e.Parcel == "" {
		return nil
	}
	if _, err := s.gcp.fs.Collection(s.names.Shipments).Doc(e.Parcel).Set(ctx, e); err != nil {
		return err
	}
	s.log.Info("shipment weighed", "parcel", e.Parcel, "weightKg", e.WeightKg)
	return nil
}
