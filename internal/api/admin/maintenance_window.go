package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type maintenanceWindowResponse struct {
	ID               uuid.UUID   `json:"id"`
	TenantID         *uuid.UUID  `json:"tenant_id"`
	Title            string      `json:"title"`
	Description      string      `json:"description"`
	StartsAt         time.Time   `json:"starts_at"`
	EndsAt           time.Time   `json:"ends_at"`
	AffectedServices []string    `json:"affected_services"`
	CronExpression   *string     `json:"cron_expression"`
	NotifyPlan       interface{} `json:"notify_plan"`
	State            string      `json:"state"`
	CreatedBy        *uuid.UUID  `json:"created_by"`
	CreatedAt        time.Time   `json:"created_at"`
}

func toMaintenanceWindowResponse(mw store.MaintenanceWindow) maintenanceWindowResponse {
	var notifyPlan interface{}
	if len(mw.NotifyPlan) > 0 {
		_ = json.Unmarshal(mw.NotifyPlan, &notifyPlan)
	}
	if notifyPlan == nil {
		notifyPlan = map[string]interface{}{}
	}
	return maintenanceWindowResponse{
		ID:               mw.ID,
		TenantID:         mw.TenantID,
		Title:            mw.Title,
		Description:      mw.Description,
		StartsAt:         mw.StartsAt,
		EndsAt:           mw.EndsAt,
		AffectedServices: mw.AffectedServices,
		CronExpression:   mw.CronExpression,
		NotifyPlan:       notifyPlan,
		State:            mw.State,
		CreatedBy:        mw.CreatedBy,
		CreatedAt:        mw.CreatedAt,
	}
}

// ListMaintenanceWindows GET /api/v1/admin/maintenance-windows (super_admin + tenant_admin)
func (h *Handler) ListMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true"

	windows, err := h.mwStore.List(r.Context(), nil, activeOnly)
	if err != nil {
		h.logger.Error().Err(err).Msg("list maintenance windows")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]maintenanceWindowResponse, 0, len(windows))
	for _, mw := range windows {
		items = append(items, toMaintenanceWindowResponse(mw))
	}
	apierr.WriteSuccess(w, http.StatusOK, items)
}

type createMaintenanceWindowRequest struct {
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	StartsAt         time.Time       `json:"starts_at"`
	EndsAt           time.Time       `json:"ends_at"`
	AffectedServices []string        `json:"affected_services"`
	CronExpression   *string         `json:"cron_expression"`
	NotifyPlan       json.RawMessage `json:"notify_plan"`
}

// CreateMaintenanceWindow POST /api/v1/admin/maintenance-windows (super_admin)
func (h *Handler) CreateMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	var req createMaintenanceWindowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Title == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "title", "message": "Title is required", "code": "required"})
	}
	if req.Description == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "description", "message": "Description is required", "code": "required"})
	}
	if req.StartsAt.IsZero() {
		validationErrors = append(validationErrors, map[string]string{"field": "starts_at", "message": "starts_at is required", "code": "required"})
	}
	if req.EndsAt.IsZero() {
		validationErrors = append(validationErrors, map[string]string{"field": "ends_at", "message": "ends_at is required", "code": "required"})
	}
	if !req.StartsAt.IsZero() && !req.EndsAt.IsZero() && !req.EndsAt.After(req.StartsAt) {
		validationErrors = append(validationErrors, map[string]string{"field": "ends_at", "message": "ends_at must be after starts_at", "code": "invalid"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	actorID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	var actorPtr *uuid.UUID
	if actorID != uuid.Nil {
		actorPtr = &actorID
	}

	notifyPlan := []byte("{}")
	if len(req.NotifyPlan) > 0 {
		notifyPlan = req.NotifyPlan
	}

	mw, err := h.mwStore.Create(r.Context(), store.CreateMaintenanceWindowParams{
		Title:            req.Title,
		Description:      req.Description,
		StartsAt:         req.StartsAt,
		EndsAt:           req.EndsAt,
		AffectedServices: req.AffectedServices,
		CronExpression:   req.CronExpression,
		NotifyPlan:       notifyPlan,
		CreatedBy:        actorPtr,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create maintenance window")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.auditSvc != nil {
		tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
		afterData, _ := json.Marshal(map[string]interface{}{"id": mw.ID, "title": mw.Title})
		_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
			TenantID:   tenantID,
			UserID:     actorPtr,
			Action:     "maintenance.scheduled",
			EntityType: "maintenance_window",
			EntityID:   mw.ID.String(),
			AfterData:  afterData,
		})
	}

	apierr.WriteSuccess(w, http.StatusCreated, toMaintenanceWindowResponse(*mw))
}

// DeleteMaintenanceWindow DELETE /api/v1/admin/maintenance-windows/:id (super_admin)
func (h *Handler) DeleteMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid maintenance window ID")
		return
	}

	if err := h.mwStore.Delete(r.Context(), id); err != nil {
		if err == store.ErrMaintenanceWindowNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Maintenance window not found")
			return
		}
		h.logger.Error().Err(err).Str("id", idStr).Msg("delete maintenance window")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.auditSvc != nil {
		tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
		actorID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
		var actorPtr *uuid.UUID
		if actorID != uuid.Nil {
			actorPtr = &actorID
		}
		_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
			TenantID:   tenantID,
			UserID:     actorPtr,
			Action:     "maintenance.cancelled",
			EntityType: "maintenance_window",
			EntityID:   id.String(),
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
