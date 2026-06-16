package obs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/firefoxx04/toyotaview/internal/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

type Observer struct {
	tracer   trace.Tracer
	metrics  Metrics
	shutdown func(context.Context) error
}

func New(ctx context.Context, cfg config.OTELConfig, logger *zap.Logger) (*Observer, error) {
	if !cfg.Enabled {
		meter := metricnoop.NewMeterProvider().Meter(cfg.ServiceName)
		metrics, err := newMetrics(meter)
		if err != nil {
			return nil, err
		}

		return &Observer{
			tracer:   tracenoop.NewTracerProvider().Tracer(cfg.ServiceName),
			metrics:  metrics,
			shutdown: func(context.Context) error { return nil },
		}, nil
	}

	tlsConfig, err := tlsConfigFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	traceOptions := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithTimeout(cfg.Timeout),
	}
	metricOptions := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(cfg.Endpoint),
		otlpmetrichttp.WithTimeout(cfg.Timeout),
	}

	if cfg.Insecure {
		traceOptions = append(traceOptions, otlptracehttp.WithInsecure())
		metricOptions = append(metricOptions, otlpmetrichttp.WithInsecure())
	} else if tlsConfig != nil {
		traceOptions = append(traceOptions, otlptracehttp.WithTLSClientConfig(tlsConfig))
		metricOptions = append(metricOptions, otlpmetrichttp.WithTLSClientConfig(tlsConfig))
	}

	traceExporter, err := otlptracehttp.New(ctx, traceOptions...)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}

	metricExporter, err := otlpmetrichttp.New(ctx, metricOptions...)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("deployment.environment", "local"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithBatcher(traceExporter),
	)

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second)),
		),
	)

	meter := meterProvider.Meter(cfg.ServiceName)
	metrics, err := newMetrics(meter)
	if err != nil {
		return nil, err
	}

	return &Observer{
		tracer:  tracerProvider.Tracer(cfg.ServiceName),
		metrics: metrics,
		shutdown: func(shutdownCtx context.Context) error {
			var errs []error
			if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
				errs = append(errs, err)
			}

			if err := meterProvider.Shutdown(shutdownCtx); err != nil {
				errs = append(errs, err)
			}

			if joined := errors.Join(errs...); joined != nil && logger != nil {
				logger.Warn("otel shutdown", zap.Error(joined))
			}

			return errors.Join(errs...)
		},
	}, nil
}

func (o *Observer) Tracer() trace.Tracer {
	if o == nil || o.tracer == nil {
		return tracenoop.NewTracerProvider().Tracer("noop")
	}

	return o.tracer
}

func (o *Observer) Meter() metric.Meter {
	if o == nil {
		return metricnoop.NewMeterProvider().Meter("noop")
	}

	return metricnoop.NewMeterProvider().Meter("unused")
}

func (o *Observer) Shutdown(ctx context.Context) error {
	if o == nil || o.shutdown == nil {
		return nil
	}

	return o.shutdown(ctx)
}

func (o *Observer) SetCachedVehicles(count int) {
	if o == nil {
		return
	}

	o.metrics.SetCachedVehicles(count)
}

func (o *Observer) RecordSmartcar(
	ctx context.Context,
	operation string,
	status string,
	duration time.Duration,
	err error,
) {
	if o == nil {
		return
	}

	o.metrics.RecordSmartcar(ctx, operation, status, duration, errorType(err))
}

func (o *Observer) RecordRefresh(
	ctx context.Context,
	scope string,
	result string,
	duration time.Duration,
) {
	if o == nil {
		return
	}

	o.metrics.RecordRefresh(ctx, scope, result, duration)
}

func (o *Observer) RecordToken(ctx context.Context, result string) {
	if o == nil {
		return
	}

	o.metrics.RecordToken(ctx, result)
}

func tlsConfigFromConfig(cfg config.OTELConfig) (*tls.Config, error) {
	if cfg.Insecure {
		return nil, nil
	}

	if (cfg.ClientCertFile == "") != (cfg.ClientKeyFile == "") {
		return nil, errors.New("otel client cert and key must both be configured")
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: cfg.ServerName,
	}

	if cfg.CAFile != "" {
		caPEM, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read otel ca file: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("append otel ca file: no certificates found")
		}

		tlsConfig.RootCAs = pool
	}

	if cfg.ClientCertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load otel client certificate: %w", err)
		}

		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

func errorType(err error) string {
	if err == nil {
		return ""
	}

	name := fmt.Sprintf("%T", err)
	return strings.TrimPrefix(name, "*")
}
