package admin

import (
	"context"
	"fmt"
	"net/http"

	"github.com/btopcu/argus/internal/alertstate"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

// tenantUsageQuotaMetric is the unified QuotaMetric shape per the FIX-246 Wire
// Contract. status enum uses 'critical' (aligned with FIX-211) — NOT 'danger'.
type tenantUsageQuotaMetric struct {
	Current int     `json:"current"`
	Max     int     `json:"max"`
	Pct     float64 `json:"pct"`
	Status  string  `json:"status"` // "ok" | "warning" | "critical"
}

// tenantUsageStorageMetric mirrors tenantUsageQuotaMetric but uses int64 fields
// for current+max because storage_bytes maps to the BIGINT column
// `tenants.max_storage_bytes` (10TB+ values exceed int32). FE TS `number` is
// double-precision and safe to ~9 PB. FIX-246 Gate F-A9.
type tenantUsageStorageMetric struct {
	Current int64   `json:"current"`
	Max     int64   `json:"max"`
	Pct     float64 `json:"pct"`
	Status  string  `json:"status"` // "ok" | "warning" | "critical"
}

// tenantUsageItem is the wire shape for GET /api/v1/admin/tenants/usage.
type tenantUsageItem struct {
	TenantID        uuid.UUID                `json:"tenant_id"`
	TenantName      string                   `json:"tenant_name"`
	Plan            string                   `json:"plan"`
	State           string                   `json:"state"`
	SIMs            tenantUsageQuotaMetric   `json:"sims"`
	Sessions        tenantUsageQuotaMetric   `json:"sessions"`
	APIRPS          tenantUsageQuotaMetric   `json:"api_rps"`
	StorageBytes    tenantUsageStorageMetric `json:"storage_bytes"`
	UserCount       int                      `json:"user_count"`
	CDRBytes30d     int64                    `json:"cdr_bytes_30d"`
	OpenBreachCount int                      `json:"open_breach_count"`
}

// usageQuotaStatus maps a utilisation percentage to the FIX-211 severity
// enum. Note: "critical" instead of "danger" — aligns with FIX-211 taxonomy.
func usageQuotaStatus(pct float64) string {
	if pct >= 95 {
		return "critical"
	}
	if pct >= 80 {
		return "warning"
	}
	return "ok"
}

// calcUsagePct returns utilisation as a percentage clamped to [0, 100].
func calcUsagePct(current, max int) float64 {
	if max == 0 {
		return 0
	}
	pct := float64(current) / float64(max) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

// calcUsagePct64 is the int64 variant used for storage_bytes.
func calcUsagePct64(current int64, max int64) float64 {
	if max == 0 {
		return 0
	}
	pct := float64(current) / float64(max) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

// ListTenantUsage GET /api/v1/admin/tenants/usage (super_admin)
//
// Returns unified per-tenant quota + resource usage in a single array.
// Query budget per request:
//  1. tenantStore.List           — fetch all tenants (1 query)
//  2. tenantStore.GetStats       — per-tenant stats (N queries, bounded to <100 tenants)
//  3. breach count batch query   — single GROUP BY across all tenants (1 query)
//  4. cdrBytes30d helper         — per-tenant CDR sum (N queries, reused from ListTenantResources)
//  5. estimateTenantAPIRPS       — per-tenant audit_logs count (N queries, reused from ListTenantResources)
//
// The per-tenant N pattern is pre-existing in ListTenantResources / ListTenantQuotas
// and is acceptable for the bounded tenant population (<100). The breach count
// (step 3) is always a single batch query — never N+1.
func (h *Handler) ListTenantUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenants, _, err := h.tenantStore.List(ctx, "", 100, "")
	if err != nil {
		h.logger.Error().Err(err).Msg("list tenants for usage")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	// Batch-fetch open quota.breach counts per tenant (single query, no N+1).
	breachCounts, err := h.batchQuotaBreachCounts(ctx)
	if err != nil {
		h.logger.Warn().Err(err).Msg("batch quota breach counts — defaulting to 0")
		breachCounts = map[uuid.UUID]int{}
	}

	items := make([]tenantUsageItem, 0, len(tenants))
	for _, t := range tenants {
		stats, statsErr := h.tenantStore.GetStats(ctx, t.ID)
		if statsErr != nil || stats == nil {
			h.logger.Warn().Err(statsErr).Str("tenant_id", t.ID.String()).Msg("get tenant stats for usage")
			stats = &store.TenantStats{}
		}

		// Use new plan-aware max_* columns (added by Wave 1 migration).
		// Fall back to sensible M2M defaults if zero (e.g. tenant predates migration).
		maxSessions := t.MaxSessions
		if maxSessions == 0 {
			maxSessions = 2000
		}
		maxAPIRPS := t.MaxAPIRPS
		if maxAPIRPS == 0 {
			maxAPIRPS = 2000
		}
		maxStorageBytes := t.MaxStorageBytes
		if maxStorageBytes == 0 {
			maxStorageBytes = 107374182400 // 100 GB
		}

		currentAPIRPS := h.estimateTenantAPIRPS(ctx, t.ID)
		currentAPIRPSInt := int(currentAPIRPS)
		cdrBytes := h.cdrBytes30d(ctx, t.ID)

		simPct := calcUsagePct(stats.SimCount, t.MaxSims)
		sessPct := calcUsagePct(stats.ActiveSessions, maxSessions)
		apiPct := calcUsagePct(currentAPIRPSInt, maxAPIRPS)
		storagePct := calcUsagePct64(int64(stats.StorageBytes), maxStorageBytes)

		plan := t.Plan
		if plan == "" {
			plan = "standard"
		}

		items = append(items, tenantUsageItem{
			TenantID:   t.ID,
			TenantName: t.Name,
			Plan:       plan,
			State:      t.State,
			SIMs: tenantUsageQuotaMetric{
				Current: stats.SimCount,
				Max:     t.MaxSims,
				Pct:     simPct,
				Status:  usageQuotaStatus(simPct),
			},
			Sessions: tenantUsageQuotaMetric{
				Current: stats.ActiveSessions,
				Max:     maxSessions,
				Pct:     sessPct,
				Status:  usageQuotaStatus(sessPct),
			},
			APIRPS: tenantUsageQuotaMetric{
				Current: currentAPIRPSInt,
				Max:     maxAPIRPS,
				Pct:     apiPct,
				Status:  usageQuotaStatus(apiPct),
			},
			StorageBytes: tenantUsageStorageMetric{
				Current: int64(stats.StorageBytes),
				Max:     maxStorageBytes,
				Pct:     storagePct,
				Status:  usageQuotaStatus(storagePct),
			},
			UserCount:       stats.UserCount,
			CDRBytes30d:     cdrBytes,
			OpenBreachCount: breachCounts[t.ID],
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, items)
}

// batchQuotaBreachCounts returns a map of tenant_id → count of open quota.breach
// alerts (state IN ('open','acknowledged','suppressed')) using a single GROUP BY
// query — never N+1 regardless of tenant count.
func (h *Handler) batchQuotaBreachCounts(ctx context.Context) (map[uuid.UUID]int, error) {
	if h.db == nil {
		return map[uuid.UUID]int{}, nil
	}

	rows, err := h.db.Query(ctx, `
		SELECT tenant_id, COUNT(*)::int
		FROM alerts
		WHERE type = 'quota.breach'
		  AND state = ANY($1)
		GROUP BY tenant_id
	`, alertstate.ActiveStates)
	if err != nil {
		return nil, fmt.Errorf("admin: batch quota breach counts: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]int)
	for rows.Next() {
		var tenantID uuid.UUID
		var count int
		if scanErr := rows.Scan(&tenantID, &count); scanErr != nil {
			return nil, fmt.Errorf("admin: scan breach count: %w", scanErr)
		}
		result[tenantID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: breach count rows: %w", err)
	}
	return result, nil
}
