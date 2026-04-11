package job

import "github.com/google/uuid"

type BulkOpError struct {
	SimID        string `json:"sim_id"`
	ICCID        string `json:"iccid"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

type BulkStateChangePayload struct {
	SegmentID   uuid.UUID          `json:"segment_id"`
	TargetState string             `json:"target_state"`
	Reason      *string            `json:"reason,omitempty"`
	UndoRecords []StateUndoRecord  `json:"undo_records,omitempty"`
}

type StateUndoRecord struct {
	SimID         uuid.UUID `json:"sim_id"`
	PreviousState string    `json:"previous_state"`
}

type BulkPolicyAssignPayload struct {
	SegmentID       uuid.UUID            `json:"segment_id"`
	PolicyVersionID uuid.UUID            `json:"policy_version_id"`
	UndoRecords     []PolicyUndoRecord   `json:"undo_records,omitempty"`
}

type PolicyUndoRecord struct {
	SimID                   uuid.UUID  `json:"sim_id"`
	PreviousPolicyVersionID *uuid.UUID `json:"previous_policy_version_id"`
}

type BulkEsimSwitchPayload struct {
	SegmentID        uuid.UUID          `json:"segment_id"`
	TargetOperatorID uuid.UUID          `json:"target_operator_id"`
	TargetAPNID      uuid.UUID          `json:"target_apn_id"`
	UndoRecords      []EsimUndoRecord   `json:"undo_records,omitempty"`
}

type EsimUndoRecord struct {
	SimID              uuid.UUID `json:"sim_id"`
	OldProfileID       uuid.UUID `json:"old_profile_id"`
	NewProfileID       uuid.UUID `json:"new_profile_id"`
	PreviousOperatorID uuid.UUID `json:"previous_operator_id"`
}

type BulkResult struct {
	ProcessedCount int         `json:"processed_count"`
	FailedCount    int         `json:"failed_count"`
	TotalCount     int         `json:"total_count"`
	UndoRecords    interface{} `json:"undo_records,omitempty"`
	CoASentCount   int         `json:"coa_sent_count,omitempty"`
	CoAAckedCount  int         `json:"coa_acked_count,omitempty"`
	CoAFailedCount int         `json:"coa_failed_count,omitempty"`
}
