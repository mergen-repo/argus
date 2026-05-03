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
	// FIX-236 DEV-546: ad-hoc filter resolution for *ByFilter bulk endpoints.
	// Caller passes a ListSIMsParams subset; store returns matching ids capped
	// at `limit` plus the precise totalCount when the cap is hit (otherwise
	// totalCount = len(ids)).
	ListIDsByFilter(ctx context.Context, tenantID uuid.UUID, p store.ListSIMsParams, limit int) ([]uuid.UUID, int64, error)
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

// ─── FIX-236: filter-based bulk (ad-hoc, no saved Segment) ─────────────────
//
// The pre-FIX-236 bulk endpoints accept either an explicit `sim_ids[]` list
// or a `segment_id`. Filter-based bulk lets the user act on "every SIM
// matching the URL filter currently in the list view" without first creating
// a Segment. The handler resolves the filter server-side, hard-capped at
// `maxBulkByFilter`; if the matching count exceeds the cap, the request is
// rejected with the precise total so the user can narrow the filter or
// switch to a saved Segment.

const maxBulkByFilter = 10000
const previewSampleSize = 5

// bulkFilter is the URL-style subset accepted by *ByFilter handlers. Mirrors
// the SIM list filter surface; cursor + limit are intentionally absent.
type bulkFilter struct {
	State      string `json:"state,omitempty"`
	OperatorID string `json:"operator_id,omitempty"`
	APNID      string `json:"apn_id,omitempty"`
	RATType    string `json:"rat_type,omitempty"`
	IPAddress  string `json:"ip_address,omitempty"`
	ICCID      string `json:"iccid,omitempty"`
	IMSI       string `json:"imsi,omitempty"`
	MSISDN     string `json:"msisdn,omitempty"`
	Q          string `json:"q,omitempty"`
}

func (f bulkFilter) toListParams() (store.ListSIMsParams, error) {
	p := store.ListSIMsParams{
		ICCID:     f.ICCID,
		IMSI:      f.IMSI,
		MSISDN:    f.MSISDN,
		State:     f.State,
		RATType:   f.RATType,
		IPAddress: f.IPAddress,
		Q:         f.Q,
	}
	if f.OperatorID != "" {
		opID, err := uuid.Parse(f.OperatorID)
		if err != nil {
			return p, fmt.Errorf("operator_id: %w", err)
		}
		p.OperatorID = &opID
	}
	if f.APNID != "" {
		apnID, err := uuid.Parse(f.APNID)
		if err != nil {
			return p, fmt.Errorf("apn_id: %w", err)
		}
		p.APNID = &apnID
	}
	return p, nil
}

// resolveByFilter is the shared filter→ids path used by every *ByFilter
// handler. Returns the resolved ids on success; on cap-exceeded it writes the
// 422 response and returns ok=false so the caller can return immediately.
func (h *BulkHandler) resolveByFilter(w http.ResponseWriter, r *http.Request, filter bulkFilter, maxAffected int) (ids []uuid.UUID, totalCount int64, ok bool) {
	if h.sims == nil {
		h.logger.Error().Msg("sim tenant filter not configured for *ByFilter")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Bulk store not configured")
		return nil, 0, false
	}

	tenantID, err := store.TenantIDFromContext(r.Context())
	if err != nil {
		h.logger.Error().Err(err).Msg("resolve tenant for *ByFilter")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to resolve tenant context")
		return nil, 0, false
	}

	p, parseErr := filter.toListParams()
	if parseErr != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Invalid filter: "+parseErr.Error())
		return nil, 0, false
	}

	cap := maxAffected
	if cap <= 0 || cap > maxBulkByFilter {
		cap = maxBulkByFilter
	}

	ids, totalCount, err = h.sims.ListIDsByFilter(r.Context(), tenantID, p, cap)
	if err != nil {
		h.logger.Error().Err(err).Msg("resolve sim ids by filter")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to resolve filter")
		return nil, 0, false
	}

	if len(ids) == 0 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Filter matched zero SIMs")
		return nil, 0, false
	}

	if totalCount > int64(cap) {
		apierr.WriteJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
			"status": "error",
			"error": map[string]interface{}{
				"code":         "limit_exceeded",
				"message":      fmt.Sprintf("Filter matches %d SIMs but cap is %d. Narrow the filter or use a saved Segment.", totalCount, cap),
				"actual_count": totalCount,
				"cap":          cap,
			},
		})
		return nil, 0, false
	}

	return ids, totalCount, true
}

// PreviewCount resolves a filter and returns its size + a small sample of
// matching ids. Used by the FE to gate the destructive double-confirm dialog.
//
// POST /api/v1/sims/bulk/preview-count
// Body: {"filter": {...}, "max_affected": 10000}
// 200: {"count": N, "sample_ids": ["uuid1","uuid2",...]}
func (h *BulkHandler) PreviewCount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filter      bulkFilter `json:"filter"`
		MaxAffected int        `json:"max_affected,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
		return
	}

	tenantID, err := store.TenantIDFromContext(r.Context())
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to resolve tenant")
		return
	}
	p, parseErr := req.Filter.toListParams()
	if parseErr != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Invalid filter: "+parseErr.Error())
		return
	}

	cap := req.MaxAffected
	if cap <= 0 || cap > maxBulkByFilter {
		cap = maxBulkByFilter
	}
	ids, total, err := h.sims.ListIDsByFilter(r.Context(), tenantID, p, cap)
	if err != nil {
		h.logger.Error().Err(err).Msg("preview count")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to preview")
		return
	}

	sampleSize := previewSampleSize
	if len(ids) < sampleSize {
		sampleSize = len(ids)
	}
	sample := make([]string, 0, sampleSize)
	for i := 0; i < sampleSize; i++ {
		sample = append(sample, ids[i].String())
	}

	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
		Status: "success",
		Data: map[string]interface{}{
			"count":      total,
			"sample_ids": sample,
			"capped":     total > int64(cap),
			"cap":        cap,
		},
	})
}

// dispatchByFilterJob is the common tail for every *ByFilter handler:
// create the job (with the resolved ids materialised into the existing
// Bulk*Payload type), publish to the bus, and return the standard 202.
// Audit is emitted by the downstream worker; *ByFilter does not duplicate.
func (h *BulkHandler) dispatchByFilterJob(w http.ResponseWriter, r *http.Request, jobType string, payload []byte, totalItems int) {
	j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
		Type:       jobType,
		Priority:   5,
		Payload:    payload,
		TotalItems: totalItems,
		CreatedBy:  userIDFromRequest(r),
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create bulk by-filter job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to create job")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
		JobID:    j.ID,
		TenantID: j.TenantID,
		Type:     jobType,
	})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: bulkSimIDsResponse{
			JobID:     j.ID.String(),
			Status:    "queued",
			TotalSIMs: totalItems,
		},
	})
}

// StateChangeByFilter resolves the URL-style filter into a concrete sim_ids
// list (capped) and dispatches the same downstream pipeline as StateChange.
//
// POST /api/v1/sims/bulk/state-change-by-filter
// Body: {"filter": {...}, "target_state": "...", "reason": "...", "max_affected": 10000}
func (h *BulkHandler) StateChangeByFilter(w http.ResponseWriter, r *http.Request) {
	if h.checkBulkKillSwitch(w) {
		return
	}
	var req struct {
		Filter      bulkFilter `json:"filter"`
		TargetState string     `json:"target_state"`
		Reason      *string    `json:"reason,omitempty"`
		MaxAffected int        `json:"max_affected,omitempty"`
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

	ids, _, ok := h.resolveByFilter(w, r, req.Filter, req.MaxAffected)
	if !ok {
		return
	}

	payload, _ := json.Marshal(job.BulkStateChangePayload{
		SimIDs:      ids,
		TargetState: req.TargetState,
		Reason:      req.Reason,
	})
	h.dispatchByFilterJob(w, r, job.JobTypeBulkStateChange, payload, len(ids))
}

// PolicyAssignByFilter — POST /api/v1/sims/bulk/policy-assign-by-filter
func (h *BulkHandler) PolicyAssignByFilter(w http.ResponseWriter, r *http.Request) {
	if h.checkBulkKillSwitch(w) {
		return
	}
	var req struct {
		Filter          bulkFilter `json:"filter"`
		PolicyID        uuid.UUID  `json:"policy_id"`
		PolicyVersionID *uuid.UUID `json:"policy_version_id,omitempty"`
		Reason          *string    `json:"reason,omitempty"`
		MaxAffected     int        `json:"max_affected,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
		return
	}
	if req.PolicyID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "policy_id is required")
		return
	}

	ids, _, ok := h.resolveByFilter(w, r, req.Filter, req.MaxAffected)
	if !ok {
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"sim_ids":           ids,
		"policy_id":         req.PolicyID,
		"policy_version_id": req.PolicyVersionID,
		"reason":            req.Reason,
	})
	h.dispatchByFilterJob(w, r, job.JobTypeBulkPolicyAssign, payload, len(ids))
}

// OperatorSwitchByFilter — POST /api/v1/sims/bulk/operator-switch-by-filter
func (h *BulkHandler) OperatorSwitchByFilter(w http.ResponseWriter, r *http.Request) {
	if h.checkBulkKillSwitch(w) {
		return
	}
	var req struct {
		Filter         bulkFilter `json:"filter"`
		TargetOperator uuid.UUID  `json:"target_operator_id"`
		Reason         *string    `json:"reason,omitempty"`
		MaxAffected    int        `json:"max_affected,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid JSON body")
		return
	}
	if req.TargetOperator == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "target_operator_id is required")
		return
	}

	ids, _, ok := h.resolveByFilter(w, r, req.Filter, req.MaxAffected)
	if !ok {
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"sim_ids":            ids,
		"target_operator_id": req.TargetOperator,
		"reason":             req.Reason,
	})
	h.dispatchByFilterJob(w, r, job.JobTypeBulkEsimSwitch, payload, len(ids))
}

// DeviceBindingsCSV — POST /api/v1/sims/bulk/device-bindings
//
// Accepts a multipart CSV file with columns: iccid, bound_imei, binding_mode.
// Enqueues a bulk_device_bindings async job and returns 202 Accepted with the
// job_id. Per-row outcomes are available via GET /api/v1/jobs/{id}/errors.
func (h *BulkHandler) DeviceBindingsCSV(w http.ResponseWriter, r *http.Request) {
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

	normalized := make([]string, len(headers))
	for i, h := range headers {
		normalized[i] = strings.ToLower(strings.TrimSpace(h))
	}

	requiredCols := []string{"iccid", "bound_imei", "binding_mode"}
	colIndex := make(map[string]int, len(requiredCols))
	for _, col := range requiredCols {
		colIndex[col] = -1
	}
	for i, h := range normalized {
		if _, ok := colIndex[h]; ok {
			colIndex[h] = i
		}
	}
	var missingCols []string
	for _, col := range requiredCols {
		if colIndex[col] < 0 {
			missingCols = append(missingCols, col)
		}
	}
	if len(missingCols) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("Missing required CSV columns: %s", strings.Join(missingCols, ", ")),
			[]map[string]interface{}{{"missing_columns": missingCols}},
		)
		return
	}

	var rows []job.DeviceBindingsBulkRowSpec
	rowNum := 1
	for {
		rec, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				fmt.Sprintf("CSV parse error at row %d: %v", rowNum+1, readErr))
			return
		}
		rows = append(rows, job.DeviceBindingsBulkRowSpec{
			ICCID:       strings.TrimSpace(rec[colIndex["iccid"]]),
			BoundIMEI:   strings.TrimSpace(rec[colIndex["bound_imei"]]),
			BindingMode: strings.TrimSpace(rec[colIndex["binding_mode"]]),
		})
		rowNum++
		if len(rows) > maxBulkSimIDs {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				fmt.Sprintf("CSV exceeds maximum row limit of %d", maxBulkSimIDs))
			return
		}
	}

	if len(rows) == 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"CSV file contains no data rows")
		return
	}

	payload, _ := json.Marshal(job.BulkDeviceBindingsPayload{Rows: rows})

	j, err := h.jobs.Create(r.Context(), store.CreateJobParams{
		Type:       job.JobTypeBulkDeviceBindings,
		Priority:   5,
		Payload:    payload,
		TotalItems: len(rows),
		CreatedBy:  userIDFromRequest(r),
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create bulk device bindings job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"Failed to create job")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
		JobID:    j.ID,
		TenantID: j.TenantID,
		Type:     job.JobTypeBulkDeviceBindings,
	})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data:   map[string]string{"job_id": j.ID.String()},
	})
}
