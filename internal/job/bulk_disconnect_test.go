package job

import (
	"encoding/json"
	"testing"
)

func TestBulkDisconnectPayloadMarshal(t *testing.T) {
	payload := BulkDisconnectPayload{
		SimIDs: []string{"sim-1", "sim-2", "sim-3"},
		Reason: "maintenance",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkDisconnectPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Reason != "maintenance" {
		t.Errorf("Reason = %q, want maintenance", decoded.Reason)
	}
	if len(decoded.SimIDs) != 3 {
		t.Errorf("SimIDs len = %d, want 3", len(decoded.SimIDs))
	}
}

func TestBulkDisconnectPayloadWithSegment(t *testing.T) {
	segID := "seg-123"
	payload := BulkDisconnectPayload{
		SegmentID: &segID,
		Reason:    "policy_change",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkDisconnectPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SegmentID == nil {
		t.Fatal("SegmentID is nil, want non-nil")
	}
	if *decoded.SegmentID != "seg-123" {
		t.Errorf("SegmentID = %q, want seg-123", *decoded.SegmentID)
	}
}

func TestBulkDisconnectResultMarshal(t *testing.T) {
	result := BulkDisconnectResult{
		TotalSessions:     50,
		DisconnectedCount: 48,
		FailedCount:       2,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkDisconnectResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TotalSessions != 50 {
		t.Errorf("TotalSessions = %d, want 50", decoded.TotalSessions)
	}
	if decoded.DisconnectedCount != 48 {
		t.Errorf("DisconnectedCount = %d, want 48", decoded.DisconnectedCount)
	}
	if decoded.FailedCount != 2 {
		t.Errorf("FailedCount = %d, want 2", decoded.FailedCount)
	}
}

func TestBulkDisconnectProcessorType(t *testing.T) {
	p := &BulkDisconnectProcessor{}
	if p.Type() != JobTypeBulkDisconnect {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeBulkDisconnect)
	}
}
