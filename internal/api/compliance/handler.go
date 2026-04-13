package compliance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	compliancesvc "github.com/btopcu/argus/internal/compliance"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// handlerJobEnqueuer is satisfied by *store.JobStore and by test fakes.
type handlerJobEnqueuer interface {
	CreateWithTenantID(ctx context.Context, tenantID uuid.UUID, p store.CreateJobParams) (*store.Job, error)
}

// handlerEventPublisher is satisfied by *bus.EventBus and by test fakes.
type handlerEventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type Handler struct {
	complianceSvc *compliancesvc.Service
	tenantStore   *store.TenantStore
	auditSvc      audit.Auditor
	jobStore      handlerJobEnqueuer
	eventBus      handlerEventPublisher
	logger        zerolog.Logger
}

func NewHandler(
	complianceSvc *compliancesvc.Service,
	tenantStore *store.TenantStore,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		complianceSvc: complianceSvc,
		tenantStore:   tenantStore,
		auditSvc:      auditSvc,
		logger:        logger.With().Str("component", "compliance_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type HandlerOption func(*Handler)

func WithJobStore(js *store.JobStore) HandlerOption {
	return func(h *Handler) { h.jobStore = js }
}

func WithEventBus(eb *bus.EventBus) HandlerOption {
	return func(h *Handler) { h.eventBus = eb }
}

func (h *Handler) setTestDeps(js handlerJobEnqueuer, eb handlerEventPublisher) {
	h.jobStore = js
	h.eventBus = eb
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	tenant, err := h.tenantStore.GetByID(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("get tenant for dashboard")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dashboard, err := h.complianceSvc.Dashboard(r.Context(), tenantID, tenant.PurgeRetentionDays)
	if err != nil {
		h.logger.Error().Err(err).Msg("get compliance dashboard")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, dashboard)
}

func (h *Handler) DataSubjectAccess(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	simIDStr := chi.URLParam(r, "simId")
	simID, err := uuid.Parse(simIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	export, err := h.complianceSvc.DataSubjectAccess(r.Context(), tenantID, simID)
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", simIDStr).Msg("data subject access")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dsar_%s_%s.json", simIDStr, time.Now().UTC().Format("20060102")))
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(export)
}

type erasureRequest struct {
	SimID string `json:"sim_id"`
}

func (h *Handler) RightToErasure(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	simIDStr := chi.URLParam(r, "simId")
	simID, err := uuid.Parse(simIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	if err := h.complianceSvc.RightToErasure(r.Context(), tenantID, simID); err != nil {
		h.logger.Error().Err(err).Str("sim_id", simIDStr).Msg("right to erasure")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	audit.Emit(r, h.logger, h.auditSvc, "sim.erasure", "sim", simID.String(), nil, map[string]string{"status": "purged"})

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"sim_id":  simID.String(),
		"status":  "purged",
		"message": "SIM data erased per GDPR/KVKK right to erasure",
	})
}

func (h *Handler) BTKReport(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "csv" {
		csvData, err := h.complianceSvc.ExportBTKReportCSV(r.Context(), tenantID)
		if err != nil {
			h.logger.Error().Err(err).Msg("btk report csv")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}

		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=btk_report_%s.csv", time.Now().UTC().Format("200601")))
		w.WriteHeader(http.StatusOK)
		w.Write(csvData)
		return
	}

	if format == "pdf" {
		pdfData, err := h.complianceSvc.ExportBTKReportPDF(r.Context(), tenantID)
		if err != nil {
			h.logger.Error().Err(err).Msg("btk report pdf")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=btk_report_%s.pdf", time.Now().UTC().Format("200601")))
		w.WriteHeader(http.StatusOK)
		w.Write(pdfData)
		return
	}

	report, err := h.complianceSvc.GenerateBTKReport(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("btk report json")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, report)
}

type updateRetentionRequest struct {
	RetentionDays int `json:"retention_days"`
}

func (h *Handler) UpdateRetention(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req updateRetentionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.RetentionDays < 30 || req.RetentionDays > 365 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"retention_days must be between 30 and 365")
		return
	}

	if err := h.complianceSvc.UpdateRetention(r.Context(), tenantID, req.RetentionDays); err != nil {
		h.logger.Error().Err(err).Msg("update retention")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	audit.Emit(r, h.logger, h.auditSvc, "retention.update", "compliance", tenantID.String(), nil, map[string]int{"retention_days": req.RetentionDays})

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"retention_days": req.RetentionDays,
		"message":        "Retention period updated successfully",
	})
}
