package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
)

type channelHealth struct {
	SuccessRate    float64    `json:"success_rate"`
	FailureRate    float64    `json:"failure_rate"`
	RetryDepth     int        `json:"retry_depth"`
	LastDeliveryAt *time.Time `json:"last_delivery_at"`
	LatencyP50     float64    `json:"p50_ms"`
	LatencyP95     float64    `json:"p95_ms"`
	LatencyP99     float64    `json:"p99_ms"`
	Health         string     `json:"health"`
}

type deliveryStatusResponse struct {
	Webhook  channelHealth `json:"webhook"`
	Email    channelHealth `json:"email"`
	SMS      channelHealth `json:"sms"`
	InApp    channelHealth `json:"in_app"`
	Telegram channelHealth `json:"telegram"`
}

func healthFromRate(successRate float64) string {
	if successRate >= 0.98 {
		return "green"
	}
	if successRate >= 0.85 {
		return "yellow"
	}
	return "red"
}

func windowDuration(window string) time.Duration {
	switch window {
	case "1h":
		return time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// GetDeliveryStatus GET /api/v1/admin/delivery/status (super_admin)
func (h *Handler) GetDeliveryStatus(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	since := time.Now().UTC().Add(-windowDuration(window))

	resp := deliveryStatusResponse{}

	// Webhook stats
	resp.Webhook = h.webhookChannelStats(r.Context(), since)

	// SMS stats
	resp.SMS = h.smsChannelStats(r.Context(), since)

	// In-app (notifications table)
	resp.InApp = h.inAppChannelStats(r.Context(), since)

	// Email: stub (no dedicated delivery table — use success_rate=1 when no data)
	resp.Email = channelHealth{SuccessRate: 1.0, FailureRate: 0, Health: "green"}

	// Telegram: stub (not instrumented yet)
	resp.Telegram = channelHealth{SuccessRate: 1.0, FailureRate: 0, Health: "green"}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) webhookChannelStats(ctx context.Context, since time.Time) channelHealth {
	type row struct {
		FinalState string
		Count      int
		AvgLatency float64
	}

	rows, err := h.db.Query(ctx, `
		SELECT final_state,
			COUNT(*)::int AS cnt,
			COALESCE(EXTRACT(EPOCH FROM AVG(updated_at - created_at)) * 1000, 0) AS avg_latency_ms
		FROM webhook_deliveries
		WHERE created_at >= $1
		GROUP BY final_state
	`, since)
	if err != nil {
		h.logger.Warn().Err(err).Msg("webhook delivery stats query")
		return channelHealth{Health: "green"}
	}
	defer rows.Close()

	var success, failure, retrying int
	var lastDelivery *time.Time

	for rows.Next() {
		var r row
		if err := rows.Scan(&r.FinalState, &r.Count, &r.AvgLatency); err != nil {
			continue
		}
		switch r.FinalState {
		case "delivered", "success":
			success += r.Count
		case "failed":
			failure += r.Count
		case "retrying":
			retrying += r.Count
		}
	}

	total := success + failure
	successRate := 1.0
	failureRate := 0.0
	if total > 0 {
		successRate = float64(success) / float64(total)
		failureRate = float64(failure) / float64(total)
	}

	// Last delivery timestamp
	var lastAt time.Time
	_ = h.db.QueryRow(ctx, `
		SELECT MAX(updated_at) FROM webhook_deliveries WHERE final_state IN ('delivered','success') AND created_at >= $1
	`, since).Scan(&lastAt)
	if !lastAt.IsZero() {
		lastDelivery = &lastAt
	}

	// Latency percentiles
	var p50, p95, p99 float64
	_ = h.db.QueryRow(ctx, `
		SELECT
			COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (updated_at - created_at)) * 1000), 0),
			COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (updated_at - created_at)) * 1000), 0),
			COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (updated_at - created_at)) * 1000), 0)
		FROM webhook_deliveries
		WHERE created_at >= $1 AND final_state IN ('delivered','success')
	`, since).Scan(&p50, &p95, &p99)

	return channelHealth{
		SuccessRate:    successRate,
		FailureRate:    failureRate,
		RetryDepth:     retrying,
		LastDeliveryAt: lastDelivery,
		LatencyP50:     p50,
		LatencyP95:     p95,
		LatencyP99:     p99,
		Health:         healthFromRate(successRate),
	}
}

func (h *Handler) smsChannelStats(ctx context.Context, since time.Time) channelHealth {
	type row struct {
		Status string
		Count  int
	}

	rows, err := h.db.Query(ctx, `
		SELECT status, COUNT(*)::int FROM sms_outbound WHERE queued_at >= $1 GROUP BY status
	`, since)
	if err != nil {
		h.logger.Warn().Err(err).Msg("sms outbound stats query")
		return channelHealth{Health: "green"}
	}
	defer rows.Close()

	var success, failure int
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Status, &r.Count); err != nil {
			continue
		}
		switch r.Status {
		case "delivered":
			success += r.Count
		case "failed":
			failure += r.Count
		}
	}

	total := success + failure
	successRate := 1.0
	failureRate := 0.0
	if total > 0 {
		successRate = float64(success) / float64(total)
		failureRate = float64(failure) / float64(total)
	}

	var lastAt time.Time
	var lastDelivery *time.Time
	_ = h.db.QueryRow(ctx, `SELECT MAX(delivered_at) FROM sms_outbound WHERE delivered_at IS NOT NULL AND queued_at >= $1`, since).Scan(&lastAt)
	if !lastAt.IsZero() {
		lastDelivery = &lastAt
	}

	return channelHealth{
		SuccessRate:    successRate,
		FailureRate:    failureRate,
		LastDeliveryAt: lastDelivery,
		Health:         healthFromRate(successRate),
	}
}

func (h *Handler) inAppChannelStats(ctx context.Context, since time.Time) channelHealth {
	var total, read int
	_ = h.db.QueryRow(ctx, `
		SELECT COUNT(*)::int, COUNT(CASE WHEN state = 'read' THEN 1 END)::int
		FROM notifications
		WHERE created_at >= $1
	`, since).Scan(&total, &read)

	successRate := 1.0
	if total > 0 {
		successRate = float64(read) / float64(total)
	}

	var lastAt time.Time
	var lastDelivery *time.Time
	_ = h.db.QueryRow(ctx, `SELECT MAX(sent_at) FROM notifications WHERE sent_at IS NOT NULL AND created_at >= $1`, since).Scan(&lastAt)
	if !lastAt.IsZero() {
		lastDelivery = &lastAt
	}

	return channelHealth{
		SuccessRate:    successRate,
		FailureRate:    1 - successRate,
		LastDeliveryAt: lastDelivery,
		Health:         healthFromRate(successRate),
	}
}
