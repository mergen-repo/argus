package radius

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

// acctReleaseFixture seeds tenant + apn + pool + ip_addresses + SIM with a
// pre-allocated IP. Returns (tenantID, simID, imsi, poolID, ipID).
// allocationType is 'dynamic' or 'static'.
func acctReleaseFixture(t *testing.T, pool *pgxpool.Pool, allocationType string) (uuid.UUID, uuid.UUID, string, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('story092-acctrel-'||gen_random_uuid()::text, 'acctrel@story092.test')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator in test DB: %v", err)
	}

	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'story092-acctrel-'||gen_random_uuid()::text, 'STORY-092 Acct Release', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	// used_addresses starts at 1 because the IP we seed below is pre-allocated.
	var poolID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'STORY-092 Acct Release Pool', '10.252.0.0/24'::cidr, 5, 1, 'active')
		RETURNING id`, tenantID, apnID).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	iccid := fmt.Sprintf("899002%014d", time.Now().UnixNano()%100_000_000_000_000)
	imsi := fmt.Sprintf("286998%09d", time.Now().UnixNano()%1_000_000_000)
	msisdn := fmt.Sprintf("9054%08d", time.Now().UnixNano()%100_000_000)

	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type)
		VALUES ($1, $2, $3, $4, $5, $6, 'physical', 'active', 'lte')
		RETURNING id`, tenantID, operatorID, apnID, iccid, imsi, msisdn).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	// Seed a pre-allocated IP bound to this SIM.
	var ipID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state, sim_id, allocated_at)
		VALUES ($1, '10.252.0.7'::inet, $2, 'allocated', $3, NOW())
		RETURNING id`, poolID, allocationType, simID).Scan(&ipID); err != nil {
		t.Fatalf("seed ip: %v", err)
	}

	// Seed a second IP that is 'reserved' and not bound (so pool arithmetic is unambiguous).
	// Required so used_addresses=1 means exactly this one row is occupying the slot.
	if _, err := pool.Exec(ctx, `
		INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
		VALUES ($1, '10.252.0.8'::inet, 'dynamic', 'available')`, poolID); err != nil {
		t.Fatalf("seed spare ip: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE sims SET ip_address_id = $1 WHERE id = $2`, ipID, simID); err != nil {
		t.Fatalf("set sim.ip_address_id: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, simID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM ip_addresses WHERE pool_id = $1`, poolID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM ip_pools WHERE id = $1`, poolID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM sims WHERE id = $1`, simID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM apns WHERE id = $1`, apnID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	return tenantID, simID, imsi, poolID, ipID
}

// buildAcctStopPacket constructs a RADIUS Accounting-Request with
// Acct-Status-Type=Stop, a session ID, and the IMSI set as User-Name.
func buildAcctStopPacket(t *testing.T, secret, imsi, acctSessionID string, bytesIn, bytesOut uint32) *radius.Packet {
	t.Helper()
	pkt := radius.New(radius.CodeAccountingRequest, []byte(secret))
	rfc2866.AcctStatusType_Set(pkt, rfc2866.AcctStatusType_Value_Stop)
	rfc2866.AcctSessionID_SetString(pkt, acctSessionID)
	rfc2866.AcctInputOctets_Set(pkt, rfc2866.AcctInputOctets(bytesIn))
	rfc2866.AcctOutputOctets_Set(pkt, rfc2866.AcctOutputOctets(bytesOut))
	rfc2866.AcctTerminateCause_Set(pkt, rfc2866.AcctTerminateCause_Value_UserRequest)
	rfc2865.UserName_SetString(pkt, imsi)
	return pkt
}

// newAcctTestServer wires a RADIUS server with a real SIMCache/sessionMgr/
// simStore bound to a real DB pool. The session manager uses in-memory store
// (nil radiusSessionStore) so Create/Terminate work without a sessions table.
func newAcctTestServer(t *testing.T, pool *pgxpool.Pool) *Server {
	t.Helper()
	simStore := store.NewSIMStore(pool)
	ipPoolStore := store.NewIPPoolStore(pool)
	operatorStore := store.NewOperatorStore(pool)
	simCache := NewSIMCache(nil, simStore, zerolog.Nop())
	radiusSessionStore := store.NewRadiusSessionStore(pool)
	sessionMgr := session.NewManager(radiusSessionStore, nil, zerolog.Nop(), session.WithSIMStore(simStore))

	srv := NewServer(
		ServerConfig{
			AuthAddr:       ":0",
			AcctAddr:       ":0",
			DefaultSecret:  "testing123",
			WorkerPoolSize: 4,
		},
		simCache,
		sessionMgr,
		operatorStore,
		ipPoolStore,
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)
	srv.SetSIMStore(simStore)
	return srv
}

// TestRADIUSAccountingStop_ReleasesDynamic — STORY-092 Wave 2 AC-3.
//
// Dynamic allocation: Acct-Stop must call ReleaseIP → ip_addresses.state
// returns to 'available', ip_pools.used_addresses decrements, sims.ip_address_id
// becomes NULL.
func TestRADIUSAccountingStop_ReleasesDynamic(t *testing.T) {
	pool := testDBPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	tenantID, simID, imsi, poolID, ipID := acctReleaseFixture(t, pool, "dynamic")
	srv := newAcctTestServer(t, pool)
	ctx := context.Background()

	// Fetch real operator_id so the radius_sessions FK (operator_id → operators)
	// and tenant_id → tenants is satisfied.
	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("read operator: %v", err)
	}

	// Seed a radius_sessions row so handleAcctStop can look up the session by
	// acctSessionID. Using sessionMgr directly is sufficient here.
	acctSessionID := fmt.Sprintf("acct-rel-%d", time.Now().UnixNano())
	sess := &session.Session{
		ID:            uuid.New().String(),
		SimID:         simID.String(),
		TenantID:      tenantID.String(),
		OperatorID:    operatorID.String(),
		IMSI:          imsi,
		AcctSessionID: acctSessionID,
		FramedIP:      "10.252.0.7",
		SessionState:  "active",
		StartedAt:     time.Now().UTC(),
		LastInterimAt: time.Now().UTC(),
	}
	if err := srv.sessionMgr.Create(ctx, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// Trigger handleAcctStop via the real handleAcct entry point (exercises
	// the Acct-Status-Type dispatch in addition to the release logic).
	req := &radius.Request{
		Packet:     buildAcctStopPacket(t, "testing123", imsi, acctSessionID, 1024, 2048),
		RemoteAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1813},
	}
	req = req.WithContext(ctx)
	cw := &captureResponseWriter{}
	srv.handleAcct(cw, req)

	if cw.pkt == nil || cw.pkt.Code != radius.CodeAccountingResponse {
		t.Fatalf("expected Accounting-Response, got %v", cw.pkt)
	}

	// Assert DB state.
	var ipState string
	if err := pool.QueryRow(ctx, `SELECT state FROM ip_addresses WHERE id = $1`, ipID).Scan(&ipState); err != nil {
		t.Fatalf("re-read ip_address: %v", err)
	}
	if ipState != "available" {
		t.Errorf("ip_addresses.state after dynamic release = %q, want 'available'", ipState)
	}

	var used int
	if err := pool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("re-read pool: %v", err)
	}
	if used != 0 {
		t.Errorf("ip_pools.used_addresses after dynamic release = %d, want 0 (started at 1, one release)", used)
	}

	var simIPID *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPID); err != nil {
		t.Fatalf("re-read sim: %v", err)
	}
	if simIPID != nil {
		t.Errorf("sims.ip_address_id should be NULL after dynamic release, got %v", simIPID)
	}
}

// TestRADIUSAccountingStop_PreservesStatic — STORY-092 Wave 2 AC-3 inverse.
//
// Static allocation: Acct-Stop must NOT release — ip_addresses row stays
// allocated to the SIM (transitioned to 'reclaiming' only on explicit
// ReleaseIP with allocation_type='static', and Acct-Stop is required to
// skip that call entirely).
func TestRADIUSAccountingStop_PreservesStatic(t *testing.T) {
	pool := testDBPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	tenantID, simID, imsi, poolID, ipID := acctReleaseFixture(t, pool, "static")
	srv := newAcctTestServer(t, pool)
	ctx := context.Background()

	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("read operator: %v", err)
	}

	acctSessionID := fmt.Sprintf("acct-rel-static-%d", time.Now().UnixNano())
	sess := &session.Session{
		ID:            uuid.New().String(),
		SimID:         simID.String(),
		TenantID:      tenantID.String(),
		OperatorID:    operatorID.String(),
		IMSI:          imsi,
		AcctSessionID: acctSessionID,
		FramedIP:      "10.252.0.7",
		SessionState:  "active",
		StartedAt:     time.Now().UTC(),
		LastInterimAt: time.Now().UTC(),
	}
	if err := srv.sessionMgr.Create(ctx, sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	req := &radius.Request{
		Packet:     buildAcctStopPacket(t, "testing123", imsi, acctSessionID, 512, 512),
		RemoteAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1813},
	}
	req = req.WithContext(ctx)
	cw := &captureResponseWriter{}
	srv.handleAcct(cw, req)

	if cw.pkt == nil || cw.pkt.Code != radius.CodeAccountingResponse {
		t.Fatalf("expected Accounting-Response, got %v", cw.pkt)
	}

	var ipState string
	if err := pool.QueryRow(ctx, `SELECT state FROM ip_addresses WHERE id = $1`, ipID).Scan(&ipState); err != nil {
		t.Fatalf("re-read ip_address: %v", err)
	}
	if ipState != "allocated" {
		t.Errorf("ip_addresses.state for static after Acct-Stop = %q, want 'allocated' (static must not auto-release)", ipState)
	}

	var used int
	if err := pool.QueryRow(ctx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("re-read pool: %v", err)
	}
	if used != 1 {
		t.Errorf("ip_pools.used_addresses for static after Acct-Stop = %d, want 1 (unchanged)", used)
	}

	var simIPID *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPID); err != nil {
		t.Fatalf("re-read sim: %v", err)
	}
	if simIPID == nil || *simIPID != ipID {
		t.Errorf("sims.ip_address_id for static after Acct-Stop = %v, want %v (unchanged)", simIPID, ipID)
	}
}

// TestRADIUSAccountingStart_FallbackFramedIP — STORY-092 Wave 2 AC-2.
//
// When the NAS omits Framed-IP-Address (simulator-style), handleAcctStart
// must fall back to the SIM's pre-allocated IP so radius_sessions.framed_ip
// is populated.
func TestRADIUSAccountingStart_FallbackFramedIP(t *testing.T) {
	pool := testDBPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	_, simID, imsi, _, _ := acctReleaseFixture(t, pool, "dynamic")
	srv := newAcctTestServer(t, pool)
	ctx := context.Background()

	// Build Acct-Start WITHOUT Framed-IP-Address.
	acctSessionID := fmt.Sprintf("acct-start-fallback-%d", time.Now().UnixNano())
	pkt := radius.New(radius.CodeAccountingRequest, []byte("testing123"))
	rfc2866.AcctStatusType_Set(pkt, rfc2866.AcctStatusType_Value_Start)
	rfc2866.AcctSessionID_SetString(pkt, acctSessionID)
	rfc2865.UserName_SetString(pkt, imsi)
	rfc2865.NASIPAddress_Set(pkt, net.ParseIP("127.0.0.1").To4())
	// Explicitly do NOT set rfc2865.FramedIPAddress_Set — exercise the fallback.

	req := &radius.Request{
		Packet:     pkt,
		RemoteAddr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1813},
	}
	req = req.WithContext(ctx)
	cw := &captureResponseWriter{}
	srv.handleAcct(cw, req)

	if cw.pkt == nil || cw.pkt.Code != radius.CodeAccountingResponse {
		t.Fatalf("expected Accounting-Response, got %v", cw.pkt)
	}

	// Verify the session was created with the fallback framed IP. The session
	// manager stores it by acct_session_id.
	sess, err := srv.sessionMgr.GetByAcctSessionID(ctx, acctSessionID)
	if err != nil {
		t.Fatalf("GetByAcctSessionID: %v", err)
	}
	if sess == nil {
		t.Fatal("no session stored for acct session id")
	}
	// Postgres INET column SELECT ::text returns "10.252.0.7/32". Accept either
	// the bare form (pre-persistence) or the /32 form (post-persistence).
	if sess.FramedIP != "10.252.0.7" && sess.FramedIP != "10.252.0.7/32" {
		t.Errorf("session.FramedIP = %q, want '10.252.0.7' or '10.252.0.7/32' (fallback from sim.ip_address_id)", sess.FramedIP)
	}

	// Defensive: make sure simID wasn't mangled along the way.
	if sess.SimID != simID.String() {
		t.Errorf("session.SimID = %q, want %q", sess.SimID, simID.String())
	}
}
