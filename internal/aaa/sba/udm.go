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
	"github.com/btopcu/argus/internal/policy/binding"
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

	// bindingGate is the STORY-096 IMEI/SIM binding pre-check gate.
	// Nil (the default) preserves exact pre-STORY-096 behaviour (AC-17).
	bindingGate BindingGate
	// simResolver resolves IMSI → *store.SIM for the binding pre-check.
	// Nil → binding gate is skipped (AC-17 nil-safe).
	simResolver SIMResolver
}

func NewUDMHandler(sessionMgr *session.Manager, eventBus *bus.EventBus, reg *obsmetrics.Registry, logger zerolog.Logger) *UDMHandler {
	return &UDMHandler{
		sessionMgr: sessionMgr,
		eventBus:   eventBus,
		reg:        reg,
		logger:     logger.With().Str("component", "sba_udm").Logger(),
	}
}

// SetBindingGate attaches the STORY-096 IMEI/SIM binding pre-check gate.
// When non-nil, the gate is called after PEI capture and before the
// registration response is written. Nil (the default) preserves
// pre-STORY-096 behaviour (AC-17).
func (h *UDMHandler) SetBindingGate(g BindingGate) {
	h.bindingGate = g
}

// SetSIMResolver attaches the SIM resolver used by the binding pre-check
// to look up the SIM record for the requesting IMSI. Nil → binding gate
// is skipped (nil-safe, AC-17).
func (h *UDMHandler) SetSIMResolver(r SIMResolver) {
	h.simResolver = r
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

	// STORY-096 AC-1/AC-10: binding pre-check after IMEI capture, before
	// registration response. Fail-open: evaluate/apply errors are logged
	// and registration continues. Nil gate or nil SIM resolver → skip
	// (AC-17).
	if h.bindingGate != nil && h.simResolver != nil {
		imsi := extractIMSI(supi)
		if sim, simErr := h.simResolver.GetByIMSI(r.Context(), imsi); simErr == nil && sim != nil {
			bSession := binding.SessionContext{
				TenantID:        sim.TenantID,
				SIMID:           sim.ID,
				IMEI:            imei,
				SoftwareVersion: imeiSV,
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
				h.logger.Warn().Err(evalErr).Msg("binding precheck evaluate failed (5g_sba/udm, fail-open)")
			} else {
				if applyErr := h.bindingGate.Apply(r.Context(), verdict, bSession, bSIM, "5g_sba"); applyErr != nil {
					h.logger.Warn().Err(applyErr).Msg("binding precheck apply failed (5g_sba/udm, fail-open)")
				}
				if verdict.Kind == binding.VerdictReject {
					h.logger.Info().
						Str("supi", supi).
						Str("reason", verdict.Reason).
						Msg("binding precheck rejected (5g_sba/udm)")
					writeProblem(w, http.StatusForbidden, verdict.Reason, "Binding mismatch")
					return
				}
			}
		}
	}

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
