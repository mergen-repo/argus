package diameter

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	aaasession "github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// testDiameterDBPool opens a pgxpool.Pool from DATABASE_URL. Matches the
// radius.testDBPool helper (internal/aaa/radius/dynamic_alloc_test.go:23).
func testDiameterDBPool(t *testing.T) *pgxpool.Pool {
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

// gxAllocFixture seeds tenant + apn + pool (3 available addresses, no
// pre-allocation) + SIM with ip_address_id=NULL. The Gx handleInitial call
// is expected to pick an IP from this pool.
func gxAllocFixture(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID, uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()

	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('story092-gx-alloc-'||gen_random_uuid()::text, 'gxalloc@story092.test')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator: %v", err)
	}

	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'story092-gx-'||gen_random_uuid()::text, 'STORY-092 Gx Alloc', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	var poolID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'STORY-092 Gx Pool', '10.253.0.0/24'::cidr, 3, 0, 'active')
		RETURNING id`, tenantID, apnID).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	for i := 1; i <= 3; i++ {
		ipv4 := fmt.Sprintf("10.253.0.%d", i)
		if _, err := pool.Exec(ctx, `
			INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
			VALUES ($1, $2::inet, 'dynamic', 'available')`, poolID, ipv4); err != nil {
			t.Fatalf("seed ip %s: %v", ipv4, err)
		}
	}

	iccid := fmt.Sprintf("899010%014d", time.Now().UnixNano()%100_000_000_000_000)
	imsi := fmt.Sprintf("286997%09d", time.Now().UnixNano()%1_000_000_000)
	msisdn := fmt.Sprintf("9055%08d", time.Now().UnixNano()%100_000_000)

	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type)
		VALUES ($1, $2, $3, $4, $5, $6, 'physical', 'active', 'lte')
		RETURNING id`, tenantID, operatorID, apnID, iccid, imsi, msisdn).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, simID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_addresses WHERE pool_id = $1`, poolID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_pools WHERE id = $1`, poolID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, simID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	return tenantID, simID, poolID, imsi
}

// gxReleaseFixture seeds tenant + apn + pool + SIM with a pre-allocated IP
// of the given allocationType ('dynamic' or 'static'). Used by the CCR-T
// release assertions.
func gxReleaseFixture(t *testing.T, pool *pgxpool.Pool, allocationType string) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()

	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('story092-gx-rel-'||gen_random_uuid()::text, 'gxrel@story092.test')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator: %v", err)
	}

	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'story092-gx-rel-'||gen_random_uuid()::text, 'STORY-092 Gx Rel', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	var poolID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'STORY-092 Gx Rel Pool', '10.254.0.0/24'::cidr, 3, 1, 'active')
		RETURNING id`, tenantID, apnID).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	iccid := fmt.Sprintf("899011%014d", time.Now().UnixNano()%100_000_000_000_000)
	imsi := fmt.Sprintf("286996%09d", time.Now().UnixNano()%1_000_000_000)
	msisdn := fmt.Sprintf("9056%08d", time.Now().UnixNano()%100_000_000)

	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type)
		VALUES ($1, $2, $3, $4, $5, $6, 'physical', 'active', 'lte')
		RETURNING id`, tenantID, operatorID, apnID, iccid, imsi, msisdn).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	var ipID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state, sim_id, allocated_at)
		VALUES ($1, '10.254.0.11'::inet, $2, 'allocated', $3, NOW())
		RETURNING id`, poolID, allocationType, simID).Scan(&ipID); err != nil {
		t.Fatalf("seed ip: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE sims SET ip_address_id = $1 WHERE id = $2`, ipID, simID); err != nil {
		t.Fatalf("set sim.ip_address_id: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, simID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_addresses WHERE pool_id = $1`, poolID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_pools WHERE id = $1`, poolID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, simID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	return tenantID, simID, poolID, ipID, imsi
}

// simStoreResolver adapts *store.SIMStore to the diameter.SIMResolver
// interface without depending on radius.SIMCache (which would introduce an
// import cycle). Matches the resolver contract at sim_resolver.go:9.
type simStoreResolver struct {
	s *store.SIMStore
}

func (r *simStoreResolver) GetByIMSI(ctx context.Context, imsi string) (*store.SIM, error) {
	return r.s.GetByIMSI(ctx, imsi)
}

// newGxTestHandler wires a GxHandler with a real DB-backed ipPoolStore and
// simStore, a nil session.Manager (handleInitial is safe with nil sessionMgr
// — it just skips the Create/Terminate calls), and a stateMap. Sufficient
// to exercise the IP-allocation and Framed-IP AVP paths in isolation.
func newGxTestHandler(t *testing.T, pool *pgxpool.Pool) (*GxHandler, *aaasession.Manager) {
	t.Helper()
	simStore := store.NewSIMStore(pool)
	ipPoolStore := store.NewIPPoolStore(pool)
	resolver := &simStoreResolver{s: simStore}
	stateMap := NewSessionStateMap()
	radiusSessionStore := store.NewRadiusSessionStore(pool)
	sessionMgr := aaasession.NewManager(radiusSessionStore, nil, zerolog.Nop(), aaasession.WithSIMStore(simStore))
	h := NewGxHandler(sessionMgr, nil, resolver, ipPoolStore, simStore, stateMap, zerolog.Nop())
	return h, sessionMgr
}

// buildGxCCRInitial builds a minimal Gx CCR-I message for a given imsi.
func buildGxCCRInitial(sessionID, imsi string) *Message {
	ccr := NewRequest(CommandCCR, ApplicationIDGx, 10, 10)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 0))
	ccr.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	for _, sub := range BuildSubscriptionID(imsi, "") {
		ccr.AddAVP(sub)
	}
	return ccr
}

// buildGxCCRTermination builds a minimal CCR-T for a given session.
func buildGxCCRTermination(sessionID, imsi string) *Message {
	ccr := NewRequest(CommandCCR, ApplicationIDGx, 12, 12)
	ccr.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeTermination))
	ccr.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, 2))
	ccr.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	for _, sub := range BuildSubscriptionID(imsi, "") {
		ccr.AddAVP(sub)
	}
	return ccr
}

// TestGxCCAInitial_FramedIPAddress — STORY-092 Wave 2 AC-4.
//
// CCR-I for a SIM with no ip_address_id must:
//   - allocate an IP from the pool (ip_addresses row moves to 'allocated'),
//   - set sims.ip_address_id,
//   - emit a Framed-IP-Address AVP (code 8) on the CCA-I with vendor=0,
//     M flag set, 4-byte IPv4 payload matching the allocated address.
func TestGxCCAInitial_FramedIPAddress(t *testing.T) {
	pool := testDiameterDBPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	_, simID, poolID, imsi := gxAllocFixture(t, pool)
	h, _ := newGxTestHandler(t, pool)

	sessionID := fmt.Sprintf("gx-alloc-%d", time.Now().UnixNano())
	ccr := buildGxCCRInitial(sessionID, imsi)
	cca := h.HandleCCR(ccr)

	if cca == nil {
		t.Fatal("HandleCCR returned nil")
	}
	if cca.GetResultCode() != ResultCodeSuccess {
		t.Fatalf("result code = %d, want Success(%d)", cca.GetResultCode(), ResultCodeSuccess)
	}

	// Locate the Framed-IP-Address AVP on the CCA-I.
	ipAVP := FindAVP(cca.AVPs, AVPCodeFramedIPAddress)
	if ipAVP == nil {
		t.Fatal("CCA-I missing Framed-IP-Address AVP (code 8)")
	}
	if ipAVP.VendorID != 0 {
		t.Errorf("Framed-IP vendor = %d, want 0 (RFC 7155 base, not 3GPP vendor)", ipAVP.VendorID)
	}
	if ipAVP.Flags&AVPFlagMandatory == 0 {
		t.Errorf("Framed-IP M flag not set, flags = %#x", ipAVP.Flags)
	}
	if ipAVP.Flags&AVPFlagVendor != 0 {
		t.Errorf("Framed-IP V flag set, flags = %#x (must be 0 for vendor=0)", ipAVP.Flags)
	}
	// Address AVP format: 2-byte address-family (1 = IPv4) + 4-byte payload.
	if len(ipAVP.Data) != 6 {
		t.Fatalf("Framed-IP AVP data len = %d, want 6 (2-byte family + 4-byte v4)", len(ipAVP.Data))
	}
	// Family bytes: 0x00 0x01.
	if ipAVP.Data[0] != 0x00 || ipAVP.Data[1] != 0x01 {
		t.Errorf("Framed-IP address-family bytes = %#x %#x, want 0x00 0x01", ipAVP.Data[0], ipAVP.Data[1])
	}
	ip4 := [4]byte{ipAVP.Data[2], ipAVP.Data[3], ipAVP.Data[4], ipAVP.Data[5]}
	if ip4[0] != 10 || ip4[1] != 253 || ip4[2] != 0 {
		t.Errorf("Framed-IP = %d.%d.%d.%d, want 10.253.0.x", ip4[0], ip4[1], ip4[2], ip4[3])
	}

	// DB state: ip_addresses.state='allocated' for the row matching ip4;
	// sims.ip_address_id populated; pool.used_addresses incremented.
	expectedAddr := fmt.Sprintf("%d.%d.%d.%d", ip4[0], ip4[1], ip4[2], ip4[3])
	var ipState, allocType string
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `
		SELECT state, allocation_type FROM ip_addresses WHERE pool_id = $1 AND address_v4 = $2::inet`,
		poolID, expectedAddr).Scan(&ipState, &allocType); err != nil {
		t.Fatalf("re-read ip_address for %s: %v", expectedAddr, err)
	}
	if ipState != "allocated" {
		t.Errorf("ip_addresses.state = %q, want 'allocated'", ipState)
	}
	if allocType != "dynamic" {
		t.Errorf("ip_addresses.allocation_type = %q, want 'dynamic'", allocType)
	}

	var simIPID *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPID); err != nil {
		t.Fatalf("re-read sim: %v", err)
	}
	if simIPID == nil {
		t.Fatal("sims.ip_address_id still NULL after CCA-I allocation")
	}

	var used int
	if err := pool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("re-read pool: %v", err)
	}
	if used != 1 {
		t.Errorf("ip_pools.used_addresses = %d, want 1", used)
	}
}

// TestGxCCRTermination_ReleasesDynamic — STORY-092 Wave 2 AC-5.
//
// CCR-T for a SIM with a 'dynamic' pre-allocated IP must release it:
// ip_addresses.state returns to 'available', sims.ip_address_id becomes
// NULL, ip_pools.used_addresses decrements. Static-allocated IPs are
// preserved (the inverse assertion is in the _PreservesStatic subtest).
func TestGxCCRTermination_ReleasesDynamic(t *testing.T) {
	t.Run("dynamic_released", func(t *testing.T) {
		pool := testDiameterDBPool(t)
		if pool == nil {
			t.Skip("no test database available (set DATABASE_URL)")
		}

		_, simID, poolID, ipID, imsi := gxReleaseFixture(t, pool, "dynamic")
		h, sessionMgr := newGxTestHandler(t, pool)
		ctx := context.Background()

		// Seed a radius_sessions row so handleTermination's
		// GetByAcctSessionID succeeds and Terminate has something to mark
		// 'ended'. We need operator_id from FK target.
		var operatorID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
			t.Fatalf("read operator: %v", err)
		}
		sessionID := fmt.Sprintf("gx-rel-%d", time.Now().UnixNano())
		// Use tenant/simID from fixture.
		var tenantID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT tenant_id FROM sims WHERE id = $1`, simID).Scan(&tenantID); err != nil {
			t.Fatalf("read sim.tenant_id: %v", err)
		}
		sess := &aaasession.Session{
			ID:            uuid.New().String(),
			SimID:         simID.String(),
			TenantID:      tenantID.String(),
			OperatorID:    operatorID.String(),
			IMSI:          imsi,
			AcctSessionID: sessionID,
			FramedIP:      "10.254.0.11",
			SessionState:  "active",
			StartedAt:     time.Now().UTC(),
			LastInterimAt: time.Now().UTC(),
			AuthMethod:    "diameter_gx",
		}
		if err := sessionMgr.Create(ctx, sess); err != nil {
			t.Fatalf("seed session: %v", err)
		}

		ccr := buildGxCCRTermination(sessionID, imsi)
		cca := h.HandleCCR(ccr)
		if cca == nil {
			t.Fatal("HandleCCR(CCR-T) returned nil")
		}
		if cca.GetResultCode() != ResultCodeSuccess {
			t.Fatalf("result code = %d, want Success", cca.GetResultCode())
		}

		// Assertions.
		var ipState string
		if err := pool.QueryRow(ctx, `SELECT state FROM ip_addresses WHERE id = $1`, ipID).Scan(&ipState); err != nil {
			t.Fatalf("re-read ip_address: %v", err)
		}
		if ipState != "available" {
			t.Errorf("ip_addresses.state = %q, want 'available'", ipState)
		}

		var used int
		if err := pool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
			t.Fatalf("re-read pool: %v", err)
		}
		if used != 0 {
			t.Errorf("ip_pools.used_addresses = %d, want 0", used)
		}

		var simIPID *uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPID); err != nil {
			t.Fatalf("re-read sim: %v", err)
		}
		if simIPID != nil {
			t.Errorf("sims.ip_address_id = %v, want NULL", simIPID)
		}
	})

	t.Run("static_preserved", func(t *testing.T) {
		pool := testDiameterDBPool(t)
		if pool == nil {
			t.Skip("no test database available (set DATABASE_URL)")
		}

		_, simID, poolID, ipID, imsi := gxReleaseFixture(t, pool, "static")
		h, sessionMgr := newGxTestHandler(t, pool)
		ctx := context.Background()

		var operatorID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
			t.Fatalf("read operator: %v", err)
		}
		sessionID := fmt.Sprintf("gx-rel-static-%d", time.Now().UnixNano())
		var tenantID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT tenant_id FROM sims WHERE id = $1`, simID).Scan(&tenantID); err != nil {
			t.Fatalf("read sim.tenant_id: %v", err)
		}
		sess := &aaasession.Session{
			ID:            uuid.New().String(),
			SimID:         simID.String(),
			TenantID:      tenantID.String(),
			OperatorID:    operatorID.String(),
			IMSI:          imsi,
			AcctSessionID: sessionID,
			FramedIP:      "10.254.0.11",
			SessionState:  "active",
			StartedAt:     time.Now().UTC(),
			LastInterimAt: time.Now().UTC(),
			AuthMethod:    "diameter_gx",
		}
		if err := sessionMgr.Create(ctx, sess); err != nil {
			t.Fatalf("seed session: %v", err)
		}

		ccr := buildGxCCRTermination(sessionID, imsi)
		cca := h.HandleCCR(ccr)
		if cca == nil {
			t.Fatal("HandleCCR(CCR-T) returned nil")
		}
		if cca.GetResultCode() != ResultCodeSuccess {
			t.Fatalf("result code = %d, want Success", cca.GetResultCode())
		}

		var ipState string
		if err := pool.QueryRow(ctx, `SELECT state FROM ip_addresses WHERE id = $1`, ipID).Scan(&ipState); err != nil {
			t.Fatalf("re-read ip_address: %v", err)
		}
		if ipState != "allocated" {
			t.Errorf("ip_addresses.state for static after CCR-T = %q, want 'allocated' (must not auto-release)", ipState)
		}

		var used int
		if err := pool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
			t.Fatalf("re-read pool: %v", err)
		}
		if used != 1 {
			t.Errorf("ip_pools.used_addresses for static = %d, want 1 (unchanged)", used)
		}

		var simIPID *uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPID); err != nil {
			t.Fatalf("re-read sim: %v", err)
		}
		if simIPID == nil || *simIPID != ipID {
			t.Errorf("sims.ip_address_id for static = %v, want %v (unchanged)", simIPID, ipID)
		}
	})
}
