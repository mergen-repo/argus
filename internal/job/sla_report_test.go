package job

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// --- SLA fakes ---

type fakeTenantLister struct {
	tenants []store.Tenant
}

func (f *fakeTenantLister) List(_ context.Context, _ string, _ int, _ string) ([]store.Tenant, string, error) {
	return f.tenants, "", nil
}

type fakeOperatorQuerier struct {
	mu      sync.Mutex
	grants  map[uuid.UUID][]store.GrantWithOperator
	agg     *store.SLAAggregate
	aggCalls []struct {
		opID uuid.UUID
	}
}

func (f *fakeOperatorQuerier) ListGrantsWithOperators(_ context.Context, tenantID uuid.UUID) ([]store.GrantWithOperator, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.grants[tenantID], nil
}

func (f *fakeOperatorQuerier) AggregateHealthForSLA(_ context.Context, opID uuid.UUID, _, _ time.Time) (*store.SLAAggregate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aggCalls = append(f.aggCalls, struct{ opID uuid.UUID }{opID})
	agg := f.agg
	if agg == nil {
		agg = &store.SLAAggregate{UptimePct: 99.5, LatencyP95Ms: 120, IncidentCount: 0, MTTRSec: 0}
	}
	return agg, nil
}

type fakeRadiusCounter struct{}

func (f *fakeRadiusCounter) CountInWindow(_ context.Context, _ uuid.UUID, _, _ time.Time) (int64, error) {
	return 1000, nil
}

type fakeSLAReportCreator struct {
	mu   sync.Mutex
	rows []*store.SLAReportRow
}

func (f *fakeSLAReportCreator) Create(_ context.Context, row *store.SLAReportRow) (*store.SLAReportRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	created := *row
	created.ID = uuid.New()
	created.GeneratedAt = time.Now()
	f.rows = append(f.rows, &created)
	return &created, nil
}

func makeTenant() store.Tenant {
	return store.Tenant{
		ID:    uuid.New(),
		Name:  "test-tenant",
		State: "active",
	}
}

func makeGrant(tenantID uuid.UUID) store.GrantWithOperator {
	opID := uuid.New()
	return store.GrantWithOperator{
		OperatorGrant: store.OperatorGrant{
			ID:         uuid.New(),
			TenantID:   tenantID,
			OperatorID: opID,
			Enabled:    true,
		},
		OperatorName:  "Test Operator",
		OperatorCode:  "TST",
		HealthStatus:  "healthy",
		OperatorState: "active",
	}
}

func TestSLAReportProcessor_Type(t *testing.T) {
	p := &SLAReportProcessor{logger: zerolog.Nop()}
	if p.Type() != JobTypeSLAReport {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeSLAReport)
	}
}

func TestSLAReportProcessor_Process_Creates2Tenants2OperatorsReports(t *testing.T) {
	tenant1 := makeTenant()
	tenant2 := makeTenant()

	grant1a := makeGrant(tenant1.ID)
	grant1b := makeGrant(tenant1.ID)
	grant2a := makeGrant(tenant2.ID)
	grant2b := makeGrant(tenant2.ID)

	grantsMap := map[uuid.UUID][]store.GrantWithOperator{
		tenant1.ID: {grant1a, grant1b},
		tenant2.ID: {grant2a, grant2b},
	}

	tenantLister := &fakeTenantLister{tenants: []store.Tenant{tenant1, tenant2}}
	opQuerier := &fakeOperatorQuerier{grants: grantsMap}
	radiusCounter := &fakeRadiusCounter{}
	slaCreator := &fakeSLAReportCreator{}
	jobs := &fakeJobTracker{}
	bus := &fakeBusPublisher{}

	p := &SLAReportProcessor{
		jobs:            jobs,
		slaStore:        slaCreator,
		operatorStore:   opQuerier,
		tenantStore:     tenantLister,
		radiusSessStore: radiusCounter,
		eventBus:        bus,
		logger:          zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeSLAReport}
	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	slaCreator.mu.Lock()
	rowCount := len(slaCreator.rows)
	slaCreator.mu.Unlock()

	if rowCount != 4 {
		t.Errorf("sla_reports created = %d, want 4 (2 tenants × 2 operators)", rowCount)
	}
}

func TestSLAReportProcessor_Process_UptimePositive(t *testing.T) {
	tenant := makeTenant()
	grant := makeGrant(tenant.ID)

	tenantLister := &fakeTenantLister{tenants: []store.Tenant{tenant}}
	opQuerier := &fakeOperatorQuerier{
		grants: map[uuid.UUID][]store.GrantWithOperator{tenant.ID: {grant}},
		agg:    &store.SLAAggregate{UptimePct: 99.9, LatencyP95Ms: 50},
	}
	slaCreator := &fakeSLAReportCreator{}
	jobs := &fakeJobTracker{}
	bus := &fakeBusPublisher{}

	p := &SLAReportProcessor{
		jobs:            jobs,
		slaStore:        slaCreator,
		operatorStore:   opQuerier,
		tenantStore:     tenantLister,
		radiusSessStore: &fakeRadiusCounter{},
		eventBus:        bus,
		logger:          zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeSLAReport}
	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	slaCreator.mu.Lock()
	defer slaCreator.mu.Unlock()

	if len(slaCreator.rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(slaCreator.rows))
	}
	row := slaCreator.rows[0]
	if row.UptimePct <= 0 {
		t.Errorf("UptimePct = %v, want > 0", row.UptimePct)
	}
}

func TestSLAReportProcessor_Process_CorrectWindowTimes(t *testing.T) {
	tenant := makeTenant()
	grant := makeGrant(tenant.ID)

	now := time.Now().UTC()
	windowStart := now.Add(-24 * time.Hour)
	payloadJSON, _ := json.Marshal(map[string]interface{}{
		"window_start": windowStart.Format(time.RFC3339Nano),
		"window_end":   now.Format(time.RFC3339Nano),
	})

	tenantLister := &fakeTenantLister{tenants: []store.Tenant{tenant}}
	opQuerier := &fakeOperatorQuerier{
		grants: map[uuid.UUID][]store.GrantWithOperator{tenant.ID: {grant}},
	}
	slaCreator := &fakeSLAReportCreator{}
	jobs := &fakeJobTracker{}
	bus := &fakeBusPublisher{}

	p := &SLAReportProcessor{
		jobs:            jobs,
		slaStore:        slaCreator,
		operatorStore:   opQuerier,
		tenantStore:     tenantLister,
		radiusSessStore: &fakeRadiusCounter{},
		eventBus:        bus,
		logger:          zerolog.Nop(),
	}

	job := &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Type:     JobTypeSLAReport,
		Payload:  payloadJSON,
	}
	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	slaCreator.mu.Lock()
	defer slaCreator.mu.Unlock()

	if len(slaCreator.rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(slaCreator.rows))
	}
	row := slaCreator.rows[0]

	if row.WindowEnd.IsZero() {
		t.Error("WindowEnd is zero")
	}
	if row.WindowStart.IsZero() {
		t.Error("WindowStart is zero")
	}
	if !row.WindowStart.Before(row.WindowEnd) {
		t.Errorf("WindowStart %v is not before WindowEnd %v", row.WindowStart, row.WindowEnd)
	}
}

func TestSLAReportProcessor_Process_ResultJSON(t *testing.T) {
	tenant := makeTenant()
	grant := makeGrant(tenant.ID)

	tenantLister := &fakeTenantLister{tenants: []store.Tenant{tenant}}
	opQuerier := &fakeOperatorQuerier{
		grants: map[uuid.UUID][]store.GrantWithOperator{tenant.ID: {grant}},
	}
	slaCreator := &fakeSLAReportCreator{}
	jobs := &fakeJobTracker{}
	bus := &fakeBusPublisher{}

	p := &SLAReportProcessor{
		jobs:            jobs,
		slaStore:        slaCreator,
		operatorStore:   opQuerier,
		tenantStore:     tenantLister,
		radiusSessStore: &fakeRadiusCounter{},
		eventBus:        bus,
		logger:          zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeSLAReport}
	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	jobs.mu.Lock()
	resultJSON := jobs.result
	jobs.mu.Unlock()

	var result map[string]interface{}
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["reports_generated"] == nil {
		t.Error("reports_generated missing from result JSON")
	}
	if int(result["reports_generated"].(float64)) != 1 {
		t.Errorf("reports_generated = %v, want 1", result["reports_generated"])
	}
}

func TestSLAReportProcessor_Process_NoTenants_NoReports(t *testing.T) {
	tenantLister := &fakeTenantLister{tenants: nil}
	opQuerier := &fakeOperatorQuerier{grants: map[uuid.UUID][]store.GrantWithOperator{}}
	slaCreator := &fakeSLAReportCreator{}
	jobs := &fakeJobTracker{}
	bus := &fakeBusPublisher{}

	p := &SLAReportProcessor{
		jobs:            jobs,
		slaStore:        slaCreator,
		operatorStore:   opQuerier,
		tenantStore:     tenantLister,
		radiusSessStore: &fakeRadiusCounter{},
		eventBus:        bus,
		logger:          zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeSLAReport}
	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	slaCreator.mu.Lock()
	if len(slaCreator.rows) != 0 {
		t.Errorf("expected 0 sla rows, got %d", len(slaCreator.rows))
	}
	slaCreator.mu.Unlock()
}
