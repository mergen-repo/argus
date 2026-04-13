package admin

import (
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

// tenantCostItem mirrors FE CostTenant (web/src/types/admin.ts). The FE expects a
// flat cost breakdown (radius/operator/sms/storage) and a 6-month trend array.
// We surface both the legacy `cost_total` / `by_operator` fields (used by older
// clients) and the new flat fields. Extra keys are harmless for forward-compat.
type tenantCostItem struct {
	TenantID     uuid.UUID   `json:"tenant_id"`
	TenantName   string      `json:"tenant_name"`
	Currency     string      `json:"currency"`
	Total        float64     `json:"total"`
	RadiusCost   float64     `json:"radius_cost"`
	OperatorCost float64     `json:"operator_cost"`
	SMSCost      float64     `json:"sms_cost"`
	StorageCost  float64     `json:"storage_cost"`
	Trend        []float64   `json:"trend"`
	ByOperator   interface{} `json:"by_operator"`
}

func periodDates(period string) (time.Time, time.Time) {
	now := time.Now().UTC()
	switch period {
	case "day":
		return now.AddDate(0, 0, -1), now
	case "week":
		return now.AddDate(0, 0, -7), now
	case "quarter":
		return now.AddDate(0, -3, 0), now
	default: // month
		return now.AddDate(0, -1, 0), now
	}
}

// ListCostByTenant GET /api/v1/admin/cost/by-tenant (super_admin)
func (h *Handler) ListCostByTenant(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	from, to := periodDates(period)

	tenants, _, err := h.tenantStore.List(r.Context(), "", 100, "")
	if err != nil {
		h.logger.Error().Err(err).Msg("list tenants for cost")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	costStore := store.NewCostAnalyticsStore(h.db)
	items := make([]tenantCostItem, 0, len(tenants))

	for _, t := range tenants {
		totals, err := costStore.GetCostTotals(r.Context(), store.CostQueryParams{
			TenantID: t.ID,
			From:     from,
			To:       to,
		})
		if err != nil || totals == nil {
			totals = &store.CostTotals{}
		}

		byOp, _ := costStore.GetCostByOperator(r.Context(), store.CostQueryParams{
			TenantID: t.ID,
			From:     from,
			To:       to,
		})

		// 6-month trend (monthly buckets).
		trend := make([]float64, 6)
		for i := 0; i < 6; i++ {
			mFrom := time.Now().UTC().AddDate(0, -(5 - i), 0).Truncate(24 * time.Hour)
			mTo := mFrom.AddDate(0, 1, 0)
			mTotals, err := costStore.GetCostTotals(r.Context(), store.CostQueryParams{
				TenantID: t.ID,
				From:     mFrom,
				To:       mTo,
			})
			if err == nil && mTotals != nil {
				trend[i] = mTotals.TotalUsageCost
			}
		}

		items = append(items, tenantCostItem{
			TenantID:     t.ID,
			TenantName:   t.Name,
			Currency:     "TRY",
			Total:        totals.TotalUsageCost,
			OperatorCost: totals.TotalUsageCost,
			RadiusCost:   0,
			SMSCost:      0,
			StorageCost:  0,
			Trend:        trend,
			ByOperator:   byOp,
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, items)
}
