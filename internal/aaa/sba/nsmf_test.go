package sba

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// fakeIPPoolStore satisfies IPPoolOperations with in-memory state. Used by
// the unit tests to exercise AllocateIP success, ErrPoolExhausted mapping,
// and ReleaseIP round-trip without a DB.
type fakeIPPoolStore struct {
	mu          sync.Mutex
	pools       []store.IPPool                  // returned by List
	allocateErr error                           // when non-nil, AllocateIP returns this
	allocated   map[uuid.UUID]*store.IPAddress  // keyed by ip_address.ID
	released    map[uuid.UUID]bool
	listErr     error
}

func (f *fakeIPPoolStore) List(_ context.Context, _ uuid.UUID, _ string, _ int, _ *uuid.UUID) ([]store.IPPool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, "", f.listErr
	}
	return f.pools, "", nil
}

func (f *fakeIPPoolStore) AllocateIP(_ context.Context, poolID, simID uuid.UUID) (*store.IPAddress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.allocateErr != nil {
		return nil, f.allocateErr
	}
	addr := &store.IPAddress{
		ID:             uuid.New(),
		PoolID:         poolID,
		AddressV4:      strPtr("10.99.0.1/32"),
		AllocationType: "dynamic",
		SimID:          &simID,
		State:          "allocated",
	}
	if f.allocated == nil {
		f.allocated = make(map[uuid.UUID]*store.IPAddress)
	}
	f.allocated[addr.ID] = addr
	return addr, nil
}

func (f *fakeIPPoolStore) GetIPAddressByID(_ context.Context, id uuid.UUID) (*store.IPAddress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if addr, ok := f.allocated[id]; ok {
		return addr, nil
	}
	return nil, store.ErrIPNotFound
}

func (f *fakeIPPoolStore) ReleaseIP(_ context.Context, _, simID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.released == nil {
		f.released = make(map[uuid.UUID]bool)
	}
	f.released[simID] = true
	return nil
}

// fakeSIMUpdater satisfies SIMUpdater in memory. Tracks SetIPAndPolicy /
// ClearIPAddress calls so tests can assert the handler persisted the
// allocation / released it.
type fakeSIMUpdater struct {
	mu      sync.Mutex
	set     map[uuid.UUID]*uuid.UUID
	cleared map[uuid.UUID]bool
}

func (f *fakeSIMUpdater) SetIPAndPolicy(_ context.Context, simID uuid.UUID, ipAddressID *uuid.UUID, _ *uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.set == nil {
		f.set = make(map[uuid.UUID]*uuid.UUID)
	}
	f.set[simID] = ipAddressID
	return nil
}

func (f *fakeSIMUpdater) ClearIPAddress(_ context.Context, simID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cleared == nil {
		f.cleared = make(map[uuid.UUID]bool)
	}
	f.cleared[simID] = true
	return nil
}

func strPtr(s string) *string { return &s }

// fakeSIMResolver is a minimal in-memory SIMResolver for unit tests — same
// pattern as the Diameter stubSIMResolver in internal/simulator/diameter/
// integration_test.go:21. It returns ErrSIMNotFound for any IMSI not in the
// map.
type fakeSIMResolver struct {
	sims map[string]*store.SIM
}

func (f *fakeSIMResolver) GetByIMSI(_ context.Context, imsi string) (*store.SIM, error) {
	s, ok := f.sims[imsi]
	if !ok {
		return nil, store.ErrSIMNotFound
	}
	return s, nil
}

// newTestNsmfHandler builds an NsmfHandler wired to a resolver but with the
// ipPoolStore and simStore explicitly nil. The handler's Create short-circuits
// with 503 INSUFFICIENT_RESOURCES when either is nil — this lets us exercise
// the resolver paths (user-not-found, inactive state) without a DB.
func newTestNsmfHandler(resolver SIMResolver) *NsmfHandler {
	return &NsmfHandler{
		simResolver: resolver,
		simStore:    nil,
		ipPoolStore: nil,
		simCache:    nil,
		logger:      zerolog.Nop(),
	}
}

// TestSBANsmfCreateSMContext_AllocatesIP is the unit-level coverage for Task
// 7a. Integration coverage (real DB + real IPPoolStore + SIMStore + verifying
// ip_addresses row state) is in nsmf_integration_test.go.
//
// Sub-tests at this level exercise request-handling branches without a DB:
//   - user_not_found:  resolver returns ErrSIMNotFound → 404 USER_NOT_FOUND
//   - sim_not_active:  resolver returns sim.State="suspended" → 403 USER_UNKNOWN
//   - missing_supi:    empty SUPI in body → 400 MANDATORY_IE_INCORRECT
//   - wrong_method:    GET on endpoint → 405 METHOD_NOT_ALLOWED
//   - bad_body:        malformed JSON → 400 MANDATORY_IE_INCORRECT
//   - pool_exhausted_placeholder: deps-nil short-circuit → 503 INSUFFICIENT_RESOURCES
//     (the real pool-exhaustion path needs a store, covered by the integration test)
func TestSBANsmfCreateSMContext_AllocatesIP(t *testing.T) {
	t.Run("user_not_found", func(t *testing.T) {
		h := newTestNsmfHandler(&fakeSIMResolver{sims: map[string]*store.SIM{}})
		srv := httptest.NewServer(http.HandlerFunc(h.HandleCreate))
		t.Cleanup(srv.Close)

		body := `{"supi":"imsi-286019999999999","dnn":"internet","sNssai":{"sst":1,"sd":"000001"},"pduSessionId":5,"servingNetwork":"5G:mnc001.mcc286.3gppnetwork.org","anType":"3GPP_ACCESS","ratType":"NR"}`
		resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		var prob ProblemDetails
		_ = json.NewDecoder(resp.Body).Decode(&prob)
		if prob.Cause != "USER_NOT_FOUND" {
			t.Errorf("cause = %q, want USER_NOT_FOUND", prob.Cause)
		}
	})

	t.Run("sim_not_active", func(t *testing.T) {
		tenantID := uuid.New()
		operatorID := uuid.New()
		apnID := uuid.New()
		simID := uuid.New()

		sim := &store.SIM{
			ID:         simID,
			TenantID:   tenantID,
			OperatorID: operatorID,
			APNID:      &apnID,
			IMSI:       "286019999999888",
			State:      "suspended",
		}
		resolver := &fakeSIMResolver{sims: map[string]*store.SIM{sim.IMSI: sim}}

		h := newTestNsmfHandler(resolver)
		srv := httptest.NewServer(http.HandlerFunc(h.HandleCreate))
		t.Cleanup(srv.Close)

		body := `{"supi":"imsi-286019999999888","dnn":"internet","sNssai":{"sst":1,"sd":"000001"},"pduSessionId":5,"servingNetwork":"5G:mnc001.mcc286.3gppnetwork.org","anType":"3GPP_ACCESS","ratType":"NR"}`
		resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
		var prob ProblemDetails
		_ = json.NewDecoder(resp.Body).Decode(&prob)
		if prob.Cause != "USER_UNKNOWN" {
			t.Errorf("cause = %q, want USER_UNKNOWN", prob.Cause)
		}
	})

	t.Run("missing_supi", func(t *testing.T) {
		h := newTestNsmfHandler(&fakeSIMResolver{sims: map[string]*store.SIM{}})
		srv := httptest.NewServer(http.HandlerFunc(h.HandleCreate))
		t.Cleanup(srv.Close)

		body := `{"dnn":"internet","sNssai":{"sst":1,"sd":"000001"},"pduSessionId":5}`
		resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		var prob ProblemDetails
		_ = json.NewDecoder(resp.Body).Decode(&prob)
		if prob.Cause != "MANDATORY_IE_INCORRECT" {
			t.Errorf("cause = %q, want MANDATORY_IE_INCORRECT", prob.Cause)
		}
	})

	t.Run("wrong_method", func(t *testing.T) {
		h := newTestNsmfHandler(&fakeSIMResolver{sims: map[string]*store.SIM{}})
		srv := httptest.NewServer(http.HandlerFunc(h.HandleCreate))
		t.Cleanup(srv.Close)

		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", resp.StatusCode)
		}
	})

	t.Run("bad_body", func(t *testing.T) {
		h := newTestNsmfHandler(&fakeSIMResolver{sims: map[string]*store.SIM{}})
		srv := httptest.NewServer(http.HandlerFunc(h.HandleCreate))
		t.Cleanup(srv.Close)

		resp, err := http.Post(srv.URL, "application/json", strings.NewReader("{not-json"))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("deps_not_wired_503", func(t *testing.T) {
		// With simStore / ipPoolStore nil (handler deps not wired) Create
		// short-circuits with 503 INSUFFICIENT_RESOURCES before reaching
		// AllocateIP. Separate from the real pool-exhausted path below.
		tenantID := uuid.New()
		operatorID := uuid.New()
		apnID := uuid.New()
		simID := uuid.New()

		sim := &store.SIM{
			ID:         simID,
			TenantID:   tenantID,
			OperatorID: operatorID,
			APNID:      &apnID,
			IMSI:       "286019999999777",
			State:      "active",
		}
		resolver := &fakeSIMResolver{sims: map[string]*store.SIM{sim.IMSI: sim}}

		h := newTestNsmfHandler(resolver)
		srv := httptest.NewServer(http.HandlerFunc(h.HandleCreate))
		t.Cleanup(srv.Close)

		body := `{"supi":"imsi-286019999999777","dnn":"internet","sNssai":{"sst":1,"sd":"000001"},"pduSessionId":5,"servingNetwork":"5G:mnc001.mcc286.3gppnetwork.org","anType":"3GPP_ACCESS","ratType":"NR"}`
		resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", resp.StatusCode)
		}
		var prob ProblemDetails
		_ = json.NewDecoder(resp.Body).Decode(&prob)
		if prob.Cause != "INSUFFICIENT_RESOURCES" {
			t.Errorf("cause = %q, want INSUFFICIENT_RESOURCES", prob.Cause)
		}
	})

	t.Run("success", func(t *testing.T) {
		// Real success path using in-memory fakes. Exercises:
		//   - resolver → active SIM
		//   - ipPoolStore.List → 1 pool
		//   - ipPoolStore.AllocateIP → success with IPAddress
		//   - simStore.SetIPAndPolicy → persistence
		//   - smContextRef stored in-memory + Location header + 201 body
		tenantID := uuid.New()
		operatorID := uuid.New()
		apnID := uuid.New()
		simID := uuid.New()
		poolID := uuid.New()

		sim := &store.SIM{
			ID: simID, TenantID: tenantID, OperatorID: operatorID,
			APNID: &apnID, IMSI: "286018888888888", State: "active",
		}
		resolver := &fakeSIMResolver{sims: map[string]*store.SIM{sim.IMSI: sim}}
		fakePool := &fakeIPPoolStore{pools: []store.IPPool{{ID: poolID, TenantID: tenantID, APNID: apnID, Name: "test-pool", State: "active"}}}
		fakeSIM := &fakeSIMUpdater{}

		h := &NsmfHandler{
			simResolver: resolver,
			simStore:    fakeSIM,
			ipPoolStore: fakePool,
			simCache:    nil,
			logger:      zerolog.Nop(),
		}
		srv := httptest.NewServer(http.HandlerFunc(h.HandleCreate))
		t.Cleanup(srv.Close)

		body := `{"supi":"imsi-286018888888888","dnn":"internet","sNssai":{"sst":1,"sd":"000001"},"pduSessionId":5,"servingNetwork":"5G:mnc001.mcc286.3gppnetwork.org","anType":"3GPP_ACCESS","ratType":"NR"}`
		resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", resp.StatusCode)
		}
		loc := resp.Header.Get("Location")
		if !strings.HasPrefix(loc, "/nsmf-pdusession/v1/sm-contexts/") {
			t.Errorf("Location = %q, want /nsmf-pdusession/v1/sm-contexts/ prefix", loc)
		}
		var createResp CreateSMContextResponse
		if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if createResp.UEIPv4Address != "10.99.0.1" {
			t.Errorf("ueIpv4 = %q, want 10.99.0.1 (CIDR stripped)", createResp.UEIPv4Address)
		}
		// Persistence assertion: SetIPAndPolicy was called with the
		// allocated ID.
		fakeSIM.mu.Lock()
		gotIP, ok := fakeSIM.set[simID]
		fakeSIM.mu.Unlock()
		if !ok || gotIP == nil {
			t.Errorf("SetIPAndPolicy not called for simID %s", simID)
		}
	})

	t.Run("pool_exhausted", func(t *testing.T) {
		// Real pool-exhausted path: fake AllocateIP returns ErrPoolExhausted,
		// handler must surface 503 INSUFFICIENT_RESOURCES with the correct
		// cause (mirrors the RADIUS / Gx fall-through when ErrPoolExhausted
		// surfaces).
		tenantID := uuid.New()
		operatorID := uuid.New()
		apnID := uuid.New()
		simID := uuid.New()
		poolID := uuid.New()

		sim := &store.SIM{
			ID: simID, TenantID: tenantID, OperatorID: operatorID,
			APNID: &apnID, IMSI: "286017777777777", State: "active",
		}
		resolver := &fakeSIMResolver{sims: map[string]*store.SIM{sim.IMSI: sim}}
		fakePool := &fakeIPPoolStore{
			pools:       []store.IPPool{{ID: poolID, TenantID: tenantID, APNID: apnID, Name: "exhausted", State: "active"}},
			allocateErr: store.ErrPoolExhausted,
		}
		fakeSIM := &fakeSIMUpdater{}

		h := &NsmfHandler{
			simResolver: resolver,
			simStore:    fakeSIM,
			ipPoolStore: fakePool,
			simCache:    nil,
			logger:      zerolog.Nop(),
		}
		srv := httptest.NewServer(http.HandlerFunc(h.HandleCreate))
		t.Cleanup(srv.Close)

		body := `{"supi":"imsi-286017777777777","dnn":"internet","sNssai":{"sst":1,"sd":"000001"},"pduSessionId":5,"servingNetwork":"5G:mnc001.mcc286.3gppnetwork.org","anType":"3GPP_ACCESS","ratType":"NR"}`
		resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", resp.StatusCode)
		}
		var prob ProblemDetails
		_ = json.NewDecoder(resp.Body).Decode(&prob)
		if prob.Cause != "INSUFFICIENT_RESOURCES" {
			t.Errorf("cause = %q, want INSUFFICIENT_RESOURCES", prob.Cause)
		}
	})
}

// TestSBANsmfReleaseSMContext_Unknown covers the 404 branch on Release for an
// unknown smContextRef (either never created, or already released). The
// dynamic/static release paths are covered by the integration test since
// they require real ip_addresses rows.
func TestSBANsmfReleaseSMContext_Unknown(t *testing.T) {
	h := newTestNsmfHandler(&fakeSIMResolver{sims: map[string]*store.SIM{}})
	mux := http.NewServeMux()
	mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts/", h.HandleRelease)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/nsmf-pdusession/v1/sm-contexts/"+uuid.New().String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var prob ProblemDetails
	_ = json.NewDecoder(resp.Body).Decode(&prob)
	if prob.Cause != "CONTEXT_NOT_FOUND" {
		t.Errorf("cause = %q, want CONTEXT_NOT_FOUND", prob.Cause)
	}
}

// TestExtractSmContextRef covers the path-extraction helper which is load-
// bearing for the route-to-handler contract (DELETE /sm-contexts/{ref}).
func TestExtractSmContextRef(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"happy", "/nsmf-pdusession/v1/sm-contexts/abc-123", "abc-123"},
		{"trailing_slash", "/nsmf-pdusession/v1/sm-contexts/abc-123/", "abc-123"},
		{"sub_resource_rejected", "/nsmf-pdusession/v1/sm-contexts/abc-123/modify", ""},
		{"no_ref", "/nsmf-pdusession/v1/sm-contexts/", ""},
		{"wrong_prefix", "/nausf-auth/v1/ue-authentications/xyz", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractSmContextRef(tc.path); got != tc.want {
				t.Errorf("extractSmContextRef(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}
