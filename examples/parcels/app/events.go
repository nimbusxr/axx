package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/iskorotkov/avro/v2"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"
)

// Kafka, with Avro values in the Confluent wire format and schemas in the
// Schema Registry:
//
//   - every registered parcel (API or manifest) is announced on the events
//     topic (parcel-events) as a ParcelRegistered event keyed by its
//     reference, with the header X-Event-Type: ParcelRegistered;
//   - depot scanners publish DepotScan events on the scans topic
//     (depot-scans); the service stores them for the tracking read model.
const parcelRegisteredSchema = `{
  "type": "record",
  "name": "ParcelRegistered",
  "namespace": "example.parcels",
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
	Reference    string    `avro:"reference"`
	Sender       string    `avro:"sender"`
	WeightGrams  int       `avro:"weightGrams"`
	ServiceLevel string    `avro:"serviceLevel"`
	Zone         string    `avro:"zone"`
	Source       string    `avro:"source"`
	RegisteredAt time.Time `avro:"registeredAt"`
}

type events struct {
	producer   *kgo.Client
	brokers    []string
	registry   *sr.Client
	topic      string
	scansTopic string
	schema     avro.Schema
	schemaID   int
	log        *slog.Logger

	mu      sync.Mutex
	writers map[int]avro.Schema
}

func openEvents(ctx context.Context, cfg config, log *slog.Logger) (*events, error) {
	producer, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.KafkaBrokers...),
		kgo.AllowAutoTopicCreation(),
		kgo.ProducerLinger(0),
		kgo.RequiredAcks(kgo.AllISRAcks()),
	)
	if err != nil {
		return nil, err
	}
	e := &events{
		producer: producer, brokers: cfg.KafkaBrokers, topic: cfg.EventsTopic, scansTopic: cfg.ScansTopic,
		log: log, writers: map[int]avro.Schema{},
	}
	if err := retry(ctx, log, "kafka", func() error { return e.createTopics(ctx) }); err != nil {
		producer.Close()
		return nil, err
	}
	if e.schema, err = avro.Parse(parcelRegisteredSchema); err != nil {
		producer.Close()
		return nil, err
	}
	if e.registry, err = sr.NewClient(sr.URLs(cfg.RegistryURL)); err != nil {
		producer.Close()
		return nil, err
	}
	err = retry(ctx, log, "schema registry", func() error {
		s, err := e.registry.CreateSchema(ctx, e.topic+"-value", sr.Schema{Schema: parcelRegisteredSchema, Type: sr.TypeAvro})
		e.schemaID = s.ID
		return err
	})
	if err != nil {
		producer.Close()
		return nil, err
	}
	return e, nil
}

func (e *events) Close() { e.producer.Close() }

// createTopics makes sure both topics exist, so the scan consumer does not
// wait for a metadata refresh when the first scan arrives.
func (e *events) createTopics(ctx context.Context) error {
	res, err := kadm.NewClient(e.producer).CreateTopics(ctx, 1, 1, nil, e.topic, e.scansTopic)
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.Err != nil && !errors.Is(r.Err, kerr.TopicAlreadyExists) {
			return r.Err
		}
	}
	return nil
}

func (e *events) publishRegistered(ctx context.Context, ev parcelRegistered) error {
	body, err := avro.Marshal(e.schema, ev)
	if err != nil {
		return err
	}
	var h sr.ConfluentHeader
	value, err := h.AppendEncode(nil, e.schemaID, nil)
	if err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	rec := &kgo.Record{
		Topic: e.topic, Key: []byte(ev.Reference), Value: append(value, body...),
		Headers: []kgo.RecordHeader{{Key: "X-Event-Type", Value: []byte("ParcelRegistered")}},
	}
	return e.producer.ProduceSync(pctx, rec).FirstErr()
}

// consumeScans stores every DepotScan event from the scans topic until ctx
// ends. Records that cannot be read are logged and skipped.
func (e *events) consumeScans(ctx context.Context, tracking *trackingStore) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(e.brokers...),
		kgo.ConsumerGroup("parcels-tracking"),
		kgo.ConsumeTopics(e.scansTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		e.log.Error("scan consumer", "err", err)
		return
	}
	defer cl.Close()
	for {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		fetches.EachError(func(topic string, _ int32, err error) {
			e.log.Warn("reading scans failed", "topic", topic, "err", err)
		})
		fetches.EachRecord(func(r *kgo.Record) {
			ref, s, err := e.decodeScan(ctx, r.Value)
			if err == nil {
				err = tracking.AddScan(ctx, ref, s)
			}
			if err != nil {
				e.log.Warn("depot scan skipped", "offset", r.Offset, "err", err)
				return
			}
			e.log.Info("depot scan stored", "parcel", ref, "status", s.Status)
		})
	}
}

func (e *events) decodeScan(ctx context.Context, value []byte) (string, scan, error) {
	var h sr.ConfluentHeader
	id, body, err := h.DecodeID(value)
	if err != nil {
		return "", scan{}, err
	}
	schema, err := e.writerSchema(ctx, id)
	if err != nil {
		return "", scan{}, err
	}
	var m map[string]any
	if err := avro.Unmarshal(schema, body, &m); err != nil {
		return "", scan{}, err
	}
	text := func(k string) string { s, _ := m[k].(string); return s }
	at, err := time.Parse(time.RFC3339Nano, text("scannedAt"))
	if err != nil {
		return "", scan{}, fmt.Errorf("scannedAt: %w", err)
	}
	return text("parcelRef"), scan{ScanID: text("scanId"), Status: text("status"), Location: text("location"), ScannedAt: at.UTC()}, nil
}

// writerSchema fetches (once) the schema a record was written with.
func (e *events) writerSchema(ctx context.Context, id int) (avro.Schema, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s, ok := e.writers[id]; ok {
		return s, nil
	}
	got, err := e.registry.SchemaByID(ctx, id)
	if err != nil {
		return nil, err
	}
	s, err := avro.Parse(got.Schema)
	if err != nil {
		return nil, err
	}
	e.writers[id] = s
	return s, nil
}
