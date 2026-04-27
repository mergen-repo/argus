package reports

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var cronFieldRe = regexp.MustCompile(`^[0-9/*,\-]+$`)

// FIX-248 DEV-560: scope reduction. KVKK / GDPR / BTK (regulatory reports
// removed alongside compliance page deprecation per DEV-254) and
// CostAnalysis (cost page deprecated) are no longer accepted by the
// generate / scheduled endpoints. Underlying builder Go code is parked
// in tree until D-166 deletes it atomically with the 5 new builders
// added under D-165 — one git commit per builder add/remove rather than
// two passes through the codebase.
var validReportTypes = map[string]bool{
	string(report.ReportSLAMonthly):   true,
	string(report.ReportUsageSummary): true,
	string(report.ReportAuditExport):  true,
	string(report.ReportSIMInventory): true,
}

var validFormats = map[string]bool{
	string(report.FormatPDF):  true,
	string(report.FormatCSV):  true,
	string(report.FormatXLSX): true,
}

// ScheduledReportStore is the interface this handler depends on.
type ScheduledReportStore interface {
	Create(ctx context.Context, tenantID uuid.UUID, createdBy *uuid.UUID, reportType, scheduleCron, format string, recipients []string, filters json.RawMessage, nextRunAt time.Time) (*store.ScheduledReport, error)
	GetByID(ctx context.Context, id uuid.UUID) (*store.ScheduledReport, error)
	List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int) ([]*store.ScheduledReport, string, error)
	Update(ctx context.Context, id uuid.UUID, patch store.ScheduledReportPatch) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// JobEnqueuer is the interface for creating and publishing jobs.
type JobEnqueuer interface {
	CreateWithTenantID(ctx context.Context, tenantID uuid.UUID, p store.CreateJobParams) (*store.Job, error)
}

// EventPublisher is the interface for publishing job messages.
type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type Handler struct {
	store    ScheduledReportStore
	jobs     JobEnqueuer
	eventBus EventPublisher
	auditSvc audit.Auditor
	logger   zerolog.Logger
}

func NewHandler(s ScheduledReportStore, jobs JobEnqueuer, eventBus EventPublisher, auditSvc audit.Auditor, logger zerolog.Logger) *Handler {
	return &Handler{
		store:    s,
		jobs:     jobs,
		eventBus: eventBus,
		auditSvc: auditSvc,
		logger:   logger.With().Str("component", "reports_handler").Logger(),
	}
}

// generateRequest is the request body for POST /api/v1/reports/generate.
type generateRequest struct {
	ReportType string         `json:"report_type"`
	Format     string         `json:"format"`
	Filters    map[string]any `json:"filters"`
}

// scheduledReportCreateRequest is the request body for POST /api/v1/reports/scheduled.
type scheduledReportCreateRequest struct {
	ReportType   string         `json:"report_type"`
	ScheduleCron string         `json:"schedule_cron"`
	Format       string         `json:"format"`
	Recipients   []string       `json:"recipients"`
	Filters      map[string]any `json:"filters"`
}

// scheduledReportPatchRequest is the request body for PATCH /api/v1/reports/scheduled/:id.
type scheduledReportPatchRequest struct {
	ScheduleCron *string        `json:"schedule_cron"`
	Recipients   *[]string      `json:"recipients"`
	Filters      map[string]any `json:"filters"`
	State        *string        `json:"state"`
	Format       *string        `json:"format"`
}

// Generate handles POST /api/v1/reports/generate.
// Always enqueues an async job and returns 202 with {job_id, status:"queued"}.
// Deviation from spec: sync path not implemented; all requests are async regardless of
// format or report_type, keeping the handler simple and avoiding streaming complexity.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := tenantAndUser(r)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
		return
	}

	if !validReportTypes[req.ReportType] {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"report_type must be one of: compliance_kvkk, compliance_gdpr, compliance_btk, sla_monthly, usage_summary, cost_analysis, audit_log_export, sim_inventory")
		return
	}
	if !validFormats[req.Format] {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"format must be one of: pdf, csv, xlsx")
		return
	}

	payload, err := json.Marshal(map[string]any{
		"report_type":  req.ReportType,
		"format":       req.Format,
		"filters":      req.Filters,
		"tenant_id":    tenantID.String(),
		"requested_by": userID.String(),
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("marshal generate payload")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to enqueue report")
		return
	}

	j, err := h.jobs.CreateWithTenantID(r.Context(), tenantID, store.CreateJobParams{
		Type:      job.JobTypeScheduledReportRun,
		Priority:  5,
		Payload:   payload,
		CreatedBy: &userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create report job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to enqueue report")
		return
	}

	if h.eventBus != nil {
		_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
			JobID:    j.ID,
			TenantID: j.TenantID,
			Type:     job.JobTypeScheduledReportRun,
		})
	}

	audit.Emit(r, h.logger, h.auditSvc, "report.generated", "report_job", j.ID.String(), nil, map[string]interface{}{
		"report_type": req.ReportType,
		"format":      req.Format,
		"async":       true,
	})
	apierr.WriteSuccess(w, http.StatusAccepted, map[string]string{
		"job_id": j.ID.String(),
		"status": "queued",
	})
}

// ListScheduled handles GET /api/v1/reports/scheduled.
func (h *Handler) ListScheduled(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := tenantAndUser(r)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	cursor := q.Get("cursor")
	limit := 20
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'limit' parameter")
			return
		}
		if n > 100 {
			n = 100
		}
		limit = n
	}

	rows, nextCursor, err := h.store.List(r.Context(), tenantID, cursor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list scheduled reports")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to list scheduled reports")
		return
	}

	apierr.WriteList(w, http.StatusOK, rows, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

// CreateScheduled handles POST /api/v1/reports/scheduled.
func (h *Handler) CreateScheduled(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := tenantAndUser(r)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req scheduledReportCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
		return
	}

	if !validReportTypes[req.ReportType] {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"report_type must be one of: compliance_kvkk, compliance_gdpr, compliance_btk, sla_monthly, usage_summary, cost_analysis, audit_log_export, sim_inventory")
		return
	}
	if !validFormats[req.Format] {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"format must be one of: pdf, csv, xlsx")
		return
	}
	if !isValidCron(req.ScheduleCron) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"schedule_cron must be a valid cron expression (e.g. '0 9 * * 1') or @daily, @hourly, @weekly, @monthly")
		return
	}

	nextRun, err := job.NextRunAfter(req.ScheduleCron, time.Now().UTC())
	if err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"schedule_cron does not produce a valid next run time within 365 days")
		return
	}

	var filtersRaw json.RawMessage
	if req.Filters != nil {
		filtersRaw, _ = json.Marshal(req.Filters)
	}

	row, err := h.store.Create(r.Context(), tenantID, &userID, req.ReportType, req.ScheduleCron, req.Format, req.Recipients, filtersRaw, nextRun)
	if err != nil {
		h.logger.Error().Err(err).Msg("create scheduled report")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to create scheduled report")
		return
	}

	audit.Emit(r, h.logger, h.auditSvc, "scheduled_report.created", "scheduled_report", row.ID.String(), nil, map[string]interface{}{
		"report_type":   row.ReportType,
		"format":        row.Format,
		"schedule_cron": row.ScheduleCron,
		"recipients":    row.Recipients,
	})
	apierr.WriteSuccess(w, http.StatusCreated, row)
}

// PatchScheduled handles PATCH /api/v1/reports/scheduled/:id.
func (h *Handler) PatchScheduled(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := tenantAndUser(r)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid scheduled report ID")
		return
	}

	existing, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		if err == store.ErrScheduledReportNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "scheduled report not found")
			return
		}
		h.logger.Error().Err(err).Str("id", id.String()).Msg("get scheduled report for patch")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to fetch scheduled report")
		return
	}
	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "scheduled report not found")
		return
	}

	var req scheduledReportPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
		return
	}

	if req.State != nil && *req.State != "active" && *req.State != "paused" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"state must be 'active' or 'paused'")
		return
	}
	if req.Format != nil && !validFormats[*req.Format] {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"format must be one of: pdf, csv, xlsx")
		return
	}

	patch := store.ScheduledReportPatch{
		Recipients: req.Recipients,
		State:      req.State,
		Format:     req.Format,
	}

	if req.Filters != nil {
		patch.Filters, _ = json.Marshal(req.Filters)
	}

	if req.ScheduleCron != nil {
		if !isValidCron(*req.ScheduleCron) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				"schedule_cron must be a valid cron expression")
			return
		}
		nextRun, err := job.NextRunAfter(*req.ScheduleCron, time.Now().UTC())
		if err != nil {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				"schedule_cron does not produce a valid next run time within 365 days")
			return
		}
		patch.ScheduleCron = req.ScheduleCron
		patch.NextRunAt = &nextRun
	}

	if err := h.store.Update(r.Context(), id, patch); err != nil {
		if err == store.ErrScheduledReportNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "scheduled report not found")
			return
		}
		h.logger.Error().Err(err).Str("id", id.String()).Msg("update scheduled report")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to update scheduled report")
		return
	}

	updated, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Str("id", id.String()).Msg("fetch updated scheduled report")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to fetch updated scheduled report")
		return
	}

	audit.Emit(r, h.logger, h.auditSvc, "scheduled_report.updated", "scheduled_report", id.String(), existing, updated)
	apierr.WriteSuccess(w, http.StatusOK, updated)
}

// DeleteScheduled handles DELETE /api/v1/reports/scheduled/:id.
func (h *Handler) DeleteScheduled(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := tenantAndUser(r)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid scheduled report ID")
		return
	}

	existing, err := h.store.GetByID(r.Context(), id)
	if err != nil {
		if err == store.ErrScheduledReportNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "scheduled report not found")
			return
		}
		h.logger.Error().Err(err).Str("id", id.String()).Msg("get scheduled report for delete")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to fetch scheduled report")
		return
	}
	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "scheduled report not found")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if err == store.ErrScheduledReportNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "scheduled report not found")
			return
		}
		h.logger.Error().Err(err).Str("id", id.String()).Msg("delete scheduled report")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to delete scheduled report")
		return
	}

	audit.Emit(r, h.logger, h.auditSvc, "scheduled_report.deleted", "scheduled_report", id.String(), existing, nil)
	w.WriteHeader(http.StatusNoContent)
}

// tenantAndUser extracts tenant ID and user ID from request context.
func tenantAndUser(r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		return uuid.Nil, uuid.Nil, false
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	return tenantID, userID, true
}

// isValidCron returns true if the expression is a named alias or passes the
// 5-field regex check. Each field must match [0-9/*,-]+.
// This is a syntactic check; NextRunAfter provides semantic validation.
func isValidCron(expr string) bool {
	switch expr {
	case "@hourly", "@daily", "@weekly", "@monthly":
		return true
	}
	fields := splitFields(expr)
	if len(fields) != 5 {
		return false
	}
	for _, f := range fields {
		if !cronFieldRe.MatchString(f) {
			return false
		}
	}
	return true
}

func splitFields(expr string) []string {
	var fields []string
	start := -1
	for i, c := range expr {
		if c == ' ' || c == '\t' {
			if start >= 0 {
				fields = append(fields, expr[start:i])
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		fields = append(fields, expr[start:])
	}
	return fields
}

type reportDefinition struct {
	ID            string   `json:"id"`
	Category      string   `json:"category"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	FormatOptions []string `json:"format_options"`
}

// FIX-248 DEV-560: scope reduction. Compliance trio (BTK/KVKK/GDPR) and
// CostAnalysis removed. The 5 new operational reports (fleet_health,
// policy_rollout_audit, ip_pool_forecast, coa_enforcement, traffic_trend)
// are tracked under D-165 — when those builders ship, they'll be added
// here in the same commit.
var reportDefinitions = []reportDefinition{
	{
		ID:            string(report.ReportSLAMonthly),
		Category:      "operational",
		Name:          "Monthly SLA Report",
		Description:   "Operator uptime, latency, and incident summary for the selected month.",
		FormatOptions: []string{"pdf", "csv", "xlsx"},
	},
	{
		ID:            string(report.ReportUsageSummary),
		Category:      "analytics",
		Name:          "Usage Summary",
		Description:   "SIM activation, data consumption, and session counts aggregated by operator and APN.",
		FormatOptions: []string{"pdf", "csv", "xlsx"},
	},
	{
		ID:            string(report.ReportAuditExport),
		Category:      "security",
		Name:          "Audit Log Export",
		Description:   "Full audit trail export of administrative and user actions within the selected period.",
		FormatOptions: []string{"csv", "xlsx"},
	},
	{
		ID:            string(report.ReportSIMInventory),
		Category:      "operational",
		Name:          "SIM Inventory Report",
		Description:   "Complete SIM card inventory with state, operator assignment, APN, and metadata.",
		FormatOptions: []string{"csv", "xlsx"},
	},
}

// ListDefinitions handles GET /api/v1/reports/definitions.
func (h *Handler) ListDefinitions(w http.ResponseWriter, r *http.Request) {
	apierr.WriteSuccess(w, http.StatusOK, reportDefinitions)
}
