package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Couriers confirm deliveries over NATS, and the service tells the other
// services of every tracking update there:
//
//	deliveries.confirmed  a courier's handheld confirms a delivery:
//	                      {"reference", "location", "deliveredAt",
//	                      "signedBy"}, the courier in a header
//	tracking.<reference>  every scan the service records, from the depots'
//	                      systems (Kafka), their scanners (MQTT) or the
//	                      couriers: {"reference", "scanId", "status",
//	                      "location", "scannedAt"}, where it came from in a
//	                      source header (depot-system, scanner, courier);
//	                      the TRACKING stream keeps them
const (
	deliveriesSubject = "deliveries.confirmed"
	trackingStream    = "TRACKING"
)

type trackingUpdates struct {
	nc  *nats.Conn
	js  jetstream.JetStream
	log *slog.Logger
}

func openTrackingUpdates(ctx context.Context, url string, log *slog.Logger) (*trackingUpdates, error) {
	var nc *nats.Conn
	err := retry(ctx, log, "nats", func() error {
		c, err := nats.Connect(url, nats.Name("parcels"))
		if err == nil {
			nc = c
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, err
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: trackingStream, Subjects: []string{"tracking.>"}}); err != nil {
		nc.Close()
		return nil, fmt.Errorf("creating the %s stream: %w", trackingStream, err)
	}
	return &trackingUpdates{nc: nc, js: js, log: log}, nil
}

func (u *trackingUpdates) Close() { u.nc.Close() }

// publish tells the other services of a scan the service recorded.
func (u *trackingUpdates) publish(ctx context.Context, ref string, s scan, source string, headers map[string]string) error {
	body, err := json.Marshal(map[string]any{
		"reference": ref, "scanId": s.ScanID, "status": s.Status, "location": s.Location, "scannedAt": s.ScannedAt,
	})
	if err != nil {
		return err
	}
	msg := &nats.Msg{Subject: "tracking." + ref, Header: nats.Header{}, Data: body}
	msg.Header.Set("source", source)
	for k, v := range headers {
		msg.Header.Set(k, v)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = u.js.PublishMsg(ctx, msg)
	return err
}

// confirmDeliveries records the couriers' delivery confirmations as
// delivery scans, until ctx ends.
func (u *trackingUpdates) confirmDeliveries(ctx context.Context, rec *recorder) error {
	sub, err := u.nc.Subscribe(deliveriesSubject, func(m *nats.Msg) {
		var d struct {
			Reference   string    `json:"reference"`
			Location    string    `json:"location"`
			DeliveredAt time.Time `json:"deliveredAt"`
		}
		if err := json.Unmarshal(m.Data, &d); err != nil || d.Reference == "" {
			u.log.Warn("delivery confirmation skipped: not a confirmation", "err", err)
			return
		}
		if d.DeliveredAt.IsZero() {
			d.DeliveredAt = time.Now().UTC()
		}
		var headers map[string]string
		if courier := m.Header.Get("courier"); courier != "" {
			headers = map[string]string{"courier": courier}
		}
		s := scan{ScanID: "DLV-" + d.Reference, Status: "DELIVERED", Location: d.Location, ScannedAt: d.DeliveredAt.UTC()}
		if err := rec.record(ctx, d.Reference, s, "courier", headers); err != nil {
			u.log.Warn("delivery confirmation skipped", "reference", d.Reference, "err", err)
		}
	})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
	return u.nc.Flush()
}

// recorder stores the scans of parcels, wherever they come from, tells the
// shop of a delivery, and then the other services of each scan.
type recorder struct {
	tracking *trackingStore
	updates  *trackingUpdates
	store    *store
	shops    *shopWebhooks
	log      *slog.Logger
}

// tellShop tells the shop of a parcel's delivery; a parcel the service
// does not know has no shop to tell.
func (r *recorder) tellShop(ctx context.Context, ref string, s scan) {
	p, err := r.store.Get(ctx, ref)
	if err != nil {
		if !errors.Is(err, errNotFound) {
			r.log.Error("telling the shop of a delivery failed", "parcel", ref, "err", err)
		}
		return
	}
	if err := r.shops.delivered(ctx, p, s); err != nil {
		r.log.Error("telling the shop of a delivery failed", "parcel", ref, "shop", p.Sender, "err", err)
	}
}

func (r *recorder) record(ctx context.Context, ref string, s scan, source string, headers map[string]string) error {
	if err := r.tracking.AddScan(ctx, ref, s); err != nil {
		return err
	}
	r.log.Info("scan stored", "parcel", ref, "status", s.Status, "source", source)
	if s.Status == "DELIVERED" {
		r.tellShop(ctx, ref, s)
	}
	if err := r.updates.publish(ctx, ref, s, source, headers); err != nil {
		r.log.Error("publishing a tracking update failed", "parcel", ref, "err", err)
	}
	return nil
}
