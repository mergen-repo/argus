package sba

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/aaa/rattype"
	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type UDMHandler struct {
	sessionMgr *session.Manager
	eventBus   *bus.EventBus
	logger     zerolog.Logger
	// reg threads the metrics registry into ParsePEI so the
	// argus_imei_capture_parse_errors_total{protocol="5g_sba"} counter
	// fires on malformed PEI input (STORY-093 AC-6, gate F-A1). Nil-safe.
	reg *obsmetrics.Registry
}

func NewUDMHandler(sessionMgr *session.Manager, eventBus *bus.EventBus, reg *obsmetrics.Registry, logger zerolog.Logger) *UDMHandler {
	return &UDMHandler{
		sessionMgr: sessionMgr,
		eventBus:   eventBus,
		reg:        reg,
		logger:     logger.With().Str("component", "sba_udm").Logger(),
	}
}

func (h *UDMHandler) HandleSecurityInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is supported")
		return
	}

	supiOrSuci := extractSUPIFromPath(r.URL.Path, "/nudm-ueau/v1/", "/security-information")
	if supiOrSuci == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Missing supiOrSuci in path")
		return
	}

	supi := resolveSUPI(supiOrSuci)
	if supi == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid SUPI/SUCI format")
		return
	}

	servingNetwork := r.URL.Query().Get("servingNetworkName")
	if servingNetwork == "" {
		servingNetwork = "5G:mnc001.mcc001.3gppnetwork.org"
	}

	rand, autn, xresStar, kausf := generate5GAV(supi, servingNetwork)

	resp := SecurityInfoResponse{
		AuthVector: &AuthVector5G{
			AvType:   AuthType5GAKA,
			RAND:     base64.StdEncoding.EncodeToString(rand),
			AUTN:     base64.StdEncoding.EncodeToString(autn),
			XresStar: base64.StdEncoding.EncodeToString(xresStar),
			Kausf:    base64.StdEncoding.EncodeToString(kausf),
		},
		SUPI: supi,
	}

	h.logger.Info().
		Str("supi", supi).
		Str("serving_network", servingNetwork).
		Msg("UDM security info requested")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *UDMHandler) HandleAuthEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is supported")
		return
	}

	supiOrSuci := extractSUPIFromPath(r.URL.Path, "/nudm-ueau/v1/", "/auth-events")
	if supiOrSuci == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Missing supiOrSuci in path")
		return
	}

	supi := resolveSUPI(supiOrSuci)
	if supi == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid SUPI/SUCI format")
		return
	}

	var event AuthEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid request body")
		return
	}

	authEventID := uuid.New().String()

	h.logger.Info().
		Str("supi", supi).
		Bool("success", event.Success).
		Str("auth_type", event.AuthType).
		Str("auth_event_id", authEventID).
		Msg("UDM auth event recorded")

	if h.eventBus != nil {
		env := bus.NewSessionEnvelope("session.updated", bus.SystemTenantID.String(), "", "", "UDM auth event recorded").
			WithMeta("supi", supi).
			WithMeta("auth_event_id", authEventID).
			WithMeta("success", event.Success).
			WithMeta("auth_type", event.AuthType).
			WithMeta("protocol", "5g_sba")
		h.eventBus.Publish(r.Context(), bus.SubjectSessionUpdated, env)
	}

	resp := AuthEventResponse{
		AuthEventID: authEventID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *UDMHandler) HandleRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeProblem(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only PUT is supported")
		return
	}

	supi := extractSUPIFromUECMPath(r.URL.Path)
	if supi == "" {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Missing SUPI in path")
		return
	}

	var reg Amf3GppAccessRegistration
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		writeProblem(w, http.StatusBadRequest, "MANDATORY_IE_INCORRECT", "Invalid request body")
		return
	}

	imei, imeiSV, _ := ParsePEI(reg.PEI, h.logger, h.reg)

	h.logger.Info().
		Str("supi", supi).
		Str("amf_instance_id", reg.AmfInstanceID).
		Str("rat_type", reg.RATType).
		Bool("initial_reg", reg.InitialRegInd).
		Str("imei", imei).
		Str("imei_sv", imeiSV).
		Msg("UDM AMF registration")

	if h.sessionMgr != nil && reg.InitialRegInd {
		sess := &session.Session{
			IMSI:            extractIMSI(supi),
			AcctSessionID:   "5g-reg-" + uuid.New().String(),
			RATType:         rattype.FromSBA(reg.RATType),
			SessionState:    "active",
			StartedAt:       time.Now().UTC(),
			ProtocolType:    session.ProtocolType5GSBA,
			IMEI:            imei,
			SoftwareVersion: imeiSV,
		}
		if err := h.sessionMgr.Create(r.Context(), sess); err != nil {
			h.logger.Error().Err(err).Str("supi", supi).Msg("failed to create session for AMF registration")
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(reg)
}

func extractSUPIFromPath(path, prefix, suffix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	idx := strings.Index(rest, suffix)
	if idx < 0 {
		return ""
	}
	return rest[:idx]
}

func extractSUPIFromUECMPath(path string) string {
	const prefix = "/nudm-uecm/v1/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return rest
	}
	return rest[:idx]
}
