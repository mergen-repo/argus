package admin

import (
	"encoding/json"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type killSwitchResponse struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	Enabled     bool       `json:"enabled"`
	Reason      *string    `json:"reason"`
	ToggledBy   *uuid.UUID `json:"toggled_by"`
	ToggledAt   interface{} `json:"toggled_at"`
}

func toKillSwitchResponse(ks store.KillSwitch) killSwitchResponse {
	var toggledAt interface{}
	if ks.ToggledAt != nil {
		toggledAt = ks.ToggledAt
	}
	return killSwitchResponse{
		Key:         ks.Key,
		Label:       ks.Label,
		Description: ks.Description,
		Enabled:     ks.Enabled,
		Reason:      ks.Reason,
		ToggledBy:   ks.ToggledBy,
		ToggledAt:   toggledAt,
	}
}

// ListKillSwitches GET /api/v1/admin/kill-switches (super_admin)
func (h *Handler) ListKillSwitches(w http.ResponseWriter, r *http.Request) {
	switches, err := h.ksStore.List(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("list kill switches")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]killSwitchResponse, 0, len(switches))
	for _, ks := range switches {
		items = append(items, toKillSwitchResponse(ks))
	}
	apierr.WriteSuccess(w, http.StatusOK, items)
}

type toggleKillSwitchRequest struct {
	Enabled bool    `json:"enabled"`
	Reason  string  `json:"reason"`
}

// ToggleKillSwitch PATCH /api/v1/admin/kill-switches/:key (super_admin)
func (h *Handler) ToggleKillSwitch(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Kill switch key is required")
		return
	}

	var req toggleKillSwitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.Enabled && req.Reason == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Reason is required when enabling a kill switch",
			[]map[string]string{{"field": "reason", "message": "Reason is required when enabling a kill switch", "code": "required"}})
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	actorID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	var actorPtr *uuid.UUID
	if actorID != uuid.Nil {
		actorPtr = &actorID
	}

	var reasonPtr *string
	if req.Reason != "" {
		reasonPtr = &req.Reason
	}

	ks, err := h.ksSvc.Toggle(r.Context(), key, req.Enabled, reasonPtr, actorPtr, tenantID)
	if err != nil {
		if err == store.ErrKillSwitchNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Kill switch not found")
			return
		}
		h.logger.Error().Err(err).Str("key", key).Msg("toggle kill switch")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toKillSwitchResponse(*ks))
}
