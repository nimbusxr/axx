package kafka

import (
	"context"
	"fmt"
	"sync"

	"github.com/iskorotkov/avro/v2"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nimbusxr/axx/core"
)

// What other packs reuse to produce a topic's records as the publish steps
// do, at rates of their own: the producer of a service's topic, its
// options, and its serialization.

// producerSpec is the producer configuration of topic on the service: its
// topic client's, or that of a topic client with no properties.
func (s *Service) producerSpec(topic string) (*clientSpec, *registry, error) {
	if tc, ok := s.client(topic); ok {
		return tc.producer, tc.preg, nil
	}
	spec, _, err := translate(roleProducer, s.Brokers, nil)
	if err != nil {
		return nil, nil, err
	}
	return spec, nil, nil
}

// Producer returns the client the publish steps produce topic's records
// with on the service: that of the topic client the scenario created for
// topic or, without one, that of a topic client with no properties. Every
// topic client of the run with the same producer configuration shares it,
// and the run closes it when it ends: do not close it.
func (s *Service) Producer(sc *core.Scenario, topic string) (*kgo.Client, error) {
	if tc, ok := s.client(topic); ok {
		return tc.prod, nil
	}
	spec, _, err := s.producerSpec(topic)
	if err != nil {
		return nil, err
	}
	return producerFor(sc.Suite(), spec)
}

// ProducerOpts returns the options Producer's client is made with: its
// connection's (brokers, client.id, security.protocol, SASL, TLS and their
// timeouts) and its producing's (acks, idempotence, compression, linger,
// batch and buffer sizes, retries, partitioner), for a pack that makes a
// client of its own (and closes it), to produce at a rate the shared one is
// not tuned for, say. Options appended after them take precedence.
func (s *Service) ProducerOpts(sc *core.Scenario, topic string) ([]kgo.Opt, error) {
	spec, _, err := s.producerSpec(topic)
	if err != nil {
		return nil, err
	}
	return producerOpts(spec, sc.Suite().ResolvePath)
}

// Serializer returns the serialization of topic's producer on the service:
// that of the topic client the scenario created for topic or, without one,
// that of a topic client with no properties, which writes keys and values
// as text.
func (s *Service) Serializer(sc *core.Scenario, topic string) (*Serializer, error) {
	spec, reg, err := s.producerSpec(topic)
	if err != nil {
		return nil, err
	}
	if reg == nil && (spec.Key == serdeAvro || spec.Value == serdeAvro) {
		if reg, err = registryFor(sc.Suite(), spec); err != nil {
			return nil, err
		}
	}
	cfg, err := configOf(sc.Suite())
	if err != nil {
		return nil, err
	}
	return &Serializer{topic: topic, spec: spec, reg: reg, lenient: cfg.LenientUnions, schemas: map[string]*avroSchema{}}, nil
}

// Serializer writes a topic's record keys and values as its producer
// writes those of the events the publish steps publish: with its key and
// value serializers and, for KafkaAvroSerializer, its Schema Registry, its
// subject name strategies and its way to the schema ID
// (auto.register.schemas, use.schema.id, use.latest.version, or a lookup).
// It keeps the schemas it parsed and the IDs it was given, and is safe for
// concurrent use.
type Serializer struct {
	topic   string
	spec    *clientSpec
	reg     *registry
	lenient bool

	mu      sync.Mutex
	schemas map[string]*avroSchema
}

// avroSchema is a schema's text parsed, and compacted as it is registered.
type avroSchema struct {
	schema avro.Schema
	text   string
}

// Key serializes a record key as the publish steps serialize an event's: its
// text with a String or ByteArray serializer; with KafkaAvroSerializer, an
// Avro string under the key subject, in the Confluent wire format.
func (z *Serializer) Key(ctx context.Context, key string) ([]byte, error) {
	b, _, err := serializeKey(ctx, z.spec, z.reg, z.topic, &key)
	return b, err
}

// AvroValue serializes value as the "is published using schema" step does
// an event's payload: as a value of schema (an .avsc's text) under the
// subject the producer's value.subject.name.strategy names, with the schema
// ID the registry gives, in Avro's binary encoding framed in the Confluent
// wire format (magic byte 0, the ID in four big-endian bytes, the Avro
// bytes). value is in the generic representation of
// github.com/iskorotkov/avro/v2: a record a map[string]any of its fields, an
// int an int, a long an int64, a union's value nil or map[string]any{"<branch
// type>": value}. The producer must serialize values with
// KafkaAvroSerializer.
func (z *Serializer) AvroValue(ctx context.Context, schema string, value any) ([]byte, error) {
	if err := needsAvro(z.spec, z.topic); err != nil {
		return nil, err
	}
	s, err := z.parse(schema)
	if err != nil {
		return nil, err
	}
	b, _, _, err := serializeAvro(ctx, z.spec, z.reg, z.topic, s.schema, s.text, value)
	return b, err
}

// AvroJSON is AvroValue for a value in Avro's JSON encoding, which an
// event's payload file holds, read as the publish step reads it (with
// packs.kafka.lenientUnions).
func (z *Serializer) AvroJSON(ctx context.Context, schema string, payload []byte) ([]byte, error) {
	if err := needsAvro(z.spec, z.topic); err != nil {
		return nil, err
	}
	s, err := z.parse(schema)
	if err != nil {
		return nil, err
	}
	name := string(s.schema.Type())
	if named, ok := s.schema.(avro.NamedSchema); ok {
		name = named.FullName()
	}
	value, err := decodeAvroJSON(s.schema, payload, z.lenient, z.topic, name)
	if err != nil {
		return nil, err
	}
	b, _, _, err := serializeAvro(ctx, z.spec, z.reg, z.topic, s.schema, s.text, value)
	return b, err
}

// parse returns schema text parsed, once per text.
func (z *Serializer) parse(text string) (*avroSchema, error) {
	z.mu.Lock()
	s, ok := z.schemas[text]
	z.mu.Unlock()
	if ok {
		return s, nil
	}
	schema, err := parseSchema(text)
	if err != nil {
		return nil, fmt.Errorf("invalid Avro schema: %w", err)
	}
	s = &avroSchema{schema: schema, text: compactSchema([]byte(text), schema)}
	z.mu.Lock()
	z.schemas[text] = s
	z.mu.Unlock()
	return s, nil
}

// RecordHeaders are the record headers the publish steps send for an
// event's headers: a null value as the text null, as the Java client sent
// String.valueOf(null).
func RecordHeaders(headers []Header) []kgo.RecordHeader {
	var out []kgo.RecordHeader
	for _, h := range headers {
		v := "null"
		if h.Value != nil {
			v = *h.Value
		}
		out = append(out, kgo.RecordHeader{Key: h.Key, Value: []byte(v)})
	}
	return out
}
