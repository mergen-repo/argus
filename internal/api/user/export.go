package user

import (
	"net/http"

	"github.com/btopcu/argus/internal/export"
)

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filters := map[string]string{}
	if v := q.Get("role"); v != "" {
		filters["role"] = v
	}
	if v := q.Get("state"); v != "" {
		filters["state"] = v
	}
	filename := export.BuildFilename("users", filters)
	header := []string{"id", "email", "name", "role", "state", "created_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			users, next, err := h.userStore.ListByTenant(r.Context(), cursor, 500, q.Get("role"), q.Get("state"))
			if err != nil {
				h.logger.Error().Err(err).Msg("export users")
				return
			}
			for _, u := range users {
				if !yield([]string{
					u.ID.String(), u.Email, u.Name, u.Role, u.State,
					u.CreatedAt.Format("2006-01-02T15:04:05Z"),
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
