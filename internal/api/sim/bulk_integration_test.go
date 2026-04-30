// Package sim — Bulk handler integration tests (FIX-201 Task 11).
//
// These tests bridge the handler→job-creation contract at scale (up to 10000
// SIMs) and exercise code paths not covered by the existing unit tests in
// bulk_handler_test.go (kill-switch, 100-SIM payload shape, event-publish chain).
//
// # Scope and approach
//
// The handler's fake dependencies (fakeJobCreator, fakeSimTenantFilter, etc.)
// are reused from bulk_handler_test.go (same package). After the handler
// returns 202, the captured job payload is verified against the job package's
// exported payload types (job.BulkStateChangePayload etc.). This exercises
// the full handler→encode→decode contract without a live Postgres.
//
// The processor's inner loop (Process → DB-read → state-transition → DB-write)
// requires *store.JobStore and *store.SIMStore (concrete types with no interface
// substitution point exposed from the `job` package). That step is documented
// as MANUAL VERIFICATION REQUIRED below.
//
// The processor helper methods (emitStateChangeAudit, emitPolicyAssignAudit,
// dispatchCoAForSIM, emitSwitchAudit) are unexported and exercised in-package
// by the existing tests in internal/job/bulk_*_test.go. Those tests already
// provide Scenario 7 (audit field correctness) and Scenario 2 (CoA dispatch
// chain) coverage — cross-referenced below.
//
// # Coverage map — Acceptance Criteria → test location
//
// Scenario 1 (handler → job row → 100-SIM payload):
//   TestIntegration_BulkStateChange_100SIMs_HandlerPayloadContract (this file)
//   → AC-1 (sim_ids array accepted), AC-5 (100 within 1–10000), AC-7 (job row).
//
// Scenario 2 (policy-assign + CoA):
//   TestBulkPolicyAssign_DispatchCoA_MixedSessions, TestBulkPolicyAssign_CoADispatchedForMultipleSIMs
//   in internal/job/bulk_policy_assign_test.go (package job, same helper surface).
//   → AC-9 (CoA per SIM with active session, acked/failed counters).
//   Handler half: TestIntegration_BulkPolicyAssign_5SIMs_HandlerPayloadContract (this file).
//   → AC-1..3 (sim_ids + policy_version_id → 202 + job row).
//
// Scenario 3 (cross-tenant 403):
//   TestBulkStateChange_CrossTenantSimId_403_WithViolationsList,
//   TestBulkPolicyAssign_CrossTenantSimId_403,
//   TestBulkOperatorSwitch_CrossTenantSimId_403 in bulk_handler_test.go.
//   → AC-6 (FORBIDDEN_CROSS_TENANT, violations list, no job created).
//
// Scenario 4 (rate limit 429 + Retry-After + tenant independence):
//   TestBulkRateLimit_SecondImmediate_429, TestBulkRateLimit_429Response_IncludesRetryAfter,
//   TestBulkRateLimit_DifferentTenants_Independent in internal/gateway/bulk_ratelimit_test.go.
//   → AC-14 (rate limiting). No duplication here — Task 8 tests use real middleware via httptest.
//
// Scenario 5 (legacy segment_id path):
//   TestBulkStateChange_SegmentId_Accepted_202, TestBulkPolicyAssign_SegmentId_Accepted_202,
//   TestBulkOperatorSwitch_SegmentId_Accepted_202 in bulk_handler_test.go.
//   → AC-1..3 legacy backward-compat: segment_id accepted, payload uses segment path.
//
// Scenario 6 (validation edge cases):
//   TestBulkStateChange_EmptyArray_400, TestBulkStateChange_ArrayOverLimit_400,
//   TestBulkStateChange_InvalidUUIDInArray_400_OffendingIndices,
//   TestBulkStateChange_BothProvided_400_MutualExclusion, TestBulkStateChange_NeitherProvided_400
//   (and parallel tests for PolicyAssign + OperatorSwitch) in bulk_handler_test.go.
//   → AC-4 (offending_indices), AC-5 (array bounds), AC-mutual-exclusion, AC-neither.
//   Boundary at 10000 exactly: TestIntegration_BulkStateChange_LargeBatch_10000_Accepted (this file).
//
// Scenario 7 (audit field correctness):
//   TestEmitStateChangeAudit_FieldsAndCorrelationID, TestEmitPolicyAssignAudit_FieldsAndCorrelationID,
//   TestEmitSwitchAudit_FieldsAndCorrelationID in internal/job/bulk_*_test.go.
//   → AC-8 (Action, EntityType, CorrelationID=&jobID, BeforeData/AfterData non-nil).
//
// # MANUAL VERIFICATION REQUIRED
//
// The full Process() round-trip (job dequeue → DB-fetch SIMs → transition each → DB-write) cannot
// be exercised without a running Postgres. To verify manually (requires `make infra-up`):
//
//  1. Seed 100 SIMs for a tenant.
//  2. POST /api/v1/sims/bulk/state-change with {"sim_ids":[...100 UUIDs...],"target_state":"suspended"}
//     → assert 202 + job_id.
//  3. Wait for job runner to dequeue (or trigger synchronously in tests/e2e).
//  4. SELECT state FROM sims WHERE id IN (...100 UUIDs...) → all must be 'suspended'.
//  5. SELECT COUNT(*) FROM audit_log WHERE correlation_id = '<job_id>'
//     → must be 100 rows with action = 'sim.state_change'.
package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var errForTest = errors.New("store unavailable")

// fakeKillSwitch is a test double for killSwitchChecker.
type fakeKillSwitch struct {
	enabled bool
}

func (f *fakeKillSwitch) IsEnabled(_ string) bool {
	return f.enabled
}

// noopLogger returns a zerolog.Logger that discards all output.
func noopLogger() zerolog.Logger {
	return zerolog.Nop()
}

// makeIntegrationRequest builds a POST request with tenant/user/role context.
func makeIntegrationRequest(t *testing.T, path string, body interface{}, tenantID uuid.UUID, role string) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, role)
	return req.WithContext(ctx)
}

// makeUniqueSimIDs returns n distinct UUIDs guaranteed unique
// (safe with fakeSimTenantFilter's dedup logic).
func makeUniqueSimIDs(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	return ids
}

// buildTenantMap creates the tenantIDs map consumed by fakeSimTenantFilter.
func buildTenantMap(ids []uuid.UUID) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// uuidsToStrings converts []uuid.UUID → []string for HTTP request bodies.
func uuidsToStrings(ids []uuid.UUID) []string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	return s
}

// TestIntegration_BulkStateChange_100SIMs_HandlerPayloadContract is Scenario 1.
//
// Covers AC-1 (sim_ids array accepted → 202), AC-5 (100 within 1–10000 bounds),
// AC-7 (job created with type=bulk_state_change, total_items=100, payload contains
// all 100 UUIDs and target_state; job_id + total_sims returned in response body).
func TestIntegration_BulkStateChange_100SIMs_HandlerPayloadContract(t *testing.T) {
	tenantID := uuid.New()
	simIDs := makeUniqueSimIDs(100)

	jobID := uuid.New()
	userID := uuid.New()
	jobs := &fakeJobCreator{
		created: &store.Job{ID: jobID, TenantID: tenantID, Type: job.JobTypeBulkStateChange, CreatedBy: &userID},
	}
	sims := &fakeSimTenantFilter{tenantIDs: buildTenantMap(simIDs)}
	pub := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, pub)

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/state-change", map[string]interface{}{
		"sim_ids":      uuidsToStrings(simIDs),
		"target_state": "suspended",
		"reason":       "integration-test",
	}, tenantID, "sim_manager")
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	// AC-1 + AC-5: accepted.
	if w.Code != http.StatusAccepted {
		t.Fatalf("AC-1/AC-5: status = %d, want 202; body = %s", w.Code, w.Body.String())
	}

	// AC-7: job row — type and total_items.
	if jobs.lastParams.Type != job.JobTypeBulkStateChange {
		t.Errorf("AC-7: job type = %q, want %q", jobs.lastParams.Type, job.JobTypeBulkStateChange)
	}
	if jobs.lastParams.TotalItems != 100 {
		t.Errorf("AC-7: TotalItems = %d, want 100", jobs.lastParams.TotalItems)
	}

	// AC-7: payload round-trips all 100 UUIDs.
	var payload job.BulkStateChangePayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("AC-7: unmarshal payload: %v", err)
	}
	if len(payload.SimIDs) != 100 {
		t.Errorf("AC-7: payload.SimIDs len = %d, want 100", len(payload.SimIDs))
	}
	if payload.SegmentID != uuid.Nil {
		t.Errorf("AC-7: payload.SegmentID should be zero, got %s", payload.SegmentID)
	}
	if payload.TargetState != "suspended" {
		t.Errorf("AC-7: payload.TargetState = %q, want suspended", payload.TargetState)
	}
	if payload.Reason == nil || *payload.Reason != "integration-test" {
		t.Errorf("AC-7: payload.Reason = %v, want integration-test", payload.Reason)
	}

	// Verify all 100 UUIDs are present and match (order may differ after tenant filter).
	payloadSet := make(map[uuid.UUID]struct{}, 100)
	for _, id := range payload.SimIDs {
		payloadSet[id] = struct{}{}
	}
	for i, id := range simIDs {
		if _, ok := payloadSet[id]; !ok {
			t.Errorf("AC-7: simID[%d]=%s missing from payload", i, id)
		}
	}

	// AC-7: response body — job_id, status, total_sims.
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("AC-7: decode response: %v", err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["job_id"] != jobID.String() {
		t.Errorf("AC-7: response.job_id = %v, want %s", data["job_id"], jobID.String())
	}
	if data["status"] != "queued" {
		t.Errorf("AC-7: response.status = %v, want queued", data["status"])
	}
	if total, ok := data["total_sims"].(float64); !ok || int(total) != 100 {
		t.Errorf("AC-7: response.total_sims = %v, want 100", data["total_sims"])
	}

	// One event published per successful job.
	if len(pub.subjects) != 1 {
		t.Errorf("AC-7: event publish calls = %d, want 1", len(pub.subjects))
	}
}

// TestIntegration_BulkPolicyAssign_5SIMs_HandlerPayloadContract is the handler
// half of Scenario 2 (CoA + audit chain — processor half is in package job).
//
// Covers AC-1..3 (sim_ids + policy_version_id → 202, job row, payload correct).
func TestIntegration_BulkPolicyAssign_5SIMs_HandlerPayloadContract(t *testing.T) {
	tenantID := uuid.New()
	simIDs := makeUniqueSimIDs(5)
	policyVersionID := uuid.New()

	jobID := uuid.New()
	userID := uuid.New()
	jobs := &fakeJobCreator{
		created: &store.Job{ID: jobID, TenantID: tenantID, Type: job.JobTypeBulkPolicyAssign, CreatedBy: &userID},
	}
	sims := &fakeSimTenantFilter{tenantIDs: buildTenantMap(simIDs)}
	pub := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, pub)

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/policy-assign", map[string]interface{}{
		"sim_ids":           uuidsToStrings(simIDs),
		"policy_version_id": policyVersionID.String(),
		"reason":            "compliance rollout",
	}, tenantID, "policy_editor")
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	// AC-1: 202 accepted.
	if w.Code != http.StatusAccepted {
		t.Fatalf("AC-1: status = %d, want 202; body = %s", w.Code, w.Body.String())
	}

	// AC-7: job row shape.
	if jobs.lastParams.Type != job.JobTypeBulkPolicyAssign {
		t.Errorf("AC-7: job type = %q, want %q", jobs.lastParams.Type, job.JobTypeBulkPolicyAssign)
	}
	if jobs.lastParams.TotalItems != 5 {
		t.Errorf("AC-7: TotalItems = %d, want 5", jobs.lastParams.TotalItems)
	}

	// AC-7: payload.
	var payload job.BulkPolicyAssignPayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("AC-7: unmarshal payload: %v", err)
	}
	if len(payload.SimIDs) != 5 {
		t.Errorf("AC-7: payload.SimIDs len = %d, want 5", len(payload.SimIDs))
	}
	if payload.PolicyVersionID != policyVersionID {
		t.Errorf("AC-7: payload.PolicyVersionID = %v, want %v", payload.PolicyVersionID, policyVersionID)
	}
	if payload.SegmentID != uuid.Nil {
		t.Errorf("AC-7: payload.SegmentID should be zero; got %s", payload.SegmentID)
	}

	// AC-7: response body.
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("AC-7: decode response: %v", err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["job_id"] != jobID.String() {
		t.Errorf("AC-7: response.job_id = %v, want %s", data["job_id"], jobID.String())
	}
	if data["status"] != "queued" {
		t.Errorf("AC-7: response.status = %v, want queued", data["status"])
	}
	if total, ok := data["total_sims"].(float64); !ok || int(total) != 5 {
		t.Errorf("AC-7: response.total_sims = %v, want 5", data["total_sims"])
	}

	if len(pub.subjects) != 1 {
		t.Errorf("AC-7: event publish calls = %d, want 1", len(pub.subjects))
	}
}

// TestIntegration_BulkOperatorSwitch_4SIMs_HandlerPayloadContract verifies the
// operator-switch handler for the sim_ids path.
// Covers AC-1..3 (sim_ids + operator/APN IDs → 202, payload correct).
func TestIntegration_BulkOperatorSwitch_4SIMs_HandlerPayloadContract(t *testing.T) {
	tenantID := uuid.New()
	simIDs := makeUniqueSimIDs(4)
	operatorID := uuid.New()
	apnID := uuid.New()

	jobID := uuid.New()
	jobs := &fakeJobCreator{
		created: &store.Job{ID: jobID, TenantID: tenantID, Type: job.JobTypeBulkEsimSwitch},
	}
	sims := &fakeSimTenantFilter{tenantIDs: buildTenantMap(simIDs)}
	pub := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, pub)

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/operator-switch", map[string]interface{}{
		"sim_ids":            uuidsToStrings(simIDs),
		"target_operator_id": operatorID.String(),
		"target_apn_id":      apnID.String(),
		"reason":             "carrier migration",
	}, tenantID, "tenant_admin")
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("AC-1: status = %d, want 202; body = %s", w.Code, w.Body.String())
	}
	if jobs.lastParams.TotalItems != 4 {
		t.Errorf("AC-7: TotalItems = %d, want 4", jobs.lastParams.TotalItems)
	}

	var payload job.BulkEsimSwitchPayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("AC-7: unmarshal payload: %v", err)
	}
	if len(payload.SimIDs) != 4 {
		t.Errorf("AC-7: payload.SimIDs len = %d, want 4", len(payload.SimIDs))
	}
	if payload.TargetOperatorID != operatorID {
		t.Errorf("AC-7: payload.TargetOperatorID = %v, want %v", payload.TargetOperatorID, operatorID)
	}
	if payload.TargetAPNID != apnID {
		t.Errorf("AC-7: payload.TargetAPNID = %v, want %v", payload.TargetAPNID, apnID)
	}
	if payload.SegmentID != uuid.Nil {
		t.Errorf("AC-7: payload.SegmentID should be zero; got %s", payload.SegmentID)
	}

	if len(pub.subjects) != 1 {
		t.Errorf("AC-7: event publish calls = %d, want 1", len(pub.subjects))
	}
}

// TestIntegration_BulkStateChange_LargeBatch_10000_Accepted tests the exact
// upper boundary (maxBulkSimIDs=10000 must succeed; 10001 must fail — already
// tested by TestBulkStateChange_ArrayOverLimit_400).
// Covers AC-5 boundary.
func TestIntegration_BulkStateChange_LargeBatch_10000_Accepted(t *testing.T) {
	tenantID := uuid.New()
	simIDs := makeUniqueSimIDs(maxBulkSimIDs)

	jobID := uuid.New()
	jobs := &fakeJobCreator{
		created: &store.Job{ID: jobID, TenantID: tenantID, Type: job.JobTypeBulkStateChange},
	}
	sims := &fakeSimTenantFilter{tenantIDs: buildTenantMap(simIDs)}
	pub := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, pub)

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/state-change", map[string]interface{}{
		"sim_ids":      uuidsToStrings(simIDs),
		"target_state": "active",
	}, tenantID, "sim_manager")
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("AC-5/boundary: status = %d, want 202 for %d items; body=%s", w.Code, maxBulkSimIDs, w.Body.String())
	}
	if jobs.lastParams.TotalItems != maxBulkSimIDs {
		t.Errorf("AC-5/boundary: TotalItems = %d, want %d", jobs.lastParams.TotalItems, maxBulkSimIDs)
	}

	var payload job.BulkStateChangePayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("AC-5/boundary: unmarshal payload: %v", err)
	}
	if len(payload.SimIDs) != maxBulkSimIDs {
		t.Errorf("AC-5/boundary: payload.SimIDs len = %d, want %d", len(payload.SimIDs), maxBulkSimIDs)
	}
}

// TestIntegration_BulkStateChange_KillSwitch_503 verifies the bulk kill-switch
// path. This code path is separate from validation and is not exercised by the
// validation tests in bulk_handler_test.go.
func TestIntegration_BulkStateChange_KillSwitch_503(t *testing.T) {
	h := &BulkHandler{
		killSwitch: &fakeKillSwitch{enabled: true},
		logger:     noopLogger(),
	}

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/state-change", map[string]interface{}{
		"sim_ids":      []string{uuid.New().String()},
		"target_state": "suspended",
	}, uuid.New(), "sim_manager")
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("kill-switch: status = %d, want 503", w.Code)
	}
}

// TestIntegration_BulkPolicyAssign_KillSwitch_503 mirrors the kill switch test
// for the policy-assign endpoint.
func TestIntegration_BulkPolicyAssign_KillSwitch_503(t *testing.T) {
	h := &BulkHandler{
		killSwitch: &fakeKillSwitch{enabled: true},
		logger:     noopLogger(),
	}

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/policy-assign", map[string]interface{}{
		"sim_ids":           []string{uuid.New().String()},
		"policy_version_id": uuid.New().String(),
	}, uuid.New(), "policy_editor")
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("kill-switch: status = %d, want 503", w.Code)
	}
}

// TestIntegration_BulkOperatorSwitch_KillSwitch_503 mirrors the kill switch test
// for the operator-switch endpoint.
func TestIntegration_BulkOperatorSwitch_KillSwitch_503(t *testing.T) {
	h := &BulkHandler{
		killSwitch: &fakeKillSwitch{enabled: true},
		logger:     noopLogger(),
	}

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/operator-switch", map[string]interface{}{
		"sim_ids":            []string{uuid.New().String()},
		"target_operator_id": uuid.New().String(),
		"target_apn_id":      uuid.New().String(),
	}, uuid.New(), "tenant_admin")
	w := httptest.NewRecorder()

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("kill-switch: status = %d, want 503", w.Code)
	}
}

// TestIntegration_BulkStateChange_CrossTenant_NoClobber verifies that a
// mixed-ownership request (50 owned + 1 foreign) returns 403 AND no job is
// created. This extends the existing 2-SIM cross-tenant test to confirm the
// invariant holds at scale.
// Covers AC-6 (cross-tenant rejection, no job at any batch size).
func TestIntegration_BulkStateChange_CrossTenant_NoClobber(t *testing.T) {
	tenantID := uuid.New()
	owned := makeUniqueSimIDs(50)
	foreign := uuid.New()

	tenantMap := buildTenantMap(owned)
	// foreign not in tenant map → violations list will contain it.

	jobs := &fakeJobCreator{}
	sims := &fakeSimTenantFilter{tenantIDs: tenantMap}
	pub := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, pub)

	all := append(owned, foreign) //nolint:gocritic
	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/state-change", map[string]interface{}{
		"sim_ids":      uuidsToStrings(all),
		"target_state": "suspended",
	}, tenantID, "sim_manager")
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	// AC-6: 403 + FORBIDDEN_CROSS_TENANT.
	if w.Code != http.StatusForbidden {
		t.Fatalf("AC-6: status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	resp := decodeErrorResponse(t, w.Body)
	if resp.Error.Code != apierr.CodeForbiddenCrossTenant {
		t.Errorf("AC-6: code = %q, want %q", resp.Error.Code, apierr.CodeForbiddenCrossTenant)
	}
	details, ok := resp.Error.Details.(map[string]interface{})
	if !ok {
		t.Fatalf("AC-6: details = %T, want map", resp.Error.Details)
	}
	violations, _ := details["violations"].([]interface{})
	if len(violations) != 1 {
		t.Errorf("AC-6: violations len = %d, want 1", len(violations))
	}
	if len(violations) > 0 {
		if got, _ := violations[0].(string); got != foreign.String() {
			t.Errorf("AC-6: violations[0] = %q, want %q", got, foreign.String())
		}
	}

	// AC-6: no job created, no event published.
	if jobs.lastParams.Type != "" {
		t.Errorf("AC-6: job should NOT have been created; got type=%q", jobs.lastParams.Type)
	}
	if len(pub.subjects) != 0 {
		t.Errorf("AC-6: event should NOT have been published; got %d events", len(pub.subjects))
	}
}

// TestIntegration_BulkStateChange_JobCreatorError_500 exercises the error path
// when the job store returns an error, ensuring the handler returns 500 and does
// not publish an event.
func TestIntegration_BulkStateChange_JobCreatorError_500(t *testing.T) {
	tenantID := uuid.New()
	simIDs := makeUniqueSimIDs(3)

	jobs := &fakeJobCreator{err: errForTest}
	sims := &fakeSimTenantFilter{tenantIDs: buildTenantMap(simIDs)}
	pub := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, pub)

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/state-change", map[string]interface{}{
		"sim_ids":      uuidsToStrings(simIDs),
		"target_state": "suspended",
	}, tenantID, "sim_manager")
	w := httptest.NewRecorder()

	h.StateChange(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("job-error: status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if len(pub.subjects) != 0 {
		t.Errorf("job-error: event should NOT have been published; got %d events", len(pub.subjects))
	}
}

// TestIntegration_BulkPolicyAssign_JobCreatorError_500 mirrors the job creator
// error test for the policy-assign endpoint.
func TestIntegration_BulkPolicyAssign_JobCreatorError_500(t *testing.T) {
	tenantID := uuid.New()
	simIDs := makeUniqueSimIDs(3)

	jobs := &fakeJobCreator{err: errForTest}
	sims := &fakeSimTenantFilter{tenantIDs: buildTenantMap(simIDs)}
	pub := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, sims, pub)

	req := makeIntegrationRequest(t, "/api/v1/sims/bulk/policy-assign", map[string]interface{}{
		"sim_ids":           uuidsToStrings(simIDs),
		"policy_version_id": uuid.New().String(),
	}, tenantID, "policy_editor")
	w := httptest.NewRecorder()

	h.PolicyAssign(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("job-error: status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	if len(pub.subjects) != 0 {
		t.Errorf("job-error: event should NOT have been published; got %d events", len(pub.subjects))
	}
}
