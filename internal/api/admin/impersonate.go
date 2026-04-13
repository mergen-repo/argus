package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type impersonateResponse struct {
	JWT      string `json:"jwt"`
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	TenantID string `json:"tenant_id"`
	Role     string `json:"role"`
}

func (h *Handler) Impersonate(w http.ResponseWriter, r *http.Request) {
	if h.userStore == nil || h.jwtSecret == "" {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "impersonation not configured")
		return
	}

	adminID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || adminID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "auth context required")
		return
	}

	targetIDStr := chi.URLParam(r, "user_id")
	targetID, err := uuid.Parse(targetIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid user_id")
		return
	}

	target, err := h.userStore.GetByIDGlobal(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "user not found")
			return
		}
		h.logger.Error().Err(err).Str("target_id", targetIDStr).Msg("impersonate: get user")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to fetch target user")
		return
	}

	jwtStr, err := auth.GenerateImpersonationToken(h.jwtSecret, target.ID, target.TenantID, target.Role, adminID)
	if err != nil {
		h.logger.Error().Err(err).Msg("generate impersonation token")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to issue token")
		return
	}

	afterData, _ := json.Marshal(map[string]string{
		"target_user_id":   target.ID.String(),
		"target_tenant_id": target.TenantID.String(),
	})
	_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:   target.TenantID,
		UserID:     &adminID,
		Action:     "admin.impersonate",
		EntityType: "user",
		EntityID:   target.ID.String(),
		AfterData:  afterData,
	})

	apierr.WriteSuccess(w, http.StatusOK, impersonateResponse{
		JWT:      jwtStr,
		UserID:   target.ID.String(),
		Email:    target.Email,
		TenantID: target.TenantID.String(),
		Role:     target.Role,
	})
}

func (h *Handler) ImpersonateExit(w http.ResponseWriter, r *http.Request) {
	apierr.WriteSuccess(w, http.StatusOK, map[string]string{"message": "use original JWT to restore session"})
}
