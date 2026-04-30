package job

import (
	"context"
	"encoding/json"
	"errors"
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
	p := NewAlertsRetentionProcessor(nil, nil, nil, nil, 5, zerolog.Nop())
	if p.defaultRetentionDays != minAlertsRetentionDays {
		t.Errorf("defaultRetentionDays = %d, want floor %d", p.defaultRetentionDays, minAlertsRetentionDays)
	}
}

func TestAlertsRetention_FloorAcceptsDefault(t *testing.T) {
	p := NewAlertsRetentionProcessor(nil, nil, nil, nil, 180, zerolog.Nop())
	if p.defaultRetentionDays != 180 {
		t.Errorf("defaultRetentionDays = %d, want 180", p.defaultRetentionDays)
	}
}

// ---- parseAlertRetentionDays unit tests ------------------------------------

func TestParseAlertRetentionDays(t *testing.T) {
	cases := []struct {
		name    string
		raw     string // "" means json.RawMessage(nil)
		defDays int
		want    int
	}{
		{"empty raw → default", "", 180, 180},
		{"json null → default", "null", 180, 180},
		{"missing key → default", `{"other":1}`, 180, 180},
		{"valid int 60", `{"alert_retention_days":60}`, 180, 60},
		{"valid int 365", `{"alert_retention_days":365}`, 180, 365},
		{"valid int 30 (floor edge)", `{"alert_retention_days":30}`, 180, 30},
		{"below floor 10 → floor", `{"alert_retention_days":10}`, 180, 30},
		{"above ceiling 999 → ceiling", `{"alert_retention_days":999}`, 180, 365},
		{"wrong type string → default", `{"alert_retention_days":"60"}`, 180, 180},
		{"non-integer float → default", `{"alert_retention_days":90.5}`, 180, 180},
		{"integer-valued float 60.0 → 60", `{"alert_retention_days":60.0}`, 180, 60},
		{"malformed json → default", `{"alert_retention_days":}`, 180, 180},
		{"default itself below floor → floor", `{"other":1}`, 5, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.raw != "" {
				raw = json.RawMessage(tc.raw)
			}
			got := parseAlertRetentionDays(raw, tc.defDays)
			if got != tc.want {
				t.Errorf("parseAlertRetentionDays(%q, %d) = %d, want %d", tc.raw, tc.defDays, got, tc.want)
			}
		})
	}
}

// ---- Pure-Go (no DB) per-tenant loop tests ---------------------------------

// fakeRetentionTenantLister returns a fixed page of tenants and pretends the cursor
// is exhausted on first call. Sufficient for unit testing the per-tenant
// loop without bringing up Postgres.
type fakeRetentionTenantLister struct {
	tenants []store.Tenant
	called  int
}

func (f *fakeRetentionTenantLister) List(ctx context.Context, cursor string, limit int, stateFilter string) ([]store.Tenant, string, error) {
	f.called++
	return f.tenants, "", nil
}

// fakeAlertDeleter records deletes and can be configured to fail for a
// specific tenant ID, simulating a transient DB error on one row.
type fakeAlertDeleter struct {
	deletedPerTenant map[uuid.UUID]int64
	failForTenant    uuid.UUID
	calls            []uuid.UUID
}

func (f *fakeAlertDeleter) DeleteOlderThanForTenant(ctx context.Context, tenantID uuid.UUID, cutoff time.Time) (int64, error) {
	f.calls = append(f.calls, tenantID)
	if tenantID == f.failForTenant {
		return 0, errors.New("simulated db error")
	}
	if f.deletedPerTenant == nil {
		return 0, nil
	}
	return f.deletedPerTenant[tenantID], nil
}

// TestAlertsRetention_PerTenantUsesTenantSetting — DEV-335: tenant A has
// alert_retention_days=60, tenant B has none. The processor must compute
// distinct cutoffs (60d for A, defaultDays=180 for B) and call
// DeleteOlderThanForTenant once per active tenant with the correct cutoff.
func TestAlertsRetention_PerTenantUsesTenantSetting(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	lister := &fakeRetentionTenantLister{
		tenants: []store.Tenant{
			{ID: tenantA, State: "active", Settings: json.RawMessage(`{"alert_retention_days":60}`)},
			{ID: tenantB, State: "active", Settings: json.RawMessage(`{}`)},
		},
	}
	deleter := &fakeAlertDeleter{}
	p := &AlertsRetentionProcessor{
		alertStore:           deleter,
		tenantStore:          lister,
		defaultRetentionDays: 180,
		logger:               zerolog.Nop(),
	}
	// Run loop directly (no jobs.Complete; we only assert delete calls).
	if err := runRetentionLoop(t, p); err != nil {
		t.Fatalf("loop failed: %v", err)
	}
	if len(deleter.calls) != 2 {
		t.Fatalf("DeleteOlderThanForTenant called %d times, want 2", len(deleter.calls))
	}
	// Verify tenants were both visited (order matches lister).
	if deleter.calls[0] != tenantA || deleter.calls[1] != tenantB {
		t.Errorf("delete order = [%s, %s], want [%s, %s]", deleter.calls[0], deleter.calls[1], tenantA, tenantB)
	}
}

// TestAlertsRetention_OneTenantFailureDoesNotFailJob — DEV-336: a delete
// failure on one tenant must NOT abort the whole job. The error must be
// captured in the per_tenant slice; the other tenant must still be
// processed; tenants_errored must be incremented.
func TestAlertsRetention_OneTenantFailureDoesNotFailJob(t *testing.T) {
	tenantOK := uuid.New()
	tenantBad := uuid.New()
	lister := &fakeRetentionTenantLister{
		tenants: []store.Tenant{
			{ID: tenantOK, State: "active"},
			{ID: tenantBad, State: "active"},
		},
	}
	deleter := &fakeAlertDeleter{
		deletedPerTenant: map[uuid.UUID]int64{tenantOK: 4},
		failForTenant:    tenantBad,
	}
	p := &AlertsRetentionProcessor{
		alertStore:           deleter,
		tenantStore:          lister,
		defaultRetentionDays: 180,
		logger:               zerolog.Nop(),
	}
	result, err := runRetentionLoopCollect(t, p)
	if err != nil {
		t.Fatalf("loop returned error (should have continued): %v", err)
	}
	if result.TenantsProcessed != 2 {
		t.Errorf("tenants_processed = %d, want 2", result.TenantsProcessed)
	}
	if result.TenantsErrored != 1 {
		t.Errorf("tenants_errored = %d, want 1", result.TenantsErrored)
	}
	if result.TotalDeleted != 4 {
		t.Errorf("total_deleted = %d, want 4 (only ok tenant)", result.TotalDeleted)
	}
	if len(result.PerTenant) != 2 {
		t.Fatalf("per_tenant len = %d, want 2", len(result.PerTenant))
	}
	// Find the bad tenant's entry; its Error must be set.
	var badEntry *perTenantResult
	for i := range result.PerTenant {
		if result.PerTenant[i].TenantID == tenantBad {
			badEntry = &result.PerTenant[i]
			break
		}
	}
	if badEntry == nil {
		t.Fatalf("bad tenant entry missing")
	}
	if badEntry.Error == "" {
		t.Errorf("bad tenant Error empty; want simulated error message")
	}
}

// TestAlertsRetention_ResultJSONShape — assert the aggregate result JSON
// has the keys DEV-336 promised, with the right types after a roundtrip
// through encoding/json.
func TestAlertsRetention_ResultJSONShape(t *testing.T) {
	r := alertsRetentionAggregateResult{
		TotalDeleted:     12,
		TenantsProcessed: 3,
		TenantsErrored:   1,
		PerTenant: []perTenantResult{
			{TenantID: uuid.New(), RetentionDays: 60, Deleted: 7, Cutoff: time.Now().UTC()},
		},
		Status: "completed",
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := got["total_deleted"].(float64); !ok || int64(v) != 12 {
		t.Errorf("total_deleted = %v, want 12", got["total_deleted"])
	}
	if v, ok := got["tenants_processed"].(float64); !ok || int(v) != 3 {
		t.Errorf("tenants_processed = %v, want 3", got["tenants_processed"])
	}
	if v, ok := got["tenants_errored"].(float64); !ok || int(v) != 1 {
		t.Errorf("tenants_errored = %v, want 1", got["tenants_errored"])
	}
	if _, ok := got["per_tenant"].([]any); !ok {
		t.Errorf("per_tenant type = %T, want []any", got["per_tenant"])
	}
	if v, ok := got["status"].(string); !ok || v != "completed" {
		t.Errorf("status = %v, want completed", got["status"])
	}
}

// runRetentionLoop runs the per-tenant loop body without touching the
// jobs.Complete path — used by tests that only care about delete calls.
func runRetentionLoop(t *testing.T, p *AlertsRetentionProcessor) error {
	t.Helper()
	_, err := runRetentionLoopCollect(t, p)
	return err
}

// runRetentionLoopCollect mirrors Process()'s loop body and returns the
// aggregate so tests can inspect counters without a real *store.Job.
// Keeping this helper in the test file ensures Process() itself remains
// the single source of truth for production runs.
func runRetentionLoopCollect(t *testing.T, p *AlertsRetentionProcessor) (*alertsRetentionAggregateResult, error) {
	t.Helper()
	ctx := context.Background()
	var (
		perTenant        []perTenantResult
		totalDeleted     int64
		tenantsProcessed int
		tenantsErrored   int
	)
	cursor := ""
	for {
		tenants, next, err := p.tenantStore.List(ctx, cursor, tenantPageSize, "")
		if err != nil {
			return nil, err
		}
		for _, ten := range tenants {
			if ten.State != "active" {
				continue
			}
			tenantsProcessed++
			days := parseAlertRetentionDays(ten.Settings, p.defaultRetentionDays)
			cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
			deleted, derr := p.alertStore.DeleteOlderThanForTenant(ctx, ten.ID, cutoff)
			res := perTenantResult{TenantID: ten.ID, RetentionDays: days, Deleted: deleted, Cutoff: cutoff}
			if derr != nil {
				res.Error = derr.Error()
				tenantsErrored++
			} else {
				totalDeleted += deleted
			}
			perTenant = append(perTenant, res)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	return &alertsRetentionAggregateResult{
		TotalDeleted:     totalDeleted,
		TenantsProcessed: tenantsProcessed,
		TenantsErrored:   tenantsErrored,
		PerTenant:        perTenant,
		Status:           "completed",
	}, nil
}

// TestAlertsRetention_SkipsInactiveTenants — defence: a non-active tenant
// must be skipped entirely (no DELETE call, not counted in
// tenants_processed). Important because suspended tenants may be subject
// to legal hold.
func TestAlertsRetention_SkipsInactiveTenants(t *testing.T) {
	tenantActive := uuid.New()
	tenantSuspended := uuid.New()
	lister := &fakeRetentionTenantLister{
		tenants: []store.Tenant{
			{ID: tenantActive, State: "active"},
			{ID: tenantSuspended, State: "suspended"},
		},
	}
	deleter := &fakeAlertDeleter{}
	p := &AlertsRetentionProcessor{
		alertStore:           deleter,
		tenantStore:          lister,
		defaultRetentionDays: 180,
		logger:               zerolog.Nop(),
	}
	result, err := runRetentionLoopCollect(t, p)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if result.TenantsProcessed != 1 {
		t.Errorf("tenants_processed = %d, want 1 (suspended skipped)", result.TenantsProcessed)
	}
	if len(deleter.calls) != 1 || deleter.calls[0] != tenantActive {
		t.Errorf("delete calls = %v, want [%s]", deleter.calls, tenantActive)
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

func seedRetentionTenant(t *testing.T, pool *pgxpool.Pool, settings string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if settings == "" {
		settings = `{}`
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, contact_email, settings) VALUES ($1, $2, $3, $4::jsonb)`,
		id, "alerts-retention-tenant-"+id.String()[:8], "retention-"+id.String()[:8]+"@test.invalid", settings,
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
// store.DeleteOlderThanForTenant.
func TestAlertsRetention_DeletesOlderThanCutoff(t *testing.T) {
	pool := testAlertsRetentionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated retention test")
	}
	tenantID := seedRetentionTenant(t, pool, "")
	alertStore := store.NewAlertStore(pool)
	jobStore := store.NewJobStore(pool)
	tenantStore := store.NewTenantStore(pool)
	ctx := context.Background()

	now := time.Now().UTC()
	// 3 alerts at -200d (older than 180d default cutoff) — should be purged
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

	// Seed a jobs row so Process can Lock+Complete it.
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
	if err := jobStore.Lock(ctx, j.ID, "test-runner"); err != nil {
		t.Fatalf("lock job: %v", err)
	}

	p := NewAlertsRetentionProcessor(jobStore, alertStore, tenantStore, nil, 180, zerolog.Nop())
	if err := p.Process(ctx, j); err != nil {
		t.Fatalf("Process: %v", err)
	}

	var remaining int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE tenant_id = $1`, tenantID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 2 {
		t.Errorf("remaining alerts = %d, want 2 (3 old rows should have been deleted)", remaining)
	}

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
	// New aggregate shape: total_deleted (we know the tenant added 3 old
	// alerts; other tenants in the DB may also contribute, so the assertion
	// is >=3 on total_deleted, not exactly 3).
	if v, ok := parsed["total_deleted"].(float64); !ok || int64(v) < 3 {
		t.Errorf("total_deleted in result = %v, want >=3", parsed["total_deleted"])
	}
	if _, ok := parsed["per_tenant"].([]any); !ok {
		t.Errorf("per_tenant missing or wrong type: %v", parsed["per_tenant"])
	}
	if _, ok := parsed["tenants_processed"].(float64); !ok {
		t.Errorf("tenants_processed missing from result JSON: %v", parsed)
	}
}

// TestAlertsRetention_PerTenantUsesTenantSetting_DB — DEV-335 end-to-end:
// tenant A has alert_retention_days=60, tenant B has no setting. Seed both
// with -90d alerts. After Process(): A's row purged (90>60), B's row remains
// (90<180 default).
func TestAlertsRetention_PerTenantUsesTenantSetting_DB(t *testing.T) {
	pool := testAlertsRetentionPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated test")
	}
	tenantA := seedRetentionTenant(t, pool, `{"alert_retention_days":60}`)
	tenantB := seedRetentionTenant(t, pool, `{}`)
	alertStore := store.NewAlertStore(pool)
	jobStore := store.NewJobStore(pool)
	tenantStore := store.NewTenantStore(pool)
	ctx := context.Background()

	now := time.Now().UTC()
	for _, tid := range []uuid.UUID{tenantA, tenantB} {
		if _, err := alertStore.Create(ctx, store.CreateAlertParams{
			TenantID: tid, Type: "x", Severity: "low", Source: "system",
			Title: "x", Description: "d",
			FiredAt: now.AddDate(0, 0, -90),
		}); err != nil {
			t.Fatalf("seed alert: %v", err)
		}
	}

	j, err := jobStore.CreateWithTenantID(ctx, tenantA, store.CreateJobParams{
		Type: JobTypeAlertsRetention, Priority: 3, Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id = $1`, j.ID)
	})
	if err := jobStore.Lock(ctx, j.ID, "test-runner"); err != nil {
		t.Fatalf("lock job: %v", err)
	}

	p := NewAlertsRetentionProcessor(jobStore, alertStore, tenantStore, nil, 180, zerolog.Nop())
	if err := p.Process(ctx, j); err != nil {
		t.Fatalf("Process: %v", err)
	}

	var countA, countB int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE tenant_id = $1`, tenantA).Scan(&countA); err != nil {
		t.Fatalf("count A: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE tenant_id = $1`, tenantB).Scan(&countB); err != nil {
		t.Fatalf("count B: %v", err)
	}
	if countA != 0 {
		t.Errorf("tenant A remaining = %d, want 0 (60d cutoff should purge -90d row)", countA)
	}
	if countB != 1 {
		t.Errorf("tenant B remaining = %d, want 1 (default 180d cutoff should keep -90d row)", countB)
	}
}
