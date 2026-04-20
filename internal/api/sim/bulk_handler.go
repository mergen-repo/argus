package sim

import (
	"context"
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

const maxBulkSimIDs = 10000

// killSwitchChecker allows the BulkHandler to check if bulk_operations is disabled.
type killSwitchChecker interface {
	IsEnabled(key string) bool
}

// jobCreator is the minimal surface of *store.JobStore the handler uses when
// creating bulk jobs. Extracted as an interface for unit-test substitution.
type jobCreator interface {
	Create(ctx context.Context, p store.CreateJobParams) (*store.Job, error)
}

// eventPublisher is the minimal surface of *bus.EventBus used by the handler.
type eventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

// simTenantFilter exposes the tenant-ownership check required for the
// sim_ids branch of bulk state change / policy assign / operator switch.
type simTenantFilter interface {
	FilterSIMIDsByTenant(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, []uuid.UUID, error)
}

// segmentSIMCounter is the minimal surface of *store.SegmentStore used by the
// bulk handlers to size segment-driven jobs.
type segmentSIMCounter interface {
	CountMatchingSIMs(ctx context.Context, segmentID uuid.UUID) (int64, error)
}

type BulkHandler struct {
	jobs       jobCreator
	segments   segmentSIMCounter
	sims       simTenantFilter
	eventBus   eventPublisher
	killSwitch killSwitchChecker
	logger     zerolog.Logger
}

func NewBulkHandler(jobs *store.JobStore, segments *store.SegmentStore, eventBus *bus.EventBus, logger zerolog.Logger) *BulkHandler {
	h := &BulkHandler{
		logger: logger,
	}
	if jobs != nil {
		h.jobs = jobs
	}
	if segments != nil {
		h.segments = segments
	}
	if eventBus != nil {
		h.eventBus = eventBus
	}
	return h
}

// SetKillSwitch attaches an optional kill-switch service.
func (h *BulkHandler) SetKillSwitch(ks killSwitchChecker) {
	h.killSwitch = ks
}

// SetSIMStore wires the SIM store used for tenant-ownership checks on the
// sim_ids branch of bulk endpoints. main.go MUST call this after construction.
func (h *BulkHandler) SetSIMStore(sims simTenantFilter) {
	h.sims = sims
}

func (h *BulkHandler) checkBulkKillSwitch(w http.ResponseWriter) bool {
	if h.killSwitch != nil && h.killSwitch.IsEnabled("bulk_operations") {
		apierr.WriteError(w, http.StatusServiceUnavailable, "SERVICE_DEGRADED",
			"Bulk operations are currently disabled. The system is in degraded mode.",
			map[string]string{"key": "bulk_operations"})
		return true
	}
	return false
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

// bulkSimIDsResponse is returned for the sim_ids branch where the caller
// supplied the authoritative ID set and we know the total up front.
type bulkSimIDsResponse struct {
	JobID     string `json:"job_id"`
	Status    string `json:"status"`
	TotalSIMs int    `json:"total_sims"`
}

var validBulkTargetStates = map[string]bool{
	"active":      true,
	"suspended":   true,
	"terminated":  true,
	"stolen_lost": true,
}

// bulkTargetSelection is the resolved output of validateSimIDsOrSegment.
// Exactly one of SimIDs / SegmentID is populated.
type bulkTargetSelection struct {
	SimIDs    []uuid.UUID
	SegmentID uuid.UUID
}

// UseSimIDs reports whether the caller provided the sim_ids array.
func (s bulkTargetSelection) UseSimIDs() bool {
	return len(s.SimIDs) > 0
}

// validateSimIDsOrSegment enforces the dual-shape rules for bulk endpoints
// that accept either a pre-resolved SIM ID array OR a segment reference.
// On failure it writes the 400 response and returns ok=false.
//
// Rules:
//   - Exactly one of sim_ids / segment_id must be provided.
//   - sim_ids must contain between 1 and maxBulkSimIDs entries.
//   - Each sim_ids entry must parse as a UUID; offending_indices reported on
//     any failure.
//
// Reused by StateChange, PolicyAssign (Task 4), OperatorSwitch (Task 4).
func validateSimIDsOrSegment(w http.ResponseWriter, rawSimIDs []string, segmentID uuid.UUID) (bulkTargetSelection, bool) {
	hasSimIDs := len(rawSimIDs) > 0
	hasSegment := segmentID != uuid.Nil

	if !hasSimIDs && !hasSegment {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"one of sim_ids or segment_id is required")
		return bulkTargetSelection{}, false
	}
	if hasSimIDs && hasSegment {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"sim_ids and segment_id are mutually exclusive")
		return bulkTargetSelection{}, false
	}

	if !hasSimIDs {
		return bulkTargetSelection{SegmentID: segmentID}, true
	}

	if len(rawSimIDs) > maxBulkSimIDs {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			fmt.Sprintf("sim_ids exceeds maximum of %d", maxBulkSimIDs))
		return bulkTargetSelection{}, false
	}

	parsed := make([]uuid.UUID, 0, len(rawSimIDs))
	var offending []int
	for i, raw := range rawSimIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil || id == uuid.Nil {
			offending = append(offending, i)
			continue
		}
		parsed = append(parsed, id)
	}
	if len(offending) > 0 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"invalid UUIDs in sim_ids",
			map[string]interface{}{"offending_indices": offending})
		return bulkTargetSelection{}, false
	}
	if len(parsed) == 0 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"sim_ids must contain at least 1 entry")
		return bulkTargetSelection{}, false
	}
	return bulkTargetSelection{SimIDs: parsed}, true
}

// resolveOwnedSimIDs runs the tenant-ownership check for a sim_ids selection.
// On any violation OR internal error it writes the response and returns ok=false.
func (h *BulkHandler) resolveOwnedSimIDs(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, bool) {
	if h.sims == nil {
		h.logger.Error().Msg("sim tenant filter not configured on BulkHandler")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"Bulk SIM ID validation is not configured")
		return nil, false
	}
	owned, violations, err := h.sims.FilterSIMIDsByTenant(r.Context(), tenantID, ids)
	if err != nil {
		h.logger.Error().Err(err).Msg("filter sim ids by tenant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"Failed to validate SIM ownership")
		return nil, false
	}
	if len(violations) > 0 {
		violationStrs := make([]string, len(violations))
		for i, v := range violations {
			violationStrs[i] = v.String()
		}
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbiddenCrossTenant,
			"One or more sim_ids do not belong to the caller's tenant",
			map[string]interface{}{"violations": violationStrs})
		return nil, false
	}
	return owned, true
}

func (h *BulkHandler) Import(w http.ResponseWriter, r *http.Request) {
	if h.checkBulkKillSwitch(w) {
		return
	}
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

	reserveStaticIP := r.FormValue("reserve_static_ip") == "true"

	payload, _ := json.Marshal(job.ImportPayload{
		CSVData:         string(csvData),
		FileName:        header.Filename,
		ReserveStaticIP: reserveStaticIP,
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
	if h.checkBulkKillSwitch(w) {
		return
	}
	var req struct {
		SimIDs      []string  `json:"sim_ids,omitempty"`
		SegmentID   uuid.UUID `json:"segment_id,omitempty"`
		TargetState string    `json:"target_state"`
		Reason      *string   `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
		return
	}

	if !validBulkTargetStates[req.TargetState] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			fmt.Sprintf("Invalid target_state: %q. Must be one of: active, suspended, terminated, stolen_lost", req.TargetState))
		return
	}

	selection, ok := validateSimIDsOrSegment(w, req.SimIDs, req.SegmentID)
	if !ok {
		return
	}

	if selection.UseSimIDs() {
		tenantID, err := store.TenantIDFromContext(r.Context())
		if err != nil {
			h.logger.Error().Err(err).Msg("resolve tenant for bulk state change")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
				"Failed to resolve tenant context")
			return
		}
		owned, ok := h.resolveOwnedSimIDs(w, r, tenantID, selection.SimIDs)
		if !ok {
			return
		}

		payload, _ := json.Marshal(job.BulkStateChangePayload{
			SimIDs:      owned,
			TargetState: req.TargetState,
			Reason:      req.Reason,
		})

		j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
			Type:       job.JobTypeBulkStateChange,
			Priority:   5,
			Payload:    payload,
			TotalItems: len(owned),
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
			Data: bulkSimIDsResponse{
				JobID:     j.ID.String(),
				Status:    "queued",
				TotalSIMs: len(owned),
			},
		})
		return
	}

	count, err := h.segments.CountMatchingSIMs(r.Context(), selection.SegmentID)
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
		"segment_id":   selection.SegmentID,
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
	if h.checkBulkKillSwitch(w) {
		return
	}
	var req struct {
		SimIDs          []string  `json:"sim_ids,omitempty"`
		SegmentID       uuid.UUID `json:"segment_id,omitempty"`
		PolicyVersionID uuid.UUID `json:"policy_version_id"`
		Reason          string    `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
		return
	}

	if req.PolicyVersionID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "policy_version_id is required")
		return
	}

	selection, ok := validateSimIDsOrSegment(w, req.SimIDs, req.SegmentID)
	if !ok {
		return
	}

	if selection.UseSimIDs() {
		tenantID, err := store.TenantIDFromContext(r.Context())
		if err != nil {
			h.logger.Error().Err(err).Msg("resolve tenant for bulk policy assign")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
				"Failed to resolve tenant context")
			return
		}
		owned, ok := h.resolveOwnedSimIDs(w, r, tenantID, selection.SimIDs)
		if !ok {
			return
		}

		payload, _ := json.Marshal(job.BulkPolicyAssignPayload{
			SimIDs:          owned,
			PolicyVersionID: req.PolicyVersionID,
			Reason:          req.Reason,
		})

		j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
			Type:       job.JobTypeBulkPolicyAssign,
			Priority:   5,
			Payload:    payload,
			TotalItems: len(owned),
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
			Data: bulkSimIDsResponse{
				JobID:     j.ID.String(),
				Status:    "queued",
				TotalSIMs: len(owned),
			},
		})
		return
	}

	count, err := h.segments.CountMatchingSIMs(r.Context(), selection.SegmentID)
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
		"segment_id":        selection.SegmentID,
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
	if h.checkBulkKillSwitch(w) {
		return
	}
	var req struct {
		SimIDs           []string  `json:"sim_ids,omitempty"`
		SegmentID        uuid.UUID `json:"segment_id,omitempty"`
		TargetOperatorID uuid.UUID `json:"target_operator_id"`
		TargetAPNID      uuid.UUID `json:"target_apn_id"`
		Reason           string    `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
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

	selection, ok := validateSimIDsOrSegment(w, req.SimIDs, req.SegmentID)
	if !ok {
		return
	}

	if selection.UseSimIDs() {
		tenantID, err := store.TenantIDFromContext(r.Context())
		if err != nil {
			h.logger.Error().Err(err).Msg("resolve tenant for bulk operator switch")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
				"Failed to resolve tenant context")
			return
		}
		owned, ok := h.resolveOwnedSimIDs(w, r, tenantID, selection.SimIDs)
		if !ok {
			return
		}

		payload, _ := json.Marshal(job.BulkEsimSwitchPayload{
			SimIDs:           owned,
			TargetOperatorID: req.TargetOperatorID,
			TargetAPNID:      req.TargetAPNID,
			Reason:           req.Reason,
		})

		j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
			Type:       job.JobTypeBulkEsimSwitch,
			Priority:   5,
			Payload:    payload,
			TotalItems: len(owned),
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
			Data: bulkSimIDsResponse{
				JobID:     j.ID.String(),
				Status:    "queued",
				TotalSIMs: len(owned),
			},
		})
		return
	}

	count, err := h.segments.CountMatchingSIMs(r.Context(), selection.SegmentID)
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
		"segment_id":         selection.SegmentID,
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
