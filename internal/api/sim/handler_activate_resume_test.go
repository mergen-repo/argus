package sim

// FIX-253 Task 5 — handler-level regression tests for Activate empty-pool guard,
// audit-on-failure branches, and Resume static-IP skip-allocation.
//
// All tests are DB-gated (DATABASE_URL required). The concrete *store.SIMStore /
// *store.IPPoolStore types used by Handler have no interface substitution point,
// so a live Postgres is required for tests that exercise code past the early-return
// guards. A fakeAuditor (implements audit.Auditor) captures audit entries in-memory
// so assertions don't need to query the audit_log table.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// ── fakeAuditor ──────────────────────────────────────────────────────────────

type capturedAuditEntry struct {
	Action   string
	EntityID string
	After    map[string]interface{}
}

type fakeAuditor struct {
	entries []capturedAuditEntry
}

func (f *fakeAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	e := capturedAuditEntry{Action: p.Action, EntityID: p.EntityID}
	if p.AfterData != nil {
		_ = json.Unmarshal(p.AfterData, &e.After)
	}
	f.entries = append(f.entries, e)
	return &audit.Entry{}, nil
}

func (f *fakeAuditor) lastByAction(action string) (capturedAuditEntry, bool) {
	for i := len(f.entries) - 1; i >= 0; i-- {
		if f.entries[i].Action == action {
			return f.entries[i], true
		}
	}
	return capturedAuditEntry{}, false
}

func (f *fakeAuditor) countByAction(action string) int {
	n := 0
	for _, e := range f.entries {
		if e.Action == action {
			n++
		}
	}
	return n
}

// ── DB pool helper ────────────────────────────────────────────────────────────

func testHandlerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("skip: cannot connect to postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Logf("skip: postgres ping failed: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// ── seed helpers ─────────────────────────────────────────────────────────────

type handlerTestFixture struct {
	tenantID   uuid.UUID
	operatorID uuid.UUID
	apnID      uuid.UUID
	poolID     uuid.UUID
	simID      uuid.UUID
	ipID       uuid.UUID // may be uuid.Nil if no ip seeded
}

// seedHandlerFixture creates tenant→operator→apn→pool→(optional ip)→sim.
// If allocType == "" no ip_pool or ip_address is created (withPool=false).
// simState sets the initial SIM state.
func seedHandlerFixture(t *testing.T, dbPool *pgxpool.Pool, withPool bool, allocType string, simState string) handlerTestFixture {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000

	var fix handlerTestFixture

	// Tenant
	if err := dbPool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('htest-'||gen_random_uuid()::text, 'htest@test.invalid')
		RETURNING id`).Scan(&fix.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Operator — use the fixed mock operator that always has a sims_mock partition.
	fix.operatorID = uuid.MustParse("00000000-0000-0000-0000-000000000100")

	// APN
	if err := dbPool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, $3, 'HTest APN', 'iot', 'active')
		RETURNING id`,
		fix.tenantID, fix.operatorID,
		fmt.Sprintf("htest-%d", nonce),
	).Scan(&fix.apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	if withPool {
		// IP Pool
		if err := dbPool.QueryRow(ctx, `
			INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
			VALUES ($1, $2, 'htest-pool', '10.200.0.0/24'::cidr, 10, 0, 'active')
			RETURNING id`,
			fix.tenantID, fix.apnID,
		).Scan(&fix.poolID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}

		// Seed available IPs into the pool
		for i := 1; i <= 5; i++ {
			if _, err := dbPool.Exec(ctx, `
				INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
				VALUES ($1, $2::inet, $3, 'available')`,
				fix.poolID, fmt.Sprintf("10.200.0.%d", i), allocType,
			); err != nil {
				t.Fatalf("seed ip %d: %v", i, err)
			}
		}
	}

	// SIM
	iccid := fmt.Sprintf("8992530%09d", nonce%1_000_000_000)
	imsi := fmt.Sprintf("28699%010d", nonce%1_000_000_000)
	if len(iccid) > 22 {
		iccid = iccid[:22]
	}
	if len(imsi) > 15 {
		imsi = imsi[:15]
	}

	if err := dbPool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, $5, 'physical', $6)
		RETURNING id`,
		fix.tenantID, fix.operatorID, fix.apnID, iccid, imsi, simState,
	).Scan(&fix.simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = dbPool.Exec(cctx, `DELETE FROM sim_state_history WHERE sim_id = $1`, fix.simID)
		_, _ = dbPool.Exec(cctx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, fix.simID)
		_, _ = dbPool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, fix.simID)
		if fix.poolID != uuid.Nil {
			_, _ = dbPool.Exec(cctx, `DELETE FROM ip_addresses WHERE pool_id = $1`, fix.poolID)
			_, _ = dbPool.Exec(cctx, `DELETE FROM ip_pools WHERE id = $1`, fix.poolID)
		}
		_, _ = dbPool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, fix.apnID)
		_, _ = dbPool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, fix.tenantID)
	})

	return fix
}

// ── request builder ───────────────────────────────────────────────────────────

func makeActivateRequest(tenantID, simID uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+simID.String()+"/activate", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", simID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func makeResumeRequest(tenantID, simID uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+simID.String()+"/resume", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", simID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

func makeSuspendRequest(tenantID, simID uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/"+simID.String()+"/suspend", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", simID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

// ── Test 9: Activate → pool empty → 422 POOL_EXHAUSTED + audit ───────────────

// TestActivate_PoolEmpty_Returns422 verifies that when the APN has no IP pools,
// Activate returns HTTP 422 with POOL_EXHAUSTED code and writes an audit entry
// with action "sim.activate.failed" and reason "no_pool_for_apn".
func TestActivate_PoolEmpty_Returns422(t *testing.T) {
	dbPool := testHandlerPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}

	// Seed SIM with APN that has NO pool
	fix := seedHandlerFixture(t, dbPool, false, "", "ordered")

	auditor := &fakeAuditor{}
	h := NewHandler(
		store.NewSIMStore(dbPool),
		store.NewAPNStore(dbPool),
		store.NewOperatorStore(dbPool),
		store.NewIPPoolStore(dbPool),
		store.NewTenantStore(dbPool),
		auditor,
		zerolog.Nop(),
	)

	req := makeActivateRequest(fix.tenantID, fix.simID)
	w := httptest.NewRecorder()
	h.Activate(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body = %s", w.Code, w.Body.String())
	}

	var resp apierr.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error.Code != apierr.CodePoolExhausted {
		t.Errorf("error.code = %q, want %q", resp.Error.Code, apierr.CodePoolExhausted)
	}

	// Assert audit entry with reason no_pool_for_apn
	entry, ok := auditor.lastByAction("sim.activate.failed")
	if !ok {
		t.Fatalf("no sim.activate.failed audit entry written")
	}
	if reason, _ := entry.After["reason"].(string); reason != "no_pool_for_apn" {
		t.Errorf("audit reason = %q, want %q", reason, "no_pool_for_apn")
	}
	if entry.EntityID != fix.simID.String() {
		t.Errorf("audit entity_id = %q, want %q", entry.EntityID, fix.simID.String())
	}
}

// ── Test 10: Activate audit-on-failure for reachable branches ────────────────

// TestActivate_AuditOnFailure_AllBranches verifies that audit entries with
// action "sim.activate.failed" are written on each failure branch that can be
// triggered reliably via DB.
//
// Branches tested:
//   - validate_apn_missing  (SIM has no APN — nil apn_id)
//   - no_pool_for_apn       (APN has zero pools)
//   - pool_exhausted        (all IPs pre-allocated)
//   - state_transition_failed (SIM in terminated state)
//
// Branches NOT tested (require injecting query errors, infeasible with concrete stores):
//   - get_sim_failed        (needs DB failure mid-GetByID)
//   - list_pools_failed     (needs DB failure mid-List)
//   - allocate_failed       (needs non-ErrPoolExhausted DB failure)
func TestActivate_AuditOnFailure_AllBranches(t *testing.T) {
	dbPool := testHandlerPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	ctx := context.Background()

	// Sub-test: validate_apn_missing — SIM with null apn_id
	t.Run("validate_apn_missing", func(t *testing.T) {
		nonce := uuid.New().ID() % 1_000_000_000
		var tenantID uuid.UUID
		if err := dbPool.QueryRow(ctx, `
			INSERT INTO tenants (name, contact_email)
			VALUES ('htest-noapn-'||gen_random_uuid()::text, 'noapn@test.invalid')
			RETURNING id`).Scan(&tenantID); err != nil {
			t.Fatalf("seed tenant: %v", err)
		}
		// Fixed mock operator that always has a sims_mock partition.
		operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000100")
		iccid := fmt.Sprintf("8990001%09d", nonce%1_000_000_000)
		imsi := fmt.Sprintf("28001%010d", nonce%1_000_000_000)
		if len(iccid) > 22 {
			iccid = iccid[:22]
		}
		if len(imsi) > 15 {
			imsi = imsi[:15]
		}
		var simID uuid.UUID
		// Insert SIM with NULL apn_id
		if err := dbPool.QueryRow(ctx, `
			INSERT INTO sims (tenant_id, operator_id, iccid, imsi, sim_type, state)
			VALUES ($1, $2, $3, $4, 'physical', 'ordered')
			RETURNING id`,
			tenantID, operatorID, iccid, imsi,
		).Scan(&simID); err != nil {
			t.Fatalf("seed sim no-apn: %v", err)
		}
		t.Cleanup(func() {
			cctx := context.Background()
			_, _ = dbPool.Exec(cctx, `DELETE FROM sim_state_history WHERE sim_id = $1`, simID)
			_, _ = dbPool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, simID)
			_, _ = dbPool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
		})

		auditor := &fakeAuditor{}
		h := NewHandler(
			store.NewSIMStore(dbPool), store.NewAPNStore(dbPool), store.NewOperatorStore(dbPool),
			store.NewIPPoolStore(dbPool), store.NewTenantStore(dbPool), auditor, zerolog.Nop(),
		)
		req := makeActivateRequest(tenantID, simID)
		w := httptest.NewRecorder()
		h.Activate(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", w.Code)
		}
		entry, ok := auditor.lastByAction("sim.activate.failed")
		if !ok {
			t.Fatal("no sim.activate.failed audit entry")
		}
		if reason, _ := entry.After["reason"].(string); reason != "validate_apn_missing" {
			t.Errorf("reason = %q, want 'validate_apn_missing'", reason)
		}
	})

	// Sub-test: no_pool_for_apn — SIM with APN but zero pools
	t.Run("no_pool_for_apn", func(t *testing.T) {
		fix := seedHandlerFixture(t, dbPool, false, "", "ordered")
		auditor := &fakeAuditor{}
		h := NewHandler(
			store.NewSIMStore(dbPool), store.NewAPNStore(dbPool), store.NewOperatorStore(dbPool),
			store.NewIPPoolStore(dbPool), store.NewTenantStore(dbPool), auditor, zerolog.Nop(),
		)
		req := makeActivateRequest(fix.tenantID, fix.simID)
		w := httptest.NewRecorder()
		h.Activate(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422", w.Code)
		}
		entry, ok := auditor.lastByAction("sim.activate.failed")
		if !ok {
			t.Fatal("no sim.activate.failed audit entry")
		}
		if reason, _ := entry.After["reason"].(string); reason != "no_pool_for_apn" {
			t.Errorf("reason = %q, want 'no_pool_for_apn'", reason)
		}
	})

	// Sub-test: pool_exhausted — all IPs pre-allocated to another SIM
	t.Run("pool_exhausted", func(t *testing.T) {
		fix := seedHandlerFixture(t, dbPool, true, "dynamic", "ordered")
		// Mark all IPs as allocated
		if _, err := dbPool.Exec(ctx, `UPDATE ip_addresses SET state = 'allocated' WHERE pool_id = $1`, fix.poolID); err != nil {
			t.Fatalf("pre-allocate ips: %v", err)
		}
		// Set used_addresses = total
		if _, err := dbPool.Exec(ctx, `UPDATE ip_pools SET used_addresses = total_addresses WHERE id = $1`, fix.poolID); err != nil {
			t.Fatalf("set pool used: %v", err)
		}

		auditor := &fakeAuditor{}
		h := NewHandler(
			store.NewSIMStore(dbPool), store.NewAPNStore(dbPool), store.NewOperatorStore(dbPool),
			store.NewIPPoolStore(dbPool), store.NewTenantStore(dbPool), auditor, zerolog.Nop(),
		)
		req := makeActivateRequest(fix.tenantID, fix.simID)
		w := httptest.NewRecorder()
		h.Activate(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422; body = %s", w.Code, w.Body.String())
		}
		var resp apierr.ErrorResponse
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp.Error.Code != apierr.CodePoolExhausted {
			t.Errorf("code = %q, want POOL_EXHAUSTED", resp.Error.Code)
		}
		entry, ok := auditor.lastByAction("sim.activate.failed")
		if !ok {
			t.Fatal("no sim.activate.failed audit entry")
		}
		if reason, _ := entry.After["reason"].(string); reason != "pool_exhausted" {
			t.Errorf("reason = %q, want 'pool_exhausted'", reason)
		}
	})

	// Sub-test: state_transition_failed — SIM in 'terminated' state cannot be activated
	t.Run("state_transition_failed", func(t *testing.T) {
		fix := seedHandlerFixture(t, dbPool, true, "dynamic", "terminated")
		auditor := &fakeAuditor{}
		h := NewHandler(
			store.NewSIMStore(dbPool), store.NewAPNStore(dbPool), store.NewOperatorStore(dbPool),
			store.NewIPPoolStore(dbPool), store.NewTenantStore(dbPool), auditor, zerolog.Nop(),
		)
		req := makeActivateRequest(fix.tenantID, fix.simID)
		w := httptest.NewRecorder()
		h.Activate(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422 (InvalidStateTransition)", w.Code)
		}
		entry, ok := auditor.lastByAction("sim.activate.failed")
		if !ok {
			t.Fatal("no sim.activate.failed audit entry")
		}
		if reason, _ := entry.After["reason"].(string); reason != "state_transition_failed" {
			t.Errorf("reason = %q, want 'state_transition_failed'", reason)
		}
	})

	// NOTE: branches get_sim_failed, list_pools_failed, allocate_failed require injecting
	// DB errors into concrete store calls — infeasible without store interface substitution.
	// These branches are covered by code inspection (each branch calls createAuditEntry).
}

// ── Test 11: Resume with static IP skips allocation ──────────────────────────

// TestResume_StaticIPSkipsAllocation verifies that when a SIM has a static IP
// preserved by Suspend (ip_address_id still set, allocation_type='static'),
// Resume does not allocate a new IP and preserves the original ip_address_id.
func TestResume_StaticIPSkipsAllocation(t *testing.T) {
	dbPool := testHandlerPool(t)
	if dbPool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated handler test")
	}
	ctx := context.Background()

	// Seed: tenant → operator → apn → pool → static ip → sim (active)
	nonce := uuid.New().ID() % 1_000_000_000

	var tenantID uuid.UUID
	if err := dbPool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('htest-static-'||gen_random_uuid()::text, 'static@test.invalid')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Fixed mock operator that always has a sims_mock partition.
	operatorID := uuid.MustParse("00000000-0000-0000-0000-000000000100")

	var apnID uuid.UUID
	if err := dbPool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, $3, 'Static Test APN', 'iot', 'active')
		RETURNING id`,
		tenantID, operatorID, fmt.Sprintf("htest-static-%d", nonce),
	).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	var poolID uuid.UUID
	if err := dbPool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'static-pool', '10.201.0.0/24'::cidr, 10, 1, 'active')
		RETURNING id`,
		tenantID, apnID,
	).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	var staticIPID uuid.UUID
	if err := dbPool.QueryRow(ctx, `
		INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
		VALUES ($1, '10.201.0.50'::inet, 'static', 'reserved')
		RETURNING id`, poolID,
	).Scan(&staticIPID); err != nil {
		t.Fatalf("seed static ip: %v", err)
	}

	iccid := fmt.Sprintf("8994530%09d", nonce%1_000_000_000)
	imsi := fmt.Sprintf("286990%09d", (nonce+1)%1_000_000_000)
	if len(iccid) > 22 {
		iccid = iccid[:22]
	}
	if len(imsi) > 15 {
		imsi = imsi[:15]
	}

	var simID uuid.UUID
	if err := dbPool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state, ip_address_id)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'active', $6)
		RETURNING id`,
		tenantID, operatorID, apnID, iccid, imsi, staticIPID,
	).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}
	// Backfill sim_id on static IP
	if _, err := dbPool.Exec(ctx, `UPDATE ip_addresses SET sim_id = $1 WHERE id = $2`, simID, staticIPID); err != nil {
		t.Fatalf("backfill ip sim_id: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = dbPool.Exec(cctx, `DELETE FROM sim_state_history WHERE sim_id = $1`, simID)
		_, _ = dbPool.Exec(cctx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, simID)
		_, _ = dbPool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, simID)
		_, _ = dbPool.Exec(cctx, `DELETE FROM ip_addresses WHERE id = $1`, staticIPID)
		_, _ = dbPool.Exec(cctx, `DELETE FROM ip_pools WHERE id = $1`, poolID)
		_, _ = dbPool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, apnID)
		_, _ = dbPool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	auditor := &fakeAuditor{}
	h := NewHandler(
		store.NewSIMStore(dbPool),
		store.NewAPNStore(dbPool),
		store.NewOperatorStore(dbPool),
		store.NewIPPoolStore(dbPool),
		store.NewTenantStore(dbPool),
		auditor,
		zerolog.Nop(),
	)

	// Step 1: Suspend (T1 preserves static ip — ip_address_id stays set)
	suspendReq := makeSuspendRequest(tenantID, simID)
	wSuspend := httptest.NewRecorder()
	h.Suspend(wSuspend, suspendReq)
	if wSuspend.Code != http.StatusOK {
		t.Fatalf("Suspend status = %d, want 200; body = %s", wSuspend.Code, wSuspend.Body.String())
	}

	// After suspend: static IP's ip_address row should be untouched
	var ipState string
	if err := dbPool.QueryRow(ctx, `SELECT state FROM ip_addresses WHERE id = $1`, staticIPID).Scan(&ipState); err != nil {
		t.Fatalf("read ip_address post-suspend: %v", err)
	}
	if ipState != "reserved" {
		t.Errorf("static IP state post-suspend = %q, want 'reserved'", ipState)
	}

	// After suspend: sims.ip_address_id should still be set (static branch preserves it)
	var simIPIDPostSuspend *uuid.UUID
	if err := dbPool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPIDPostSuspend); err != nil {
		t.Fatalf("read sim ip_address_id post-suspend: %v", err)
	}
	if simIPIDPostSuspend == nil || *simIPIDPostSuspend != staticIPID {
		t.Errorf("sims.ip_address_id post-suspend = %v, want %v (static preserved)", simIPIDPostSuspend, staticIPID)
	}

	// Step 2: Resume — should detect static IP, skip alloc, call store.Resume(nil)
	auditor.entries = nil // reset captured entries
	resumeReq := makeResumeRequest(tenantID, simID)
	wResume := httptest.NewRecorder()
	h.Resume(wResume, resumeReq)
	if wResume.Code != http.StatusOK {
		t.Fatalf("Resume status = %d, want 200; body = %s", wResume.Code, wResume.Body.String())
	}

	// Assert: sim is active
	var simState string
	if err := dbPool.QueryRow(ctx, `SELECT state FROM sims WHERE id = $1`, simID).Scan(&simState); err != nil {
		t.Fatalf("read sim state post-resume: %v", err)
	}
	if simState != "active" {
		t.Errorf("sim state post-resume = %q, want 'active'", simState)
	}

	// Assert: ip_address_id unchanged (still the original static IP)
	var simIPIDPostResume *uuid.UUID
	if err := dbPool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPIDPostResume); err != nil {
		t.Fatalf("read sim ip_address_id post-resume: %v", err)
	}
	if simIPIDPostResume == nil || *simIPIDPostResume != staticIPID {
		t.Errorf("sims.ip_address_id post-resume = %v, want %v (static IP unchanged)", simIPIDPostResume, staticIPID)
	}

	// Assert: no sim.resume.failed audit entry
	if n := auditor.countByAction("sim.resume.failed"); n > 0 {
		t.Errorf("got %d sim.resume.failed audit entries, want 0", n)
	}

	// Assert: sim.resume success audit present
	if _, ok := auditor.lastByAction("sim.resume"); !ok {
		t.Error("no sim.resume success audit entry found")
	}

	// Assert: no new IP was allocated (total ip_addresses count in pool unchanged)
	var ipCount int
	if err := dbPool.QueryRow(ctx, `SELECT COUNT(*) FROM ip_addresses WHERE pool_id = $1`, poolID).Scan(&ipCount); err != nil {
		t.Fatalf("count ips: %v", err)
	}
	if ipCount != 1 {
		t.Errorf("ip_addresses count = %d, want 1 (no new alloc for static)", ipCount)
	}
}
