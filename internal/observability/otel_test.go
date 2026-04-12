package observability

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
)

func discardLogger() zerolog.Logger {
	return zerolog.Nop()
}

func TestInit_EmptyEndpoint(t *testing.T) {
	cfg := Config{
		Endpoint:    "",
		ServiceName: "test-svc",
	}

	shutdown, err := Init(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown func")
	}
	if otel.GetTracerProvider() == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

func TestInit_WithEndpoint_DoesNotPanic(t *testing.T) {
	cfg := Config{
		Endpoint:         "localhost:4317",
		ServiceName:      "test-svc",
		ServiceVersion:   "0.0.1",
		DeploymentEnv:    "test",
		SamplerRatio:     1.0,
		ExportTimeoutSec: 5,
	}

	ctx := context.Background()
	shutdown, err := Init(ctx, cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown func")
	}
	_ = shutdown(ctx)
}

func TestInit_TracerProduceSpans(t *testing.T) {
	cfg := Config{
		Endpoint:    "",
		ServiceName: "test-svc",
	}

	_, err := Init(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, span := otel.Tracer("test").Start(context.Background(), "test-span")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	span.End()
}

func TestInit_ShutdownIsCalled(t *testing.T) {
	cfg := Config{
		Endpoint:    "",
		ServiceName: "test-svc",
	}

	shutdown, err := Init(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown returned unexpected error: %v", err)
	}
}
