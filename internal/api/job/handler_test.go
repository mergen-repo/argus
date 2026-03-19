package job

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func TestToJobDTO(t *testing.T) {
	now := time.Now()
	userID := uuid.New()
	startedAt := now.Add(-5 * time.Minute)
	completedAt := now

	job := &store.Job{
		ID:             uuid.New(),
		TenantID:       uuid.New(),
		Type:           "bulk_sim_import",
		State:          "completed",
		Priority:       5,
		Payload:        json.RawMessage(`{}`),
		TotalItems:     100,
		ProcessedItems: 95,
		FailedItems:    5,
		ProgressPct:    100.0,
		MaxRetries:     3,
		RetryCount:     0,
		StartedAt:      &startedAt,
		CompletedAt:    &completedAt,
		CreatedAt:      now.Add(-10 * time.Minute),
		CreatedBy:      &userID,
	}

	dto := toJobDTO(job)

	if dto.ID != job.ID {
		t.Errorf("ID = %v, want %v", dto.ID, job.ID)
	}
	if dto.Type != "bulk_sim_import" {
		t.Errorf("Type = %s, want bulk_sim_import", dto.Type)
	}
	if dto.State != "completed" {
		t.Errorf("State = %s, want completed", dto.State)
	}
	if dto.TotalItems != 100 {
		t.Errorf("TotalItems = %d, want 100", dto.TotalItems)
	}
	if dto.ProcessedItems != 95 {
		t.Errorf("ProcessedItems = %d, want 95", dto.ProcessedItems)
	}
	if dto.FailedItems != 5 {
		t.Errorf("FailedItems = %d, want 5", dto.FailedItems)
	}
	if dto.StartedAt == nil {
		t.Error("StartedAt should not be nil")
	}
	if dto.CompletedAt == nil {
		t.Error("CompletedAt should not be nil")
	}
	if dto.CreatedBy == nil {
		t.Error("CreatedBy should not be nil")
	}
}

func TestToJobDTONilOptionals(t *testing.T) {
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Type:     "bulk_sim_import",
		State:    "queued",
		Priority: 5,
		Payload:  json.RawMessage(`{}`),
		CreatedAt: time.Now(),
	}

	dto := toJobDTO(job)

	if dto.StartedAt != nil {
		t.Error("StartedAt should be nil for queued job")
	}
	if dto.CompletedAt != nil {
		t.Error("CompletedAt should be nil for queued job")
	}
	if dto.CreatedBy != nil {
		t.Error("CreatedBy should be nil when not set")
	}
}
