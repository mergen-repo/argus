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
	eventBus *bus.EventBus
	logger   zerolog.Logger
}

func NewBulkHandler(jobs *store.JobStore, eventBus *bus.EventBus, logger zerolog.Logger) *BulkHandler {
	return &BulkHandler{
		jobs:     jobs,
		eventBus: eventBus,
		logger:   logger,
	}
}

type bulkImportResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
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
			JobID:  j.ID.String(),
			Status: "queued",
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
