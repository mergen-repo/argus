package observability

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config holds all configuration for the OTel SDK bootstrap.
type Config struct {
	Endpoint         string
	SamplerRatio     float64
	ServiceName      string
	ServiceVersion   string
	DeploymentEnv    string
	ExportTimeoutSec int
}

// Init sets up the global TracerProvider. Returns a shutdown function that
// flushes and stops the provider. Safe to call with empty Endpoint — uses a
// noop TracerProvider in that case.
func Init(ctx context.Context, cfg Config, log zerolog.Logger) (func(context.Context) error, error) {
	if cfg.Endpoint == "" {
		log.Info().Msg("observability: OTLP endpoint not configured, using noop TracerProvider")
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(_ context.Context) error { return nil }, nil
	}

	res := sdkresource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(cfg.ServiceName),
		semconv.ServiceVersionKey.String(cfg.ServiceVersion),
		semconv.DeploymentEnvironmentKey.String(cfg.DeploymentEnv),
	)

	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(cfg.ExportTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	bsp := sdktrace.NewBatchSpanProcessor(exp,
		sdktrace.WithBatchTimeout(timeout),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplerRatio))),
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	log.Info().
		Str("endpoint", cfg.Endpoint).
		Str("service", cfg.ServiceName).
		Str("version", cfg.ServiceVersion).
		Str("env", cfg.DeploymentEnv).
		Float64("sampler_ratio", cfg.SamplerRatio).
		Msg("observability: OTel TracerProvider initialized")

	return func(shutdownCtx context.Context) error {
		return tp.Shutdown(shutdownCtx)
	}, nil
}
