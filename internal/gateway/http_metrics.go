package gateway

import (
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func PrometheusHTTPMetrics(reg *metrics.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reg == nil {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rc := &responseCapture{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rc, r)

			duration := time.Since(start)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}

			tenant := TenantLabel(r.Context())
			status := strconv.Itoa(rc.status)

			reg.HTTPRequestsTotal.WithLabelValues(r.Method, route, status, tenant).Inc()
			reg.HTTPRequestDuration.WithLabelValues(r.Method, route, tenant).Observe(duration.Seconds())

			if span := trace.SpanFromContext(r.Context()); span.IsRecording() {
				span.SetAttributes(
					attribute.String("http.route", route),
					attribute.String("tenant.id", tenant),
					attribute.Int("http.status_code", rc.status),
				)
				if uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID); ok && uid != uuid.Nil {
					span.SetAttributes(attribute.String("user.id", uid.String()))
				}
			}
		})
	}
}
