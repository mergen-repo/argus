package dashboard

import (
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	simStore      *store.SIMStore
	sessionStore  *store.RadiusSessionStore
	operatorStore *store.OperatorStore
	anomalyStore  *store.AnomalyStore
	apnStore      *store.APNStore
	logger        zerolog.Logger
}

func NewHandler(
	simStore *store.SIMStore,
	sessionStore *store.RadiusSessionStore,
	operatorStore *store.OperatorStore,
	anomalyStore *store.AnomalyStore,
	apnStore *store.APNStore,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		simStore:      simStore,
		sessionStore:  sessionStore,
		operatorStore: operatorStore,
		anomalyStore:  anomalyStore,
		apnStore:      apnStore,
		logger:        logger.With().Str("component", "dashboard_handler").Logger(),
	}
}

type simByStateDTO struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

type operatorHealthDTO struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	HealthPct    float64 `json:"health_pct"`
}

type topAPNDTO struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Count  int64  `json:"session_count"`
}

type alertDTO struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	State      string `json:"state"`
	Message    string `json:"message"`
	DetectedAt string `json:"detected_at"`
}

type dashboardDTO struct {
	TotalSIMs      int               `json:"total_sims"`
	ActiveSessions int64             `json:"active_sessions"`
	AuthPerSec     float64           `json:"auth_per_sec"`
	MonthlyCost    float64           `json:"monthly_cost"`
	SIMByState     []simByStateDTO   `json:"sim_by_state"`
	OperatorHealth []operatorHealthDTO `json:"operator_health"`
	TopAPNs        []topAPNDTO       `json:"top_apns"`
	RecentAlerts   []alertDTO        `json:"recent_alerts"`
}

func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	ctx := r.Context()
	resp := dashboardDTO{}

	totalSIMs, simStates, err := h.simStore.CountByState(ctx, tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("count sims by state")
	} else {
		resp.TotalSIMs = totalSIMs
		resp.SIMByState = make([]simByStateDTO, len(simStates))
		for i, sc := range simStates {
			resp.SIMByState[i] = simByStateDTO{State: sc.State, Count: sc.Count}
		}
	}

	if h.sessionStore != nil {
		stats, err := h.sessionStore.GetActiveStats(ctx, &tenantID)
		if err != nil {
			h.logger.Error().Err(err).Msg("get active sessions")
		} else {
			resp.ActiveSessions = stats.TotalActive

			topAPNs := make([]topAPNDTO, 0, 5)
			for apnID, count := range stats.ByAPN {
				topAPNs = append(topAPNs, topAPNDTO{
					ID:    apnID,
					Name:  apnID,
					Count: count,
				})
			}
			sortTopAPNs(topAPNs)
			if len(topAPNs) > 5 {
				topAPNs = topAPNs[:5]
			}
			resp.TopAPNs = topAPNs
		}
	}

	grants, err := h.operatorStore.ListGrantsWithOperators(ctx, tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list operator grants")
	} else {
		health := make([]operatorHealthDTO, 0, len(grants))
		for _, g := range grants {
			pct := healthStatusToPct(g.HealthStatus)
			health = append(health, operatorHealthDTO{
				ID:        g.OperatorGrant.OperatorID.String(),
				Name:      g.OperatorName,
				Status:    g.HealthStatus,
				HealthPct: pct,
			})
		}
		resp.OperatorHealth = health
	}

	anomalies, _, err := h.anomalyStore.ListByTenant(ctx, tenantID, store.ListAnomalyParams{
		Limit: 10,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("list recent anomalies")
	} else {
		alerts := make([]alertDTO, 0, len(anomalies))
		for _, a := range anomalies {
			msg := a.Type
			if a.Source != nil {
				msg = msg + ": " + *a.Source
			}
			alerts = append(alerts, alertDTO{
				ID:         a.ID.String(),
				Type:       a.Type,
				Severity:   a.Severity,
				State:      a.State,
				Message:    msg,
				DetectedAt: a.DetectedAt.Format(time.RFC3339),
			})
		}
		resp.RecentAlerts = alerts
	}

	if resp.SIMByState == nil {
		resp.SIMByState = []simByStateDTO{}
	}
	if resp.OperatorHealth == nil {
		resp.OperatorHealth = []operatorHealthDTO{}
	}
	if resp.TopAPNs == nil {
		resp.TopAPNs = []topAPNDTO{}
	}
	if resp.RecentAlerts == nil {
		resp.RecentAlerts = []alertDTO{}
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func healthStatusToPct(status string) float64 {
	switch status {
	case "healthy":
		return 99.9
	case "degraded":
		return 95.0
	case "down":
		return 0.0
	default:
		return 50.0
	}
}

func sortTopAPNs(apns []topAPNDTO) {
	for i := 1; i < len(apns); i++ {
		for j := i; j > 0 && apns[j].Count > apns[j-1].Count; j-- {
			apns[j], apns[j-1] = apns[j-1], apns[j]
		}
	}
}
