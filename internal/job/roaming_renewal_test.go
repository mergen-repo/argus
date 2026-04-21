package job

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newRedisForRoamingTest(t *testing.T) *redis.Client {
	t.Helper()
	rc := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 14})
	if rc.Ping(context.Background()).Err() != nil {
		t.Skip("Redis not available for roaming renewal tests")
	}
	return rc
}

type fakeRoamingAgreementStore struct {
	agreements []store.RoamingAgreement
	err        error
}

func (f *fakeRoamingAgreementStore) ListExpiringWithin(_ context.Context, _ int) ([]store.RoamingAgreement, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.agreements, nil
}

type fakeRoamingUserStore struct{}

func (f *fakeRoamingUserStore) ListByRole(_ context.Context, _ uuid.UUID, _ string) ([]store.User, error) {
	return nil, nil
}

type fakeRoamingJobTracker struct {
	completed []uuid.UUID
	result    json.RawMessage
}

func (f *fakeRoamingJobTracker) UpdateProgress(_ context.Context, _ uuid.UUID, _, _, _ int) error {
	return nil
}

func (f *fakeRoamingJobTracker) CheckCancelled(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeRoamingJobTracker) Complete(_ context.Context, jobID uuid.UUID, _ json.RawMessage, result json.RawMessage) error {
	f.completed = append(f.completed, jobID)
	f.result = result
	return nil
}

type fakeRoamingBus struct {
	subjects []string
	payloads []interface{}
}

func (f *fakeRoamingBus) Publish(_ context.Context, subject string, payload interface{}) error {
	f.subjects = append(f.subjects, subject)
	f.payloads = append(f.payloads, payload)
	return nil
}

func makeExpiringAgreement(daysToExpiry int) store.RoamingAgreement {
	now := time.Now().UTC()
	return store.RoamingAgreement{
		ID:                  uuid.New(),
		TenantID:            uuid.New(),
		OperatorID:          uuid.New(),
		PartnerOperatorName: "Test Partner",
		AgreementType:       "international",
		SLATerms:            json.RawMessage(`{}`),
		CostTerms:           json.RawMessage(`{}`),
		StartDate:           now.AddDate(-1, 0, 0),
		EndDate:             now.AddDate(0, 0, daysToExpiry),
		AutoRenew:           false,
		State:               "active",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func TestRoamingRenewalSweeper_Type(t *testing.T) {
	p := &RoamingRenewalSweeper{
		logger: zerolog.Nop(),
	}
	if p.Type() != JobTypeRoamingRenewal {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeRoamingRenewal)
	}
}

func TestRoamingRenewalSweeper_TypeValue(t *testing.T) {
	if JobTypeRoamingRenewal != "roaming_renewal_sweep" {
		t.Errorf("JobTypeRoamingRenewal = %q, want %q", JobTypeRoamingRenewal, "roaming_renewal_sweep")
	}
}

func TestRoamingRenewalSweeper_EmptyAgreements(t *testing.T) {
	rc := newRedisForRoamingTest(t)
	defer rc.FlushDB(context.Background())
	defer rc.Close()

	tracker := &fakeRoamingJobTracker{}
	bus := &fakeRoamingBus{}

	p := &RoamingRenewalSweeper{
		agreements: &fakeRoamingAgreementStore{agreements: []store.RoamingAgreement{}},
		users:      &fakeRoamingUserStore{},
		jobs:       tracker,
		eventBus:   bus,
		redis:      rc,
		alertDays:  30,
		logger:     zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New()}
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("Process(empty) error: %v", err)
	}

	if len(bus.subjects) != 0 {
		t.Errorf("expected 0 events published, got %d", len(bus.subjects))
	}
	if len(tracker.completed) != 1 {
		t.Fatalf("expected 1 job completed, got %d", len(tracker.completed))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tracker.result, &result); err != nil {
		t.Fatalf("result is not valid json: %v", err)
	}
	if result["total"].(float64) != 0 {
		t.Errorf("total = %v, want 0", result["total"])
	}
}

func TestRoamingRenewalSweeper_PublishesAlerts(t *testing.T) {
	rc := newRedisForRoamingTest(t)
	defer rc.FlushDB(context.Background())
	defer rc.Close()

	ag1 := makeExpiringAgreement(20)
	ag2 := makeExpiringAgreement(5)

	tracker := &fakeRoamingJobTracker{}
	eventBus := &fakeRoamingBus{}

	p := &RoamingRenewalSweeper{
		agreements: &fakeRoamingAgreementStore{agreements: []store.RoamingAgreement{ag1, ag2}},
		users:      &fakeRoamingUserStore{},
		jobs:       tracker,
		eventBus:   eventBus,
		redis:      rc,
		alertDays:  30,
		logger:     zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New()}
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if len(eventBus.subjects) != 2 {
		t.Errorf("expected 2 events published, got %d", len(eventBus.subjects))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tracker.result, &result); err != nil {
		t.Fatalf("result is not valid json: %v", err)
	}
	if result["notified"].(float64) != 2 {
		t.Errorf("notified = %v, want 2", result["notified"])
	}
	if result["skipped"].(float64) != 0 {
		t.Errorf("skipped = %v, want 0", result["skipped"])
	}
}

func TestRoamingRenewalSweeper_Dedup(t *testing.T) {
	rc := newRedisForRoamingTest(t)
	defer rc.FlushDB(context.Background())
	defer rc.Close()

	ag := makeExpiringAgreement(10)
	now := time.Now().UTC()
	dedupKey := fmt.Sprintf("argus:roaming:renewal:%s:%s", ag.ID.String(), now.Format("2006-01"))
	rc.Set(context.Background(), dedupKey, "1", roamingRenewalRedisTTL)

	tracker := &fakeRoamingJobTracker{}
	eventBus := &fakeRoamingBus{}

	p := &RoamingRenewalSweeper{
		agreements: &fakeRoamingAgreementStore{agreements: []store.RoamingAgreement{ag}},
		users:      &fakeRoamingUserStore{},
		jobs:       tracker,
		eventBus:   eventBus,
		redis:      rc,
		alertDays:  30,
		logger:     zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New()}
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if len(eventBus.subjects) != 0 {
		t.Errorf("dedup: expected 0 events published, got %d", len(eventBus.subjects))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(tracker.result, &result); err != nil {
		t.Fatalf("result json error: %v", err)
	}
	if result["skipped"].(float64) != 1 {
		t.Errorf("skipped = %v, want 1", result["skipped"])
	}
	if result["notified"].(float64) != 0 {
		t.Errorf("notified = %v, want 0", result["notified"])
	}
}

func TestRoamingRenewalSweeper_SeverityCriticalWhenLessThan7Days(t *testing.T) {
	rc := newRedisForRoamingTest(t)
	defer rc.FlushDB(context.Background())
	defer rc.Close()

	ag := makeExpiringAgreement(3)

	tracker := &fakeRoamingJobTracker{}
	eventBus := &fakeRoamingBus{}

	p := &RoamingRenewalSweeper{
		agreements: &fakeRoamingAgreementStore{agreements: []store.RoamingAgreement{ag}},
		users:      &fakeRoamingUserStore{},
		jobs:       tracker,
		eventBus:   eventBus,
		redis:      rc,
		alertDays:  30,
		logger:     zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New()}
	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if len(eventBus.payloads) == 0 {
		t.Fatal("no events published")
	}

	// FIX-212: payload is now *bus.Envelope.
	env, ok := eventBus.payloads[0].(*bus.Envelope)
	if !ok {
		t.Fatalf("payload type = %T, want *bus.Envelope", eventBus.payloads[0])
	}
	if env.Severity != "critical" {
		t.Errorf("severity = %v, want critical", env.Severity)
	}
}

func TestRoamingRenewalSweeper_SeverityMediumWhenMoreThan7Days(t *testing.T) {
	rc := newRedisForRoamingTest(t)
	defer rc.FlushDB(context.Background())
	defer rc.Close()

	ag := makeExpiringAgreement(20)

	tracker := &fakeRoamingJobTracker{}
	eventBus := &fakeRoamingBus{}

	p := &RoamingRenewalSweeper{
		agreements: &fakeRoamingAgreementStore{agreements: []store.RoamingAgreement{ag}},
		users:      &fakeRoamingUserStore{},
		jobs:       tracker,
		eventBus:   eventBus,
		redis:      rc,
		alertDays:  30,
		logger:     zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New()}
	if err := p.Process(context.Background(), job); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	if len(eventBus.payloads) == 0 {
		t.Fatal("no events published")
	}

	env, ok := eventBus.payloads[0].(*bus.Envelope)
	if !ok {
		t.Fatalf("payload type = %T, want *bus.Envelope", eventBus.payloads[0])
	}
	if env.Severity != "medium" {
		t.Errorf("severity = %v, want medium", env.Severity)
	}
}

func TestRoamingRenewalSweeper_StoreError(t *testing.T) {
	rc := newRedisForRoamingTest(t)
	defer rc.Close()

	tracker := &fakeRoamingJobTracker{}
	eventBus := &fakeRoamingBus{}

	p := &RoamingRenewalSweeper{
		agreements: &fakeRoamingAgreementStore{err: fmt.Errorf("db down")},
		users:      &fakeRoamingUserStore{},
		jobs:       tracker,
		eventBus:   eventBus,
		redis:      rc,
		alertDays:  30,
		logger:     zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New()}
	err := p.Process(context.Background(), job)
	if err == nil {
		t.Fatal("expected error when store fails, got nil")
	}
}

func TestRoamingRenewalSweeper_NilEventBus(t *testing.T) {
	rc := newRedisForRoamingTest(t)
	defer rc.FlushDB(context.Background())
	defer rc.Close()

	ag := makeExpiringAgreement(15)

	tracker := &fakeRoamingJobTracker{}

	p := &RoamingRenewalSweeper{
		agreements: &fakeRoamingAgreementStore{agreements: []store.RoamingAgreement{ag}},
		users:      &fakeRoamingUserStore{},
		jobs:       tracker,
		eventBus:   nil,
		redis:      rc,
		alertDays:  30,
		logger:     zerolog.Nop(),
	}

	job := &store.Job{ID: uuid.New()}
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("nil eventBus should not cause error: %v", err)
	}

	if len(tracker.completed) != 1 {
		t.Error("job should complete even with nil eventBus")
	}
}
