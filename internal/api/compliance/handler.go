package compliance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	compliancesvc "github.com/btopcu/argus/internal/compliance"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	complianceSvc *compliancesvc.Service
	tenantStore   *store.TenantStore
	logger        zerolog.Logger
}

func NewHandler(
	complianceSvc *compliancesvc.Service,
	tenantStore *store.TenantStore,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		complianceSvc: complianceSvc,
		tenantStore:   tenantStore,
		logger:        logger.With().Str("component", "compliance_handler").Logger(),
	}
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

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"retention_days": req.RetentionDays,
		"message":        "Retention period updated successfully",
	})
}
