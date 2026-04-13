package compliance

import (
	"encoding/json"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	jobtypes "github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// dataPortabilityPayload is the job payload schema for data_portability_export.
type dataPortabilityPayload struct {
	UserID          string `json:"user_id"`
	RequesterUserID string `json:"requester_user_id"`
	TenantID        string `json:"tenant_id"`
}

// RequestDataPortability handles POST /api/v1/compliance/data-portability/:user_id
//
// RBAC: tenant_admin+ (any user in tenant) OR self (caller's user_id == path user_id).
// Route is mounted under RequireRole("api_user") so unauthenticated requests never reach here;
// the admin-or-self check is enforced inside this handler.
func (h *Handler) RequestDataPortability(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	callerID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	callerRole, _ := r.Context().Value(apierr.RoleKey).(string)

	userIDStr := chi.URLParam(r, "user_id")
	targetUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid user_id format")
		return
	}

	isSelf := callerID != uuid.Nil && callerID == targetUserID
	isAdmin := apierr.HasRole(callerRole, "tenant_admin")
	if !isSelf && !isAdmin {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden,
			"Access denied: requires tenant_admin role or matching user_id")
		return
	}

	if h.jobStore == nil || h.eventBus == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"Data portability service not configured")
		return
	}

	payload, _ := json.Marshal(dataPortabilityPayload{
		UserID:          targetUserID.String(),
		RequesterUserID: callerID.String(),
		TenantID:        tenantID.String(),
	})

	var requester *uuid.UUID
	if callerID != uuid.Nil {
		cid := callerID
		requester = &cid
	}

	job, err := h.jobStore.CreateWithTenantID(r.Context(), tenantID, store.CreateJobParams{
		Type:      jobtypes.JobTypeDataPortabilityExport,
		Priority:  5,
		Payload:   payload,
		CreatedBy: requester,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userIDStr).Msg("create data portability job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"An unexpected error occurred")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, jobtypes.JobMessage{
		JobID:    job.ID,
		TenantID: tenantID,
		Type:     jobtypes.JobTypeDataPortabilityExport,
	})

	audit.Emit(r, h.logger, h.auditSvc, "data_portability.requested", "user", targetUserID.String(),
		nil, map[string]string{
			"job_id":            job.ID.String(),
			"requester_user_id": callerID.String(),
		})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: map[string]string{
			"job_id": job.ID.String(),
			"status": "queued",
		},
	})
}
