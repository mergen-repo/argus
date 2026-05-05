package job

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fakeWebhookDeliveryStore struct {
	mu        sync.Mutex
	due       []*store.WebhookDelivery
	updates   []deliveryUpdate
	finals    map[uuid.UUID]string
	updateErr error
	markErr   error
}

type deliveryUpdate struct {
	id           uuid.UUID
	attemptCount int
	nextRetryAt  *time.Time
	respStatus   *int
}

func (f *fakeWebhookDeliveryStore) ListDueForRetry(_ context.Context, _ time.Time, _ int) ([]*store.WebhookDelivery, error) {
	return f.due, nil
}

func (f *fakeWebhookDeliveryStore) UpdateAttempt(_ context.Context, id uuid.UUID, attemptCount int, nextRetryAt *time.Time, respStatus *int, _ *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, deliveryUpdate{id, attemptCount, nextRetryAt, respStatus})
	return f.updateErr
}

func (f *fakeWebhookDeliveryStore) MarkFinal(_ context.Context, id uuid.UUID, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finals == nil {
		f.finals = make(map[uuid.UUID]string)
	}
	f.finals[id] = state
	return f.markErr
}

type fakeWebhookConfigStore struct {
	mu     sync.Mutex
	cfg    *store.WebhookConfig
	getErr error
	bumps  []string
}

func (f *fakeWebhookConfigStore) Get(_ context.Context, _ uuid.UUID) (*store.WebhookConfig, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.cfg, nil
}

func (f *fakeWebhookConfigStore) BumpSuccess(_ context.Context, _ uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bumps = append(f.bumps, "success")
	return nil
}

func (f *fakeWebhookConfigStore) BumpFailure(_ context.Context, _ uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bumps = append(f.bumps, "failure")
	return nil
}

type fakeWebhookMetrics struct {
	results map[string]int
	mu      sync.Mutex
}

func (f *fakeWebhookMetrics) IncWebhookRetry(result string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.results == nil {
		f.results = make(map[string]int)
	}
	f.results[result]++
}

func newRetryProc(ds webhookRetryDeliveryStore, cs webhookRetryConfigStore, eb *fakeBusPublisher, jobs *fakeJobTracker, met *fakeWebhookMetrics, client *http.Client) *WebhookRetryProcessor {
	return &WebhookRetryProcessor{
		deliveries: ds,
		configs:    cs,
		jobs:       jobs,
		eventBus:   eb,
		client:     client,
		metrics:    met,
		now:        func() time.Time { return time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC) },
		logger:     zerolog.Nop(),
	}
}

func TestWebhookRetryProcessor_Type(t *testing.T) {
	p := &WebhookRetryProcessor{logger: zerolog.Nop()}
	if p.Type() != JobTypeWebhookRetry {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeWebhookRetry)
	}
}

func TestWebhookRetry_SuccessMarksSucceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	deliveryID := uuid.New()
	cfgID := uuid.New()
	ds := &fakeWebhookDeliveryStore{
		due: []*store.WebhookDelivery{{
			ID:             deliveryID,
			ConfigID:       cfgID,
			EventType:      "test.event",
			PayloadPreview: `{"hello":"world"}`,
			AttemptCount:   1,
			FinalState:     "retrying",
		}},
	}
	cs := &fakeWebhookConfigStore{cfg: &store.WebhookConfig{
		ID:       cfgID,
		TenantID: uuid.New(),
		URL:      srv.URL,
		Secret:   "shhh",
	}}
	eb := &fakeBusPublisher{}
	jobs := &fakeJobTracker{}
	met := &fakeWebhookMetrics{}
	p := newRetryProc(ds, cs, eb, jobs, met, srv.Client())

	if err := p.Process(context.Background(), &store.Job{ID: uuid.New(), Type: JobTypeWebhookRetry}); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if got := ds.finals[deliveryID]; got != "succeeded" {
		t.Errorf("final state = %q, want succeeded", got)
	}
	if met.results["succeeded"] != 1 {
		t.Errorf("succeeded metric = %d, want 1", met.results["succeeded"])
	}
}

func TestWebhookRetry_BackoffSchedulesNextRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	deliveryID := uuid.New()
	ds := &fakeWebhookDeliveryStore{
		due: []*store.WebhookDelivery{{
			ID:             deliveryID,
			ConfigID:       uuid.New(),
			EventType:      "test.event",
			PayloadPreview: "{}",
			AttemptCount:   1, // becomes 2 after this attempt — backoff = 2m
			FinalState:     "retrying",
		}},
	}
	cs := &fakeWebhookConfigStore{cfg: &store.WebhookConfig{ID: uuid.New(), URL: srv.URL, Secret: "x"}}
	jobs := &fakeJobTracker{}
	met := &fakeWebhookMetrics{}
	p := newRetryProc(ds, cs, &fakeBusPublisher{}, jobs, met, srv.Client())

	_ = p.Process(context.Background(), &store.Job{ID: uuid.New(), Type: JobTypeWebhookRetry})

	if len(ds.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(ds.updates))
	}
	u := ds.updates[0]
	if u.attemptCount != 2 {
		t.Errorf("attemptCount = %d, want 2", u.attemptCount)
	}
	if u.nextRetryAt == nil {
		t.Fatal("nextRetryAt is nil, want set for retrying delivery")
	}
	wantNext := time.Date(2026, 4, 13, 12, 2, 0, 0, time.UTC)
	if !u.nextRetryAt.Equal(wantNext) {
		t.Errorf("nextRetryAt = %v, want %v (2m backoff)", u.nextRetryAt, wantNext)
	}
	if state, ok := ds.finals[deliveryID]; ok {
		t.Errorf("retrying delivery should not be finalized, got %q", state)
	}
}

func TestWebhookRetry_DeadLetterAfterFifthFailure(t *testing.T) {
	hits := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	deliveryID := uuid.New()
	ds := &fakeWebhookDeliveryStore{
		due: []*store.WebhookDelivery{{
			ID:             deliveryID,
			ConfigID:       uuid.New(),
			EventType:      "test.event",
			PayloadPreview: "{}",
			AttemptCount:   4, // fifth attempt — dead-letter after this fails
			FinalState:     "retrying",
		}},
	}
	cs := &fakeWebhookConfigStore{cfg: &store.WebhookConfig{ID: uuid.New(), URL: srv.URL, Secret: "x"}}
	eb := &fakeBusPublisher{}
	jobs := &fakeJobTracker{}
	met := &fakeWebhookMetrics{}
	p := newRetryProc(ds, cs, eb, jobs, met, srv.Client())

	_ = p.Process(context.Background(), &store.Job{ID: uuid.New(), Type: JobTypeWebhookRetry})

	if hits.Load() != 1 {
		t.Errorf("expected 1 HTTP attempt, got %d", hits.Load())
	}
	if state := ds.finals[deliveryID]; state != "dead_letter" {
		t.Errorf("final state = %q, want dead_letter", state)
	}
	if met.results["dead_letter"] != 1 {
		t.Errorf("dead_letter metric = %d, want 1", met.results["dead_letter"])
	}
	if len(eb.events) != 1 {
		t.Fatalf("expected 1 dead-letter event, got %d", len(eb.events))
	}
	if eb.events[0].subject == "" {
		t.Error("event subject empty")
	}
}

func TestWebhookRetry_GetConfigErrorDeadLetters(t *testing.T) {
	deliveryID := uuid.New()
	ds := &fakeWebhookDeliveryStore{
		due: []*store.WebhookDelivery{{ID: deliveryID, ConfigID: uuid.New(), AttemptCount: 1, FinalState: "retrying"}},
	}
	cs := &fakeWebhookConfigStore{getErr: store.ErrWebhookConfigNotFound}
	jobs := &fakeJobTracker{}
	met := &fakeWebhookMetrics{}
	p := newRetryProc(ds, cs, &fakeBusPublisher{}, jobs, met, http.DefaultClient)

	_ = p.Process(context.Background(), &store.Job{ID: uuid.New(), Type: JobTypeWebhookRetry})

	if state := ds.finals[deliveryID]; state != "dead_letter" {
		t.Errorf("final state = %q, want dead_letter", state)
	}
}
