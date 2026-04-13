package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

const (
	dsarSLADays  = 30
	dsarSLAHours = dsarSLADays * 24
)

// dsarQueueItem mirrors FE DSARQueueItem (web/src/types/admin.ts).
type dsarQueueItem struct {
	JobID             uuid.UUID `json:"job_id"`
	Type              string    `json:"type"`
	SubjectID         string    `json:"subject_id"`
	UserEmail         string    `json:"user_email"`
	TenantID          uuid.UUID `json:"tenant_id"`
	Status            string    `json:"status"`
	SLAHours          int       `json:"sla_hours"`
	SLARemainingHours int       `json:"sla_remaining_hours"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func jobStateToDSARStatus(state string) string {
	switch state {
	case "queued", "pending":
		return "received"
	case "running":
		return "processing"
	case "succeeded":
		return "completed"
	default:
		return state
	}
}

// ListDSARQueue GET /api/v1/admin/dsar/queue (super_admin + tenant_admin)
func (h *Handler) ListDSARQueue(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
		limit = v
	}

	statusFilter := r.URL.Query().Get("status")

	role, _ := r.Context().Value(apierr.RoleKey).(string)
	callerTenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	var args []interface{}
	cond := "j.type IN ('data_portability_export','kvkk_purge_daily','sim_erasure')"
	argIdx := 1

	if role != "super_admin" {
		cond += " AND j.tenant_id = $1"
		args = append(args, callerTenantID)
		argIdx++
	}

	if statusFilter != "" {
		var states []string
		switch statusFilter {
		case "received":
			states = []string{"queued", "pending"}
		case "processing":
			states = []string{"running"}
		case "completed", "delivered":
			states = []string{"succeeded"}
		}
		if len(states) > 0 {
			cond += " AND j.state = ANY($" + strconv.Itoa(argIdx) + ")"
			args = append(args, states)
			argIdx++
		}
	}

	args = append(args, limit)
	// jobs table has no updated_at column — we fall back to COALESCE(started_at, created_at)
	// as a best-effort "last activity" marker for the DSAR queue UI.
	query := `
		SELECT j.id, j.type, j.tenant_id, j.state, j.created_at, j.completed_at,
			COALESCE(j.started_at, j.created_at) AS updated_at,
			COALESCE(u.email, '') AS user_email,
			COALESCE(j.created_by::text, '') AS subject_id
		FROM jobs j
		LEFT JOIN users u ON u.id = j.created_by
		WHERE ` + cond + `
		ORDER BY j.created_at DESC
		LIMIT $` + strconv.Itoa(argIdx)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		h.logger.Error().Err(err).Msg("list dsar queue")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	items := make([]dsarQueueItem, 0)
	for rows.Next() {
		var jobID, tenantID uuid.UUID
		var jobType, state, userEmail, subjectID string
		var createdAt, updatedAt time.Time
		var completedAt *time.Time

		if err := rows.Scan(&jobID, &jobType, &tenantID, &state, &createdAt, &completedAt, &updatedAt, &userEmail, &subjectID); err != nil {
			continue
		}

		remaining := dsarSLAHours - int(now.Sub(createdAt).Hours())
		if remaining < 0 {
			remaining = 0
		}

		items = append(items, dsarQueueItem{
			JobID:             jobID,
			Type:              jobType,
			SubjectID:         subjectID,
			UserEmail:         userEmail,
			TenantID:          tenantID,
			Status:            jobStateToDSARStatus(state),
			SLAHours:          dsarSLAHours,
			SLARemainingHours: remaining,
			CreatedAt:         createdAt,
			UpdatedAt:         updatedAt,
		})
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Limit:   limit,
		HasMore: len(items) == limit,
	})
}
