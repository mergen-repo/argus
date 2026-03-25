package analytics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/analytics/cost"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	usageStore    *store.UsageAnalyticsStore
	simStore      *store.SIMStore
	operatorStore *store.OperatorStore
	apnStore      *store.APNStore
	ippoolStore   *store.IPPoolStore
	costService   *cost.Service
	logger        zerolog.Logger
}

func NewHandler(usageStore *store.UsageAnalyticsStore, logger zerolog.Logger) *Handler {
	return &Handler{
		usageStore: usageStore,
		logger:     logger.With().Str("component", "analytics_handler").Logger(),
	}
}

func (h *Handler) WithStores(simStore *store.SIMStore, operatorStore *store.OperatorStore, apnStore *store.APNStore, ippoolStore *store.IPPoolStore) *Handler {
	h.simStore = simStore
	h.operatorStore = operatorStore
	h.apnStore = apnStore
	h.ippoolStore = ippoolStore
	return h
}

func (h *Handler) SetCostService(svc *cost.Service) {
	h.costService = svc
}

type timeSeriesDTO struct {
	Timestamp  string `json:"ts"`
	TotalBytes int64  `json:"total_bytes"`
	Sessions   int64  `json:"sessions"`
	Auths      int64  `json:"auths"`
	UniqueSims int64  `json:"unique_sims"`
	GroupKey   string `json:"group_key,omitempty"`
}

type totalsDTO struct {
	TotalBytes    int64 `json:"total_bytes"`
	TotalSessions int64 `json:"total_sessions"`
	TotalAuths    int64 `json:"total_auths"`
	UniqueSims    int64 `json:"unique_sims"`
}

type breakdownDTO struct {
	Key        string  `json:"key"`
	TotalBytes int64   `json:"total_bytes"`
	Sessions   int64   `json:"sessions"`
	Auths      int64   `json:"auths"`
	Percentage float64 `json:"percentage"`
}

type topConsumerDTO struct {
	SimID        string `json:"sim_id"`
	ICCID        string `json:"iccid,omitempty"`
	OperatorName string `json:"operator_name,omitempty"`
	APNName      string `json:"apn_name,omitempty"`
	IPAddress    string `json:"ip_address,omitempty"`
	TotalBytes   int64  `json:"total_bytes"`
	Sessions     int64  `json:"sessions"`
}

type comparisonDTO struct {
	PreviousTotals totalsDTO `json:"previous_totals"`
	BytesDelta     float64   `json:"bytes_delta_pct"`
	SessionsDelta  float64   `json:"sessions_delta_pct"`
	AuthsDelta     float64   `json:"auths_delta_pct"`
	SimsDelta      float64   `json:"sims_delta_pct"`
}

type usageResponseDTO struct {
	Period       string           `json:"period"`
	From         string           `json:"from"`
	To           string           `json:"to"`
	BucketSize   string           `json:"bucket_size"`
	TimeSeries   []timeSeriesDTO  `json:"time_series"`
	Totals       totalsDTO        `json:"totals"`
	Breakdowns   map[string][]breakdownDTO `json:"breakdowns"`
	TopConsumers []topConsumerDTO `json:"top_consumers"`
	Comparison   *comparisonDTO   `json:"comparison,omitempty"`
}

var validPeriods = map[string]bool{
	"1h": true, "24h": true, "7d": true, "30d": true, "custom": true,
}

var validGroupBy = map[string]bool{
	"operator": true, "operator_id": true,
	"apn": true, "apn_id": true,
	"rat_type": true,
}

func (h *Handler) GetUsage(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()

	period := q.Get("period")
	if period == "" {
		period = "24h"
	}
	if !validPeriods[period] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			fmt.Sprintf("Invalid period %q, supported: 1h, 24h, 7d, 30d, custom", period))
		return
	}

	var from, to time.Time
	if period == "custom" {
		fromStr := q.Get("from")
		toStr := q.Get("to")
		if fromStr == "" || toStr == "" {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				"'from' and 'to' are required for custom period")
			return
		}
		var err error
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				"Invalid 'from' date format, expected RFC3339")
			return
		}
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				"Invalid 'to' date format, expected RFC3339")
			return
		}
		if from.After(to) {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				"'from' must be before 'to'")
			return
		}
	} else {
		from, to = store.ResolveTimeRange(period)
	}

	groupBy := q.Get("group_by")
	if groupBy != "" && !validGroupBy[groupBy] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			fmt.Sprintf("Invalid group_by %q, supported: operator, apn, rat_type", groupBy))
		return
	}

	params := store.UsageQueryParams{
		TenantID: tenantID,
		Period:   period,
		From:     from,
		To:       to,
		GroupBy:  groupBy,
	}

	if v := q.Get("operator_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.OperatorID = &id
		} else {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
			return
		}
	}
	if v := q.Get("apn_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.APNID = &id
		} else {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid apn_id format")
			return
		}
	}
	if v := q.Get("rat_type"); v != "" {
		params.RATType = &v
	}

	ctx := r.Context()

	timeSeries, err := h.usageStore.GetTimeSeries(ctx, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("get time series")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	totals, err := h.usageStore.GetTotals(ctx, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("get totals")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	breakdowns := make(map[string][]breakdownDTO)
	for _, dim := range []string{"operator_id", "apn_id", "rat_type"} {
		items, err := h.usageStore.GetBreakdowns(ctx, params, dim)
		if err != nil {
			h.logger.Error().Err(err).Str("dimension", dim).Msg("get breakdowns")
			continue
		}
		if len(items) > 0 {
			dtos := make([]breakdownDTO, len(items))
			for i, item := range items {
				key := item.Key
				if dim == "operator_id" && h.operatorStore != nil {
					if id, err := uuid.Parse(item.Key); err == nil {
						if op, err := h.operatorStore.GetByID(ctx, id); err == nil {
							key = op.Name
						}
					}
				} else if dim == "apn_id" && h.apnStore != nil {
					if id, err := uuid.Parse(item.Key); err == nil {
						if apn, err := h.apnStore.GetByID(ctx, tenantID, id); err == nil {
							if apn.DisplayName != nil && *apn.DisplayName != "" {
								key = *apn.DisplayName
							} else {
								key = apn.Name
							}
						}
					}
				}
				dtos[i] = breakdownDTO{
					Key:        key,
					TotalBytes: item.TotalBytes,
					Sessions:   item.Sessions,
					Auths:      item.Auths,
					Percentage: item.Percentage,
				}
			}
			outputDim := dim
			if dim == "operator_id" {
				outputDim = "operator"
			} else if dim == "apn_id" {
				outputDim = "apn"
			}
			breakdowns[outputDim] = dtos
		}
	}

	topConsumers, err := h.usageStore.GetTopConsumers(ctx, params, 20)
	if err != nil {
		h.logger.Error().Err(err).Msg("get top consumers")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var comparison *comparisonDTO
	if q.Get("compare") == "true" {
		duration := to.Sub(from)
		prevFrom := from.Add(-duration)
		prevTo := from

		prevParams := params
		prevParams.From = prevFrom
		prevParams.To = prevTo

		prevTotals, err := h.usageStore.GetTotals(ctx, prevParams)
		if err != nil {
			h.logger.Warn().Err(err).Msg("get comparison totals")
		} else {
			comparison = &comparisonDTO{
				PreviousTotals: totalsDTO{
					TotalBytes:    prevTotals.TotalBytes,
					TotalSessions: prevTotals.TotalSessions,
					TotalAuths:    prevTotals.TotalAuths,
					UniqueSims:    prevTotals.UniqueSims,
				},
				BytesDelta:    deltaPercent(totals.TotalBytes, prevTotals.TotalBytes),
				SessionsDelta: deltaPercent(totals.TotalSessions, prevTotals.TotalSessions),
				AuthsDelta:    deltaPercent(totals.TotalAuths, prevTotals.TotalAuths),
				SimsDelta:     deltaPercent(totals.UniqueSims, prevTotals.UniqueSims),
			}
		}
	}

	spec := store.ResolvePeriod(period, from, to)

	tsDTO := make([]timeSeriesDTO, 0, len(timeSeries))
	for _, tp := range timeSeries {
		gk := tp.GroupKey
		if gk != "" && groupBy != "rat_type" {
			gk = h.resolveGroupKeyName(ctx, groupBy, gk, tenantID)
		}
		tsDTO = append(tsDTO, timeSeriesDTO{
			Timestamp:  tp.Timestamp.Format(time.RFC3339),
			TotalBytes: tp.TotalBytes,
			Sessions:   tp.Sessions,
			Auths:      tp.Auths,
			UniqueSims: tp.UniqueSims,
			GroupKey:   gk,
		})
	}

	tcDTO := make([]topConsumerDTO, 0, len(topConsumers))
	for _, tc := range topConsumers {
		dto := topConsumerDTO{
			SimID:      tc.SimID.String(),
			TotalBytes: tc.TotalBytes,
			Sessions:   tc.Sessions,
		}
		if h.simStore != nil {
			dto = h.enrichTopConsumer(ctx, tenantID, tc.SimID, dto)
		}
		tcDTO = append(tcDTO, dto)
	}

	resp := usageResponseDTO{
		Period:     period,
		From:       from.Format(time.RFC3339),
		To:         to.Format(time.RFC3339),
		BucketSize: spec.BucketInterval,
		TimeSeries: tsDTO,
		Totals: totalsDTO{
			TotalBytes:    totals.TotalBytes,
			TotalSessions: totals.TotalSessions,
			TotalAuths:    totals.TotalAuths,
			UniqueSims:    totals.UniqueSims,
		},
		Breakdowns:   breakdowns,
		TopConsumers: tcDTO,
		Comparison:   comparison,
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) enrichTopConsumer(ctx context.Context, tenantID, simID uuid.UUID, dto topConsumerDTO) topConsumerDTO {
	sim, err := h.simStore.GetByID(ctx, tenantID, simID)
	if err != nil {
		return dto
	}
	dto.ICCID = sim.ICCID

	if h.operatorStore != nil {
		if op, err := h.operatorStore.GetByID(ctx, sim.OperatorID); err == nil {
			dto.OperatorName = op.Name
		}
	}

	if h.apnStore != nil && sim.APNID != nil {
		if apn, err := h.apnStore.GetByID(ctx, tenantID, *sim.APNID); err == nil {
			if apn.DisplayName != nil && *apn.DisplayName != "" {
				dto.APNName = *apn.DisplayName
			} else {
				dto.APNName = apn.Name
			}
		}
	}

	if h.ippoolStore != nil && sim.IPAddressID != nil {
		if addr, err := h.ippoolStore.GetIPAddressByID(ctx, *sim.IPAddressID); err == nil {
			if addr.AddressV4 != nil {
				dto.IPAddress = *addr.AddressV4
			} else if addr.AddressV6 != nil {
				dto.IPAddress = *addr.AddressV6
			}
		}
	}

	return dto
}

func (h *Handler) resolveGroupKeyName(ctx context.Context, groupBy, key string, tenantID uuid.UUID) string {
	id, err := uuid.Parse(key)
	if err != nil {
		return key
	}
	switch groupBy {
	case "operator":
		if h.operatorStore != nil {
			if op, err := h.operatorStore.GetByID(ctx, id); err == nil {
				return op.Name
			}
		}
	case "apn":
		if h.apnStore != nil {
			if apn, err := h.apnStore.GetByID(ctx, tenantID, id); err == nil {
				if apn.DisplayName != nil && *apn.DisplayName != "" {
					return *apn.DisplayName
				}
				return apn.Name
			}
		}
	}
	return key
}

func deltaPercent(current, previous int64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100.0
	}
	return float64(current-previous) / float64(previous) * 100.0
}

func (h *Handler) GetCost(w http.ResponseWriter, r *http.Request) {
	if h.costService == nil {
		apierr.WriteError(w, http.StatusServiceUnavailable, apierr.CodeInternalError, "Cost analytics not available")
		return
	}

	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()

	period := q.Get("period")
	if period == "" {
		period = "30d"
	}
	if !validPeriods[period] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			fmt.Sprintf("Invalid period %q, supported: 1h, 24h, 7d, 30d, custom", period))
		return
	}

	var from, to time.Time
	if period == "custom" {
		fromStr := q.Get("from")
		toStr := q.Get("to")
		if fromStr == "" || toStr == "" {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				"'from' and 'to' are required for custom period")
			return
		}
		var err error
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				"Invalid 'from' date format, expected RFC3339")
			return
		}
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				"Invalid 'to' date format, expected RFC3339")
			return
		}
		if from.After(to) {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				"'from' must be before 'to'")
			return
		}
	} else {
		from, to = store.ResolveTimeRange(period)
	}

	var operatorID *uuid.UUID
	if v := q.Get("operator_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			operatorID = &id
		} else {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
			return
		}
	}

	var apnID *uuid.UUID
	if v := q.Get("apn_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			apnID = &id
		} else {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid apn_id format")
			return
		}
	}

	var ratType *string
	if v := q.Get("rat_type"); v != "" {
		ratType = &v
	}

	result, err := h.costService.GetCostAnalytics(r.Context(), tenantID, from, to, operatorID, apnID, ratType)
	if err != nil {
		h.logger.Error().Err(err).Msg("get cost analytics")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, result)
}
