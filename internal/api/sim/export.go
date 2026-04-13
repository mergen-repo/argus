package sim

import (
	"fmt"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/export"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	filters := map[string]string{}
	for _, key := range []string{"state", "operator_id", "apn_id", "rat_type"} {
		if v := q.Get(key); v != "" {
			filters[key] = v
		}
	}
	filename := export.BuildFilename("sims", filters)

	header := []string{"iccid", "imsi", "msisdn", "state", "operator_id", "apn_id", "sim_type", "rat_type", "created_at"}

	params := store.ListSIMsParams{
		Limit:  500,
		State:  q.Get("state"),
		Q:      q.Get("q"),
	}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			params.Cursor = cursor
			sims, next, err := h.simStore.List(r.Context(), tenantID, params)
			if err != nil {
				h.logger.Error().Err(err).Msg("export sims: list")
				return
			}
			for _, s := range sims {
				msisdn := ""
				if s.MSISDN != nil {
					msisdn = *s.MSISDN
				}
				ratType := ""
				if s.RATType != nil {
					ratType = *s.RATType
				}
				apnID := ""
				if s.APNID != nil {
					apnID = s.APNID.String()
				}
				if !yield([]string{
					s.ICCID, s.IMSI, msisdn, s.State,
					s.OperatorID.String(), apnID,
					s.SimType, ratType,
					fmt.Sprintf("%s", s.CreatedAt.Format("2006-01-02T15:04:05Z")),
				}) {
					return
				}
			}
			if next == "" {
				break
			}
			cursor = next
		}
	})
}
