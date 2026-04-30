package job

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// fakeReaperCommandStore implements reaperCommandStore for tests.
type fakeReaperCommandStore struct {
	mu              sync.Mutex
	sentCommands    []store.EsimOTACommand
	timeoutCalls    []uuid.UUID
	retryCalls      map[uuid.UUID]int
	retryAt         map[uuid.UUID]time.Time
	markFailedCalls []string
}

func newFakeReaperCommandStore(cmds ...store.EsimOTACommand) *fakeReaperCommandStore {
	return &fakeReaperCommandStore{
		sentCommands: cmds,
		retryCalls:   make(map[uuid.UUID]int),
		retryAt:      make(map[uuid.UUID]time.Time),
	}
}

func (f *fakeReaperCommandStore) ListSentBefore(_ context.Context, _ time.Time) ([]store.EsimOTACommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.EsimOTACommand(nil), f.sentCommands...), nil
}

func (f *fakeReaperCommandStore) MarkTimeout(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.timeoutCalls = append(f.timeoutCalls, id)
	return nil
}

func (f *fakeReaperCommandStore) IncrementRetry(_ context.Context, id uuid.UUID, nextRetryAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryCalls[id]++
	f.retryAt[id] = nextRetryAt
	return nil
}

func (f *fakeReaperCommandStore) MarkFailed(_ context.Context, id uuid.UUID, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markFailedCalls = append(f.markFailedCalls, id.String()+":"+errMsg)
	return nil
}

// fakeReaperProfileStore implements reaperProfileStore for tests.
type fakeReaperProfileStore struct {
	mu            sync.Mutex
	markFailedIDs []uuid.UUID
}

func (f *fakeReaperProfileStore) MarkFailed(_ context.Context, profileID uuid.UUID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markFailedIDs = append(f.markFailedIDs, profileID)
	return nil
}

// fakeReaperAudit implements reaperAuditStore for tests.
type fakeReaperAudit struct {
	mu    sync.Mutex
	calls []audit.CreateEntryParams
}

func (f *fakeReaperAudit) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)
	return &audit.Entry{}, nil
}

// fakeReaperEventBus implements reaperEventBus for tests.
type fakeReaperEventBus struct {
	mu       sync.Mutex
	subjects []string
}

func (f *fakeReaperEventBus) Publish(_ context.Context, subject string, _ interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subjects = append(f.subjects, subject)
	return nil
}

func newTestReaper(
	cmdStore reaperCommandStore,
	profStore reaperProfileStore,
	aud reaperAuditStore,
	eb reaperEventBus,
) *ESimOTATimeoutReaperProcessor {
	return &ESimOTATimeoutReaperProcessor{
		commandStore:   cmdStore,
		profileStore:   profStore,
		auditor:        aud,
		eventBus:       eb,
		timeoutMinutes: reaperDefaultTimeoutMinutes,
		maxRetries:     reaperMaxRetries,
		logger:         zerolog.Nop(),
	}
}

func TestESimOTATimeoutReaper_Type(t *testing.T) {
	p := &ESimOTATimeoutReaperProcessor{}
	if p.Type() != JobTypeESimOTATimeoutReaper {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeESimOTATimeoutReaper)
	}
	if JobTypeESimOTATimeoutReaper != "esim_ota_timeout_reaper" {
		t.Errorf("JobTypeESimOTATimeoutReaper = %q", JobTypeESimOTATimeoutReaper)
	}
	found := false
	for _, jt := range AllJobTypes {
		if jt == JobTypeESimOTATimeoutReaper {
			found = true
			break
		}
	}
	if !found {
		t.Error("JobTypeESimOTATimeoutReaper not found in AllJobTypes")
	}
}

// TestESimOTATimeoutReaper_Requeue_WhenRetriesRemain verifies that a command with
// sent_at=15 min ago and retry_count=2 is transitioned to queued with retry_count=3.
func TestESimOTATimeoutReaper_Requeue_WhenRetriesRemain(t *testing.T) {
	cmdID := uuid.New()
	profID := uuid.New()
	sentAt := time.Now().Add(-15 * time.Minute)
	cmd := store.EsimOTACommand{
		ID:          cmdID,
		TenantID:    uuid.New(),
		EID:         "89000000000000000010",
		CommandType: "switch",
		Status:      "sent",
		RetryCount:  2,
		SentAt:      &sentAt,
		ProfileID:   &profID,
	}

	cmdStore := newFakeReaperCommandStore(cmd)
	profStore := &fakeReaperProfileStore{}
	aud := &fakeReaperAudit{}
	eb := &fakeReaperEventBus{}

	p := newTestReaper(cmdStore, profStore, aud, eb)
	res, err := p.sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep error: %v", err)
	}

	if res.Requeued != 1 {
		t.Errorf("Requeued = %d, want 1", res.Requeued)
	}
	if res.Failed != 0 {
		t.Errorf("Failed = %d, want 0", res.Failed)
	}

	cmdStore.mu.Lock()
	defer cmdStore.mu.Unlock()

	if len(cmdStore.timeoutCalls) != 1 || cmdStore.timeoutCalls[0] != cmdID {
		t.Errorf("MarkTimeout calls = %v, want [%v]", cmdStore.timeoutCalls, cmdID)
	}
	if cmdStore.retryCalls[cmdID] != 1 {
		t.Errorf("IncrementRetry calls for cmdID = %d, want 1", cmdStore.retryCalls[cmdID])
	}
	if len(cmdStore.markFailedCalls) != 0 {
		t.Errorf("MarkFailed should not be called when retries remain; calls=%v", cmdStore.markFailedCalls)
	}

	profStore.mu.Lock()
	defer profStore.mu.Unlock()
	if len(profStore.markFailedIDs) != 0 {
		t.Errorf("profileStore.MarkFailed should not be called when requeuing")
	}
}

// TestESimOTATimeoutReaper_Terminal_WhenMaxRetriesExceeded verifies that a command
// with retry_count >= maxRetries is marked terminal and emits SubjectESimCommandFailed.
func TestESimOTATimeoutReaper_Terminal_WhenMaxRetriesExceeded(t *testing.T) {
	cmdID := uuid.New()
	profID := uuid.New()
	sentAt := time.Now().Add(-15 * time.Minute)
	cmd := store.EsimOTACommand{
		ID:          cmdID,
		TenantID:    uuid.New(),
		EID:         "89000000000000000011",
		CommandType: "switch",
		Status:      "sent",
		RetryCount:  5,
		SentAt:      &sentAt,
		ProfileID:   &profID,
	}

	cmdStore := newFakeReaperCommandStore(cmd)
	profStore := &fakeReaperProfileStore{}
	aud := &fakeReaperAudit{}
	eb := &fakeReaperEventBus{}

	p := newTestReaper(cmdStore, profStore, aud, eb)
	res, err := p.sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep error: %v", err)
	}

	if res.Failed != 1 {
		t.Errorf("Failed = %d, want 1", res.Failed)
	}
	if res.Requeued != 0 {
		t.Errorf("Requeued = %d, want 0", res.Requeued)
	}

	cmdStore.mu.Lock()
	defer cmdStore.mu.Unlock()
	if len(cmdStore.timeoutCalls) != 0 {
		t.Errorf("MarkTimeout should not be called on terminal path; calls=%v", cmdStore.timeoutCalls)
	}
	if len(cmdStore.markFailedCalls) != 1 {
		t.Errorf("MarkFailed calls = %d, want 1", len(cmdStore.markFailedCalls))
	}

	profStore.mu.Lock()
	defer profStore.mu.Unlock()
	if len(profStore.markFailedIDs) != 1 || profStore.markFailedIDs[0] != profID {
		t.Errorf("profileStore.MarkFailed not called with profID; calls=%v", profStore.markFailedIDs)
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()
	foundFailed := false
	for _, s := range eb.subjects {
		if s == bus.SubjectESimCommandFailed {
			foundFailed = true
			break
		}
	}
	if !foundFailed {
		t.Errorf("SubjectESimCommandFailed not emitted; subjects=%v", eb.subjects)
	}
}

// TestESimOTATimeoutReaper_Mixed_RequeueAndTerminal verifies mixed batch handling.
func TestESimOTATimeoutReaper_Mixed_RequeueAndTerminal(t *testing.T) {
	sentAt := time.Now().Add(-15 * time.Minute)
	cmds := []store.EsimOTACommand{
		{ID: uuid.New(), EID: "eid1", CommandType: "switch", Status: "sent", RetryCount: 2, SentAt: &sentAt},
		{ID: uuid.New(), EID: "eid2", CommandType: "switch", Status: "sent", RetryCount: 5, SentAt: &sentAt},
		{ID: uuid.New(), EID: "eid3", CommandType: "switch", Status: "sent", RetryCount: 1, SentAt: &sentAt},
	}

	cmdStore := newFakeReaperCommandStore(cmds...)
	profStore := &fakeReaperProfileStore{}
	aud := &fakeReaperAudit{}
	eb := &fakeReaperEventBus{}

	p := newTestReaper(cmdStore, profStore, aud, eb)
	res, err := p.sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep error: %v", err)
	}

	if res.Requeued != 2 {
		t.Errorf("Requeued = %d, want 2", res.Requeued)
	}
	if res.Failed != 1 {
		t.Errorf("Failed = %d, want 1", res.Failed)
	}
}
