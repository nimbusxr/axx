package kafka

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nimbusxr/axx/internal/avrojson"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// deserializers is a topic client's consumer-side key and value
// deserialization; records cache their decoded form per configuration.
type deserializers struct {
	id         string
	key, value serde
	reg        *registry
}

// decoded is a record as the steps see it: the deserialized key, the
// value's toString() (for Avro, GenericData's rendering of the record) and
// the header values as strings.
type decoded struct {
	Key      *string
	KeyErr   string
	Value    *string
	ValueErr string
	SchemaID int
	Headers  []header

	docOnce sync.Once
	doc     any
	docErr  error
}

type header struct {
	Key   string
	Value *string // nil for a null header value
}

// view returns the record decoded with d, decoding it on first use.
func (r *record) view(ctx context.Context, d *deserializers) *decoded {
	r.mu.Lock()
	v, ok := r.views[d.id]
	r.mu.Unlock()
	if ok {
		return v
	}
	v, transient := d.decode(ctx, r.Record)
	if !transient {
		r.mu.Lock()
		if r.views == nil {
			r.views = map[string]*decoded{}
		}
		r.views[d.id] = v
		r.mu.Unlock()
	}
	return v
}

// decode deserializes a record. transient reports a failure worth retrying
// (the registry could not be reached), which is not cached.
func (d *deserializers) decode(ctx context.Context, r *kgo.Record) (_ *decoded, transient bool) {
	out := &decoded{}
	var err error
	var t1, t2 bool
	out.Key, _, t1, err = d.one(ctx, d.key, r.Key)
	if err != nil {
		out.KeyErr = err.Error()
	}
	out.Value, out.SchemaID, t2, err = d.one(ctx, d.value, r.Value)
	if err != nil {
		out.ValueErr = err.Error()
	}
	for _, h := range r.Headers {
		hv := header{Key: h.Key}
		if h.Value != nil {
			s := javaString(h.Value)
			hv.Value = &s
		}
		out.Headers = append(out.Headers, hv)
	}
	return out, t1 || t2
}

func (d *deserializers) one(ctx context.Context, kind serde, b []byte) (text *string, schemaID int, transient bool, err error) {
	if b == nil {
		return nil, 0, false, nil
	}
	if kind != serdeAvro {
		s := javaString(b)
		return &s, 0, false, nil
	}
	if len(b) < 5 || b[0] != 0 {
		return nil, 0, false, errors.New("not in the Confluent Avro wire format (magic byte 0 and a schema ID)")
	}
	id := int(binary.BigEndian.Uint32(b[1:5]))
	s, err := d.reg.schema(ctx, id)
	if err != nil {
		return nil, id, true, err
	}
	v, err := avrojson.DecodeBinary(s, b[5:])
	if err != nil {
		return nil, id, false, fmt.Errorf("decoding with schema %d: %w", id, err)
	}
	str := avrojson.ObjectString(s, v)
	return &str, id, false, nil
}

// javaString is new String(bytes, UTF_8): malformed input becomes U+FFFD.
func javaString(b []byte) string { return strings.ToValidUTF8(string(b), "�") }

// payloadDoc parses the value like json-path-assert does (json-smart),
// once per decoded record.
func (d *decoded) payloadDoc() (any, error) {
	d.docOnce.Do(func() {
		if d.Value == nil {
			d.docErr = errors.New("the record has no value (tombstone)")
			return
		}
		d.doc, d.docErr = jsonx.Parse(*d.Value)
	})
	return d.doc, d.docErr
}

// headerValues returns the values of header key (distinct for the "headers
// match" step).
func (d *decoded) headerValues(key string, distinct bool) []*string {
	var out []*string
	for _, h := range d.Headers {
		if h.Key != key {
			continue
		}
		if distinct && containsValue(out, h.Value) {
			continue
		}
		out = append(out, h.Value)
	}
	return out
}

func containsValue(vs []*string, v *string) bool {
	for _, x := range vs {
		if (x == nil) == (v == nil) && (x == nil || *x == *v) {
			return true
		}
	}
	return false
}

// recordInfo is a record as world values and failure reports show it.
type recordInfo struct {
	Topic     string       `json:"topic"`
	Partition int32        `json:"partition"`
	Offset    int64        `json:"offset"`
	Timestamp string       `json:"timestamp,omitempty"`
	Key       *string      `json:"key"`
	Headers   []headerInfo `json:"headers,omitempty"`
	Value     any          `json:"value"`
	SchemaID  int          `json:"schemaId,omitempty"`
	Error     string       `json:"error,omitempty"`
	Mismatch  string       `json:"mismatch,omitempty"`
}

type headerInfo struct {
	Key   string  `json:"key"`
	Value *string `json:"value"`
}

// info describes a record; limit truncates long values (0: no limit).
func info(r *record, d *decoded, limit int) recordInfo {
	out := recordInfo{
		Topic: r.Topic, Partition: r.Partition, Offset: r.Offset, Key: d.Key, SchemaID: d.SchemaID,
		Error: strings.TrimSpace(d.KeyErr + " " + d.ValueErr),
	}
	if !r.Timestamp.IsZero() {
		out.Timestamp = r.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	for _, h := range d.Headers {
		out.Headers = append(out.Headers, headerInfo(h))
	}
	if d.Value != nil {
		text := *d.Value
		switch {
		case limit > 0 && len(text) > limit:
			out.Value = text[:limit] + "…"
		case json.Valid([]byte(text)):
			out.Value = json.RawMessage(text)
		default:
			out.Value = text
		}
	}
	return out
}
