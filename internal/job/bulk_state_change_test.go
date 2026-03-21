package job

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

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
