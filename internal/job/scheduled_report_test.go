package job

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fakeReportEngine struct {
	artifact *report.Artifact
	err      error
	called   int
}

func (f *fakeReportEngine) Build(_ context.Context, _ report.Request) (*report.Artifact, error) {
	f.called++
	if f.err != nil {
		return nil, f.err
	}
	return f.artifact, nil
}

type fakeReportRowStore struct {
	mu      sync.Mutex
	row     *store.ScheduledReport
	getErr  error
	updates []reportRowUpdate
}

type reportRowUpdate struct {
	id        uuid.UUID
	lastRunAt time.Time
	nextRunAt time.Time
	jobID     uuid.UUID
}

func (f *fakeReportRowStore) GetByID(_ context.Context, _ uuid.UUID) (*store.ScheduledReport, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.row, nil
}

func (f *fakeReportRowStore) UpdateLastRun(_ context.Context, id uuid.UUID, lastRunAt, nextRunAt time.Time, jobID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, reportRowUpdate{id, lastRunAt, nextRunAt, jobID})
	return nil
}

type fakeReportStorage struct {
	mu        sync.Mutex
	uploads   map[string][]byte
	presigned string
	uploadErr error
	signErr   error
}

func (f *fakeReportStorage) Upload(_ context.Context, _, key string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.uploadErr != nil {
		return f.uploadErr
	}
	if f.uploads == nil {
		f.uploads = make(map[string][]byte)
	}
	f.uploads[key] = data
	return nil
}

func (f *fakeReportStorage) PresignGet(_ context.Context, _, _ string, _ time.Duration) (string, error) {
	if f.signErr != nil {
		return "", f.signErr
	}
	return f.presigned, nil
}

type fakeReportMetrics struct {
	mu    sync.Mutex
	calls map[string]int
}

func (f *fakeReportMetrics) IncScheduledReportRun(reportType, result string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[reportType+":"+result]++
}

func newReportProc(eng *fakeReportEngine, rows *fakeReportRowStore, st *fakeReportStorage, eb *fakeBusPublisher, jobs *fakeJobTracker, met *fakeReportMetrics) *ScheduledReportProcessor {
	return &ScheduledReportProcessor{
		jobs:     jobs,
		rows:     rows,
		engine:   eng,
		storage:  st,
		eventBus: eb,
		metrics:  met,
		now:      func() time.Time { return time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC) },
		logger:   zerolog.Nop(),
	}
}

func TestScheduledReportProcessor_Type(t *testing.T) {
	p := &ScheduledReportProcessor{logger: zerolog.Nop()}
	if p.Type() != JobTypeScheduledReportRun {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeScheduledReportRun)
	}
}

func TestScheduledReport_OnDemandProducesArtifact(t *testing.T) {
	eng := &fakeReportEngine{artifact: &report.Artifact{
		Bytes:    []byte("col1,col2\n1,2"),
		MIME:     "text/csv",
		Filename: "ondemand.csv",
	}}
	st := &fakeReportStorage{presigned: "https://signed/url"}
	jobs := &fakeJobTracker{}
	met := &fakeReportMetrics{}
	eb := &fakeBusPublisher{}
	rows := &fakeReportRowStore{}
	p := newReportProc(eng, rows, st, eb, jobs, met)

	tenantID := uuid.New()
	jobID := uuid.New()
	payload, _ := json.Marshal(map[string]any{
		"report_type":  "usage_summary",
		"format":       "csv",
		"tenant_id":    tenantID.String(),
		"requested_by": uuid.New().String(),
	})
	if err := p.Process(context.Background(), &store.Job{ID: jobID, TenantID: tenantID, Type: JobTypeScheduledReportRun, Payload: payload}); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if eng.called != 1 {
		t.Errorf("engine.Build called %d times, want 1", eng.called)
	}
	if len(st.uploads) != 1 {
		t.Errorf("uploads = %d, want 1", len(st.uploads))
	}
	if met.calls["usage_summary:succeeded"] != 1 {
		t.Errorf("metric usage_summary:succeeded = %d, want 1", met.calls["usage_summary:succeeded"])
	}
	if len(rows.updates) != 0 {
		t.Errorf("on-demand should not update scheduled row, got %d updates", len(rows.updates))
	}
}

func TestScheduledReport_ScheduledLoadsRowAndAdvancesNextRun(t *testing.T) {
	tenantID := uuid.New()
	schedID := uuid.New()
	rows := &fakeReportRowStore{row: &store.ScheduledReport{
		ID:           schedID,
		TenantID:     tenantID,
		ReportType:   "compliance_kvkk",
		ScheduleCron: "@daily",
		Format:       "pdf",
		Recipients:   []string{"ops@example.com"},
		Filters:      json.RawMessage(`{"foo":"bar"}`),
	}}

	eng := &fakeReportEngine{artifact: &report.Artifact{
		Bytes:    []byte("%PDF-1.4..."),
		MIME:     "application/pdf",
		Filename: "kvkk.pdf",
	}}
	st := &fakeReportStorage{presigned: "https://signed/url"}
	jobs := &fakeJobTracker{}
	met := &fakeReportMetrics{}
	eb := &fakeBusPublisher{}
	p := newReportProc(eng, rows, st, eb, jobs, met)

	payload, _ := json.Marshal(map[string]string{"scheduled_report_id": schedID.String()})
	jobID := uuid.New()
	if err := p.Process(context.Background(), &store.Job{ID: jobID, TenantID: tenantID, Type: JobTypeScheduledReportRun, Payload: payload}); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if len(rows.updates) != 1 {
		t.Fatalf("expected 1 row update, got %d", len(rows.updates))
	}
	if rows.updates[0].id != schedID {
		t.Errorf("update id = %v, want %v", rows.updates[0].id, schedID)
	}
	if rows.updates[0].jobID != jobID {
		t.Errorf("update jobID = %v, want %v", rows.updates[0].jobID, jobID)
	}
	if !rows.updates[0].nextRunAt.After(time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("nextRunAt = %v, want > 2026-04-13T12:00:00Z", rows.updates[0].nextRunAt)
	}

	gotNotification := false
	for _, ev := range eb.events {
		if m, ok := ev.payload.(map[string]any); ok {
			if m["event_type"] == "report_ready" {
				gotNotification = true
				break
			}
		}
	}
	if !gotNotification {
		t.Errorf("expected report_ready notification published, got events=%v", eb.events)
	}
}

func TestScheduledReport_BuildFailureMarksJobFailed(t *testing.T) {
	eng := &fakeReportEngine{err: errors.New("boom")}
	st := &fakeReportStorage{}
	jobs := &fakeJobTracker{}
	met := &fakeReportMetrics{}
	rows := &fakeReportRowStore{}
	p := newReportProc(eng, rows, st, &fakeBusPublisher{}, jobs, met)

	payload, _ := json.Marshal(map[string]any{
		"report_type": "usage_summary",
		"format":      "csv",
		"tenant_id":   uuid.New().String(),
	})
	err := p.Process(context.Background(), &store.Job{ID: uuid.New(), TenantID: uuid.New(), Payload: payload})
	if err == nil {
		t.Fatal("Process should return error on engine failure")
	}
	if met.calls["usage_summary:failed"] != 1 {
		t.Errorf("expected failed metric, got %v", met.calls)
	}
}

// ─── Sweeper ──────────────────────────────────────────────────────────────────

type fakeReportLister struct {
	due []*store.ScheduledReport
	err error
}

func (f *fakeReportLister) ListDue(_ context.Context, _ time.Time, _ int) ([]*store.ScheduledReport, error) {
	return f.due, f.err
}

type fakeReportEnqueuer struct {
	mu    sync.Mutex
	calls []store.CreateJobParams
}

func (f *fakeReportEnqueuer) CreateWithTenantID(_ context.Context, tenantID uuid.UUID, p store.CreateJobParams) (*store.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)
	return &store.Job{ID: uuid.New(), TenantID: tenantID, Type: p.Type, Payload: p.Payload}, nil
}

func TestScheduledReportSweeper_EnqueuesEachDueRow(t *testing.T) {
	t1 := uuid.New()
	t2 := uuid.New()
	lister := &fakeReportLister{due: []*store.ScheduledReport{
		{ID: uuid.New(), TenantID: t1, ScheduleCron: "@daily"},
		{ID: uuid.New(), TenantID: t2, ScheduleCron: "*/5 * * * *"},
	}}
	enq := &fakeReportEnqueuer{}
	jobs := &fakeJobTracker{}
	eb := &fakeBusPublisher{}

	p := &ScheduledReportSweeper{
		jobs:     jobs,
		rows:     lister,
		enqueue:  enq,
		eventBus: eb,
		now:      func() time.Time { return time.Now().UTC() },
		logger:   zerolog.Nop(),
	}

	if err := p.Process(context.Background(), &store.Job{ID: uuid.New(), Type: JobTypeScheduledReportSweeper}); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if len(enq.calls) != 2 {
		t.Errorf("enqueue called %d times, want 2", len(enq.calls))
	}
	for _, c := range enq.calls {
		if c.Type != JobTypeScheduledReportRun {
			t.Errorf("enqueued type = %q, want %q", c.Type, JobTypeScheduledReportRun)
		}
	}
}
