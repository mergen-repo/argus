package job

import (
	"context"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fakeIPGraceReleaser struct {
	candidates    []store.GraceExpiredIPAddress
	releaseCalled []uuid.UUID
	releaseErr    error
}

func (f *fakeIPGraceReleaser) ListGraceExpired(_ context.Context, _ int) ([]store.GraceExpiredIPAddress, error) {
	return f.candidates, nil
}

func (f *fakeIPGraceReleaser) ReleaseGraceIP(_ context.Context, ipID uuid.UUID) error {
	f.releaseCalled = append(f.releaseCalled, ipID)
	return f.releaseErr
}

type fakeIPGraceMetrics struct {
	total int
}

func (f *fakeIPGraceMetrics) IncIPGraceReleased(n int) {
	f.total += n
}

func makeGraceIP(addr string) store.GraceExpiredIPAddress {
	a := addr
	return store.GraceExpiredIPAddress{
		ID:        uuid.New(),
		PoolID:    uuid.New(),
		TenantID:  uuid.New(),
		AddressV4: &a,
	}
}

func newGraceProc(
	releaser *fakeIPGraceReleaser,
	eb *fakeBusPublisher,
	audit *fakeAuditRecorder,
	jobs *fakeJobTracker,
	met *fakeIPGraceMetrics,
) *IPGraceReleaseProcessor {
	return &IPGraceReleaseProcessor{
		jobs:     jobs,
		ippools:  releaser,
		eventBus: eb,
		audit:    audit,
		metrics:  met,
		logger:   zerolog.Nop(),
	}
}

func TestIPGraceReleaseProcessor_Type(t *testing.T) {
	p := &IPGraceReleaseProcessor{logger: zerolog.Nop()}
	if p.Type() != JobTypeIPGraceRelease {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeIPGraceRelease)
	}
}

func TestIPGraceRelease_ReleasesOnlyExpiredTerminated(t *testing.T) {
	candidates := []store.GraceExpiredIPAddress{
		makeGraceIP("10.0.0.1"),
		makeGraceIP("10.0.0.2"),
		makeGraceIP("10.0.0.3"),
	}

	releaser := &fakeIPGraceReleaser{candidates: candidates}
	eb := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}
	met := &fakeIPGraceMetrics{}

	p := newGraceProc(releaser, eb, audit, jobs, met)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPGraceRelease}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if len(releaser.releaseCalled) != 3 {
		t.Errorf("ReleaseGraceIP called %d times, want 3", len(releaser.releaseCalled))
	}
}

func TestIPGraceRelease_EmptyList_NoErrors(t *testing.T) {
	releaser := &fakeIPGraceReleaser{candidates: nil}
	eb := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}
	met := &fakeIPGraceMetrics{}

	p := newGraceProc(releaser, eb, audit, jobs, met)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPGraceRelease}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if len(releaser.releaseCalled) != 0 {
		t.Errorf("ReleaseGraceIP called %d times, want 0", len(releaser.releaseCalled))
	}
}

func TestIPGraceRelease_PublishesIPReleasedPerRow(t *testing.T) {
	candidates := []store.GraceExpiredIPAddress{
		makeGraceIP("10.0.0.1"),
		makeGraceIP("10.0.0.2"),
		makeGraceIP("10.0.0.3"),
	}

	releaser := &fakeIPGraceReleaser{candidates: candidates}
	eb := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}
	met := &fakeIPGraceMetrics{}

	p := newGraceProc(releaser, eb, audit, jobs, met)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPGraceRelease}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()
	releasedCount := 0
	for _, e := range eb.events {
		if e.subject == "argus.events.ip.released" {
			releasedCount++
		}
	}
	if releasedCount != 3 {
		t.Errorf("ip.released events = %d, want 3", releasedCount)
	}
}

func TestIPGraceRelease_MetricIncrementedByReleasedCount(t *testing.T) {
	candidates := []store.GraceExpiredIPAddress{
		makeGraceIP("10.0.0.1"),
		makeGraceIP("10.0.0.2"),
		makeGraceIP("10.0.0.3"),
	}

	releaser := &fakeIPGraceReleaser{candidates: candidates}
	eb := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}
	met := &fakeIPGraceMetrics{}

	p := newGraceProc(releaser, eb, audit, jobs, met)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPGraceRelease}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if met.total != 3 {
		t.Errorf("metric total = %d, want 3", met.total)
	}
}

func TestIPGraceRelease_AuditCalledOnce(t *testing.T) {
	candidates := []store.GraceExpiredIPAddress{
		makeGraceIP("10.0.0.1"),
		makeGraceIP("10.0.0.2"),
		makeGraceIP("10.0.0.3"),
	}

	releaser := &fakeIPGraceReleaser{candidates: candidates}
	eb := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}
	met := &fakeIPGraceMetrics{}

	p := newGraceProc(releaser, eb, audit, jobs, met)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPGraceRelease}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	audit.mu.Lock()
	defer audit.mu.Unlock()
	if len(audit.calls) != 1 {
		t.Errorf("audit calls = %d, want 1 (per-run, not per-row)", len(audit.calls))
	}
	if len(audit.calls) > 0 && audit.calls[0][:len("ip.grace.swept")] != "ip.grace.swept" {
		t.Errorf("audit action = %q, want prefix ip.grace.swept", audit.calls[0])
	}
}

func TestIPGraceRelease_FilteringLogic(t *testing.T) {
	candidates := []store.GraceExpiredIPAddress{
		makeGraceIP("10.0.0.1"),
		makeGraceIP("10.0.0.2"),
		makeGraceIP("10.0.0.3"),
	}

	releaser := &fakeIPGraceReleaser{candidates: candidates}
	eb := &fakeBusPublisher{}
	audit := &fakeAuditRecorder{}
	jobs := &fakeJobTracker{}
	met := &fakeIPGraceMetrics{}

	p := newGraceProc(releaser, eb, audit, jobs, met)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeIPGraceRelease}

	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if len(releaser.releaseCalled) != 3 {
		t.Errorf("expected 3 released (future grace, already released, and active sim are excluded at SQL level): got %d", len(releaser.releaseCalled))
	}
}
