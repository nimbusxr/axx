//go:build integration

package otel

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

const collectorImage = "otel/opentelemetry-collector:0.164.0"

const collectorConfig = `receivers:
  otlp:
    protocols:
      grpc: {endpoint: 0.0.0.0:4317}
      http: {endpoint: 0.0.0.0:4318}
exporters:
  file:
    path: /out/telemetry.jsonl
    flush_interval: 100ms
service:
  pipelines:
    traces: {receivers: [otlp], exporters: [file]}
    metrics: {receivers: [otlp], exporters: [file]}
    logs: {receivers: [otlp], exporters: [file]}
`

// lab is a real Collector, writing into a folder of the test's.
type lab struct {
	dir, endpoint string
}

var (
	labOnce sync.Once
	theLab  lab
	labErr  error
)

func startLab(t *testing.T) lab {
	t.Helper()
	labOnce.Do(func() {
		ctx := context.Background()
		dir, err := os.MkdirTemp("", "axx-otel")
		if err != nil {
			labErr = err
			return
		}
		config := filepath.Join(dir, "collector.yaml")
		if labErr = os.WriteFile(config, []byte(collectorConfig), 0o644); labErr != nil {
			return
		}
		c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image: collectorImage, ExposedPorts: []string{"4318/tcp"}, User: "0:0",
				Cmd: []string{"--config=/etc/collector.yaml"},
				HostConfigModifier: func(hc *container.HostConfig) {
					hc.Binds = []string{config + ":/etc/collector.yaml:ro", dir + ":/out"}
				},
				WaitingFor: wait.ForLog("Everything is ready").WithStartupTimeout(time.Minute),
			},
			Started: true,
		})
		if err != nil {
			labErr = err
			return
		}
		host, err := c.Host(ctx)
		if err != nil {
			labErr = err
			return
		}
		port, err := c.MappedPort(ctx, "4318/tcp")
		if err != nil {
			labErr = err
			return
		}
		theLab = lab{dir: dir, endpoint: "http://" + host + ":" + port.Port()}
	})
	if labErr != nil {
		t.Skipf("the Collector is unavailable: %v", labErr)
	}
	return theLab
}

// send exports OTLP JSON to the Collector, over HTTP as an SDK does.
func (l lab) send(t *testing.T, signal, body string) {
	t.Helper()
	res, err := http.Post(l.endpoint+"/v1/"+signal, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the Collector answered %d to %s", res.StatusCode, body)
	}
}

const resource = `"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"parcels-api"}}]}`

func (l lab) span(t *testing.T, trace, id, parent, name string, status int, attrs string) {
	t.Helper()
	l.send(t, "traces", fmt.Sprintf(`{"resourceSpans":[{%s,"scopeSpans":[{"spans":[{"traceId":%q,"spanId":%q,"parentSpanId":%q,"name":%q,"kind":2,"startTimeUnixNano":"1000000000","endTimeUnixNano":"1250000000","attributes":[%s],"status":{"code":%d}}]}]}]}`,
		resource, trace, id, parent, name, attrs, status))
}

func (l lab) logRecord(t *testing.T, trace, id, severity, body string) {
	t.Helper()
	l.send(t, "logs", fmt.Sprintf(`{"resourceLogs":[{%s,"scopeLogs":[{"logRecords":[{"traceId":%q,"spanId":%q,"severityText":%q,"body":{"stringValue":%q}}]}]}]}`, resource, trace, id, severity, body))
}

func (l lab) sum(t *testing.T, metric string, temporality int, attr, value, started string) {
	t.Helper()
	l.send(t, "metrics", fmt.Sprintf(`{"resourceMetrics":[{%s,"scopeMetrics":[{"metrics":[{"name":%q,"sum":{"aggregationTemporality":%d,"isMonotonic":true,"dataPoints":[{"attributes":[{"key":"sender","value":{"stringValue":%q}}],"startTimeUnixNano":%q,"timeUnixNano":"2000000000","asInt":%q}]}}]}]}]}`,
		resource, metric, temporality, attr, started, value))
}

func (l lab) histogram(t *testing.T, metric, attr, count string) {
	t.Helper()
	l.send(t, "metrics", fmt.Sprintf(`{"resourceMetrics":[{%s,"scopeMetrics":[{"metrics":[{"name":%q,"histogram":{"aggregationTemporality":1,"dataPoints":[{"attributes":[{"key":"sender","value":{"stringValue":%q}}],"startTimeUnixNano":"1000000000","timeUnixNano":"2000000000","count":%q,"sum":12.5}]}}]}]}]}`,
		resource, metric, attr, count))
}

func (l lab) gaugeInt(t *testing.T, metric, attr, value string) {
	t.Helper()
	l.send(t, "metrics", fmt.Sprintf(`{"resourceMetrics":[{%s,"scopeMetrics":[{"metrics":[{"name":%q,"gauge":{"dataPoints":[{"attributes":[{"key":"sender","value":{"stringValue":%q}}],"timeUnixNano":"2000000000","asInt":%q}]}}]}]}]}`,
		resource, metric, attr, value))
}

type harness struct {
	t   *testing.T
	reg *match.Registry
	sc  *core.Scenario
}

func newHarness(t *testing.T, project string) *harness {
	t.Helper()
	reg := match.NewRegistry()
	if err := reg.AddParams("core", core.ParamsPack().Manifest().Params); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddPack(Name, Pack().Manifest()); err != nil {
		t.Fatal(err)
	}
	suite := core.NewSuite(core.SuiteOptions{ProjectDir: project})
	t.Cleanup(func() { _ = suite.Close(context.Background()) })
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: t.Name(), Name: t.Name()}, suite, nil)
	t.Cleanup(func() { _ = sc.Close() })
	return &harness{t: t, reg: reg, sc: sc}
}

func (h *harness) step(text string, rows ...[]string) error {
	h.t.Helper()
	ms := h.reg.Match(text)
	if len(ms) != 1 {
		h.t.Fatalf("%q matched %d definitions", text, len(ms))
	}
	var tbl *core.Table
	if rows != nil {
		tbl = &core.Table{Rows: rows}
	}
	args, err := h.reg.Resolve(h.sc, ms[0], text, tbl, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	h.sc.SetContext(ctx)
	return ms[0].Def().Step.Run(h.sc, args)
}

func (h *harness) ok(text string, rows ...[]string) {
	h.t.Helper()
	if err := h.step(text, rows...); err != nil {
		h.t.Fatalf("%s: %v", text, err)
	}
}

func (h *harness) fails(want, text string, rows ...[]string) {
	h.t.Helper()
	err := h.step(text, rows...)
	if err == nil || !strings.Contains(err.Error(), want) {
		h.t.Errorf("%s: %v, want a failure with %q", text, err, want)
	}
}

func (h *harness) telemetry() {
	h.t.Helper()
	h.ok("the parcels-api telemetry with the following properties:", []string{"url", "file://telemetry.jsonl"}, []string{"service", "parcels-api"})
}

// The scenario's trace ID, from the traceparent its requests carry.
func (h *harness) trace() string {
	h.t.Helper()
	parts := strings.Split(h.sc.RequestHeaders()["traceparent"], "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || parts[3] != "01" {
		h.t.Fatalf("traceparent: %v", h.sc.RequestHeaders())
	}
	return parts[1]
}

func TestTheScenariosTrace(t *testing.T) {
	l := startLab(t)
	h := newHarness(t, l.dir)
	h.telemetry()
	trace := h.trace()

	other := newHarness(t, l.dir)
	other.telemetry()
	if other.trace() == trace {
		t.Fatal("two scenarios share a trace")
	}

	l.span(t, trace, "1111111111111111", "ffffffffffffffff", "POST /api/parcels", 1,
		`{"key":"http.route","value":{"stringValue":"/api/parcels"}},{"key":"http.response.status_code","value":{"intValue":"201"}},{"key":"weight","value":{"doubleValue":2}}`)
	l.span(t, trace, "2222222222222222", "1111111111111111", "store parcel", 0, "")
	l.logRecord(t, trace, "1111111111111111", "ERROR", "the printer is out of labels")
	// Another scenario's, at the same time.
	l.span(t, other.trace(), "3333333333333333", "", "GET /api/parcels", 2, "")

	h.ok("within 10s the trace has a span where:",
		[]string{"name", "POST /api/parcels"},
		[]string{"status", "OK"},
		[]string{"kind", "SERVER"},
		[]string{"service", "parcels-api"},
		[]string{"attribute http.route", "/api/parcels"},
		[]string{"attribute http.response.status_code", "201"},
		[]string{"attribute weight", "2.0"},
		[]string{"attribute absent", "undefined"})
	h.ok("the trace has no span where:", []string{"status", "ERROR"})
	h.ok(`the "store parcel" span is a child of the "POST /api/parcels" span`)
	h.ok(`the "POST /api/parcels" span took less than 500ms`)
	h.ok("the trace has a log where:", []string{"severity", "ERROR"}, []string{"body", "out of labels"})

	// Values by the rule, and what a failure says.
	h.fails(`write`, "within 1s the trace has a span where:", []string{"attribute http.response.status_code", `"201"`})
	h.fails(`write`, "within 1s the trace has a span where:", []string{"attribute weight", "2"})
	h.fails("POST /api/parcels", "within 1s the trace has a span where:", []string{"name", "GET /nothing"})
	h.fails("should have no such span", "the trace has no span where:", []string{"name", "store parcel"})
	h.fails("took too long", `the "POST /api/parcels" span took less than 100ms`)
	h.fails("unknown row", "the trace has no span where:", []string{"colour", "red"})
	if d, ok := h.sc.Descriptions()[Name].(map[string]any); !ok || d["traceId"] != trace || d["spans"] == nil {
		t.Errorf("the report's description: %v", h.sc.Descriptions()[Name])
	}

	// The other scenario sees its own.
	other.ok("within 10s the trace has a span where:", []string{"status", "ERROR"})
	other.fails("no such span", "within 1s the trace has a span where:", []string{"name", "store parcel"})
}

func TestAnEmptyTraceSaysWhy(t *testing.T) {
	l := startLab(t)
	h := newHarness(t, l.dir)
	h.fails("telemetry with the following properties", "the trace has no span where:", []string{"status", "ERROR"})
	h.telemetry()
	h.fails("W3C Trace Context", "within 1s the trace has a span where:", []string{"name", "x"})
	h.fails("no log at all", "within 1s the trace has a log where:", []string{"severity", "ERROR"})
}

func TestMetrics(t *testing.T) {
	l := startLab(t)
	// Before the scenario: what a cumulative counter already holds.
	l.sum(t, "parcels_registered", 2, "juniper-and-jay", "5", "1000000000")
	time.Sleep(time.Second)
	h := newHarness(t, l.dir)
	h.telemetry()
	where := []string{"attribute sender", "juniper-and-jay"}

	h.ok("the parcels-api telemetry counted 0 parcels_registered where:", where)
	l.sum(t, "parcels_registered", 2, "juniper-and-jay", "6", "1000000000")
	h.ok("within 10s the parcels-api telemetry counted 1 parcels_registered where:", where)
	h.fails("more parcels_registered", "the parcels-api telemetry counted 0 parcels_registered where:", where)

	// A delta, whose points add up; and another sender, counted apart.
	l.sum(t, "parcels_rejected", 1, "juniper-and-jay", "2", "1000000000")
	l.sum(t, "parcels_rejected", 1, "juniper-and-jay", "1", "2000000000")
	l.sum(t, "parcels_rejected", 1, "wisteria-way", "9", "2000000000")
	h.ok("within 10s the parcels-api telemetry counted 3 parcels_rejected where:", where)
	h.ok("within 10s the parcels-api telemetry counted 12 parcels_rejected")

	// A service that restarted starts its cumulative counter again.
	l.sum(t, "parcels_registered", 2, "lark-ceramics", "4", "1000000000")
	h2 := newHarness(t, l.dir)
	time.Sleep(time.Second)
	h2.telemetry()
	l.sum(t, "parcels_registered", 2, "lark-ceramics", "1", "9000000000")
	h2.ok("within 10s the parcels-api telemetry counted 1 parcels_registered where:", []string{"attribute sender", "lark-ceramics"})

	// More than the number fails at once.
	start := time.Now()
	h.fails("more parcels_rejected", "within 30s the parcels-api telemetry counted 1 parcels_rejected where:", where)
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("it took %s to fail", took)
	}

	l.histogram(t, "parcel_weight", "juniper-and-jay", "2")
	h.ok("within 10s the parcels-api telemetry recorded 2 parcel_weight values where:", where)
	h.fails("OTEL_METRIC_EXPORT_INTERVAL", "within 1s the parcels-api telemetry recorded 3 parcel_weight values where:", where)

	l.gaugeInt(t, "last_parcel_weight", "juniper-and-jay", "1200")
	h.ok("within 10s the parcels-api telemetry's last_parcel_weight gauge is 1200 where:", where)
	h.fails("write", "within 1s the parcels-api telemetry's last_parcel_weight gauge is 1200.0 where:", where)
	l.gaugeInt(t, "last_parcel_weight", "juniper-and-jay", "900")
	h.ok("within 10s the parcels-api telemetry's last_parcel_weight gauge is 900 where:", where)

	h.fails("parcels_registered", "within 1s the parcels-api telemetry counted 1 parcels_unknown")
}

func TestTheTelemetryDoesNotContain(t *testing.T) {
	l := startLab(t)
	h := newHarness(t, l.dir)
	h.telemetry()
	h.fails("looked at nothing", "the telemetry does not contain 'Marta Keller'")
	l.span(t, h.trace(), "1111111111111111", "", "POST /api/parcels", 1, `{"key":"recipient","value":{"stringValue":"Tim \"the\" Vogel"}}`)
	h.ok("within 10s the trace has a span where:", []string{"name", "POST /api/parcels"})
	h.ok("the telemetry does not contain 'Marta Keller'")
	h.fails("contains", `the telemetry does not contain 'Tim "the" Vogel'`)
}
