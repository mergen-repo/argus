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
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type AUSFHandler struct {
	sessionMgr *session.Manager
	eventBus   *bus.EventBus
	logger     zerolog.Logger

	mu       sync.RWMutex
	contexts map[string]*AuthContext

	allowedSlices []SNSSAI
}

func NewAUSFHandler(sessionMgr *session.Manager, eventBus *bus.EventBus, logger zerolog.Logger) *AUSFHandler {
	return &AUSFHandler{
		sessionMgr: sessionMgr,
		eventBus:   eventBus,
		logger:     logger.With().Str("component", "sba_ausf").Logger(),
		contexts:   make(map[string]*AuthContext),
		allowedSlices: []SNSSAI{
			{SST: 1, SD: "000001"},
			{SST: 2, SD: "000001"},
			{SST: 3, SD: "000002"},
		},
	}
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
	}

	h.mu.Lock()
	h.contexts[authCtxID] = authCtx
	h.mu.Unlock()

	go h.expireContext(authCtxID, 30*time.Second)

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
			h.eventBus.Publish(context.Background(), bus.SubjectSessionEnded, map[string]interface{}{
				"auth_ctx_id": authCtxID,
				"supi":        authCtx.SUPI,
				"result":      "failure",
				"reason":      "res_star_mismatch",
				"protocol":    "5g_sba",
				"timestamp":   time.Now().UTC(),
			})
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
			IMSI:          extractIMSI(authCtx.SUPI),
			AcctSessionID: "5g-sba-" + authCtxID,
			RATType:       rattype.NR5G,
			SessionState:  "active",
			StartedAt:     time.Now().UTC(),
			ProtocolType:  session.ProtocolType5GSBA,
			SliceInfo:     sliceInfoJSON,
		}
		if err := h.sessionMgr.Create(context.Background(), sess); err != nil {
			h.logger.Error().Err(err).
				Str("supi", authCtx.SUPI).
				Msg("failed to create session after 5G-AKA confirmation")
		}
	}

	if h.eventBus != nil {
		h.eventBus.Publish(context.Background(), bus.SubjectSessionStarted, map[string]interface{}{
			"auth_ctx_id": authCtxID,
			"supi":        authCtx.SUPI,
			"result":      "success",
			"protocol":    "5g_sba",
			"auth_type":   string(authCtx.AuthType),
			"timestamp":   time.Now().UTC(),
		})
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
