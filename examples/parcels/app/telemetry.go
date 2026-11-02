package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// The service reports what it does through OpenTelemetry, to wherever the SDK's own
// settings say (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME, ...): in compose, a
// Collector. With no endpoint set, it reports nothing.

const instrumentation = "parcels"

func tracer() trace.Tracer { return otel.Tracer(instrumentation) }

// startTelemetry sets up the SDK, and returns what stops it and what logs through it.
// It sets nothing up with no endpoint: what the code calls then does nothing.
func startTelemetry(ctx context.Context) (func(context.Context) error, slog.Handler, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil, nil
	}
	otel.SetTextMapPropagator(propagation.TraceContext{})
	spans, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	traces := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spans))
	otel.SetTracerProvider(traces)
	exported, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	metrics := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exported)))
	otel.SetMeterProvider(metrics)
	records, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, nil, err
	}
	logs := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(records)))
	otel.SetLoggerProvider(logs)
	stop := func(ctx context.Context) error {
		return errors.Join(traces.Shutdown(ctx), metrics.Shutdown(ctx), logs.Shutdown(ctx))
	}
	return stop, otelslog.NewHandler(instrumentation), nil
}

// traced gives each request a span of its own, in the trace its traceparent header
// names, called by its route.
func traced(next http.Handler) http.Handler {
	named := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Pattern == "" {
			return
		}
		span := trace.SpanFromContext(r.Context())
		span.SetName(r.Pattern)
		if _, route, ok := strings.Cut(r.Pattern, " "); ok {
			span.SetAttributes(attribute.String("http.route", route))
		}
	})
	return otelhttp.NewHandler(named, "request", otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/health" }))
}

// inSpan runs part of a request's work in a span of its own.
func inSpan(ctx context.Context, name string, work func(context.Context) error) error {
	ctx, span := tracer().Start(ctx, name)
	defer span.End()
	err := work(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// countRegistered reports a registered parcel, by the shop that sent it.
func countRegistered(ctx context.Context, p *Parcel) {
	meter := otel.Meter(instrumentation)
	shop := metric.WithAttributes(attribute.String("sender", p.Sender))
	if registered, err := meter.Int64Counter("parcels_registered"); err == nil {
		registered.Add(ctx, 1, shop)
	}
	if weights, err := meter.Int64Histogram("parcel_weight", metric.WithUnit("g")); err == nil {
		weights.Record(ctx, int64(p.WeightGrams), shop)
	}
	if last, err := meter.Int64Gauge("last_parcel_weight", metric.WithUnit("g")); err == nil {
		last.Record(ctx, int64(p.WeightGrams), shop)
	}
}
