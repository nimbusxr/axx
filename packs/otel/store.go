package otel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Span is a span the Collector wrote.
type Span struct {
	TraceID, SpanID, ParentSpanID string
	Name                          string
	// Kind is SERVER, CLIENT, INTERNAL, PRODUCER or CONSUMER.
	Kind string
	// Status is OK, ERROR or UNSET.
	Status string
	// Service is its resource's service.name.
	Service    string
	Start, End time.Time
	// Attributes are its attributes' values, each as JSON.
	Attributes map[string]string
}

// Duration is how long the span took.
func (s Span) Duration() time.Duration { return s.End.Sub(s.Start) }

// Log is a log record the Collector wrote.
type Log struct {
	TraceID, SpanID string
	// Severity is its severity's text, in upper case, or the name of its
	// severity's number when it has no text: ERROR.
	Severity string
	Body     string
	Service  string
	// Attributes are its attributes' values, each as JSON.
	Attributes map[string]string
}

// metricKind is what a metric's data points hold.
type metricKind string

const (
	kindCounter   metricKind = "counter"
	kindGauge     metricKind = "gauge"
	kindHistogram metricKind = "histogram"
)

// point is a metric's data point, of a series: the resource's service and the point's
// attributes.
type point struct {
	metric, series, service string
	kind                    metricKind
	cumulative              bool
	// value is a counter's or a gauge's number, as JSON, or a histogram's count.
	value      string
	attributes map[string]string
	start      string // when a cumulative series started: a new one has reset
	seq        int
}

// number is the point's value as a number.
func (p point) number() float64 {
	n, _ := strconv.ParseFloat(p.value, 64)
	return n
}

// entry is a thing of the file, with the place of its line, so a scenario sees what
// came after it started.
type entry[T any] struct {
	value T
	seq   int
}

// store is what a Collector's file exporter has written so far, for the whole run.
type store struct {
	path string

	mu     sync.Mutex
	offset int64
	seq    int
	spans  []entry[Span]
	logs   []entry[Log]
	points []point
	lines  []entry[string]
	bad    int // lines that are not OTLP JSON
}

// refresh reads what the Collector has appended. A Collector that restarts empties its
// file unless told to append, so a shorter file starts over.
func (st *store) refresh() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	f, err := os.Open(st.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < st.offset {
		st.offset = 0
	}
	if info.Size() == st.offset {
		return nil
	}
	if _, err := f.Seek(st.offset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return nil // a line the Collector is still writing waits
		}
		if err != nil {
			return err
		}
		st.offset += int64(len(line))
		if line = bytes.TrimSpace(line); len(line) > 0 {
			st.seq++
			st.lines = append(st.lines, entry[string]{string(line), st.seq})
			if st.read(line) != nil {
				st.bad++
			}
		}
	}
}

func decodeNumbers(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

type otlpValue map[string]any

type otlpAttribute struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

func (r otlpResource) service() string {
	for _, a := range r.Attributes {
		if a.Key == "service.name" {
			s, _ := a.Value["stringValue"].(string)
			return s
		}
	}
	return ""
}

type otlpFile struct {
	ResourceSpans []struct {
		Resource   otlpResource `json:"resource"`
		ScopeSpans []struct {
			Spans []struct {
				TraceID           string          `json:"traceId"`
				SpanID            string          `json:"spanId"`
				ParentSpanID      string          `json:"parentSpanId"`
				Name              string          `json:"name"`
				Kind              any             `json:"kind"`
				StartTimeUnixNano any             `json:"startTimeUnixNano"`
				EndTimeUnixNano   any             `json:"endTimeUnixNano"`
				Attributes        []otlpAttribute `json:"attributes"`
				Status            struct {
					Code any `json:"code"`
				} `json:"status"`
			} `json:"spans"`
		} `json:"scopeSpans"`
	} `json:"resourceSpans"`
	ResourceLogs []struct {
		Resource  otlpResource `json:"resource"`
		ScopeLogs []struct {
			LogRecords []struct {
				TraceID        string          `json:"traceId"`
				SpanID         string          `json:"spanId"`
				SeverityText   string          `json:"severityText"`
				SeverityNumber any             `json:"severityNumber"`
				Body           otlpValue       `json:"body"`
				Attributes     []otlpAttribute `json:"attributes"`
			} `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
	ResourceMetrics []struct {
		Resource     otlpResource `json:"resource"`
		ScopeMetrics []struct {
			Metrics []otlpMetric `json:"metrics"`
		} `json:"scopeMetrics"`
	} `json:"resourceMetrics"`
}

type otlpPoints struct {
	AggregationTemporality any `json:"aggregationTemporality"`
	DataPoints             []struct {
		Attributes        []otlpAttribute `json:"attributes"`
		StartTimeUnixNano any             `json:"startTimeUnixNano"`
		AsInt             any             `json:"asInt"`
		AsDouble          any             `json:"asDouble"`
		Count             any             `json:"count"`
	} `json:"dataPoints"`
}

type otlpMetric struct {
	Name                 string      `json:"name"`
	Sum                  *otlpPoints `json:"sum"`
	Gauge                *otlpPoints `json:"gauge"`
	Histogram            *otlpPoints `json:"histogram"`
	ExponentialHistogram *otlpPoints `json:"exponentialHistogram"`
	Summary              *otlpPoints `json:"summary"`
}

func (st *store) read(line []byte) error {
	var f otlpFile
	if err := decodeNumbers(line, &f); err != nil {
		return err
	}
	for _, rs := range f.ResourceSpans {
		service := rs.Resource.service()
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				st.spans = append(st.spans, entry[Span]{Span{
					TraceID: s.TraceID, SpanID: s.SpanID, ParentSpanID: s.ParentSpanID, Name: s.Name, Service: service,
					Kind: spanKind(s.Kind), Status: statusCode(s.Status.Code),
					Start: nanos(s.StartTimeUnixNano), End: nanos(s.EndTimeUnixNano), Attributes: attributes(s.Attributes),
				}, st.seq})
			}
		}
	}
	for _, rl := range f.ResourceLogs {
		service := rl.Resource.service()
		for _, sl := range rl.ScopeLogs {
			for _, l := range sl.LogRecords {
				st.logs = append(st.logs, entry[Log]{Log{
					TraceID: l.TraceID, SpanID: l.SpanID, Service: service, Body: bodyText(l.Body),
					Severity: severity(l.SeverityText, l.SeverityNumber), Attributes: attributes(l.Attributes),
				}, st.seq})
			}
		}
	}
	for _, rm := range f.ResourceMetrics {
		service := rm.Resource.service()
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				st.readMetric(m, service)
			}
		}
	}
	return nil
}

func (st *store) readMetric(m otlpMetric, service string) {
	add := func(points *otlpPoints, kind metricKind) {
		if points == nil {
			return
		}
		cumulative := temporalityCumulative(points.AggregationTemporality)
		for _, d := range points.DataPoints {
			attrs := attributes(d.Attributes)
			value := ""
			switch {
			case kind == kindHistogram:
				value = numberText(d.Count)
			case d.AsInt != nil:
				value = numberText(d.AsInt)
			case d.AsDouble != nil:
				value = decimalText(d.AsDouble)
			}
			if value == "" {
				continue
			}
			st.points = append(st.points, point{
				metric: m.Name, service: service, kind: kind, cumulative: cumulative, value: value, attributes: attrs,
				series: seriesKey(service, attrs), start: fmt.Sprint(d.StartTimeUnixNano), seq: st.seq,
			})
		}
	}
	add(m.Sum, kindCounter)
	add(m.Gauge, kindGauge)
	add(m.Histogram, kindHistogram)
	add(m.ExponentialHistogram, kindHistogram)
	add(m.Summary, kindHistogram)
}

func seriesKey(service string, attrs map[string]string) string {
	var b strings.Builder
	b.WriteString(service)
	for _, k := range slices.Sorted(maps.Keys(attrs)) {
		b.WriteString("\x00" + k + "=" + attrs[k])
	}
	return b.String()
}

// temporalityCumulative reads a metric's temporality: 2 or its name.
func temporalityCumulative(v any) bool {
	return fmt.Sprint(v) == "2" || fmt.Sprint(v) == "AGGREGATION_TEMPORALITY_CUMULATIVE"
}

// numberText is an integer of OTLP's JSON, which writes a 64-bit one as a string.
func numberText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

// decimalText is a double, whole ones too: a source keeps its type (ADR 0014).
func decimalText(v any) string {
	s := numberText(v)
	if s != "" && !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func nanos(v any) time.Time {
	n, err := strconv.ParseInt(numberText(v), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, n)
}

func attributes(attrs []otlpAttribute) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, a := range attrs {
		out[a.Key] = valueJSON(a.Value)
	}
	return out
}

// valueJSON is an attribute's value as JSON, of its type.
func valueJSON(v otlpValue) string {
	marshal := func(x any) string {
		b, _ := json.Marshal(x)
		return string(b)
	}
	switch {
	case v["stringValue"] != nil:
		return marshal(v["stringValue"])
	case v["bytesValue"] != nil:
		return marshal(v["bytesValue"])
	case v["boolValue"] != nil:
		return marshal(v["boolValue"])
	case v["intValue"] != nil:
		return numberText(v["intValue"])
	case v["doubleValue"] != nil:
		return decimalText(v["doubleValue"])
	case v["arrayValue"] != nil:
		list, _ := v["arrayValue"].(map[string]any)
		values, _ := list["values"].([]any)
		items := make([]string, len(values))
		for i, x := range values {
			m, _ := x.(map[string]any)
			items[i] = valueJSON(m)
		}
		return "[" + strings.Join(items, ",") + "]"
	case v["kvlistValue"] != nil:
		list, _ := v["kvlistValue"].(map[string]any)
		values, _ := list["values"].([]any)
		items := make([]string, len(values))
		for i, x := range values {
			kv, _ := x.(map[string]any)
			value, _ := kv["value"].(map[string]any)
			items[i] = marshal(kv["key"]) + ":" + valueJSON(value)
		}
		return "{" + strings.Join(items, ",") + "}"
	}
	return "null"
}

func bodyText(v otlpValue) string {
	if s, ok := v["stringValue"].(string); ok {
		return s
	}
	if len(v) == 0 {
		return ""
	}
	return valueJSON(v)
}

// spanKind reads a span's kind: its number, in the Collector's JSON, or its name.
func spanKind(v any) string {
	names := map[string]string{"1": "INTERNAL", "2": "SERVER", "3": "CLIENT", "4": "PRODUCER", "5": "CONSUMER"}
	text := fmt.Sprint(v)
	if name, ok := names[text]; ok {
		return name
	}
	if name, ok := strings.CutPrefix(text, "SPAN_KIND_"); ok {
		return name
	}
	return "UNSPECIFIED"
}

// statusCode reads a span's status: its number or its name.
func statusCode(v any) string {
	switch text := fmt.Sprint(v); text {
	case "1", "STATUS_CODE_OK":
		return "OK"
	case "2", "STATUS_CODE_ERROR":
		return "ERROR"
	}
	return "UNSET"
}

// severity is a record's severity's text, or the name of its number.
func severity(text string, number any) string {
	if text != "" {
		return strings.ToUpper(text)
	}
	n, _ := strconv.Atoi(fmt.Sprint(number))
	switch {
	case n >= 21:
		return "FATAL"
	case n >= 17:
		return "ERROR"
	case n >= 13:
		return "WARN"
	case n >= 9:
		return "INFO"
	case n >= 5:
		return "DEBUG"
	case n >= 1:
		return "TRACE"
	}
	return ""
}

func (st *store) position() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.seq
}

func (st *store) allSpans(match func(Span) bool) []Span {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Span
	for _, e := range st.spans {
		if match(e.value) {
			out = append(out, e.value)
		}
	}
	return out
}

func (st *store) allLogs(match func(Log) bool) []Log {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Log
	for _, e := range st.logs {
		if match(e.value) {
			out = append(out, e.value)
		}
	}
	return out
}

func (st *store) pointsOf(match func(point) bool) []point {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []point
	for _, p := range st.points {
		if match(p) {
			out = append(out, p)
		}
	}
	return out
}

func (st *store) linesSince(seq int) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []string
	for _, e := range st.lines {
		if e.seq > seq {
			out = append(out, e.value)
		}
	}
	return out
}
