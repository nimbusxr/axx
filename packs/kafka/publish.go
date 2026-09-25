package kafka

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/iskorotkov/avro/v2"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/avrojson"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// addClient creates a topic client from the Java properties in t (nil for
// the defaults).
func addClient(sc *core.Scenario, svc *Service, topic string, t *core.Table) error {
	if _, ok := svc.client(topic); ok {
		return fmt.Errorf("Kafka topic client already exists: %s", topic) //nolint:staticcheck // user-facing message
	}
	var producerProps, consumerProps []prop
	if t != nil {
		pairs, err := t.Pairs()
		if err != nil {
			return err
		}
		for _, p := range pairs {
			key, isProducer := strings.CutPrefix(p.Key, "producer.")
			if !isProducer {
				var isConsumer bool
				if key, isConsumer = strings.CutPrefix(p.Key, "consumer."); !isConsumer {
					sc.Log("warning: property %q is ignored: prefix it with producer. or consumer.", p.Key)
					continue
				}
			}
			if p.Null {
				return fmt.Errorf("property %s has no value", p.Key)
			}
			pr := prop{Key: key, Value: sc.Suite().Interpolate(p.Value)}
			if isProducer {
				producerProps = append(producerProps, pr)
			} else {
				consumerProps = append(consumerProps, pr)
			}
		}
	}
	cfg, err := configOf(sc.Suite())
	if err != nil {
		return err
	}
	pspec, notes, err := translate(roleProducer, svc.Brokers, producerProps)
	if err != nil {
		return err
	}
	cspec, cnotes, err := translate(roleConsumer, svc.Brokers, consumerProps)
	if err != nil {
		return err
	}
	for _, n := range append(notes, cnotes...) {
		sc.Log("%s", n)
	}
	tc := &TopicClient{Topic: topic, Service: svc, producer: pspec, consumer: cspec}
	if tc.prod, err = producerFor(sc.Suite(), pspec); err != nil {
		return fmt.Errorf("creating the %s producer: %w", topic, err)
	}
	if pspec.Key == serdeAvro || pspec.Value == serdeAvro {
		if tc.preg, err = registryFor(sc.Suite(), pspec); err != nil {
			return err
		}
	}
	tc.deser = &deserializers{key: cspec.Key, value: cspec.Value}
	if cspec.Key == serdeAvro || cspec.Value == serdeAvro {
		if tc.deser.reg, err = registryFor(sc.Suite(), cspec); err != nil {
			return err
		}
	}
	tc.deser.id = fmt.Sprintf("%d/%d/%p", cspec.Key, cspec.Value, tc.deser.reg)
	if tc.tailer, err = tailerFor(sc.Suite(), cspec, topic, cfg.MaxRecords); err != nil {
		return fmt.Errorf("creating the %s consumer: %w", topic, err)
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	for _, c := range svc.clients {
		if c.Topic == topic {
			return fmt.Errorf("Kafka topic client already exists: %s", topic) //nolint:staticcheck // user-facing message
		}
	}
	svc.clients = append(svc.clients, tc)
	return nil
}

func loadPayload(sc *core.Scenario, ev *Event, file string) error {
	path, err := sc.Suite().ResolvePath(file)
	if err != nil {
		return fmt.Errorf("Error reading file: %w", err) //nolint:staticcheck // user-facing message
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Error reading file: %w", err) //nolint:staticcheck // user-facing message
	}
	ev.Payload = string(b)
	return nil
}

// setProperties sets each row's JSONPath to its value as a string (an empty
// cell sets null), like DocumentContext.set on the payload.
func setProperties(ev *Event, t *core.Table) error {
	pairs, err := t.Pairs()
	if err != nil {
		return err
	}
	doc, err := parsePayload(ev)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		var v any = p.Value
		if p.Null {
			v = nil
		}
		if err := setPath(doc, p.Key, v); err != nil {
			return err
		}
	}
	return writePayload(ev, doc)
}

func setProperty(ev *Event, path string, v any) error {
	doc, err := parsePayload(ev)
	if err != nil {
		return err
	}
	if err := setPath(doc, path, v); err != nil {
		return err
	}
	return writePayload(ev, doc)
}

func parsePayload(ev *Event) (any, error) {
	doc, err := jsonx.Parse(ev.Payload)
	if err != nil {
		return nil, fmt.Errorf("the kafka event payload is not JSON: %w", err)
	}
	return doc, nil
}

func setPath(doc any, path string, v any) error {
	err := jsonx.Set(doc, path, v)
	if err == nil {
		return nil
	}
	msg := err.Error()
	var je *jsonx.Error
	if errors.As(err, &je) {
		msg = je.JavaMessage()
		if je.Kind.Is(jsonx.PathNotFound) && je.NoMessage {
			msg = path + " does not exist in the payload; the property must already be there (set it in the payload file)"
		}
	}
	return fmt.Errorf("Property not found in Kafka event payload: %s", msg) //nolint:staticcheck // user-facing message
}

func writePayload(ev *Event, doc any) error {
	s, err := jsonx.Marshal(doc)
	if err != nil {
		return err
	}
	ev.Payload = s
	return nil
}

// serializeKey applies the producer's key serializer.
func (tc *TopicClient) serializeKey(sc *core.Scenario, key *string) ([]byte, string, error) {
	if key == nil {
		return nil, "", nil
	}
	if tc.producer.Key != serdeAvro {
		return []byte(*key), "", nil
	}
	// KafkaAvroSerializer writes a String key with the primitive "string" schema.
	s := avro.MustParse(`"string"`)
	subject, err := subjectFor(tc.producer.Registry.KeyStrategy, tc.Topic, "key", s)
	if err != nil {
		return nil, "", err
	}
	id, writer, err := tc.preg.writerID(sc.Context(), tc.producer.Registry, subject, `"string"`, s)
	if err != nil {
		return nil, "", fmt.Errorf("serializing the key: %w", err)
	}
	b, err := avrojson.Marshal(writer, *key)
	if err != nil {
		return nil, "", fmt.Errorf("serializing the key: %w", err)
	}
	return wire(id, b), subject, nil
}

// wire is the Confluent wire format: magic byte 0, the schema ID (4 bytes,
// big-endian) and the Avro binary.
func wire(id int, avroBytes []byte) []byte {
	out := make([]byte, 5, 5+len(avroBytes))
	binary.BigEndian.PutUint32(out[1:], uint32(id))
	return append(out, avroBytes...)
}

// subjectFor names the registry subject for a schema.
func subjectFor(strategy, topic, kind string, s avro.Schema) (string, error) {
	switch strategy {
	case recordNameStrategy, topicRecordStrategy:
		rs, ok := s.(*avro.RecordSchema)
		if !ok {
			return "", fmt.Errorf("%s needs a record schema, got %s", strategy[strings.LastIndexByte(strategy, '.')+1:], s.Type())
		}
		if strategy == recordNameStrategy {
			return rs.FullName(), nil
		}
		return topic + "-" + rs.FullName(), nil
	}
	return topic + "-" + kind, nil
}

func (tc *TopicClient) publishAvro(sc *core.Scenario, ev *Event, schemaPath string) error {
	if !strings.HasSuffix(schemaPath, ".avsc") {
		return fmt.Errorf("Supported schema types are .avsc") //nolint:staticcheck // user-facing message
	}
	if tc.producer.Value != serdeAvro {
		return fmt.Errorf("the %s kafka topic client serializes values with %sSerializer; publishing with a schema needs "+
			"producer.value.serializer=%s and producer.schema.registry.url", tc.Topic, tc.producer.Value, avroSerializer)
	}
	path, err := sc.Suite().ResolvePath(schemaPath)
	if err != nil {
		return fmt.Errorf("Schema file not found: %s: %w", schemaPath, err) //nolint:staticcheck // user-facing message
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Schema file not found: %s: %w", schemaPath, err) //nolint:staticcheck // user-facing message
	}
	schema, err := parseSchema(string(raw))
	if err != nil {
		return fmt.Errorf("invalid Avro schema %s: %w", schemaPath, err)
	}
	cfg, err := configOf(sc.Suite())
	if err != nil {
		return err
	}
	value, err := avrojson.Decode(schema, []byte(ev.Payload), avrojson.Options{LenientUnions: cfg.LenientUnions})
	if err != nil {
		hint := ""
		var de *avrojson.Error
		if errors.As(err, &de) && strings.Contains(de.Msg, "union value") && !cfg.LenientUnions {
			hint = " (Avro's JSON encoding wraps union values as {\"<branch>\": value}; set packs.kafka.lenientUnions to accept bare values)"
		}
		return fmt.Errorf("the %s kafka event payload does not match schema %s: %w%s", tc.Topic, schemaPath, err, hint)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		compact.Reset()
		compact.WriteString(schema.String())
	}
	subject, err := subjectFor(tc.producer.Registry.ValueStrategy, tc.Topic, "value", schema)
	if err != nil {
		return err
	}
	id, writer, err := tc.preg.writerID(sc.Context(), tc.producer.Registry, subject, compact.String(), schema)
	if err != nil {
		return err
	}
	body, err := avrojson.Marshal(writer, value)
	if err != nil {
		return fmt.Errorf("serializing the %s kafka event with schema %d: %w", tc.Topic, id, err)
	}
	sc.Attach("application/json", []byte(ev.Payload), tc.Topic+" kafka event")
	return tc.produce(sc, ev, wire(id, body), published{SchemaID: id, Subject: subject, Schema: schemaPath})
}

func (tc *TopicClient) publishPlain(sc *core.Scenario, ev *Event) error {
	if tc.producer.Value == serdeAvro {
		return fmt.Errorf("the %s kafka topic client serializes values with KafkaAvroSerializer: publish with \"is published using schema <file>.avsc\"", tc.Topic)
	}
	sc.Attach("text/plain", []byte(ev.Payload), tc.Topic+" kafka event")
	return tc.produce(sc, ev, []byte(ev.Payload), published{})
}

func (tc *TopicClient) produce(sc *core.Scenario, ev *Event, value []byte, info published) error {
	key, keySubject, err := tc.serializeKey(sc, ev.Key)
	if err != nil {
		return err
	}
	rec := &kgo.Record{Topic: tc.Topic, Key: key, Value: value}
	for _, h := range ev.Headers {
		v := "null"
		if h.Value != nil {
			v = *h.Value
		}
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: h.Key, Value: []byte(v)})
	}
	res, err := tc.prod.ProduceSync(sc.Context(), rec).First()
	if err != nil {
		return fmt.Errorf("publishing the %s kafka event: %w", tc.Topic, err)
	}
	info.Partition, info.Offset = res.Partition, res.Offset
	if !res.Timestamp.IsZero() {
		info.Timestamp = res.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	tc.mu.Lock()
	ev.Published = append(ev.Published, info)
	tc.mu.Unlock()
	msg := fmt.Sprintf("published to %s partition %d offset %d", tc.Topic, res.Partition, res.Offset)
	if info.SchemaID > 0 {
		msg += fmt.Sprintf(" (schema %d, subject %s)", info.SchemaID, info.Subject)
	}
	if keySubject != "" {
		msg += fmt.Sprintf(" (Avro key, subject %s)", keySubject)
	}
	sc.Log("%s", msg)
	return nil
}
