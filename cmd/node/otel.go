package main

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

type silentErrorHandler struct{}

func (s silentErrorHandler) Handle(err error) {
	// Silently ignore OpenTelemetry export timeouts to prevent log spam
}

func initMetrics(ctx context.Context, agentID string) (*metric.MeterProvider, error) {
	otel.SetErrorHandler(silentErrorHandler{})

	// The OTLP exporter will "push" metrics to our collector (Prometheus backend)
	// without requiring an inbound port.
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint("monitoring.synthos-mesh.net:4317"),
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
