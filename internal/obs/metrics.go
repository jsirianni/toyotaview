package obs

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Metrics struct {
	httpRequests     metric.Int64Counter
	httpDuration     metric.Float64Histogram
	httpErrors       metric.Int64Counter
	smartcarRequests metric.Int64Counter
	smartcarDuration metric.Float64Histogram
	smartcarErrors   metric.Int64Counter
	refreshTotal     metric.Int64Counter
	refreshDuration  metric.Float64Histogram
	tokenRequests    metric.Int64Counter
	cachedVehicles   metric.Int64ObservableGauge
	cachedCount      *atomic.Int64
}

func newMetrics(meter metric.Meter) (Metrics, error) {
	httpRequests, err := meter.Int64Counter("sc4r_http_server_requests_total")
	if err != nil {
		return Metrics{}, fmt.Errorf("create http requests counter: %w", err)
	}

	httpDuration, err := meter.Float64Histogram("sc4r_http_server_duration_ms")
	if err != nil {
		return Metrics{}, fmt.Errorf("create http duration histogram: %w", err)
	}

	httpErrors, err := meter.Int64Counter("sc4r_http_server_errors_total")
	if err != nil {
		return Metrics{}, fmt.Errorf("create http errors counter: %w", err)
	}

	smartcarRequests, err := meter.Int64Counter("sc4r_smartcar_requests_total")
	if err != nil {
		return Metrics{}, fmt.Errorf("create smartcar requests counter: %w", err)
	}

	smartcarDuration, err := meter.Float64Histogram("sc4r_smartcar_request_duration_ms")
	if err != nil {
		return Metrics{}, fmt.Errorf("create smartcar duration histogram: %w", err)
	}

	smartcarErrors, err := meter.Int64Counter("sc4r_smartcar_errors_total")
	if err != nil {
		return Metrics{}, fmt.Errorf("create smartcar errors counter: %w", err)
	}

	refreshTotal, err := meter.Int64Counter("sc4r_refresh_total")
	if err != nil {
		return Metrics{}, fmt.Errorf("create refresh counter: %w", err)
	}

	refreshDuration, err := meter.Float64Histogram("sc4r_refresh_duration_ms")
	if err != nil {
		return Metrics{}, fmt.Errorf("create refresh histogram: %w", err)
	}

	tokenRequests, err := meter.Int64Counter("sc4r_token_requests_total")
	if err != nil {
		return Metrics{}, fmt.Errorf("create token counter: %w", err)
	}

	cachedVehicles, err := meter.Int64ObservableGauge("sc4r_cached_vehicles")
	if err != nil {
		return Metrics{}, fmt.Errorf("create cached vehicles gauge: %w", err)
	}

	metrics := Metrics{
		httpRequests:     httpRequests,
		httpDuration:     httpDuration,
		httpErrors:       httpErrors,
		smartcarRequests: smartcarRequests,
		smartcarDuration: smartcarDuration,
		smartcarErrors:   smartcarErrors,
		refreshTotal:     refreshTotal,
		refreshDuration:  refreshDuration,
		tokenRequests:    tokenRequests,
		cachedVehicles:   cachedVehicles,
		cachedCount:      &atomic.Int64{},
	}

	if _, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observer.ObserveInt64(metrics.cachedVehicles, metrics.cachedCount.Load())
		return nil
	}, cachedVehicles); err != nil {
		return Metrics{}, fmt.Errorf("register cached vehicles callback: %w", err)
	}

	return metrics, nil
}

func (m *Metrics) SetCachedVehicles(count int) {
	m.cachedCount.Store(int64(count))
}

func (m *Metrics) RecordHTTP(
	ctx context.Context,
	method string,
	route string,
	status int,
	duration time.Duration,
) {
	attrs := metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("route", route),
		attribute.String("status", fmt.Sprintf("%d", status)),
	)
	m.httpRequests.Add(ctx, 1, attrs)
	m.httpDuration.Record(ctx, duration.Seconds()*1000, attrs)
	if status >= 500 {
		m.httpErrors.Add(ctx, 1, attrs)
	}
}

func (m *Metrics) RecordSmartcar(
	ctx context.Context,
	operation string,
	status string,
	duration time.Duration,
	errorType string,
) {
	attrs := metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.String("status", status),
	)
	m.smartcarRequests.Add(ctx, 1, attrs)
	m.smartcarDuration.Record(ctx, duration.Seconds()*1000, attrs)
	if errorType != "" {
		m.smartcarErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("status", status),
			attribute.String("error_type", errorType),
		))
	}
}

func (m *Metrics) RecordRefresh(
	ctx context.Context,
	scope string,
	result string,
	duration time.Duration,
) {
	attrs := metric.WithAttributes(
		attribute.String("scope", scope),
		attribute.String("result", result),
	)
	m.refreshTotal.Add(ctx, 1, attrs)
	m.refreshDuration.Record(ctx, duration.Seconds()*1000, attrs)
}

func (m *Metrics) RecordToken(ctx context.Context, result string) {
	m.tokenRequests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}
