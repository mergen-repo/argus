package sba

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// testSBADBPool opens a pgxpool.Pool from DATABASE_URL. Matches the diameter
// and radius test helpers (gx_ipalloc_test.go:19, dynamic_alloc_test.go:23).
func testSBADBPool(t *testing.T) *pgxpool.Pool {
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

// sbaFullFlowFixture seeds tenant + apn + pool (5 available addresses, no
// pre-allocation) + 1 active SIM with policy_version_id pointing at an
// allow-all policy. Used by TestSBAFullFlow_NsmfAllocates.
func sbaFullFlowFixture(t *testing.T, pool *pgxpool.Pool) (tenantID, simID, poolID uuid.UUID, imsi string) {
	t.Helper()
	ctx := context.Background()

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('story092-sba-full-'||gen_random_uuid()::text, 'sba@story092.test')
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
		VALUES ($1, $2, 'story092-sba-'||gen_random_uuid()::text, 'STORY-092 SBA Full-Flow', 'iot', 'active')
		RETURNING id`, tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, total_addresses, used_addresses, state)
		VALUES ($1, $2, 'STORY-092 SBA Full-Flow Pool', '10.252.0.0/24'::cidr, 5, 0, 'active')
		RETURNING id`, tenantID, apnID).Scan(&poolID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	for i := 1; i <= 5; i++ {
		ipv4 := fmt.Sprintf("10.252.0.%d", i)
		if _, err := pool.Exec(ctx, `
			INSERT INTO ip_addresses (pool_id, address_v4, allocation_type, state)
			VALUES ($1, $2::inet, 'dynamic', 'available')`, poolID, ipv4); err != nil {
			t.Fatalf("seed ip %s: %v", ipv4, err)
		}
	}

	iccid := fmt.Sprintf("899012%014d", time.Now().UnixNano()%100_000_000_000_000)
	imsi = fmt.Sprintf("286995%09d", time.Now().UnixNano()%1_000_000_000)
	msisdn := fmt.Sprintf("9057%08d", time.Now().UnixNano()%100_000_000)

	// Seed an allow-all policy + policy_version so the SIM has a valid
	// policy_version_id. Column names verified against the live schema on
	// 2026-04-18 (policies.scope ∈ {tenant, apn, operator, sim}; no
	// operator_id column; policy_versions uses dsl_content + compiled_rules).
	// RLS is force-enabled on both tables — explicit tenant set via
	// session_user default is not set here, but since the test uses the
	// 'argus' application user which has BYPASSRLS off, we must set
	// app.current_tenant per this session for the policy_versions lookup
	// elsewhere in the codebase. For the seeds here, RLS does not block
	// because we're the owner — but the SIM.policy_version_id is set, and
	// the enforcer path is not exercised by the Nsmf test, so we don't need
	// the policy to actually compile.
	var policyID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'story092-sba-allow-all-'||gen_random_uuid()::text, 'tenant', 'active')
		RETURNING id`, tenantID).Scan(&policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	var policyVersionID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow all', $2::jsonb, 'active')
		RETURNING id`, policyID, `{"rules":[{"when":{},"then":{"action":"allow"}}]}`).Scan(&policyVersionID); err != nil {
		t.Fatalf("seed policy_version: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id)
		VALUES ($1, $2, $3, $4, $5, $6, 'physical', 'active', 'nr', $7)
		RETURNING id`, tenantID, operatorID, apnID, iccid, imsi, msisdn, policyVersionID).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `UPDATE sims SET ip_address_id = NULL WHERE id = $1`, simID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_addresses WHERE pool_id = $1`, poolID)
		_, _ = pool.Exec(cctx, `DELETE FROM ip_pools WHERE id = $1`, poolID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE id = $1`, simID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE id = $1`, policyVersionID)
		_, _ = pool.Exec(cctx, `UPDATE policies SET current_version_id = NULL WHERE id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	return tenantID, simID, poolID, imsi
}

// simResolverAdapter adapts *store.SIMStore to the SBA-local SIMResolver
// interface. Mirrors the Diameter simStoreResolver (gx_ipalloc_test.go:186).
type simResolverAdapter struct {
	s *store.SIMStore
}

func (r *simResolverAdapter) GetByIMSI(ctx context.Context, imsi string) (*store.SIM, error) {
	return r.s.GetByIMSI(ctx, imsi)
}

// TestSBAFullFlow_NsmfAllocates drives the full SBA auth+register+allocate
// sequence against a real Argus SBA server backed by a real DB:
//
//  1. POST  /nausf-auth/v1/ue-authentications           → HandleAuthentication
//  2. PUT   /nausf-auth/v1/ue-authentications/{id}/…    → HandleConfirmation
//  3. POST  /nudm-ueau/v1/{supi}/auth-events            → HandleAuthEvents
//  4. POST  /nsmf-pdusession/v1/sm-contexts             → HandleCreate (Nsmf)
//  5. DELETE /nsmf-pdusession/v1/sm-contexts/{ref}      → HandleRelease (Nsmf)
//
// Assertions:
//   - CreateSMContext returns 201 with a non-empty ueIpv4Address and Location header.
//   - The returned IP appears in ip_addresses with state='allocated', sim_id set.
//   - ip_pools.used_addresses == 1.
//   - Post-Release: ip_addresses.state='available', sim_id NULL, used_addresses == 0.
//
// Skipped when DATABASE_URL is unset — same idiom as migration_freshvol_test.go.
func TestSBAFullFlow_NsmfAllocates(t *testing.T) {
	pool := testSBADBPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	_, simID, poolID, imsi := sbaFullFlowFixture(t, pool)

	simStore := store.NewSIMStore(pool)
	ipPoolStore := store.NewIPPoolStore(pool)
	resolver := &simResolverAdapter{s: simStore}

	// Mount the full SBA handler surface on an httptest.Server. We don't use
	// aaasba.NewServer directly (it owns NRF heartbeats + TLS boot) — this
	// mirrors the approach the simulator integration_test.go takes.
	ausf := NewAUSFHandler(nil, nil, zerolog.Nop())
	udm := NewUDMHandler(nil, nil, zerolog.Nop())
	nsmf := NewNsmfHandler(resolver, simStore, ipPoolStore, nil /* simCache nil — permitted */, zerolog.Nop())

	mux := http.NewServeMux()
	mux.HandleFunc("/nausf-auth/v1/ue-authentications", ausf.HandleAuthentication)
	mux.HandleFunc("/nausf-auth/v1/ue-authentications/", func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) > len("/nausf-auth/v1/ue-authentications/") &&
			r.URL.Path[len(r.URL.Path)-len("/5g-aka-confirmation"):] == "/5g-aka-confirmation" {
			ausf.HandleConfirmation(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/nudm-ueau/v1/", func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) > 0 && r.URL.Path[len(r.URL.Path)-len("/auth-events"):] == "/auth-events" {
			udm.HandleAuthEvents(w, r)
			return
		}
		if len(r.URL.Path) > 0 && r.URL.Path[len(r.URL.Path)-len("/security-information"):] == "/security-information" {
			udm.HandleSecurityInfo(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts", nsmf.HandleCreate)
	mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts/", nsmf.HandleRelease)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	supi := "imsi-" + imsi
	servingNetwork := "5G:mnc001.mcc286.3gppnetwork.org"

	// Step 1: AUSF POST ue-authentications → returns 5g-aka confirmation href.
	authReqBody := fmt.Sprintf(`{"supiOrSuci":%q,"servingNetworkName":%q}`, supi, servingNetwork)
	resp, err := http.Post(srv.URL+"/nausf-auth/v1/ue-authentications", "application/json", bytes.NewReader([]byte(authReqBody)))
	if err != nil {
		t.Fatalf("AUSF POST: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("AUSF POST status = %d, body = %s", resp.StatusCode, body)
	}
	var authResp AuthenticationResponse
	_ = json.NewDecoder(resp.Body).Decode(&authResp)
	resp.Body.Close()

	confirmLink, ok := authResp.Links["5g-aka"]
	if !ok || confirmLink.Href == "" {
		t.Fatalf("AUSF POST: missing 5g-aka link in response: %+v", authResp)
	}

	// Step 2: AUSF PUT 5g-aka-confirmation. Compute xresStar locally with the
	// same deterministic AV generator the server uses — see
	// internal/aaa/sba/ausf.go:340 generate5GAV.
	_, _, xresStar, _ := generate5GAV(supi, servingNetwork)
	confReqBody := fmt.Sprintf(`{"resStar":%q}`, base64.StdEncoding.EncodeToString(xresStar))
	req, _ := http.NewRequest(http.MethodPut, srv.URL+confirmLink.Href, bytes.NewReader([]byte(confReqBody)))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("AUSF PUT: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("AUSF PUT status = %d, body = %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	// Step 3: UDM auth-events. Non-load-bearing for IP allocation but
	// included to exercise the AUSF→UDM→Nsmf sequence as a real 5G client
	// would. Failures are tolerated here by design (the surface is narrower
	// than Nsmf).
	eventReqBody := `{"nfInstanceId":"story092-test","success":true,"timeStamp":"2026-04-18T00:00:00Z","authType":"5G_AKA","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org"}`
	resp, err = http.Post(srv.URL+"/nudm-ueau/v1/"+supi+"/auth-events", "application/json", bytes.NewReader([]byte(eventReqBody)))
	if err != nil {
		t.Logf("UDM auth-events (non-fatal): %v", err)
	} else {
		resp.Body.Close()
	}

	// Step 4: Nsmf CreateSMContext — the assertion-critical step.
	smReqBody := fmt.Sprintf(`{"supi":%q,"dnn":"internet","sNssai":{"sst":1,"sd":"000001"},"pduSessionId":5,"servingNetwork":%q,"anType":"3GPP_ACCESS","ratType":"NR"}`, supi, servingNetwork)
	resp, err = http.Post(srv.URL+"/nsmf-pdusession/v1/sm-contexts", "application/json", bytes.NewReader([]byte(smReqBody)))
	if err != nil {
		t.Fatalf("Nsmf POST: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("Nsmf POST status = %d, body = %s", resp.StatusCode, body)
	}
	locHeader := resp.Header.Get("Location")
	if locHeader == "" {
		resp.Body.Close()
		t.Fatal("Nsmf POST: missing Location header")
	}
	var smResp CreateSMContextResponse
	if err := json.NewDecoder(resp.Body).Decode(&smResp); err != nil {
		resp.Body.Close()
		t.Fatalf("decode Nsmf response: %v", err)
	}
	resp.Body.Close()

	if smResp.UEIPv4Address == "" {
		t.Fatal("ueIpv4Address empty in response body")
	}

	// DB assertions — allocation state.
	dbCtx := context.Background()
	var allocState, allocType string
	var simIDinIP *uuid.UUID
	if err := pool.QueryRow(dbCtx, `
		SELECT state, allocation_type, sim_id FROM ip_addresses
		WHERE pool_id = $1 AND address_v4 = $2::inet`,
		poolID, smResp.UEIPv4Address).Scan(&allocState, &allocType, &simIDinIP); err != nil {
		t.Fatalf("re-read ip_address: %v", err)
	}
	if allocState != "allocated" {
		t.Errorf("ip_addresses.state = %q, want 'allocated'", allocState)
	}
	if allocType != "dynamic" {
		t.Errorf("ip_addresses.allocation_type = %q, want 'dynamic'", allocType)
	}
	if simIDinIP == nil || *simIDinIP != simID {
		t.Errorf("ip_addresses.sim_id = %v, want %v", simIDinIP, simID)
	}

	var usedAfter int
	if err := pool.QueryRow(dbCtx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&usedAfter); err != nil {
		t.Fatalf("re-read pool after alloc: %v", err)
	}
	if usedAfter != 1 {
		t.Errorf("ip_pools.used_addresses after alloc = %d, want 1", usedAfter)
	}

	// Step 5: Nsmf ReleaseSMContext.
	smContextRef := locHeader
	// extract trailing segment from the Location header (matches simulator behaviour).
	if idx := lastSlashIndex(locHeader); idx >= 0 {
		smContextRef = locHeader[idx+1:]
	}
	relReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/nsmf-pdusession/v1/sm-contexts/"+smContextRef, nil)
	relResp, err := http.DefaultClient.Do(relReq)
	if err != nil {
		t.Fatalf("Nsmf DELETE: %v", err)
	}
	if relResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(relResp.Body)
		relResp.Body.Close()
		t.Fatalf("Nsmf DELETE status = %d, body = %s", relResp.StatusCode, body)
	}
	relResp.Body.Close()

	// Post-release DB assertions.
	var relState string
	var relSimID *uuid.UUID
	if err := pool.QueryRow(dbCtx, `
		SELECT state, sim_id FROM ip_addresses
		WHERE pool_id = $1 AND address_v4 = $2::inet`,
		poolID, smResp.UEIPv4Address).Scan(&relState, &relSimID); err != nil {
		t.Fatalf("re-read ip_address post-release: %v", err)
	}
	if relState != "available" {
		t.Errorf("ip_addresses.state post-release = %q, want 'available'", relState)
	}
	if relSimID != nil {
		t.Errorf("ip_addresses.sim_id post-release = %v, want NULL", relSimID)
	}

	var usedPostRelease int
	if err := pool.QueryRow(dbCtx, `SELECT used_addresses FROM ip_pools WHERE id = $1`, poolID).Scan(&usedPostRelease); err != nil {
		t.Fatalf("re-read pool post-release: %v", err)
	}
	if usedPostRelease != 0 {
		t.Errorf("ip_pools.used_addresses post-release = %d, want 0", usedPostRelease)
	}

	var simIPIDPostRelease *uuid.UUID
	if err := pool.QueryRow(dbCtx, `SELECT ip_address_id FROM sims WHERE id = $1`, simID).Scan(&simIPIDPostRelease); err != nil {
		t.Fatalf("re-read sim post-release: %v", err)
	}
	if simIPIDPostRelease != nil {
		t.Errorf("sims.ip_address_id post-release = %v, want NULL", simIPIDPostRelease)
	}
}

// lastSlashIndex returns the index of the last '/' in s, or -1 if none.
// Local helper to avoid pulling in strings for a 6-line function (file only
// uses stdlib basics already).
func lastSlashIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
