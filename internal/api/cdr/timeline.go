package cdr

import (
	"net/http"

	"github.com/btopcu/argus/internal/analytics/aggregates"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type sessionTimelineResponse struct {
	SessionID string         `json:"session_id"`
	Count     int            `json:"count"`
	Items     []cdrDTO       `json:"items"`
	Stats     *timelineStats `json:"stats,omitempty"`
}

type timelineStats struct {
	TotalBytesIn  int64   `json:"total_bytes_in"`
	TotalBytesOut int64   `json:"total_bytes_out"`
	TotalCost     float64 `json:"total_cost"`
	DurationSec   int     `json:"duration_sec"`
}

// BySession returns all CDR rows for a given session ordered chronologically.
// GET /api/v1/cdrs/by-session/{session_id}
func (h *Handler) BySession(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	sessionIDStr := chi.URLParam(r, "session_id")
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid session_id format")
		return
	}

	rows, err := h.cdrStore.ListBySession(r.Context(), tenantID, sessionID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list cdrs by session")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if len(rows) == 0 {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeForbidden, "Session not found")
		return
	}

	items := make([]cdrDTO, 0, len(rows))
	var stats timelineStats
	for _, c := range rows {
		items = append(items, toCDRDTO(c))
		stats.TotalBytesIn += c.BytesIn
		stats.TotalBytesOut += c.BytesOut
		if c.UsageCost != nil {
			stats.TotalCost += *c.UsageCost
		}
		if c.DurationSec > stats.DurationSec {
			stats.DurationSec = c.DurationSec
		}
	}

	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
		Status: "success",
		Data: sessionTimelineResponse{
			SessionID: sessionID.String(),
			Count:     len(items),
			Items:     items,
			Stats:     &stats,
		},
	})
}

// Stats returns aggregate CDR counters over a filter window via the aggregates facade.
// GET /api/v1/cdrs/stats — mirrors list filter params; date range is required.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	params, ok := h.parseFilters(w, r, true)
	if !ok {
		return
	}

	filter := aggregates.CDRFilter{
		SimID:      params.SimID,
		OperatorID: params.OperatorID,
		APNID:      params.APNID,
		SessionID:  params.SessionID,
		RecordType: params.RecordType,
		RATType:    params.RATType,
		From:       params.From,
		To:         params.To,
		MinCost:    params.MinCost,
	}

	// PAT-012 — Aggregates facade is the single source of truth for cross-surface counts.
	// h.aggSvc is wired unconditionally in cmd/argus/main.go (WithAggregates).
	if h.aggSvc == nil {
		h.logger.Error().Msg("cdr stats: aggregates facade not wired")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	stats, err := h.aggSvc.CDRStatsInWindow(r.Context(), tenantID, filter)
	if err != nil {
		h.logger.Error().Err(err).Msg("cdr stats in window")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
		Status: "success",
		Data:   stats,
	})
}
