package job

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestJobMessageMarshal(t *testing.T) {
	msg := JobMessage{
		JobID:    uuid.New(),
		TenantID: uuid.New(),
		Type:     JobTypeBulkImport,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded JobMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.JobID != msg.JobID {
		t.Errorf("JobID = %v, want %v", decoded.JobID, msg.JobID)
	}
	if decoded.TenantID != msg.TenantID {
		t.Errorf("TenantID = %v, want %v", decoded.TenantID, msg.TenantID)
	}
	if decoded.Type != msg.Type {
		t.Errorf("Type = %s, want %s", decoded.Type, msg.Type)
	}
}

func TestImportPayloadMarshal(t *testing.T) {
	payload := ImportPayload{
		CSVData:  "iccid,imsi\n123,456\n",
		FileName: "test.csv",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ImportPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.FileName != payload.FileName {
		t.Errorf("FileName = %s, want %s", decoded.FileName, payload.FileName)
	}
	if decoded.CSVData != payload.CSVData {
		t.Errorf("CSVData mismatch")
	}
}
