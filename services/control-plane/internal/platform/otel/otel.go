// Package otel initialises the global OpenTelemetry tracer provider with
// an OTLP HTTP exporter pointing at the local collector. Call Init at
// startup and defer the returned shutdown.
package otel

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init wires the global tracer provider + propagator. Returns a shutdown
// function. Safe to call with empty endpoint — emits to a no-op exporter
// (well, almost: it still creates the provider so spans/ids are generated;
// they just never reach the collector. That keeps the server healthy if
// the collector is down).
func Init(ctx context.Context, serviceName, endpoint string) (func(context.Context) error, error) {
	if serviceName == "" {
		serviceName = "control-plane"
	}
	if endpoint == "" {
		// No exporter — install a TracerProvider that drops spans but still
		// propagates context. Avoids panics in handlers using otel APIs.
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{}, propagation.Baggage{},
		))
		return tp.Shutdown, nil
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(stripScheme(endpoint)),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, _ := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
	))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithMaxExportBatchSize(64)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return func(c context.Context) error {
		ctx2, cancel := context.WithTimeout(c, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx2)
	}, nil
}

func stripScheme(s string) string {
	if strings.HasPrefix(s, "http://") {
		return s[len("http://"):]
	}
	if strings.HasPrefix(s, "https://") {
		return s[len("https://"):]
	}
	return s
}
