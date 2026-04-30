package audit

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type captureAuditor struct {
	entries []CreateEntryParams
}

func (c *captureAuditor) CreateEntry(ctx context.Context, p CreateEntryParams) (*Entry, error) {
	c.entries = append(c.entries, p)
	return &Entry{Action: p.Action, EntityType: p.EntityType, EntityID: p.EntityID}, nil
}

func TestEmit_RecordsCorrectActionAndEntityID(t *testing.T) {
	cases := []struct {
		action     string
		entityType string
		entityID   string
	}{
		{"cdr.export", "cdr_export", uuid.New().String()},
		{"sim.erasure", "sim", uuid.New().String()},
		{"retention.update", "compliance", uuid.New().String()},
		{"msisdn.import", "msisdn_pool", uuid.New().String()},
		{"msisdn.assign", "msisdn_pool", uuid.New().String()},
		{"anomaly.update", "anomaly", uuid.New().String()},
		{"segment.create", "segment", uuid.New().String()},
		{"segment.delete", "segment", uuid.New().String()},
		{"notification_config.update", "notification_config", uuid.New().String()},
		{"job.cancel", "job", uuid.New().String()},
		{"job.retry", "job", uuid.New().String()},
		{"apikey.rotate", "api_key", uuid.New().String()},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			auditor := &captureAuditor{}
			tenantID := uuid.New()
			userID := uuid.New()

			req := httptest.NewRequest("POST", "/test", nil)
			ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
			ctx = context.WithValue(ctx, apierr.UserIDKey, userID)
			req = req.WithContext(ctx)

			Emit(req, zerolog.Nop(), auditor, tc.action, tc.entityType, tc.entityID, nil, nil)

			if len(auditor.entries) != 1 {
				t.Fatalf("expected 1 audit entry, got %d", len(auditor.entries))
			}
			entry := auditor.entries[0]
			if entry.Action != tc.action {
				t.Errorf("action = %q, want %q", entry.Action, tc.action)
			}
			if entry.EntityID != tc.entityID {
				t.Errorf("entity_id = %q, want %q", entry.EntityID, tc.entityID)
			}
			if entry.EntityType != tc.entityType {
				t.Errorf("entity_type = %q, want %q", entry.EntityType, tc.entityType)
			}
			if entry.TenantID != tenantID {
				t.Errorf("tenant_id = %v, want %v", entry.TenantID, tenantID)
			}
			if entry.UserID == nil || *entry.UserID != userID {
				t.Errorf("user_id = %v, want %v", entry.UserID, userID)
			}
		})
	}
}

func TestEmit_NilAuditor_DoesNotPanic(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	Emit(req, zerolog.Nop(), nil, "test.action", "entity", "entity-id", nil, nil)
}

func TestEmit_NoTenantOrUser(t *testing.T) {
	auditor := &captureAuditor{}
	req := httptest.NewRequest("POST", "/test", nil)

	Emit(req, zerolog.Nop(), auditor, "test.action", "entity", "entity-id", nil, nil)

	if len(auditor.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditor.entries))
	}
	entry := auditor.entries[0]
	if entry.TenantID != uuid.Nil {
		t.Errorf("expected nil tenant_id, got %v", entry.TenantID)
	}
	if entry.UserID != nil {
		t.Errorf("expected nil user_id, got %v", entry.UserID)
	}
}
