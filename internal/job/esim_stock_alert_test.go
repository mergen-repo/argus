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

// fakeStockAlerterTenantStore satisfies stockAlerterTenantStore for tests.
type fakeStockAlerterTenantStore struct {
	mu      sync.Mutex
	tenants []store.Tenant
}

func (f *fakeStockAlerterTenantStore) List(_ context.Context, _ string, _ int, _ string) ([]store.Tenant, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Tenant(nil), f.tenants...), "", nil
}

// fakeStockAlerterStockStore satisfies stockAlerterStockStore for tests.
type fakeStockAlerterStockStore struct {
	mu     sync.Mutex
	stocks map[uuid.UUID][]store.EsimProfileStock
}

func (f *fakeStockAlerterStockStore) ListSummary(_ context.Context, tenantID uuid.UUID) ([]store.EsimProfileStock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.EsimProfileStock(nil), f.stocks[tenantID]...), nil
}

// fakeStockAlerterAlertStore satisfies stockAlerterAlertStore for tests with in-memory dedup.
type fakeStockAlerterAlertStore struct {
	mu     sync.Mutex
	alerts map[string]*store.Alert
	calls  []store.CreateAlertParams
}

func newFakeStockAlerterAlertStore() *fakeStockAlerterAlertStore {
	return &fakeStockAlerterAlertStore{
		alerts: make(map[string]*store.Alert),
	}
}

func (f *fakeStockAlerterAlertStore) UpsertWithDedup(_ context.Context, p store.CreateAlertParams, _ int) (*store.Alert, store.UpsertResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)

	if p.DedupKey == nil {
		a := &store.Alert{ID: uuid.New(), TenantID: p.TenantID, Type: p.Type}
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

func (f *fakeStockAlerterAlertStore) FindActiveByDedupKey(_ context.Context, tenantID uuid.UUID, dedupKey string) (*store.Alert, error) {
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

func (f *fakeStockAlerterAlertStore) UpdateState(_ context.Context, tenantID, id uuid.UUID, newState string, _ *uuid.UUID, _ int) (*store.Alert, error) {
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

func (f *fakeStockAlerterAlertStore) activeAlertCount() int {
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

func newTestStockAlerter(
	tStore stockAlerterTenantStore,
	sStore stockAlerterStockStore,
	aStore stockAlerterAlertStore,
) *ESimStockAlerterProcessor {
	return &ESimStockAlerterProcessor{
		stockStore:  sStore,
		tenantStore: tStore,
		alertStore:  aStore,
		logger:      zerolog.Nop(),
	}
}

func runStockSweep(t *testing.T, p *ESimStockAlerterProcessor) stockAlerterResult {
	t.Helper()
	res, err := p.runSweep(context.Background())
	if err != nil {
		t.Fatalf("runSweep error: %v", err)
	}
	return res
}

func TestESimStockAlerter_Type(t *testing.T) {
	p := &ESimStockAlerterProcessor{}
	if p.Type() != JobTypeESimStockAlerter {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeESimStockAlerter)
	}
	if JobTypeESimStockAlerter != "esim_stock_alerter" {
		t.Errorf("JobTypeESimStockAlerter = %q", JobTypeESimStockAlerter)
	}
	found := false
	for _, jt := range AllJobTypes {
		if jt == JobTypeESimStockAlerter {
			found = true
			break
		}
	}
	if !found {
		t.Error("JobTypeESimStockAlerter not found in AllJobTypes")
	}
}

// TestESimStockAlerter_AlertSourceIsSystem is the mandatory PAT-024 regression guard.
// Every UpsertWithDedup call must carry Source="system".
func TestESimStockAlerter_AlertSourceIsSystem(t *testing.T) {
	tid := uuid.New()
	opID := uuid.New()

	tStore := &fakeStockAlerterTenantStore{
		tenants: []store.Tenant{{ID: tid, Name: "T-low", State: "active"}},
	}
	sStore := &fakeStockAlerterStockStore{
		stocks: map[uuid.UUID][]store.EsimProfileStock{
			tid: {
				{TenantID: tid, OperatorID: opID, Total: 100, Available: 5},
			},
		},
	}
	aStore := newFakeStockAlerterAlertStore()

	p := newTestStockAlerter(tStore, sStore, aStore)
	_ = runStockSweep(t, p)

	aStore.mu.Lock()
	defer aStore.mu.Unlock()
	if len(aStore.calls) == 0 {
		t.Fatalf("expected at least one alert insert; got 0 calls")
	}
	for i, c := range aStore.calls {
		if c.Source != "system" {
			t.Errorf("call[%d]: Source = %q, want %q (chk_alerts_source CHECK only allows sim|operator|infra|policy|system)",
				i, c.Source, "system")
		}
	}
}

// TestESimStockAlerter_LowStock_MediumAlert verifies that 8% available → medium alert.
func TestESimStockAlerter_LowStock_MediumAlert(t *testing.T) {
	tid := uuid.New()
	opID := uuid.New()

	tStore := &fakeStockAlerterTenantStore{
		tenants: []store.Tenant{{ID: tid, Name: "T-medium", State: "active"}},
	}
	sStore := &fakeStockAlerterStockStore{
		stocks: map[uuid.UUID][]store.EsimProfileStock{
			tid: {{TenantID: tid, OperatorID: opID, Total: 100, Available: 8}},
		},
	}
	aStore := newFakeStockAlerterAlertStore()

	p := newTestStockAlerter(tStore, sStore, aStore)
	res := runStockSweep(t, p)

	if res.Alerted != 1 {
		t.Errorf("Alerted = %d, want 1", res.Alerted)
	}
	aStore.mu.Lock()
	defer aStore.mu.Unlock()
	if len(aStore.calls) != 1 || aStore.calls[0].Severity != severity.Medium {
		t.Errorf("expected medium alert; calls=%+v", aStore.calls)
	}
}

// TestESimStockAlerter_CriticallyLow_HighAlert verifies that 3% available → high alert.
func TestESimStockAlerter_CriticallyLow_HighAlert(t *testing.T) {
	tid := uuid.New()
	opID := uuid.New()

	tStore := &fakeStockAlerterTenantStore{
		tenants: []store.Tenant{{ID: tid, Name: "T-critical", State: "active"}},
	}
	sStore := &fakeStockAlerterStockStore{
		stocks: map[uuid.UUID][]store.EsimProfileStock{
			tid: {{TenantID: tid, OperatorID: opID, Total: 100, Available: 3}},
		},
	}
	aStore := newFakeStockAlerterAlertStore()

	p := newTestStockAlerter(tStore, sStore, aStore)
	res := runStockSweep(t, p)

	if res.Alerted != 1 {
		t.Errorf("Alerted = %d, want 1", res.Alerted)
	}
	aStore.mu.Lock()
	defer aStore.mu.Unlock()
	if len(aStore.calls) != 1 || aStore.calls[0].Severity != severity.High {
		t.Errorf("expected high alert; calls=%+v", aStore.calls)
	}
}

// TestESimStockAlerter_HealthyStock_NoAlert verifies that 50% → no alert.
func TestESimStockAlerter_HealthyStock_NoAlert(t *testing.T) {
	tid := uuid.New()
	opID := uuid.New()

	tStore := &fakeStockAlerterTenantStore{
		tenants: []store.Tenant{{ID: tid, Name: "T-ok", State: "active"}},
	}
	sStore := &fakeStockAlerterStockStore{
		stocks: map[uuid.UUID][]store.EsimProfileStock{
			tid: {{TenantID: tid, OperatorID: opID, Total: 100, Available: 50}},
		},
	}
	aStore := newFakeStockAlerterAlertStore()

	p := newTestStockAlerter(tStore, sStore, aStore)
	res := runStockSweep(t, p)

	if res.Alerted != 0 || res.AlertFails != 0 {
		t.Errorf("expected no alerts for healthy stock; res=%+v", res)
	}
}

// TestESimStockAlerter_AutoResolveOnRecovery verifies that when stock recovers to ≥10%,
// any open alert is resolved.
func TestESimStockAlerter_AutoResolveOnRecovery(t *testing.T) {
	tid := uuid.New()
	opID := uuid.New()

	tStore := &fakeStockAlerterTenantStore{
		tenants: []store.Tenant{{ID: tid, Name: "T-recover", State: "active"}},
	}
	sStore := &fakeStockAlerterStockStore{
		stocks: map[uuid.UUID][]store.EsimProfileStock{
			tid: {{TenantID: tid, OperatorID: opID, Total: 100, Available: 8}},
		},
	}
	aStore := newFakeStockAlerterAlertStore()

	p := newTestStockAlerter(tStore, sStore, aStore)

	res1 := runStockSweep(t, p)
	if res1.Alerted != 1 {
		t.Fatalf("sweep 1: Alerted = %d, want 1", res1.Alerted)
	}

	// Restore stock to 50% (above threshold).
	sStore.mu.Lock()
	sStore.stocks[tid] = []store.EsimProfileStock{
		{TenantID: tid, OperatorID: opID, Total: 100, Available: 50},
	}
	sStore.mu.Unlock()

	res2 := runStockSweep(t, p)
	if res2.Resolved != 1 {
		t.Errorf("sweep 2: Resolved = %d, want 1", res2.Resolved)
	}
	if aStore.activeAlertCount() != 0 {
		t.Errorf("active alerts after recovery = %d, want 0", aStore.activeAlertCount())
	}
}

// TestESimStockAlerter_Deduplication verifies second sweep on same low stock deduplicates.
func TestESimStockAlerter_Deduplication(t *testing.T) {
	tid := uuid.New()
	opID := uuid.New()

	tStore := &fakeStockAlerterTenantStore{
		tenants: []store.Tenant{{ID: tid, Name: "T-dedup", State: "active"}},
	}
	sStore := &fakeStockAlerterStockStore{
		stocks: map[uuid.UUID][]store.EsimProfileStock{
			tid: {{TenantID: tid, OperatorID: opID, Total: 100, Available: 8}},
		},
	}
	aStore := newFakeStockAlerterAlertStore()

	p := newTestStockAlerter(tStore, sStore, aStore)

	res1 := runStockSweep(t, p)
	res2 := runStockSweep(t, p)

	if res1.Alerted != 1 {
		t.Errorf("sweep 1: Alerted = %d, want 1", res1.Alerted)
	}
	if res2.Alerted != 0 {
		t.Errorf("sweep 2: Alerted = %d, want 0 (deduped)", res2.Alerted)
	}
	if res2.Deduped != 1 {
		t.Errorf("sweep 2: Deduped = %d, want 1", res2.Deduped)
	}
}

// TestESimStockAlerter_DedupKeyFormat verifies the dedup key contains both tenant_id and operator_id.
func TestESimStockAlerter_DedupKeyFormat(t *testing.T) {
	tid := uuid.New()
	opID := uuid.New()

	tStore := &fakeStockAlerterTenantStore{
		tenants: []store.Tenant{{ID: tid, Name: "T-key", State: "active"}},
	}
	sStore := &fakeStockAlerterStockStore{
		stocks: map[uuid.UUID][]store.EsimProfileStock{
			tid: {{TenantID: tid, OperatorID: opID, Total: 100, Available: 5}},
		},
	}
	aStore := newFakeStockAlerterAlertStore()

	p := newTestStockAlerter(tStore, sStore, aStore)
	_ = runStockSweep(t, p)

	aStore.mu.Lock()
	defer aStore.mu.Unlock()
	if len(aStore.calls) == 0 {
		t.Fatal("expected at least 1 alert call")
	}
	key := aStore.calls[0].DedupKey
	if key == nil {
		t.Fatal("DedupKey is nil")
	}
	if !strings.Contains(*key, tid.String()) {
		t.Errorf("DedupKey %q does not contain tenant_id %q", *key, tid.String())
	}
	if !strings.Contains(*key, opID.String()) {
		t.Errorf("DedupKey %q does not contain operator_id %q", *key, opID.String())
	}
	if !strings.HasPrefix(*key, "esim_stock:") {
		t.Errorf("DedupKey %q does not start with esim_stock:", *key)
	}
}
