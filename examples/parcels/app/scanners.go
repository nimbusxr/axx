package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/eclipse/paho.golang/paho"
)

// The depots' handheld scanners publish each scan over MQTT, on
// depots/<depot>/scans: {"scanId", "parcelRef", "status", "location",
// "scannedAt"}, the scanner's name in a user property. A scan of a parcel
// the service does not know is answered on depots/<depot>/alerts, with the
// scanner's name, so the depot finds the parcel before it goes further.
type scanners struct {
	client *paho.Client
	store  *store
	rec    *recorder
	log    *slog.Logger
}

func openScanners(ctx context.Context, rawURL string, store *store, rec *recorder, log *slog.Logger) (*scanners, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("PARCELS_MQTT_URL: %w", err)
	}
	s := &scanners{store: store, rec: rec, log: log}
	var conn net.Conn
	err = retry(ctx, log, "mqtt", func() error {
		c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", u.Host)
		if err == nil {
			conn = c
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	s.client = paho.NewClient(paho.ClientConfig{
		ClientID: "parcels", Conn: conn,
		OnPublishReceived: []func(paho.PublishReceived) (bool, error){func(r paho.PublishReceived) (bool, error) {
			// Answering from the callback would wait on the client that calls it.
			go s.handle(ctx, r.Packet)
			return true, nil
		}},
	})
	if _, err := s.client.Connect(ctx, &paho.Connect{ClientID: "parcels", KeepAlive: 30, CleanStart: true}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("connecting to mqtt: %w", err)
	}
	if _, err := s.client.Subscribe(ctx, &paho.Subscribe{Subscriptions: []paho.SubscribeOptions{{Topic: "depots/+/scans", QoS: 1}}}); err != nil {
		_ = s.client.Disconnect(&paho.Disconnect{})
		return nil, fmt.Errorf("subscribing to the depots' scans: %w", err)
	}
	return s, nil
}

func (s *scanners) Close() { _ = s.client.Disconnect(&paho.Disconnect{}) }

func (s *scanners) handle(ctx context.Context, p *paho.Publish) {
	var in struct {
		ScanID    string    `json:"scanId"`
		ParcelRef string    `json:"parcelRef"`
		Status    string    `json:"status"`
		Location  string    `json:"location"`
		ScannedAt time.Time `json:"scannedAt"`
	}
	if err := json.Unmarshal(p.Payload, &in); err != nil || in.ParcelRef == "" {
		s.log.Warn("scanner scan skipped: not a scan", "topic", p.Topic, "err", err)
		return
	}
	depot := strings.Split(p.Topic, "/")[1]
	if _, err := s.store.Get(ctx, in.ParcelRef); errors.Is(err, errNotFound) {
		s.alert(ctx, depot, p, in.ParcelRef, in.ScanID)
		return
	} else if err != nil {
		s.log.Error("scanner scan skipped", "parcel", in.ParcelRef, "err", err)
		return
	}
	if in.ScannedAt.IsZero() {
		in.ScannedAt = time.Now()
	}
	sc := scan{ScanID: in.ScanID, Status: in.Status, Location: in.Location, ScannedAt: in.ScannedAt.UTC()}
	if err := s.rec.record(ctx, in.ParcelRef, sc, "scanner", nil); err != nil {
		s.log.Warn("scanner scan skipped", "parcel", in.ParcelRef, "err", err)
	}
}

// alert tells the depot's scanners of a scan of a parcel the service does
// not know.
func (s *scanners) alert(ctx context.Context, depot string, scanned *paho.Publish, ref, scanID string) {
	body, _ := json.Marshal(map[string]string{"parcelRef": ref, "scanId": scanID, "problem": "unknown parcel"})
	props := &paho.PublishProperties{ContentType: "application/json"}
	if scanned.Properties != nil {
		if scanner := scanned.Properties.User.Get("scanner"); scanner != "" {
			props.User = paho.UserProperties{{Key: "scanner", Value: scanner}}
		}
	}
	if _, err := s.client.Publish(ctx, &paho.Publish{Topic: "depots/" + depot + "/alerts", QoS: 1, Payload: body, Properties: props}); err != nil {
		s.log.Error("alerting the depot failed", "depot", depot, "parcel", ref, "err", err)
		return
	}
	s.log.Warn("scan of an unknown parcel", "depot", depot, "parcel", ref)
}
