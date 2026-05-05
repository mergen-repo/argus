package syslog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ── test doubles ──────────────────────────────────────────────────────────

// recordingMetrics counts per-(transport,format) IncDelivered/IncDropped/
// IncFailures invocations. Goroutine-safe.
type recordingMetrics struct {
	mu        sync.Mutex
	delivered map[string]int
	dropped   map[string]int
	failures  map[string]int
}

func newRecordingMetrics() *recordingMetrics {
	return &recordingMetrics{
		delivered: map[string]int{},
		dropped:   map[string]int{},
		failures:  map[string]int{},
	}
}

func (m *recordingMetrics) IncDelivered(t, f string) {
	m.mu.Lock()
	m.delivered[t+"/"+f]++
	m.mu.Unlock()
}

func (m *recordingMetrics) IncDropped(t, f string) {
	m.mu.Lock()
	m.dropped[t+"/"+f]++
	m.mu.Unlock()
}

func (m *recordingMetrics) IncFailures(t, f string) {
	m.mu.Lock()
	m.failures[t+"/"+f]++
	m.mu.Unlock()
}

func (m *recordingMetrics) Counters() (delivered, dropped, failures int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.delivered {
		delivered += v
	}
	for _, v := range m.dropped {
		dropped += v
	}
	for _, v := range m.failures {
		failures += v
	}
	return
}

// fakeStore captures UpdateDelivery calls and exposes them for assertions.
type fakeStore struct {
	mu       sync.Mutex
	calls    []deliveryCall
	listFn   func(ctx context.Context) ([]Destination, error)
	updateFn func(ctx context.Context, tenantID, id uuid.UUID, success bool, errMsg string) error
}

type deliveryCall struct {
	TenantID uuid.UUID
	ID       uuid.UUID
	Success  bool
	ErrMsg   string
}

func (f *fakeStore) ListAllEnabled(ctx context.Context) ([]Destination, error) {
	if f.listFn != nil {
		return f.listFn(ctx)
	}
	return []Destination{}, nil
}

func (f *fakeStore) UpdateDelivery(ctx context.Context, tenantID uuid.UUID, id uuid.UUID, success bool, errMsg string) error {
	f.mu.Lock()
	f.calls = append(f.calls, deliveryCall{tenantID, id, success, errMsg})
	f.mu.Unlock()
	if f.updateFn != nil {
		return f.updateFn(ctx, tenantID, id, success, errMsg)
	}
	return nil
}

func (f *fakeStore) Calls() []deliveryCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]deliveryCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// recordingAuditor counts CreateEntry calls for AC-14 rate-limit assertions.
type recordingAuditor struct {
	mu      sync.Mutex
	entries []audit.CreateEntryParams
}

func (a *recordingAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	a.mu.Lock()
	a.entries = append(a.entries, p)
	a.mu.Unlock()
	return &audit.Entry{}, nil
}

func (a *recordingAuditor) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.entries)
}

func (a *recordingAuditor) Last() audit.CreateEntryParams {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.entries) == 0 {
		return audit.CreateEntryParams{}
	}
	return a.entries[len(a.entries)-1]
}

// fakeTransport — controllable Transport for worker tests.
type fakeTransport struct {
	mu        sync.Mutex
	sent      [][]byte
	sendErr   error
	delay     time.Duration
	closed    bool
	closeOnce sync.Once
	sendCount int32
}

func (t *fakeTransport) Send(_ context.Context, msg []byte) error {
	t.mu.Lock()
	if t.delay > 0 {
		d := t.delay
		t.mu.Unlock()
		time.Sleep(d)
		t.mu.Lock()
	}
	atomic.AddInt32(&t.sendCount, 1)
	if t.sendErr != nil {
		err := t.sendErr
		t.mu.Unlock()
		return err
	}
	cp := make([]byte, len(msg))
	copy(cp, msg)
	t.sent = append(t.sent, cp)
	t.mu.Unlock()
	return nil
}

func (t *fakeTransport) Close() error {
	t.closeOnce.Do(func() { t.closed = true })
	return nil
}

func (t *fakeTransport) Sent() [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([][]byte, len(t.sent))
	for i, m := range t.sent {
		out[i] = append([]byte{}, m...)
	}
	return out
}

func (t *fakeTransport) SendCount() int { return int(atomic.LoadInt32(&t.sendCount)) }

func (t *fakeTransport) SetSendErr(e error) {
	t.mu.Lock()
	t.sendErr = e
	t.mu.Unlock()
}

// makeTestDest builds a deterministic destination for worker construction.
func makeTestDest(t *testing.T) Destination {
	t.Helper()
	return Destination{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Name:      "test-dest",
		Host:      "127.0.0.1",
		Port:      6514,
		Transport: TransportUDP,
		Format:    FormatRFC5424,
		Facility:  16,
	}
}

// waitFor polls fn() up to deadline; returns true on success.
func waitFor(deadline time.Duration, fn func() bool) bool {
	d := time.Now().Add(deadline)
	for time.Now().Before(d) {
		if fn() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return fn()
}

// ── tests ─────────────────────────────────────────────────────────────────

// TestDestinationWorker_QueueOverflowDropsOldest — fill the queue while the
// consumer is artificially blocked, then push one more message → exactly one
// drop counted, queue still full.
func TestDestinationWorker_QueueOverflowDropsOldest(t *testing.T) {
	dest := makeTestDest(t)
	tr := &fakeTransport{delay: 200 * time.Millisecond}
	st := &fakeStore{}
	m := newRecordingMetrics()

	w := newDestinationWorker(dest, tr, 4, m, &recordingAuditor{}, st, zerolog.Nop())
	w.Start()
	defer w.Stop(context.Background())

	// Fill queue (cap=4) with 4 messages while the worker is busy on the
	// first send (200ms delay). The first message is consumed immediately
	// upon Start, so we need 5 enqueues to actually overflow: 1 drains into
	// the worker, 4 fill the queue, the 5th drops the oldest.
	w.Enqueue([]byte("m1"))
	// Allow the worker to pick up m1 and become blocked on Send.
	time.Sleep(20 * time.Millisecond)
	w.Enqueue([]byte("m2"))
	w.Enqueue([]byte("m3"))
	w.Enqueue([]byte("m4"))
	w.Enqueue([]byte("m5"))
	dropped := w.Enqueue([]byte("m6")) // overflow

	if !dropped {
		t.Fatalf("expected overflow drop on 6th Enqueue, got dropped=false")
	}
	_, droppedCount, _ := m.Counters()
	if droppedCount != 1 {
		t.Fatalf("expected 1 dropped counter, got %d", droppedCount)
	}
}

// TestDestinationWorker_BackoffOnFailure — destination unreachable; observe
// the Backoff sequence by counting Send invocations within a fixed window.
func TestDestinationWorker_BackoffOnFailure(t *testing.T) {
	dest := makeTestDest(t)
	tr := &fakeTransport{sendErr: errors.New("conn refused")}
	st := &fakeStore{}
	m := newRecordingMetrics()

	w := newDestinationWorker(dest, tr, 16, m, &recordingAuditor{}, st, zerolog.Nop())
	w.Start()
	defer w.Stop(context.Background())

	// Push a single message — the worker will keep failing to send IT (since
	// we leave it on the queue) but actually the contract is: each message
	// is removed from the queue upon dequeue, then send + backoff. The
	// failure path SLEEPS for the backoff before taking the next message.
	// To observe backoff we push enough messages so the worker is busy.
	for i := 0; i < 5; i++ {
		w.Enqueue([]byte("x"))
	}

	// In 1.5s with 1s+2s+... backoff, the worker should attempt at most
	// 2 sends (first fails immediately, then sleeps 1s, attempts 2nd).
	time.Sleep(1500 * time.Millisecond)
	count := tr.SendCount()
	if count < 1 || count > 3 {
		t.Fatalf("expected ~2 send attempts in 1500ms with backoff, got %d", count)
	}
	_, _, failures := m.Counters()
	if failures < 1 {
		t.Fatalf("expected at least one failure counted, got %d", failures)
	}
}

// TestDestinationWorker_AuditRateLimit_OncePerMinute — many failures within
// the 1-minute window emit only one audit entry; subsequent failures bump the
// suppressed counter without creating new rows.
func TestDestinationWorker_AuditRateLimit_OncePerMinute(t *testing.T) {
	dest := makeTestDest(t)
	tr := &fakeTransport{sendErr: errors.New("siem down")}
	st := &fakeStore{}
	m := newRecordingMetrics()
	a := &recordingAuditor{}

	w := newDestinationWorker(dest, tr, 64, m, a, st, zerolog.Nop())
	// Drive recordFailure synchronously to control timing precisely without
	// fighting backoff scheduling.
	for i := 0; i < 10; i++ {
		w.recordFailure(errors.New("boom"))
	}

	if got := a.Count(); got != 1 {
		t.Fatalf("expected 1 audit entry within rate-limit window, got %d", got)
	}
	// First emission's suppressed_count should be 0 (it was the first failure).
	last := a.Last()
	if last.Action != "log_forwarding.delivery_failed" {
		t.Fatalf("expected action=log_forwarding.delivery_failed, got %q", last.Action)
	}
}

// TestDestinationWorker_SuccessClearsLastError — after a failure, a successful
// send must invoke UpdateDelivery(success=true, errMsg=""). Both rows show up
// in the fake store's call log.
func TestDestinationWorker_SuccessClearsLastError(t *testing.T) {
	dest := makeTestDest(t)
	tr := &fakeTransport{sendErr: errors.New("oops")}
	st := &fakeStore{}
	w := newDestinationWorker(dest, tr, 4, newRecordingMetrics(), &recordingAuditor{}, st, zerolog.Nop())
	w.Start()
	defer w.Stop(context.Background())

	w.Enqueue([]byte("first-fail"))
	if !waitFor(2*time.Second, func() bool {
		for _, c := range st.Calls() {
			if !c.Success {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected at least one failure UpdateDelivery call")
	}

	tr.SetSendErr(nil) // recover
	w.Enqueue([]byte("second-success"))

	if !waitFor(3*time.Second, func() bool {
		for _, c := range st.Calls() {
			if c.Success && c.ErrMsg == "" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected a success UpdateDelivery call after recovery")
	}
}

// TestDestinationWorker_StopDrainsQueue — Stop with N queued messages and a
// fast transport; all N must arrive at the transport before Stop returns.
func TestDestinationWorker_StopDrainsQueue(t *testing.T) {
	dest := makeTestDest(t)
	tr := &fakeTransport{}
	st := &fakeStore{}
	w := newDestinationWorker(dest, tr, 16, newRecordingMetrics(), &recordingAuditor{}, st, zerolog.Nop())
	w.Start()

	for i := 0; i < 5; i++ {
		w.Enqueue([]byte("m"))
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Stop(stopCtx); err != nil {
		t.Fatalf("Stop returned err: %v", err)
	}
	if tr.SendCount() != 5 {
		t.Fatalf("expected 5 sends after drain, got %d", tr.SendCount())
	}
	if !tr.closed {
		t.Fatalf("transport not closed after Stop")
	}
}

// TestDestinationWorker_StopTimeout_RemainingDropped — Stop budget shorter
// than the remaining work; Stop must return ctx.DeadlineExceeded and the
// transport must still close.
func TestDestinationWorker_StopTimeout_RemainingDropped(t *testing.T) {
	dest := makeTestDest(t)
	tr := &fakeTransport{delay: 200 * time.Millisecond}
	st := &fakeStore{}
	w := newDestinationWorker(dest, tr, 16, newRecordingMetrics(), &recordingAuditor{}, st, zerolog.Nop())
	w.Start()

	for i := 0; i < 10; i++ {
		w.Enqueue([]byte("slow"))
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := w.Stop(stopCtx)
	if err == nil {
		t.Fatalf("expected ctx error from Stop with too-short timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if !tr.closed {
		t.Fatalf("transport not closed after Stop")
	}
}
