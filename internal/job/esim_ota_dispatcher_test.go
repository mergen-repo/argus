package job

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/smsr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// --- fakes ---

type fakeDispatcherCommandStore struct {
	mu              sync.Mutex
	queued          []store.EsimOTACommand
	markSentCalls   []uuid.UUID
	markFailedCalls []uuid.UUID
	retryCount      map[uuid.UUID]int
	retryAt         map[uuid.UUID]time.Time
}

func newFakeDispatcherCommandStore(cmds ...store.EsimOTACommand) *fakeDispatcherCommandStore {
	return &fakeDispatcherCommandStore{
		queued:     cmds,
		retryCount: make(map[uuid.UUID]int),
		retryAt:    make(map[uuid.UUID]time.Time),
	}
}

func (f *fakeDispatcherCommandStore) ListQueued(_ context.Context, limit int, _ time.Time) ([]store.EsimOTACommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit > len(f.queued) {
		limit = len(f.queued)
	}
	return f.queued[:limit], nil
}

func (f *fakeDispatcherCommandStore) MarkSent(_ context.Context, id uuid.UUID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markSentCalls = append(f.markSentCalls, id)
	return nil
}

func (f *fakeDispatcherCommandStore) MarkFailed(_ context.Context, id uuid.UUID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markFailedCalls = append(f.markFailedCalls, id)
	return nil
}

func (f *fakeDispatcherCommandStore) IncrementRetry(_ context.Context, id uuid.UUID, nextRetryAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryCount[id]++
	f.retryAt[id] = nextRetryAt
	return nil
}

type fakeDispatcherProfileStore struct {
	mu            sync.Mutex
	markFailedIDs []uuid.UUID
}

func (f *fakeDispatcherProfileStore) MarkFailed(_ context.Context, profileID uuid.UUID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markFailedIDs = append(f.markFailedIDs, profileID)
	return nil
}

type fakeDispatcherSMSRClient struct {
	mu     sync.Mutex
	pushFn func(smsr.PushRequest) (smsr.PushResponse, error)
	calls  []smsr.PushRequest
}

func (f *fakeDispatcherSMSRClient) Push(_ context.Context, req smsr.PushRequest) (smsr.PushResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if f.pushFn != nil {
		return f.pushFn(req)
	}
	return smsr.PushResponse{SMSRCommandID: "smsr-" + req.CommandID, AcceptedAt: time.Now()}, nil
}

type fakeDispatcherAudit struct {
	mu    sync.Mutex
	calls []audit.CreateEntryParams
}

func (f *fakeDispatcherAudit) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)
	return &audit.Entry{}, nil
}

type fakeDispatcherEventBus struct {
	mu       sync.Mutex
	subjects []string
}

func (f *fakeDispatcherEventBus) Publish(_ context.Context, subject string, _ interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subjects = append(f.subjects, subject)
	return nil
}

func newTestDispatcher(
	cmdStore dispatcherCommandStore,
	profStore dispatcherProfileStore,
	smsrClient dispatcherSMSRClient,
	aud dispatcherAuditStore,
	eb dispatcherEventBus,
) *ESimOTADispatcherProcessor {
	return &ESimOTADispatcherProcessor{
		commandStore: cmdStore,
		profileStore: profStore,
		smsrClient:   smsrClient,
		auditor:      aud,
		eventBus:     eb,
		batchSize:    200,
		rateLimitRPS: 10000,
		maxRetries:   5,
		rateLimiters: make(map[string]*rate.Limiter),
		logger:       zerolog.Nop(),
	}
}

func TestESimOTADispatcher_Type(t *testing.T) {
	p := &ESimOTADispatcherProcessor{}
	if p.Type() != JobTypeOTACommand {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeOTACommand)
	}
}

func TestESimOTADispatcher_SuccessPath(t *testing.T) {
	profID := uuid.New()
	cmd := store.EsimOTACommand{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		EID:         "89000000000000000001",
		CommandType: "switch",
		Status:      "queued",
		ProfileID:   &profID,
	}

	cmdStore := newFakeDispatcherCommandStore(cmd)
	profStore := &fakeDispatcherProfileStore{}
	smsrClient := &fakeDispatcherSMSRClient{}
	aud := &fakeDispatcherAudit{}
	eb := &fakeDispatcherEventBus{}

	p := newTestDispatcher(cmdStore, profStore, smsrClient, aud, eb)

	j := &store.Job{ID: uuid.New(), TenantID: cmd.TenantID}
	if err := p.Process(context.Background(), j); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	cmdStore.mu.Lock()
	defer cmdStore.mu.Unlock()
	if len(cmdStore.markSentCalls) != 1 {
		t.Errorf("MarkSent calls = %d, want 1", len(cmdStore.markSentCalls))
	}
	if len(cmdStore.markFailedCalls) != 0 {
		t.Errorf("MarkFailed calls = %d, want 0", len(cmdStore.markFailedCalls))
	}

	aud.mu.Lock()
	defer aud.mu.Unlock()
	if len(aud.calls) == 0 {
		t.Error("expected at least 1 audit entry on success")
	}
	for _, c := range aud.calls {
		if c.Action != "ota.dispatch" {
			t.Errorf("audit action = %q, want ota.dispatch", c.Action)
		}
		if c.EntityType != "esim_profile" {
			t.Errorf("audit entity_type = %q, want esim_profile", c.EntityType)
		}
	}
}

func TestESimOTADispatcher_TransientRetry(t *testing.T) {
	cmd := store.EsimOTACommand{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		EID:         "89000000000000000002",
		CommandType: "switch",
		Status:      "queued",
		RetryCount:  0,
	}

	cmdStore := newFakeDispatcherCommandStore(cmd)
	profStore := &fakeDispatcherProfileStore{}
	smsrClient := &fakeDispatcherSMSRClient{
		pushFn: func(_ smsr.PushRequest) (smsr.PushResponse, error) {
			return smsr.PushResponse{}, smsr.ErrSMSRConnectionFailed
		},
	}
	aud := &fakeDispatcherAudit{}
	eb := &fakeDispatcherEventBus{}

	p := newTestDispatcher(cmdStore, profStore, smsrClient, aud, eb)
	p.maxRetries = 5

	j := &store.Job{ID: uuid.New(), TenantID: cmd.TenantID}
	if err := p.Process(context.Background(), j); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	cmdStore.mu.Lock()
	defer cmdStore.mu.Unlock()
	if cmdStore.retryCount[cmd.ID] != 1 {
		t.Errorf("IncrementRetry calls = %d, want 1", cmdStore.retryCount[cmd.ID])
	}
	if len(cmdStore.markFailedCalls) != 0 {
		t.Errorf("MarkFailed should not be called on first transient failure")
	}
}

func TestESimOTADispatcher_TerminalAfterMaxRetries(t *testing.T) {
	profID := uuid.New()
	cmd := store.EsimOTACommand{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		EID:         "89000000000000000003",
		CommandType: "switch",
		Status:      "queued",
		RetryCount:  4,
		ProfileID:   &profID,
	}

	cmdStore := newFakeDispatcherCommandStore(cmd)
	profStore := &fakeDispatcherProfileStore{}
	smsrClient := &fakeDispatcherSMSRClient{
		pushFn: func(_ smsr.PushRequest) (smsr.PushResponse, error) {
			return smsr.PushResponse{}, smsr.ErrSMSRConnectionFailed
		},
	}
	aud := &fakeDispatcherAudit{}
	eb := &fakeDispatcherEventBus{}

	p := newTestDispatcher(cmdStore, profStore, smsrClient, aud, eb)
	p.maxRetries = 5

	j := &store.Job{ID: uuid.New(), TenantID: cmd.TenantID}
	if err := p.Process(context.Background(), j); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	cmdStore.mu.Lock()
	defer cmdStore.mu.Unlock()
	if len(cmdStore.markFailedCalls) != 1 {
		t.Errorf("MarkFailed calls = %d, want 1 (terminal after max retries)", len(cmdStore.markFailedCalls))
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

func TestESimOTADispatcher_RateLimitBlocks(t *testing.T) {
	var cmds []store.EsimOTACommand
	opID := uuid.New()
	for i := 0; i < 5; i++ {
		cmds = append(cmds, store.EsimOTACommand{
			ID:               uuid.New(),
			TenantID:         uuid.New(),
			EID:              fmt.Sprintf("8900000000000000%04d", i),
			CommandType:      "switch",
			Status:           "queued",
			TargetOperatorID: &opID,
		})
	}

	cmdStore := newFakeDispatcherCommandStore(cmds...)
	profStore := &fakeDispatcherProfileStore{}
	smsrClient := &fakeDispatcherSMSRClient{}
	aud := &fakeDispatcherAudit{}
	eb := &fakeDispatcherEventBus{}

	p := newTestDispatcher(cmdStore, profStore, smsrClient, aud, eb)
	p.rateLimitRPS = 1

	j := &store.Job{ID: uuid.New()}
	if err := p.Process(context.Background(), j); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	cmdStore.mu.Lock()
	defer cmdStore.mu.Unlock()
	if len(cmdStore.markSentCalls) > 2 {
		t.Errorf("expected at most 2 dispatches with rate limit=1 burst=1; got %d", len(cmdStore.markSentCalls))
	}
}

func TestESimOTADispatcher_PermanentRejection_TerminalImmediate(t *testing.T) {
	profID := uuid.New()
	cmd := store.EsimOTACommand{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		EID:         "89000000000000000004",
		CommandType: "switch",
		Status:      "queued",
		RetryCount:  0,
		ProfileID:   &profID,
	}

	cmdStore := newFakeDispatcherCommandStore(cmd)
	profStore := &fakeDispatcherProfileStore{}
	smsrClient := &fakeDispatcherSMSRClient{
		pushFn: func(_ smsr.PushRequest) (smsr.PushResponse, error) {
			return smsr.PushResponse{}, smsr.ErrSMSRRejected
		},
	}
	aud := &fakeDispatcherAudit{}
	eb := &fakeDispatcherEventBus{}

	p := newTestDispatcher(cmdStore, profStore, smsrClient, aud, eb)

	j := &store.Job{ID: uuid.New(), TenantID: cmd.TenantID}
	if err := p.Process(context.Background(), j); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	cmdStore.mu.Lock()
	defer cmdStore.mu.Unlock()
	if len(cmdStore.markFailedCalls) != 1 {
		t.Errorf("MarkFailed calls = %d, want 1 (permanent rejection)", len(cmdStore.markFailedCalls))
	}
	if cmdStore.retryCount[cmd.ID] != 0 {
		t.Errorf("IncrementRetry should not be called on permanent rejection")
	}
}

func TestESimOTADispatcher_AuditCalledPerDispatch(t *testing.T) {
	var cmds []store.EsimOTACommand
	for i := 0; i < 3; i++ {
		cmds = append(cmds, store.EsimOTACommand{
			ID:          uuid.New(),
			TenantID:    uuid.New(),
			EID:         fmt.Sprintf("8900000000000000%04d", i),
			CommandType: "switch",
			Status:      "queued",
		})
	}

	cmdStore := newFakeDispatcherCommandStore(cmds...)
	profStore := &fakeDispatcherProfileStore{}
	smsrClient := &fakeDispatcherSMSRClient{}
	aud := &fakeDispatcherAudit{}
	eb := &fakeDispatcherEventBus{}

	p := newTestDispatcher(cmdStore, profStore, smsrClient, aud, eb)

	j := &store.Job{ID: uuid.New()}
	if err := p.Process(context.Background(), j); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	aud.mu.Lock()
	defer aud.mu.Unlock()
	if len(aud.calls) != 3 {
		t.Errorf("audit entries = %d, want 3 (one per dispatch)", len(aud.calls))
	}
}
