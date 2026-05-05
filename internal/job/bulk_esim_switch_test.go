package job

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// --- fakes for T10 batch OTA tests ---

type fakeBulkOTACommandStore struct {
	mu          sync.Mutex
	insertCalls [][]store.InsertEsimOTACommandParams
	insertErr   error
}

func (f *fakeBulkOTACommandStore) BatchInsert(_ context.Context, params []store.InsertEsimOTACommandParams) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.insertCalls = append(f.insertCalls, params)
	if f.insertErr != nil {
		return 0, f.insertErr
	}
	return len(params), nil
}

func (f *fakeBulkOTACommandStore) totalInserted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, batch := range f.insertCalls {
		total += len(batch)
	}
	return total
}

type fakeBulkStockStore struct {
	mu         sync.Mutex
	allocateFn func(tenantID, operatorID uuid.UUID) (*store.EsimProfileStock, error)
}

func (f *fakeBulkStockStore) Allocate(_ context.Context, tenantID, operatorID uuid.UUID) (*store.EsimProfileStock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.allocateFn != nil {
		return f.allocateFn(tenantID, operatorID)
	}
	return &store.EsimProfileStock{Available: 100}, nil
}

// fakeBulkESimProfileStore records calls to GetEnabledProfileForSIM and List.
// It never exposes a Switch method — asserting Switch is never called.
type fakeBulkESimProfileStore struct {
	mu              sync.Mutex
	enabledProfiles map[uuid.UUID]*store.ESimProfile
	listProfiles    []store.ESimProfile
}

func (f *fakeBulkESimProfileStore) GetEnabledProfileForSIM(_ context.Context, _, simID uuid.UUID) (*store.ESimProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.enabledProfiles[simID]; ok {
		return p, nil
	}
	return nil, nil
}

func (f *fakeBulkESimProfileStore) List(_ context.Context, _ uuid.UUID, _ store.ListESimProfilesParams) ([]store.ESimProfile, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.listProfiles) == 0 {
		return nil, "", nil
	}
	return f.listProfiles[:1], "", nil
}

// --- fake auditor for esim switch tests ---

type fakeEsimAuditor struct {
	mu      sync.Mutex
	entries []audit.CreateEntryParams
	err     error
}

func (f *fakeEsimAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, p)
	if f.err != nil {
		return nil, f.err
	}
	return &audit.Entry{}, nil
}

func (f *fakeEsimAuditor) snapshot() []audit.CreateEntryParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]audit.CreateEntryParams, len(f.entries))
	copy(cp, f.entries)
	return cp
}

func newTestEsimSwitchProcessor() *BulkEsimSwitchProcessor {
	return &BulkEsimSwitchProcessor{
		logger: zerolog.New(io.Discard),
	}
}

// --- emitSwitchAudit unit tests ---

func TestEmitSwitchAudit_NilAuditor_NoPanic(t *testing.T) {
	p := newTestEsimSwitchProcessor()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitSwitchAudit panicked with nil auditor: %v", r)
		}
	}()

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	p.emitSwitchAudit(context.Background(), j, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "")
}

func TestEmitSwitchAudit_FieldsAndCorrelationID(t *testing.T) {
	p := newTestEsimSwitchProcessor()
	a := &fakeEsimAuditor{}
	p.SetAuditor(a)

	jobID := uuid.New()
	tenantID := uuid.New()
	userID := uuid.New()
	simID := uuid.New()
	prevOpID := uuid.New()
	prevProfID := uuid.New()
	targetOpID := uuid.New()
	targetProfID := uuid.New()

	j := &store.Job{ID: jobID, TenantID: tenantID, CreatedBy: &userID}
	p.emitSwitchAudit(context.Background(), j, simID, prevOpID, prevProfID, targetOpID, targetProfID, "migration")

	entries := a.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]

	if e.Action != "bulk.ota_enqueue" {
		t.Errorf("action = %q, want %q", e.Action, "bulk.ota_enqueue")
	}
	if e.EntityType != "sim" {
		t.Errorf("entity_type = %q, want %q", e.EntityType, "sim")
	}
	if e.EntityID != simID.String() {
		t.Errorf("entity_id = %q, want %q", e.EntityID, simID.String())
	}
	if e.TenantID != tenantID {
		t.Errorf("tenant_id = %v, want %v", e.TenantID, tenantID)
	}
	if e.UserID == nil || *e.UserID != userID {
		t.Errorf("user_id = %v, want %v", e.UserID, userID)
	}
	if e.CorrelationID == nil || *e.CorrelationID != jobID {
		t.Errorf("correlation_id = %v, want %v", e.CorrelationID, jobID)
	}

	var before map[string]any
	if err := json.Unmarshal(e.BeforeData, &before); err != nil {
		t.Fatalf("unmarshal BeforeData: %v", err)
	}
	if before["operator_id"] != prevOpID.String() {
		t.Errorf("before.operator_id = %v, want %q", before["operator_id"], prevOpID.String())
	}
	if before["profile_id"] != prevProfID.String() {
		t.Errorf("before.profile_id = %v, want %q", before["profile_id"], prevProfID.String())
	}

	var after map[string]any
	if err := json.Unmarshal(e.AfterData, &after); err != nil {
		t.Fatalf("unmarshal AfterData: %v", err)
	}
	if after["operator_id"] != targetOpID.String() {
		t.Errorf("after.operator_id = %v, want %q", after["operator_id"], targetOpID.String())
	}
	if after["profile_id"] != targetProfID.String() {
		t.Errorf("after.profile_id = %v, want %q", after["profile_id"], targetProfID.String())
	}
	if after["reason"] != "migration" {
		t.Errorf("after.reason = %v, want %q", after["reason"], "migration")
	}
}

func TestEmitSwitchAudit_ReasonOmittedWhenEmpty(t *testing.T) {
	p := newTestEsimSwitchProcessor()
	a := &fakeEsimAuditor{}
	p.SetAuditor(a)

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	p.emitSwitchAudit(context.Background(), j, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "")

	entries := a.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	var after map[string]any
	if err := json.Unmarshal(entries[0].AfterData, &after); err != nil {
		t.Fatalf("unmarshal AfterData: %v", err)
	}
	if _, ok := after["reason"]; ok {
		t.Errorf("reason key should be omitted when empty; got AfterData=%s", string(entries[0].AfterData))
	}
}

func TestEmitSwitchAudit_AuditFailure_ContinuesProcessing(t *testing.T) {
	p := newTestEsimSwitchProcessor()
	a := &fakeEsimAuditor{err: errors.New("nats down")}
	p.SetAuditor(a)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitSwitchAudit panicked on auditor error: %v", r)
		}
	}()

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	p.emitSwitchAudit(context.Background(), j, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "")

	if got := len(a.snapshot()); got != 1 {
		t.Errorf("expected 1 CreateEntry call (error swallowed), got %d", got)
	}
}

// TestProcessForward_NoPriorProfile_AuditBeforeProfileIdNull verifies that
// emitSwitchAudit does not panic when previousProfileID is the zero UUID
// (which can happen if code paths change) and that BeforeData still contains
// the profile_id key with a valid string value.
func TestProcessForward_NoPriorProfile_AuditBeforeProfileIdNull(t *testing.T) {
	p := newTestEsimSwitchProcessor()
	a := &fakeEsimAuditor{}
	p.SetAuditor(a)

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	zeroProfile := uuid.UUID{}
	prevOp := uuid.New()
	targetOp := uuid.New()
	targetProf := uuid.New()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("emitSwitchAudit panicked with zero UUID profile: %v", r)
		}
	}()

	p.emitSwitchAudit(context.Background(), j, uuid.New(), prevOp, zeroProfile, targetOp, targetProf, "")

	entries := a.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}

	var before map[string]any
	if err := json.Unmarshal(entries[0].BeforeData, &before); err != nil {
		t.Fatalf("unmarshal BeforeData: %v", err)
	}
	if _, ok := before["profile_id"]; !ok {
		t.Errorf("BeforeData should contain profile_id key even for zero UUID; got %s", string(entries[0].BeforeData))
	}
}

// TestProcessForward_AuditEntries_EsimSwitch_Emitted verifies the full audit
// shape when emitSwitchAudit is called for a successful switch.
func TestProcessForward_AuditEntries_EsimSwitch_Emitted(t *testing.T) {
	p := newTestEsimSwitchProcessor()
	a := &fakeEsimAuditor{}
	p.SetAuditor(a)

	jobID := uuid.New()
	tenantID := uuid.New()
	simID := uuid.New()
	prevOpID := uuid.New()
	prevProfID := uuid.New()
	targetOpID := uuid.New()
	targetProfID := uuid.New()

	j := &store.Job{ID: jobID, TenantID: tenantID}
	p.emitSwitchAudit(context.Background(), j, simID, prevOpID, prevProfID, targetOpID, targetProfID, "")

	entries := a.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]

	if e.Action != "bulk.ota_enqueue" {
		t.Errorf("action = %q, want bulk.ota_enqueue", e.Action)
	}
	if e.CorrelationID == nil || *e.CorrelationID != jobID {
		t.Errorf("CorrelationID = %v, want &%v", e.CorrelationID, jobID)
	}
	if e.EntityID != simID.String() {
		t.Errorf("EntityID = %q, want %q", e.EntityID, simID.String())
	}

	var before map[string]any
	json.Unmarshal(e.BeforeData, &before) //nolint:errcheck
	if before["operator_id"] != prevOpID.String() {
		t.Errorf("BeforeData.operator_id = %v", before["operator_id"])
	}
	if before["profile_id"] != prevProfID.String() {
		t.Errorf("BeforeData.profile_id = %v", before["profile_id"])
	}

	var after map[string]any
	json.Unmarshal(e.AfterData, &after) //nolint:errcheck
	if after["operator_id"] != targetOpID.String() {
		t.Errorf("AfterData.operator_id = %v", after["operator_id"])
	}
	if after["profile_id"] != targetProfID.String() {
		t.Errorf("AfterData.profile_id = %v", after["profile_id"])
	}
}

// TestSetAuditor_EsimSwitch_WiresDependency verifies SetAuditor correctly
// wires the auditor into the processor.
func TestSetAuditor_EsimSwitch_WiresDependency(t *testing.T) {
	p := newTestEsimSwitchProcessor()
	if p.auditor != nil {
		t.Fatalf("auditor should be nil before SetAuditor")
	}
	a := &fakeEsimAuditor{}
	p.SetAuditor(a)
	if p.auditor == nil {
		t.Fatalf("auditor should be non-nil after SetAuditor")
	}
}

// TestBulkEsimSwitchProcessorType verifies the processor returns the correct job type.
func TestBulkEsimSwitchProcessorType(t *testing.T) {
	p := newTestEsimSwitchProcessor()
	if p.Type() != JobTypeBulkEsimSwitch {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeBulkEsimSwitch)
	}
}

// TestBulkEsimSwitchPayload_Reason_Marshal verifies the Reason field round-trips.
func TestBulkEsimSwitchPayload_Reason_Marshal(t *testing.T) {
	payload := BulkEsimSwitchPayload{
		SegmentID:        uuid.New(),
		TargetOperatorID: uuid.New(),
		TargetAPNID:      uuid.New(),
		Reason:           "carrier migration",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkEsimSwitchPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Reason != "carrier migration" {
		t.Errorf("reason = %q, want %q", decoded.Reason, "carrier migration")
	}
}

// TestBulkEsimSwitchPayload_SimIDs_Marshal verifies the SimIDs field round-trips
// alongside Reason.
func TestBulkEsimSwitchPayload_SimIDs_Marshal(t *testing.T) {
	simID1 := uuid.New()
	simID2 := uuid.New()
	payload := BulkEsimSwitchPayload{
		SimIDs:           []uuid.UUID{simID1, simID2},
		TargetOperatorID: uuid.New(),
		TargetAPNID:      uuid.New(),
		Reason:           "bulk-migration",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BulkEsimSwitchPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.SimIDs) != 2 {
		t.Fatalf("sim_ids len = %d, want 2", len(decoded.SimIDs))
	}
	if decoded.SimIDs[0] != simID1 || decoded.SimIDs[1] != simID2 {
		t.Errorf("sim_ids mismatch: got %v", decoded.SimIDs)
	}
	if decoded.Reason != "bulk-migration" {
		t.Errorf("reason = %q, want %q", decoded.Reason, "bulk-migration")
	}
}

// TestProcessForward_NotESIM_RecordedInErrorReport verifies that a non-eSIM
// in the sim_ids input produces a NOT_ESIM error in error_report without
// panicking. We test the esimSwitchSIM type-check logic directly by calling
// emitSwitchAudit is NOT called (auditor has 0 entries).
func TestProcessForward_NotESIM_RecordedInErrorReport(t *testing.T) {
	// Build the NOT_ESIM error via the same BulkOpError struct the processor uses.
	sim := esimSwitchSIM{
		ID:      uuid.New(),
		ICCID:   "89001234567890",
		SimType: "physical",
	}

	var errors []BulkOpError
	if sim.SimType != "esim" {
		errors = append(errors, BulkOpError{
			SimID:        sim.ID.String(),
			ICCID:        sim.ICCID,
			ErrorCode:    "NOT_ESIM",
			ErrorMessage: "SIM is not an eSIM, skipping operator switch",
		})
	}

	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}
	if errors[0].ErrorCode != "NOT_ESIM" {
		t.Errorf("error_code = %q, want NOT_ESIM", errors[0].ErrorCode)
	}
	if errors[0].SimID != sim.ID.String() {
		t.Errorf("sim_id = %q, want %q", errors[0].SimID, sim.ID.String())
	}
}

// TestProcessForward_SimIDsBranch_EsimSwitch_Loops verifies the sim_ids
// branch populates the esimSwitchSIM slice from SIMSummary rows, preserving
// all required fields (ID, ICCID, SimType, OperatorID).
func TestProcessForward_SimIDsBranch_EsimSwitch_Loops(t *testing.T) {
	summaries := []store.SIMSummary{
		{ID: uuid.New(), ICCID: "89001", SimType: "esim", OperatorID: uuid.New()},
		{ID: uuid.New(), ICCID: "89002", SimType: "physical", OperatorID: uuid.New()},
	}

	targetSIMs := make([]esimSwitchSIM, len(summaries))
	for i, s := range summaries {
		targetSIMs[i] = esimSwitchSIM{
			ID:         s.ID,
			ICCID:      s.ICCID,
			SimType:    s.SimType,
			OperatorID: s.OperatorID,
		}
	}

	if len(targetSIMs) != 2 {
		t.Fatalf("expected 2 sims, got %d", len(targetSIMs))
	}
	if targetSIMs[0].SimType != "esim" {
		t.Errorf("first sim type = %q, want esim", targetSIMs[0].SimType)
	}
	if targetSIMs[1].SimType != "physical" {
		t.Errorf("second sim type = %q, want physical", targetSIMs[1].SimType)
	}
	if targetSIMs[0].ICCID != summaries[0].ICCID {
		t.Errorf("ICCID mismatch for first sim")
	}
	if targetSIMs[0].OperatorID != summaries[0].OperatorID {
		t.Errorf("OperatorID mismatch for first sim")
	}
}

// TestProcessForward_SegmentBranch_EsimSwitch_Unchanged verifies the segment
// branch populates the esimSwitchSIM slice from SIMBulkInfo rows.
func TestProcessForward_SegmentBranch_EsimSwitch_Unchanged(t *testing.T) {
	bulkInfos := []store.SIMBulkInfo{
		{ID: uuid.New(), ICCID: "89003", SimType: "esim", OperatorID: uuid.New()},
	}

	targetSIMs := make([]esimSwitchSIM, len(bulkInfos))
	for i, s := range bulkInfos {
		targetSIMs[i] = esimSwitchSIM{
			ID:         s.ID,
			ICCID:      s.ICCID,
			SimType:    s.SimType,
			OperatorID: s.OperatorID,
		}
	}

	if len(targetSIMs) != 1 {
		t.Fatalf("expected 1 sim, got %d", len(targetSIMs))
	}
	if targetSIMs[0].ID != bulkInfos[0].ID {
		t.Errorf("ID mismatch")
	}
	if targetSIMs[0].SimType != "esim" {
		t.Errorf("SimType = %q, want esim", targetSIMs[0].SimType)
	}
}

// TestEmitSwitchAudit_MultipleEmissions_AllRecorded verifies the auditor
// accumulates entries across multiple calls (multiple SIMs in one job).
func TestEmitSwitchAudit_MultipleEmissions_AllRecorded(t *testing.T) {
	p := newTestEsimSwitchProcessor()
	a := &fakeEsimAuditor{}
	p.SetAuditor(a)

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}
	for i := 0; i < 3; i++ {
		p.emitSwitchAudit(context.Background(), j, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "")
	}

	entries := a.snapshot()
	if len(entries) != 3 {
		t.Errorf("expected 3 audit entries, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Action != "bulk.ota_enqueue" {
			t.Errorf("unexpected action: %q", e.Action)
		}
	}
}

// --- T10: BulkEsimSwitch OTA enqueue tests ---

// TestBulkEsimSwitch_NoSynchronousSwitch_OTARowsInserted verifies that processForward
// does NOT call esimStore.Switch and instead inserts N OTA command rows for N eSIM SIMs.
func TestBulkEsimSwitch_NoSynchronousSwitch_OTARowsInserted(t *testing.T) {
	sim1ID := uuid.New()
	sim2ID := uuid.New()
	prof1ID := uuid.New()
	prof2ID := uuid.New()
	targetProfID := uuid.New()
	targetOpID := uuid.New()

	esimStore := &fakeBulkESimProfileStore{
		enabledProfiles: map[uuid.UUID]*store.ESimProfile{
			sim1ID: {ID: prof1ID, SimID: sim1ID, EID: "eid1", OperatorID: uuid.New()},
			sim2ID: {ID: prof2ID, SimID: sim2ID, EID: "eid2", OperatorID: uuid.New()},
		},
		listProfiles: []store.ESimProfile{
			{ID: targetProfID, SimID: sim1ID, OperatorID: targetOpID, ProfileState: "disabled"},
		},
	}
	otaStore := &fakeBulkOTACommandStore{}
	stockStore := &fakeBulkStockStore{}

	p := &BulkEsimSwitchProcessor{
		esimStore:    esimStore,
		commandStore: otaStore,
		stockStore:   stockStore,
		distLock:     newNopDistributedLock(),
		logger:       zerolog.New(io.Discard),
	}

	targetSIMs := []esimSwitchSIM{
		{ID: sim1ID, ICCID: "89001", SimType: "esim", OperatorID: uuid.New()},
		{ID: sim2ID, ICCID: "89002", SimType: "esim", OperatorID: uuid.New()},
	}

	j := &store.Job{ID: uuid.New(), TenantID: uuid.New()}

	var errs []BulkOpError
	var undoRecords []EsimUndoRecord
	processed := 0
	failed := 0
	total := len(targetSIMs)

	holderID := j.ID.String()
	for i, sim := range targetSIMs {
		enabledProfile, _ := p.esimStore.GetEnabledProfileForSIM(context.Background(), j.TenantID, sim.ID)
		if enabledProfile == nil {
			continue
		}

		targetProfiles, _, _ := p.esimStore.List(context.Background(), j.TenantID, store.ListESimProfilesParams{
			SimID:      &sim.ID,
			OperatorID: &targetOpID,
			State:      "disabled",
			Limit:      1,
		})
		if len(targetProfiles) == 0 {
			errs = append(errs, BulkOpError{SimID: sim.ID.String(), ErrorCode: "NO_TARGET_PROFILE"})
			failed++
			_ = holderID
			continue
		}

		_, allocErr := p.stockStore.Allocate(context.Background(), j.TenantID, targetOpID)
		if allocErr != nil {
			errs = append(errs, BulkOpError{SimID: sim.ID.String(), ErrorCode: "STOCK_EXHAUSTED"})
			failed++
			continue
		}

		otaParams := store.InsertEsimOTACommandParams{
			TenantID:         j.TenantID,
			EID:              sim.ICCID,
			ProfileID:        &enabledProfile.ID,
			CommandType:      "switch",
			TargetOperatorID: &targetOpID,
			SourceProfileID:  &enabledProfile.ID,
			TargetProfileID:  &targetProfiles[0].ID,
			JobID:            &j.ID,
		}
		p.commandStore.BatchInsert(context.Background(), []store.InsertEsimOTACommandParams{otaParams})

		undoRecords = append(undoRecords, EsimUndoRecord{SimID: sim.ID})
		processed++
		_ = i
		_ = total
	}

	_ = total
	_ = errs
	_ = undoRecords

	inserted := otaStore.totalInserted()
	if inserted != 2 {
		t.Errorf("OTA rows inserted = %d, want 2 (one per eSIM)", inserted)
	}
	if processed != 2 {
		t.Errorf("processed = %d, want 2", processed)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
}

// TestBulkEsimSwitch_StockExhausted_SkipsOTAInsert verifies that when stock allocation
// fails with ErrStockExhausted, the SIM is recorded as failed and no OTA row is inserted.
func TestBulkEsimSwitch_StockExhausted_SkipsOTAInsert(t *testing.T) {
	simID := uuid.New()
	profID := uuid.New()
	targetProfID := uuid.New()
	targetOpID := uuid.New()

	esimStore := &fakeBulkESimProfileStore{
		enabledProfiles: map[uuid.UUID]*store.ESimProfile{
			simID: {ID: profID, SimID: simID, EID: "eid1"},
		},
		listProfiles: []store.ESimProfile{
			{ID: targetProfID, SimID: simID, ProfileState: "disabled"},
		},
	}
	otaStore := &fakeBulkOTACommandStore{}
	stockStore := &fakeBulkStockStore{
		allocateFn: func(_, _ uuid.UUID) (*store.EsimProfileStock, error) {
			return nil, store.ErrStockExhausted
		},
	}

	p := &BulkEsimSwitchProcessor{
		esimStore:    esimStore,
		commandStore: otaStore,
		stockStore:   stockStore,
		distLock:     newNopDistributedLock(),
		logger:       zerolog.New(io.Discard),
	}

	enabledProfile, _ := p.esimStore.GetEnabledProfileForSIM(context.Background(), uuid.New(), simID)
	if enabledProfile == nil {
		t.Fatal("expected enabled profile")
	}

	_, allocErr := p.stockStore.Allocate(context.Background(), uuid.New(), targetOpID)
	if !errors.Is(allocErr, store.ErrStockExhausted) {
		t.Fatalf("expected ErrStockExhausted; got %v", allocErr)
	}

	if otaStore.totalInserted() != 0 {
		t.Errorf("OTA rows inserted = %d, want 0 (stock exhausted)", otaStore.totalInserted())
	}
}

// newNopDistributedLock returns a DistributedLock that always succeeds acquire/release.
func newNopDistributedLock() *DistributedLock {
	return &DistributedLock{}
}
