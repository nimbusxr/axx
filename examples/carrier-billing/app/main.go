// The billing service: it reconciles the invoices carriers upload against
// the rates the company agreed with them. It runs on Google Cloud: invoices
// and disputes in Cloud Storage, shipments and invoice summaries in
// Firestore, rates and reconciled lines in BigQuery, and events over
// Pub/Sub.
//
//	billing             serve and process the subscriptions
//	billing provision   create the Google Cloud resources it needs (what Terraform does in a real project)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/pubsub/v2"
	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// names are the Google Cloud resources the service uses.
type names struct {
	Invoices, Disputes            string // Cloud Storage buckets
	InvoiceUploads, InvoiceEvents string // Pub/Sub topics
	ShipmentEvents                string
	UploadsSub, ShipmentsSub      string // the service's subscriptions
	Dataset, Rates, Lines         string // BigQuery
	Shipments, InvoiceDocs        string // Firestore collections
}

var defaults = names{
	Invoices: "carrier-invoices", Disputes: "billing-disputes",
	InvoiceUploads: "invoice-uploads", InvoiceEvents: "invoice-events", ShipmentEvents: "shipment-events",
	UploadsSub: "billing-invoice-uploads", ShipmentsSub: "billing-shipment-events",
	Dataset: "billing", Rates: "carrier_rates", Lines: "invoice_lines",
	Shipments: "shipments", InvoiceDocs: "invoices",
}

// clients are the Google Cloud clients of the service.
type clients struct {
	project string
	gcs     *storage.Client
	ps      *pubsub.Client
	bq      *bigquery.Client
	fs      *firestore.Client
}

// newClients configures the clients as for Google Cloud: Application
// Default Credentials, and the usual emulator variables (STORAGE_,
// PUBSUB_ and FIRESTORE_EMULATOR_HOST) to use an emulator instead.
// BigQuery has no such variable; BIGQUERY_ENDPOINT stands in for it.
func newClients(ctx context.Context) (*clients, error) {
	project := os.Getenv("GOOGLE_CLOUD_PROJECT")
	if project == "" {
		return nil, errors.New("set GOOGLE_CLOUD_PROJECT")
	}
	c := &clients{project: project}
	var err error
	if c.gcs, err = storage.NewClient(ctx); err != nil {
		return nil, err
	}
	if c.ps, err = pubsub.NewClient(ctx, project); err != nil {
		return nil, err
	}
	var bqOpts []option.ClientOption
	if ep := os.Getenv("BIGQUERY_ENDPOINT"); ep != "" {
		bqOpts = append(bqOpts, option.WithEndpoint(strings.TrimRight(ep, "/")+"/bigquery/v2/"), option.WithoutAuthentication())
	}
	if c.bq, err = bigquery.NewClient(ctx, project, bqOpts...); err != nil {
		return nil, err
	}
	if c.fs, err = firestore.NewClient(ctx, project); err != nil {
		return nil, err
	}
	return c, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("billing failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := newClients(ctx)
	if err != nil {
		return fmt.Errorf("configure Google Cloud: %w", err)
	}
	if len(os.Args) > 1 && os.Args[1] == "provision" {
		return provision(ctx, c, defaults, log)
	}

	svc := &service{gcp: c, names: defaults, log: log, now: time.Now}
	svc.startSubscribers(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Addr: env("BILLING_ADDR", ":8600"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("billing service listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
