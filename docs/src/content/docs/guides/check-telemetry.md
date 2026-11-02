---
title: Check telemetry
description: Check the traces, logs and metrics your services report through OpenTelemetry, each scenario with a trace of its own, read from what a real Collector writes.
---

Your services report what they do through OpenTelemetry. The `otel` pack checks that report: the spans of what a scenario did, the logs that came with them, and the metrics they counted.

```gherkin
Scenario: A registered parcel is traced, logged and counted
  Given a POST request to /api/parcels
  And a request payload using an application/json content example
  And the request payload property reference is 'PX-OTL-9801'
  And the request payload property sender is 'juniper-and-jay'
  When the request is executed
  Then the response status code is 201
  And within 10s the trace has a span where:
    | name                 | POST /api/parcels |
    | attribute http.route | /api/parcels      |
  And the "store parcel" span is a child of the "POST /api/parcels" span
  And the trace has no span where:
    | status | ERROR |
  And within 30s the parcels-api telemetry counted 1 parcels_registered where:
    | attribute sender | juniper-and-jay |
```

Add the pack to the project with `axx pack add otel` ([Choose packs](/guides/use-packs/)).

## Run a Collector

Your service exports OTLP as it does in production, over gRPC or HTTP, to a real [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/) in your test environment. The Collector's `file` exporter writes everything to one file, where axx reads it. It needs no axx to run, so you can start it on its own to look at what a service reports, and add other exporters to send the same data to Jaeger or Grafana.

```yaml title="otel-collector.yaml"
receivers:
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
```

```yaml title="compose.yaml"
services:
  otel-collector:
    image: otel/opentelemetry-collector:0.164.0
    command: ["--config=/etc/otel-collector.yaml"]
    # The exporter writes into the folder below, which root owns on Linux.
    user: "0:0"
    volumes:
      - './otel-collector.yaml:/etc/otel-collector.yaml:ro'
      - '../acceptance/.axx/otel:/out'

  app:
    environment:
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
      OTEL_SERVICE_NAME: parcels-api
      # Metrics, in milliseconds: every 60 seconds by default, too slow for a test.
      OTEL_METRIC_EXPORT_INTERVAL: "1000"
      # Spans and logs are batched: every 5 seconds by default.
      OTEL_BSP_SCHEDULE_DELAY: "200"
      OTEL_BLRP_SCHEDULE_DELAY: "200"
```

Pin the Collector's version: the `file` exporter is alpha, and its fields can change between versions. It empties its file when it restarts, unless you set `append: true`, which it cannot combine with `rotation`.

Then register what it writes, usually in the `Background`:

```gherkin
Given the parcels-api telemetry with the following properties:
  | url     | file://.axx/otel/telemetry.jsonl |
  | service | parcels-api                      |
```

A relative path is from the directory of `axx.yaml`. `service` limits the metric checks that name this telemetry to that `service.name`.

## A trace of each scenario's own

Once telemetry is registered, every request the scenario sends carries one `traceparent` header, with a trace ID of the scenario's own. A service that follows W3C Trace Context, as OpenTelemetry's SDKs do by default, puts its spans in that trace. So `the trace` is exactly what this scenario did, however many scenarios run beside it. That goes for REST, GraphQL, JSON-RPC, gRPC, MCP, A2A, WebSockets and event streams; a browser's requests do not carry it. A request that sets its own `traceparent` keeps it.

| Step | Checks |
| --- | --- |
| `the trace has a span where:` | A span with those values has arrived |
| `the trace has no span where:` | No span with those values has arrived |
| `the "store parcel" span is a child of the "POST /api/parcels" span` | The first has the second for its parent |
| `the "POST /api/parcels" span took less than 500ms` | Every span of that name took less |
| `the trace has a log where:` | A log record with those values has arrived, logged with the span's context |

A span's table takes `name`, `status` (`OK`, `ERROR`, `UNSET`), `kind`, `service` and `attribute <key>`. A log's takes `severity`, `body` (text it contains), `service` and `attribute <key>`. Attributes keep their types: `| attribute http.response.status_code | 201 |` is the integer, and `undefined` is an attribute that is not there.

## Metrics

| Step | Checks |
| --- | --- |
| `the parcels-api telemetry counted 1 parcels_registered` | What a counter added since the telemetry was registered |
| `the parcels-api telemetry recorded 3 parcel_weight values` | How many values a histogram recorded since then |
| `the parcels-api telemetry's queue_depth gauge is 4` | The latest value a gauge reported since then |

Each also takes `where:` and a table of `attribute <key>` rows. A cumulative counter, which is OpenTelemetry's default, and a delta counter count alike, and a counter that starts again because its service restarted is followed.

Metrics are not per trace, so scenarios running at the same time count together. Give a check an attribute that is its scenario's own, like its shop.

## What must stay out

```gherkin
Then the telemetry does not contain 'Marta Keller'
```

This looks through everything the services reported since the telemetry was registered, which is how to check that a person's details stay out of your telemetry.

## When checks wait

A check that something arrived waits 10 seconds, or `within {duration}`. A count higher than the check's fails at once, as waiting cannot lower it.

A check that something did not arrive looks once, at what has arrived. Put it after a check that waits, so that it has something to look at. `the telemetry does not contain` fails if nothing has arrived at all.

A failure lists the spans, logs or metrics that did arrive.

## Use it from your own steps

The pack's public context gives your own steps what it read:

```go
st := otel.Context(sc)
if err := st.Refresh(); err != nil {
	return err
}
for _, span := range st.Spans() {
	// span.Name, span.Duration(), span.Attributes...
}
```
