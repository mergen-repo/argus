package alert

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/alertstate"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// reportEngine is the narrow interface the alert handler needs from the
// report package — kept as an interface (not *report.Engine) so tests can
// stub it without spinning up a full engine. Concrete production wiring
// passes *report.Engine.
type reportEngine interface {
	Build(ctx context.Context, req report.Request) (*report.Artifact, error)
}

// suppressionStore is the narrow interface the alert handler needs from
// store.AlertSuppressionStore — kept as an interface so tests can capture
// CreateAlertSuppressionParams without a live database. Production wiring
// passes *store.AlertSuppressionStore.
type suppressionStore interface {
	Create(ctx context.Context, p store.CreateAlertSuppressionParams) (*store.AlertSuppression, error)
	List(ctx context.Context, tenantID uuid.UUID, p store.ListAlertSuppressionsParams) ([]store.AlertSuppression, *uuid.UUID, error)
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
}

// alertBackfillStore is the narrow interface for backfill / restore on the
// alerts table when a suppression is created or deleted. Implemented by
// *store.AlertStore. Carved out so tests can run the handler without forcing
// a full alert-store stub.
type alertBackfillStore interface {
	BackfillSuppression(ctx context.Context, tenantID uuid.UUID, scopeType, scopeValue string, suppressionID uuid.UUID, reason string) (int64, error)
	RestoreSuppressedByMetaID(ctx context.Context, tenantID, suppressionID uuid.UUID) (int64, error)
}

// alertExportRowCap is the maximum number of rows returned by export endpoints.
// The store enforces a 100-row limit per call, so export handlers paginate
// internally until this cap or EOF is reached.
const alertExportRowCap = 10000

var validAlertSources = map[string]bool{
	"sim":      true,
	"operator": true,
	"infra":    true,
	"policy":   true,
	"system":   true,
}

type Handler struct {
	alertStore       *store.AlertStore
	auditSvc         audit.Auditor
	logger           zerolog.Logger
	cooldownMinutes  int
	reportEngine     reportEngine
	suppressionStore suppressionStore
	// alertBackfill defaults to alertStore but is split as an interface so
	// CreateSuppression / DeleteSuppression handler tests can stub backfill
	// without a real *store.AlertStore.
	alertBackfill alertBackfillStore
}

func NewHandler(alertStore *store.AlertStore, auditSvc audit.Auditor, logger zerolog.Logger, cooldownMinutes int) *Handler {
	if cooldownMinutes < 0 {
		cooldownMinutes = 0
	}
	h := &Handler{
		alertStore:      alertStore,
		auditSvc:        auditSvc,
		logger:          logger.With().Str("component", "alert_handler").Logger(),
		cooldownMinutes: cooldownMinutes,
	}
	if alertStore != nil {
		h.alertBackfill = alertStore
	}
	return h
}

// WithReportEngine wires the report engine for PDF exports. When nil,
// ExportPDF returns 503 SERVICE_UNAVAILABLE. Mirrors store.AlertStore's
// WithSuppressionStore builder pattern.
func (h *Handler) WithReportEngine(engine reportEngine) *Handler {
	h.reportEngine = engine
	return h
}

// WithSuppressionStore wires the alert-suppression store used by the
// CreateSuppression / ListSuppressions / DeleteSuppression endpoints
// (FIX-229 AC-1 + AC-5). When nil, those endpoints return 503
// SERVICE_UNAVAILABLE.
func (h *Handler) WithSuppressionStore(s *store.AlertSuppressionStore) *Handler {
	if s == nil {
		h.suppressionStore = nil
		return h
	}
	h.suppressionStore = s
	return h
}

// withAlertBackfillStore is a test-only seam for substituting the
// BackfillSuppression / RestoreSuppressedByMetaID dependency. Production code
// uses NewHandler which wires the real *store.AlertStore.
func (h *Handler) withAlertBackfillStore(s alertBackfillStore) *Handler {
	h.alertBackfill = s
	return h
}

type alertDTO struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	Type            string          `json:"type"`
	Severity        string          `json:"severity"`
	Source          string          `json:"source"`
	State           string          `json:"state"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	Meta            json.RawMessage `json:"meta"`
	SimID           *string         `json:"sim_id"`
	OperatorID      *string         `json:"operator_id"`
	APNID           *string         `json:"apn_id"`
	DedupKey        *string         `json:"dedup_key"`
	FiredAt         string          `json:"fired_at"`
	AcknowledgedAt  *string         `json:"acknowledged_at"`
	AcknowledgedBy  *string         `json:"acknowledged_by"`
	ResolvedAt      *string         `json:"resolved_at"`
	OccurrenceCount int             `json:"occurrence_count"`
	FirstSeenAt     string          `json:"first_seen_at"`
	LastSeenAt      string          `json:"last_seen_at"`
	CooldownUntil   *string         `json:"cooldown_until"`
}

func toAlertDTO(a *store.Alert) alertDTO {
	dto := alertDTO{
		ID:              a.ID.String(),
		TenantID:        a.TenantID.String(),
		Type:            a.Type,
		Severity:        a.Severity,
		Source:          a.Source,
		State:           a.State,
		Title:           a.Title,
		Description:     a.Description,
		Meta:            a.Meta,
		DedupKey:        a.DedupKey,
		FiredAt:         a.FiredAt.UTC().Format(time.RFC3339),
		OccurrenceCount: a.OccurrenceCount,
		FirstSeenAt:     a.FirstSeenAt.UTC().Format(time.RFC3339),
		LastSeenAt:      a.LastSeenAt.UTC().Format(time.RFC3339),
	}
	if a.SimID != nil {
		s := a.SimID.String()
		dto.SimID = &s
	}
	if a.OperatorID != nil {
		s := a.OperatorID.String()
		dto.OperatorID = &s
	}
	if a.APNID != nil {
		s := a.APNID.String()
		dto.APNID = &s
	}
	if a.AcknowledgedAt != nil {
		s := a.AcknowledgedAt.UTC().Format(time.RFC3339)
		dto.AcknowledgedAt = &s
	}
	if a.AcknowledgedBy != nil {
		s := a.AcknowledgedBy.String()
		dto.AcknowledgedBy = &s
	}
	if a.ResolvedAt != nil {
		s := a.ResolvedAt.UTC().Format(time.RFC3339)
		dto.ResolvedAt = &s
	}
	if a.CooldownUntil != nil {
		s := a.CooldownUntil.UTC().Format(time.RFC3339)
		dto.CooldownUntil = &s
	}
	return dto
}

// parseAlertListFilters extracts the standard alert list filters from a request
// query string. Reused by List, ExportCSV, ExportJSON, and ExportPDF.
// It does NOT parse limit or cursor — callers control those independently.
func (h *Handler) parseAlertListFilters(r *http.Request) (store.ListAlertsParams, int, string, string) {
	q := r.URL.Query()

	sevFilter := q.Get("severity")
	if sevFilter != "" {
		if err := severity.Validate(sevFilter); err != nil {
			return store.ListAlertsParams{}, http.StatusBadRequest, apierr.CodeInvalidSeverity,
				"severity must be one of: critical, high, medium, low, info; got '" + sevFilter + "'"
		}
	}

	stateFilter := q.Get("state")
	if stateFilter != "" && alertstate.Validate(stateFilter) != nil {
		return store.ListAlertsParams{}, http.StatusBadRequest, apierr.CodeValidationError,
			"state must be one of: open, acknowledged, resolved, suppressed; got '" + stateFilter + "'"
	}

	sourceFilter := q.Get("source")
	if sourceFilter != "" && !validAlertSources[sourceFilter] {
		return store.ListAlertsParams{}, http.StatusBadRequest, apierr.CodeValidationError,
			"source must be one of: sim, operator, infra, policy, system; got '" + sourceFilter + "'"
	}

	params := store.ListAlertsParams{
		Type:     q.Get("type"),
		Severity: sevFilter,
		Source:   sourceFilter,
		State:    stateFilter,
		// FIX-229 Gate F-A1: dedup_key deeplink — similar-alerts "View all"
		// links to /alerts?dedup_key=<key> when the anchor row has one.
		DedupKey: q.Get("dedup_key"),
		Q:        q.Get("q"),
	}

	if v := q.Get("sim_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return store.ListAlertsParams{}, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id format"
		}
		params.SimID = &id
	}
	if v := q.Get("operator_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return store.ListAlertsParams{}, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format"
		}
		params.OperatorID = &id
	}
	if v := q.Get("apn_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return store.ListAlertsParams{}, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid apn_id format"
		}
		params.APNID = &id
	}

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return store.ListAlertsParams{}, http.StatusBadRequest, apierr.CodeValidationError, "Invalid 'from' date format; expected RFC3339"
		}
		params.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return store.ListAlertsParams{}, http.StatusBadRequest, apierr.CodeValidationError, "Invalid 'to' date format; expected RFC3339"
		}
		params.To = &t
	}

	return params, 0, "", ""
}

// collectExportAlerts pages through the store (store caps each call at 100) until
// alertExportRowCap rows are collected or there are no more results.
func (h *Handler) collectExportAlerts(r *http.Request, tenantID uuid.UUID, base store.ListAlertsParams) ([]store.Alert, error) {
	const pageSize = 100
	var all []store.Alert
	params := base
	params.Limit = pageSize
	params.Cursor = nil

	for len(all) < alertExportRowCap {
		batch, nextCursor, err := h.alertStore.ListByTenant(r.Context(), tenantID, params)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if nextCursor == nil || len(all) >= alertExportRowCap {
			break
		}
		params.Cursor = nextCursor
	}
	if len(all) > alertExportRowCap {
		all = all[:alertExportRowCap]
	}
	return all, nil
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	params, status, code, msg := h.parseAlertListFilters(r)
	if status != 0 {
		apierr.WriteError(w, status, code, msg)
		return
	}
	params.Limit = limit

	if v := q.Get("cursor"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid cursor format")
			return
		}
		params.Cursor = &id
	}

	alerts, nextCursor, err := h.alertStore.ListByTenant(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list alerts")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]alertDTO, 0, len(alerts))
	for i := range alerts {
		dtos = append(dtos, toAlertDTO(&alerts[i]))
	}

	meta := apierr.ListMeta{
		HasMore: nextCursor != nil,
		Limit:   limit,
	}
	if nextCursor != nil {
		meta.Cursor = nextCursor.String()
	}
	apierr.WriteList(w, http.StatusOK, dtos, meta)
}

// ExportCSV streams all matching alerts (up to alertExportRowCap) as a CSV download.
// Not enveloped — this is a file download endpoint.
func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	base, status, code, msg := h.parseAlertListFilters(r)
	if status != 0 {
		apierr.WriteError(w, status, code, msg)
		return
	}

	alerts, err := h.collectExportAlerts(r, tenantID, base)
	if err != nil {
		h.logger.Error().Err(err).Msg("export alerts csv")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	filename := fmt.Sprintf("alerts-%s.csv", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"id", "fired_at", "severity", "state", "source", "type",
		"title", "description", "sim_id", "operator_id", "apn_id",
		"dedup_key", "occurrence_count", "first_seen_at", "last_seen_at",
		"acknowledged_at", "resolved_at",
	})
	for i := range alerts {
		a := &alerts[i]
		row := []string{
			a.ID.String(),
			a.FiredAt.UTC().Format(time.RFC3339),
			a.Severity,
			a.State,
			a.Source,
			a.Type,
			a.Title,
			a.Description,
			nullUUIDStr(a.SimID),
			nullUUIDStr(a.OperatorID),
			nullUUIDStr(a.APNID),
			nullStrVal(a.DedupKey),
			strconv.Itoa(a.OccurrenceCount),
			a.FirstSeenAt.UTC().Format(time.RFC3339),
			a.LastSeenAt.UTC().Format(time.RFC3339),
			nullTimeStr(a.AcknowledgedAt),
			nullTimeStr(a.ResolvedAt),
		}
		_ = cw.Write(row)
	}
	cw.Flush()

	audit.Emit(r, h.logger, h.auditSvc, "alert.exported", "alert", "export", nil, map[string]interface{}{
		"format":  "csv",
		"rows":    len(alerts),
		"filters": exportFilterMap(base),
	})
}

// ExportJSON returns a raw JSON array (NOT the standard envelope).
// This is a download endpoint, not a JSON-API endpoint. Mirrors the CSV behavior.
func (h *Handler) ExportJSON(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	base, status, code, msg := h.parseAlertListFilters(r)
	if status != 0 {
		apierr.WriteError(w, status, code, msg)
		return
	}

	alerts, err := h.collectExportAlerts(r, tenantID, base)
	if err != nil {
		h.logger.Error().Err(err).Msg("export alerts json")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]alertDTO, 0, len(alerts))
	for i := range alerts {
		dtos = append(dtos, toAlertDTO(&alerts[i]))
	}

	filename := fmt.Sprintf("alerts-%s.json", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")

	_ = json.NewEncoder(w).Encode(dtos)

	audit.Emit(r, h.logger, h.auditSvc, "alert.exported", "alert", "export", nil, map[string]interface{}{
		"format":  "json",
		"rows":    len(alerts),
		"filters": exportFilterMap(base),
	})
}

// ExportPDF streams a server-rendered PDF of matching alerts. Filters mirror
// List/ExportCSV/ExportJSON. Internally pages through the store via the
// report.Engine data provider; first 200 rows print, full count appears in
// the breakdown header. Not enveloped — binary stream.
//
// FIX-229 Task 7 (DEV-338, D-091): synchronous engine path is acceptable now;
// FIX-248 will migrate to async/queued.
func (h *Handler) ExportPDF(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	if h.reportEngine == nil {
		apierr.WriteError(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "PDF export unavailable")
		return
	}

	base, status, code, msg := h.parseAlertListFilters(r)
	if status != 0 {
		apierr.WriteError(w, status, code, msg)
		return
	}

	// Cheap empty-set probe BEFORE engine build — surfaces the 404 with the
	// standard envelope before any binary header is written. The engine
	// itself would still build a valid (header-only) PDF for zero rows, so
	// this short-circuit also avoids wasted work.
	probe := base
	probe.Limit = 1
	probe.Cursor = nil
	probeRows, _, probeErr := h.alertStore.ListByTenant(r.Context(), tenantID, probe)
	if probeErr != nil {
		h.logger.Error().Err(probeErr).Msg("export alerts pdf: probe")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if len(probeRows) == 0 {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeAlertNoData, "no alerts match the requested filters")
		return
	}

	artifact, buildErr := h.reportEngine.Build(r.Context(), report.Request{
		Type:     report.ReportAlertsExport,
		Format:   report.FormatPDF,
		TenantID: tenantID,
		Filters:  alertFilterMap(base),
	})
	if buildErr != nil {
		h.logger.Error().Err(buildErr).Msg("export alerts pdf: build")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to generate PDF")
		return
	}
	if artifact == nil || len(artifact.Bytes) == 0 {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeAlertNoData, "no alerts match the requested filters")
		return
	}

	filename := fmt.Sprintf("alerts-%s.pdf", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact.Bytes)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Bytes)

	audit.Emit(r, h.logger, h.auditSvc, "alert.exported", "alert", "export", nil, map[string]interface{}{
		"format":  "pdf",
		"bytes":   len(artifact.Bytes),
		"filters": exportFilterMap(base),
	})
}

// alertFilterMap converts ListAlertsParams into the map[string]any expected
// by report.Request.Filters. Mirrors exportFilterMap but preserves typed
// values (uuid.UUID, time.Time) the report data provider unpacks.
func alertFilterMap(p store.ListAlertsParams) map[string]any {
	m := map[string]any{}
	if p.Type != "" {
		m["type"] = p.Type
	}
	if p.Severity != "" {
		m["severity"] = p.Severity
	}
	if p.Source != "" {
		m["source"] = p.Source
	}
	if p.State != "" {
		m["state"] = p.State
	}
	if p.Q != "" {
		m["q"] = p.Q
	}
	if p.SimID != nil {
		m["sim_id"] = *p.SimID
	}
	if p.OperatorID != nil {
		m["operator_id"] = *p.OperatorID
	}
	if p.APNID != nil {
		m["apn_id"] = *p.APNID
	}
	if p.From != nil {
		m["from"] = *p.From
	}
	if p.To != nil {
		m["to"] = *p.To
	}
	return m
}

func nullUUIDStr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func nullStrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullTimeStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func exportFilterMap(p store.ListAlertsParams) map[string]interface{} {
	m := map[string]interface{}{}
	if p.Type != "" {
		m["type"] = p.Type
	}
	if p.Severity != "" {
		m["severity"] = p.Severity
	}
	if p.Source != "" {
		m["source"] = p.Source
	}
	if p.State != "" {
		m["state"] = p.State
	}
	if p.Q != "" {
		m["q"] = p.Q
	}
	if p.SimID != nil {
		m["sim_id"] = p.SimID.String()
	}
	if p.OperatorID != nil {
		m["operator_id"] = p.OperatorID.String()
	}
	if p.APNID != nil {
		m["apn_id"] = p.APNID.String()
	}
	if p.From != nil {
		m["from"] = p.From.UTC().Format(time.RFC3339)
	}
	if p.To != nil {
		m["to"] = p.To.UTC().Format(time.RFC3339)
	}
	return m
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid alert ID")
		return
	}

	a, err := h.alertStore.GetByID(r.Context(), tenantID, id)
	if errors.Is(err, store.ErrAlertNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeAlertNotFound, "alert not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("get alert")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toAlertDTO(a))
}

func (h *Handler) ListSimilar(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	anchorID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid alert ID")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		parsed, parseErr := strconv.Atoi(v)
		if parseErr != nil || parsed < 1 || parsed > 50 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "limit must be an integer between 1 and 50")
			return
		}
		limit = parsed
	}

	anchor, err := h.alertStore.GetByID(r.Context(), tenantID, anchorID)
	if errors.Is(err, store.ErrAlertNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeAlertNotFound, "alert not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("get anchor alert for similar")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	similar, strategy, err := h.alertStore.ListSimilar(r.Context(), tenantID, anchor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list similar alerts")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]alertDTO, 0, len(similar))
	for i := range similar {
		dtos = append(dtos, toAlertDTO(&similar[i]))
	}

	apierr.WriteJSON(w, http.StatusOK, struct {
		Status string      `json:"status"`
		Data   []alertDTO  `json:"data"`
		Meta   interface{} `json:"meta"`
	}{
		Status: "success",
		Data:   dtos,
		Meta: map[string]interface{}{
			"anchor_id":       anchorID.String(),
			"match_strategy":  strategy,
			"count":           len(dtos),
		},
	})
}

type updateStateRequest struct {
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
}

func (h *Handler) UpdateState(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid alert ID")
		return
	}

	var req updateStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	if !alertstate.IsUpdateAllowed(req.State) {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeInvalidStateTransition,
			"invalid alert state transition; allowed: acknowledged, resolved")
		return
	}
	if len(req.Note) > 2000 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "note must be 2000 characters or fewer")
		return
	}

	var userPtr *uuid.UUID
	if userID != uuid.Nil && req.State == "acknowledged" {
		uid := userID
		userPtr = &uid
	}

	a, err := h.alertStore.UpdateState(r.Context(), tenantID, id, req.State, userPtr, h.cooldownMinutes)
	if errors.Is(err, store.ErrAlertNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeAlertNotFound, "alert not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAlertTransition) {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeInvalidStateTransition,
			"invalid alert state transition; allowed: acknowledged, resolved")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("update alert state")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	afterData := map[string]string{"state": req.State}
	if req.Note != "" {
		afterData["note"] = req.Note
	}
	audit.Emit(r, h.logger, h.auditSvc, "alert.update", "alert", id.String(), nil, afterData)

	apierr.WriteSuccess(w, http.StatusOK, toAlertDTO(a))
}

// ----- FIX-229 AC-1 + AC-5 — Alert suppressions CRUD -----------------------

// suppressionDTO is the wire shape for an alert_suppressions row.
type suppressionDTO struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	ScopeType  string  `json:"scope_type"`
	ScopeValue string  `json:"scope_value"`
	ExpiresAt  string  `json:"expires_at"`
	Reason     *string `json:"reason"`
	RuleName   *string `json:"rule_name"`
	CreatedBy  *string `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
}

func toSuppressionDTO(s *store.AlertSuppression) suppressionDTO {
	dto := suppressionDTO{
		ID:         s.ID.String(),
		TenantID:   s.TenantID.String(),
		ScopeType:  s.ScopeType,
		ScopeValue: s.ScopeValue,
		ExpiresAt:  s.ExpiresAt.UTC().Format(time.RFC3339),
		Reason:     s.Reason,
		RuleName:   s.RuleName,
		CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
	}
	if s.CreatedBy != nil {
		v := s.CreatedBy.String()
		dto.CreatedBy = &v
	}
	return dto
}

type createSuppressionRequest struct {
	ScopeType  string     `json:"scope_type"`
	ScopeValue string     `json:"scope_value"`
	Duration   string     `json:"duration,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	RuleName   string     `json:"rule_name,omitempty"`
}

// validScopeTypes mirrors the DB CHECK constraint (chk_alert_suppressions_scope_type).
var validScopeTypes = map[string]bool{
	"this":      true,
	"type":      true,
	"operator":  true,
	"dedup_key": true,
}

// suppressionMaxLookahead caps a custom expires_at to NOW+30d (DEV-341).
const (
	suppressionMaxLookahead   = 30 * 24 * time.Hour
	suppressionReasonMaxLen   = 500
	suppressionRuleNameMaxLen = 100
	suppressionScopeValueMax  = 255
)

// CreateSuppression — POST /api/v1/alerts/suppressions (FIX-229 AC-1 + AC-5).
// Inserts a new mute rule, optionally backfills already-open matching alerts to
// state='suppressed', and emits alert.suppression.created.
func (h *Handler) CreateSuppression(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	if h.suppressionStore == nil {
		apierr.WriteError(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alert suppressions unavailable")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	var req createSuppressionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	if !validScopeTypes[req.ScopeType] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"scope_type must be one of: this, type, operator, dedup_key")
		return
	}
	scopeValue := strings.TrimSpace(req.ScopeValue)
	if scopeValue == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "scope_value is required")
		return
	}
	if len(scopeValue) > suppressionScopeValueMax {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("scope_value must be %d characters or fewer", suppressionScopeValueMax))
		return
	}
	// Canonicalise UUID-bearing scopes to lowercase hyphenated form so
	// MatchActive's text comparison hits at trigger time (DEV-340).
	if req.ScopeType == "this" || req.ScopeType == "operator" {
		parsed, perr := uuid.Parse(scopeValue)
		if perr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				fmt.Sprintf("scope_value must be a valid UUID for scope_type=%q", req.ScopeType))
			return
		}
		scopeValue = parsed.String()
	}

	// Exactly one of duration / expires_at.
	durationProvided := req.Duration != ""
	expiresProvided := req.ExpiresAt != nil
	if durationProvided == expiresProvided {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"exactly one of duration or expires_at is required")
		return
	}

	now := time.Now().UTC()
	var expiresAt time.Time
	if durationProvided {
		switch req.Duration {
		case "1h":
			expiresAt = now.Add(time.Hour)
		case "24h":
			expiresAt = now.Add(24 * time.Hour)
		case "7d":
			expiresAt = now.Add(7 * 24 * time.Hour)
		default:
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				`duration must be one of: "1h", "24h", "7d"`)
			return
		}
	} else {
		expiresAt = req.ExpiresAt.UTC()
		if !expiresAt.After(now) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				"expires_at must be in the future")
			return
		}
		if expiresAt.After(now.Add(suppressionMaxLookahead)) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				"expires_at must be within 30 days from now")
			return
		}
	}

	if len(req.Reason) > suppressionReasonMaxLen {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("reason must be %d characters or fewer", suppressionReasonMaxLen))
		return
	}
	if len(req.RuleName) > suppressionRuleNameMaxLen {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("rule_name must be %d characters or fewer", suppressionRuleNameMaxLen))
		return
	}

	params := store.CreateAlertSuppressionParams{
		TenantID:   tenantID,
		ScopeType:  req.ScopeType,
		ScopeValue: scopeValue,
		ExpiresAt:  expiresAt,
	}
	if req.Reason != "" {
		v := req.Reason
		params.Reason = &v
	}
	if req.RuleName != "" {
		v := req.RuleName
		params.RuleName = &v
	}
	if userID != uuid.Nil {
		uid := userID
		params.CreatedBy = &uid
	}

	created, err := h.suppressionStore.Create(r.Context(), params)
	if errors.Is(err, store.ErrDuplicateRuleName) {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeDuplicate,
			"a suppression rule with this name already exists")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("create alert suppression")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	// Best-effort backfill of currently-open matching alerts (R7: NOT acked/resolved).
	var appliedCount int64
	reason := ""
	if created.Reason != nil {
		reason = *created.Reason
	}
	if h.alertBackfill != nil {
		n, bfErr := h.alertBackfill.BackfillSuppression(r.Context(), tenantID, created.ScopeType, created.ScopeValue, created.ID, reason)
		if bfErr != nil {
			h.logger.Warn().Err(bfErr).Str("suppression_id", created.ID.String()).Msg("backfill suppression failed")
		} else {
			appliedCount = n
		}
	}

	auditAfter := map[string]interface{}{
		"scope_type":    created.ScopeType,
		"scope_value":   created.ScopeValue,
		"expires_at":    created.ExpiresAt.UTC().Format(time.RFC3339),
		"applied_count": appliedCount,
	}
	if created.RuleName != nil {
		auditAfter["rule_name"] = *created.RuleName
	}
	audit.Emit(r, h.logger, h.auditSvc, "alert.suppression.created", "alert_suppression", created.ID.String(), nil, auditAfter)

	resp := struct {
		suppressionDTO
		AppliedCount int64 `json:"applied_count"`
	}{
		suppressionDTO: toSuppressionDTO(created),
		AppliedCount:   appliedCount,
	}
	apierr.WriteSuccess(w, http.StatusCreated, resp)
}

// ListSuppressions — GET /api/v1/alerts/suppressions (FIX-229 AC-5).
func (h *Handler) ListSuppressions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	if h.suppressionStore == nil {
		apierr.WriteError(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alert suppressions unavailable")
		return
	}

	q := r.URL.Query()
	activeOnly := true
	if v := q.Get("active_only"); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "no":
			activeOnly = false
		case "true", "1", "yes":
			activeOnly = true
		default:
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				"active_only must be true or false")
			return
		}
	}

	limit := 50
	if v := q.Get("limit"); v != "" {
		parsed, perr := strconv.Atoi(v)
		if perr != nil || parsed < 1 || parsed > 100 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				"limit must be an integer between 1 and 100")
			return
		}
		limit = parsed
	}

	params := store.ListAlertSuppressionsParams{
		ActiveOnly: activeOnly,
		Limit:      limit,
	}
	if v := q.Get("cursor"); v != "" {
		id, perr := uuid.Parse(v)
		if perr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid cursor format")
			return
		}
		params.Cursor = &id
	}

	rows, nextCursor, err := h.suppressionStore.List(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list alert suppressions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]suppressionDTO, 0, len(rows))
	for i := range rows {
		dtos = append(dtos, toSuppressionDTO(&rows[i]))
	}

	meta := apierr.ListMeta{
		HasMore: nextCursor != nil,
		Limit:   limit,
	}
	if nextCursor != nil {
		meta.Cursor = nextCursor.String()
	}
	apierr.WriteList(w, http.StatusOK, dtos, meta)
}

type deleteSuppressionRequest struct {
	Reason string `json:"reason,omitempty"`
}

// DeleteSuppression — DELETE /api/v1/alerts/suppressions/{id} (FIX-229 AC-1).
// Deletes the rule and (best-effort) restores any alerts whose
// meta.suppression_id matches the deleted rule.
func (h *Handler) DeleteSuppression(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	if h.suppressionStore == nil {
		apierr.WriteError(w, http.StatusServiceUnavailable, apierr.CodeServiceUnavailable, "alert suppressions unavailable")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid suppression ID")
		return
	}

	// Optional body — empty body is fine.
	var req deleteSuppressionRequest
	if r.Body != nil {
		if derr := json.NewDecoder(r.Body).Decode(&req); derr != nil && !errors.Is(derr, io.EOF) {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
			return
		}
	}
	if len(req.Reason) > suppressionReasonMaxLen {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("reason must be %d characters or fewer", suppressionReasonMaxLen))
		return
	}

	if delErr := h.suppressionStore.Delete(r.Context(), tenantID, id); delErr != nil {
		if errors.Is(delErr, store.ErrAlertSuppressionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeSuppressionNotFound, "alert suppression not found")
			return
		}
		h.logger.Error().Err(delErr).Msg("delete alert suppression")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	// Best-effort restore — failures are LOGGED only.
	var restoredCount int64
	if h.alertBackfill != nil {
		n, restErr := h.alertBackfill.RestoreSuppressedByMetaID(r.Context(), tenantID, id)
		if restErr != nil {
			h.logger.Warn().Err(restErr).Str("suppression_id", id.String()).Msg("restore suppressed alerts failed")
		} else {
			restoredCount = n
		}
	}

	auditAfter := map[string]interface{}{
		"deleted_id":     id.String(),
		"restored_count": restoredCount,
	}
	if req.Reason != "" {
		auditAfter["reason"] = req.Reason
	}
	audit.Emit(r, h.logger, h.auditSvc, "alert.suppression.deleted", "alert_suppression", id.String(), nil, auditAfter)

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"deleted_id":     id.String(),
		"restored_count": restoredCount,
	})
}
