package session

import (
	"net/http"
	"strconv"

	aaa "github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/export"
	"github.com/google/uuid"
)

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := ""
	if tid, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID); ok && tid != uuid.Nil {
		tenantIDStr = tid.String()
	}

	q := r.URL.Query()
	filters := map[string]string{}
	for _, key := range []string{"operator_id", "apn_id", "session_state"} {
		if v := q.Get(key); v != "" {
			filters[key] = v
		}
	}
	if v := q.Get("from"); v != "" {
		filters["from"] = v
	}
	if v := q.Get("to"); v != "" {
		filters["to"] = v
	}

	filename := export.BuildFilename("sessions", filters)
	header := []string{"id", "sim_id", "imsi", "operator_id", "apn_id", "rat_type", "session_state", "started_at", "ended_at", "bytes_in", "bytes_out", "framed_ip"}

	filter := aaa.SessionFilter{
		TenantID:   tenantIDStr,
		OperatorID: q.Get("operator_id"),
		APNID:      q.Get("apn_id"),
	}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			sessions, next, err := h.sessionMgr.ListActive(r.Context(), cursor, 500, filter)
			if err != nil {
				h.logger.Error().Err(err).Msg("export sessions")
				return
			}
			for _, s := range sessions {
				endedAt := ""
				if !s.EndedAt.IsZero() {
					endedAt = s.EndedAt.Format("2006-01-02T15:04:05Z")
				}
				if !yield([]string{
					s.ID, s.SimID, s.IMSI,
					s.OperatorID, s.APNID, s.RATType,
					s.SessionState,
					s.StartedAt.Format("2006-01-02T15:04:05Z"),
					endedAt,
					strconv.FormatUint(s.BytesIn, 10),
					strconv.FormatUint(s.BytesOut, 10),
					s.FramedIP,
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
