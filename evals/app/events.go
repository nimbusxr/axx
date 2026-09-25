package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Every registered parcel (API or manifest) publishes a ParcelRegistered
// event to the events topic (default parcel-events), keyed by the parcel
// reference, Avro-encoded in the Confluent wire format (magic byte 0, the
// 4-byte schema id, then the Avro binary body). The schema is registered
// under the subject "<topic>-value".
const parcelRegisteredSchema = `{
  "type": "record",
  "name": "ParcelRegistered",
  "namespace": "evals.parcels",
  "fields": [
    {"name": "reference", "type": "string"},
    {"name": "sender", "type": "string"},
    {"name": "weightGrams", "type": "int"},
    {"name": "serviceLevel", "type": "string"},
    {"name": "zone", "type": "string"},
    {"name": "source", "type": "string"},
    {"name": "registeredAt", "type": {"type": "long", "logicalType": "timestamp-millis"}}
  ]
}`

type parcelRegistered struct {
	Reference    string
	Sender       string
	WeightGrams  int32
	ServiceLevel string
	Zone         string
	Source       string
	RegisteredAt int64
}

// avro encodes the event as an Avro binary record (field order of the schema).
func (e parcelRegistered) avro() []byte {
	var b []byte
	b = avroString(b, e.Reference)
	b = avroString(b, e.Sender)
	b = avroLong(b, int64(e.WeightGrams))
	b = avroString(b, e.ServiceLevel)
	b = avroString(b, e.Zone)
	b = avroString(b, e.Source)
	b = avroLong(b, e.RegisteredAt)
	return b
}

func avroLong(b []byte, v int64) []byte {
	u := uint64((v << 1) ^ (v >> 63)) // zig-zag
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func avroString(b []byte, s string) []byte {
	b = avroLong(b, int64(len(s)))
	return append(b, s...)
}

type eventPublisher struct {
	client   *kgo.Client
	registry string
	topic    string
	log      *slog.Logger

	mu       sync.Mutex
	schemaID int32
}

func newEventPublisher(ctx context.Context, brokers []string, registry, topic string, log *slog.Logger) (*eventPublisher, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
		kgo.ProducerLinger(0),
		kgo.RequiredAcks(kgo.AllISRAcks()),
	)
	if err != nil {
		return nil, err
	}
	if err := retry(ctx, log, "kafka", func() error { return client.Ping(ctx) }); err != nil {
		client.Close()
		return nil, err
	}
	p := &eventPublisher{client: client, registry: registry, topic: topic, log: log}
	if err := retry(ctx, log, "schema registry", func() error { _, err := p.id(ctx); return err }); err != nil {
		client.Close()
		return nil, err
	}
	return p, nil
}

func (p *eventPublisher) Close() { p.client.Close() }

// id registers the schema once and caches its id.
func (p *eventPublisher) id(ctx context.Context) (int32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.schemaID != 0 {
		return p.schemaID, nil
	}
	body, _ := json.Marshal(map[string]string{"schema": parcelRegisteredSchema})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.registry+"/subjects/"+p.topic+"-value/versions", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	rb, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("schema registry answered %d: %s", res.StatusCode, rb)
	}
	var out struct {
		ID int32 `json:"id"`
	}
	if err := json.Unmarshal(rb, &out); err != nil || out.ID == 0 {
		return 0, fmt.Errorf("schema registry: unexpected answer %s", rb)
	}
	p.schemaID = out.ID
	return out.ID, nil
}

func (p *eventPublisher) publish(ctx context.Context, e parcelRegistered) error {
	id, err := p.id(ctx)
	if err != nil {
		return err
	}
	value := make([]byte, 5, 64)
	binary.BigEndian.PutUint32(value[1:], uint32(id))
	value = append(value, e.avro()...)
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	rec := &kgo.Record{
		Topic: p.topic, Key: []byte(e.Reference), Value: value,
		Headers: []kgo.RecordHeader{{Key: "X-Event-Type", Value: []byte("ParcelRegistered")}},
	}
	return p.client.ProduceSync(pctx, rec).FirstErr()
}

// resetEvents deletes the events topic (it is recreated on the next publish)
// and waits until the deletion has completed.
func resetEvents(ctx context.Context, brokers []string, topic string) error {
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return err
	}
	defer client.Close()
	adm := kadm.NewClient(client)
	res, err := adm.DeleteTopics(ctx, topic)
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Err != nil && !errors.Is(r.Err, kerr.UnknownTopicOrPartition) {
			return r.Err
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		topics, err := adm.ListTopics(ctx)
		if err == nil && !topics.Has(topic) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("topic %s still exists 30s after its deletion", topic)
}
