package job

import "github.com/google/uuid"

type BulkOpError struct {
	SimID        string `json:"sim_id"`
	ICCID        string `json:"iccid"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

type BulkStateChangePayload struct {
	SegmentID   uuid.UUID         `json:"segment_id,omitempty"`
	SimIDs      []uuid.UUID       `json:"sim_ids,omitempty"`
	TargetState string            `json:"target_state"`
	Reason      *string           `json:"reason,omitempty"`
	UndoRecords []StateUndoRecord `json:"undo_records,omitempty"`
}

type StateUndoRecord struct {
	SimID         uuid.UUID `json:"sim_id"`
	PreviousState string    `json:"previous_state"`
}

type BulkPolicyAssignPayload struct {
	SegmentID       uuid.UUID          `json:"segment_id,omitempty"`
	SimIDs          []uuid.UUID        `json:"sim_ids,omitempty"`
	PolicyVersionID uuid.UUID          `json:"policy_version_id"`
	Reason          string             `json:"reason,omitempty"`
	UndoRecords     []PolicyUndoRecord `json:"undo_records,omitempty"`
}

type PolicyUndoRecord struct {
	SimID                   uuid.UUID  `json:"sim_id"`
	PreviousPolicyVersionID *uuid.UUID `json:"previous_policy_version_id"`
}

type BulkEsimSwitchPayload struct {
	SegmentID        uuid.UUID        `json:"segment_id,omitempty"`
	SimIDs           []uuid.UUID      `json:"sim_ids,omitempty"`
	TargetOperatorID uuid.UUID        `json:"target_operator_id"`
	TargetAPNID      uuid.UUID        `json:"target_apn_id"`
	Reason           string           `json:"reason,omitempty"`
	UndoRecords      []EsimUndoRecord `json:"undo_records,omitempty"`
}

type EsimUndoRecord struct {
	SimID              uuid.UUID `json:"sim_id"`
	EID                string    `json:"eid"`
	OldProfileID       uuid.UUID `json:"old_profile_id"`
	NewProfileID       uuid.UUID `json:"new_profile_id"`
	PreviousOperatorID uuid.UUID `json:"previous_operator_id"`
}

// DeviceBindingsBulkRowSpec is one row from the uploaded CSV: the ICCID to
// update, the IMEI to bind (may be empty — means "clear"), and the binding mode.
type DeviceBindingsBulkRowSpec struct {
	ICCID       string `json:"iccid"`
	BoundIMEI   string `json:"bound_imei"`
	BindingMode string `json:"binding_mode"`
}

// DeviceBindingsBulkRowResult records the per-row outcome stored in the job's
// error report. Outcome codes: "success", "unknown_iccid", "invalid_imei",
// "invalid_mode", "store_error".
type DeviceBindingsBulkRowResult struct {
	ICCID    string `json:"iccid"`
	Outcome  string `json:"outcome"`
	ErrorMsg string `json:"error,omitempty"`
}

// BulkDeviceBindingsPayload is the job payload for JobTypeBulkDeviceBindings.
type BulkDeviceBindingsPayload struct {
	Rows []DeviceBindingsBulkRowSpec `json:"rows"`
}

// IMEIPoolImportRowSpec is one row from the uploaded CSV for an IMEI pool import job.
type IMEIPoolImportRowSpec struct {
	Kind             string `json:"kind"`
	IMEIOrTAC        string `json:"imei_or_tac"`
	DeviceModel      string `json:"device_model,omitempty"`
	Description      string `json:"description,omitempty"`
	QuarantineReason string `json:"quarantine_reason,omitempty"`
	BlockReason      string `json:"block_reason,omitempty"`
	ImportedFrom     string `json:"imported_from,omitempty"`
}

// IMEIPoolImportRowResult records the per-row outcome for a BulkIMEIPoolImport job.
// Outcome codes: "success", "imei_pool_duplicate", "invalid_imei_length",
// "invalid_kind", "missing_quarantine_reason", "missing_block_reason",
// "missing_imported_from", "invalid_imported_from", "invalid_csv_injection".
type IMEIPoolImportRowResult struct {
	RowNumber int    `json:"row_number"`
	Outcome   string `json:"outcome"`
	Message   string `json:"message,omitempty"`
}

// BulkIMEIPoolImportPayload is the job payload for JobTypeBulkIMEIPoolImport.
type BulkIMEIPoolImportPayload struct {
	TenantID string                  `json:"tenant_id"`
	UserID   string                  `json:"user_id,omitempty"`
	Pool     string                  `json:"pool"`
	Rows     []IMEIPoolImportRowSpec `json:"rows"`
}

// BulkIMEIPoolImportResult is the result JSON stored in the job's result column.
type BulkIMEIPoolImportResult struct {
	Total        int                       `json:"total"`
	SuccessCount int                       `json:"success_count"`
	FailedCount  int                       `json:"failed_count"`
	Rows         []IMEIPoolImportRowResult `json:"rows"`
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
