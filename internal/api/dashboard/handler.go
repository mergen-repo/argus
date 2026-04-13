package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Handler struct {
	simStore      *store.SIMStore
	sessionStore  *store.RadiusSessionStore
	operatorStore *store.OperatorStore
	anomalyStore  *store.AnomalyStore
	apnStore      *store.APNStore
	cdrStore      *store.CDRStore
	redisClient   *redis.Client
	logger        zerolog.Logger
}

type HandlerOption func(*Handler)

func WithRedisClient(rc *redis.Client) HandlerOption {
	return func(h *Handler) {
		h.redisClient = rc
	}
}

func WithCDRStore(cs *store.CDRStore) HandlerOption {
	return func(h *Handler) {
		h.cdrStore = cs
	}
}

func NewHandler(
	simStore *store.SIMStore,
	sessionStore *store.RadiusSessionStore,
	operatorStore *store.OperatorStore,
	anomalyStore *store.AnomalyStore,
	apnStore *store.APNStore,
	logger zerolog.Logger,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		simStore:      simStore,
		sessionStore:  sessionStore,
		operatorStore: operatorStore,
		anomalyStore:  anomalyStore,
		apnStore:      apnStore,
		logger:        logger.With().Str("component", "dashboard_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
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

type trafficHeatmapCell struct {
	Day   int     `json:"day"`
	Hour  int     `json:"hour"`
	Value float64 `json:"value"`
}

type dashboardDTO struct {
	TotalSIMs      int                   `json:"total_sims"`
	ActiveSessions int64                 `json:"active_sessions"`
	AuthPerSec     float64               `json:"auth_per_sec"`
	MonthlyCost    float64               `json:"monthly_cost"`
	SIMByState     []simByStateDTO       `json:"sim_by_state"`
	OperatorHealth []operatorHealthDTO   `json:"operator_health"`
	TopAPNs        []topAPNDTO           `json:"top_apns"`
	RecentAlerts   []alertDTO            `json:"recent_alerts"`
	Sparklines     map[string][]float64  `json:"sparklines"`
	Deltas         map[string]float64    `json:"deltas"`
	TrafficHeatmap []trafficHeatmapCell  `json:"traffic_heatmap"`
}

func (h *Handler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	cacheKey := fmt.Sprintf("dashboard:%s", tenantID.String())
	if h.redisClient != nil {
		if cached, err := h.redisClient.Get(r.Context(), cacheKey).Bytes(); err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.Write(cached)
			return
		}
	}

	ctx := r.Context()
	resp := dashboardDTO{}

	var wg sync.WaitGroup
	var mu sync.Mutex

	wg.Add(6)

	go func() {
		defer wg.Done()
		totalSIMs, simStates, err := h.simStore.CountByState(ctx, tenantID)
		if err != nil {
			h.logger.Error().Err(err).Msg("count sims by state")
			return
		}
		mu.Lock()
		resp.TotalSIMs = totalSIMs
		resp.SIMByState = make([]simByStateDTO, len(simStates))
		for i, sc := range simStates {
			resp.SIMByState[i] = simByStateDTO{State: sc.State, Count: sc.Count}
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		if h.sessionStore == nil {
			return
		}
		stats, err := h.sessionStore.GetActiveStats(ctx, &tenantID)
		if err != nil {
			h.logger.Error().Err(err).Msg("get active sessions")
			return
		}
		topAPNs := make([]topAPNDTO, 0, 5)
		for apnID, count := range stats.ByAPN {
			name := apnID
			if apnID != "none" && h.apnStore != nil {
				if parsed, parseErr := uuid.Parse(apnID); parseErr == nil {
					if apn, apnErr := h.apnStore.GetByID(ctx, tenantID, parsed); apnErr == nil {
						if apn.DisplayName != nil && *apn.DisplayName != "" {
							name = *apn.DisplayName
						} else {
							name = apn.Name
						}
					}
				}
			}
			topAPNs = append(topAPNs, topAPNDTO{
				ID:    apnID,
				Name:  name,
				Count: count,
			})
		}
		sortTopAPNs(topAPNs)
		if len(topAPNs) > 5 {
			topAPNs = topAPNs[:5]
		}
		mu.Lock()
		resp.ActiveSessions = stats.TotalActive
		resp.TopAPNs = topAPNs
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		grants, err := h.operatorStore.ListGrantsWithOperators(ctx, tenantID)
		if err != nil {
			h.logger.Error().Err(err).Msg("list operator grants")
			return
		}
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
		mu.Lock()
		resp.OperatorHealth = health
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		anomalies, _, err := h.anomalyStore.ListByTenant(ctx, tenantID, store.ListAnomalyParams{
			Limit: 10,
		})
		if err != nil {
			h.logger.Error().Err(err).Msg("list recent anomalies")
			return
		}
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
		mu.Lock()
		resp.RecentAlerts = alerts
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		if h.cdrStore == nil {
			return
		}
		cost, err := h.cdrStore.GetMonthlyCostForTenant(ctx, tenantID)
		if err != nil {
			h.logger.Error().Err(err).Msg("get monthly cost")
		}
		sparklines, err := h.cdrStore.GetDailyKPISparklines(ctx, tenantID, 7)
		if err != nil {
			h.logger.Error().Err(err).Msg("get daily kpi sparklines")
			sparklines = map[string][]float64{}
		}

		var totalSimsDelta, activeSessionsDelta, monthlyCostDelta float64
		if costSeries, ok := sparklines["monthly_cost"]; ok && len(costSeries) >= 2 {
			today := costSeries[len(costSeries)-1]
			yesterday := costSeries[len(costSeries)-2]
			if yesterday != 0 {
				monthlyCostDelta = (today - yesterday) / yesterday * 100
			}
		}
		if simSeries, ok := sparklines["total_sims"]; ok && len(simSeries) >= 2 {
			today := simSeries[len(simSeries)-1]
			yesterday := simSeries[len(simSeries)-2]
			if yesterday != 0 {
				totalSimsDelta = (today - yesterday) / yesterday * 100
			}
		}

		mu.Lock()
		resp.MonthlyCost = cost
		resp.Sparklines = sparklines
		resp.Deltas = map[string]float64{
			"total_sims_delta":       totalSimsDelta,
			"active_sessions_delta":  activeSessionsDelta,
			"monthly_cost_delta":     monthlyCostDelta,
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		if h.cdrStore == nil {
			return
		}
		matrix, err := h.cdrStore.GetTrafficHeatmap7x24(ctx, tenantID)
		if err != nil {
			h.logger.Error().Err(err).Msg("get traffic heatmap")
			return
		}
		cells := make([]trafficHeatmapCell, 0, 168)
		for day, hours := range matrix {
			for hour, val := range hours {
				cells = append(cells, trafficHeatmapCell{Day: day, Hour: hour, Value: val})
			}
		}
		mu.Lock()
		resp.TrafficHeatmap = cells
		mu.Unlock()
	}()

	wg.Wait()

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
	if resp.Sparklines == nil {
		resp.Sparklines = map[string][]float64{}
	}
	if resp.Deltas == nil {
		resp.Deltas = map[string]float64{}
	}
	if resp.TrafficHeatmap == nil {
		resp.TrafficHeatmap = []trafficHeatmapCell{}
	}

	if h.redisClient != nil {
		envelope := apierr.SuccessResponse{Status: "success", Data: resp}
		if respBytes, err := json.Marshal(envelope); err == nil {
			h.redisClient.Set(r.Context(), cacheKey, respBytes, 15*time.Second)
		}
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
