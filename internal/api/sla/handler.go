package sla

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const retentionDays = 91

type reportEngine interface {
	Build(ctx context.Context, req report.Request) (*report.Artifact, error)
}

type slaStoreIface interface {
	ListByTenant(ctx context.Context, tenantID uuid.UUID, from, to time.Time, operatorID *uuid.UUID, cursor string, limit int) ([]store.SLAReportRow, string, error)
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*store.SLAReportRow, error)
	HistoryByMonth(ctx context.Context, tenantID uuid.UUID, year, months int, operatorID *uuid.UUID) ([]store.MonthSummary, error)
	MonthDetail(ctx context.Context, tenantID uuid.UUID, year, month int) (*store.MonthSummary, error)
	GetByTenantOperatorMonth(ctx context.Context, tenantID, operatorID uuid.UUID, year, month int) (*store.SLAReportRow, error)
}

type Handler struct {
	store         slaStoreIface
	operatorStore *store.OperatorStore
	engine        reportEngine
	logger        zerolog.Logger
}

func NewHandler(s *store.SLAReportStore, os *store.OperatorStore, engine reportEngine, logger zerolog.Logger) *Handler {
	return &Handler{
		store:         s,
		operatorStore: os,
		engine:        engine,
		logger:        logger.With().Str("component", "sla_handler").Logger(),
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()

	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'from' parameter: must be RFC3339")
			return
		}
		from = t.UTC()
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'to' parameter: must be RFC3339")
			return
		}
		to = t.UTC()
	}

	if from.After(to) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "'from' must be before 'to'")
		return
	}

	var operatorID *uuid.UUID
	if v := q.Get("operator_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'operator_id' parameter")
			return
		}
		operatorID = &id
	}

	cursor := q.Get("cursor")

	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'limit' parameter")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}

	rows, nextCursor, err := h.store.ListByTenant(r.Context(), tenantID, from, to, operatorID, cursor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list sla reports")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to list SLA reports")
		return
	}

	apierr.WriteList(w, http.StatusOK, rows, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid SLA report ID")
		return
	}

	row, err := h.store.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if err == store.ErrSLAReportNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SLA report not found")
			return
		}
		h.logger.Error().Err(err).Str("id", id.String()).Msg("get sla report")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to get SLA report")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, row)
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	now := time.Now().UTC()

	var year, months int

	if v := q.Get("year"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 2020 || n > now.Year() {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidYear, "year must be between 2020 and current year")
			return
		}
		year = n
	}

	if v := q.Get("months"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 24 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidMonthsRange, "months must be between 1 and 24")
			return
		}
		months = n
	}

	if year == 0 && months == 0 {
		months = 6
	}

	var operatorID *uuid.UUID
	if v := q.Get("operator_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidOperatorID, "operator_id must be a valid UUID")
			return
		}
		if h.operatorStore != nil {
			if _, gErr := h.operatorStore.GetGrantByTenantOperator(r.Context(), tenantID, id); gErr != nil {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "operator not found")
				return
			}
		}
		operatorID = &id
	}

	summaries, err := h.store.HistoryByMonth(r.Context(), tenantID, year, months, operatorID)
	if err != nil {
		h.logger.Error().Err(err).Msg("sla history by month")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to load SLA history")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, summaries)
}

func (h *Handler) MonthDetail(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	yearStr := chi.URLParam(r, "year")
	monthStr := chi.URLParam(r, "month")

	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2020 || year > time.Now().UTC().Year() {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidYear, "year must be between 2020 and current year")
		return
	}

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidMonth, "month must be between 1 and 12")
		return
	}

	summary, err := h.store.MonthDetail(r.Context(), tenantID, year, month)
	if err != nil {
		h.logger.Error().Err(err).Int("year", year).Int("month", month).Msg("sla month detail")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to load SLA month detail")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, summary)
}

func (h *Handler) OperatorMonthBreaches(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	operatorIDStr := chi.URLParam(r, "operatorId")
	operatorID, err := uuid.Parse(operatorIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidOperatorID, "operatorId must be a valid UUID")
		return
	}

	yearStr := chi.URLParam(r, "year")
	monthStr := chi.URLParam(r, "month")

	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2020 || year > time.Now().UTC().Year() {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidYear, "year must be between 2020 and current year")
		return
	}

	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidMonth, "month must be between 1 and 12")
		return
	}

	ctx := r.Context()
	now := time.Now().UTC()
	monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	monthSeconds := int64(monthEnd.Sub(monthStart).Seconds())

	if monthEnd.Before(now.AddDate(0, 0, -retentionDays)) {
		rep, repErr := h.store.GetByTenantOperatorMonth(ctx, tenantID, operatorID, year, month)
		if errors.Is(repErr, store.ErrSLAReportNotFound) || rep == nil {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeSLAMonthNotAvailable, "No persisted SLA data for that month")
			return
		}
		if repErr != nil {
			h.logger.Error().Err(repErr).Msg("sla operator month breaches (persisted)")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to load persisted SLA data")
			return
		}
		breaches := parseBreachesFromDetails(rep.Details)
		breaches = enrichAffectedSessions(breaches, rep.SessionsTotal, monthSeconds)
		totals := computeBreachTotals(breaches)
		meta := map[string]any{
			"breach_source":              "persisted",
			"affected_sessions_est_note": "approx: sessions_total * (duration_sec / month_seconds)",
		}
		apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
			Status: "success",
			Data: map[string]any{
				"breaches": breaches,
				"totals":   totals,
			},
			Meta: meta,
		})
		return
	}

	if h.operatorStore != nil {
		if _, gErr := h.operatorStore.GetGrantByTenantOperator(ctx, tenantID, operatorID); gErr != nil {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "operator not found")
			return
		}
	}

	op, opErr := h.operatorStore.GetByID(ctx, operatorID)
	if opErr != nil {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "operator not found")
		return
	}

	breaches, liveErr := h.operatorStore.BreachesForOperatorMonth(ctx, operatorID, year, month, op.SLALatencyThresholdMs)
	if liveErr != nil {
		h.logger.Error().Err(liveErr).Msg("sla operator month breaches (live)")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to load SLA breach data")
		return
	}

	var sessionsForMonth int64
	if rep, rErr := h.store.GetByTenantOperatorMonth(ctx, tenantID, operatorID, year, month); rErr == nil && rep != nil {
		sessionsForMonth = rep.SessionsTotal
	}
	breaches = enrichAffectedSessions(breaches, sessionsForMonth, monthSeconds)
	totals := computeBreachTotals(breaches)

	meta := map[string]any{
		"breach_source":              "live",
		"affected_sessions_est_note": "approx: sessions_total * (duration_sec / month_seconds)",
	}
	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
		Status: "success",
		Data: map[string]any{
			"breaches": breaches,
			"totals":   totals,
		},
		Meta: meta,
	})
}

func (h *Handler) DownloadPDF(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	now := time.Now().UTC()

	yearStr := q.Get("year")
	if yearStr == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidYear, "year is required")
		return
	}
	year, err := strconv.Atoi(yearStr)
	if err != nil || year < 2020 || year > now.Year() {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidYear, "year must be between 2020 and current year")
		return
	}

	monthStr := q.Get("month")
	if monthStr == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidMonth, "month is required")
		return
	}
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidMonth, "month must be between 1 and 12")
		return
	}

	var operatorID *uuid.UUID
	if v := q.Get("operator_id"); v != "" {
		id, parseErr := uuid.Parse(v)
		if parseErr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidOperatorID, "operator_id must be a valid UUID")
			return
		}
		if h.operatorStore != nil {
			if _, gErr := h.operatorStore.GetGrantByTenantOperator(r.Context(), tenantID, id); gErr != nil {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "operator not found")
				return
			}
		}
		operatorID = &id
	}

	windowStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.AddDate(0, 1, 0)

	rows, _, rowErr := h.store.ListByTenant(r.Context(), tenantID, windowStart, windowEnd, operatorID, "", 1)
	if rowErr != nil {
		h.logger.Error().Err(rowErr).Msg("sla pdf: check month data")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to check SLA data availability")
		return
	}
	if len(rows) == 0 {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeSLAMonthNotAvailable, "no SLA data available for the requested month")
		return
	}

	filters := map[string]any{
		"year":  year,
		"month": month,
	}
	if operatorID != nil {
		filters["operator_id"] = operatorID.String()
	}

	artifact, buildErr := h.engine.Build(r.Context(), report.Request{
		Type:     report.ReportSLAMonthly,
		Format:   report.FormatPDF,
		TenantID: tenantID,
		Filters:  filters,
	})
	if buildErr != nil {
		h.logger.Error().Err(buildErr).Msg("sla pdf: build")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to generate PDF")
		return
	}

	if len(artifact.Bytes) == 0 {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeSLAMonthNotAvailable, "no SLA data available for the requested month")
		return
	}

	filename := fmt.Sprintf("sla-%04d-%02d-all.pdf", year, month)
	if operatorID != nil {
		code := operatorID.String()[:8]
		if h.operatorStore != nil {
			if op, opErr := h.operatorStore.GetByID(r.Context(), *operatorID); opErr == nil && op.Code != "" {
				code = op.Code
			}
		}
		filename = fmt.Sprintf("sla-%04d-%02d-%s.pdf", year, month, code)
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact.Bytes)))
	w.WriteHeader(http.StatusOK)
	w.Write(artifact.Bytes) //nolint:errcheck
}

type detailsBreachShape struct {
	Breaches []store.Breach `json:"breaches"`
}

func parseBreachesFromDetails(raw json.RawMessage) []store.Breach {
	if len(raw) == 0 {
		return []store.Breach{}
	}
	var shape detailsBreachShape
	if err := json.Unmarshal(raw, &shape); err != nil {
		return []store.Breach{}
	}
	if shape.Breaches == nil {
		return []store.Breach{}
	}
	return shape.Breaches
}

// enrichAffectedSessions fills per-breach AffectedSessionsEst using
// sessions_total * (duration_sec / month_seconds). Safe with zero inputs.
func enrichAffectedSessions(breaches []store.Breach, sessionsTotal, monthSeconds int64) []store.Breach {
	if monthSeconds <= 0 || sessionsTotal <= 0 {
		return breaches
	}
	out := make([]store.Breach, len(breaches))
	for i, b := range breaches {
		if b.AffectedSessionsEst == 0 && b.DurationSec > 0 {
			b.AffectedSessionsEst = sessionsTotal * int64(b.DurationSec) / monthSeconds
		}
		out[i] = b
	}
	return out
}

// computeBreachTotals aggregates totals for the response envelope per plan API spec.
func computeBreachTotals(breaches []store.Breach) map[string]any {
	var downtime int64
	var sessions int64
	for _, b := range breaches {
		downtime += int64(b.DurationSec)
		sessions += b.AffectedSessionsEst
	}
	return map[string]any{
		"breaches_count":        len(breaches),
		"downtime_seconds":      downtime,
		"affected_sessions_est": sessions,
	}
}
