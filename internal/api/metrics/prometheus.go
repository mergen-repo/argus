package metrics

import (
	"fmt"
	"net/http"
	"strings"
)

func (h *Handler) Prometheus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	m, err := h.collector.GetMetrics(ctx)
	if err != nil {
		http.Error(w, "failed to collect metrics", http.StatusInternalServerError)
		return
	}

	var b strings.Builder

	b.WriteString("# HELP argus_auth_requests_per_second Authentication requests per second.\n")
	b.WriteString("# TYPE argus_auth_requests_per_second gauge\n")
	b.WriteString(fmt.Sprintf("argus_auth_requests_per_second %d\n", m.AuthPerSec))

	b.WriteString("# HELP argus_auth_error_rate Authentication error rate (0-1).\n")
	b.WriteString("# TYPE argus_auth_error_rate gauge\n")
	b.WriteString(fmt.Sprintf("argus_auth_error_rate %.4f\n", m.AuthErrorRate))

	b.WriteString("# HELP argus_latency_p50_ms Authentication latency p50 in milliseconds.\n")
	b.WriteString("# TYPE argus_latency_p50_ms gauge\n")
	b.WriteString(fmt.Sprintf("argus_latency_p50_ms %d\n", m.Latency.P50))

	b.WriteString("# HELP argus_latency_p95_ms Authentication latency p95 in milliseconds.\n")
	b.WriteString("# TYPE argus_latency_p95_ms gauge\n")
	b.WriteString(fmt.Sprintf("argus_latency_p95_ms %d\n", m.Latency.P95))

	b.WriteString("# HELP argus_latency_p99_ms Authentication latency p99 in milliseconds.\n")
	b.WriteString("# TYPE argus_latency_p99_ms gauge\n")
	b.WriteString(fmt.Sprintf("argus_latency_p99_ms %d\n", m.Latency.P99))

	b.WriteString("# HELP argus_active_sessions Number of active sessions.\n")
	b.WriteString("# TYPE argus_active_sessions gauge\n")
	b.WriteString(fmt.Sprintf("argus_active_sessions %d\n", m.ActiveSessions))

	for opID, opM := range m.ByOperator {
		b.WriteString(fmt.Sprintf("argus_auth_requests_per_second{operator_id=\"%s\"} %d\n", opID, opM.AuthPerSec))
		b.WriteString(fmt.Sprintf("argus_auth_error_rate{operator_id=\"%s\"} %.4f\n", opID, opM.AuthErrorRate))
		b.WriteString(fmt.Sprintf("argus_latency_p50_ms{operator_id=\"%s\"} %d\n", opID, opM.Latency.P50))
		b.WriteString(fmt.Sprintf("argus_latency_p95_ms{operator_id=\"%s\"} %d\n", opID, opM.Latency.P95))
		b.WriteString(fmt.Sprintf("argus_latency_p99_ms{operator_id=\"%s\"} %d\n", opID, opM.Latency.P99))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, b.String())
}
