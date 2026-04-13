package job

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/export"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	_, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	filters := map[string]string{}
	if v := q.Get("state"); v != "" {
		filters["state"] = v
	}
	if v := q.Get("type"); v != "" {
		filters["type"] = v
	}
	filename := export.BuildFilename("jobs", filters)
	header := []string{"id", "type", "state", "priority", "processed_items", "total_items", "failed_items", "progress_pct", "created_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			filter := store.JobListFilter{
				Type:  q.Get("type"),
				State: q.Get("state"),
			}
			jobs, next, err := h.jobs.List(r.Context(), cursor, 500, filter)
			if err != nil {
				h.logger.Error().Err(err).Msg("export jobs")
				return
			}
			for _, j := range jobs {
				if !yield([]string{
					j.ID.String(), j.Type, j.State,
					strconv.Itoa(j.Priority),
					strconv.Itoa(j.ProcessedItems),
					strconv.Itoa(j.TotalItems),
					strconv.Itoa(j.FailedItems),
					fmt.Sprintf("%.2f", j.ProgressPct),
					j.CreatedAt.Format("2006-01-02T15:04:05Z"),
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
