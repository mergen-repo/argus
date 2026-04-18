package radius

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/policy/enforcer"
	"github.com/btopcu/argus/internal/store"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// nilCacheTestDBName is the name of the disposable PostgreSQL database
// created for this test. Matches the STORY-087 migration_freshvol_test.go
// pattern but with a distinct name so the two tests can coexist.
const nilCacheTestDBName = "argus_story092_nilcache_test"

// TestEnforcerNilCacheIntegration_STORY092 — STORY-092 Wave 1 AC-9 +
// closure of D-038.
//
// Exercises the FULL happy path of the RADIUS Access-Accept dynamic IP
// allocation with a nil policy cache. The enforcer is constructed with
// literal `nil` in positions 1 (policyCache) and 5 (redis), EXACTLY
// matching the production main.go call site — so this test catches any
// regression that reintroduces a nil-pointer dereference via the
// cache-miss DB fall-through path (see enforcer.go:84 "if e.policyCache
// != nil").
//
// Skips when DATABASE_URL is unset (CI gates it to the DB-enabled matrix).
// Builds a disposable DB via the STORY-087 freshvol pattern so the test
// never contaminates shared state.
//
// Assertions:
//   - Response code == Access-Accept
//   - Framed-IP-Address attached (IPv4, inside the pool CIDR)
//   - ip_addresses.state == 'allocated' for the SIM's new IP
//   - ip_pools.used_addresses == 1 (was 0)
//   - Enforcer Allow = true via DB fall-through (no panic on nil cache)
func TestEnforcerNilCacheIntegration_STORY092(t *testing.T) {
	adminDSN := os.Getenv("DATABASE_URL")
	if adminDSN == "" {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	ctx := context.Background()

	// Build an admin DSN pointing at the maintenance "postgres" DB so we can
	// CREATE / DROP the disposable one without being connected to it.
	adminConnDSN, err := swapNilCacheDBName(adminDSN, "postgres")
	if err != nil {
		t.Fatalf("build admin DSN: %v", err)
	}

	adminConn, err := pgx.Connect(ctx, adminConnDSN)
	if err != nil {
		t.Skipf("skip: cannot connect to admin DB: %v", err)
	}
	_, _ = adminConn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", nilCacheTestDBName))
	if _, err := adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", nilCacheTestDBName)); err != nil {
		adminConn.Close(ctx)
		t.Fatalf("CREATE DATABASE: %v", err)
	}
	adminConn.Close(ctx)

	testDSN, err := swapNilCacheDBName(adminDSN, nilCacheTestDBName)
	if err != nil {
		t.Fatalf("build test DSN: %v", err)
	}

	migrDir := nilCacheMigrationsDir(t)
	m, err := migrate.New("file://"+migrDir, testDSN)
	if err != nil {
		dropNilCacheDB(t, adminConnDSN)
		t.Fatalf("migrate.New: %v", err)
	}
	t.Cleanup(func() {
		m.Close()
		dropNilCacheDB(t, adminConnDSN)
	})

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("m.Up(): %v", err)
	}

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// ────────────────────────────────────────────────────────────────
	// Seed a minimal fixture: tenant → operator → apn → pool (5 IPs) →
	// policy + version (unconditional Allow) → SIM pointing at the
	// active version.
	// ────────────────────────────────────────────────────────────────

	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('story092-nilcache', 'nilcache@story092.test')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	var operatorID uuid.UUID
	// STORY-090 Wave 2 D2-B: adapter_type column dropped — the
	// nested adapter_config JSONB carries the per-protocol flags.
	if err := pool.QueryRow(ctx, `
		INSERT INTO operators (code, name, mcc, mnc, adapter_config, state)
		VALUES ('STY092', 'STORY-092 Operator', '286', '99', '{"radius":{"enabled":true}}'::jsonb, 'active')
		RETURNING id`).Scan(&operatorID); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'story092-nilcache-apn', 'STORY-092 Nil-Cache APN', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	var poolID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'STORY-092 Nil-Cache Pool', '10.252.0.0/24'::cidr, 5, 0, 'active')
		RETURNING id`, tenantID, apnID).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	for i := 1; i <= 5; i++ {
		ipv4 := "10.252.0." + strconv.Itoa(i)
		if _, err := pool.Exec(ctx, `
			INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
			VALUES ($1, $2::inet, 'dynamic', 'available')`, poolID, ipv4); err != nil {
			t.Fatalf("seed ip_address %s: %v", ipv4, err)
		}
	}

	// Policy + active version with unconditionally-Allow compiled_rules.
	compiled := json.RawMessage(`{"rules":[{"when":{},"then":{"action":"allow"}}]}`)

	var policyID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'STORY-092 Nil-Cache Policy', 'tenant', 'active')
		RETURNING id`, tenantID).Scan(&policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	var versionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow', $2::jsonb, 'active')
		RETURNING id`, policyID, string(compiled)).Scan(&versionID); err != nil {
		t.Fatalf("seed policy_version: %v", err)
	}

	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id)
		VALUES ($1, $2, $3, '89900100000000092001', '286999000092001', '9053000092001', 'physical', 'active', 'lte', $4)
		RETURNING id`, tenantID, operatorID, apnID, versionID).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	// ────────────────────────────────────────────────────────────────
	// Build the RADIUS server exactly like main.go does for an
	// enforcer-enabled deploy, but with nil cache + nil redis.
	// ────────────────────────────────────────────────────────────────

	simStore := store.NewSIMStore(pool)
	ipPoolStore := store.NewIPPoolStore(pool)
	operatorStore := store.NewOperatorStore(pool)
	policyStore := store.NewPolicyStore(pool)
	violationStore := store.NewPolicyViolationStore(pool, zerolog.Nop())

	// Nil cache + nil redis — THIS IS THE TEST. Matches main.go's literal
	// nil at the enforcer.New call site.
	pe := enforcer.New(nil, policyStore, violationStore, nil, nil, zerolog.Nop())

	// nil-redis SIMCache → SIMStore.GetByIMSI fall-through (sim_cache.go:36).
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
	srv.SetPolicyEnforcer(pe)
	srv.SetSIMStore(simStore)

	// ────────────────────────────────────────────────────────────────
	// Exercise the full path via handleDirectAuth. An in-process
	// ResponseWriter captures the response packet without UDP.
	// ────────────────────────────────────────────────────────────────

	req := radius.New(radius.CodeAccessRequest, []byte("testing123"))
	rfc2865.UserName_SetString(req, "286999000092001")
	rfc2865.NASIPAddress_Set(req, net.ParseIP("127.0.0.1").To4())

	rReq := &radius.Request{Packet: req}
	rReq = rReq.WithContext(ctx)

	cw := &captureResponseWriter{}
	srv.handleDirectAuth(ctx, cw, rReq, zerolog.Nop(), time.Now())

	if cw.pkt == nil {
		t.Fatal("no response packet — handleDirectAuth did not write")
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
		t.Fatal("FramedIPAddress missing from Access-Accept")
	}
	if ip4 := ip.To4(); ip4 == nil || ip4[0] != 10 || ip4[1] != 252 || ip4[2] != 0 {
		t.Errorf("FramedIPAddress = %s, want in 10.252.0.0/24", ip)
	}

	var simIPID *uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPID); err != nil {
		t.Fatalf("re-read sim: %v", err)
	}
	if simIPID == nil {
		t.Fatal("sims.ip_address_id NULL after allocation")
	}

	var ipState string
	if err := pool.QueryRow(ctx,
		`SELECT state FROM ip_addresses WHERE id = $1`, *simIPID).Scan(&ipState); err != nil {
		t.Fatalf("re-read ip_address: %v", err)
	}
	if ipState != "allocated" {
		t.Errorf("ip_addresses.state = %q, want 'allocated'", ipState)
	}

	var used int
	if err := pool.QueryRow(ctx,
		`SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&used); err != nil {
		t.Fatalf("re-read pool: %v", err)
	}
	if used != 1 {
		t.Errorf("ip_pools.used_addresses = %d, want 1", used)
	}

	// Explicitly re-evaluate the enforcer via its public API to prove the
	// DB fall-through path works with nil cache — the earlier RADIUS call
	// already did this implicitly, but this assertion documents intent.
	sim, err := simStore.GetByIMSI(ctx, "286999000092001")
	if err != nil {
		t.Fatalf("GetByIMSI for enforcer verification: %v", err)
	}
	result, err := pe.Evaluate(ctx, sim, buildMinimalSessionContext(sim))
	if err != nil {
		t.Fatalf("enforcer.Evaluate via nil cache: %v", err)
	}
	if !result.Allow {
		t.Fatal("enforcer Allow=false via nil-cache DB fall-through — AC-9 regression")
	}
}

// swapNilCacheDBName replaces the dbname portion of a postgres DSN. Duplicated
// locally so this test doesn't depend on internal/store's unexported helper.
// Handles URL-style and keyword-value DSNs identically to the canonical
// STORY-087 implementation at internal/store/migration_freshvol_test.go:156.
func swapNilCacheDBName(dsn, newDB string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		schemeEnd := strings.Index(dsn, "://")
		if schemeEnd < 0 {
			return "", fmt.Errorf("invalid DSN: %s", dsn)
		}
		rest := dsn[schemeEnd+3:]
		slashIdx := strings.Index(rest, "/")
		if slashIdx < 0 {
			return dsn + "/" + newDB, nil
		}
		authority := rest[:slashIdx+1]
		tail := rest[slashIdx+1:]
		qIdx := strings.Index(tail, "?")
		var params string
		if qIdx >= 0 {
			params = tail[qIdx:]
		}
		return dsn[:schemeEnd+3] + authority + newDB + params, nil
	}
	parts := strings.Fields(dsn)
	found := false
	for i, p := range parts {
		if strings.HasPrefix(p, "dbname=") {
			parts[i] = "dbname=" + newDB
			found = true
			break
		}
	}
	if !found {
		parts = append(parts, "dbname="+newDB)
	}
	return strings.Join(parts, " "), nil
}

// nilCacheMigrationsDir returns an absolute path to the migrations/ dir by
// walking up from this source file (internal/aaa/radius/…). Independent of
// cwd — matches the STORY-087 pattern.
func nilCacheMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := file
	for i := 0; i < 4; i++ {
		dir = parentDir(dir)
	}
	// dir is now project root; append migrations
	migrDir := dir + string(os.PathSeparator) + "migrations"
	if _, err := os.Stat(migrDir); err != nil {
		t.Fatalf("migrations dir not found at %s: %v", migrDir, err)
	}
	return migrDir
}

func parentDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == os.PathSeparator {
			return p[:i]
		}
	}
	return p
}

func dropNilCacheDB(t *testing.T, adminDSN string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Logf("warn: drop DB connect: %v", err)
		return
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", nilCacheTestDBName)); err != nil {
		t.Logf("warn: DROP DATABASE: %v", err)
	}
}

// buildMinimalSessionContext constructs the DSL session context the way
// handleDirectAuth would. Lives next to the test so the test is self-contained.
func buildMinimalSessionContext(sim *store.SIM) dsl.SessionContext {
	sessCtx := dsl.SessionContext{
		SIMID:     sim.ID.String(),
		TenantID:  sim.TenantID.String(),
		RATType:   "lte",
		SimType:   sim.SimType,
		TimeOfDay: time.Now().Format("15:04"),
		DayOfWeek: time.Now().Weekday().String(),
	}
	if sim.APNID != nil {
		sessCtx.APN = sim.APNID.String()
	}
	return sessCtx
}

