package job

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
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

func TestRunnerConfig_Defaults(t *testing.T) {
	r := NewRunner(nil, nil, nil, RunnerConfig{}, zerolog.Nop())
	if r.config.MaxConcurrentPerTenant != 5 {
		t.Errorf("default MaxConcurrentPerTenant = %d, want 5", r.config.MaxConcurrentPerTenant)
	}
	if r.config.LockRenewInterval != 30*time.Second {
		t.Errorf("default LockRenewInterval = %v, want 30s", r.config.LockRenewInterval)
	}
}

func TestRunnerConfig_Custom(t *testing.T) {
	r := NewRunner(nil, nil, nil, RunnerConfig{
		MaxConcurrentPerTenant: 10,
		LockRenewInterval:     15 * time.Second,
	}, zerolog.Nop())

	if r.config.MaxConcurrentPerTenant != 10 {
		t.Errorf("MaxConcurrentPerTenant = %d, want 10", r.config.MaxConcurrentPerTenant)
	}
	if r.config.LockRenewInterval != 15*time.Second {
		t.Errorf("LockRenewInterval = %v, want 15s", r.config.LockRenewInterval)
	}
}

func TestRunner_TryAcquireReleaseSlot(t *testing.T) {
	r := NewRunner(nil, nil, nil, RunnerConfig{MaxConcurrentPerTenant: 2}, zerolog.Nop())
	tenantID := uuid.New()

	if !r.tryAcquireSlot(tenantID) {
		t.Fatal("first slot should be acquired")
	}
	if !r.tryAcquireSlot(tenantID) {
		t.Fatal("second slot should be acquired")
	}
	if r.tryAcquireSlot(tenantID) {
		t.Fatal("third slot should be rejected (max=2)")
	}

	r.releaseSlot(tenantID)
	if !r.tryAcquireSlot(tenantID) {
		t.Fatal("should acquire after release")
	}
}

func TestRunner_TryAcquireSlot_MultiTenant(t *testing.T) {
	r := NewRunner(nil, nil, nil, RunnerConfig{MaxConcurrentPerTenant: 1}, zerolog.Nop())
	tenant1 := uuid.New()
	tenant2 := uuid.New()

	if !r.tryAcquireSlot(tenant1) {
		t.Fatal("tenant1 first slot should be acquired")
	}
	if r.tryAcquireSlot(tenant1) {
		t.Fatal("tenant1 second slot should be rejected")
	}
	if !r.tryAcquireSlot(tenant2) {
		t.Fatal("tenant2 first slot should be acquired (independent)")
	}
}

func TestRunner_Register(t *testing.T) {
	r := NewRunner(nil, nil, nil, RunnerConfig{}, zerolog.Nop())

	stub := NewStubProcessor("test_proc", nil, nil, zerolog.Nop())
	r.Register(stub)

	if _, ok := r.processors["test_proc"]; !ok {
		t.Fatal("processor should be registered")
	}
}

func TestRunner_CancelJob_NoOp(t *testing.T) {
	r := NewRunner(nil, nil, nil, RunnerConfig{}, zerolog.Nop())
	r.CancelJob(uuid.New())
}
