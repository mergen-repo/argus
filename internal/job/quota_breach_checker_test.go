package job

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// fakeQuotaTenantStore satisfies quotaBreachTenantStore for tests.
type fakeQuotaTenantStore struct {
	mu      sync.Mutex
	tenants []store.Tenant
	stats   map[uuid.UUID]*store.TenantStats
	apiRPS  map[uuid.UUID]float64
}

func (f *fakeQuotaTenantStore) List(_ context.Context, _ string, _ int, _ string) ([]store.Tenant, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tenants, "", nil
}

func (f *fakeQuotaTenantStore) GetStats(_ context.Context, tenantID uuid.UUID) (*store.TenantStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.stats[tenantID]; ok {
		return s, nil
	}
	return &store.TenantStats{}, nil
}

// apiRPS allows tests to inject per-tenant API RPS values. Default zero.
func (f *fakeQuotaTenantStore) EstimateAPIRPS(_ context.Context, tenantID uuid.UUID) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.apiRPS[tenantID]; ok {
		return v
	}
	return 0
}

// fakeQuotaAlertStore satisfies quotaBreachAlertStore for tests with in-memory dedup.
type fakeQuotaAlertStore struct {
	mu     sync.Mutex
	alerts map[string]*store.Alert
	calls  []store.CreateAlertParams
}

func newFakeQuotaAlertStore() *fakeQuotaAlertStore {
	return &fakeQuotaAlertStore{
		alerts: make(map[string]*store.Alert),
	}
}

func (f *fakeQuotaAlertStore) UpsertWithDedup(_ context.Context, p store.CreateAlertParams, _ int) (*store.Alert, store.UpsertResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, p)

	if p.DedupKey == nil {
		a := &store.Alert{ID: uuid.New(), TenantID: p.TenantID, Type: p.Type, Severity: p.Severity}
		return a, store.UpsertInserted, nil
	}

	key := *p.DedupKey
	if existing, ok := f.alerts[key]; ok {
		return existing, store.UpsertDeduplicated, nil
	}

	a := &store.Alert{
		ID:       uuid.New(),
		TenantID: p.TenantID,
		Type:     p.Type,
		Severity: p.Severity,
		State:    "open",
		DedupKey: p.DedupKey,
	}
	f.alerts[key] = a
	return a, store.UpsertInserted, nil
}

func (f *fakeQuotaAlertStore) FindActiveByDedupKey(_ context.Context, tenantID uuid.UUID, dedupKey string) (*store.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	a, ok := f.alerts[dedupKey]
	if !ok || a.TenantID != tenantID {
		return nil, store.ErrAlertNotFound
	}
	if a.State == "resolved" {
		return nil, store.ErrAlertNotFound
	}
	return a, nil
}

func (f *fakeQuotaAlertStore) UpdateState(_ context.Context, tenantID, id uuid.UUID, newState string, _ *uuid.UUID, _ int) (*store.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for key, a := range f.alerts {
		if a.ID == id && a.TenantID == tenantID {
			a.State = newState
			f.alerts[key] = a
			return a, nil
		}
	}
	return nil, errors.New("alert not found")
}

func (f *fakeQuotaAlertStore) activeAlertCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, a := range f.alerts {
		if a.State != "resolved" {
			count++
		}
	}
	return count
}

// runQuotaSweep drives the core logic of QuotaBreachCheckerProcessor without
// requiring a real *store.JobStore (mirrors coa_failure_alerter_test pattern).
func runQuotaSweep(t *testing.T, p *QuotaBreachCheckerProcessor) quotaBreachCheckerResult {
	t.Helper()
	ctx := context.Background()

	var alerted, deduped, resolved, alertFails int

	cursor := ""
	for {
		tenants, nextCursor, err := p.tenantStore.List(ctx, cursor, 100, "active")
		if err != nil {
			t.Fatalf("List tenants error: %v", err)
		}

		for _, ten := range tenants {
			if cErr := p.checkTenant(ctx, ten, &alerted, &deduped, &resolved, &alertFails); cErr != nil {
				t.Logf("tenant %s check error: %v", ten.ID, cErr)
			}
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return quotaBreachCheckerResult{
		Alerted:    alerted,
		Deduped:    deduped,
		Resolved:   resolved,
		AlertFails: alertFails,
	}
}

// TestQuotaBreachChecker_TypeConst verifies the type constant and AllJobTypes registration.
func TestQuotaBreachChecker_TypeConst(t *testing.T) {
	p := &QuotaBreachCheckerProcessor{}
	if p.Type() != JobTypeQuotaBreachChecker {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeQuotaBreachChecker)
	}
	if JobTypeQuotaBreachChecker != "quota_breach_checker" {
		t.Errorf("JobTypeQuotaBreachChecker = %q, want %q", JobTypeQuotaBreachChecker, "quota_breach_checker")
	}
	for _, jt := range AllJobTypes {
		if jt == JobTypeQuotaBreachChecker {
			return
		}
	}
	t.Errorf("JobTypeQuotaBreachChecker not found in AllJobTypes")
}

// TestQuotaBreachChecker_ThreeTenants verifies the 3-tenant scenario:
// 50% → no alert, 82% → medium alert, 96% → critical alert.
func TestQuotaBreachChecker_ThreeTenants(t *testing.T) {
	tid1 := uuid.New()
	tid2 := uuid.New()
	tid3 := uuid.New()

	tStore := &fakeQuotaTenantStore{
		tenants: []store.Tenant{
			{ID: tid1, Name: "T1-50pct", State: "active", MaxSims: 1000},
			{ID: tid2, Name: "T2-82pct", State: "active", MaxSims: 1000},
			{ID: tid3, Name: "T3-96pct", State: "active", MaxSims: 1000},
		},
		stats: map[uuid.UUID]*store.TenantStats{
			tid1: {SimCount: 500},
			tid2: {SimCount: 820},
			tid3: {SimCount: 960},
		},
	}
	aStore := newFakeQuotaAlertStore()

	p := &QuotaBreachCheckerProcessor{
		tenantStore: tStore,
		alertStore:  aStore,
		logger:      zerolog.Nop(),
	}

	res := runQuotaSweep(t, p)

	// T1 at 50% → no alert; T2 at 82% → 1 medium; T3 at 96% → 1 critical.
	if res.Alerted != 2 {
		t.Errorf("Alerted = %d, want 2 (1 medium + 1 critical)", res.Alerted)
	}
	if res.Deduped != 0 {
		t.Errorf("Deduped = %d, want 0 on first run", res.Deduped)
	}
	if res.AlertFails != 0 {
		t.Errorf("AlertFails = %d, want 0", res.AlertFails)
	}
	if aStore.activeAlertCount() != 2 {
		t.Errorf("active alert count = %d, want 2", aStore.activeAlertCount())
	}

	// Verify severity values match canonical taxonomy.
	aStore.mu.Lock()
	defer aStore.mu.Unlock()
	sevByTenant := map[uuid.UUID]string{}
	for _, a := range aStore.alerts {
		sevByTenant[a.TenantID] = a.Severity
	}
	if sev, ok := sevByTenant[tid2]; !ok || sev != severity.Medium {
		t.Errorf("T2 alert severity = %q, want %q", sev, severity.Medium)
	}
	if sev, ok := sevByTenant[tid3]; !ok || sev != severity.Critical {
		t.Errorf("T3 alert severity = %q, want %q", sev, severity.Critical)
	}
}

// TestQuotaBreachChecker_DeduplicationBlocksSecondRun verifies that running
// the checker twice with identical conditions produces no new alert inserts
// on the second run (UpsertDeduplicated path).
func TestQuotaBreachChecker_DeduplicationBlocksSecondRun(t *testing.T) {
	tid1 := uuid.New()
	tid2 := uuid.New()

	tStore := &fakeQuotaTenantStore{
		tenants: []store.Tenant{
			{ID: tid1, Name: "T1-82pct", State: "active", MaxSims: 1000},
			{ID: tid2, Name: "T2-96pct", State: "active", MaxSims: 1000},
		},
		stats: map[uuid.UUID]*store.TenantStats{
			tid1: {SimCount: 820},
			tid2: {SimCount: 960},
		},
	}
	aStore := newFakeQuotaAlertStore()

	p := &QuotaBreachCheckerProcessor{
		tenantStore: tStore,
		alertStore:  aStore,
		logger:      zerolog.Nop(),
	}

	res1 := runQuotaSweep(t, p)
	res2 := runQuotaSweep(t, p)

	if res1.Alerted != 2 {
		t.Errorf("sweep 1: Alerted = %d, want 2", res1.Alerted)
	}
	if res2.Alerted != 0 {
		t.Errorf("sweep 2: Alerted = %d, want 0 (all deduped)", res2.Alerted)
	}
	if res2.Deduped != 2 {
		t.Errorf("sweep 2: Deduped = %d, want 2", res2.Deduped)
	}
	if aStore.activeAlertCount() != 2 {
		t.Errorf("active alert count after 2 sweeps = %d, want 2", aStore.activeAlertCount())
	}
}

// TestQuotaBreachChecker_AutoResolveWhenBelowThreshold verifies that when a
// tenant's utilization drops below 80%, any open quota alert is resolved.
func TestQuotaBreachChecker_AutoResolveWhenBelowThreshold(t *testing.T) {
	tid := uuid.New()

	tStore := &fakeQuotaTenantStore{
		tenants: []store.Tenant{
			{ID: tid, Name: "T-drop", State: "active", MaxSims: 1000},
		},
		stats: map[uuid.UUID]*store.TenantStats{
			tid: {SimCount: 960},
		},
	}
	aStore := newFakeQuotaAlertStore()

	p := &QuotaBreachCheckerProcessor{
		tenantStore: tStore,
		alertStore:  aStore,
		logger:      zerolog.Nop(),
	}

	// First sweep: 96% → critical alert created.
	res1 := runQuotaSweep(t, p)
	if res1.Alerted != 1 {
		t.Errorf("sweep 1: Alerted = %d, want 1", res1.Alerted)
	}
	if aStore.activeAlertCount() != 1 {
		t.Fatalf("active alerts after sweep 1 = %d, want 1", aStore.activeAlertCount())
	}

	// Drop below 80%: sim count 500 → 50%.
	tStore.mu.Lock()
	tStore.stats[tid] = &store.TenantStats{SimCount: 500}
	tStore.mu.Unlock()

	// Second sweep: below threshold → critical alert resolved.
	res2 := runQuotaSweep(t, p)
	if res2.Resolved != 1 {
		t.Errorf("sweep 2: Resolved = %d, want 1", res2.Resolved)
	}
	if aStore.activeAlertCount() != 0 {
		t.Errorf("active alerts after drop = %d, want 0 (resolved)", aStore.activeAlertCount())
	}
}

// TestQuotaBreachChecker_NoAlertBelowThreshold verifies that a tenant at 50%
// generates no alerts and no resolves (nothing to resolve).
func TestQuotaBreachChecker_NoAlertBelowThreshold(t *testing.T) {
	tid := uuid.New()

	tStore := &fakeQuotaTenantStore{
		tenants: []store.Tenant{
			{ID: tid, Name: "T-ok", State: "active", MaxSims: 1000},
		},
		stats: map[uuid.UUID]*store.TenantStats{
			tid: {SimCount: 500},
		},
	}
	aStore := newFakeQuotaAlertStore()

	p := &QuotaBreachCheckerProcessor{
		tenantStore: tStore,
		alertStore:  aStore,
		logger:      zerolog.Nop(),
	}

	res := runQuotaSweep(t, p)
	if res.Alerted != 0 || res.Resolved != 0 || res.AlertFails != 0 {
		t.Errorf("unexpected activity for 50%% tenant: %+v", res)
	}
	if aStore.activeAlertCount() != 0 {
		t.Errorf("active alert count = %d, want 0", aStore.activeAlertCount())
	}
}

// TestQuotaBreachChecker_AlertSourceIsSystem is a regression guard for FIX-246
// Gate F-A1 CRITICAL: the alerts table CHECK constraint chk_alerts_source
// allows ONLY ('sim','operator','infra','policy','system'). Any insert with
// Source="quota_breach_checker" fails at runtime. This test asserts the
// processor stamps Source="system" per plan D-5 — never the legacy job-name
// value that historical drafts used.
func TestQuotaBreachChecker_AlertSourceIsSystem(t *testing.T) {
	tid := uuid.New()
	tStore := &fakeQuotaTenantStore{
		tenants: []store.Tenant{
			{ID: tid, Name: "T-breach", State: "active", MaxSims: 1000},
		},
		stats: map[uuid.UUID]*store.TenantStats{tid: {SimCount: 960}},
	}
	aStore := newFakeQuotaAlertStore()

	p := &QuotaBreachCheckerProcessor{
		tenantStore: tStore,
		alertStore:  aStore,
		logger:      zerolog.Nop(),
	}

	_ = runQuotaSweep(t, p)

	if len(aStore.calls) == 0 {
		t.Fatalf("expected at least one alert insert; got 0 calls")
	}
	for i, c := range aStore.calls {
		if c.Source != "system" {
			t.Errorf("call[%d]: Source = %q, want %q (chk_alerts_source CHECK only allows sim|operator|infra|policy|system)", i, c.Source, "system")
		}
	}
}

// TestQuotaBreachChecker_APIRPSBreachFires verifies F-A2 fix: the checker reads
// the live API RPS estimate (via EstimateAPIRPS) so api_rps quotas can breach.
// Before the fix, current was hardcoded to 0 and api_rps alerts never fired.
func TestQuotaBreachChecker_APIRPSBreachFires(t *testing.T) {
	tid := uuid.New()

	tStore := &fakeQuotaTenantStore{
		tenants: []store.Tenant{
			// MaxSims=0 → sims metric skipped; MaxAPIRPS=100, current=96 → 96% critical
			{ID: tid, Name: "T-rps", State: "active", MaxAPIRPS: 100},
		},
		stats:  map[uuid.UUID]*store.TenantStats{tid: {}},
		apiRPS: map[uuid.UUID]float64{tid: 96.0},
	}
	aStore := newFakeQuotaAlertStore()

	p := &QuotaBreachCheckerProcessor{
		tenantStore: tStore,
		alertStore:  aStore,
		logger:      zerolog.Nop(),
	}

	res := runQuotaSweep(t, p)
	if res.Alerted != 1 {
		t.Errorf("Alerted = %d, want 1 (api_rps critical)", res.Alerted)
	}
	if res.AlertFails != 0 {
		t.Errorf("AlertFails = %d, want 0", res.AlertFails)
	}

	// The single insert should target metric "api_rps" with severity critical.
	if len(aStore.calls) != 1 {
		t.Fatalf("expected 1 alert call; got %d", len(aStore.calls))
	}
	got := aStore.calls[0]
	if got.Severity != severity.Critical {
		t.Errorf("Severity = %q, want %q", got.Severity, severity.Critical)
	}
	if got.DedupKey == nil || !strings.Contains(*got.DedupKey, "api_rps") {
		var k string
		if got.DedupKey != nil {
			k = *got.DedupKey
		}
		t.Errorf("DedupKey = %q, want substring %q", k, "api_rps")
	}
}
