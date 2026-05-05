package system

import (
	"net/http"

	"github.com/btopcu/argus/internal/analytics/aggregates"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

type CapacityConfig struct {
	SIMs          int
	Sessions      int
	AuthPerSec    int
	MonthlyGrowth int
}

type capacityIPPool struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	CIDR            string   `json:"cidr"`
	Total           int      `json:"total"`
	Used            int      `json:"used"`
	Available       int      `json:"available"`
	UtilizationPct  float64  `json:"utilization_pct"`
	AllocationRate  float64  `json:"allocation_rate"`
	ExhaustionHours *float64 `json:"exhaustion_hours"`
}

type capacityData struct {
	TotalSIMs         int              `json:"total_sims"`
	ActiveSessions    int64            `json:"active_sessions"`
	AuthPerSec        float64          `json:"auth_per_sec"`
	SIMCapacity       int              `json:"sim_capacity"`
	SessionCapacity   int              `json:"session_capacity"`
	AuthCapacity      int              `json:"auth_capacity"`
	MonthlyGrowthSIMs int              `json:"monthly_growth_sims"`
	IPPools           []capacityIPPool `json:"ip_pools"`
}

type CapacityHandler struct {
	cfg          CapacityConfig
	simStore     *store.SIMStore
	sessionStore *store.RadiusSessionStore
	ipPoolStore  *store.IPPoolStore
	cdrStore     *store.CDRStore
	agg          aggregates.Aggregates
}

func NewCapacityHandler(
	cfg CapacityConfig,
	simStore *store.SIMStore,
	sessionStore *store.RadiusSessionStore,
	ipPoolStore *store.IPPoolStore,
	cdrStore *store.CDRStore,
	agg aggregates.Aggregates,
) *CapacityHandler {
	return &CapacityHandler{
		cfg:          cfg,
		simStore:     simStore,
		sessionStore: sessionStore,
		ipPoolStore:  ipPoolStore,
		cdrStore:     cdrStore,
		agg:          agg,
	}
}

func (h *CapacityHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID, ok := ctx.Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	totalSIMs, err := h.agg.SIMCountByTenant(ctx, tenantID)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to count SIMs")
		return
	}

	activeSessions, err := h.sessionStore.CountActive(ctx)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to count sessions")
		return
	}

	sparklines, err := h.cdrStore.GetDailyKPISparklines(ctx, tenantID, 1)
	var authPerSec float64
	if err == nil && len(sparklines["total_sims"]) > 0 {
		authPerSec = float64(h.cfg.AuthPerSec)
	}

	pools, err := h.ipPoolStore.GetCapacitySummary(ctx, tenantID)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to get IP pool capacity")
		return
	}

	ipPools := make([]capacityIPPool, 0, len(pools))
	for _, p := range pools {
		ipPools = append(ipPools, capacityIPPool{
			ID:              p.ID.String(),
			Name:            p.Name,
			CIDR:            p.CIDR,
			Total:           p.Total,
			Used:            p.Used,
			Available:       p.Available,
			UtilizationPct:  p.UtilizationPct,
			AllocationRate:  p.AllocationRate,
			ExhaustionHours: p.ExhaustionHours,
		})
	}

	data := capacityData{
		TotalSIMs:         totalSIMs,
		ActiveSessions:    activeSessions,
		AuthPerSec:        authPerSec,
		SIMCapacity:       h.cfg.SIMs,
		SessionCapacity:   h.cfg.Sessions,
		AuthCapacity:      h.cfg.AuthPerSec,
		MonthlyGrowthSIMs: h.cfg.MonthlyGrowth,
		IPPools:           ipPools,
	}

	apierr.WriteSuccess(w, http.StatusOK, data)
}
