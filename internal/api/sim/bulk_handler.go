package sim

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const maxUploadSize = 50 << 20 // 50MB

type BulkHandler struct {
	jobs     *store.JobStore
	segments *store.SegmentStore
	eventBus *bus.EventBus
	logger   zerolog.Logger
}

func NewBulkHandler(jobs *store.JobStore, segments *store.SegmentStore, eventBus *bus.EventBus, logger zerolog.Logger) *BulkHandler {
	return &BulkHandler{
		jobs:     jobs,
		segments: segments,
		eventBus: eventBus,
		logger:   logger,
	}
}

type bulkImportResponse struct {
	JobID    string `json:"job_id"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
}

type bulkJobResponse struct {
	JobID          string `json:"job_id"`
	Status         string `json:"status"`
	EstimatedCount int64  `json:"estimated_count"`
}

var validBulkTargetStates = map[string]bool{
	"active":      true,
	"suspended":   true,
	"terminated":  true,
	"stolen_lost": true,
}

func (h *BulkHandler) Import(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"File too large or invalid multipart form. Max size: 50MB")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"Missing 'file' field in multipart form")
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"File must be a CSV file")
		return
	}

	csvData, err := io.ReadAll(file)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"Failed to read uploaded file")
		return
	}

	reader := csv.NewReader(strings.NewReader(string(csvData)))
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Invalid CSV: cannot read header row")
		return
	}

	requiredHeaders := []string{"iccid", "imsi", "msisdn", "operator_code", "apn_name"}
	normalized := make([]string, len(headers))
	for i, h := range headers {
		normalized[i] = strings.ToLower(strings.TrimSpace(h))
	}

	var missing []string
	for _, req := range requiredHeaders {
		found := false
		for _, h := range normalized {
			if h == req {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, req)
		}
	}

	if len(missing) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("Missing required CSV columns: %s", strings.Join(missing, ", ")),
			[]map[string]interface{}{{"missing_columns": missing}},
		)
		return
	}

	totalRows := 0
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				fmt.Sprintf("CSV parse error at row %d: %v", totalRows+2, err))
			return
		}
		totalRows++
	}

	if totalRows == 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"CSV file contains no data rows")
		return
	}

	payload, _ := json.Marshal(job.ImportPayload{
		CSVData:  string(csvData),
		FileName: header.Filename,
	})

	userID := userIDFromRequest(r)

	j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
		Type:       job.JobTypeBulkImport,
		Priority:   5,
		Payload:    payload,
		TotalItems: totalRows,
		CreatedBy:  userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create bulk import job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"Failed to create import job")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
		JobID:    j.ID,
		TenantID: j.TenantID,
		Type:     job.JobTypeBulkImport,
	})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: bulkImportResponse{
			JobID:    j.ID.String(),
			TenantID: j.TenantID.String(),
			Status:   "queued",
		},
	})
}

func (h *BulkHandler) StateChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SegmentID   uuid.UUID `json:"segment_id"`
		TargetState string    `json:"target_state"`
		Reason      *string   `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
		return
	}

	if req.SegmentID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "segment_id is required")
		return
	}
	if !validBulkTargetStates[req.TargetState] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			fmt.Sprintf("Invalid target_state: %q. Must be one of: active, suspended, terminated, stolen_lost", req.TargetState))
		return
	}

	count, err := h.segments.CountMatchingSIMs(r.Context(), req.SegmentID)
	if err != nil {
		if err == store.ErrNotFound {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeNotFound, "Segment not found")
			return
		}
		h.logger.Error().Err(err).Msg("count segment sims for state change")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to count segment SIMs")
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"segment_id":   req.SegmentID,
		"target_state": req.TargetState,
		"reason":       req.Reason,
	})

	j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
		Type:       job.JobTypeBulkStateChange,
		Priority:   5,
		Payload:    payload,
		TotalItems: int(count),
		CreatedBy:  userIDFromRequest(r),
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create bulk state change job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to create job")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
		JobID:    j.ID,
		TenantID: j.TenantID,
		Type:     job.JobTypeBulkStateChange,
	})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: bulkJobResponse{
			JobID:          j.ID.String(),
			Status:         "queued",
			EstimatedCount: count,
		},
	})
}

func (h *BulkHandler) PolicyAssign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SegmentID       uuid.UUID `json:"segment_id"`
		PolicyVersionID uuid.UUID `json:"policy_version_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
		return
	}

	if req.SegmentID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "segment_id is required")
		return
	}
	if req.PolicyVersionID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "policy_version_id is required")
		return
	}

	count, err := h.segments.CountMatchingSIMs(r.Context(), req.SegmentID)
	if err != nil {
		if err == store.ErrNotFound {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeNotFound, "Segment not found")
			return
		}
		h.logger.Error().Err(err).Msg("count segment sims for policy assign")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to count segment SIMs")
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"segment_id":        req.SegmentID,
		"policy_version_id": req.PolicyVersionID,
	})

	j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
		Type:       job.JobTypeBulkPolicyAssign,
		Priority:   5,
		Payload:    payload,
		TotalItems: int(count),
		CreatedBy:  userIDFromRequest(r),
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create bulk policy assign job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to create job")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
		JobID:    j.ID,
		TenantID: j.TenantID,
		Type:     job.JobTypeBulkPolicyAssign,
	})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: bulkJobResponse{
			JobID:          j.ID.String(),
			Status:         "queued",
			EstimatedCount: count,
		},
	})
}

func (h *BulkHandler) OperatorSwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SegmentID        uuid.UUID `json:"segment_id"`
		TargetOperatorID uuid.UUID `json:"target_operator_id"`
		TargetAPNID      uuid.UUID `json:"target_apn_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
		return
	}

	if req.SegmentID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "segment_id is required")
		return
	}
	if req.TargetOperatorID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "target_operator_id is required")
		return
	}
	if req.TargetAPNID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "target_apn_id is required")
		return
	}

	count, err := h.segments.CountMatchingSIMs(r.Context(), req.SegmentID)
	if err != nil {
		if err == store.ErrNotFound {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeNotFound, "Segment not found")
			return
		}
		h.logger.Error().Err(err).Msg("count segment sims for operator switch")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to count segment SIMs")
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"segment_id":         req.SegmentID,
		"target_operator_id": req.TargetOperatorID,
		"target_apn_id":      req.TargetAPNID,
	})

	j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
		Type:       job.JobTypeBulkEsimSwitch,
		Priority:   5,
		Payload:    payload,
		TotalItems: int(count),
		CreatedBy:  userIDFromRequest(r),
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create bulk operator switch job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to create job")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
		JobID:    j.ID,
		TenantID: j.TenantID,
		Type:     job.JobTypeBulkEsimSwitch,
	})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: bulkJobResponse{
			JobID:          j.ID.String(),
			Status:         "queued",
			EstimatedCount: count,
		},
	})
}

func userIDFromRequest(r *http.Request) *uuid.UUID {
	userIDStr, _ := r.Context().Value(apierr.UserIDKey).(string)
	if uid, err := uuid.Parse(userIDStr); err == nil {
		return &uid
	}
	return nil
}
