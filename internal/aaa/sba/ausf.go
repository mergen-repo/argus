package sba

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/aaa/rattype"
	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/policy/binding"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type AUSFHandler struct {
	sessionMgr *session.Manager
	eventBus   *bus.EventBus
	logger     zerolog.Logger
	// reg threads the metrics registry into ParsePEI so the
	// argus_imei_capture_parse_errors_total{protocol="5g_sba"} counter
	// fires on malformed PEI input (STORY-093 AC-6, gate F-A1). Nil-safe.
	reg *obsmetrics.Registry

	// bindingGate is the STORY-096 IMEI/SIM binding pre-check gate.
	// Nil (the default) preserves exact pre-STORY-096 behaviour (AC-17).
	bindingGate BindingGate
	// simResolver resolves IMSI → *store.SIM for the binding pre-check.
	// Nil → binding gate is skipped (AC-17 nil-safe).
	simResolver SIMResolver

	mu       sync.RWMutex
	contexts map[string]*AuthContext

	allowedSlices []SNSSAI
}

func NewAUSFHandler(sessionMgr *session.Manager, eventBus *bus.EventBus, reg *obsmetrics.Registry, logger zerolog.Logger) *AUSFHandler {
	return &AUSFHandler{
		sessionMgr: sessionMgr,
		eventBus:   eventBus,
		reg:        reg,
		logger:     logger.With().Str("component", "sba_ausf").Logger(),
		contexts:   make(map[string]*AuthContext),
		allowedSlices: []SNSSAI{
			{SST: 1, SD: "000001"},
			{SST: 2, SD: "000001"},
			{SST: 3, SD: "000002"},
		},
	}
}

// SetBindingGate attaches the STORY-096 IMEI/SIM binding pre-check gate.
// When non-nil, the gate is called after PEI capture and before the auth
// response is written. Nil (the default) preserves pre-STORY-096 behaviour
// (AC-17).
func (h *AUSFHandler) SetBindingGate(g BindingGate) {
	h.bindingGate = g
}

// SetSIMResolver attaches the SIM resolver used by the binding pre-check
// to look up the SIM record for the requesting IMSI. Nil → binding gate
// is skipped (nil-safe, AC-17).
func (h *AUSFHandler) SetSIMResolver(r SIMResolver) {
	h.simResolver = r
}

func (h *AUSFHandler) HandleAuthentication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is supported")
		return
	}

	var req AuthenticationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid request body")
		return
	}

	if req.SUPIOrSUCI == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "supiOrSuci is required")
		return
	}

	imei, imeiSV, _ := ParsePEI(req.PEI, h.logger, h.reg)
	peiRaw := ExtractPEIRaw(req.PEI)

	if req.ServingNetworkName == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "servingNetworkName is required")
		return
	}

	supi := resolveSUPI(req.SUPIOrSUCI)
	if supi == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid SUPI/SUCI format")
		return
	}

	if len(req.RequestedNSSAI) > 0 {
		if err := h.validateNSSAI(req.RequestedNSSAI); err != nil {
			writeProblem(w, http.StatusForbidden, "SNSSAI_NOT_ALLOWED", err.Error())
			return
		}
	}

	authCtxID := uuid.New().String()

	rand, autn, xresStar, kausf := generate5GAV(supi, req.ServingNetworkName)

	hxresStar := computeHxresStar(xresStar)
	kseaf := deriveKseaf(kausf, req.ServingNetworkName)

	authCtx := &AuthContext{
		ID:                 authCtxID,
		SUPI:               supi,
		SUCI:               req.SUPIOrSUCI,
		ServingNetworkName: req.ServingNetworkName,
		AuthType:           AuthType5GAKA,
		RAND:               rand,
		AUTN:               autn,
		XresStar:           xresStar,
		Kausf:              kausf,
		Kseaf:              kseaf,
		HxresStar:          hxresStar,
		AllowedNSSAI:       h.filterAllowedNSSAI(req.RequestedNSSAI),
		CreatedAt:          time.Now().UTC(),
		IMEI:               imei,
		SoftwareVersion:    imeiSV,
	}

	h.mu.Lock()
	h.contexts[authCtxID] = authCtx
	h.mu.Unlock()

	go h.expireContext(authCtxID, 30*time.Second)

	// STORY-096 AC-1/AC-10: binding pre-check after IMEI capture, before
	// auth response. Fail-open: evaluate/apply errors are logged and auth
	// continues. Nil gate or nil SIM resolver → skip (AC-17).
	if h.bindingGate != nil && h.simResolver != nil {
		imsi := extractIMSI(supi)
		if sim, simErr := h.simResolver.GetByIMSI(r.Context(), imsi); simErr == nil && sim != nil {
			bSession := binding.SessionContext{
				TenantID:        sim.TenantID,
				SIMID:           sim.ID,
				IMEI:            imei,
				SoftwareVersion: imeiSV,
				PEIRaw:          peiRaw,
			}
			bSIM := binding.SIMView{
				ID:                    sim.ID,
				TenantID:              sim.TenantID,
				BindingMode:           sim.BindingMode,
				BoundIMEI:             sim.BoundIMEI,
				BindingGraceExpiresAt: sim.BindingGraceExpiresAt,
			}
			verdict, evalErr := h.bindingGate.Evaluate(r.Context(), bSession, bSIM)
			if evalErr != nil {
				h.logger.Warn().Err(evalErr).Msg("binding precheck evaluate failed (5g_sba/ausf, fail-open)")
			} else {
				if applyErr := h.bindingGate.Apply(r.Context(), verdict, bSession, bSIM, "5g_sba"); applyErr != nil {
					h.logger.Warn().Err(applyErr).Msg("binding precheck apply failed (5g_sba/ausf, fail-open)")
				}
				if verdict.Kind == binding.VerdictReject {
					h.mu.Lock()
					delete(h.contexts, authCtxID)
					h.mu.Unlock()
					h.logger.Info().
						Str("auth_ctx_id", authCtxID).
						Str("supi", supi).
						Str("reason", verdict.Reason).
						Msg("binding precheck rejected (5g_sba/ausf)")
					writeProblem(w, http.StatusForbidden, verdict.Reason, "Binding mismatch")
					return
				}
			}
		}
	}

	resp := AuthenticationResponse{
		AuthType: AuthType5GAKA,
		AuthData5G: &AKA5GAuthData{
			RAND:      base64.StdEncoding.EncodeToString(rand),
			AUTN:      base64.StdEncoding.EncodeToString(autn),
			HxresStar: base64.StdEncoding.EncodeToString(hxresStar),
		},
		Links: map[string]AuthLink{
			"5g-aka": {
				Href: fmt.Sprintf("/nausf-auth/v1/ue-authentications/%s/5g-aka-confirmation", authCtxID),
			},
		},
	}

	h.logger.Info().
		Str("auth_ctx_id", authCtxID).
		Str("supi", supi).
		Str("serving_network", req.ServingNetworkName).
		Int("requested_slices", len(req.RequestedNSSAI)).
		Str("imei", imei).
		Str("imei_sv", imeiSV).
		Msg("5G-AKA authentication initiated")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/nausf-auth/v1/ue-authentications/%s", authCtxID))
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *AUSFHandler) HandleConfirmation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PUT is supported")
		return
	}

	authCtxID := extractAuthCtxID(r.URL.Path)
	if authCtxID == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Missing authCtxId")
		return
	}

	h.mu.RLock()
	authCtx, exists := h.contexts[authCtxID]
	h.mu.RUnlock()

	if !exists {
		writeProblem(w, http.StatusNotFound, "AUTH_CONTEXT_NOT_FOUND", "Authentication context not found or expired")
		return
	}

	var req ConfirmationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid request body")
		return
	}

	resStar, err := base64.StdEncoding.DecodeString(req.ResStar)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid resStar encoding")
		return
	}

	hresStar := computeHxresStar(resStar)
	if !hmac.Equal(hresStar, authCtx.HxresStar) {
		h.mu.Lock()
		delete(h.contexts, authCtxID)
		h.mu.Unlock()

		h.logger.Warn().
			Str("auth_ctx_id", authCtxID).
			Str("supi", authCtx.SUPI).
			Msg("5G-AKA confirmation failed: RES* mismatch")

		if h.eventBus != nil {
			env := bus.NewSessionEnvelope("session.ended", bus.SystemTenantID.String(), "", "", "5G-AKA confirmation failed").
				WithMeta("auth_ctx_id", authCtxID).
				WithMeta("supi", authCtx.SUPI).
				WithMeta("result", "failure").
				WithMeta("reason", "res_star_mismatch").
				WithMeta("protocol", "5g_sba")
			h.eventBus.Publish(context.Background(), bus.SubjectSessionEnded, env)
		}

		writeProblem(w, http.StatusUnauthorized, "AUTH_REJECTED", "Authentication verification failed")
		return
	}

	authCtx.Confirmed = true

	h.mu.Lock()
	delete(h.contexts, authCtxID)
	h.mu.Unlock()

	if h.sessionMgr != nil {
		var sliceInfoJSON json.RawMessage
		if len(authCtx.AllowedNSSAI) > 0 {
			sliceInfoJSON, _ = json.Marshal(authCtx.AllowedNSSAI)
		}

		sess := &session.Session{
			IMSI:            extractIMSI(authCtx.SUPI),
			AcctSessionID:   "5g-sba-" + authCtxID,
			RATType:         rattype.NR5G,
			SessionState:    "active",
			StartedAt:       time.Now().UTC(),
			ProtocolType:    session.ProtocolType5GSBA,
			SliceInfo:       sliceInfoJSON,
			IMEI:            authCtx.IMEI,
			SoftwareVersion: authCtx.SoftwareVersion,
		}
		if err := h.sessionMgr.Create(context.Background(), sess); err != nil {
			h.logger.Error().Err(err).
				Str("supi", authCtx.SUPI).
				Msg("failed to create session after 5G-AKA confirmation")
		}
	}

	if h.eventBus != nil {
		env := bus.NewSessionEnvelope("session.started", bus.SystemTenantID.String(), "", "", "Session started (5G-SBA)").
			WithMeta("auth_ctx_id", authCtxID).
			WithMeta("supi", authCtx.SUPI).
			WithMeta("result", "success").
			WithMeta("protocol", "5g_sba").
			WithMeta("auth_type", string(authCtx.AuthType))
		h.eventBus.Publish(context.Background(), bus.SubjectSessionStarted, env)
	}

	h.logger.Info().
		Str("auth_ctx_id", authCtxID).
		Str("supi", authCtx.SUPI).
		Msg("5G-AKA authentication confirmed successfully")

	resp := ConfirmationResponse{
		AuthResult: "SUCCESS",
		SUPI:       authCtx.SUPI,
		Kseaf:      base64.StdEncoding.EncodeToString(authCtx.Kseaf),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *AUSFHandler) validateNSSAI(requested []SNSSAI) error {
	for _, s := range requested {
		if !h.isSliceAllowed(s) {
			return fmt.Errorf("S-NSSAI (SST=%d, SD=%s) is not authorized", s.SST, s.SD)
		}
	}
	return nil
}

func (h *AUSFHandler) isSliceAllowed(s SNSSAI) bool {
	for _, allowed := range h.allowedSlices {
		if allowed.SST == s.SST {
			if allowed.SD == "" || s.SD == "" || allowed.SD == s.SD {
				return true
			}
		}
	}
	return false
}

func (h *AUSFHandler) filterAllowedNSSAI(requested []SNSSAI) []SNSSAI {
	if len(requested) == 0 {
		return nil
	}
	var allowed []SNSSAI
	for _, s := range requested {
		if h.isSliceAllowed(s) {
			allowed = append(allowed, s)
		}
	}
	return allowed
}

func (h *AUSFHandler) expireContext(id string, ttl time.Duration) {
	time.Sleep(ttl)
	h.mu.Lock()
	delete(h.contexts, id)
	h.mu.Unlock()
}

func (h *AUSFHandler) GetContext(id string) (*AuthContext, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ctx, ok := h.contexts[id]
	return ctx, ok
}

func resolveSUPI(supiOrSuci string) string {
	if strings.HasPrefix(supiOrSuci, "imsi-") {
		return supiOrSuci
	}
	if strings.HasPrefix(supiOrSuci, "suci-") {
		parts := strings.Split(supiOrSuci, "-")
		if len(parts) >= 4 {
			return "imsi-" + parts[1] + parts[2] + parts[3]
		}
	}
	if strings.HasPrefix(supiOrSuci, "nai-") {
		return supiOrSuci
	}
	return ""
}

func extractIMSI(supi string) string {
	if strings.HasPrefix(supi, "imsi-") {
		return supi[5:]
	}
	return supi
}

func extractAuthCtxID(path string) string {
	const prefix = "/nausf-auth/v1/ue-authentications/"
	const suffix = "/5g-aka-confirmation"

	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	rest = strings.TrimSuffix(rest, suffix)
	rest = strings.TrimSuffix(rest, "/")
	if rest == "" {
		return ""
	}
	return rest
}

func generate5GAV(supi, servingNetwork string) (rand, autn, xresStar, kausf []byte) {
	seed := sha256Sum([]byte("5g-av:" + supi + ":" + servingNetwork))

	rand = derivePseudoRandom(seed[:], 0, 16)
	autn = derivePseudoRandom(seed[:], 1, 16)
	xresStar = derivePseudoRandom(seed[:], 2, 16)
	kausf = derivePseudoRandom(seed[:], 3, 32)

	return
}

func computeHxresStar(xresStar []byte) []byte {
	h := sha256.Sum256(xresStar)
	return h[:16]
}

func deriveKseaf(kausf []byte, servingNetwork string) []byte {
	mac := hmac.New(sha256.New, kausf)
	mac.Write([]byte("5G:KSEAF:" + servingNetwork))
	return mac.Sum(nil)[:32]
}

func sha256Sum(data []byte) [32]byte {
	return sha256.Sum256(data)
}

func derivePseudoRandom(seed []byte, index int, length int) []byte {
	input := make([]byte, len(seed)+1)
	copy(input, seed)
	input[len(seed)] = byte(index)
	h := sha256.Sum256(input)
	if length > 32 {
		length = 32
	}
	return h[:length]
}

func writeProblem(w http.ResponseWriter, status int, cause, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ProblemDetails{
		Status: status,
		Cause:  cause,
		Detail: detail,
	})
}
