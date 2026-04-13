package operator

import (
	"net/http"

	"github.com/btopcu/argus/internal/export"
)

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filters := map[string]string{}
	if v := q.Get("state"); v != "" {
		filters["state"] = v
	}
	filename := export.BuildFilename("operators", filters)
	header := []string{"id", "name", "code", "mcc", "mnc", "adapter_type", "health_status", "state", "created_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			operators, next, err := h.operatorStore.List(r.Context(), cursor, 500, q.Get("state"))
			if err != nil {
				h.logger.Error().Err(err).Msg("export operators")
				return
			}
			for _, o := range operators {
				if !yield([]string{
					o.ID.String(), o.Name, o.Code, o.MCC, o.MNC,
					o.AdapterType, o.HealthStatus, o.State,
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
