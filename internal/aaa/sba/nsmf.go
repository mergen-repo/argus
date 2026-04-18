// Package sba — minimal mock Nsmf_PDUSession handler (STORY-092 Wave 3, D3-B).
//
// Scope (explicit): this handler is a MOCK. It implements only the two
// endpoints required to close the "/sessions UI shows `-` for 5G sessions"
// defect end-to-end:
//
//   - POST /nsmf-pdusession/v1/sm-contexts                — CreateSMContext
//   - DELETE /nsmf-pdusession/v1/sm-contexts/{smContextRef} — ReleaseSMContext
//
// Out of scope (deliberately, per advisor Risk 7):
//
//   - PATCH updates to an existing SMContext (no QoS flow updates).
//   - PCF / PCRF integration.
//   - UPF selection, N4 interface, packet forwarding.
//   - Persistence of smContextRef → allocation across process restarts
//     (the in-memory sync.Map is lost on reboot; callers that drive a
//     Release after a restart will receive 404 — acceptable for a mock).
//
// Allocation semantics mirror the RADIUS / Gx hot-path precedent
// (internal/aaa/radius/server.go allocateDynamicIPIfNeeded,
// internal/aaa/diameter/gx.go allocateDynamicIPIfNeeded): SIM resolution →
// active-state gate → APN pool selection → IPPoolStore.AllocateIP →
// SIMStore.SetIPAndPolicy → SIMCache.InvalidateIMSI. Release mirrors the
// RADIUS Accounting-Stop AC-3 path — dynamic allocations go back to the
// pool, static allocations are preserved.
//
// Enforcer gating is NOT performed here. Policy evaluation is the
// responsibility of the AUSF/UDM stages upstream; if those succeed, Nsmf
// proceeds. A future story may revisit this if Nsmf-side policy context
// becomes load-bearing.
//
// Response body shape follows 3GPP TS 29.502 conventions (ProblemDetails
// on error, not the Argus `{status,data,error}` envelope). The house
// writeProblem helper (ausf.go:377) renders ProblemDetails with fields
// {status, cause, detail} — this matches the 3GPP-native shape the SBA
// simulator client decodes in decodeCause (simulator/sba/ausf.go:233).
package sba

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// SIMCache is the minimal surface the Nsmf handler needs for cross-protocol
// cache coherence. The concrete implementation in production is
// *radius.SIMCache, which already implements both methods. Declared locally
// in the SBA package to avoid an import cycle with internal/aaa/radius.
type SIMCache interface {
	GetByIMSI(ctx context.Context, imsi string) (*store.SIM, error)
	InvalidateIMSI(ctx context.Context, imsi string) error
}

// IPPoolOperations is the minimal pool-side surface the Nsmf handler needs.
// *store.IPPoolStore satisfies this interface automatically — keeping the
// handler interface-typed lets unit tests substitute a fake that returns
// ErrPoolExhausted / ErrIPNotFound on demand, exercising real branches
// without a DB. Mirrors the same pattern we use for SIMResolver.
type IPPoolOperations interface {
	List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int, apnIDFilter *uuid.UUID) ([]store.IPPool, string, error)
	AllocateIP(ctx context.Context, poolID, simID uuid.UUID) (*store.IPAddress, error)
	GetIPAddressByID(ctx context.Context, id uuid.UUID) (*store.IPAddress, error)
	ReleaseIP(ctx context.Context, poolID, simID uuid.UUID) error
}

// SIMUpdater is the minimal SIM-side surface — matches the subset of
// *store.SIMStore the Nsmf handler uses. Exists so unit tests can assert
// that SetIPAndPolicy / ClearIPAddress were called without a DB.
type SIMUpdater interface {
	SetIPAndPolicy(ctx context.Context, simID uuid.UUID, ipAddressID *uuid.UUID, policyVersionID *uuid.UUID) error
	ClearIPAddress(ctx context.Context, simID uuid.UUID) error
}

// nsmfAllocation tracks the in-memory smContextRef → allocation mapping for
// this mock. Persistence across process restarts is out of scope (see package
// comment). The sync.Map is safe for concurrent Create / Release.
type nsmfAllocation struct {
	poolID      uuid.UUID
	simID       uuid.UUID
	allocatedID uuid.UUID
	imsi        string
}

type NsmfHandler struct {
	simResolver SIMResolver
	simStore    SIMUpdater
	ipPoolStore IPPoolOperations
	simCache    SIMCache
	logger      zerolog.Logger

	// smContexts maps smContextRef (uuid.String()) → nsmfAllocation. In-memory
	// only — see package comment for the rationale / scope limit.
	smContexts sync.Map
}

// SIMResolver (SBA-local) is the minimal subset of SIM lookup the Nsmf
// handler needs. Mirrors diameter.SIMResolver. Concrete impls in production:
// *radius.SIMCache. Kept tiny so tests can substitute a fake without pulling
// in the full SIMCache surface.
type SIMResolver interface {
	GetByIMSI(ctx context.Context, imsi string) (*store.SIM, error)
}

// NewNsmfHandler wires dependencies. All stores may be nil; when any of
// simResolver / simStore / ipPoolStore is nil, CreateSMContext returns 503
// INSUFFICIENT_RESOURCES (handler cannot allocate without its dependencies).
// simCache may be nil safely — InvalidateIMSI is skipped when nil.
//
// Accepts interface-typed stores so tests can substitute fakes without a DB.
// Concrete *store.SIMStore and *store.IPPoolStore satisfy SIMUpdater and
// IPPoolOperations respectively, so production wiring in main.go needs no
// adaptation.
func NewNsmfHandler(simResolver SIMResolver, simStore SIMUpdater, ipPoolStore IPPoolOperations, simCache SIMCache, logger zerolog.Logger) *NsmfHandler {
	return &NsmfHandler{
		simResolver: simResolver,
		simStore:    simStore,
		ipPoolStore: ipPoolStore,
		simCache:    simCache,
		logger:      logger.With().Str("component", "sba_nsmf").Logger(),
	}
}

// CreateSMContextRequest matches the minimal 3GPP TS 29.502 request shape the
// mock consumes. Additional fields in the real spec (anchorSmfUri, etc.) are
// ignored by json.Decoder.
type CreateSMContextRequest struct {
	SUPI            string `json:"supi"`
	DNN             string `json:"dnn"`
	SNSSAI          SNSSAI `json:"sNssai"`
	PDUSessionID    int    `json:"pduSessionId"`
	ServingNetwork  string `json:"servingNetwork"`
	ANType          string `json:"anType"`
	RATType         string `json:"ratType"`
}

// CreateSMContextResponse is the minimal 201-Created body. Real SMF responses
// carry far more (qosFlowSetupList, selectedSmfId, etc.); we emit only the
// fields needed to surface the allocated IP to the caller.
type CreateSMContextResponse struct {
	SUPI          string `json:"supi"`
	DNN           string `json:"dnn"`
	SNSSAI        SNSSAI `json:"sNssai"`
	UEIPv4Address string `json:"ueIpv4Address"`
}

// HandleCreate is POST /nsmf-pdusession/v1/sm-contexts.
func (h *NsmfHandler) HandleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is supported")
		return
	}

	var req CreateSMContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid request body")
		return
	}

	if req.SUPI == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "supi is required")
		return
	}

	// TS 23.003 §2.2: SUPI of type IMSI carries the "imsi-" prefix. Accept
	// bare IMSIs permissively: if TrimPrefix left nothing (either empty
	// SUPI, or the prefix was the whole string), fall back to the original
	// SUPI value so downstream GetByIMSI can surface ErrSIMNotFound naturally.
	imsi := strings.TrimPrefix(req.SUPI, "imsi-")
	if imsi == "" {
		imsi = req.SUPI
	}

	ctx := r.Context()

	// Subscriber resolution runs FIRST — before the store-deps gate. This
	// lets the user-not-found and SIM-state branches return the correct
	// 3GPP-native status codes (404 USER_NOT_FOUND / 403 USER_UNKNOWN) even
	// when the IP allocation backing deps (simStore / ipPoolStore) are not
	// wired (e.g. unit-test fakes, misconfigured env). The allocation gate
	// below still 503s if those deps are nil, which is the correct answer
	// for an active user the server cannot actually allocate for.
	if h.simResolver == nil {
		writeProblem(w, http.StatusServiceUnavailable, "INSUFFICIENT_RESOURCES", "nsmf sim resolver not wired")
		return
	}
	sim, err := h.simResolver.GetByIMSI(ctx, imsi)
	if err != nil {
		if err == store.ErrSIMNotFound {
			writeProblem(w, http.StatusNotFound, "USER_NOT_FOUND", "subscriber not provisioned")
			return
		}
		h.logger.Error().Err(err).Str("imsi", imsi).Msg("nsmf: SIM lookup failed")
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "subscriber lookup failed")
		return
	}

	// Mirror RADIUS / Gx Wave-1/Wave-2 advisor flag #6: allocation only after
	// SIM is confirmed active.
	if sim.State != "active" {
		writeProblem(w, http.StatusForbidden, "USER_UNKNOWN", fmt.Sprintf("sim state is %s, expected active", sim.State))
		return
	}

	if sim.APNID == nil {
		writeProblem(w, http.StatusServiceUnavailable, "INSUFFICIENT_RESOURCES", "sim has no APN — cannot select pool")
		return
	}

	// Allocation gate: without both stores we cannot produce an IP. Returns
	// 503 INSUFFICIENT_RESOURCES, same as a pool-exhausted case — semantically
	// correct (the server cannot satisfy the request).
	if h.simStore == nil || h.ipPoolStore == nil {
		writeProblem(w, http.StatusServiceUnavailable, "INSUFFICIENT_RESOURCES", "nsmf allocation stores not wired")
		return
	}

	pools, _, err := h.ipPoolStore.List(ctx, sim.TenantID, "", 1, sim.APNID)
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", sim.ID.String()).Msg("nsmf: list pools failed")
		writeProblem(w, http.StatusServiceUnavailable, "INSUFFICIENT_RESOURCES", "pool lookup failed")
		return
	}
	if len(pools) == 0 {
		writeProblem(w, http.StatusServiceUnavailable, "INSUFFICIENT_RESOURCES", "no ip pool for tenant+apn")
		return
	}

	allocated, err := h.ipPoolStore.AllocateIP(ctx, pools[0].ID, sim.ID)
	if err != nil {
		if err == store.ErrPoolExhausted {
			writeProblem(w, http.StatusServiceUnavailable, "INSUFFICIENT_RESOURCES", "ip pool exhausted")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", sim.ID.String()).Str("pool_id", pools[0].ID.String()).Msg("nsmf: AllocateIP failed")
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ip allocation failed")
		return
	}

	if err := h.simStore.SetIPAndPolicy(ctx, sim.ID, &allocated.ID, nil); err != nil {
		h.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Str("ip_id", allocated.ID.String()).Msg("nsmf: persist ip_address_id failed")
	}

	if h.simCache != nil {
		if err := h.simCache.InvalidateIMSI(ctx, imsi); err != nil {
			h.logger.Warn().Err(err).Str("imsi", imsi).Msg("nsmf: SIMCache InvalidateIMSI failed")
		}
	}

	smContextRef := uuid.New().String()
	h.smContexts.Store(smContextRef, &nsmfAllocation{
		poolID:      pools[0].ID,
		simID:       sim.ID,
		allocatedID: allocated.ID,
		imsi:        imsi,
	})

	ueIPv4 := ""
	if allocated.AddressV4 != nil {
		ueIPv4 = stripCIDR(*allocated.AddressV4)
	}

	resp := CreateSMContextResponse{
		SUPI:          req.SUPI,
		DNN:           req.DNN,
		SNSSAI:        req.SNSSAI,
		UEIPv4Address: ueIPv4,
	}

	h.logger.Info().
		Str("sm_context_ref", smContextRef).
		Str("supi", req.SUPI).
		Str("sim_id", sim.ID.String()).
		Str("ip_id", allocated.ID.String()).
		Str("ue_ipv4", ueIPv4).
		Msg("nsmf: SMContext created, IP allocated")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/nsmf-pdusession/v1/sm-contexts/"+smContextRef)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleRelease is DELETE /nsmf-pdusession/v1/sm-contexts/{smContextRef}.
func (h *NsmfHandler) HandleRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only DELETE is supported")
		return
	}

	smContextRef := extractSmContextRef(r.URL.Path)
	if smContextRef == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Missing smContextRef in path")
		return
	}

	v, ok := h.smContexts.LoadAndDelete(smContextRef)
	if !ok {
		writeProblem(w, http.StatusNotFound, "CONTEXT_NOT_FOUND", "sm context not found or already released")
		return
	}
	alloc := v.(*nsmfAllocation)

	ctx := r.Context()

	// Fetch ip_addresses row to determine dynamic vs static — static reservations
	// are preserved (mirror RADIUS AC-3 Test 2 semantics).
	if h.ipPoolStore != nil {
		ipAddr, err := h.ipPoolStore.GetIPAddressByID(ctx, alloc.allocatedID)
		if err != nil {
			h.logger.Warn().Err(err).Str("sm_context_ref", smContextRef).Msg("nsmf: GetIPAddressByID failed during release; dropping ref")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if ipAddr.AllocationType == "dynamic" {
			if err := h.ipPoolStore.ReleaseIP(ctx, alloc.poolID, alloc.simID); err != nil {
				h.logger.Warn().Err(err).Str("sim_id", alloc.simID.String()).Str("ip_id", alloc.allocatedID.String()).Msg("nsmf: ReleaseIP failed")
			}
			if h.simStore != nil {
				if err := h.simStore.ClearIPAddress(ctx, alloc.simID); err != nil {
					h.logger.Warn().Err(err).Str("sim_id", alloc.simID.String()).Msg("nsmf: ClearIPAddress failed")
				}
			}
			if h.simCache != nil && alloc.imsi != "" {
				if err := h.simCache.InvalidateIMSI(ctx, alloc.imsi); err != nil {
					h.logger.Warn().Err(err).Str("imsi", alloc.imsi).Msg("nsmf: SIMCache InvalidateIMSI failed on release")
				}
			}
			h.logger.Info().
				Str("sm_context_ref", smContextRef).
				Str("sim_id", alloc.simID.String()).
				Str("ip_id", alloc.allocatedID.String()).
				Msg("nsmf: dynamic IP released")
		} else {
			h.logger.Info().
				Str("sm_context_ref", smContextRef).
				Str("sim_id", alloc.simID.String()).
				Str("allocation_type", ipAddr.AllocationType).
				Msg("nsmf: non-dynamic allocation preserved on release")
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// extractSmContextRef pulls the {smContextRef} path segment from
// /nsmf-pdusession/v1/sm-contexts/{smContextRef}. Returns "" on malformed or
// missing ref. Mirrors extractAuthCtxID (ausf.go:324) for consistency — no
// chi dependency.
func extractSmContextRef(path string) string {
	const prefix = "/nsmf-pdusession/v1/sm-contexts/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return ""
	}
	// Defensive: reject anything with further path segments (this handler
	// owns only /{smContextRef}, no sub-resources in the mock).
	if strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// stripCIDR removes an optional /prefixlen suffix from a PostgreSQL INET-style
// address string (e.g. "10.100.0.1/32" → "10.100.0.1"). The UE IP is emitted
// without the suffix per TS 29.502 — callers expect a dotted-quad.
func stripCIDR(addr string) string {
	if i := strings.Index(addr, "/"); i >= 0 {
		return addr[:i]
	}
	return addr
}
