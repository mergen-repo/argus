package job

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/bus"
	jobtypes "github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	CodeJobNotFound       = "JOB_NOT_FOUND"
	CodeJobAlreadyRunning = "JOB_ALREADY_RUNNING"
	CodeJobCancelled      = "JOB_CANCELLED"
)

type Handler struct {
	jobs     *store.JobStore
	eventBus *bus.EventBus
	logger   zerolog.Logger
}

func NewHandler(jobs *store.JobStore, eventBus *bus.EventBus, logger zerolog.Logger) *Handler {
	return &Handler{
		jobs:     jobs,
		eventBus: eventBus,
		logger:   logger,
	}
}

type jobDTO struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       uuid.UUID       `json:"tenant_id"`
	Type           string          `json:"type"`
	State          string          `json:"state"`
	Priority       int             `json:"priority"`
	TotalItems     int             `json:"total_items"`
	ProcessedItems int             `json:"processed_items"`
	FailedItems    int             `json:"failed_items"`
	ProgressPct    float64         `json:"progress_pct"`
	ErrorReport    json.RawMessage `json:"error_report,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	MaxRetries     int             `json:"max_retries"`
	RetryCount     int             `json:"retry_count"`
	StartedAt      *string         `json:"started_at,omitempty"`
	CompletedAt    *string         `json:"completed_at,omitempty"`
	CreatedAt      string          `json:"created_at"`
	CreatedBy      *string         `json:"created_by,omitempty"`
}

const timeFmt = "2006-01-02T15:04:05Z07:00"

func toJobDTO(j *store.Job) jobDTO {
	dto := jobDTO{
		ID:             j.ID,
		TenantID:       j.TenantID,
		Type:           j.Type,
		State:          j.State,
		Priority:       j.Priority,
		TotalItems:     j.TotalItems,
		ProcessedItems: j.ProcessedItems,
		FailedItems:    j.FailedItems,
		ProgressPct:    j.ProgressPct,
		ErrorReport:    j.ErrorReport,
		Result:         j.Result,
		MaxRetries:     j.MaxRetries,
		RetryCount:     j.RetryCount,
		CreatedAt:      j.CreatedAt.Format(timeFmt),
	}
	if j.StartedAt != nil {
		v := j.StartedAt.Format(timeFmt)
		dto.StartedAt = &v
	}
	if j.CompletedAt != nil {
		v := j.CompletedAt.Format(timeFmt)
		dto.CompletedAt = &v
	}
	if j.CreatedBy != nil {
		v := j.CreatedBy.String()
		dto.CreatedBy = &v
	}
	return dto
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")

	limit := 20
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	filter := store.JobListFilter{
		Type:  r.URL.Query().Get("type"),
		State: r.URL.Query().Get("state"),
	}

	results, nextCursor, err := h.jobs.List(r.Context(), cursor, limit, filter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list jobs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]jobDTO, 0, len(results))
	for i := range results {
		items = append(items, toJobDTO(&results[i]))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor: nextCursor,
		Limit:  limit,
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid job ID")
		return
	}

	j, err := h.jobs.GetByID(r.Context(), id)
	if err != nil {
		if err == store.ErrJobNotFound {
			apierr.WriteError(w, http.StatusNotFound, CodeJobNotFound, "Job not found")
			return
		}
		h.logger.Error().Err(err).Msg("get job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toJobDTO(j))
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid job ID")
		return
	}

	if err := h.jobs.Cancel(r.Context(), id); err != nil {
		if err == store.ErrJobNotFound {
			apierr.WriteError(w, http.StatusNotFound, CodeJobNotFound, "Job not found")
			return
		}
		if err == store.ErrJobAlreadyRunning {
			apierr.WriteError(w, http.StatusConflict, CodeJobAlreadyRunning, "Job is currently running and cannot be cancelled")
			return
		}
		h.logger.Error().Err(err).Msg("cancel job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid job ID")
		return
	}

	j, err := h.jobs.GetByID(r.Context(), id)
	if err != nil {
		if err == store.ErrJobNotFound {
			apierr.WriteError(w, http.StatusNotFound, CodeJobNotFound, "Job not found")
			return
		}
		h.logger.Error().Err(err).Msg("get job for retry")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if j.State == "running" {
		apierr.WriteError(w, http.StatusConflict, CodeJobAlreadyRunning,
			"Job is already running",
			[]map[string]interface{}{{
				"job_id":       j.ID.String(),
				"state":        j.State,
				"progress_pct": j.ProgressPct,
			}})
		return
	}

	if j.State == "cancelled" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, CodeJobCancelled,
			"Job has been cancelled and cannot be retried")
		return
	}

	if j.State != "failed" && j.State != "completed" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("Job in state '%s' cannot be retried", j.State))
		return
	}

	if err := h.jobs.SetRetryPending(r.Context(), j.ID); err != nil {
		h.logger.Error().Err(err).Msg("set retry pending")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, jobtypes.JobMessage{
		JobID:    j.ID,
		TenantID: j.TenantID,
		Type:     j.Type,
	})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: map[string]string{
			"job_id": j.ID.String(),
			"status": "retry_pending",
		},
	})
}

func (h *Handler) ErrorReport(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid job ID")
		return
	}

	report, err := h.jobs.GetErrorReport(r.Context(), id)
	if err != nil {
		if err == store.ErrJobNotFound {
			apierr.WriteError(w, http.StatusNotFound, CodeJobNotFound, "Job not found")
			return
		}
		h.logger.Error().Err(err).Msg("get error report")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if report == nil || string(report) == "null" {
		apierr.WriteSuccess(w, http.StatusOK, []interface{}{})
		return
	}

	format := r.URL.Query().Get("format")
	if format == "csv" {
		h.writeErrorReportCSV(w, report)
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, json.RawMessage(report))
}

func (h *Handler) writeErrorReportCSV(w http.ResponseWriter, report json.RawMessage) {
	var errors []jobtypes.ImportRowError
	if err := json.Unmarshal(report, &errors); err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to parse error report")
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=error_report.csv")
	w.WriteHeader(http.StatusOK)

	writer := csv.NewWriter(w)
	writer.Write([]string{"row", "iccid", "error"})
	for _, e := range errors {
		writer.Write([]string{
			strconv.Itoa(e.Row),
			e.ICCID,
			e.ErrorMessage,
		})
	}
	writer.Flush()
}
