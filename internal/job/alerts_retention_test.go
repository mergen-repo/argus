package job

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

func TestAlertsRetentionProcessor_Type(t *testing.T) {
	p := &AlertsRetentionProcessor{}
	if p.Type() != JobTypeAlertsRetention {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeAlertsRetention)
	}
	if JobTypeAlertsRetention != "alerts_retention" {
		t.Errorf("JobTypeAlertsRetention = %q, want %q", JobTypeAlertsRetention, "alerts_retention")
	}
}

func TestAllJobTypes_ContainsAlertsRetention(t *testing.T) {
	for _, jt := range AllJobTypes {
		if jt == JobTypeAlertsRetention {
			return
		}
	}
	t.Errorf("JobTypeAlertsRetention not found in AllJobTypes")
}

func TestAlertsRetention_FloorEnforced(t *testing.T) {
	p := NewAlertsRetentionProcessor(nil, nil, nil, 5, zerolog.Nop())
	if p.retentionDays != minAlertsRetentionDays {
		t.Errorf("retentionDays = %d, want floor %d", p.retentionDays, minAlertsRetentionDays)
	}
}

func TestAlertsRetention_FloorAcceptsDefault(t *testing.T) {
	p := NewAlertsRetentionProcessor(nil, nil, nil, 180, zerolog.Nop())
	if p.retentionDays != 180 {
		t.Errorf("retentionDays = %d, want 180", p.retentionDays)
	}
}

func TestAlertsRetention_ResultJSONShape(t *testing.T) {
	cutoff := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	b, err := buildRetentionResult(7, cutoff, 180)
	if err != nil {
		t.Fatalf("buildRetentionResult: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := got["deleted_count"].(float64); !ok || int64(v) != 7 {
		t.Errorf("deleted_count = %v, want 7 (int)", got["deleted_count"])
	}
	if v, ok := got["cutoff"].(string); !ok || v != "2026-04-01T12:00:00Z" {
		t.Errorf("cutoff = %v, want RFC3339 2026-04-01T12:00:00Z", got["cutoff"])
	}
	if v, ok := got["retention_days"].(float64); !ok || int(v) != 180 {
		t.Errorf("retention_days = %v, want 180", got["retention_days"])
	}
	if v, ok := got["status"].(string); !ok || v != "completed" {
		t.Errorf("status = %v, want completed", got["status"])
	}
}

// ---- DB-gated retention test ------------------------------------------------

func testAlertsRetentionPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("skip: cannot connect to postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Logf("skip: postgres ping failed: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func seedRetentionTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, contact_email) VALUES ($1, $2, $3)`,
		id, "alerts-retention-tenant-"+id.String()[:8], "retention-"+id.String()[:8]+"@test.invalid",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM alerts WHERE tenant_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

// TestAlertsRetention_DeletesOlderThanCutoff exercises the processor end-to-end:
// it seeds alerts on both sides of the cutoff, constructs the real processor,
// and invokes Process(). This guards the cutoff math (retentionDays→duration)
// and the jobs.Complete result-JSON wiring. If someone flipped the sign or
// inverted the time unit, this test would catch it — unlike a direct call to
// store.DeleteOlderThan.
func TestAlertsRetention_DeletesOlderThanCutoff(t *testing.T) {
	pool := testAlertsRetentionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated retention test")
	}
	tenantID := seedRetentionTenant(t, pool)
	alertStore := store.NewAlertStore(pool)
	jobStore := store.NewJobStore(pool)
	ctx := context.Background()

	now := time.Now().UTC()
	// 3 alerts at -200d (older than 180d cutoff) — should be purged
	for i := 0; i < 3; i++ {
		if _, err := alertStore.Create(ctx, store.CreateAlertParams{
			TenantID: tenantID, Type: "old", Severity: "low", Source: "system",
			Title: "old", Description: "d",
			FiredAt: now.AddDate(0, 0, -200),
		}); err != nil {
			t.Fatalf("seed old alert: %v", err)
		}
	}
	// 2 alerts at -10d (within retention window) — should remain
	for i := 0; i < 2; i++ {
		if _, err := alertStore.Create(ctx, store.CreateAlertParams{
			TenantID: tenantID, Type: "fresh", Severity: "low", Source: "system",
			Title: "fresh", Description: "d",
			FiredAt: now.AddDate(0, 0, -10),
		}); err != nil {
			t.Fatalf("seed fresh alert: %v", err)
		}
	}

	// Seed a jobs row so Process can Lock+Complete it. Tenant is reused (FK to tenants).
	j, err := jobStore.CreateWithTenantID(ctx, tenantID, store.CreateJobParams{
		Type:     JobTypeAlertsRetention,
		Priority: 3,
		Payload:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, j.ID)
	})
	// Lock the job — Complete expects state to have been transitioned from queued.
	if err := jobStore.Lock(ctx, j.ID, "test-runner"); err != nil {
		t.Fatalf("lock job: %v", err)
	}

	p := NewAlertsRetentionProcessor(jobStore, alertStore, nil, 180, zerolog.Nop())
	if err := p.Process(ctx, j); err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Verify exactly 2 alerts remain for this tenant (the 3 -200d rows were purged).
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE tenant_id = $1`, tenantID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 2 {
		t.Errorf("remaining alerts = %d, want 2 (3 old rows should have been deleted)", remaining)
	}

	// Verify the job was marked completed with a result JSON carrying deleted_count.
	var state string
	var resultJSON []byte
	err = pool.QueryRow(ctx, `SELECT state, result FROM jobs WHERE id = $1`, j.ID).Scan(&state, &resultJSON)
	if err != nil {
		t.Fatalf("fetch job state: %v", err)
	}
	if state != "completed" {
		t.Errorf("job state = %q, want completed", state)
	}
	var parsed map[string]any
	if err := json.Unmarshal(resultJSON, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if v, ok := parsed["deleted_count"].(float64); !ok || int64(v) != 3 {
		t.Errorf("deleted_count in result = %v, want 3", parsed["deleted_count"])
	}
	if _, ok := parsed["cutoff"].(string); !ok {
		t.Errorf("cutoff missing from result JSON: %v", parsed)
	}
}
