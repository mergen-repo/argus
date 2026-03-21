package job

import (
	"encoding/json"
	"testing"

	"github.com/btopcu/argus/internal/ota"
)

func TestOTAProcessor_Type(t *testing.T) {
	p := &OTAProcessor{}
	if p.Type() != JobTypeOTACommand {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeOTACommand)
	}
}

func TestOTAProcessor_Type_Value(t *testing.T) {
	if JobTypeOTACommand != "ota_command" {
		t.Errorf("JobTypeOTACommand = %q, want 'ota_command'", JobTypeOTACommand)
	}
}

func TestBulkOTAPayload_Unmarshal(t *testing.T) {
	raw := `{
		"sim_ids": ["sim-1", "sim-2", "sim-3"],
		"command_type": "UPDATE_FILE",
		"channel": "sms_pp",
		"security_mode": "none",
		"payload": {"file_id": "3F00", "content": "AQID"},
		"max_retries": 5
	}`

	var payload ota.BulkOTAPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(payload.SimIDs) != 3 {
		t.Errorf("SimIDs len = %d, want 3", len(payload.SimIDs))
	}
	if payload.CommandType != ota.CmdUpdateFile {
		t.Errorf("CommandType = %s, want UPDATE_FILE", payload.CommandType)
	}
	if payload.Channel != ota.ChannelSMSPP {
		t.Errorf("Channel = %s, want sms_pp", payload.Channel)
	}
	if payload.SecurityMode != ota.SecurityNone {
		t.Errorf("SecurityMode = %s, want none", payload.SecurityMode)
	}
	if payload.MaxRetries != 5 {
		t.Errorf("MaxRetries = %d, want 5", payload.MaxRetries)
	}
}

func TestBulkOTAPayload_UnmarshalWithSegment(t *testing.T) {
	raw := `{
		"sim_ids": [],
		"segment_id": "seg-abc",
		"command_type": "INSTALL_APPLET",
		"channel": "bip",
		"security_mode": "kic_kid",
		"payload": {"package_aid": [160,0], "applet_aid": [160,1]},
		"max_retries": 3
	}`

	var payload ota.BulkOTAPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if payload.SegmentID == nil {
		t.Fatal("SegmentID should not be nil")
	}
	if *payload.SegmentID != "seg-abc" {
		t.Errorf("SegmentID = %q, want seg-abc", *payload.SegmentID)
	}
	if payload.CommandType != ota.CmdInstallApplet {
		t.Errorf("CommandType = %s, want INSTALL_APPLET", payload.CommandType)
	}
	if payload.Channel != ota.ChannelBIP {
		t.Errorf("Channel = %s, want bip", payload.Channel)
	}
}

func TestBulkOTAPayload_UnmarshalInvalidJSON(t *testing.T) {
	var payload ota.BulkOTAPayload
	err := json.Unmarshal([]byte(`{broken`), &payload)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestBulkOTAResult_Marshal(t *testing.T) {
	result := ota.BulkOTAResult{
		TotalSIMs:   1000,
		QueuedCount: 990,
		FailedCount: 10,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ota.BulkOTAResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TotalSIMs != 1000 {
		t.Errorf("TotalSIMs = %d, want 1000", decoded.TotalSIMs)
	}
	if decoded.QueuedCount != 990 {
		t.Errorf("QueuedCount = %d, want 990", decoded.QueuedCount)
	}
	if decoded.FailedCount != 10 {
		t.Errorf("FailedCount = %d, want 10", decoded.FailedCount)
	}
}

func TestBulkOTAResult_Zero(t *testing.T) {
	result := ota.BulkOTAResult{}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ota.BulkOTAResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TotalSIMs != 0 {
		t.Errorf("TotalSIMs = %d, want 0", decoded.TotalSIMs)
	}
	if decoded.QueuedCount != 0 {
		t.Errorf("QueuedCount = %d, want 0", decoded.QueuedCount)
	}
	if decoded.FailedCount != 0 {
		t.Errorf("FailedCount = %d, want 0", decoded.FailedCount)
	}
}

func TestOTACommandTypes_AllValid(t *testing.T) {
	types := []ota.CommandType{
		ota.CmdUpdateFile,
		ota.CmdInstallApplet,
		ota.CmdDeleteApplet,
		ota.CmdReadFile,
		ota.CmdSIMToolkit,
	}

	for _, ct := range types {
		if err := ct.Validate(); err != nil {
			t.Errorf("%s.Validate() = %v, want nil", ct, err)
		}
	}
}

func TestOTAChannels_AllValid(t *testing.T) {
	channels := []ota.DeliveryChannel{ota.ChannelSMSPP, ota.ChannelBIP}

	for _, ch := range channels {
		if err := ch.Validate(); err != nil {
			t.Errorf("%s.Validate() = %v, want nil", ch, err)
		}
	}
}
