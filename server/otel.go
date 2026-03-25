package server

import (
	"context"
	"errors"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log"

	cosmosevmserverconfig "github.com/cosmos/evm/server/config"
	"github.com/cosmos/evm/version"
)

// InitOTel initializes both the TracerProvider and MeterProvider with OTLP gRPC exporters.
// Returns a shutdown function that must be called on application exit.
func InitOTel(ctx context.Context, cfg cosmosevmserverconfig.OTelConfig, logger log.Logger) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }

	logger.Info("InitOTel called", "enable", cfg.Enable, "endpoint", cfg.Endpoint, "insecure", cfg.Insecure)
	if !cfg.Enable {
		logger.Info("OpenTelemetry is DISABLED, skipping initialization")
		return noop, nil
	}

	hostname, _ := os.Hostname()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("og-evm"),
			semconv.ServiceVersionKey.String(version.AppVersion),
			semconv.ServiceInstanceIDKey.String(hostname),
			attribute.String("chain_id", cfg.ChainID),
		),
	)
	if err != nil {
		return noop, err
	}

	traceOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		traceOpts = append(traceOpts, otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()))
	}

	traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
	if err != nil {
		return noop, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRate))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	metricOpts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		metricOpts = append(metricOpts, otlpmetricgrpc.WithTLSCredentials(insecure.NewCredentials()))
	}

	metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		logger.Error("failed to create OTel metric exporter", "error", err.Error())
		return tp.Shutdown, nil
	}

	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter)),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	logger.Info("OpenTelemetry tracing and metrics enabled",
		"endpoint", cfg.Endpoint,
		"insecure", cfg.Insecure,
		"sample_rate", cfg.SampleRate,
		"chain_id", cfg.ChainID,
	)

	shutdown := func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}

	return shutdown, nil
}

// initOTelWithCleanup initializes OTel and returns a cleanup function safe to defer.
// Logs errors rather than returning them — the node should start even if OTel fails.
func initOTelWithCleanup(ctx context.Context, cfg cosmosevmserverconfig.OTelConfig, logger log.Logger) func() {
	shutdown, err := InitOTel(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to init OpenTelemetry", "error", err.Error())
	}
	return func() {
		if shutdown == nil {
			return
		}
		if err := shutdown(context.Background()); err != nil {
			logger.Error("failed to shutdown OpenTelemetry", "error", err.Error())
		}
	}
}
