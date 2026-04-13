package user

import (
	"encoding/json"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
)

type updatePrefsBody struct {
	Locale      *string         `json:"locale"`
	PageKey     *string         `json:"page_key"`
	ColumnsJSON json.RawMessage `json:"columns_json"`
}

func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := h.tenantUser(w, r)
	_ = tenantID

	if !ok {
		return
	}

	var body updatePrefsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
		return
	}

	if body.Locale != nil {
		if *body.Locale != "en" && *body.Locale != "tr" {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "locale must be 'en' or 'tr'")
			return
		}
		if err := h.userStore.UpdateLocale(r.Context(), userID, *body.Locale); err != nil {
			h.logger.Error().Err(err).Msg("update user locale")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to update locale")
			return
		}
	}

	if body.PageKey != nil && body.ColumnsJSON != nil && h.columnPrefStore != nil {
		if err := h.columnPrefStore.Upsert(r.Context(), userID, *body.PageKey, body.ColumnsJSON); err != nil {
			h.logger.Error().Err(err).Msg("upsert column prefs")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to update column preferences")
			return
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]bool{"updated": true})
}
