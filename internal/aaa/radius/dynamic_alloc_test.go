package radius

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// testDBPool opens a pgxpool.Pool from DATABASE_URL. Matches the
// testSMSPool pattern (see internal/store/sms_outbound_test.go:15). Returns
// nil if DATABASE_URL is unset or the ping fails so callers can t.Skip.
func testDBPool(t *testing.T) *pgxpool.Pool {
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

// captureResponseWriter buffers the RADIUS Response packet so tests can
// assert on it without spinning up a UDP listener. Implements
// radius.ResponseWriter.
type captureResponseWriter struct {
	pkt *radius.Packet
}

func (c *captureResponseWriter) Write(p *radius.Packet) error {
	c.pkt = p
	return nil
}

// dynamicAllocFixture seeds an isolated tenant + operator partition + apn +
// pool (5 /32 addresses available) + active SIM with a unique IMSI. Returns
// (tenantID, simID, imsi, poolID). Cleans up all inserted rows on test exit.
func dynamicAllocFixture(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID, string, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('story092-radius-'||gen_random_uuid()::text, 'radius@story092.test')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Reuse an existing operator — we only need one to satisfy apns.operator_id.
	// All STORY-092 fixtures use this pattern (see ippool_test.go:recountTestFixture).
	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator in test DB: %v", err)
	}

	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'story092-radius-'||gen_random_uuid()::text, 'STORY-092 RADIUS', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	var poolID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'STORY-092 RADIUS Pool', '10.251.0.0/24'::cidr, 5, 0, 'active')
		RETURNING id`, tenantID, apnID).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	for i := 1; i <= 5; i++ {
		ipv4 := fmt.Sprintf("10.251.0.%d", i)
		if _, err := pool.Exec(ctx, `
			INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
			VALUES ($1, $2::inet, 'dynamic', 'available')`, poolID, ipv4); err != nil {
			t.Fatalf("seed ip_address %s: %v", ipv4, err)
		}
	}

	// SIM: no policy_version_id so the enforcer path is skipped and the
	// dynamic-alloc helper runs unconditionally. policy_version_id is
	// NULL-able (see migrations/20260401000001_create_policy_core.up.sql).
	var simID uuid.UUID
	// IMSI is varchar(15); use 15-digit value with a per-test nonce to avoid
	// collisions with seed 003 fixtures.
	imsi := fmt.Sprintf("286999%09d", time.Now().UnixNano()%1_000_000_000)
	iccid := fmt.Sprintf("899001%014d", time.Now().UnixNano()%100_000_000_000_000)
	msisdn := fmt.Sprintf("9053%08d", time.Now().UnixNano()%100_000_000)
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type)
		VALUES ($1, $2, $3, $4, $5, $6, 'physical', 'active', 'lte')
		RETURNING id`, tenantID, operatorID, apnID, iccid, imsi, msisdn).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
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

	return tenantID, simID, imsi, poolID
}

// testRADIUSDynamicAllocHappyPath — STORY-092 AC-1.
// Separate function so server_test.go stays compact.
func testRADIUSDynamicAllocHappyPath(t *testing.T) {
	pool := testDBPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	_, simID, imsi, poolID := dynamicAllocFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	ipPoolStore := store.NewIPPoolStore(pool)
	operatorStore := store.NewOperatorStore(pool)
	// nil-redis SIMCache falls through to SIMStore.GetByIMSI (sim_cache.go:36).
	simCache := NewSIMCache(nil, simStore, zerolog.Nop())
	sessionMgr := session.NewManager(nil, nil, zerolog.Nop())

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

	// Build an Access-Request the way handleDirectAuth expects.
	req := radius.New(radius.CodeAccessRequest, []byte("testing123"))
	rfc2865.UserName_SetString(req, imsi)
	rfc2865.NASIPAddress_Set(req, net.ParseIP("127.0.0.1").To4())

	// Wrap in radius.Request — the Context is used by handleDirectAuth.
	rReq := &radius.Request{Packet: req}
	rReq = rReq.WithContext(context.Background())

	cw := &captureResponseWriter{}
	srv.handleDirectAuth(context.Background(), cw, rReq, zerolog.Nop(), time.Now())

	if cw.pkt == nil {
		t.Fatal("no response packet captured — handleDirectAuth did not write")
	}
	if cw.pkt.Code != radius.CodeAccessAccept {
		msg, _ := rfc2865.ReplyMessage_LookupString(cw.pkt)
		t.Fatalf("response code = %d (reply=%q), want AccessAccept(%d)", cw.pkt.Code, msg, radius.CodeAccessAccept)
	}

	ip, err := rfc2865.FramedIPAddress_Lookup(cw.pkt)
	if err != nil {
		t.Fatalf("FramedIPAddress_Lookup: %v", err)
	}
	if ip == nil {
		t.Fatal("FramedIPAddress missing from Access-Accept — dynamic alloc did not attach")
	}
	if ip.To4() == nil {
		t.Fatalf("FramedIPAddress not IPv4: %s", ip)
	}
	// Must be inside 10.251.0.0/24 (the pool CIDR).
	if ip4 := ip.To4(); ip4[0] != 10 || ip4[1] != 251 || ip4[2] != 0 {
		t.Errorf("FramedIPAddress = %s, want in 10.251.0.0/24", ip)
	}

	// Verify DB state: the SIM now has ip_address_id set, the matching
	// ip_addresses row is 'allocated', and ip_pools.used_addresses == 1.
	var simIPID *uuid.UUID
	if err := pool.QueryRow(context.Background(),
		`SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPID); err != nil {
		t.Fatalf("re-read sim: %v", err)
	}
	if simIPID == nil {
		t.Fatal("sims.ip_address_id still NULL after allocation — SetIPAndPolicy never ran")
	}

	var ipState string
	if err := pool.QueryRow(context.Background(),
		`SELECT state FROM ip_addresses WHERE id = $1`, *simIPID).Scan(&ipState); err != nil {
		t.Fatalf("re-read ip_address: %v", err)
	}
	if ipState != "allocated" {
		t.Errorf("ip_addresses.state = %q, want 'allocated'", ipState)
	}

	var used int
	if err := pool.QueryRow(context.Background(),
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("re-read pool: %v", err)
	}
	if used != 1 {
		t.Errorf("ip_pools.used_addresses = %d, want 1", used)
	}

}
