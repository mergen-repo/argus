package binding

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// -----------------------------------------------------------------------------
// Mocks for the four sinks + DropCounter.
// -----------------------------------------------------------------------------

type mockAuditor struct {
	mu    sync.Mutex
	calls []mockAuditCall
	err   error
}

type mockAuditCall struct {
	action  string
	payload AuditPayload
}

func (m *mockAuditor) Log(ctx context.Context, action string, p AuditPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockAuditCall{action: action, payload: p})
	return m.err
}

func (m *mockAuditor) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type mockNotifier struct {
	mu       sync.Mutex
	calls    []mockNotifCall
	err      error
	publishC chan struct{} // non-nil to signal each publish (synchronisation in tests)
}

type mockNotifCall struct {
	subject string
	payload NotificationPayload
}

func (m *mockNotifier) Publish(ctx context.Context, subject string, p NotificationPayload) error {
	m.mu.Lock()
	m.calls = append(m.calls, mockNotifCall{subject: subject, payload: p})
	err := m.err
	m.mu.Unlock()
	if m.publishC != nil {
		m.publishC <- struct{}{}
	}
	return err
}

func (m *mockNotifier) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockNotifier) lastCall() (mockNotifCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return mockNotifCall{}, false
	}
	return m.calls[len(m.calls)-1], true
}

type mockHistoryWriter struct {
	mu    sync.Mutex
	calls []HistoryEntry
}

func (m *mockHistoryWriter) Append(ctx context.Context, e HistoryEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, e)
}

func (m *mockHistoryWriter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockHistoryWriter) lastEntry() (HistoryEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return HistoryEntry{}, false
	}
	return m.calls[len(m.calls)-1], true
}

type mockSIMUpdater struct {
	mu    sync.Mutex
	calls []mockSIMUpdateCall
	err   error
}

type mockSIMUpdateCall struct {
	tenantID     uuid.UUID
	simID        uuid.UUID
	imei         string
	graceExpires *time.Time
}

func (m *mockSIMUpdater) LockBoundIMEI(ctx context.Context, tenantID uuid.UUID, simID uuid.UUID, imei string, graceExpires *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockSIMUpdateCall{
		tenantID:     tenantID,
		simID:        simID,
		imei:         imei,
		graceExpires: graceExpires,
	})
	return m.err
}

func (m *mockSIMUpdater) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockSIMUpdater) lastCall() (mockSIMUpdateCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return mockSIMUpdateCall{}, false
	}
	return m.calls[len(m.calls)-1], true
}

type mockDropCounter struct {
	historyDropped int64
	notifFailed    int64
}

func (m *mockDropCounter) IncHistoryDropped()     { atomic.AddInt64(&m.historyDropped, 1) }
func (m *mockDropCounter) IncNotificationFailed() { atomic.AddInt64(&m.notifFailed, 1) }

func (m *mockDropCounter) historyCount() int64 { return atomic.LoadInt64(&m.historyDropped) }
func (m *mockDropCounter) notifCount() int64   { return atomic.LoadInt64(&m.notifFailed) }

// -----------------------------------------------------------------------------
// Test fixtures.
// -----------------------------------------------------------------------------

const testGraceWindow = 24 * time.Hour

var (
	testTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	testSIMID    = uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	testNow      = time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
)

func testFixedClock() time.Time { return testNow }

func makeOrchestratorSession(imei string) SessionContext {
	return SessionContext{
		TenantID:        testTenantID,
		SIMID:           testSIMID,
		IMEI:            imei,
		SoftwareVersion: "1.0",
	}
}

func makeOrchestratorSIM(mode string, bound *string) SIMView {
	var modePtr *string
	if mode != "" {
		m := mode
		modePtr = &m
	}
	return SIMView{
		ID:          testSIMID,
		TenantID:    testTenantID,
		BindingMode: modePtr,
		BoundIMEI:   bound,
	}
}

// newTestOrchestrator wires the mocks with a deterministic clock.
func newTestOrchestrator(t *testing.T, audit *mockAuditor, notif *mockNotifier, hist *mockHistoryWriter, sims *mockSIMUpdater, metrics DropCounter) *Orchestrator {
	t.Helper()
	return NewOrchestrator(
		audit, notif, hist, sims, testGraceWindow,
		WithDropCounter(metrics),
		WithLogger(zerolog.Nop()),
		WithOrchestratorClock(testFixedClock),
	)
}

// waitForNotif blocks until the notifier records `expected` calls or the
// timeout fires. Avoids time.Sleep in tests.
func waitForNotif(t *testing.T, notif *mockNotifier, expected int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if notif.callCount() >= expected {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("notifier: expected %d calls, got %d", expected, notif.callCount())
}

// -----------------------------------------------------------------------------
// Tests.
// -----------------------------------------------------------------------------

func TestOrchestrator_AllowNoSideEffects(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	// Plain Allow (NULL-mode short-circuit shape).
	v := Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusDisabled}
	session := makeOrchestratorSession("123456789012345")
	sim := makeOrchestratorSIM("", nil)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := audit.callCount(); got != 0 {
		t.Errorf("audit.callCount = %d, want 0", got)
	}
	if got := sims.callCount(); got != 0 {
		t.Errorf("sims.callCount = %d, want 0", got)
	}
	// History MUST be recorded even on plain Allow per AC-11.
	if got := hist.callCount(); got != 1 {
		t.Fatalf("hist.callCount = %d, want 1", got)
	}
	entry, _ := hist.lastEntry()
	if entry.WasMismatch {
		t.Error("history entry: WasMismatch=true on plain Allow")
	}
	if entry.AlarmRaised {
		t.Error("history entry: AlarmRaised=true on plain Allow")
	}
	// Notification not requested; no goroutine to wait on.
	if got := notif.callCount(); got != 0 {
		t.Errorf("notif.callCount = %d, want 0", got)
	}
}

func TestOrchestrator_RejectStrictFullSinks(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	// Reject under strict mode — full sinks fan-out.
	v := Verdict{
		Kind:               VerdictReject,
		Reason:             RejectReasonMismatchStrict,
		Severity:           SeverityHigh,
		BindingStatus:      BindingStatusMismatch,
		EmitAudit:          true,
		AuditAction:        AuditActionMismatch,
		EmitNotification:   true,
		NotifSubject:       NotifSubjectIMEIChanged,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
	}
	bound := "111111111111111"
	session := makeOrchestratorSession("999999999999999")
	sim := makeOrchestratorSIM("strict", &bound)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := sims.callCount(); got != 0 {
		t.Errorf("sims.callCount = %d, want 0 (no SIM update on reject)", got)
	}

	if got := audit.callCount(); got != 1 {
		t.Fatalf("audit.callCount = %d, want 1", got)
	}
	if audit.calls[0].action != AuditActionMismatch {
		t.Errorf("audit action = %q, want %q", audit.calls[0].action, AuditActionMismatch)
	}
	if audit.calls[0].payload.ReasonCode != RejectReasonMismatchStrict {
		t.Errorf("audit reason = %q, want %q", audit.calls[0].payload.ReasonCode, RejectReasonMismatchStrict)
	}
	if audit.calls[0].payload.Protocol != "radius" {
		t.Errorf("audit protocol = %q, want %q", audit.calls[0].payload.Protocol, "radius")
	}

	waitForNotif(t, notif, 1, time.Second)
	last, _ := notif.lastCall()
	if last.subject != NotifSubjectIMEIChanged {
		t.Errorf("notif subject = %q, want %q", last.subject, NotifSubjectIMEIChanged)
	}
	if last.payload.Severity != SeverityHigh {
		t.Errorf("notif severity = %q, want %q", last.payload.Severity, SeverityHigh)
	}

	if got := hist.callCount(); got != 1 {
		t.Fatalf("hist.callCount = %d, want 1", got)
	}
	entry, _ := hist.lastEntry()
	if !entry.WasMismatch || !entry.AlarmRaised {
		t.Errorf("history flags = (mismatch=%v, alarm=%v), want (true,true)", entry.WasMismatch, entry.AlarmRaised)
	}
}

func TestOrchestrator_FirstUseLockSIMUpdateAndAudit(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{
		Kind:             VerdictAllow,
		BindingStatus:    BindingStatusVerified,
		Severity:         SeverityInfo,
		EmitAudit:        true,
		AuditAction:      AuditActionFirstUseLocked,
		EmitNotification: true,
		NotifSubject:     NotifSubjectBindingLocked,
		LockBoundIMEI:    true,
		NewBoundIMEI:     "123456789012345",
	}
	session := makeOrchestratorSession("123456789012345")
	sim := makeOrchestratorSIM("first-use", nil)

	if err := o.Apply(context.Background(), v, session, sim, "diameter_s6a"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := sims.callCount(); got != 1 {
		t.Fatalf("sims.callCount = %d, want 1", got)
	}
	last, _ := sims.lastCall()
	if last.imei != "123456789012345" {
		t.Errorf("locked imei = %q, want %q", last.imei, "123456789012345")
	}
	if last.graceExpires != nil {
		t.Errorf("graceExpires = %v, want nil (first-use, no grace)", last.graceExpires)
	}

	if got := audit.callCount(); got != 1 {
		t.Fatalf("audit.callCount = %d, want 1", got)
	}
	if audit.calls[0].action != AuditActionFirstUseLocked {
		t.Errorf("audit action = %q, want %q", audit.calls[0].action, AuditActionFirstUseLocked)
	}

	waitForNotif(t, notif, 1, time.Second)

	if got := hist.callCount(); got != 1 {
		t.Fatalf("hist.callCount = %d, want 1", got)
	}
	entry, _ := hist.lastEntry()
	if entry.WasMismatch {
		t.Error("history WasMismatch=true on first-use lock; want false")
	}
}

func TestOrchestrator_GracePeriodRefreshGraceWindow(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{
		Kind:               VerdictAllowWithAlarm,
		Severity:           SeverityMedium,
		BindingStatus:      BindingStatusPending,
		EmitAudit:          true,
		AuditAction:        AuditActionGraceChange,
		EmitNotification:   true,
		NotifSubject:       NotifSubjectBindingGraceChange,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
		LockBoundIMEI:      true,
		NewBoundIMEI:       "888888888888888",
		RefreshGraceWindow: true,
	}
	bound := "111111111111111"
	session := makeOrchestratorSession("888888888888888")
	sim := makeOrchestratorSIM("grace-period", &bound)

	if err := o.Apply(context.Background(), v, session, sim, "5g_sba"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := sims.callCount(); got != 1 {
		t.Fatalf("sims.callCount = %d, want 1", got)
	}
	last, _ := sims.lastCall()
	if last.imei != "888888888888888" {
		t.Errorf("locked imei = %q, want %q", last.imei, "888888888888888")
	}
	if last.graceExpires == nil {
		t.Fatal("graceExpires = nil, want non-nil refreshed expiry")
	}
	wantExpires := testNow.Add(testGraceWindow).UTC()
	if !last.graceExpires.Equal(wantExpires) {
		t.Errorf("graceExpires = %v, want %v", last.graceExpires, wantExpires)
	}

	waitForNotif(t, notif, 1, time.Second)
}

func TestOrchestrator_GracePeriodRefreshOnly(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	// Hypothetical: refresh grace window without locking a new IMEI
	// (e.g. operator-initiated mode flip that keeps the existing bound).
	v := Verdict{
		Kind:               VerdictAllow,
		BindingStatus:      BindingStatusVerified,
		LockBoundIMEI:      false,
		RefreshGraceWindow: true,
	}
	bound := "111111111111111"
	session := makeOrchestratorSession("111111111111111")
	sim := makeOrchestratorSIM("grace-period", &bound)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := sims.callCount(); got != 1 {
		t.Fatalf("sims.callCount = %d, want 1", got)
	}
	last, _ := sims.lastCall()
	if last.imei != "111111111111111" {
		t.Errorf("imei = %q, want existing bound %q", last.imei, "111111111111111")
	}
	if last.graceExpires == nil {
		t.Fatal("graceExpires = nil, want refreshed expiry")
	}
}

func TestOrchestrator_BlacklistOverrideHighSeverity(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{
		Kind:               VerdictReject,
		Reason:             RejectReasonBlacklist,
		Severity:           SeverityHigh,
		BindingStatus:      BindingStatusMismatch,
		EmitAudit:          true,
		AuditAction:        AuditActionBlacklistHit,
		EmitNotification:   true,
		NotifSubject:       NotifSubjectBindingBlacklistHit,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
	}
	session := makeOrchestratorSession("353490069873258")
	sim := makeOrchestratorSIM("strict", nil)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	waitForNotif(t, notif, 1, time.Second)
	last, _ := notif.lastCall()
	if last.payload.Severity != SeverityHigh {
		t.Errorf("notif severity = %q, want high", last.payload.Severity)
	}
	if last.payload.ReasonCode != RejectReasonBlacklist {
		t.Errorf("notif reason = %q, want %q", last.payload.ReasonCode, RejectReasonBlacklist)
	}
}

func TestOrchestrator_AuditErrorBubblesUp(t *testing.T) {
	auditErr := errors.New("audit hash chain inserter exploded")
	audit := &mockAuditor{err: auditErr}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{
		Kind:        VerdictReject,
		Reason:      RejectReasonMismatchStrict,
		Severity:    SeverityHigh,
		EmitAudit:   true,
		AuditAction: AuditActionMismatch,
	}
	session := makeOrchestratorSession("123456789012345")
	sim := makeOrchestratorSIM("strict", nil)

	err := o.Apply(context.Background(), v, session, sim, "radius")
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	if !errors.Is(err, auditErr) {
		t.Errorf("Apply: err = %v, want wraps %v", err, auditErr)
	}
	// Notification + history must NOT have run when audit failed.
	if notif.callCount() != 0 {
		t.Errorf("notif.callCount = %d, want 0 on audit failure", notif.callCount())
	}
	if hist.callCount() != 0 {
		t.Errorf("hist.callCount = %d, want 0 on audit failure", hist.callCount())
	}
}

func TestOrchestrator_NotificationErrorDoesNotBubble(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{
		err:      errors.New("nats publish failed"),
		publishC: make(chan struct{}, 1),
	}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{
		Kind:             VerdictReject,
		Reason:           RejectReasonMismatchStrict,
		EmitAudit:        true,
		AuditAction:      AuditActionMismatch,
		EmitNotification: true,
		NotifSubject:     NotifSubjectIMEIChanged,
		Severity:         SeverityHigh,
	}
	session := makeOrchestratorSession("123456789012345")
	sim := makeOrchestratorSIM("strict", nil)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	// Wait for the publish goroutine to register.
	select {
	case <-notif.publishC:
	case <-time.After(time.Second):
		t.Fatal("publish goroutine never fired")
	}

	if metrics.notifCount() != 1 {
		t.Errorf("notif drop counter = %d, want 1", metrics.notifCount())
	}
}

func TestOrchestrator_EmptyIMEISkipsHistory(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	// Strict-mode reject with empty observed IMEI: AC-11 + TBL-59 NOT NULL
	// constraint require we suppress the history insert.
	v := Verdict{
		Kind:               VerdictReject,
		Reason:             RejectReasonMismatchStrict,
		Severity:           SeverityHigh,
		EmitAudit:          true,
		AuditAction:        AuditActionMismatch,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
	}
	session := makeOrchestratorSession("") // empty
	sim := makeOrchestratorSIM("strict", nil)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if hist.callCount() != 0 {
		t.Errorf("hist.callCount = %d, want 0 (empty IMEI must skip history)", hist.callCount())
	}
	// Audit still runs.
	if audit.callCount() != 1 {
		t.Errorf("audit.callCount = %d, want 1", audit.callCount())
	}
}

func TestOrchestrator_SoftModeAlarmHistory(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{
		Kind:               VerdictAllowWithAlarm,
		Severity:           SeverityInfo,
		BindingStatus:      BindingStatusMismatch,
		EmitAudit:          true,
		AuditAction:        AuditActionSoftMismatch,
		EmitNotification:   true,
		NotifSubject:       NotifSubjectIMEIChanged,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
	}
	bound := "111111111111111"
	session := makeOrchestratorSession("999999999999999")
	sim := makeOrchestratorSIM("soft", &bound)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if hist.callCount() != 1 {
		t.Fatalf("hist.callCount = %d, want 1", hist.callCount())
	}
	entry, _ := hist.lastEntry()
	if !entry.AlarmRaised {
		t.Error("history AlarmRaised=false on soft-mode alarm; want true")
	}
	if !entry.WasMismatch {
		t.Error("history WasMismatch=false on soft-mode alarm; want true")
	}
}

func TestOrchestrator_LockUpdateErrorBubblesUp(t *testing.T) {
	updateErr := errors.New("sims update timed out")
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{err: updateErr}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{
		Kind:          VerdictAllow,
		BindingStatus: BindingStatusVerified,
		EmitAudit:     true,
		AuditAction:   AuditActionFirstUseLocked,
		LockBoundIMEI: true,
		NewBoundIMEI:  "123456789012345",
	}
	session := makeOrchestratorSession("123456789012345")
	sim := makeOrchestratorSIM("first-use", nil)

	err := o.Apply(context.Background(), v, session, sim, "radius")
	if err == nil {
		t.Fatal("Apply: want error, got nil")
	}
	if !errors.Is(err, updateErr) {
		t.Errorf("Apply: err = %v, want wraps %v", err, updateErr)
	}
	// Audit must NOT run when SIM update failed (early return).
	if audit.callCount() != 0 {
		t.Errorf("audit.callCount = %d, want 0 on lock failure", audit.callCount())
	}
}

func TestOrchestrator_NilSinksDegradeGracefully(t *testing.T) {
	o := NewOrchestrator(nil, nil, nil, nil, testGraceWindow,
		WithLogger(zerolog.Nop()),
		WithOrchestratorClock(testFixedClock),
	)

	v := Verdict{
		Kind:             VerdictReject,
		Reason:           RejectReasonMismatchStrict,
		Severity:         SeverityHigh,
		EmitAudit:        true,
		AuditAction:      AuditActionMismatch,
		EmitNotification: true,
		NotifSubject:     NotifSubjectIMEIChanged,
		LockBoundIMEI:    true,
		NewBoundIMEI:     "123456789012345",
	}
	session := makeOrchestratorSession("123456789012345")
	sim := makeOrchestratorSIM("strict", nil)

	// All sinks nil — must not panic and must return nil.
	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply with nil sinks: unexpected err: %v", err)
	}
}

// -----------------------------------------------------------------------------
// BufferedHistoryWriter tests.
// -----------------------------------------------------------------------------

func TestBufferedHistoryWriter_AppendQueued(t *testing.T) {
	var (
		mu      sync.Mutex
		flushed []HistoryEntry
	)
	flushFn := func(ctx context.Context, e HistoryEntry) error {
		mu.Lock()
		flushed = append(flushed, e)
		mu.Unlock()
		return nil
	}

	w := NewBufferedHistoryWriter(8, 1, flushFn, nil, zerolog.Nop())
	w.Start(context.Background())

	entry := HistoryEntry{
		TenantID:        testTenantID,
		SIMID:           testSIMID,
		ObservedIMEI:    "123456789012345",
		CapturedAt:      testNow,
		CaptureProtocol: "radius",
	}
	w.Append(context.Background(), entry)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: unexpected err: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("flushed = %d entries, want 1", len(flushed))
	}
	if flushed[0].ObservedIMEI != "123456789012345" {
		t.Errorf("flushed IMEI = %q, want %q", flushed[0].ObservedIMEI, "123456789012345")
	}
}

func TestBufferedHistoryWriter_FlushFnCalledForEachEntry(t *testing.T) {
	var count int64
	flushFn := func(ctx context.Context, e HistoryEntry) error {
		atomic.AddInt64(&count, 1)
		return nil
	}
	w := NewBufferedHistoryWriter(64, 2, flushFn, nil, zerolog.Nop())
	w.Start(context.Background())

	const n = 32
	for i := 0; i < n; i++ {
		w.Append(context.Background(), HistoryEntry{
			TenantID:     testTenantID,
			SIMID:        testSIMID,
			ObservedIMEI: "123456789012345",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := atomic.LoadInt64(&count); got != n {
		t.Errorf("flushFn calls = %d, want %d", got, n)
	}
}

func TestBufferedHistoryWriter_FullQueueDrops(t *testing.T) {
	// Pause flushFn to allow the queue to fill up.
	release := make(chan struct{})
	flushFn := func(ctx context.Context, e HistoryEntry) error {
		<-release
		return nil
	}
	metrics := &mockDropCounter{}
	w := NewBufferedHistoryWriter(2, 1, flushFn, metrics, zerolog.Nop())
	w.Start(context.Background())

	// Push 1 entry that the worker will pick up and block on.
	w.Append(context.Background(), HistoryEntry{
		TenantID: testTenantID, SIMID: testSIMID, ObservedIMEI: "1",
	})

	// Wait until the worker has dequeued the first entry (queue length
	// drops to 0) before filling — otherwise the assertion is racy.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if w.QueueLen() == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Now fill the buffered queue to capacity (2 slots).
	for i := 0; i < 2; i++ {
		w.Append(context.Background(), HistoryEntry{
			TenantID: testTenantID, SIMID: testSIMID, ObservedIMEI: "x",
		})
	}

	// One more push must drop.
	w.Append(context.Background(), HistoryEntry{
		TenantID: testTenantID, SIMID: testSIMID, ObservedIMEI: "drop-me",
	})

	if got := metrics.historyCount(); got != 1 {
		t.Errorf("history drop counter = %d, want 1", got)
	}

	// Release the worker and shut down cleanly.
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestBufferedHistoryWriter_ShutdownDrainsQueue(t *testing.T) {
	var count int64
	flushFn := func(ctx context.Context, e HistoryEntry) error {
		atomic.AddInt64(&count, 1)
		return nil
	}
	w := NewBufferedHistoryWriter(128, 2, flushFn, nil, zerolog.Nop())
	w.Start(context.Background())

	const n = 100
	for i := 0; i < n; i++ {
		w.Append(context.Background(), HistoryEntry{
			TenantID: testTenantID, SIMID: testSIMID, ObservedIMEI: "x",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := atomic.LoadInt64(&count); got != n {
		t.Errorf("flushed count = %d, want %d", got, n)
	}
}

func TestBufferedHistoryWriter_ShutdownTimeout(t *testing.T) {
	// Slow flushFn that never returns within the test deadline.
	flushFn := func(ctx context.Context, e HistoryEntry) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}
	w := NewBufferedHistoryWriter(8, 1, flushFn, nil, zerolog.Nop())
	w.Start(context.Background())

	w.Append(context.Background(), HistoryEntry{
		TenantID: testTenantID, SIMID: testSIMID, ObservedIMEI: "x",
	})

	// Short deadline — Shutdown must return ctx.Err().
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := w.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown err = %v, want DeadlineExceeded", err)
	}
}

func TestBufferedHistoryWriter_FlushErrorLoggedNotDropped(t *testing.T) {
	flushFn := func(ctx context.Context, e HistoryEntry) error {
		return errors.New("db down")
	}
	metrics := &mockDropCounter{}
	w := NewBufferedHistoryWriter(8, 1, flushFn, metrics, zerolog.Nop())
	w.Start(context.Background())

	w.Append(context.Background(), HistoryEntry{
		TenantID: testTenantID, SIMID: testSIMID, ObservedIMEI: "x",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := metrics.historyCount(); got != 1 {
		t.Errorf("flush error counter = %d, want 1", got)
	}
}

func TestBufferedHistoryWriter_AppendAfterShutdownSilentlyDrops(t *testing.T) {
	var called int64
	flushFn := func(ctx context.Context, e HistoryEntry) error {
		atomic.AddInt64(&called, 1)
		return nil
	}
	metrics := &mockDropCounter{}
	w := NewBufferedHistoryWriter(8, 1, flushFn, metrics, zerolog.Nop())
	w.Start(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Append after shutdown — must not panic, must not increment counter.
	w.Append(context.Background(), HistoryEntry{
		TenantID: testTenantID, SIMID: testSIMID, ObservedIMEI: "x",
	})

	if atomic.LoadInt64(&called) != 0 {
		t.Errorf("flushFn called after shutdown")
	}
	if metrics.historyCount() != 0 {
		t.Errorf("post-shutdown drop incremented counter (controlled stop must be silent)")
	}
}

func TestBufferedHistoryWriter_ConcurrentProducers(t *testing.T) {
	// Race-detector exercise: 50 goroutines × 50 entries.
	var count int64
	flushFn := func(ctx context.Context, e HistoryEntry) error {
		atomic.AddInt64(&count, 1)
		return nil
	}
	w := NewBufferedHistoryWriter(4096, 4, flushFn, nil, zerolog.Nop())
	w.Start(context.Background())

	const producers = 50
	const perProducer = 50
	var wg sync.WaitGroup
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				w.Append(context.Background(), HistoryEntry{
					TenantID: testTenantID, SIMID: testSIMID, ObservedIMEI: "x",
				})
			}
		}()
	}
	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := atomic.LoadInt64(&count); got != producers*perProducer {
		t.Errorf("flushed count = %d, want %d", got, producers*perProducer)
	}
}
