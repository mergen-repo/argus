package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOTACommand_StructFields(t *testing.T) {
	now := time.Now()
	sentAt := now.Add(-time.Minute)
	deliveredAt := now.Add(-30 * time.Second)
	executedAt := now.Add(-10 * time.Second)
	completedAt := now
	jobID := uuid.New()
	userID := uuid.New()
	errMsg := "delivery failed"

	cmd := &OTACommand{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		SimID:        uuid.New(),
		CommandType:  "UPDATE_FILE",
		Channel:      "sms_pp",
		Status:       "confirmed",
		APDUData:     []byte{0x00, 0xA4, 0x00, 0x04},
		SecurityMode: "kic_kid",
		Payload:      json.RawMessage(`{"file_id":"3F00"}`),
		ResponseData: json.RawMessage(`{"sw":"9000"}`),
		ErrorMessage: &errMsg,
		JobID:        &jobID,
		RetryCount:   2,
		MaxRetries:   3,
		CreatedBy:    &userID,
		SentAt:       &sentAt,
		DeliveredAt:  &deliveredAt,
		ExecutedAt:   &executedAt,
		CompletedAt:  &completedAt,
		CreatedAt:    now,
	}

	if cmd.CommandType != "UPDATE_FILE" {
		t.Errorf("CommandType = %q, want UPDATE_FILE", cmd.CommandType)
	}
	if cmd.Channel != "sms_pp" {
		t.Errorf("Channel = %q, want sms_pp", cmd.Channel)
	}
	if cmd.Status != "confirmed" {
		t.Errorf("Status = %q, want confirmed", cmd.Status)
	}
	if cmd.SecurityMode != "kic_kid" {
		t.Errorf("SecurityMode = %q, want kic_kid", cmd.SecurityMode)
	}
	if len(cmd.APDUData) != 4 {
		t.Errorf("APDUData len = %d, want 4", len(cmd.APDUData))
	}
	if cmd.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", cmd.RetryCount)
	}
	if cmd.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cmd.MaxRetries)
	}
	if cmd.JobID == nil || *cmd.JobID != jobID {
		t.Error("JobID should match")
	}
	if cmd.CreatedBy == nil || *cmd.CreatedBy != userID {
		t.Error("CreatedBy should match")
	}
	if cmd.ErrorMessage == nil || *cmd.ErrorMessage != "delivery failed" {
		t.Error("ErrorMessage should match")
	}
	if cmd.SentAt == nil {
		t.Error("SentAt should not be nil")
	}
	if cmd.DeliveredAt == nil {
		t.Error("DeliveredAt should not be nil")
	}
	if cmd.ExecutedAt == nil {
		t.Error("ExecutedAt should not be nil")
	}
	if cmd.CompletedAt == nil {
		t.Error("CompletedAt should not be nil")
	}
}

func TestOTACommand_NilOptionalFields(t *testing.T) {
	cmd := &OTACommand{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		SimID:        uuid.New(),
		CommandType:  "READ_FILE",
		Channel:      "bip",
		Status:       "queued",
		SecurityMode: "none",
		Payload:      json.RawMessage(`{}`),
		MaxRetries:   3,
		CreatedAt:    time.Now(),
	}

	if cmd.JobID != nil {
		t.Error("JobID should be nil")
	}
	if cmd.CreatedBy != nil {
		t.Error("CreatedBy should be nil")
	}
	if cmd.ErrorMessage != nil {
		t.Error("ErrorMessage should be nil")
	}
	if cmd.SentAt != nil {
		t.Error("SentAt should be nil")
	}
	if cmd.DeliveredAt != nil {
		t.Error("DeliveredAt should be nil")
	}
	if cmd.ExecutedAt != nil {
		t.Error("ExecutedAt should be nil")
	}
	if cmd.CompletedAt != nil {
		t.Error("CompletedAt should be nil")
	}
	if cmd.ResponseData != nil {
		t.Error("ResponseData should be nil")
	}
}

func TestCreateOTACommandParams_Fields(t *testing.T) {
	simID := uuid.New()
	userID := uuid.New()
	jobID := uuid.New()
	payload := json.RawMessage(`{"file_id":"7FFF","content":"AQID"}`)

	params := CreateOTACommandParams{
		SimID:        simID,
		CommandType:  "UPDATE_FILE",
		Channel:      "sms_pp",
		SecurityMode: "kic",
		APDUData:     []byte{0x00, 0xD6, 0x00, 0x00, 0x03, 0x01, 0x02, 0x03},
		Payload:      payload,
		MaxRetries:   5,
		CreatedBy:    &userID,
		JobID:        &jobID,
	}

	if params.SimID != simID {
		t.Errorf("SimID mismatch")
	}
	if params.CommandType != "UPDATE_FILE" {
		t.Errorf("CommandType = %q, want UPDATE_FILE", params.CommandType)
	}
	if params.Channel != "sms_pp" {
		t.Errorf("Channel = %q, want sms_pp", params.Channel)
	}
	if params.SecurityMode != "kic" {
		t.Errorf("SecurityMode = %q, want kic", params.SecurityMode)
	}
	if len(params.APDUData) != 8 {
		t.Errorf("APDUData len = %d, want 8", len(params.APDUData))
	}
	if params.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", params.MaxRetries)
	}
	if params.CreatedBy == nil || *params.CreatedBy != userID {
		t.Error("CreatedBy should match")
	}
	if params.JobID == nil || *params.JobID != jobID {
		t.Error("JobID should match")
	}
}

func TestOTACommandFilter_Fields(t *testing.T) {
	simID := uuid.New()
	filter := OTACommandFilter{
		SimID:       &simID,
		CommandType: "INSTALL_APPLET",
		Status:      "queued",
		Channel:     "bip",
	}

	if filter.SimID == nil || *filter.SimID != simID {
		t.Error("SimID should match")
	}
	if filter.CommandType != "INSTALL_APPLET" {
		t.Errorf("CommandType = %q, want INSTALL_APPLET", filter.CommandType)
	}
	if filter.Status != "queued" {
		t.Errorf("Status = %q, want queued", filter.Status)
	}
	if filter.Channel != "bip" {
		t.Errorf("Channel = %q, want bip", filter.Channel)
	}
}

func TestOTACommandFilter_Empty(t *testing.T) {
	filter := OTACommandFilter{}

	if filter.SimID != nil {
		t.Error("SimID should be nil")
	}
	if filter.CommandType != "" {
		t.Error("CommandType should be empty")
	}
	if filter.Status != "" {
		t.Error("Status should be empty")
	}
	if filter.Channel != "" {
		t.Error("Channel should be empty")
	}
}

func TestOTACommand_JSON(t *testing.T) {
	cmd := OTACommand{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		SimID:       uuid.New(),
		CommandType: "DELETE_APPLET",
		Channel:     "sms_pp",
		Status:      "failed",
		Payload:     json.RawMessage(`{"aid":"A000000001"}`),
		RetryCount:  3,
		MaxRetries:  3,
		CreatedAt:   time.Now(),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded OTACommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != cmd.ID {
		t.Errorf("ID mismatch")
	}
	if decoded.CommandType != cmd.CommandType {
		t.Errorf("CommandType = %q, want %q", decoded.CommandType, cmd.CommandType)
	}
	if decoded.Status != cmd.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, cmd.Status)
	}
	if decoded.RetryCount != cmd.RetryCount {
		t.Errorf("RetryCount = %d, want %d", decoded.RetryCount, cmd.RetryCount)
	}
}

func TestOTACommand_AllCommandTypes(t *testing.T) {
	types := []string{"UPDATE_FILE", "INSTALL_APPLET", "DELETE_APPLET", "READ_FILE", "SIM_TOOLKIT"}
	for _, ct := range types {
		cmd := OTACommand{CommandType: ct}
		if cmd.CommandType != ct {
			t.Errorf("CommandType = %q, want %q", cmd.CommandType, ct)
		}
	}
}

func TestOTACommand_AllStatuses(t *testing.T) {
	statuses := []string{"queued", "sent", "delivered", "executed", "confirmed", "failed"}
	for _, s := range statuses {
		cmd := OTACommand{Status: s}
		if cmd.Status != s {
			t.Errorf("Status = %q, want %q", cmd.Status, s)
		}
	}
}

func TestOTACommand_AllChannels(t *testing.T) {
	channels := []string{"sms_pp", "bip"}
	for _, ch := range channels {
		cmd := OTACommand{Channel: ch}
		if cmd.Channel != ch {
			t.Errorf("Channel = %q, want %q", cmd.Channel, ch)
		}
	}
}

func TestOTACommand_AllSecurityModes(t *testing.T) {
	modes := []string{"none", "kic", "kid", "kic_kid"}
	for _, m := range modes {
		cmd := OTACommand{SecurityMode: m}
		if cmd.SecurityMode != m {
			t.Errorf("SecurityMode = %q, want %q", cmd.SecurityMode, m)
		}
	}
}

func TestNewOTAStore(t *testing.T) {
	s := NewOTAStore(nil)
	if s == nil {
		t.Error("NewOTAStore should not return nil")
	}
}

func TestErrOTACommandNotFound(t *testing.T) {
	if ErrOTACommandNotFound == nil {
		t.Error("ErrOTACommandNotFound should not be nil")
	}
	if ErrOTACommandNotFound.Error() != "store: ota command not found" {
		t.Errorf("error = %q, want 'store: ota command not found'", ErrOTACommandNotFound.Error())
	}
}
