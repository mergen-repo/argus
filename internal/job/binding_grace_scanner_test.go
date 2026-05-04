package job

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/policy/binding"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// fakeGraceSimScanner is the test double for graceSimScanner.
type fakeGraceSimScanner struct {
	rows    []store.SIMApproachingGraceExpiry
	listErr error
	calls   int
}

func (f *fakeGraceSimScanner) ListSIMsApproachingGraceExpiry(_ context.Context, _, _ time.Time) ([]store.SIMApproachingGraceExpiry, error) {
	f.calls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.rows, nil
}

// fakeGraceDedup is the test double for graceDedupCache. notified holds the
// set of SIM IDs that have already been "notified"; first MarkNotified call
// returns alreadyNotified=false and records the ID, subsequent calls return
// alreadyNotified=true.
type fakeGraceDedup struct {
	mu       sync.Mutex
	notified map[uuid.UUID]bool
	err      error
}

func newFakeGraceDedup() *fakeGraceDedup {
	return &fakeGraceDedup{notified: map[uuid.UUID]bool{}}
}

func (f *fakeGraceDedup) MarkNotified(_ context.Context, simID uuid.UUID, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if f.notified[simID] {
		return true, nil
	}
	f.notified[simID] = true
	return false, nil
}

// makeGraceRow builds a SIMApproachingGraceExpiry for tests.
func makeGraceRow(expiresIn time.Duration) store.SIMApproachingGraceExpiry {
	return store.SIMApproachingGraceExpiry{
		ID:                    uuid.New(),
		TenantID:              uuid.New(),
		ICCID:                 "8990000000000000001",
		BindingGraceExpiresAt: time.Now().Add(expiresIn),
	}
}

// newGraceScannerForTest assembles a scanner with the supplied fakes plus a
// frozen clock. Pass nil for any dependency to exercise the nil-guard paths.
func newGraceScannerForTest(
	sims *fakeGraceSimScanner,
	dedup graceDedupCache,
	pub busPublisher,
) *BindingGraceScanner {
	return &BindingGraceScanner{
		jobs:     &fakeJobTracker{},
		sims:     sims,
		dedup:    dedup,
		eventBus: pub,
		logger:   zerolog.Nop(),
		now:      func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
	}
}

// ── PAT-026 paired tests (MANDATORY) ─────────────────────────────────────────

// TestBindingGraceScanner_Type asserts that the processor advertises the
// canonical job type constant. PAT-026 RECURRENCE prevention.
func TestBindingGraceScanner_Type(t *testing.T) {
	s := &BindingGraceScanner{logger: zerolog.Nop()}
	if got := s.Type(); got != JobTypeBindingGraceScanner {
		t.Errorf("Type() = %q, want %q", got, JobTypeBindingGraceScanner)
	}
	if JobTypeBindingGraceScanner != "binding_grace_scanner" {
		t.Errorf("JobTypeBindingGraceScanner = %q, want %q", JobTypeBindingGraceScanner, "binding_grace_scanner")
	}
}

// TestJobTypeBindingGraceScanner_RegisteredInAllJobTypes asserts the type is
// listed in the AllJobTypes slice — the registry every consumer (rollout,
// metrics labels, audit dashboards) iterates. PAT-026 RECURRENCE [STORY-095
// F-A1] was the exact shape this guards against.
func TestJobTypeBindingGraceScanner_RegisteredInAllJobTypes(t *testing.T) {
	for _, jt := range AllJobTypes {
		if jt == JobTypeBindingGraceScanner {
			return
		}
	}
	t.Errorf("JobTypeBindingGraceScanner (%q) not found in AllJobTypes", JobTypeBindingGraceScanner)
}

// ── Behaviour tests ──────────────────────────────────────────────────────────

// TestBindingGraceScanner_NoSIMs_NoNotifications: empty list → 0 publishes,
// scanned=0, notified=0.
func TestBindingGraceScanner_NoSIMs_NoNotifications(t *testing.T) {
	sims := &fakeGraceSimScanner{rows: nil}
	eb := &fakeBusPublisher{}
	dedup := newFakeGraceDedup()

	s := newGraceScannerForTest(sims, dedup, eb)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeBindingGraceScanner}

	if err := s.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	notifs := countSubject(eb, bus.SubjectNotification)
	if notifs != 0 {
		t.Errorf("notification publishes = %d, want 0", notifs)
	}
}

// TestBindingGraceScanner_OneSIMWithinWindow_NotifiesOnce: 1 SIM in window →
// 1 publish + envelope shape check (Type, severity, entity, dedup_key).
func TestBindingGraceScanner_OneSIMWithinWindow_NotifiesOnce(t *testing.T) {
	row := makeGraceRow(2 * time.Hour)
	sims := &fakeGraceSimScanner{rows: []store.SIMApproachingGraceExpiry{row}}
	eb := &fakeBusPublisher{}
	dedup := newFakeGraceDedup()

	s := newGraceScannerForTest(sims, dedup, eb)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeBindingGraceScanner}

	if err := s.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	notifEnvs := collectByEnvelopeType(eb, binding.NotifSubjectBindingGraceExpiring)
	if len(notifEnvs) != 1 {
		t.Fatalf("envelopes with Type=%q = %d, want 1", binding.NotifSubjectBindingGraceExpiring, len(notifEnvs))
	}
	env := notifEnvs[0]
	if env.Severity != severity.Medium {
		t.Errorf("envelope.Severity = %q, want %q", env.Severity, severity.Medium)
	}
	if env.TenantID != row.TenantID.String() {
		t.Errorf("envelope.TenantID = %q, want %q", env.TenantID, row.TenantID.String())
	}
	if env.Entity == nil || env.Entity.Type != "sim" || env.Entity.ID != row.ID.String() {
		t.Errorf("envelope.Entity = %+v, want sim/%s", env.Entity, row.ID)
	}
	wantDedup := "binding:grace_notified:" + row.ID.String()
	if env.DedupKey == nil || *env.DedupKey != wantDedup {
		t.Errorf("envelope.DedupKey = %v, want %q", env.DedupKey, wantDedup)
	}
}

// TestBindingGraceScanner_DedupSkipsSecondCall: run the scanner twice on the
// same SIM in window → 1 publish total (the second is dedup-skipped). AC-6
// dedup contract.
func TestBindingGraceScanner_DedupSkipsSecondCall(t *testing.T) {
	row := makeGraceRow(2 * time.Hour)
	sims := &fakeGraceSimScanner{rows: []store.SIMApproachingGraceExpiry{row}}
	eb := &fakeBusPublisher{}
	dedup := newFakeGraceDedup()

	s := newGraceScannerForTest(sims, dedup, eb)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeBindingGraceScanner}

	for i := 0; i < 2; i++ {
		if err := s.Process(context.Background(), job); err != nil {
			t.Fatalf("Process iteration %d returned error: %v", i, err)
		}
	}

	notifs := countSubject(eb, bus.SubjectNotification)
	if notifs != 1 {
		t.Errorf("publishes after 2 runs = %d, want 1 (second run must dedup)", notifs)
	}
}

// TestBindingGraceScanner_RedisError_FailsOpen: when MarkNotified returns an
// error, the scanner publishes anyway (over-notification preferable to
// missed pre-expiry warning). Documents the AC-6 fail-open trade-off.
func TestBindingGraceScanner_RedisError_FailsOpen(t *testing.T) {
	row := makeGraceRow(2 * time.Hour)
	sims := &fakeGraceSimScanner{rows: []store.SIMApproachingGraceExpiry{row}}
	eb := &fakeBusPublisher{}
	dedup := &fakeGraceDedup{notified: map[uuid.UUID]bool{}, err: errors.New("redis: connection refused")}

	s := newGraceScannerForTest(sims, dedup, eb)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeBindingGraceScanner}

	if err := s.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	notifs := countSubject(eb, bus.SubjectNotification)
	if notifs != 1 {
		t.Errorf("publishes on dedup error = %d, want 1 (fail-open)", notifs)
	}
}

// TestBindingGraceScanner_NotifierError_ContinuesLoop: when the eventBus
// returns an error mid-loop, the scanner records it and continues — does
// not abort. AC-12 (must not block on isolated failure).
func TestBindingGraceScanner_NotifierError_ContinuesLoop(t *testing.T) {
	rowA := makeGraceRow(1 * time.Hour)
	rowB := makeGraceRow(2 * time.Hour)
	rowC := makeGraceRow(3 * time.Hour)
	sims := &fakeGraceSimScanner{rows: []store.SIMApproachingGraceExpiry{rowA, rowB, rowC}}
	eb := &errOnceBusPublisher{}
	dedup := newFakeGraceDedup()

	s := newGraceScannerForTest(sims, dedup, eb)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeBindingGraceScanner}

	if err := s.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	// errOnce: first publish fails → 1 error + 2 successes. Plus the
	// terminal SubjectJobCompleted publish → 3 total publish calls.
	if eb.attempts != 4 {
		t.Errorf("publish attempts = %d, want 4 (3 envelopes + 1 job_completed)", eb.attempts)
	}
}

// TestBindingGraceScanner_NilDedup_NoDedup: a nil graceDedupCache disables
// dedup entirely — every run notifies every in-window SIM.
func TestBindingGraceScanner_NilDedup_NoDedup(t *testing.T) {
	row := makeGraceRow(2 * time.Hour)
	sims := &fakeGraceSimScanner{rows: []store.SIMApproachingGraceExpiry{row}}
	eb := &fakeBusPublisher{}

	s := newGraceScannerForTest(sims, nil, eb)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeBindingGraceScanner}

	for i := 0; i < 3; i++ {
		if err := s.Process(context.Background(), job); err != nil {
			t.Fatalf("Process iteration %d returned error: %v", i, err)
		}
	}

	notifs := countSubject(eb, bus.SubjectNotification)
	if notifs != 3 {
		t.Errorf("publishes with nil dedup over 3 runs = %d, want 3", notifs)
	}
}

// TestBindingGraceScanner_ProcessReturnsCounts asserts the result JSON keys
// (scanned, notified, skipped_dedup, errors) are populated on the JobStore
// Complete call.
func TestBindingGraceScanner_ProcessReturnsCounts(t *testing.T) {
	rows := []store.SIMApproachingGraceExpiry{makeGraceRow(2 * time.Hour), makeGraceRow(3 * time.Hour)}
	sims := &fakeGraceSimScanner{rows: rows}
	eb := &fakeBusPublisher{}
	dedup := newFakeGraceDedup()
	jobs := &fakeJobTracker{}

	s := &BindingGraceScanner{
		jobs:     jobs,
		sims:     sims,
		dedup:    dedup,
		eventBus: eb,
		logger:   zerolog.Nop(),
		now:      func() time.Time { return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC) },
	}
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeBindingGraceScanner}

	if err := s.Process(context.Background(), job); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.completed) != 1 {
		t.Fatalf("Complete called %d times, want 1", len(jobs.completed))
	}
	var got bindingGraceScannerResult
	if err := json.Unmarshal(jobs.result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.Scanned != 2 {
		t.Errorf("result.Scanned = %d, want 2", got.Scanned)
	}
	if got.Notified != 2 {
		t.Errorf("result.Notified = %d, want 2", got.Notified)
	}
	if got.SkippedDedup != 0 {
		t.Errorf("result.SkippedDedup = %d, want 0", got.SkippedDedup)
	}
	if got.Errors != 0 {
		t.Errorf("result.Errors = %d, want 0", got.Errors)
	}
}

// TestBindingGraceScanner_ListError_ReturnsError: the scanner aborts cleanly
// when the store query fails.
func TestBindingGraceScanner_ListError_ReturnsError(t *testing.T) {
	sims := &fakeGraceSimScanner{listErr: errors.New("db down")}
	eb := &fakeBusPublisher{}
	dedup := newFakeGraceDedup()

	s := newGraceScannerForTest(sims, dedup, eb)
	job := &store.Job{ID: uuid.New(), TenantID: uuid.New(), Type: JobTypeBindingGraceScanner}

	if err := s.Process(context.Background(), job); err == nil {
		t.Fatal("Process returned nil error, want non-nil from list failure")
	}
	if countSubject(eb, bus.SubjectNotification) != 0 {
		t.Errorf("publishes on list failure = %d, want 0", countSubject(eb, bus.SubjectNotification))
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func countSubject(eb *fakeBusPublisher, subject string) int {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	n := 0
	for _, e := range eb.events {
		if e.subject == subject {
			n++
		}
	}
	return n
}

func collectByEnvelopeType(eb *fakeBusPublisher, envType string) []*bus.Envelope {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	var out []*bus.Envelope
	for _, e := range eb.events {
		env, ok := e.payload.(*bus.Envelope)
		if !ok {
			continue
		}
		if env.Type == envType {
			out = append(out, env)
		}
	}
	return out
}

// errOnceBusPublisher fails the FIRST publish call (any subject), then
// succeeds. Used to prove the scanner continues past per-SIM publish errors.
type errOnceBusPublisher struct {
	mu       sync.Mutex
	attempts int
}

func (e *errOnceBusPublisher) Publish(_ context.Context, _ string, _ interface{}) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.attempts++
	if e.attempts == 1 {
		return errors.New("publish boom")
	}
	return nil
}
