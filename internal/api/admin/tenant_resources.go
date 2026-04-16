package admin

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

// tenantResourceItem is the wire shape consumed by /admin/tenants/resources.
// Field names mirror the FE TypeScript contract (web/src/types/admin.ts).
type tenantResourceItem struct {
	TenantID       uuid.UUID `json:"tenant_id"`
	TenantName     string    `json:"tenant_name"`
	State          string    `json:"state"`
	SimCount       int       `json:"sim_count"`
	APIRPS         float64   `json:"api_rps"`
	ActiveSessions int       `json:"active_sessions"`
	CDRBytes30d    int64     `json:"cdr_bytes_30d"`
	StorageBytes   int64     `json:"storage_bytes"`
	Spark          []int     `json:"spark"`
}

// ListTenantResources GET /api/v1/admin/tenants/resources (super_admin)
//
// Implementation note: this endpoint loops tenants and calls GetStats per tenant
// (N queries for N tenants). The tenant count is bounded by product policy (typically
// <100 tenants even on large deployments) so the extra queries are acceptable.
// See docs/brainstorming/decisions.md → Performance Decisions for rationale.
func (h *Handler) ListTenantResources(w http.ResponseWriter, r *http.Request) {
	limit := 100

	tenants, _, err := h.tenantStore.List(r.Context(), "", limit, "")
	if err != nil {
		h.logger.Error().Err(err).Msg("list tenants for resources")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]tenantResourceItem, 0, len(tenants))
	for _, t := range tenants {
		stats, statsErr := h.tenantStore.GetStats(r.Context(), t.ID)
		if statsErr != nil {
			h.logger.Warn().Err(statsErr).Str("tenant_id", t.ID.String()).Msg("get tenant stats")
			stats = &store.TenantStats{}
		}

		spark := make([]int, 0, 7)
		if h.cdrStore != nil {
			if sparklines, sparkErr := h.cdrStore.GetDailyKPISparklines(r.Context(), t.ID, 7); sparkErr != nil {
				h.logger.Warn().Err(sparkErr).Str("tenant_id", t.ID.String()).Msg("get tenant sparkline")
			} else {
				for _, point := range sparklines["total_sims"] {
					spark = append(spark, int(math.Round(point)))
				}
			}
		}
		if len(spark) == 0 {
			spark = make([]int, 7)
		}

		items = append(items, tenantResourceItem{
			TenantID:       t.ID,
			TenantName:     t.Name,
			State:          t.State,
			SimCount:       stats.SimCount,
			APIRPS:         h.estimateTenantAPIRPS(r.Context(), t.ID),
			ActiveSessions: stats.ActiveSessions,
			CDRBytes30d:    h.cdrBytes30d(r.Context(), t.ID),
			StorageBytes:   int64(stats.StorageBytes),
			Spark:          spark,
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, items)
}

func (h *Handler) estimateTenantAPIRPS(ctx context.Context, tenantID uuid.UUID) float64 {
	if h.db == nil {
		return 0
	}
	var count float64
	err := h.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE tenant_id = $1
		  AND api_key_id IS NOT NULL
		  AND created_at >= NOW() - INTERVAL '5 minutes'
	`, tenantID).Scan(&count)
	if err != nil {
		return 0
	}
	return count / float64((5 * time.Minute).Seconds())
}

func (h *Handler) cdrBytes30d(ctx context.Context, tenantID uuid.UUID) int64 {
	if h.db == nil {
		return 0
	}
	var total int64
	err := h.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(bytes_in + bytes_out), 0)
		FROM cdrs
		WHERE tenant_id = $1 AND timestamp >= NOW() - INTERVAL '30 days'
	`, tenantID).Scan(&total)
	if err != nil {
		return 0
	}
	return total
}

// tenantQuotaItem is the wire shape for /admin/tenants/quotas. The FE uses four
// QuotaMetric cells (sims, api_rps, sessions, storage_bytes). We derive sessions
// from tenantStats.ActiveSessions and storage_bytes from an approximated budget
// (no real storage counter is tracked yet — stub returns zero so the bar hides).
type tenantQuotaItem struct {
	TenantID     uuid.UUID   `json:"tenant_id"`
	TenantName   string      `json:"tenant_name"`
	SIMs         quotaMetric `json:"sims"`
	APIRPS       quotaMetric `json:"api_rps"`
	Sessions     quotaMetric `json:"sessions"`
	StorageBytes quotaMetric `json:"storage_bytes"`
}

type quotaMetric struct {
	Current int     `json:"current"`
	Max     int     `json:"max"`
	Pct     float64 `json:"pct"`
	Status  string  `json:"status"`
}

func quotaStatus(pct float64) string {
	if pct >= 95 {
		return "danger"
	}
	if pct >= 80 {
		return "warning"
	}
	return "ok"
}

func calcPct(current, max int) float64 {
	if max == 0 {
		return 0
	}
	return float64(current) / float64(max) * 100
}

// ListTenantQuotas GET /api/v1/admin/tenants/quotas (super_admin + tenant_admin)
func (h *Handler) ListTenantQuotas(w http.ResponseWriter, r *http.Request) {
	tenants, _, err := h.tenantStore.List(r.Context(), "", 100, "")
	if err != nil {
		h.logger.Error().Err(err).Msg("list tenants for quotas")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]tenantQuotaItem, 0, len(tenants))
	for _, t := range tenants {
		stats, err := h.tenantStore.GetStats(r.Context(), t.ID)
		if err != nil || stats == nil {
			stats = &store.TenantStats{}
		}

		// Soft API-rps ceiling: treat MaxAPIKeys * 100 as the bucket capacity
		// when the tenant config does not carry an explicit api-rps quota.
		apiRpsMax := t.MaxAPIKeys * 100
		if apiRpsMax == 0 {
			apiRpsMax = 1000
		}
		// Active sessions ceiling: MaxUsers (one session per user is a generous floor).
		sessionsMax := t.MaxUsers
		if sessionsMax == 0 {
			sessionsMax = 100
		}
		// Storage bytes ceiling: MaxSims * 4KB rough heuristic (subject/CDR trail).
		storageMax := t.MaxSims * 4096
		if storageMax == 0 {
			storageMax = 1 << 30
		}

		simPct := calcPct(stats.SimCount, t.MaxSims)
		apiPct := calcPct(0, apiRpsMax)
		sessPct := calcPct(stats.ActiveSessions, sessionsMax)
		storagePct := calcPct(stats.StorageBytes, storageMax)

		items = append(items, tenantQuotaItem{
			TenantID:     t.ID,
			TenantName:   t.Name,
			SIMs:         quotaMetric{Current: stats.SimCount, Max: t.MaxSims, Pct: simPct, Status: quotaStatus(simPct)},
			APIRPS:       quotaMetric{Current: 0, Max: apiRpsMax, Pct: apiPct, Status: quotaStatus(apiPct)},
			Sessions:     quotaMetric{Current: stats.ActiveSessions, Max: sessionsMax, Pct: sessPct, Status: quotaStatus(sessPct)},
			StorageBytes: quotaMetric{Current: stats.StorageBytes, Max: storageMax, Pct: storagePct, Status: quotaStatus(storagePct)},
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, items)
}
