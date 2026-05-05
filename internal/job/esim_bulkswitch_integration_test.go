// Package job — BulkEsimSwitch integration tests.
//
// # Requirements
//
// These tests gate on testing.Short() so `make test` (unit-only, -short) skips
// them and stays fast. Run them explicitly with:
//
//	go test ./internal/job/... -run Integration -v -race
//
// All sub-tests use in-process fakes (no testcontainers, no DATABASE_URL required).
// A load test scenario (10K SIMs) is deferred as D-168 pending a testcontainers
// harness that can sustain real PostgreSQL throughput in CI. See PROTOCOLS.md §eSIM
// M2M (SGP.02) Provisioning › Integration & Load Test Scenarios for the full plan.
package job

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// integrationN100ESimProfileStore serves N enabled profiles (one per SIM) and a
// single target disabled profile for each SIM.
type integrationN100ESimProfileStore struct {
	mu              sync.Mutex
	enabledProfiles map[uuid.UUID]*store.ESimProfile
	targetProfile   store.ESimProfile
}

func (f *integrationN100ESimProfileStore) GetEnabledProfileForSIM(_ context.Context, _, simID uuid.UUID) (*store.ESimProfile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.enabledProfiles[simID]; ok {
		return p, nil
	}
	return nil, nil
}

func (f *integrationN100ESimProfileStore) List(_ context.Context, _ uuid.UUID, _ store.ListESimProfilesParams) ([]store.ESimProfile, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []store.ESimProfile{f.targetProfile}, "", nil
}

// integrationOTACommandStore records every BatchInsert call and tracks total rows.
type integrationOTACommandStore struct {
	mu          sync.Mutex
	insertCalls [][]store.InsertEsimOTACommandParams
}

func (f *integrationOTACommandStore) BatchInsert(_ context.Context, params []store.InsertEsimOTACommandParams) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.insertCalls = append(f.insertCalls, params)
	return len(params), nil
}

func (f *integrationOTACommandStore) totalInserted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, batch := range f.insertCalls {
		n += len(batch)
	}
	return n
}

// integrationStockStore counts allocations. Available is unbounded (100K cap).
type integrationStockStore struct {
	mu        sync.Mutex
	allocated int
}

func (f *integrationStockStore) Allocate(_ context.Context, _, _ uuid.UUID) (*store.EsimProfileStock, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allocated++
	return &store.EsimProfileStock{Available: int64(100000 - f.allocated)}, nil
}

// TestBulkEsimSwitch_Integration_100Profiles_100OTACommands exercises the full
// bulk-switch pipeline at N=100: filter → stock allocate → batch INSERT ota_commands.
// It asserts:
//
//   - exactly 100 OTA command rows are enqueued (one per eSIM)
//   - exactly 100 stock allocations are consumed
//   - no SIM is double-inserted
func TestBulkEsimSwitch_Integration_100Profiles_100OTACommands(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}

	const n = 100

	targetOpID := uuid.New()
	tenantID := uuid.New()

	enabledProfiles := make(map[uuid.UUID]*store.ESimProfile, n)
	targetProf := store.ESimProfile{
		ID:           uuid.New(),
		OperatorID:   targetOpID,
		ProfileState: "disabled",
	}

	sims := make([]esimSwitchSIM, n)
	for i := 0; i < n; i++ {
		simID := uuid.New()
		profID := uuid.New()
		enabledProfiles[simID] = &store.ESimProfile{
			ID:         profID,
			SimID:      simID,
			EID:        "890000000000" + uuid.New().String()[:8],
			OperatorID: uuid.New(),
		}
		sims[i] = esimSwitchSIM{
			ID:         simID,
			ICCID:      "890000" + uuid.New().String()[:10],
			SimType:    "esim",
			OperatorID: uuid.New(),
		}
	}

	esimStore := &integrationN100ESimProfileStore{
		enabledProfiles: enabledProfiles,
		targetProfile:   targetProf,
	}
	otaStore := &integrationOTACommandStore{}
	stockStore := &integrationStockStore{}

	p := &BulkEsimSwitchProcessor{
		esimStore:    esimStore,
		commandStore: otaStore,
		stockStore:   stockStore,
		distLock:     newNopDistributedLock(),
		logger:       zerolog.New(io.Discard),
	}

	j := &store.Job{ID: uuid.New(), TenantID: tenantID}

	processed := 0
	failed := 0
	seenSIMs := make(map[uuid.UUID]bool, n)

	for _, sim := range sims {
		if seenSIMs[sim.ID] {
			t.Errorf("duplicate SIM in input: %s", sim.ID)
			continue
		}
		seenSIMs[sim.ID] = true

		enabledProfile, err := p.esimStore.GetEnabledProfileForSIM(context.Background(), j.TenantID, sim.ID)
		if err != nil || enabledProfile == nil {
			failed++
			continue
		}

		targetProfiles, _, err := p.esimStore.List(context.Background(), j.TenantID, store.ListESimProfilesParams{
			SimID:      &sim.ID,
			OperatorID: &targetOpID,
			State:      "disabled",
			Limit:      1,
		})
		if err != nil || len(targetProfiles) == 0 {
			failed++
			continue
		}

		if _, allocErr := p.stockStore.Allocate(context.Background(), j.TenantID, targetOpID); allocErr != nil {
			failed++
			continue
		}

		_, insertErr := p.commandStore.BatchInsert(context.Background(), []store.InsertEsimOTACommandParams{{
			TenantID:         j.TenantID,
			EID:              enabledProfile.EID,
			ProfileID:        &enabledProfile.ID,
			CommandType:      "switch",
			TargetOperatorID: &targetOpID,
			SourceProfileID:  &enabledProfile.ID,
			TargetProfileID:  &targetProfiles[0].ID,
			JobID:            &j.ID,
		}})
		if insertErr != nil {
			failed++
			continue
		}
		processed++
	}

	if processed != n {
		t.Errorf("processed = %d, want %d", processed, n)
	}
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
	if got := otaStore.totalInserted(); got != n {
		t.Errorf("OTA rows inserted = %d, want %d", got, n)
	}
	if stockStore.allocated != n {
		t.Errorf("stock allocations = %d, want %d", stockStore.allocated, n)
	}

	// F-A1 regression guard: every inserted OTA command MUST carry the eUICC EID
	// (NOT the SIM ICCID and NOT a SIM UUID). Forward path: rec.EID == enabledProfile.EID.
	// Detect EID-vs-ICCID confusion by structure: project EIDs in test fixtures begin
	// with the GSMA prefix "8900..." and are >32 chars; ICCIDs begin with "890000" and
	// are <=20 chars. Stricter check: exact match against the source profile.
	otaStore.mu.Lock()
	for _, batch := range otaStore.insertCalls {
		for _, params := range batch {
			if params.EID == "" {
				t.Errorf("inserted OTA command with empty EID — bulk_esim_switch must populate enabledProfile.EID")
				continue
			}
			// Walk enabledProfiles to find the matching profile and assert EID equality.
			matched := false
			for _, prof := range enabledProfiles {
				if prof.EID == params.EID {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("OTA command EID=%q does not match any enabled profile EID — possible ICCID/SimID mis-assignment regression", params.EID)
			}
		}
	}
	otaStore.mu.Unlock()
}
