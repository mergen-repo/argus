package sim

import (
	"net/http"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

type simByIDDTO struct {
	ID      string `json:"id"`
	ICCID   string `json:"iccid"`
	IMSI    string `json:"imsi"`
	MSISDN  string `json:"msisdn,omitempty"`
	State   string `json:"state"`
	SimType string `json:"sim_type"`
}

// listByIDs is the batch-fetch branch for GET /api/v1/sims?ids=uuid1,uuid2,...
// Used by the CDR Explorer page to resolve SIM identifiers for the visible page
// without per-row round trips (FIX-214 D3).
func (h *Handler) listByIDs(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, idsParam string) {
	parts := strings.Split(idsParam, ",")
	if len(parts) == 0 {
		apierr.WriteList(w, http.StatusOK, []simByIDDTO{}, apierr.ListMeta{Limit: 0})
		return
	}
	if len(parts) > 200 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Too many ids (max 200 per request)")
		return
	}

	ids := make([]uuid.UUID, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := uuid.Parse(p)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid id in ids list")
			return
		}
		ids = append(ids, id)
	}

	sims, err := h.simStore.GetSIMsByIDs(r.Context(), tenantID, ids)
	if err != nil {
		h.logger.Error().Err(err).Msg("batch get sims by ids")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]simByIDDTO, 0, len(sims))
	for _, s := range sims {
		dto := simByIDDTO{
			ID:      s.ID.String(),
			ICCID:   s.ICCID,
			IMSI:    s.IMSI,
			State:   s.State,
			SimType: s.SimType,
		}
		if s.MSISDN != nil {
			dto.MSISDN = *s.MSISDN
		}
		items = append(items, dto)
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{Limit: len(items)})
}
