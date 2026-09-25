package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// provision creates the Google Cloud resources of the service, as the
// infrastructure code of a real project does. It can run again: what
// exists is kept.
func provision(ctx context.Context, c *clients, n names, log *slog.Logger) error {
	if err := waitForGCP(ctx, c); err != nil {
		return err
	}
	for _, b := range []string{n.Invoices, n.Disputes} {
		if err := c.gcs.Bucket(b).Create(ctx, c.project, nil); err != nil && !conflict(err) {
			return fmt.Errorf("bucket %s: %w", b, err)
		}
	}
	topic := func(t string) string { return "projects/" + c.project + "/topics/" + t }
	for _, t := range []string{n.InvoiceUploads, n.InvoiceEvents, n.ShipmentEvents} {
		if _, err := c.ps.TopicAdminClient.CreateTopic(ctx, &pubsubpb.Topic{Name: topic(t)}); err != nil && status.Code(err) != codes.AlreadyExists {
			return fmt.Errorf("topic %s: %w", t, err)
		}
	}
	for sub, t := range map[string]string{n.UploadsSub: n.InvoiceUploads, n.ShipmentsSub: n.ShipmentEvents} {
		_, err := c.ps.SubscriptionAdminClient.CreateSubscription(ctx, &pubsubpb.Subscription{
			Name: "projects/" + c.project + "/subscriptions/" + sub, Topic: topic(t), AckDeadlineSeconds: 60,
		})
		if err != nil && status.Code(err) != codes.AlreadyExists {
			return fmt.Errorf("subscription %s: %w", sub, err)
		}
	}
	// Every invoice a carrier uploads is announced on invoice-uploads.
	notes, err := c.gcs.Bucket(n.Invoices).Notifications(ctx)
	if err != nil {
		return fmt.Errorf("notifications of %s: %w", n.Invoices, err)
	}
	if len(notes) == 0 {
		if err := notify(ctx, c, n.Invoices, n.InvoiceUploads); err != nil {
			return fmt.Errorf("notification of %s: %w", n.Invoices, err)
		}
	}
	ds := c.bq.Dataset(n.Dataset)
	if err := ds.Create(ctx, nil); err != nil && !conflict(err) {
		return fmt.Errorf("dataset %s: %w", n.Dataset, err)
	}
	tables := map[string]bigquery.Schema{
		n.Rates: {
			{Name: "carrier", Type: bigquery.StringFieldType, Required: true},
			{Name: "service", Type: bigquery.StringFieldType, Required: true},
			{Name: "price_per_kg", Type: bigquery.NumericFieldType, Required: true},
		},
		n.Lines: {
			{Name: "invoice", Type: bigquery.StringFieldType},
			{Name: "carrier", Type: bigquery.StringFieldType},
			{Name: "parcel", Type: bigquery.StringFieldType},
			{Name: "service", Type: bigquery.StringFieldType},
			{Name: "billed", Type: bigquery.FloatFieldType},
			{Name: "expected", Type: bigquery.FloatFieldType},
			{Name: "status", Type: bigquery.StringFieldType},
			{Name: "reconciled_at", Type: bigquery.TimestampFieldType},
		},
	}
	for name, schema := range tables {
		if err := ds.Table(name).Create(ctx, &bigquery.TableMetadata{Schema: schema}); err != nil && !conflict(err) {
			return fmt.Errorf("table %s: %w", name, err)
		}
	}
	log.Info("provisioned", "buckets", 2, "topics", 3, "subscriptions", 2, "tables", len(tables))
	return nil
}

// conflict reports a resource that exists already.
func conflict(err error) bool {
	var ge *googleapi.Error
	return errors.As(err, &ge) && ge.Code == http.StatusConflict
}

// waitForGCP waits until the Google Cloud endpoints answer (an emulator
// starting next to the service).
func waitForGCP(ctx context.Context, c *clients) error {
	deadline := time.Now().Add(time.Minute)
	for {
		_, err := c.ps.TopicAdminClient.GetTopic(ctx, &pubsubpb.GetTopicRequest{Topic: "projects/" + c.project + "/topics/probe"})
		if err == nil || status.Code(err) == codes.NotFound {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// notify announces a bucket's new objects on a topic. The client registers
// the topic by its full resource name (//pubsub.googleapis.com/projects/..),
// as Cloud Storage documents it. The floci emulator stores that name but then
// cannot find the topic when it publishes, so against an emulator the topic
// is registered by its short name (projects/../topics/..), which floci finds.
func notify(ctx context.Context, c *clients, bucket, topic string) error {
	emulator := os.Getenv("STORAGE_EMULATOR_HOST")
	if emulator == "" {
		_, err := c.gcs.Bucket(bucket).AddNotification(ctx, &storage.Notification{
			TopicProjectID: c.project, TopicID: topic, PayloadFormat: storage.JSONPayload,
			EventTypes: []string{storage.ObjectFinalizeEvent},
		})
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"topic": "projects/" + c.project + "/topics/" + topic, "payload_format": storage.JSONPayload,
		"event_types": []string{storage.ObjectFinalizeEvent},
	})
	url := strings.TrimRight(emulator, "/") + "/storage/v1/b/" + bucket + "/notificationConfigs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return nil
}
