package job

// STORY-069 e2e — exercises the new cron+processor surface end-to-end using
// in-package fakes. No DB required: runs as a normal `go test` so it lands in
// CI without integration scaffolding.
//
// Scenarios:
//   1. Webhook retry sweep — succeeds on retry, persists succeeded state
//   2. Scheduled-report sweeper enqueues run job per due row
//   3. Scheduled-report processor uploads artifact + emits report_ready
//
// The data-portability and SMS gateway processors already have dedicated tests;
// this file focuses on the multi-step cron→processor chain that ties them
// together.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// TestSTORY069_WebhookRetryToScheduledReport_HappyPath threads three
// processors together to assert that:
//   - a webhook retry succeeds and finalises the row
//   - the scheduled-report sweeper enqueues a run job
//   - the run job uploads an artifact and publishes report_ready
func TestSTORY069_WebhookRetryToScheduledReport_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// ── 1: Webhook retry sweep ────────────────────────────────────────────
	deliveryID := uuid.New()
	cfgID := uuid.New()
	tenantID := uuid.New()

	ds := &fakeWebhookDeliveryStore{
		due: []*store.WebhookDelivery{{
			ID:             deliveryID,
			ConfigID:       cfgID,
			TenantID:       tenantID,
			EventType:      "session.ended",
			PayloadPreview: `{"sim_id":"abc"}`,
			AttemptCount:   2,
			FinalState:     "retrying",
		}},
	}
	cs := &fakeWebhookConfigStore{cfg: &store.WebhookConfig{
		ID: cfgID, TenantID: tenantID, URL: srv.URL, Secret: "s",
	}}
	jobs := &fakeJobTracker{}
	met := &fakeWebhookMetrics{}
	bus := &fakeBusPublisher{}

	retryProc := newRetryProc(ds, cs, bus, jobs, met, srv.Client())
	if err := retryProc.Process(context.Background(), &store.Job{ID: uuid.New(), Type: JobTypeWebhookRetry}); err != nil {
		t.Fatalf("webhook retry: %v", err)
	}
	if got := ds.finals[deliveryID]; got != "succeeded" {
		t.Errorf("delivery final = %q, want succeeded", got)
	}

	// ── 2: Scheduled-report sweeper enqueues run job ──────────────────────
	schedID := uuid.New()
	lister := &fakeReportLister{due: []*store.ScheduledReport{{
		ID:           schedID,
		TenantID:     tenantID,
		ReportType:   "compliance_kvkk",
		ScheduleCron: "@daily",
		Format:       "pdf",
		Recipients:   []string{"ops@example.com"},
	}}}
	enqueue := &fakeReportEnqueuer{}
	sweeper := &ScheduledReportSweeper{
		jobs:     jobs,
		rows:     lister,
		enqueue:  enqueue,
		eventBus: bus,
		now:      func() time.Time { return time.Now().UTC() },
		logger:   zerolog.Nop(),
	}
	if err := sweeper.Process(context.Background(), &store.Job{ID: uuid.New(), Type: JobTypeScheduledReportSweeper}); err != nil {
		t.Fatalf("sweeper: %v", err)
	}
	if len(enqueue.calls) != 1 {
		t.Fatalf("expected sweeper to enqueue 1 run job, got %d", len(enqueue.calls))
	}
	enqueuedPayload := enqueue.calls[0].Payload

	// ── 3: Scheduled-report processor builds + uploads + notifies ─────────
	rows := &fakeReportRowStore{row: &store.ScheduledReport{
		ID:           schedID,
		TenantID:     tenantID,
		ReportType:   "compliance_kvkk",
		ScheduleCron: "@daily",
		Format:       "pdf",
		Recipients:   []string{"ops@example.com"},
	}}
	eng := &fakeReportEngine{artifact: &report.Artifact{
		Bytes:    []byte("%PDF stub"),
		MIME:     "application/pdf",
		Filename: "kvkk.pdf",
	}}
	storage := &fakeReportStorage{presigned: "https://signed/url"}
	repMet := &fakeReportMetrics{}
	repProc := newReportProc(eng, rows, storage, bus, jobs, repMet)
	if err := repProc.Process(context.Background(), &store.Job{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     JobTypeScheduledReportRun,
		Payload:  enqueuedPayload,
	}); err != nil {
		t.Fatalf("report run: %v", err)
	}

	if eng.called != 1 {
		t.Errorf("engine.Build called %d times, want 1", eng.called)
	}
	if len(storage.uploads) != 1 {
		t.Errorf("expected 1 upload, got %d", len(storage.uploads))
	}
	if repMet.calls["compliance_kvkk:succeeded"] != 1 {
		t.Errorf("expected metric inc, got %v", repMet.calls)
	}

	// Validate that the report_ready notification went out alongside the
	// other published events.
	gotReportReady := false
	for _, ev := range bus.events {
		if m, ok := ev.payload.(map[string]any); ok {
			if m["event_type"] == "report_ready" && m["tenant_id"] == tenantID.String() {
				gotReportReady = true
			}
		}
	}
	if !gotReportReady {
		t.Errorf("expected report_ready notification to be published")
	}

	// Validate scheduled row was advanced.
	if len(rows.updates) != 1 {
		t.Errorf("expected scheduled row update, got %d", len(rows.updates))
	}

	// Sanity-check the result JSON shape.
	if jobs.result == nil {
		t.Fatal("expected processor to write a result")
	}
	var result scheduledReportResult
	if err := json.Unmarshal(jobs.result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Format != "pdf" || result.ReportType != "compliance_kvkk" {
		t.Errorf("unexpected result %+v", result)
	}
}
