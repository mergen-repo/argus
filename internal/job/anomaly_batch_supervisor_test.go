package job

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.uber.org/goleak"
)

type fakeProcessor struct {
	mu         sync.Mutex
	callCount  int
	panicUntil int
	panicValue interface{}
	returnErr  error
}

func (f *fakeProcessor) Type() string { return "fake_processor" }

func (f *fakeProcessor) Process(_ context.Context, _ *store.Job) error {
	f.mu.Lock()
	f.callCount++
	call := f.callCount
	f.mu.Unlock()

	if call <= f.panicUntil {
		panic(f.panicValue)
	}
	return f.returnErr
}

func (f *fakeProcessor) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

type fakeSupervisorBus struct {
	mu     sync.Mutex
	events []map[string]interface{}
}

func (f *fakeSupervisorBus) Publish(_ context.Context, subject string, payload interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, _ := payload.(map[string]interface{})
	if m == nil {
		m = map[string]interface{}{}
	}
	m["_subject"] = subject
	f.events = append(f.events, m)
	return nil
}

func (f *fakeSupervisorBus) AlertCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, e := range f.events {
		if e["_subject"] == bus.SubjectAlertTriggered {
			count++
		}
	}
	return count
}

func makeTestJob() *store.Job {
	return &store.Job{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Type:     JobTypeAnomalyBatch,
	}
}

func makeInstantSupervisor(inner Processor, eb EventPublisher) *CrashSafeProcessor {
	p := NewCrashSafeProcessor(inner, eb, zerolog.Nop())
	p.sleepFunc = func(_ context.Context, _ time.Duration) {}
	return p
}

// Test 1: Panics on first call, succeeds on second → supervisor recovers, retries, returns nil.
func TestCrashSafeProcessor_PanicOnFirstThenSuccess(t *testing.T) {
	defer goleak.VerifyNone(t)

	inner := &fakeProcessor{
		panicUntil: 1,
		panicValue: "something went wrong",
	}
	eb := &fakeSupervisorBus{}
	p := makeInstantSupervisor(inner, eb)

	job := makeTestJob()
	err := p.Process(context.Background(), job)
	if err != nil {
		t.Errorf("expected nil error after successful retry, got: %v", err)
	}
	if inner.Calls() != 2 {
		t.Errorf("expected 2 calls (1 panic + 1 success), got %d", inner.Calls())
	}
	if eb.AlertCount() != 0 {
		t.Errorf("expected no alerts on successful retry, got %d", eb.AlertCount())
	}
}

// Test 2: Processor always panics → marked failed after 5 retries; alert published exactly once; error non-nil.
func TestCrashSafeProcessor_AlwaysPanics(t *testing.T) {
	defer goleak.VerifyNone(t)

	inner := &fakeProcessor{
		panicUntil: 100,
		panicValue: "persistent panic",
	}
	eb := &fakeSupervisorBus{}
	p := makeInstantSupervisor(inner, eb)

	job := makeTestJob()
	err := p.Process(context.Background(), job)
	if err == nil {
		t.Error("expected non-nil error after all retries exhausted")
	}

	expectedCalls := supervisorMaxRetries + 1
	if inner.Calls() != expectedCalls {
		t.Errorf("expected %d total calls (1 initial + %d retries), got %d", expectedCalls, supervisorMaxRetries, inner.Calls())
	}

	if count := eb.AlertCount(); count != 1 {
		t.Errorf("expected exactly 1 alert published, got %d", count)
	}
}

// Test 3: No goroutine leak.
func TestCrashSafeProcessor_NoGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t)

	inner := &fakeProcessor{}
	eb := &fakeSupervisorBus{}
	p := makeInstantSupervisor(inner, eb)

	job := makeTestJob()
	_ = p.Process(context.Background(), job)
}

// Test 4: Backoff grows exponentially — sequence starts ≤500ms and each retry is larger than previous (cap 30s).
func TestCrashSafeProcessor_BackoffGrowsExponentially(t *testing.T) {
	defer goleak.VerifyNone(t)

	var mu sync.Mutex
	var delays []time.Duration

	inner := &fakeProcessor{
		panicUntil: 100,
		panicValue: "boom",
	}

	eb := &fakeSupervisorBus{}
	p := NewCrashSafeProcessor(inner, eb, zerolog.Nop())

	p.sleepFunc = func(_ context.Context, d time.Duration) {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
	}

	job := makeTestJob()
	_ = p.Process(context.Background(), job)

	mu.Lock()
	captured := make([]time.Duration, len(delays))
	copy(captured, delays)
	mu.Unlock()

	if len(captured) != supervisorMaxRetries {
		t.Fatalf("expected %d backoff delays (one per retry), got %d", supervisorMaxRetries, len(captured))
	}

	if captured[0] > 500*time.Millisecond {
		t.Errorf("first backoff should be ≤500ms, got %v", captured[0])
	}

	for i := 1; i < len(captured); i++ {
		if captured[i] <= captured[i-1] && captured[i] < supervisorCapBackoff {
			t.Errorf("backoff[%d]=%v should be > backoff[%d]=%v (unless capped at %v)",
				i, captured[i], i-1, captured[i-1], supervisorCapBackoff)
		}
		if captured[i] > supervisorCapBackoff {
			t.Errorf("backoff[%d]=%v exceeds cap %v", i, captured[i], supervisorCapBackoff)
		}
	}
}

// Test 5: Context cancellation during backoff returns ctx.Err immediately.
func TestCrashSafeProcessor_ContextCancelledDuringBackoff(t *testing.T) {
	defer goleak.VerifyNone(t)

	inner := &fakeProcessor{
		panicUntil: 100,
		panicValue: "boom",
	}
	eb := &fakeSupervisorBus{}
	p := NewCrashSafeProcessor(inner, eb, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())

	sleepCalled := 0
	p.sleepFunc = func(c context.Context, d time.Duration) {
		sleepCalled++
		if sleepCalled == 1 {
			cancel()
		}
	}

	job := makeTestJob()
	err := p.Process(ctx, job)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	if inner.Calls() != 1 {
		t.Errorf("expected 1 call (first attempt only, then context cancelled before retry), got %d", inner.Calls())
	}
}
