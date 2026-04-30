package operator

import (
	"net/http"
	"strings"

	"github.com/btopcu/argus/internal/export"
)

// ExportCSV streams the operators table as CSV. STORY-090 Wave 2
// D2-B replaces the legacy `adapter_type` column with
// `enabled_protocols` (comma-separated canonical list derived from
// the nested adapter_config). The derivation runs per row via the
// same helper TestConnection / List responses use so the CSV matches
// the JSON response shape.
func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filters := map[string]string{}
	if v := q.Get("state"); v != "" {
		filters["state"] = v
	}
	filename := export.BuildFilename("operators", filters)
	header := []string{"id", "name", "code", "mcc", "mnc", "enabled_protocols", "health_status", "state", "created_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			operators, next, err := h.operatorStore.List(r.Context(), cursor, 500, q.Get("state"))
			if err != nil {
				h.logger.Error().Err(err).Msg("export operators")
				return
			}
			for _, o := range operators {
				enabled := h.deriveEnabledProtocolsFromStored(&o)
				if !yield([]string{
					o.ID.String(), o.Name, o.Code, o.MCC, o.MNC,
					strings.Join(enabled, ","), o.HealthStatus, o.State,
					o.CreatedAt.Format("2006-01-02T15:04:05Z"),
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
