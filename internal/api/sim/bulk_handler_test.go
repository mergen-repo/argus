package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestBulkImportMissingFile(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.Import(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestBulkImportNonCSVFile(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "data.txt")
	part.Write([]byte("some data"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.Import(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkImportMissingColumns(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	csvContent := "iccid,imsi\n8990111234567890123,286010123456789\n"

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "import.csv")
	part.Write([]byte(csvContent))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.Import(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkStateChangeMissingSegmentID(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"target_state": "suspended",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkStateChangeInvalidState(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"segment_id":   uuid.New().String(),
		"target_state": "invalid_state",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkPolicyAssign_MissingPolicyVersionID_400(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"segment_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/policy-assign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "policy_editor")
	req = req.WithContext(ctx)

	h.PolicyAssign(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkOperatorSwitch_MissingTargetOperatorID_400(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"segment_id":    uuid.New().String(),
		"target_apn_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/operator-switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "target_operator_id") {
		t.Errorf("message = %q, want substring 'target_operator_id'", resp.Error.Message)
	}
}

func TestBulkStateChangeInvalidJSON(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// --- dual-shape bulk state change tests ---------------------------------

type fakeJobCreator struct {
	lastParams store.CreateJobParams
	created    *store.Job
	err        error
}

func (f *fakeJobCreator) Create(_ context.Context, p store.CreateJobParams) (*store.Job, error) {
	f.lastParams = p
	if f.err != nil {
		return nil, f.err
	}
	j := f.created
	if j == nil {
		j = &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: p.Type}
	}
	return j, nil
}

type fakeEventPublisher struct {
	subjects []string
}

func (f *fakeEventPublisher) Publish(_ context.Context, subject string, _ interface{}) error {
	f.subjects = append(f.subjects, subject)
	return nil
}

type fakeSimTenantFilter struct {
	tenantIDs map[uuid.UUID]bool
	err       error
}

func (f *fakeSimTenantFilter) FilterSIMIDsByTenant(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, []uuid.UUID, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	var owned, violations []uuid.UUID
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if f.tenantIDs[id] {
			owned = append(owned, id)
		} else {
			violations = append(violations, id)
		}
	}
	return owned, violations, nil
}

type fakeSegmentCounter struct {
	count int64
	err   error
}

func (f *fakeSegmentCounter) CountMatchingSIMs(_ context.Context, _ uuid.UUID) (int64, error) {
	return f.count, f.err
}

func newBulkHandlerWithFakes(jobs *fakeJobCreator, segments *fakeSegmentCounter, sims *fakeSimTenantFilter, publisher *fakeEventPublisher) *BulkHandler {
	h := &BulkHandler{
		jobs:     jobs,
		segments: segments,
		sims:     sims,
		eventBus: publisher,
		logger:   zerolog.Nop(),
	}
	return h
}

func newBulkStateChangeRequest(t *testing.T, body interface{}, tenantID uuid.UUID) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	return req.WithContext(ctx)
}

func decodeErrorResponse(t *testing.T, body *bytes.Buffer) apierr.ErrorResponse {
	t.Helper()
	var resp apierr.ErrorResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return resp
}

func TestBulkStateChange_SimIdsArray_Accepted_202(t *testing.T) {
	tenantID := uuid.New()
	sim1, sim2 := uuid.New(), uuid.New()

	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID, Type: "bulk_state_change"}}
	sims := &fakeSimTenantFilter{tenantIDs: map[uuid.UUID]bool{sim1: true, sim2: true}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, publisher)

	req := newBulkStateChangeRequest(t, map[string]interface{}{
		"sim_ids":      []string{sim1.String(), sim2.String()},
		"target_state": "suspended",
		"reason":       "maintenance",
	}, tenantID)
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if jobs.lastParams.TotalItems != 2 {
		t.Errorf("TotalItems = %d, want 2", jobs.lastParams.TotalItems)
	}
	var payload job.BulkStateChangePayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.SimIDs) != 2 {
		t.Errorf("payload.SimIDs len = %d, want 2", len(payload.SimIDs))
	}
	if payload.SegmentID != uuid.Nil {
		t.Errorf("payload.SegmentID = %s, want zero", payload.SegmentID)
	}
	if payload.TargetState != "suspended" {
		t.Errorf("payload.TargetState = %q, want %q", payload.TargetState, "suspended")
	}
	if len(publisher.subjects) != 1 {
		t.Errorf("publisher.subjects len = %d, want 1", len(publisher.subjects))
	}

	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode success: %v", err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["status"] != "queued" {
		t.Errorf("data.status = %v, want queued", data["status"])
	}
	if total, ok := data["total_sims"].(float64); !ok || int(total) != 2 {
		t.Errorf("data.total_sims = %v, want 2", data["total_sims"])
	}
}

func TestBulkStateChange_SegmentId_Accepted_202(t *testing.T) {
	tenantID := uuid.New()

	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID, Type: "bulk_state_change"}}
	segments := &fakeSegmentCounter{count: 42}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, segments, nil, publisher)

	req := newBulkStateChangeRequest(t, map[string]interface{}{
		"segment_id":   uuid.New().String(),
		"target_state": "active",
	}, tenantID)
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if jobs.lastParams.TotalItems != 42 {
		t.Errorf("TotalItems = %d, want 42", jobs.lastParams.TotalItems)
	}
	if len(publisher.subjects) != 1 {
		t.Errorf("publisher.subjects len = %d, want 1", len(publisher.subjects))
	}
}

func TestBulkStateChange_BothProvided_400_MutualExclusion(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkStateChangeRequest(t, map[string]interface{}{
		"sim_ids":      []string{uuid.New().String()},
		"segment_id":   uuid.New().String(),
		"target_state": "suspended",
	}, uuid.New())
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "mutually exclusive") {
		t.Errorf("message = %q, want substring 'mutually exclusive'", resp.Error.Message)
	}
}

func TestBulkStateChange_NeitherProvided_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkStateChangeRequest(t, map[string]interface{}{
		"target_state": "suspended",
	}, uuid.New())
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "one of sim_ids or segment_id") {
		t.Errorf("message = %q, want substring about required fields", resp.Error.Message)
	}
}

func TestBulkStateChange_EmptyArray_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkStateChangeRequest(t, map[string]interface{}{
		"sim_ids":      []string{},
		"target_state": "suspended",
	}, uuid.New())
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkStateChange_ArrayOverLimit_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	ids := make([]string, maxBulkSimIDs+1)
	for i := range ids {
		ids[i] = uuid.New().String()
	}

	req := newBulkStateChangeRequest(t, map[string]interface{}{
		"sim_ids":      ids,
		"target_state": "suspended",
	}, uuid.New())
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "exceeds maximum") {
		t.Errorf("message = %q, want substring 'exceeds maximum'", resp.Error.Message)
	}
}

func TestBulkStateChange_InvalidUUIDInArray_400_OffendingIndices(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkStateChangeRequest(t, map[string]interface{}{
		"sim_ids": []string{
			uuid.New().String(),
			"not-a-uuid",
			uuid.New().String(),
			"also-bad",
		},
		"target_state": "suspended",
	}, uuid.New())
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	details, ok := resp.Error.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("details = %v, want map", resp.Error.Details)
	}
	rawIndices, ok := details["offending_indices"].([]interface{})
	if !ok {
		t.Fatalf("details.offending_indices = %v, want []interface{}", details["offending_indices"])
	}
	if len(rawIndices) != 2 {
		t.Errorf("offending_indices len = %d, want 2", len(rawIndices))
	}
	if idx, _ := rawIndices[0].(float64); int(idx) != 1 {
		t.Errorf("offending_indices[0] = %v, want 1", rawIndices[0])
	}
	if idx, _ := rawIndices[1].(float64); int(idx) != 3 {
		t.Errorf("offending_indices[1] = %v, want 3", rawIndices[1])
	}
}

func TestBulkStateChange_CrossTenantSimId_403_WithViolationsList(t *testing.T) {
	tenantID := uuid.New()
	owned := uuid.New()
	foreign := uuid.New()

	jobs := &fakeJobCreator{}
	sims := &fakeSimTenantFilter{tenantIDs: map[uuid.UUID]bool{owned: true}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, publisher)

	req := newBulkStateChangeRequest(t, map[string]interface{}{
		"sim_ids":      []string{owned.String(), foreign.String()},
		"target_state": "suspended",
	}, tenantID)
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeForbiddenCrossTenant {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeForbiddenCrossTenant)
	}
	details, ok := resp.Error.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("details = %v, want map", resp.Error.Details)
	}
	rawViolations, ok := details["violations"].([]interface{})
	if !ok {
		t.Fatalf("details.violations = %v, want []interface{}", details["violations"])
	}
	if len(rawViolations) != 1 {
		t.Errorf("violations len = %d, want 1", len(rawViolations))
	}
	if got, _ := rawViolations[0].(string); got != foreign.String() {
		t.Errorf("violations[0] = %q, want %q", rawViolations[0], foreign.String())
	}

	if jobs.lastParams.Type != "" {
		t.Errorf("job should NOT have been created; got type=%q", jobs.lastParams.Type)
	}
	if len(publisher.subjects) != 0 {
		t.Errorf("publisher should not have been called; got %d subjects", len(publisher.subjects))
	}
}

// --- PolicyAssign dual-shape tests ----------------------------------------

func newBulkPolicyAssignRequest(t *testing.T, body interface{}, tenantID uuid.UUID) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/policy-assign", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "policy_editor")
	return req.WithContext(ctx)
}

func newBulkOperatorSwitchRequest(t *testing.T, body interface{}, tenantID uuid.UUID) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/operator-switch", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	return req.WithContext(ctx)
}

func TestBulkPolicyAssign_SimIdsArray_Accepted_202(t *testing.T) {
	tenantID := uuid.New()
	sim1, sim2 := uuid.New(), uuid.New()
	policyVersionID := uuid.New()

	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID, Type: "bulk_policy_assign"}}
	sims := &fakeSimTenantFilter{tenantIDs: map[uuid.UUID]bool{sim1: true, sim2: true}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, publisher)

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"sim_ids":           []string{sim1.String(), sim2.String()},
		"policy_version_id": policyVersionID.String(),
	}, tenantID)
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if jobs.lastParams.TotalItems != 2 {
		t.Errorf("TotalItems = %d, want 2", jobs.lastParams.TotalItems)
	}
	var payload job.BulkPolicyAssignPayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.SimIDs) != 2 {
		t.Errorf("payload.SimIDs len = %d, want 2", len(payload.SimIDs))
	}
	if payload.SegmentID != uuid.Nil {
		t.Errorf("payload.SegmentID = %s, want zero", payload.SegmentID)
	}
	if payload.PolicyVersionID != policyVersionID {
		t.Errorf("payload.PolicyVersionID = %s, want %s", payload.PolicyVersionID, policyVersionID)
	}
	if len(publisher.subjects) != 1 {
		t.Errorf("publisher.subjects len = %d, want 1", len(publisher.subjects))
	}
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode success: %v", err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["status"] != "queued" {
		t.Errorf("data.status = %v, want queued", data["status"])
	}
	if total, ok := data["total_sims"].(float64); !ok || int(total) != 2 {
		t.Errorf("data.total_sims = %v, want 2", data["total_sims"])
	}
}

// Regression: FIX-201 Gate F-A1. Ensure `reason` from the request is propagated
// into the job payload so downstream per-SIM audit entries (AC-8) can carry it.
func TestBulkPolicyAssign_SimIdsArray_ReasonPropagatedToPayload(t *testing.T) {
	tenantID := uuid.New()
	sim1 := uuid.New()
	policyVersionID := uuid.New()

	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID, Type: "bulk_policy_assign"}}
	sims := &fakeSimTenantFilter{tenantIDs: map[uuid.UUID]bool{sim1: true}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, publisher)

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"sim_ids":           []string{sim1.String()},
		"policy_version_id": policyVersionID.String(),
		"reason":            "compliance audit 2026-Q1",
	}, tenantID)
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var payload job.BulkPolicyAssignPayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Reason != "compliance audit 2026-Q1" {
		t.Errorf("payload.Reason = %q, want %q", payload.Reason, "compliance audit 2026-Q1")
	}
}

func TestBulkPolicyAssign_SegmentId_Accepted_202(t *testing.T) {
	tenantID := uuid.New()
	policyVersionID := uuid.New()

	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID, Type: "bulk_policy_assign"}}
	segments := &fakeSegmentCounter{count: 50}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, segments, nil, publisher)

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"segment_id":        uuid.New().String(),
		"policy_version_id": policyVersionID.String(),
	}, tenantID)
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if jobs.lastParams.TotalItems != 50 {
		t.Errorf("TotalItems = %d, want 50", jobs.lastParams.TotalItems)
	}
	if len(publisher.subjects) != 1 {
		t.Errorf("publisher.subjects len = %d, want 1", len(publisher.subjects))
	}
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode success: %v", err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if ec, ok := data["estimated_count"].(float64); !ok || int(ec) != 50 {
		t.Errorf("data.estimated_count = %v, want 50", data["estimated_count"])
	}
}

func TestBulkPolicyAssign_BothProvided_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"sim_ids":           []string{uuid.New().String()},
		"segment_id":        uuid.New().String(),
		"policy_version_id": uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "mutually exclusive") {
		t.Errorf("message = %q, want substring 'mutually exclusive'", resp.Error.Message)
	}
}

func TestBulkPolicyAssign_NeitherProvided_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"policy_version_id": uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "one of sim_ids or segment_id") {
		t.Errorf("message = %q, want substring about required fields", resp.Error.Message)
	}
}

func TestBulkPolicyAssign_EmptyArray_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"sim_ids":           []string{},
		"policy_version_id": uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkPolicyAssign_ArrayOverLimit_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	ids := make([]string, maxBulkSimIDs+1)
	for i := range ids {
		ids[i] = uuid.New().String()
	}

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"sim_ids":           ids,
		"policy_version_id": uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "exceeds maximum") {
		t.Errorf("message = %q, want substring 'exceeds maximum'", resp.Error.Message)
	}
}

func TestBulkPolicyAssign_InvalidUUIDInArray_400_OffendingIndices(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"sim_ids": []string{
			uuid.New().String(),
			"not-a-uuid",
			uuid.New().String(),
			"also-bad",
		},
		"policy_version_id": uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	details, ok := resp.Error.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("details = %v, want map", resp.Error.Details)
	}
	rawIndices, ok := details["offending_indices"].([]interface{})
	if !ok {
		t.Fatalf("details.offending_indices = %v, want []interface{}", details["offending_indices"])
	}
	if len(rawIndices) != 2 {
		t.Errorf("offending_indices len = %d, want 2", len(rawIndices))
	}
	if idx, _ := rawIndices[0].(float64); int(idx) != 1 {
		t.Errorf("offending_indices[0] = %v, want 1", rawIndices[0])
	}
	if idx, _ := rawIndices[1].(float64); int(idx) != 3 {
		t.Errorf("offending_indices[1] = %v, want 3", rawIndices[1])
	}
}

func TestBulkPolicyAssign_CrossTenantSimId_403(t *testing.T) {
	tenantID := uuid.New()
	owned := uuid.New()
	foreign := uuid.New()
	policyVersionID := uuid.New()

	jobs := &fakeJobCreator{}
	sims := &fakeSimTenantFilter{tenantIDs: map[uuid.UUID]bool{owned: true}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, publisher)

	req := newBulkPolicyAssignRequest(t, map[string]interface{}{
		"sim_ids":           []string{owned.String(), foreign.String()},
		"policy_version_id": policyVersionID.String(),
	}, tenantID)
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeForbiddenCrossTenant {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeForbiddenCrossTenant)
	}
	details, ok := resp.Error.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("details = %v, want map", resp.Error.Details)
	}
	rawViolations, ok := details["violations"].([]interface{})
	if !ok {
		t.Fatalf("details.violations = %v, want []interface{}", details["violations"])
	}
	if len(rawViolations) != 1 {
		t.Errorf("violations len = %d, want 1", len(rawViolations))
	}
	if got, _ := rawViolations[0].(string); got != foreign.String() {
		t.Errorf("violations[0] = %q, want %q", rawViolations[0], foreign.String())
	}
	if jobs.lastParams.Type != "" {
		t.Errorf("job should NOT have been created; got type=%q", jobs.lastParams.Type)
	}
	if len(publisher.subjects) != 0 {
		t.Errorf("publisher should not have been called; got %d subjects", len(publisher.subjects))
	}
}

// --- OperatorSwitch dual-shape tests --------------------------------------

func TestBulkOperatorSwitch_SimIdsArray_Accepted_202(t *testing.T) {
	tenantID := uuid.New()
	sim1, sim2 := uuid.New(), uuid.New()
	operatorID := uuid.New()
	apnID := uuid.New()

	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID, Type: "bulk_esim_switch"}}
	sims := &fakeSimTenantFilter{tenantIDs: map[uuid.UUID]bool{sim1: true, sim2: true}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, publisher)

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"sim_ids":            []string{sim1.String(), sim2.String()},
		"target_operator_id": operatorID.String(),
		"target_apn_id":      apnID.String(),
	}, tenantID)
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if jobs.lastParams.TotalItems != 2 {
		t.Errorf("TotalItems = %d, want 2", jobs.lastParams.TotalItems)
	}
	var payload job.BulkEsimSwitchPayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.SimIDs) != 2 {
		t.Errorf("payload.SimIDs len = %d, want 2", len(payload.SimIDs))
	}
	if payload.SegmentID != uuid.Nil {
		t.Errorf("payload.SegmentID = %s, want zero", payload.SegmentID)
	}
	if payload.TargetOperatorID != operatorID {
		t.Errorf("payload.TargetOperatorID = %s, want %s", payload.TargetOperatorID, operatorID)
	}
	if payload.TargetAPNID != apnID {
		t.Errorf("payload.TargetAPNID = %s, want %s", payload.TargetAPNID, apnID)
	}
	if len(publisher.subjects) != 1 {
		t.Errorf("publisher.subjects len = %d, want 1", len(publisher.subjects))
	}
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode success: %v", err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["status"] != "queued" {
		t.Errorf("data.status = %v, want queued", data["status"])
	}
	if total, ok := data["total_sims"].(float64); !ok || int(total) != 2 {
		t.Errorf("data.total_sims = %v, want 2", data["total_sims"])
	}
}

// Regression: FIX-201 Gate F-A1. Ensure `reason` from the request is propagated
// into the operator-switch job payload (AC-8 audit fidelity).
func TestBulkOperatorSwitch_SimIdsArray_ReasonPropagatedToPayload(t *testing.T) {
	tenantID := uuid.New()
	sim1 := uuid.New()
	operatorID := uuid.New()
	apnID := uuid.New()

	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID, Type: "bulk_esim_switch"}}
	sims := &fakeSimTenantFilter{tenantIDs: map[uuid.UUID]bool{sim1: true}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, publisher)

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"sim_ids":            []string{sim1.String()},
		"target_operator_id": operatorID.String(),
		"target_apn_id":      apnID.String(),
		"reason":             "operator migration plan A",
	}, tenantID)
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var payload job.BulkEsimSwitchPayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Reason != "operator migration plan A" {
		t.Errorf("payload.Reason = %q, want %q", payload.Reason, "operator migration plan A")
	}
}

func TestBulkOperatorSwitch_SegmentId_Accepted_202(t *testing.T) {
	tenantID := uuid.New()
	operatorID := uuid.New()
	apnID := uuid.New()

	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID, Type: "bulk_esim_switch"}}
	segments := &fakeSegmentCounter{count: 30}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, segments, nil, publisher)

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"segment_id":         uuid.New().String(),
		"target_operator_id": operatorID.String(),
		"target_apn_id":      apnID.String(),
	}, tenantID)
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if jobs.lastParams.TotalItems != 30 {
		t.Errorf("TotalItems = %d, want 30", jobs.lastParams.TotalItems)
	}
	if len(publisher.subjects) != 1 {
		t.Errorf("publisher.subjects len = %d, want 1", len(publisher.subjects))
	}
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode success: %v", err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if ec, ok := data["estimated_count"].(float64); !ok || int(ec) != 30 {
		t.Errorf("data.estimated_count = %v, want 30", data["estimated_count"])
	}
}

func TestBulkOperatorSwitch_BothProvided_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"sim_ids":            []string{uuid.New().String()},
		"segment_id":         uuid.New().String(),
		"target_operator_id": uuid.New().String(),
		"target_apn_id":      uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "mutually exclusive") {
		t.Errorf("message = %q, want substring 'mutually exclusive'", resp.Error.Message)
	}
}

func TestBulkOperatorSwitch_NeitherProvided_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"target_operator_id": uuid.New().String(),
		"target_apn_id":      uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "one of sim_ids or segment_id") {
		t.Errorf("message = %q, want substring about required fields", resp.Error.Message)
	}
}

func TestBulkOperatorSwitch_EmptyArray_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"sim_ids":            []string{},
		"target_operator_id": uuid.New().String(),
		"target_apn_id":      uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkOperatorSwitch_ArrayOverLimit_400(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	ids := make([]string, maxBulkSimIDs+1)
	for i := range ids {
		ids[i] = uuid.New().String()
	}

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"sim_ids":            ids,
		"target_operator_id": uuid.New().String(),
		"target_apn_id":      uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	if !strings.Contains(resp.Error.Message, "exceeds maximum") {
		t.Errorf("message = %q, want substring 'exceeds maximum'", resp.Error.Message)
	}
}

func TestBulkOperatorSwitch_InvalidUUIDInArray_400_OffendingIndices(t *testing.T) {
	h := newBulkHandlerWithFakes(&fakeJobCreator{}, &fakeSegmentCounter{}, &fakeSimTenantFilter{}, &fakeEventPublisher{})

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"sim_ids": []string{
			uuid.New().String(),
			"not-a-uuid",
			uuid.New().String(),
			"also-bad",
		},
		"target_operator_id": uuid.New().String(),
		"target_apn_id":      uuid.New().String(),
	}, uuid.New())
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
	details, ok := resp.Error.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("details = %v, want map", resp.Error.Details)
	}
	rawIndices, ok := details["offending_indices"].([]interface{})
	if !ok {
		t.Fatalf("details.offending_indices = %v, want []interface{}", details["offending_indices"])
	}
	if len(rawIndices) != 2 {
		t.Errorf("offending_indices len = %d, want 2", len(rawIndices))
	}
	if idx, _ := rawIndices[0].(float64); int(idx) != 1 {
		t.Errorf("offending_indices[0] = %v, want 1", rawIndices[0])
	}
	if idx, _ := rawIndices[1].(float64); int(idx) != 3 {
		t.Errorf("offending_indices[1] = %v, want 3", rawIndices[1])
	}
}

func TestBulkOperatorSwitch_CrossTenantSimId_403(t *testing.T) {
	tenantID := uuid.New()
	owned := uuid.New()
	foreign := uuid.New()
	operatorID := uuid.New()
	apnID := uuid.New()

	jobs := &fakeJobCreator{}
	sims := &fakeSimTenantFilter{tenantIDs: map[uuid.UUID]bool{owned: true}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, publisher)

	req := newBulkOperatorSwitchRequest(t, map[string]interface{}{
		"sim_ids":            []string{owned.String(), foreign.String()},
		"target_operator_id": operatorID.String(),
		"target_apn_id":      apnID.String(),
	}, tenantID)
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeForbiddenCrossTenant {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeForbiddenCrossTenant)
	}
	details, ok := resp.Error.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("details = %v, want map", resp.Error.Details)
	}
	rawViolations, ok := details["violations"].([]interface{})
	if !ok {
		t.Fatalf("details.violations = %v, want []interface{}", details["violations"])
	}
	if len(rawViolations) != 1 {
		t.Errorf("violations len = %d, want 1", len(rawViolations))
	}
	if got, _ := rawViolations[0].(string); got != foreign.String() {
		t.Errorf("violations[0] = %q, want %q", rawViolations[0], foreign.String())
	}
	if jobs.lastParams.Type != "" {
		t.Errorf("job should NOT have been created; got type=%q", jobs.lastParams.Type)
	}
	if len(publisher.subjects) != 0 {
		t.Errorf("publisher should not have been called; got %d subjects", len(publisher.subjects))
	}
}

func TestBulkImportEmptyCSV(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	csvContent := "iccid,imsi,msisdn,operator_code,apn_name\n"

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "import.csv")
	part.Write([]byte(csvContent))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.Import(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}
