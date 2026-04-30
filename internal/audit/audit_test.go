package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestGenesisHash(t *testing.T) {
	if len(GenesisHash) != 64 {
		t.Fatalf("GenesisHash length = %d, want 64", len(GenesisHash))
	}
	for _, c := range GenesisHash {
		if c != '0' {
			t.Fatalf("GenesisHash contains non-zero char: %c", c)
		}
	}
}

func TestComputeHash_Deterministic(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ts := time.Date(2026, 3, 18, 14, 2, 0, 123456789, time.UTC)

	entry := Entry{
		TenantID:   tenantID,
		UserID:     &userID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "abc-123",
		CreatedAt:  ts,
	}

	hash1 := ComputeHash(entry, GenesisHash)
	hash2 := ComputeHash(entry, GenesisHash)

	if hash1 != hash2 {
		t.Fatalf("ComputeHash not deterministic: %s != %s", hash1, hash2)
	}

	if len(hash1) != 64 {
		t.Fatalf("hash length = %d, want 64", len(hash1))
	}
}

func TestComputeHash_NilUserID(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 18, 14, 2, 0, 0, time.UTC)

	entry := Entry{
		TenantID:   tenantID,
		UserID:     nil,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "abc-123",
		CreatedAt:  ts,
	}

	hash := ComputeHash(entry, GenesisHash)
	if len(hash) != 64 {
		t.Fatalf("hash length = %d, want 64", len(hash))
	}

	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	entryWithUser := entry
	entryWithUser.UserID = &userID

	hashWithUser := ComputeHash(entryWithUser, GenesisHash)
	if hash == hashWithUser {
		t.Fatal("nil userID should produce different hash than non-nil userID")
	}
}

func TestComputeHash_DifferentPrevHash(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 18, 14, 2, 0, 0, time.UTC)

	entry := Entry{
		TenantID:   tenantID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "abc-123",
		CreatedAt:  ts,
	}

	hash1 := ComputeHash(entry, GenesisHash)
	hash2 := ComputeHash(entry, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	if hash1 == hash2 {
		t.Fatal("different prev_hash should produce different hash")
	}
}

func TestComputeDiff_CreateOperation(t *testing.T) {
	after := json.RawMessage(`{"name":"test","email":"test@example.com"}`)
	diff := ComputeDiff(nil, after)
	if diff == nil {
		t.Fatal("diff should not be nil for create operation")
	}

	var result map[string]map[string]interface{}
	if err := json.Unmarshal(diff, &result); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}

	if result["name"]["from"] != nil {
		t.Fatalf("name.from = %v, want nil", result["name"]["from"])
	}
	if result["name"]["to"] != "test" {
		t.Fatalf("name.to = %v, want test", result["name"]["to"])
	}
}

func TestComputeDiff_UpdateOperation(t *testing.T) {
	before := json.RawMessage(`{"name":"old","state":"active"}`)
	after := json.RawMessage(`{"name":"new","state":"active"}`)
	diff := ComputeDiff(before, after)
	if diff == nil {
		t.Fatal("diff should not be nil for update with changes")
	}

	var result map[string]map[string]interface{}
	if err := json.Unmarshal(diff, &result); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}

	if _, ok := result["state"]; ok {
		t.Fatal("state should not be in diff (unchanged)")
	}

	if result["name"]["from"] != "old" {
		t.Fatalf("name.from = %v, want old", result["name"]["from"])
	}
	if result["name"]["to"] != "new" {
		t.Fatalf("name.to = %v, want new", result["name"]["to"])
	}
}

func TestComputeDiff_NoChanges(t *testing.T) {
	before := json.RawMessage(`{"name":"same","state":"active"}`)
	after := json.RawMessage(`{"name":"same","state":"active"}`)
	diff := ComputeDiff(before, after)
	if diff != nil {
		t.Fatalf("diff should be nil when no changes, got %s", string(diff))
	}
}

func TestComputeDiff_DeleteOperation(t *testing.T) {
	before := json.RawMessage(`{"name":"test","email":"test@example.com"}`)
	diff := ComputeDiff(before, nil)
	if diff == nil {
		t.Fatal("diff should not be nil for delete operation")
	}

	var result map[string]map[string]interface{}
	if err := json.Unmarshal(diff, &result); err != nil {
		t.Fatalf("unmarshal diff: %v", err)
	}

	if result["name"]["from"] != "test" {
		t.Fatalf("name.from = %v, want test", result["name"]["from"])
	}
	if result["name"]["to"] != nil {
		t.Fatalf("name.to = %v, want nil", result["name"]["to"])
	}
}

func TestComputeDiff_BothNil(t *testing.T) {
	diff := ComputeDiff(nil, nil)
	if diff != nil {
		t.Fatalf("diff should be nil when both inputs are nil, got %s", string(diff))
	}
}

func TestVerifyChain_ValidChain(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)

	entries := make([]Entry, 3)

	entries[0] = Entry{
		ID:         1,
		TenantID:   tenantID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   GenesisHash,
		CreatedAt:  ts,
	}
	entries[0].Hash = ComputeHash(entries[0], GenesisHash)

	entries[1] = Entry{
		ID:         2,
		TenantID:   tenantID,
		Action:     "update",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   entries[0].Hash,
		CreatedAt:  ts.Add(1 * time.Minute),
	}
	entries[1].Hash = ComputeHash(entries[1], entries[0].Hash)

	entries[2] = Entry{
		ID:         3,
		TenantID:   tenantID,
		Action:     "delete",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   entries[1].Hash,
		CreatedAt:  ts.Add(2 * time.Minute),
	}
	entries[2].Hash = ComputeHash(entries[2], entries[1].Hash)

	result := VerifyChain(entries)
	if !result.Verified {
		t.Fatalf("chain should be verified, got first_invalid=%v", result.FirstInvalid)
	}
	if result.EntriesChecked != 3 {
		t.Fatalf("entries_checked = %d, want 3", result.EntriesChecked)
	}
}

func TestVerifyChain_TamperedEntry(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)

	entries := make([]Entry, 3)

	entries[0] = Entry{
		ID:         1,
		TenantID:   tenantID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   GenesisHash,
		CreatedAt:  ts,
	}
	entries[0].Hash = ComputeHash(entries[0], GenesisHash)

	entries[1] = Entry{
		ID:         2,
		TenantID:   tenantID,
		Action:     "update",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   entries[0].Hash,
		CreatedAt:  ts.Add(1 * time.Minute),
	}
	entries[1].Hash = ComputeHash(entries[1], entries[0].Hash)

	entries[2] = Entry{
		ID:         3,
		TenantID:   tenantID,
		Action:     "delete",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   entries[1].Hash,
		CreatedAt:  ts.Add(2 * time.Minute),
	}
	entries[2].Hash = ComputeHash(entries[2], entries[1].Hash)

	entries[1].Action = "tampered"

	result := VerifyChain(entries)
	if result.Verified {
		t.Fatal("chain should NOT be verified after tampering")
	}
	if result.FirstInvalid == nil {
		t.Fatal("first_invalid should not be nil")
	}
	if *result.FirstInvalid != 2 {
		t.Fatalf("first_invalid = %d, want 2", *result.FirstInvalid)
	}
}

func TestVerifyChain_BrokenLink(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)

	entries := make([]Entry, 2)

	entries[0] = Entry{
		ID:         1,
		TenantID:   tenantID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   GenesisHash,
		CreatedAt:  ts,
	}
	entries[0].Hash = ComputeHash(entries[0], GenesisHash)

	entries[1] = Entry{
		ID:         2,
		TenantID:   tenantID,
		Action:     "update",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   "wrong_prev_hash_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CreatedAt:  ts.Add(1 * time.Minute),
	}
	entries[1].Hash = ComputeHash(entries[1], "wrong_prev_hash_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	result := VerifyChain(entries)
	if result.Verified {
		t.Fatal("chain should NOT be verified with broken link")
	}
	if *result.FirstInvalid != 2 {
		t.Fatalf("first_invalid = %d, want 2", *result.FirstInvalid)
	}
}

func TestVerifyChain_SingleEntry(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)

	entry := Entry{
		ID:         1,
		TenantID:   tenantID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   GenesisHash,
		CreatedAt:  ts,
	}
	entry.Hash = ComputeHash(entry, GenesisHash)

	result := VerifyChain([]Entry{entry})
	if !result.Verified {
		t.Fatal("single entry should verify")
	}
	if result.EntriesChecked != 1 {
		t.Fatalf("entries_checked = %d, want 1", result.EntriesChecked)
	}
}

func TestVerifyChain_Empty(t *testing.T) {
	result := VerifyChain([]Entry{})
	if !result.Verified {
		t.Fatal("empty entries should verify")
	}
	if result.EntriesChecked != 0 {
		t.Fatalf("entries_checked = %d, want 0", result.EntriesChecked)
	}
}

type mockAuditStore struct {
	mu      sync.Mutex
	entries []Entry
	nextID  int64
}

func (m *mockAuditStore) CreateWithChain(_ context.Context, entry *Entry) (*Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prevHash := GenesisHash
	if len(m.entries) > 0 {
		prevHash = m.entries[len(m.entries)-1].Hash
	}

	entry.PrevHash = prevHash
	entry.Hash = ComputeHash(*entry, prevHash)
	m.nextID++
	entry.ID = m.nextID
	m.entries = append(m.entries, *entry)
	return entry, nil
}

func (m *mockAuditStore) GetAll(_ context.Context) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Entry, len(m.entries))
	copy(result, m.entries)
	return result, nil
}

func (m *mockAuditStore) GetBatch(_ context.Context, afterID int64, limit int) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []Entry
	for _, e := range m.entries {
		if e.ID > afterID {
			result = append(result, e)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

type mockPublisher struct {
	mu       sync.Mutex
	messages []struct {
		Subject string
		Payload interface{}
	}
}

func (m *mockPublisher) Publish(_ context.Context, subject string, payload interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, struct {
		Subject string
		Payload interface{}
	}{subject, payload})
	return nil
}

func TestFullService_ProcessEntry(t *testing.T) {
	store := &mockAuditStore{}
	svc := NewFullService(store, nil, zerolog.Nop())

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	event := AuditEvent{
		TenantID:   tenantID,
		UserID:     &userID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
		AfterData:  json.RawMessage(`{"name":"test"}`),
	}

	if err := svc.ProcessEntry(context.Background(), event); err != nil {
		t.Fatalf("ProcessEntry: %v", err)
	}

	if len(store.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(store.entries))
	}

	entry := store.entries[0]
	if entry.PrevHash != GenesisHash {
		t.Fatalf("first entry prev_hash = %s, want GenesisHash", entry.PrevHash)
	}
	if entry.Hash == "" || len(entry.Hash) != 64 {
		t.Fatalf("hash length = %d, want 64", len(entry.Hash))
	}
	if entry.Action != "create" {
		t.Fatalf("action = %s, want create", entry.Action)
	}
	if entry.Diff == nil {
		t.Fatal("diff should not be nil for create with after_data")
	}
}

func TestFullService_ProcessEntry_ChainIntegrity(t *testing.T) {
	store := &mockAuditStore{}
	svc := NewFullService(store, nil, zerolog.Nop())

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	for i := 0; i < 5; i++ {
		event := AuditEvent{
			TenantID:   tenantID,
			Action:     "update",
			EntityType: "sim",
			EntityID:   fmt.Sprintf("sim-%d", i),
			AfterData:  json.RawMessage(fmt.Sprintf(`{"step":%d}`, i)),
		}
		if err := svc.ProcessEntry(context.Background(), event); err != nil {
			t.Fatalf("ProcessEntry %d: %v", i, err)
		}
	}

	if len(store.entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(store.entries))
	}

	result := VerifyChain(store.entries)
	if !result.Verified {
		t.Fatalf("chain should be verified, first_invalid=%v", result.FirstInvalid)
	}
}

func TestFullService_PublishAuditEvent(t *testing.T) {
	pub := &mockPublisher{}
	svc := NewFullService(nil, pub, zerolog.Nop())

	event := AuditEvent{
		TenantID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Action:     "create",
		EntityType: "user",
		EntityID:   "user-1",
	}

	if err := svc.PublishAuditEvent(context.Background(), event); err != nil {
		t.Fatalf("PublishAuditEvent: %v", err)
	}

	if len(pub.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(pub.messages))
	}
	if pub.messages[0].Subject != "argus.events.audit.create" {
		t.Fatalf("subject = %s, want argus.events.audit.create", pub.messages[0].Subject)
	}
}

func TestFullService_PublishAuditEvent_NilPublisher(t *testing.T) {
	svc := NewFullService(nil, nil, zerolog.Nop())

	event := AuditEvent{
		TenantID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Action:     "create",
		EntityType: "user",
		EntityID:   "user-1",
	}

	if err := svc.PublishAuditEvent(context.Background(), event); err != nil {
		t.Fatalf("PublishAuditEvent with nil publisher should not fail: %v", err)
	}
}

func TestFullService_VerifyChain(t *testing.T) {
	store := &mockAuditStore{}
	svc := NewFullService(store, nil, zerolog.Nop())

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	for i := 0; i < 3; i++ {
		event := AuditEvent{
			TenantID:   tenantID,
			Action:     "create",
			EntityType: "sim",
			EntityID:   fmt.Sprintf("sim-%d", i),
		}
		if err := svc.ProcessEntry(context.Background(), event); err != nil {
			t.Fatalf("ProcessEntry %d: %v", i, err)
		}
	}

	result, err := svc.VerifyChain(context.Background())
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !result.Verified {
		t.Fatal("chain should be verified")
	}
	if result.EntriesChecked != 3 {
		t.Fatalf("entries_checked = %d, want 3", result.EntriesChecked)
	}
	if result.TotalRows != 3 {
		t.Fatalf("total_rows = %d, want 3", result.TotalRows)
	}
}

func TestFullService_CreateEntry_WithPublisher(t *testing.T) {
	pub := &mockPublisher{}
	store := &mockAuditStore{}
	svc := NewFullService(store, pub, zerolog.Nop())

	params := CreateEntryParams{
		TenantID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Action:     "create",
		EntityType: "tenant",
		EntityID:   "tenant-1",
	}

	_, err := svc.CreateEntry(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	if len(pub.messages) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(pub.messages))
	}
}

func TestFullService_CreateEntry_WithoutPublisher(t *testing.T) {
	store := &mockAuditStore{}
	svc := NewFullService(store, nil, zerolog.Nop())

	params := CreateEntryParams{
		TenantID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Action:     "create",
		EntityType: "tenant",
		EntityID:   "tenant-1",
		AfterData:  json.RawMessage(`{"name":"test"}`),
	}

	_, err := svc.CreateEntry(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}

	if len(store.entries) != 1 {
		t.Fatalf("expected 1 store entry (inline processing), got %d", len(store.entries))
	}
}

func TestVerifyChain_FirstEntryNotGenesis(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)

	badPrev := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	entry := Entry{
		ID:         1,
		TenantID:   tenantID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   badPrev,
		CreatedAt:  ts,
	}
	entry.Hash = ComputeHash(entry, badPrev)

	result := VerifyChain([]Entry{entry})
	if result.Verified {
		t.Fatal("chain with non-genesis first entry should NOT verify")
	}
	if result.FirstInvalid == nil || *result.FirstInvalid != 1 {
		t.Fatalf("first_invalid = %v, want 1", result.FirstInvalid)
	}
}

func TestVerifyChain_TamperDetection(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	store := &mockAuditStore{}
	svc := NewFullService(store, nil, zerolog.Nop())

	for i := 0; i < 5; i++ {
		event := AuditEvent{
			TenantID:   tenantID,
			Action:     fmt.Sprintf("action.%d", i),
			EntityType: "sim",
			EntityID:   fmt.Sprintf("sim-%d", i),
			AfterData:  json.RawMessage(fmt.Sprintf(`{"step":%d}`, i)),
		}
		if err := svc.ProcessEntry(context.Background(), event); err != nil {
			t.Fatalf("ProcessEntry %d: %v", i, err)
		}
	}

	result := VerifyChain(store.entries)
	if !result.Verified {
		t.Fatal("chain should be valid before tampering")
	}

	store.entries[2].Action = "tampered.action"

	result = VerifyChain(store.entries)
	if result.Verified {
		t.Fatal("chain should NOT verify after tampering with action field")
	}
	if result.FirstInvalid == nil {
		t.Fatal("first_invalid should not be nil")
	}
	if *result.FirstInvalid != store.entries[2].ID {
		t.Fatalf("first_invalid = %d, want %d (tampered row)", *result.FirstInvalid, store.entries[2].ID)
	}
}

func TestFullService_ConcurrentWrites_ChainIntegrity(t *testing.T) {
	store := &mockAuditStore{}
	svc := NewFullService(store, nil, zerolog.Nop())

	tenants := []uuid.UUID{
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		uuid.MustParse("00000000-0000-0000-0000-000000000000"),
	}
	actions := []string{"sim.create", "operator.update", "tenant.delete", "policy.activate", "user.login"}

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				event := AuditEvent{
					TenantID:   tenants[goroutineID%len(tenants)],
					Action:     actions[(goroutineID+i)%len(actions)],
					EntityType: "test",
					EntityID:   fmt.Sprintf("g%d-e%d", goroutineID, i),
					AfterData:  json.RawMessage(fmt.Sprintf(`{"g":%d,"i":%d}`, goroutineID, i)),
				}
				if err := svc.ProcessEntry(context.Background(), event); err != nil {
					errs <- fmt.Errorf("goroutine %d entry %d: %w", goroutineID, i, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent write error: %v", err)
	}

	if len(store.entries) != 100 {
		t.Fatalf("entries = %d, want 100", len(store.entries))
	}

	result := VerifyChain(store.entries)
	if !result.Verified {
		t.Fatalf("chain should be verified after concurrent writes, first_invalid=%v", result.FirstInvalid)
	}
	if result.TotalRows != 100 {
		t.Fatalf("total_rows = %d, want 100", result.TotalRows)
	}
}

func TestFullService_SystemEvent_GlobalChain(t *testing.T) {
	store := &mockAuditStore{}
	svc := NewFullService(store, nil, zerolog.Nop())

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	if err := svc.ProcessEntry(context.Background(), AuditEvent{
		TenantID:   tenantID,
		Action:     "sim.create",
		EntityType: "sim",
		EntityID:   "sim-1",
	}); err != nil {
		t.Fatalf("ProcessEntry tenant event: %v", err)
	}

	if err := svc.ProcessEntry(context.Background(), AuditEvent{
		TenantID:   uuid.Nil,
		Action:     "bluegreen_flip",
		EntityType: "deployment",
		EntityID:   "deploy-1",
	}); err != nil {
		t.Fatalf("ProcessEntry system event: %v", err)
	}

	if err := svc.ProcessEntry(context.Background(), AuditEvent{
		TenantID:   tenantID,
		Action:     "sim.update",
		EntityType: "sim",
		EntityID:   "sim-2",
	}); err != nil {
		t.Fatalf("ProcessEntry tenant event 2: %v", err)
	}

	if len(store.entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(store.entries))
	}

	if store.entries[1].TenantID != uuid.Nil {
		t.Fatal("second entry should have uuid.Nil tenant (system event)")
	}

	result := VerifyChain(store.entries)
	if !result.Verified {
		t.Fatalf("global chain with system event should verify, first_invalid=%v", result.FirstInvalid)
	}
}

func TestVerifyChain_TotalRowsField(t *testing.T) {
	result := VerifyChain([]Entry{})
	if result.TotalRows != 0 {
		t.Fatalf("total_rows = %d, want 0 for empty chain", result.TotalRows)
	}

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ts := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)

	entry := Entry{
		ID:         1,
		TenantID:   tenantID,
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
		PrevHash:   GenesisHash,
		CreatedAt:  ts,
	}
	entry.Hash = ComputeHash(entry, GenesisHash)

	result = VerifyChain([]Entry{entry})
	if result.TotalRows != 1 {
		t.Fatalf("total_rows = %d, want 1", result.TotalRows)
	}
}

// FIX-302 AC-6: ComputeHash must be invariant across time.Locations for the
// same instant. The original bug: pgx returned timestamptz in PG server tz
// (Asia/Istanbul +03:00) while inserts used time.Now().UTC() — same instant,
// different RFC3339Nano string, different hash.
func TestComputeHash_TimezoneInvariant(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	istanbul, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatalf("load Istanbul: %v", err)
	}
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load LA: %v", err)
	}

	// Same instant in three different zones
	utcTime := time.Date(2026, 4, 30, 10, 20, 51, 667343000, time.UTC)
	istanbulTime := utcTime.In(istanbul)
	laTime := utcTime.In(la)

	mkEntry := func(ts time.Time) Entry {
		return Entry{
			TenantID:   tenantID,
			Action:     "create",
			EntityType: "sim",
			EntityID:   "abc-123",
			CreatedAt:  ts,
		}
	}

	hUTC := ComputeHash(mkEntry(utcTime), GenesisHash)
	hIstanbul := ComputeHash(mkEntry(istanbulTime), GenesisHash)
	hLA := ComputeHash(mkEntry(laTime), GenesisHash)

	if hUTC != hIstanbul {
		t.Errorf("UTC vs Istanbul hash mismatch:\n  utc:      %s\n  istanbul: %s", hUTC, hIstanbul)
	}
	if hUTC != hLA {
		t.Errorf("UTC vs LA hash mismatch:\n  utc: %s\n  la:  %s", hUTC, hLA)
	}
}

// FIX-302 AC-7: Trailing-zero microseconds must hash deterministically.
// time.RFC3339Nano strips trailing zeros (.650190 → .65019); PG to_char('.US')
// emits fixed 6 digits. The canonical layout matches PG (always 6 zero-padded
// digits), so the hash is stable across any sub-second value.
func TestComputeHash_TrailingZeroMicroseconds(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	cases := []struct {
		name string
		ts   time.Time
	}{
		{"trailing-zero-100", time.Date(2026, 4, 30, 10, 20, 51, 100000000, time.UTC)},   // .100000
		{"trailing-zero-65019", time.Date(2026, 4, 30, 10, 20, 51, 650190000, time.UTC)}, // .650190
		{"no-trailing-zero", time.Date(2026, 4, 30, 10, 20, 51, 123456000, time.UTC)},    // .123456
		{"all-zero-micro", time.Date(2026, 4, 30, 10, 20, 51, 0, time.UTC)},              // .000000
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry := Entry{
				TenantID:   tenantID,
				Action:     "create",
				EntityType: "sim",
				EntityID:   "abc-123",
				CreatedAt:  c.ts,
			}
			h1 := ComputeHash(entry, GenesisHash)
			// Determinism: re-hash same entry → same result
			h2 := ComputeHash(entry, GenesisHash)
			if h1 != h2 {
				t.Errorf("non-deterministic hash for %s: %s vs %s", c.name, h1, h2)
			}
			// Format must include exactly 6 microsecond digits + Z
			formatted := c.ts.UTC().Format(canonicalAuditTimeLayout)
			if len(formatted) != len("2026-04-30T10:20:51.000000Z") {
				t.Errorf("formatted length wrong for %s: %q", c.name, formatted)
			}
			if formatted[len(formatted)-1] != 'Z' {
				t.Errorf("formatted does not end with Z: %q", formatted)
			}
		})
	}
}
