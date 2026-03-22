package job

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestDataRetentionProcessor_Type(t *testing.T) {
	p := &DataRetentionProcessor{}
	if p.Type() != JobTypeDataRetention {
		t.Errorf("expected type %s, got %s", JobTypeDataRetention, p.Type())
	}
}

func TestDataRetentionProcessor_DefaultDays(t *testing.T) {
	p := NewDataRetentionProcessor(nil, nil, nil, nil, 0, zerolog.Nop())
	if p.defaultCDRDays != 365 {
		t.Errorf("expected default 365 days, got %d", p.defaultCDRDays)
	}
}

func TestDataRetentionProcessor_CustomDays(t *testing.T) {
	p := NewDataRetentionProcessor(nil, nil, nil, nil, 180, zerolog.Nop())
	if p.defaultCDRDays != 180 {
		t.Errorf("expected 180 days, got %d", p.defaultCDRDays)
	}
}

func TestDataRetentionResult_Fields(t *testing.T) {
	result := dataRetentionResult{
		CDRChunksDropped:     5,
		SessionChunksDropped: 3,
		TenantsProcessed:     2,
		DefaultRetentionDays: 365,
		Status:               "completed",
	}

	if result.CDRChunksDropped != 5 {
		t.Errorf("expected 5 CDR chunks dropped, got %d", result.CDRChunksDropped)
	}
	if result.SessionChunksDropped != 3 {
		t.Errorf("expected 3 session chunks dropped, got %d", result.SessionChunksDropped)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed status, got %s", result.Status)
	}
}
