package ota

import (
	"encoding/json"
	"testing"
)

func TestCommandType_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ct      CommandType
		wantErr bool
	}{
		{"UPDATE_FILE", CmdUpdateFile, false},
		{"INSTALL_APPLET", CmdInstallApplet, false},
		{"DELETE_APPLET", CmdDeleteApplet, false},
		{"READ_FILE", CmdReadFile, false},
		{"SIM_TOOLKIT", CmdSIMToolkit, false},
		{"invalid", CommandType("INVALID"), true},
		{"empty", CommandType(""), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ct.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeliveryChannel_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ch      DeliveryChannel
		wantErr bool
	}{
		{"sms_pp", ChannelSMSPP, false},
		{"bip", ChannelBIP, false},
		{"invalid", DeliveryChannel("http"), true},
		{"empty", DeliveryChannel(""), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ch.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCommandStatus_Values(t *testing.T) {
	statuses := []CommandStatus{
		StatusQueued, StatusSent, StatusDelivered,
		StatusExecuted, StatusConfirmed, StatusFailed,
	}
	expected := []string{
		"queued", "sent", "delivered",
		"executed", "confirmed", "failed",
	}

	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("status[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestSecurityMode_Values(t *testing.T) {
	modes := []SecurityMode{SecurityNone, SecurityKIC, SecurityKID, SecurityKICKID}
	expected := []string{"none", "kic", "kid", "kic_kid"}

	for i, m := range modes {
		if string(m) != expected[i] {
			t.Errorf("mode[%d] = %q, want %q", i, m, expected[i])
		}
	}
}

func TestBulkOTAPayload_JSON(t *testing.T) {
	segID := "seg-123"
	payload := BulkOTAPayload{
		SimIDs:       []string{"sim-1", "sim-2"},
		SegmentID:    &segID,
		CommandType:  CmdUpdateFile,
		Channel:      ChannelSMSPP,
		SecurityMode: SecurityKIC,
		Payload:      json.RawMessage(`{"file_id":"3F00","content":"AQID"}`),
		MaxRetries:   5,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkOTAPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.SimIDs) != 2 {
		t.Errorf("SimIDs len = %d, want 2", len(decoded.SimIDs))
	}
	if decoded.CommandType != CmdUpdateFile {
		t.Errorf("CommandType = %s, want %s", decoded.CommandType, CmdUpdateFile)
	}
	if decoded.Channel != ChannelSMSPP {
		t.Errorf("Channel = %s, want %s", decoded.Channel, ChannelSMSPP)
	}
	if decoded.SecurityMode != SecurityKIC {
		t.Errorf("SecurityMode = %s, want %s", decoded.SecurityMode, SecurityKIC)
	}
	if decoded.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", decoded.MaxRetries)
	}
	if decoded.SegmentID == nil || *decoded.SegmentID != "seg-123" {
		t.Error("SegmentID should be 'seg-123'")
	}
}

func TestBulkOTAResult_JSON(t *testing.T) {
	result := BulkOTAResult{
		TotalSIMs:   100,
		QueuedCount: 95,
		FailedCount: 5,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkOTAResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TotalSIMs != 100 {
		t.Errorf("TotalSIMs = %d, want 100", decoded.TotalSIMs)
	}
	if decoded.QueuedCount != 95 {
		t.Errorf("QueuedCount = %d, want 95", decoded.QueuedCount)
	}
	if decoded.FailedCount != 5 {
		t.Errorf("FailedCount = %d, want 5", decoded.FailedCount)
	}
}

func TestOTACommand_Fields(t *testing.T) {
	cmd := OTACommand{
		CommandType:  CmdUpdateFile,
		Channel:      ChannelSMSPP,
		Status:       StatusQueued,
		SecurityMode: SecurityNone,
		RetryCount:   0,
		MaxRetries:   3,
	}

	if cmd.CommandType != CmdUpdateFile {
		t.Errorf("CommandType = %s, want UPDATE_FILE", cmd.CommandType)
	}
	if cmd.Channel != ChannelSMSPP {
		t.Errorf("Channel = %s, want sms_pp", cmd.Channel)
	}
	if cmd.Status != StatusQueued {
		t.Errorf("Status = %s, want queued", cmd.Status)
	}
	if cmd.SecurityMode != SecurityNone {
		t.Errorf("SecurityMode = %s, want none", cmd.SecurityMode)
	}
}

func TestCreateCommandParams_Fields(t *testing.T) {
	p := CreateCommandParams{
		CommandType:  CmdInstallApplet,
		Channel:      ChannelBIP,
		SecurityMode: SecurityKICKID,
		MaxRetries:   5,
	}

	if p.CommandType != CmdInstallApplet {
		t.Errorf("CommandType = %s, want INSTALL_APPLET", p.CommandType)
	}
	if p.Channel != ChannelBIP {
		t.Errorf("Channel = %s, want bip", p.Channel)
	}
	if p.SecurityMode != SecurityKICKID {
		t.Errorf("SecurityMode = %s, want kic_kid", p.SecurityMode)
	}
	if p.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", p.MaxRetries)
	}
}
