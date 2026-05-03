package sba

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/btopcu/argus/internal/aaa/eap"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type EAPProxyHandler struct {
	stateMachine *eap.StateMachine
	logger       zerolog.Logger
}

type EAPAuthRequest struct {
	SUPIOrSUCI         string   `json:"supiOrSuci"`
	ServingNetworkName string   `json:"servingNetworkName"`
	AuthType           string   `json:"authType"`
	EAPPayload         string   `json:"eapPayload"`
	RequestedNSSAI     []SNSSAI `json:"requestedNssai,omitempty"`
}

type EAPAuthResponse struct {
	AuthType   AuthType            `json:"authType"`
	EAPPayload string              `json:"eapPayload"`
	SUPI       string              `json:"supi,omitempty"`
	Kseaf      string              `json:"kseaf,omitempty"`
	Links      map[string]AuthLink `json:"_links,omitempty"`
}

func NewEAPProxyHandler(sm *eap.StateMachine, logger zerolog.Logger) *EAPProxyHandler {
	return &EAPProxyHandler{
		stateMachine: sm,
		logger:       logger.With().Str("component", "sba_eap_proxy").Logger(),
	}
}

func (h *EAPProxyHandler) HandleEAPAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is supported")
		return
	}

	var req EAPAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid request body")
		return
	}

	if req.SUPIOrSUCI == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "supiOrSuci is required")
		return
	}

	supi := resolveSUPI(req.SUPIOrSUCI)
	if supi == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid SUPI/SUCI format")
		return
	}

	if h.stateMachine == nil {
		writeProblem(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "EAP-AKA' engine not available")
		return
	}

	sessionID := fmt.Sprintf("sba-eap-%s", uuid.New().String())

	var eapData []byte
	if req.EAPPayload != "" {
		var err error
		eapData, err = base64.StdEncoding.DecodeString(req.EAPPayload)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid EAP payload encoding")
			return
		}
	} else {
		eapData = h.stateMachine.StartIdentity(sessionID)
	}

	respData, err := h.stateMachine.ProcessPacket(r.Context(), sessionID, eapData)
	if err != nil {
		h.logger.Error().Err(err).Str("supi", supi).Msg("EAP-AKA' processing error")
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "EAP processing failed")
		return
	}

	resp := EAPAuthResponse{
		AuthType:   AuthTypeEAPAKA,
		EAPPayload: base64.StdEncoding.EncodeToString(respData),
		SUPI:       supi,
		Links: map[string]AuthLink{
			"eap-session": {
				Href: fmt.Sprintf("/nausf-auth/v1/eap-sessions/%s", sessionID),
			},
		},
	}

	h.logger.Info().
		Str("session_id", sessionID).
		Str("supi", supi).
		Msg("EAP-AKA' authentication via SBA proxy")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *EAPProxyHandler) HandleEAPContinue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is supported")
		return
	}

	sessionID := extractEAPSessionID(r.URL.Path)
	if sessionID == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Missing EAP session ID")
		return
	}

	var req EAPAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid request body")
		return
	}

	if req.EAPPayload == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "eapPayload is required")
		return
	}

	eapData, err := base64.StdEncoding.DecodeString(req.EAPPayload)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid EAP payload encoding")
		return
	}

	if h.stateMachine == nil {
		writeProblem(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "EAP-AKA' engine not available")
		return
	}

	respData, err := h.stateMachine.ProcessPacket(r.Context(), sessionID, eapData)
	if err != nil {
		h.logger.Error().Err(err).Str("session_id", sessionID).Msg("EAP-AKA' continue error")
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "EAP processing failed")
		return
	}

	resp := EAPAuthResponse{
		AuthType:   AuthTypeEAPAKA,
		EAPPayload: base64.StdEncoding.EncodeToString(respData),
	}

	h.logger.Info().
		Str("session_id", sessionID).
		Msg("EAP-AKA' continue via SBA proxy")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func extractEAPSessionID(path string) string {
	const prefix = "/nausf-auth/v1/eap-sessions/"
	if len(path) <= len(prefix) {
		return ""
	}
	rest := path[len(prefix):]
	if rest == "" {
		return ""
	}
	return rest
}
