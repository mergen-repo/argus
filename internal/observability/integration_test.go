//go:build integration

package observability_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/gateway"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestIntegration_RequestProducesTraceAndMetrics(t *testing.T) {
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	metricsReg := metrics.NewRegistry()
	tenantID := uuid.New()

	r := chi.NewRouter()
	r.Use(gateway.ZerologRequestLogger(zerolog.Nop()))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Use(gateway.PrometheusHTTPMetrics(metricsReg))
	r.Get("/test/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	var h http.Handler = r
	h = otelhttp.NewHandler(h, "argus.http")

	req := httptest.NewRequest(http.MethodGet, "/test/abc", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	metricsRec := httptest.NewRecorder()
	metricsReg.Handler().ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if metricsRec.Code != http.StatusOK {
		t.Fatalf("metrics handler returned %d", metricsRec.Code)
	}

	metricsBody, err := io.ReadAll(metricsRec.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	body := string(metricsBody)

	for _, want := range []string{
		"# HELP argus_http_requests_total",
		"go_goroutines",
		"process_resident_memory_bytes",
		`argus_http_requests_total{method="GET",route="/test/{id}",status="200"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q\nbody:\n%s", want, body)
		}
	}

	if err := tp.ForceFlush(context.Background()); err != nil {
		t.Fatalf("force flush: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	var found bool
	for _, s := range spans {
		for _, attr := range s.Attributes {
			if attr.Key == attribute.Key("http.route") && attr.Value.AsString() == "/test/{id}" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Errorf("no span found with http.route=/test/{id}; recorded spans:\n%+v", spans)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tp.Shutdown(shutdownCtx); err != nil {
		t.Errorf("tp.Shutdown: %v", err)
	}
}
