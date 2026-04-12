package metrics

import (
	"context"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

// AAARecorder is the contract implemented by any sink that records AAA
// authentication events. It intentionally matches the MetricsRecorder
// interface declared in internal/aaa/radius/server.go so that any
// implementation in this package drops in as a replacement.
//
// NOTE: latencyMs is int (not int64) to match the existing aaa/radius
// contract. Do not change without updating that interface.
type AAARecorder interface {
	RecordAuth(ctx context.Context, operatorID uuid.UUID, success bool, latencyMs int)
}

// tenantFromContext extracts the tenant id from the request context
// using the shared key declared in internal/apierr. Falls back to
// "unknown" when the key is absent or holds a nil UUID.
//
// Using internal/apierr (stdlib-only) avoids an import cycle with
// internal/gateway, which itself imports internal/observability/metrics.
func tenantFromContext(ctx context.Context) string {
	if ctx == nil {
		return "unknown"
	}
	if tid, ok := ctx.Value(apierr.TenantIDKey).(uuid.UUID); ok && tid != uuid.Nil {
		return tid.String()
	}
	return "unknown"
}

// PromAAARecorder is a Prometheus-backed implementation of AAARecorder.
// It increments argus_aaa_auth_requests_total and observes
// argus_aaa_auth_latency_seconds for each call.
type PromAAARecorder struct {
	reg      *Registry
	protocol string
}

// NewPromAAARecorder constructs a recorder bound to the supplied Registry
// and labelled with the given protocol (e.g. "radius", "diameter", "5g").
func NewPromAAARecorder(reg *Registry, protocol string) *PromAAARecorder {
	return &PromAAARecorder{reg: reg, protocol: protocol}
}

// RecordAuth implements AAARecorder. Safe for concurrent use and a no-op
// when the registry is nil (keeps tests and partially initialised
// servers from panicking).
func (r *PromAAARecorder) RecordAuth(ctx context.Context, operatorID uuid.UUID, success bool, latencyMs int) {
	if r == nil || r.reg == nil {
		return
	}

	result := "failure"
	if success {
		result = "success"
	}

	opID := operatorID.String()
	tenant := tenantFromContext(ctx)

	r.reg.AAAAuthRequestsTotal.WithLabelValues(r.protocol, opID, result, tenant).Inc()
	r.reg.AAAAuthLatency.WithLabelValues(r.protocol, opID, tenant).Observe(float64(latencyMs) / 1000.0)
}

// CompositeMetricsRecorder fans every RecordAuth call out to the wrapped
// recorders in the order they were supplied. A single slow or failing
// recorder will delay the others — recorders are expected to be cheap
// and non-blocking (the Redis Collector uses a pipeline, the Prom
// recorder is in-memory).
type CompositeMetricsRecorder struct {
	recorders []AAARecorder
}

// NewCompositeRecorder builds a composite fan-out over the provided
// recorders. nil entries are dropped so callers can pass optional
// recorders without guard checks.
func NewCompositeRecorder(recorders ...AAARecorder) *CompositeMetricsRecorder {
	filtered := make([]AAARecorder, 0, len(recorders))
	for _, r := range recorders {
		if r == nil {
			continue
		}
		filtered = append(filtered, r)
	}
	return &CompositeMetricsRecorder{recorders: filtered}
}

// RecordAuth fans out to every wrapped recorder.
func (c *CompositeMetricsRecorder) RecordAuth(ctx context.Context, operatorID uuid.UUID, success bool, latencyMs int) {
	if c == nil {
		return
	}
	for _, r := range c.recorders {
		r.RecordAuth(ctx, operatorID, success, latencyMs)
	}
}

// Len exposes the number of child recorders for introspection/tests.
func (c *CompositeMetricsRecorder) Len() int {
	if c == nil {
		return 0
	}
	return len(c.recorders)
}
