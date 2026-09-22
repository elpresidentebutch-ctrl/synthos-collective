package main

import (
	"context"
	"errors"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// errMetricsNotConfigured is returned by initMetrics when no collector
// endpoint is configured. It is not a real failure -- main.go already logs
// it at Printf/warning level and keeps running without metrics -- it just
// distinguishes "nothing configured" from "dial/exporter setup actually
// failed" for anyone reading the log line.
var errMetricsNotConfigured = errors.New("SYNTHOS_OTEL_ENDPOINT not set; metrics export disabled")

type silentErrorHandler struct{}

func (s silentErrorHandler) Handle(err error) {
	// Silently ignore OpenTelemetry export timeouts to prevent log spam
}

func initMetrics(ctx context.Context, agentID string) (*metric.MeterProvider, error) {
	otel.SetErrorHandler(silentErrorHandler{})

	// This used to hardcode "monitoring.synthos-mesh.net:4317" -- a
	// placeholder collector domain that was never registered and has never
	// resolved, so every export attempt failed silently forever (see
	// silentErrorHandler above) and this whole feature quietly did nothing.
	// Metrics export is now disabled unless a real collector is configured,
	// consistent with the rest of this codebase's convention of disabling a
	// feature outright rather than pointing it at a fake default endpoint.
	endpoint := os.Getenv("SYNTHOS_OTEL_ENDPOINT")
	if endpoint == "" {
		return nil, errMetricsNotConfigured
	}

	// The OTLP exporter will "push" metrics to our collector (Prometheus backend)
	// without requiring an inbound port.
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("synthos-agent"),
		semconv.ServiceInstanceIDKey.String(agentID),
	)

	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter, metric.WithInterval(15*time.Second))),
	)

	otel.SetMeterProvider(mp)
	return mp, nil
}
