package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// InitMeter installs a global MeterProvider backed by a Prometheus exporter.
// Metrics are instrumented with OpenTelemetry's vendor-neutral Meter API and
// exposed in Prometheus exposition format at /metrics. It returns a shutdown func.
func InitMeter(serviceName string) (func(context.Context) error, error) {
	exporter, err := otelprom.New()
	if err != nil {
		return nil, fmt.Errorf("creating prometheus exporter: %w", err)
	}
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
	)
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(provider)
	return provider.Shutdown, nil
}