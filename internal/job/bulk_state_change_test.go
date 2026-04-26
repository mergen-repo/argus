package job

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// --- fake auditor shared by bulk_state_change tests ---

type fakeStateChangeAuditor struct {
	mu      sync.Mutex
	entries []audit.CreateEntryParams
	err     error
}

func (f *fakeStateChangeAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, p)
	if f.err != nil {
		return nil, f.err
	}
	return &audit.Entry{}, nil
}

func (f *fakeStateChangeAuditor) snapshot() []audit.CreateEntryParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]audit.CreateEntryParams, len(f.entries))
	copy(cp, f.entries)
	return cp
}

// newTestStateChangeProcessor returns a processor whose store dependencies are
// nil. Only the auditor-facing helpers can be exercised; anything that touches
// p.sims/p.jobs/p.distLock will panic, which is the intended scope for unit
// tests that mirror bulk_policy_assign_test.go's helper-only pattern.
func newTestStateChangeProcessor() *BulkStateChangeProcessor {
	return &BulkStateChangeProcessor{
		logger: zerolog.New(io.Discard),
	}
}

func TestBulkStateChangePayloadMarshal(t *testing.T) {
	segID := uuid.New()
	reason := "maintenance window"
	payload := BulkStateChangePayload{
		SegmentID:   segID,
		TargetState: "suspended",
		Reason:      &reason,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkStateChangePayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SegmentID != segID {
		t.Errorf("segment_id = %v, want %v", decoded.SegmentID, segID)
	}
	if decoded.TargetState != "suspended" {
		t.Errorf("target_state = %q, want %q", decoded.TargetState, "suspended")
	}
	if decoded.Reason == nil || *decoded.Reason != reason {
		t.Errorf("reason = %v, want %q", decoded.Reason, reason)
	}
}

func TestBulkStateChangePayloadWithUndoRecords(t *testing.T) {
	simID1 := uuid.New()
	simID2 := uuid.New()
	payload := BulkStateChangePayload{
		SegmentID:   uuid.New(),
		TargetState: "active",
		UndoRecords: []StateUndoRecord{
			{SimID: simID1, PreviousState: "suspended"},
			{SimID: simID2, PreviousState: "active"},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkStateChangePayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.UndoRecords) != 2 {
		t.Fatalf("undo_records len = %d, want 2", len(decoded.UndoRecords))
	}
	if decoded.UndoRecords[0].SimID != simID1 {
		t.Errorf("undo[0].sim_id = %v, want %v", decoded.UndoRecords[0].SimID, simID1)
	}
	if decoded.UndoRecords[0].PreviousState != "suspended" {
		t.Errorf("undo[0].previous_state = %q, want %q", decoded.UndoRecords[0].PreviousState, "suspended")
	}
}

func TestBulkPolicyAssignPayloadMarshal(t *testing.T) {
	segID := uuid.New()
	policyID := uuid.New()
	payload := BulkPolicyAssignPayload{
		SegmentID:       segID,
		PolicyVersionID: policyID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkPolicyAssignPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SegmentID != segID {
		t.Errorf("segment_id = %v, want %v", decoded.SegmentID, segID)
	}
	if decoded.PolicyVersionID != policyID {
		t.Errorf("policy_version_id = %v, want %v", decoded.PolicyVersionID, policyID)
	}
}

func TestBulkEsimSwitchPayloadMarshal(t *testing.T) {
	segID := uuid.New()
	opID := uuid.New()
	apnID := uuid.New()
	payload := BulkEsimSwitchPayload{
		SegmentID:        segID,
		TargetOperatorID: opID,
		TargetAPNID:      apnID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkEsimSwitchPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SegmentID != segID {
		t.Errorf("segment_id = %v, want %v", decoded.SegmentID, segID)
	}
	if decoded.TargetOperatorID != opID {
		t.Errorf("target_operator_id = %v, want %v", decoded.TargetOperatorID, opID)
	}
	if decoded.TargetAPNID != apnID {
		t.Errorf("target_apn_id = %v, want %v", decoded.TargetAPNID, apnID)
	}
}

func TestBulkOpErrorJSON(t *testing.T) {
	simID := uuid.New().String()
	err := BulkOpError{
		SimID:        simID,
		ICCID:        "89901234567890123456",
		ErrorCode:    "INVALID_STATE_TRANSITION",
		ErrorMessage: "cannot transition from ordered to suspended",
	}

	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}

	var decoded BulkOpError
	if unmarshalErr := json.Unmarshal(data, &decoded); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v", unmarshalErr)
	}

	if decoded.SimID != simID {
		t.Errorf("sim_id = %q, want %q", decoded.SimID, simID)
	}
	if decoded.ErrorCode != "INVALID_STATE_TRANSITION" {
		t.Errorf("error_code = %q, want %q", decoded.ErrorCode, "INVALID_STATE_TRANSITION")
	}
}

func TestBulkResultWithUndoRecords(t *testing.T) {
	result := BulkResult{
		ProcessedCount: 95,
		FailedCount:    5,
		TotalCount:     100,
		UndoRecords: []StateUndoRecord{
			{SimID: uuid.New(), PreviousState: "active"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if int(decoded["processed_count"].(float64)) != 95 {
		t.Errorf("processed_count = %v, want 95", decoded["processed_count"])
	}
	if int(decoded["failed_count"].(float64)) != 5 {
		t.Errorf("failed_count = %v, want 5", decoded["failed_count"])
	}

	undoRecords, ok := decoded["undo_records"].([]interface{})
	if !ok || len(undoRecords) != 1 {
		t.Fatalf("undo_records len = %v, want 1", len(undoRecords))
	}
}

func TestEsimUndoRecordMarshal(t *testing.T) {
	rec := EsimUndoRecord{
		SimID:              uuid.New(),
		OldProfileID:       uuid.New(),
		NewProfileID:       uuid.New(),
		PreviousOperatorID: uuid.New(),
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EsimUndoRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SimID != rec.SimID {
		t.Errorf("sim_id mismatch")
	}
	if decoded.OldProfileID != rec.OldProfileID {
		t.Errorf("old_profile_id mismatch")
	}
	if decoded.NewProfileID != rec.NewProfileID {
		t.Errorf("new_profile_id mismatch")
	}
	if decoded.PreviousOperatorID != rec.PreviousOperatorID {
		t.Errorf("previous_operator_id mismatch")
	}
}

func TestBulkStateChangePayload_SimIDsMarshal(t *testing.T) {
	simID1 := uuid.New()
	simID2 := uuid.New()
	reason := "batch-suspend"
	payload := BulkStateChangePayload{
		SimIDs:      []uuid.UUID{simID1, simID2},
		TargetState: "suspended",
		Reason:      &reason,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkStateChangePayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.SimIDs) != 2 {
		t.Fatalf("sim_ids len = %d, want 2", len(decoded.SimIDs))
	}
	if decoded.SimIDs[0] != simID1 || decoded.SimIDs[1] != simID2 {
		t.Errorf("sim_ids mismatch: got %v, want [%v %v]", decoded.SimIDs, simID1, simID2)
	}
	if decoded.TargetState != "suspended" {
		t.Errorf("target_state = %q, want %q", decoded.TargetState, "suspended")
	}
}

func TestSetAuditor_WiresDependency(t *testing.T) {
	p := newTestStateChangeProcessor()
	if p.auditor != nil {
		t.Fatalf("auditor should be nil before SetAuditor")
	}
	a := &fakeStateChangeAuditor{}
	p.SetAuditor(a)
	if p.auditor == nil {
		t.Fatalf("auditor should be set after SetAuditor")
	}
}

func TestEmitStateChangeAudit_FieldsAndCorrelationID(t *testing.T) {
	p := newTestStateChangeProcessor()
	a := &fakeStateChangeAuditor{}
	p.SetAuditor(a)

	jobID := uuid.New()
	tenantID := uuid.New()
	userID := uuid.New()
	simID := uuid.New()
	reason := "maintenance window"

	j := &store.Job{ID: jobID, TenantID: tenantID, CreatedBy: &userID}
	p.emitStateChangeAudit(context.Background(), j, simID, "active", "suspended", &reason)

	entries := a.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]

	if e.Action != "sim.state_change" {
		t.Errorf("action = %q, want %q", e.Action, "sim.state_change")
	}
	if e.EntityType != "sim" {
		t.Errorf("entity_type = %q, want %q", e.EntityType, "sim")
	}
	if e.EntityID != simID.String() {
		t.Errorf("entity_id = %q, want %q", e.EntityID, simID.String())
	}
	if e.TenantID != tenantID {
		t.Errorf("tenant_id = %v, want %v", e.TenantID, tenantID)
	}
	if e.UserID == nil || *e.UserID != userID {
		t.Errorf("user_id = %v, want %v", e.UserID, userID)
	}
	if e.CorrelationID == nil || *e.CorrelationID != jobID {
		t.Errorf("correlation_id = %v, want %v (bulk_job_id grouping)", e.CorrelationID, jobID)
	}

	var before map[string]any
	if err := json.Unmarshal(e.BeforeData, &before); err != nil {
		t.Fatalf("unmarshal BeforeData: %v", err)
	}
	if before["state"] != "active" {
		t.Errorf("before.state = %v, want %q", before["state"], "active")
	}

	var after map[string]any
	if err := json.Unmarshal(e.AfterData, &after); err != nil {
		t.Fatalf("unmarshal AfterData: %v", err)
	}
	if after["state"] != "suspended" {
		t.Errorf("after.state = %v, want %q", after["state"], "suspended")
	}
	if after["reason"] != "maintenance window" {
		t.Errorf("after.reason = %v, want %q", after["reason"], "maintenance window")
	}
}

func TestEmitStateChangeAudit_ReasonOmittedWhenNilOrEmpty(t *testing.T) {
	cases := []struct {
		name   string
		reason *string
	}{
		{"nil reason", nil},
		{"empty reason", func() *string { s := ""; return &s }()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestStateChangeProcessor()
			a := &fakeStateChangeAuditor{}
			p.SetAuditor(a)

			j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
			p.emitStateChangeAudit(context.Background(), j, uuid.New(), "active", "suspended", tc.reason)

			entries := a.snapshot()
			if len(entries) != 1 {
				t.Fatalf("expected 1 audit entry, got %d", len(entries))
			}

			var after map[string]any
			if err := json.Unmarshal(entries[0].AfterData, &after); err != nil {
				t.Fatalf("unmarshal AfterData: %v", err)
			}
			if _, ok := after["reason"]; ok {
				t.Errorf("reason key should be omitted when %s; got AfterData=%s", tc.name, string(entries[0].AfterData))
			}
			if after["state"] != "suspended" {
				t.Errorf("after.state = %v, want %q", after["state"], "suspended")
			}
		})
	}
}

func TestEmitStateChangeAudit_NilAuditor_NoPanic(t *testing.T) {
	p := newTestStateChangeProcessor()
	// p.auditor intentionally left nil

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitStateChangeAudit panicked with nil auditor: %v", r)
		}
	}()

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	reason := "test"
	p.emitStateChangeAudit(context.Background(), j, uuid.New(), "active", "suspended", &reason)
}

func TestEmitStateChangeAudit_AuditorError_DoesNotPropagate(t *testing.T) {
	p := newTestStateChangeProcessor()
	a := &fakeStateChangeAuditor{err: errors.New("nats down")}
	p.SetAuditor(a)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitStateChangeAudit panicked on auditor error: %v", r)
		}
	}()

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	p.emitStateChangeAudit(context.Background(), j, uuid.New(), "active", "suspended", nil)

	// Helper swallows the error — caller's per-SIM loop must not observe it.
	// Auditor still recorded the attempt (error returned AFTER append).
	if got := len(a.snapshot()); got != 1 {
		t.Errorf("expected 1 CreateEntry call (error swallowed), got %d", got)
	}
}

func TestEmitStateChangeAudit_ParamsShape_GuardsAgainstFieldDrift(t *testing.T) {
	// Defensive: if audit.CreateEntryParams evolves (fields renamed/removed),
	// this test fails loudly. Rebuild a representative params struct and
	// assert every field we rely on is set to a non-zero value after emit.
	p := newTestStateChangeProcessor()
	a := &fakeStateChangeAuditor{}
	p.SetAuditor(a)

	jobID := uuid.New()
	tenantID := uuid.New()
	userID := uuid.New()
	simID := uuid.New()
	reason := "r"

	j := &store.Job{ID: jobID, TenantID: tenantID, CreatedBy: &userID}
	p.emitStateChangeAudit(context.Background(), j, simID, "active", "suspended", &reason)

	e := a.snapshot()[0]

	checks := []struct {
		name string
		zero bool
	}{
		{"TenantID", e.TenantID == uuid.Nil},
		{"UserID", e.UserID == nil},
		{"Action", e.Action == ""},
		{"EntityType", e.EntityType == ""},
		{"EntityID", e.EntityID == ""},
		{"BeforeData", len(e.BeforeData) == 0},
		{"AfterData", len(e.AfterData) == 0},
		{"CorrelationID", e.CorrelationID == nil},
	}
	for _, c := range checks {
		if c.zero {
			t.Errorf("field %s is zero/empty; CreateEntryParams shape likely drifted", c.name)
		}
	}
}

func TestBulkStateChangeProcessorType(t *testing.T) {
	p := newTestStateChangeProcessor()
	if p.Type() != JobTypeBulkStateChange {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeBulkStateChange)
	}
}

func TestProcessorTypes(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"BulkStateChange", JobTypeBulkStateChange},
		{"BulkPolicyAssign", JobTypeBulkPolicyAssign},
		{"BulkEsimSwitch", JobTypeBulkEsimSwitch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for _, jt := range AllJobTypes {
				if jt == tt.expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("job type %q not found in AllJobTypes", tt.expected)
			}
		})
	}
}

func TestBuildBulkJobEvent_AllSuccess(t *testing.T) {
	jobID := uuid.New().String()
	tenantID := uuid.New().String()

	subject, env := buildBulkJobEvent(JobTypeBulkStateChange, jobID, tenantID, 10, 0, 10)

	if subject != bus.SubjectBulkJobCompleted {
		t.Errorf("subject = %q, want %q", subject, bus.SubjectBulkJobCompleted)
	}
	if env.Type != "bulk_job.completed" {
		t.Errorf("event type = %q, want %q", env.Type, "bulk_job.completed")
	}
	if env.Severity != severity.Info {
		t.Errorf("severity = %q, want %q", env.Severity, severity.Info)
	}
	if env.TenantID != tenantID {
		t.Errorf("tenant_id = %q, want %q", env.TenantID, tenantID)
	}
	if env.Meta["bulk_job_id"] != jobID {
		t.Errorf("meta.bulk_job_id = %v, want %q", env.Meta["bulk_job_id"], jobID)
	}
	if env.Meta["job_type"] != JobTypeBulkStateChange {
		t.Errorf("meta.job_type = %v, want %q", env.Meta["job_type"], JobTypeBulkStateChange)
	}
}

func TestBuildBulkJobEvent_PartialFail(t *testing.T) {
	jobID := uuid.New().String()
	tenantID := uuid.New().String()

	subject, env := buildBulkJobEvent(JobTypeBulkPolicyAssign, jobID, tenantID, 7, 3, 10)

	if subject != bus.SubjectBulkJobCompleted {
		t.Errorf("subject = %q, want %q", subject, bus.SubjectBulkJobCompleted)
	}
	if env.Type != "bulk_job.completed" {
		t.Errorf("event type = %q, want %q", env.Type, "bulk_job.completed")
	}
	if env.Severity != severity.Medium {
		t.Errorf("severity = %q, want %q (partial fail)", env.Severity, severity.Medium)
	}
}

func TestBuildBulkJobEvent_TotalFail(t *testing.T) {
	jobID := uuid.New().String()
	tenantID := uuid.New().String()

	subject, env := buildBulkJobEvent(JobTypeBulkEsimSwitch, jobID, tenantID, 0, 10, 10)

	if subject != bus.SubjectBulkJobFailed {
		t.Errorf("subject = %q, want %q", subject, bus.SubjectBulkJobFailed)
	}
	if env.Type != "bulk_job.failed" {
		t.Errorf("event type = %q, want %q", env.Type, "bulk_job.failed")
	}
	if env.Severity != severity.High {
		t.Errorf("severity = %q, want %q (total fail)", env.Severity, severity.High)
	}
}

func TestBuildBulkJobEvent_MetaFields(t *testing.T) {
	jobID := uuid.New().String()
	tenantID := uuid.New().String()

	_, env := buildBulkJobEvent(JobTypeBulkStateChange, jobID, tenantID, 5, 2, 7)

	expectedMeta := map[string]interface{}{
		"bulk_job_id":   jobID,
		"total_count":   7,
		"success_count": 5,
		"fail_count":    2,
		"job_type":      JobTypeBulkStateChange,
	}
	for k, want := range expectedMeta {
		got := env.Meta[k]
		if got != want {
			t.Errorf("meta[%q] = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

func TestBuildBulkJobEvent_EnvelopeValidates(t *testing.T) {
	cases := []struct {
		name      string
		processed int
		failed    int
		total     int
	}{
		{"all success", 10, 0, 10},
		{"partial fail", 7, 3, 10},
		{"total fail", 0, 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobID := uuid.New().String()
			tenantID := uuid.New().String()
			_, env := buildBulkJobEvent(JobTypeBulkStateChange, jobID, tenantID, tc.processed, tc.failed, tc.total)
			if err := env.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestBuildBulkJobEvent_AllProcessorsUseCorrectSubjects(t *testing.T) {
	tenantID := uuid.New().String()

	for _, jobType := range []string{JobTypeBulkStateChange, JobTypeBulkPolicyAssign, JobTypeBulkEsimSwitch} {
		t.Run("completed/"+jobType, func(t *testing.T) {
			subject, env := buildBulkJobEvent(jobType, uuid.New().String(), tenantID, 5, 0, 5)
			if subject != bus.SubjectBulkJobCompleted {
				t.Errorf("subject = %q, want %q", subject, bus.SubjectBulkJobCompleted)
			}
			if env.Type != "bulk_job.completed" {
				t.Errorf("type = %q, want bulk_job.completed", env.Type)
			}
		})
		t.Run("failed/"+jobType, func(t *testing.T) {
			subject, env := buildBulkJobEvent(jobType, uuid.New().String(), tenantID, 0, 5, 5)
			if subject != bus.SubjectBulkJobFailed {
				t.Errorf("subject = %q, want %q", subject, bus.SubjectBulkJobFailed)
			}
			if env.Type != "bulk_job.failed" {
				t.Errorf("type = %q, want bulk_job.failed", env.Type)
			}
		})
	}
}
