package otel

import (
	"os"
	"path/filepath"
	"testing"
)

const spansLine = `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"parcels-api"}}]},"scopeSpans":[{"scope":{"name":"x"},"spans":[{"traceId":"aa","spanId":"11","parentSpanId":"00","name":"POST /api/parcels","kind":2,"startTimeUnixNano":"1000000000","endTimeUnixNano":"1250000000","attributes":[{"key":"http.route","value":{"stringValue":"/api/parcels"}},{"key":"http.response.status_code","value":{"intValue":"201"}},{"key":"weight","value":{"doubleValue":2}},{"key":"signed","value":{"boolValue":true}},{"key":"zones","value":{"arrayValue":{"values":[{"stringValue":"DE-1"},{"intValue":"2"}]}}},{"key":"to","value":{"kvlistValue":{"values":[{"key":"city","value":{"stringValue":"Berlin"}}]}}}],"status":{"code":1}}]}]}]}
`

const metricsLine = `{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"parcels-api"}}]},"scopeMetrics":[{"metrics":[{"name":"parcels_registered","sum":{"aggregationTemporality":2,"isMonotonic":true,"dataPoints":[{"attributes":[{"key":"depot","value":{"stringValue":"BER-1"}}],"startTimeUnixNano":"5","timeUnixNano":"6","asInt":"3"}]}},{"name":"queue_depth","gauge":{"dataPoints":[{"timeUnixNano":"6","asDouble":4}]}},{"name":"parcel_weight","histogram":{"aggregationTemporality":1,"dataPoints":[{"startTimeUnixNano":"5","timeUnixNano":"6","count":"2","sum":12.5}]}}]}]}]}
`

const logsLine = `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"parcels-api"}}]},"scopeLogs":[{"logRecords":[{"traceId":"aa","spanId":"11","severityNumber":17,"body":{"stringValue":"storing failed"},"attributes":[{"key":"reference","value":{"stringValue":"PX-1"}}]},{"severityText":"warn","body":{"stringValue":"slow"}}]}]}]}
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The file exporter's three signals, of their types.
func TestReadsTheCollectorsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	st := &store{path: path}
	if err := st.refresh(); err != nil {
		t.Fatalf("no file yet is no error: %v", err)
	}
	writeFile(t, path, spansLine+metricsLine+logsLine)
	if err := st.refresh(); err != nil {
		t.Fatal(err)
	}
	spans := st.allSpans(func(Span) bool { return true })
	if len(spans) != 1 {
		t.Fatalf("spans: %+v", spans)
	}
	s := spans[0]
	if s.Name != "POST /api/parcels" || s.Kind != "SERVER" || s.Status != "OK" || s.Service != "parcels-api" || s.Duration().Milliseconds() != 250 {
		t.Errorf("span: %+v", s)
	}
	for key, want := range map[string]string{
		"http.route": `"/api/parcels"`, "http.response.status_code": `201`, "weight": `2.0`, "signed": `true`,
		"zones": `["DE-1",2]`, "to": `{"city":"Berlin"}`,
	} {
		if got := s.Attributes[key]; got != want {
			t.Errorf("%s: %s, want %s", key, got, want)
		}
	}
	logs := st.allLogs(func(Log) bool { return true })
	if len(logs) != 2 || logs[0].Severity != "ERROR" || logs[0].Body != "storing failed" || logs[1].Severity != "WARN" {
		t.Errorf("logs: %+v", logs)
	}
	points := st.pointsOf(func(point) bool { return true })
	if len(points) != 3 {
		t.Fatalf("points: %+v", points)
	}
	if p := points[0]; p.kind != kindCounter || !p.cumulative || p.value != "3" {
		t.Errorf("counter: %+v", p)
	}
	if p := points[1]; p.kind != kindGauge || p.value != "4.0" {
		t.Errorf("gauge: %+v", p)
	}
	if p := points[2]; p.kind != kindHistogram || p.cumulative || p.value != "2" {
		t.Errorf("histogram: %+v", p)
	}
}

// A line the Collector is still writing waits for its end, and a Collector that restarts
// starts its file over.
func TestFollowsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	st := &store{path: path}
	writeFile(t, path, spansLine[:40])
	if err := st.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := st.position(); got != 0 {
		t.Errorf("half a line was read: %d", got)
	}
	writeFile(t, path, spansLine)
	if err := st.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := len(st.allSpans(func(Span) bool { return true })); got != 1 {
		t.Errorf("spans: %d", got)
	}
	writeFile(t, path, "")
	if err := st.refresh(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, spansLine+spansLine)
	if err := st.refresh(); err != nil {
		t.Fatal(err)
	}
	if got := len(st.allSpans(func(Span) bool { return true })); got != 3 {
		t.Errorf("spans after the file started over: %d", got)
	}
}
