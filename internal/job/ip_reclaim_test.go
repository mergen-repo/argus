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

// --- fakes ---

type fakeIPPoolReclaimer struct {
	mu             sync.Mutex
	expired        []store.ExpiredIPAddress
	finalizeErr    error
	finalizeCalled []uuid.UUID
}

func (f *fakeIPPoolReclaimer) ListExpiredReclaim(_ context.Context, _ time.Time, _ int) ([]store.ExpiredIPAddress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.expired, nil
}

func (f *fakeIPPoolReclaimer) FinalizeReclaim(_ context.Context, ipID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finalizeCalled = append(f.finalizeCalled, ipID)
	return f.finalizeErr
}

type fakeJobTracker struct {
	mu        sync.Mutex
	completed []uuid.UUID
	result    json.RawMessage
}

func (f *fakeJobTracker) UpdateProgress(_ context.Context, _ uuid.UUID, _, _, _ int) error {
	return nil
}

func (f *fakeJobTracker) CheckCancelled(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeJobTracker) Complete(_ context.Context, jobID uuid.UUID, _ json.RawMessage, result json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, jobID)
	f.result = result
	return nil
}

type fakeBusPublisher struct {
	mu     sync.Mutex
	events []struct {
		subject string
		payload interface{}
	}
}

func (f *fakeBusPublisher) Publish(_ context.Context, subject string, payload interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, struct {
		subject string
		payload interface{}
	}{subject, payload})
	return nil
}

type fakeAuditRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeAuditRecorder) Record(_ context.Context, _ uuid.UUID, action, _, entityID string, _, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action+":"+entityID)
	return nil
}

func makeTestExpiredIP(addrV4 string) store.ExpiredIPAddress {
	sim := uuid.New()
	return store.ExpiredIPAddress{
		ID:            uuid.New(),
		PoolID:        uuid.New(),
		TenantID:      uuid.New(),
		AddressV4:     &addrV4,
		PreviousSimID: &sim,
		ReclaimAt:     time.Now().Add(-1 * time.Hour),
	}
}

func newIPReclaimProc(pool *fakeIPPoolReclaimer, bus *fakeBusPublisher, audit *fakeAuditRecorder, jobs *fakeJobTracker) *IPReclaimProcessor {
	return &IPReclaimProcessor{
		jobs:     jobs,
		ippools:  pool,
		eventBus: bus,
		audit:    audit,
		logger:   zerolog.Nop(),
	}
}

func TestIPReclaimProcessor_Type(t *testing.T) {
	p := &IPReclaimProcessor{logger: zerolog.Nop()}
	if p.Type() != JobTypeIPReclaim {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeIPReclaim)
	}
}

func TestIPReclaimProcessor_Process_FinalizeCalledNTimes(t *testing.T) {
	const n = 3
	expired := []store.ExpiredIPAddress{
		makeTestExpiredIP("10.0.0.1"),
		makeTestExpiredIP("10.0.0.2"),
		makeTestExpiredIP("10.0.0.3"),
	}

	pool := &fakeIPPoolReclaimer{expired: expired}
	bus := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}

	p := newIPReclaimProc(pool, bus, audit, jobs)

	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPReclaim}
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	pool.mu.Lock()
	if len(pool.finalizeCalled) != n {
		t.Errorf("FinalizeReclaim called %d times, want %d", len(pool.finalizeCalled), n)
	}
	pool.mu.Unlock()
}

func TestIPReclaimProcessor_Process_PublishesEventsPerIP(t *testing.T) {
	const n = 4
	expired := []store.ExpiredIPAddress{
		makeTestExpiredIP("192.168.0.1"),
		makeTestExpiredIP("192.168.0.2"),
		makeTestExpiredIP("192.168.0.3"),
		makeTestExpiredIP("192.168.0.4"),
	}

	pool := &fakeIPPoolReclaimer{expired: expired}
	bus := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}

	p := newIPReclaimProc(pool, bus, audit, jobs)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPReclaim}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()
	ipReclaimedCount := 0
	for _, e := range bus.events {
		if e.subject == "argus.events.ip.reclaimed" {
			ipReclaimedCount++
		}
	}
	if ipReclaimedCount != n {
		t.Errorf("ip.reclaimed events = %d, want %d", ipReclaimedCount, n)
	}
}

func TestIPReclaimProcessor_Process_AuditCalledPerIP(t *testing.T) {
	expired := []store.ExpiredIPAddress{
		makeTestExpiredIP("172.16.0.1"),
		makeTestExpiredIP("172.16.0.2"),
	}

	pool := &fakeIPPoolReclaimer{expired: expired}
	bus := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}

	p := newIPReclaimProc(pool, bus, audit, jobs)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPReclaim}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	audit.mu.Lock()
	if len(audit.calls) != 2 {
		t.Errorf("audit calls = %d, want 2", len(audit.calls))
	}
	audit.mu.Unlock()
}

func TestIPReclaimProcessor_Process_EmptyList_NoErrors(t *testing.T) {
	pool := &fakeIPPoolReclaimer{expired: nil}
	bus := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}

	p := newIPReclaimProc(pool, bus, audit, jobs)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPReclaim}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pool.mu.Lock()
	if len(pool.finalizeCalled) != 0 {
		t.Errorf("FinalizeReclaim called %d times, want 0", len(pool.finalizeCalled))
	}
	pool.mu.Unlock()
}

func TestIPReclaimProcessor_Process_ResultJSON(t *testing.T) {
	expired := []store.ExpiredIPAddress{
		makeTestExpiredIP("10.1.1.1"),
		makeTestExpiredIP("10.1.1.2"),
	}

	pool := &fakeIPPoolReclaimer{expired: expired}
	bus := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}

	p := newIPReclaimProc(pool, bus, audit, jobs)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPReclaim}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	jobs.mu.Lock()
	resultJSON := jobs.result
	jobs.mu.Unlock()

	var result map[string]interface{}
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if int(result["total"].(float64)) != 2 {
		t.Errorf("total = %v, want 2", result["total"])
	}
	if int(result["reclaimed"].(float64)) != 2 {
		t.Errorf("reclaimed = %v, want 2", result["reclaimed"])
	}
}
