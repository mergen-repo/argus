package radius

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/aaa/eap"
	"github.com/btopcu/argus/internal/aaa/rattype"
	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/policy/enforcer"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/microsoft"
)

const (
	drainTimeout = 5 * time.Second
)

type MetricsRecorder interface {
	RecordAuth(ctx context.Context, operatorID uuid.UUID, success bool, latencyMs int)
}

// killSwitchChecker is satisfied by *killswitch.Service (or any test stub).
type killSwitchChecker interface {
	IsEnabled(key string) bool
}

type Server struct {
	authAddr       string
	acctAddr       string
	defaultSecret  string
	workerPoolSize int

	simCache      *SIMCache
	sessionMgr    *session.Manager
	operatorStore *store.OperatorStore
	ipPoolStore   *store.IPPoolStore
	eventBus      *bus.EventBus
	coaSender     *session.CoASender
	dmSender      *session.DMSender
	eapMachine      *eap.StateMachine
	eapAuthResults  sync.Map
	metricsRecorder MetricsRecorder
	policyEnforcer  *enforcer.Enforcer
	killSwitch      killSwitchChecker
	logger          zerolog.Logger

	authServer *radius.PacketServer
	acctServer *radius.PacketServer

	mu      sync.Mutex
	running bool
}

type ServerConfig struct {
	AuthAddr       string
	AcctAddr       string
	DefaultSecret  string
	WorkerPoolSize int
}

// parseV4Address strips an optional CIDR suffix (e.g. "/32") before
// parsing — PostgreSQL's INET type returns values in CIDR form, which
// net.ParseIP rejects silently.
func parseV4Address(s string) net.IP {
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	return net.ParseIP(s)
}

func NewServer(
	cfg ServerConfig,
	simCache *SIMCache,
	sessionMgr *session.Manager,
	operatorStore *store.OperatorStore,
	ipPoolStore *store.IPPoolStore,
	eventBus *bus.EventBus,
	coaSender *session.CoASender,
	dmSender *session.DMSender,
	logger zerolog.Logger,
) *Server {
	if cfg.WorkerPoolSize <= 0 {
		cfg.WorkerPoolSize = 256
	}
	return &Server{
		authAddr:       cfg.AuthAddr,
		acctAddr:       cfg.AcctAddr,
		defaultSecret:  cfg.DefaultSecret,
		workerPoolSize: cfg.WorkerPoolSize,
		simCache:       simCache,
		sessionMgr:     sessionMgr,
		operatorStore:  operatorStore,
		ipPoolStore:    ipPoolStore,
		eventBus:       eventBus,
		coaSender:      coaSender,
		dmSender:       dmSender,
		logger:         logger.With().Str("component", "radius_server").Logger(),
	}
}

// SetKillSwitch attaches an optional kill-switch service. When radius_auth is
// enabled the server rejects all Access-Request packets with Access-Reject.
func (s *Server) SetKillSwitch(ks killSwitchChecker) {
	s.killSwitch = ks
}

func (s *Server) Start(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	secretSource := radius.StaticSecretSource([]byte(s.defaultSecret))

	s.authServer = &radius.PacketServer{
		Addr:         s.authAddr,
		SecretSource: secretSource,
		Handler:      radius.HandlerFunc(s.handleAuth),
	}

	s.acctServer = &radius.PacketServer{
		Addr:         s.acctAddr,
		SecretSource: secretSource,
		Handler:      radius.HandlerFunc(s.handleAcct),
	}

	go func() {
		s.logger.Info().Str("addr", s.authAddr).Msg("RADIUS auth server starting")
		if err := s.authServer.ListenAndServe(); err != nil && err != radius.ErrServerShutdown {
			s.logger.Error().Err(err).Msg("RADIUS auth server error")
		}
	}()

	go func() {
		s.logger.Info().Str("addr", s.acctAddr).Msg("RADIUS acct server starting")
		if err := s.acctServer.ListenAndServe(); err != nil && err != radius.ErrServerShutdown {
			s.logger.Error().Err(err).Msg("RADIUS acct server error")
		}
	}()

	s.running = true
	s.logger.Info().
		Str("auth_addr", s.authAddr).
		Str("acct_addr", s.acctAddr).
		Int("worker_pool_size", s.workerPoolSize).
		Msg("RADIUS server started")

	return nil
}

func (s *Server) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := s.authServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error().Err(err).Msg("RADIUS auth server shutdown error")
		}
	}()
	go func() {
		defer wg.Done()
		if err := s.acctServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error().Err(err).Msg("RADIUS acct server shutdown error")
		}
	}()

	wg.Wait()
	s.running = false
	s.logger.Info().Msg("RADIUS server stopped")
	return nil
}

func (s *Server) Healthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Server) SetEAPMachine(machine *eap.StateMachine) {
	s.eapMachine = machine
}

func (s *Server) SetMetricsRecorder(mr MetricsRecorder) {
	s.metricsRecorder = mr
}

func (s *Server) SetPolicyEnforcer(pe *enforcer.Enforcer) {
	s.policyEnforcer = pe
}

func (s *Server) recordAuthMetric(ctx context.Context, operatorID uuid.UUID, success bool, startTime time.Time) {
	if s.metricsRecorder == nil {
		return
	}
	latencyMs := int(time.Since(startTime).Milliseconds())
	s.metricsRecorder.RecordAuth(ctx, operatorID, success, latencyMs)
}

func (s *Server) ActiveSessionCount(ctx context.Context) (int64, error) {
	return s.sessionMgr.CountActive(ctx)
}

func (s *Server) handleAuth(w radius.ResponseWriter, r *radius.Request) {
	ctx := r.Context()
	startTime := time.Now()
	correlationID := fmt.Sprintf("%s:%d", r.RemoteAddr.String(), r.Packet.Identifier)

	logger := s.logger.With().
		Str("correlation_id", correlationID).
		Str("remote_addr", r.RemoteAddr.String()).
		Str("type", "auth").
		Logger()

	// Kill-switch: radius_auth — reject all auth requests immediately.
	if s.killSwitch != nil && s.killSwitch.IsEnabled("radius_auth") {
		logger.Warn().Msg("Access-Reject: kill_switch radius_auth active")
		s.sendReject(w, r.Packet, "KILL_SWITCH_ACTIVE")
		return
	}

	eapMessage := rfc2869.EAPMessage_Get(r.Packet)
	if len(eapMessage) > 0 && s.eapMachine != nil {
		s.handleEAPAuth(ctx, w, r, eapMessage, logger, startTime)
		return
	}

	s.handleDirectAuth(ctx, w, r, logger, startTime)
}

func (s *Server) handleEAPAuth(ctx context.Context, w radius.ResponseWriter, r *radius.Request, eapMessage []byte, logger zerolog.Logger, startTime time.Time) {
	stateAttr := rfc2865.State_Get(r.Packet)
	sessionID := string(stateAttr)
	if sessionID == "" {
		sessionID = fmt.Sprintf("eap-%s-%d-%d", r.RemoteAddr.String(), r.Packet.Identifier, time.Now().UnixNano())
	}

	logger = logger.With().Str("eap_session_id", sessionID).Logger()

	eapPkt, err := eap.Decode(eapMessage)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to decode EAP-Message")
		s.sendReject(w, r.Packet, "EAP_DECODE_ERROR")
		return
	}

	if eapPkt.Code == eap.CodeResponse && eapPkt.Type == eap.MethodIdentity {
		imsi := string(eapPkt.Data)
		if imsi != "" {
			sim, err := s.simCache.GetByIMSI(ctx, imsi)
			if err != nil {
				if err == store.ErrSIMNotFound {
					logger.Info().Str("imsi", imsi).Msg("EAP Access-Reject: SIM not found")
					s.sendEAPReject(w, r.Packet, eapPkt.Identifier)
					return
				}
				logger.Error().Err(err).Msg("SIM lookup failed during EAP")
				s.sendEAPReject(w, r.Packet, eapPkt.Identifier)
				return
			}

			if sim.State != "active" {
				logger.Info().Str("sim_state", sim.State).Msg("EAP Access-Reject: SIM not active")
				s.sendEAPReject(w, r.Packet, eapPkt.Identifier)
				return
			}
		}
	}

	respRaw, err := s.eapMachine.ProcessPacket(ctx, sessionID, eapMessage)
	if err != nil {
		logger.Error().Err(err).Msg("EAP processing error")
		s.sendEAPReject(w, r.Packet, eapPkt.Identifier)
		return
	}

	respPkt, err := eap.Decode(respRaw)
	if err != nil {
		logger.Error().Err(err).Msg("failed to decode EAP response")
		s.sendEAPReject(w, r.Packet, eapPkt.Identifier)
		return
	}

	switch respPkt.Code {
	case eap.CodeSuccess:
		s.sendEAPAccept(ctx, w, r, respRaw, sessionID, logger, startTime)

	case eap.CodeFailure:
		s.sendEAPReject(w, r.Packet, respPkt.Identifier)
		var failOpID uuid.UUID
		if failIMSI, _ := rfc2865.UserName_LookupString(r.Packet); failIMSI != "" {
			if sim, err := s.simCache.GetByIMSI(ctx, failIMSI); err == nil && sim != nil {
				failOpID = sim.OperatorID
			}
		}
		s.recordAuthMetric(ctx, failOpID, false, startTime)
		logger.Info().
			Dur("latency_ms", time.Since(startTime)).
			Msg("EAP authentication failed, Access-Reject sent")

	default:
		challenge := r.Packet.Response(radius.CodeAccessChallenge)
		rfc2869.EAPMessage_Set(challenge, respRaw)
		rfc2865.State_Set(challenge, []byte(sessionID))

		if err := w.Write(challenge); err != nil {
			logger.Error().Err(err).Msg("failed to send Access-Challenge")
			return
		}
		logger.Debug().
			Dur("latency_ms", time.Since(startTime)).
			Msg("EAP Access-Challenge sent")
	}
}

func (s *Server) sendEAPAccept(ctx context.Context, w radius.ResponseWriter, r *radius.Request, eapSuccessRaw []byte, sessionID string, logger zerolog.Logger, startTime time.Time) {
	accept := r.Packet.Response(radius.CodeAccessAccept)
	rfc2869.EAPMessage_Set(accept, eapSuccessRaw)

	msk, _ := s.eapMachine.ConsumeSessionMSK(sessionID)
	if len(msk) >= 64 {
		sendKey := msk[:32]
		recvKey := msk[32:64]
		microsoft.MSMPPESendKey_Add(accept, sendKey)
		microsoft.MSMPPERecvKey_Add(accept, recvKey)
	}

	imsi, _ := rfc2865.UserName_LookupString(r.Packet)
	sessionTimeout := 86400
	idleTimeout := 3600
	filterID := "default"

	if imsi != "" {
		sim, err := s.simCache.GetByIMSI(ctx, imsi)
		if err == nil && sim != nil {
			if sim.IPAddressID != nil {
				ipAddr, err := s.ipPoolStore.GetIPAddressByID(ctx, *sim.IPAddressID)
				if err == nil && ipAddr.AddressV4 != nil {
					// PostgreSQL INET type can return values with a CIDR
					// suffix (e.g. "10.100.0.1/32") — strip it before
					// net.ParseIP, which otherwise returns nil silently.
					if ip := parseV4Address(*ipAddr.AddressV4); ip != nil {
						rfc2865.FramedIPAddress_Set(accept, ip.To4())
					}
				}
			}

			sessionTimeout = sim.SessionHardTimeoutSec
			if sessionTimeout <= 0 {
				sessionTimeout = 86400
			}
			idleTimeout = sim.SessionIdleTimeoutSec
			if idleTimeout <= 0 {
				idleTimeout = 3600
			}

			if s.policyEnforcer != nil && sim.PolicyVersionID != nil {
				ratTypeStr := extract3GPPRATType(r.Packet)
				now := time.Now()
				sessCtx := dsl.SessionContext{
					SIMID:     sim.ID.String(),
					TenantID:  sim.TenantID.String(),
					RATType:   ratTypeStr,
					SimType:   sim.SimType,
					TimeOfDay: now.Format("15:04"),
					DayOfWeek: now.Weekday().String(),
				}
				if sim.APNID != nil {
					sessCtx.APN = sim.APNID.String()
				}

				policyResult, pErr := s.policyEnforcer.Evaluate(ctx, sim, sessCtx)
				if pErr == nil && policyResult != nil {
					if !policyResult.Allow {
						logger.Info().Str("sim_id", sim.ID.String()).Msg("EAP policy denied, sending Reject")
						s.sendEAPReject(w, r.Packet, 0)
						go s.policyEnforcer.RecordViolations(ctx, sim, policyResult, nil)
						return
					}
					sessionTimeout = policyResult.SessionTimeout
					idleTimeout = policyResult.IdleTimeout
					filterID = policyResult.FilterID
					if len(policyResult.Violations) > 0 {
						go s.policyEnforcer.RecordViolations(ctx, sim, policyResult, nil)
					}
				}
			}
		}
	}

	rfc2865.SessionTimeout_Set(accept, rfc2865.SessionTimeout(sessionTimeout))
	rfc2865.IdleTimeout_Set(accept, rfc2865.IdleTimeout(idleTimeout))
	rfc2865.FilterID_SetString(accept, filterID)

	if err := w.Write(accept); err != nil {
		logger.Error().Err(err).Msg("failed to send EAP Access-Accept")
		return
	}

	var operatorID uuid.UUID
	if imsi != "" {
		if sim, err := s.simCache.GetByIMSI(ctx, imsi); err == nil && sim != nil {
			operatorID = sim.OperatorID
		}
	}
	s.recordAuthMetric(ctx, operatorID, true, startTime)

	eapMethod, _ := s.eapMachine.GetSessionMethod(ctx, sessionID)
	methodStr := eapMethod.String()

	acceptIMSI, _ := rfc2865.UserName_LookupString(r.Packet)
	if acceptIMSI != "" && methodStr != "" {
		s.eapAuthResults.Store(acceptIMSI, methodStr)
	}

	logger.Info().
		Dur("latency_ms", time.Since(startTime)).
		Str("eap_method", methodStr).
		Msg("EAP Access-Accept sent")
}

func (s *Server) sendEAPReject(w radius.ResponseWriter, request *radius.Packet, eapIdentifier uint8) {
	reject := request.Response(radius.CodeAccessReject)
	failPkt := eap.NewFailure(eapIdentifier)
	rfc2869.EAPMessage_Set(reject, eap.Encode(failPkt))
	if err := w.Write(reject); err != nil {
		s.logger.Error().Err(err).Msg("failed to send EAP Access-Reject")
	}
}

func (s *Server) handleDirectAuth(ctx context.Context, w radius.ResponseWriter, r *radius.Request, logger zerolog.Logger, startTime time.Time) {
	imsi, err := rfc2865.UserName_LookupString(r.Packet)
	if err != nil || imsi == "" {
		logger.Warn().Msg("Access-Request missing User-Name (IMSI)")
		s.sendReject(w, r.Packet, "MISSING_IMSI")
		return
	}

	logger = logger.With().Str("imsi", imsi).Logger()

	sim, err := s.simCache.GetByIMSI(ctx, imsi)
	if err != nil {
		if err == store.ErrSIMNotFound {
			logger.Info().Msg("Access-Reject: SIM not found")
			s.sendReject(w, r.Packet, "SIM_NOT_FOUND")
			return
		}
		logger.Error().Err(err).Msg("SIM lookup failed")
		s.sendReject(w, r.Packet, "INTERNAL_ERROR")
		return
	}

	if sim.State != "active" {
		reason := fmt.Sprintf("SIM_%s", sim.State)
		logger.Info().Str("sim_state", sim.State).Msg("Access-Reject: SIM not active")
		s.sendReject(w, r.Packet, reason)
		s.recordAuthMetric(ctx, sim.OperatorID, false, startTime)
		return
	}

	op, err := s.operatorStore.GetByID(ctx, sim.OperatorID)
	if err != nil {
		logger.Error().Err(err).Msg("operator lookup failed")
		s.sendReject(w, r.Packet, "INTERNAL_ERROR")
		s.recordAuthMetric(ctx, sim.OperatorID, false, startTime)
		return
	}

	if op.HealthStatus == "down" {
		logger.Info().Str("operator", op.Code).Msg("Access-Reject: operator unavailable")
		s.sendReject(w, r.Packet, "OPERATOR_UNAVAILABLE")
		s.recordAuthMetric(ctx, op.ID, false, startTime)
		return
	}

	sessionTimeout := sim.SessionHardTimeoutSec
	if sessionTimeout <= 0 {
		sessionTimeout = 86400
	}
	idleTimeout := sim.SessionIdleTimeoutSec
	if idleTimeout <= 0 {
		idleTimeout = 3600
	}
	filterID := "default"
	var bandwidthDown, bandwidthUp int64

	if s.policyEnforcer != nil && sim.PolicyVersionID != nil {
		ratTypeStr := extract3GPPRATType(r.Packet)
		now := time.Now()
		sessCtx := dsl.SessionContext{
			SIMID:    sim.ID.String(),
			TenantID: sim.TenantID.String(),
			Operator: op.Code,
			RATType:  ratTypeStr,
			SimType:  sim.SimType,
			TimeOfDay: now.Format("15:04"),
			DayOfWeek: now.Weekday().String(),
		}
		if sim.APNID != nil {
			sessCtx.APN = sim.APNID.String()
		}

		policyResult, err := s.policyEnforcer.Evaluate(ctx, sim, sessCtx)
		if err != nil {
			logger.Warn().Err(err).Msg("policy evaluation failed, proceeding with defaults")
		} else {
			if !policyResult.Allow {
				logger.Info().
					Str("sim_id", sim.ID.String()).
					Str("policy_version", policyResult.VersionID.String()).
					Msg("Access-Reject: policy denied")
				s.sendReject(w, r.Packet, "POLICY_DENIED")
				s.recordAuthMetric(ctx, op.ID, false, startTime)
				go s.policyEnforcer.RecordViolations(ctx, sim, policyResult, nil)
				return
			}
			sessionTimeout = policyResult.SessionTimeout
			idleTimeout = policyResult.IdleTimeout
			filterID = policyResult.FilterID
			bandwidthDown = policyResult.BandwidthDown
			bandwidthUp = policyResult.BandwidthUp

			if len(policyResult.Violations) > 0 {
				go s.policyEnforcer.RecordViolations(ctx, sim, policyResult, nil)
			}
		}
	}

	accept := r.Packet.Response(radius.CodeAccessAccept)

	if sim.IPAddressID != nil {
		ipAddr, err := s.ipPoolStore.GetIPAddressByID(ctx, *sim.IPAddressID)
		if err == nil && ipAddr.AddressV4 != nil {
			if ip := parseV4Address(*ipAddr.AddressV4); ip != nil {
				rfc2865.FramedIPAddress_Set(accept, ip.To4())
			}
		}
	}

	rfc2865.SessionTimeout_Set(accept, rfc2865.SessionTimeout(sessionTimeout))
	rfc2865.IdleTimeout_Set(accept, rfc2865.IdleTimeout(idleTimeout))
	rfc2865.FilterID_SetString(accept, filterID)

	if bandwidthDown > 0 {
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(bandwidthDown))
		accept.Add(radius.Type(11), buf)
	}
	if bandwidthUp > 0 {
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(bandwidthUp))
		accept.Add(radius.Type(12), buf)
	}

	if err := w.Write(accept); err != nil {
		logger.Error().Err(err).Msg("failed to send Access-Accept")
		return
	}

	s.recordAuthMetric(ctx, op.ID, true, startTime)

	logger.Info().
		Dur("latency_ms", time.Since(startTime)).
		Str("sim_id", sim.ID.String()).
		Msg("Access-Accept sent")
}

func (s *Server) handleAcct(w radius.ResponseWriter, r *radius.Request) {
	ctx := r.Context()
	correlationID := fmt.Sprintf("%s:%d", r.RemoteAddr.String(), r.Packet.Identifier)

	logger := s.logger.With().
		Str("correlation_id", correlationID).
		Str("remote_addr", r.RemoteAddr.String()).
		Str("type", "acct").
		Logger()

	statusType, err := rfc2866.AcctStatusType_Lookup(r.Packet)
	if err != nil {
		logger.Warn().Msg("Accounting-Request missing Acct-Status-Type")
		return
	}

	acctSessionID, _ := rfc2866.AcctSessionID_LookupString(r.Packet)
	imsi, _ := rfc2865.UserName_LookupString(r.Packet)

	logger = logger.With().
		Str("acct_session_id", acctSessionID).
		Str("imsi", imsi).
		Str("status_type", statusType.String()).
		Logger()

	switch statusType {
	case rfc2866.AcctStatusType_Value_Start:
		s.handleAcctStart(ctx, r, acctSessionID, imsi, logger)
	case rfc2866.AcctStatusType_Value_InterimUpdate:
		s.handleAcctInterim(ctx, r, acctSessionID, logger)
	case rfc2866.AcctStatusType_Value_Stop:
		s.handleAcctStop(ctx, r, acctSessionID, logger)
	default:
		logger.Warn().Uint32("status_type_val", uint32(statusType)).Msg("unknown Acct-Status-Type")
	}

	response := r.Packet.Response(radius.CodeAccountingResponse)
	if err := w.Write(response); err != nil {
		logger.Error().Err(err).Msg("failed to send Accounting-Response")
	}
}

func (s *Server) handleAcctStart(ctx context.Context, r *radius.Request, acctSessionID, imsi string, logger zerolog.Logger) {
	sim, err := s.simCache.GetByIMSI(ctx, imsi)
	if err != nil {
		logger.Error().Err(err).Msg("SIM lookup failed during Acct-Start")
		return
	}

	if sim.MaxConcurrentSessions > 0 {
		allowed, oldest, checkErr := s.sessionMgr.CheckConcurrentLimit(ctx, sim.ID.String(), sim.MaxConcurrentSessions)
		if checkErr != nil {
			logger.Warn().Err(checkErr).Msg("concurrent session check failed")
		} else if !allowed && oldest != nil {
			logger.Info().
				Str("sim_id", sim.ID.String()).
				Str("evicted_session_id", oldest.ID).
				Int("max_sessions", sim.MaxConcurrentSessions).
				Msg("concurrent session limit reached, evicting oldest")

			if s.dmSender != nil && oldest.NASIP != "" && oldest.AcctSessionID != "" {
				_, _ = s.dmSender.SendDM(ctx, session.DMRequest{
					NASIP:         oldest.NASIP,
					AcctSessionID: oldest.AcctSessionID,
					IMSI:          oldest.IMSI,
				})
			}
			_ = s.sessionMgr.Terminate(ctx, oldest.ID, "concurrent_limit")

			if s.eventBus != nil {
				_ = s.eventBus.Publish(ctx, bus.SubjectSessionEnded, map[string]interface{}{
					"session_id":      oldest.ID,
					"sim_id":          oldest.SimID,
					"tenant_id":       oldest.TenantID,
					"operator_id":     oldest.OperatorID,
					"imsi":            oldest.IMSI,
					"terminate_cause": "concurrent_limit",
					"ended_at":        time.Now().UTC().Format(time.RFC3339),
				})
			}
		}
	}

	nasIP := ""
	if ip, err := rfc2865.NASIPAddress_Lookup(r.Packet); err == nil {
		nasIP = ip.String()
	}

	framedIP := ""
	if ip, err := rfc2865.FramedIPAddress_Lookup(r.Packet); err == nil {
		framedIP = ip.String()
	}

	var authMethod string
	if v, ok := s.eapAuthResults.LoadAndDelete(imsi); ok {
		authMethod, _ = v.(string)
	}

	ratTypeStr := extract3GPPRATType(r.Packet)

	sess := &session.Session{
		ID:             uuid.New().String(),
		SimID:          sim.ID.String(),
		TenantID:       sim.TenantID.String(),
		OperatorID:     sim.OperatorID.String(),
		IMSI:           imsi,
		APN:            "",
		NASIP:          nasIP,
		AcctSessionID:  acctSessionID,
		FramedIP:       framedIP,
		SessionState:   "active",
		AuthMethod:     authMethod,
		RATType:        ratTypeStr,
		SessionTimeout: sim.SessionHardTimeoutSec,
		IdleTimeout:    sim.SessionIdleTimeoutSec,
		BytesIn:        0,
		BytesOut:       0,
		StartedAt:      time.Now().UTC(),
		LastInterimAt:  time.Now().UTC(),
	}

	if sim.APNID != nil {
		sess.APNID = sim.APNID.String()
	}
	if sim.MSISDN != nil {
		sess.MSISDN = *sim.MSISDN
	}

	if err := s.sessionMgr.Create(ctx, sess); err != nil {
		logger.Error().Err(err).Msg("failed to create session")
		return
	}

	if s.eventBus != nil {
		payload := map[string]interface{}{
			"session_id":  sess.ID,
			"sim_id":      sess.SimID,
			"tenant_id":   sess.TenantID,
			"operator_id": sess.OperatorID,
			"imsi":        imsi,
			"nas_ip":      nasIP,
			"framed_ip":   framedIP,
			"rat_type":    sess.RATType,
			"started_at":  sess.StartedAt.Format(time.RFC3339),
		}
		if err := s.eventBus.Publish(ctx, bus.SubjectSessionStarted, payload); err != nil {
			logger.Warn().Err(err).Msg("failed to publish session.started event")
		}
	}

	logger.Info().Str("session_id", sess.ID).Str("rat_type", sess.RATType).Msg("session started")
}

func (s *Server) handleAcctInterim(ctx context.Context, r *radius.Request, acctSessionID string, logger zerolog.Logger) {
	sess, err := s.sessionMgr.GetByAcctSessionID(ctx, acctSessionID)
	if err != nil {
		logger.Warn().Err(err).Msg("session not found for interim update")
		return
	}

	bytesIn := uint64(rfc2866.AcctInputOctets_Get(r.Packet))
	bytesOut := uint64(rfc2866.AcctOutputOctets_Get(r.Packet))

	if err := s.sessionMgr.UpdateCounters(ctx, sess.ID, bytesIn, bytesOut); err != nil {
		logger.Error().Err(err).Msg("failed to update session counters")
		return
	}

	if s.eventBus != nil {
		payload := map[string]interface{}{
			"session_id":  sess.ID,
			"sim_id":      sess.SimID,
			"tenant_id":   sess.TenantID,
			"operator_id": sess.OperatorID,
			"imsi":        sess.IMSI,
			"bytes_in":    bytesIn,
			"bytes_out":   bytesOut,
			"updated_at":  time.Now().Format(time.RFC3339),
		}
		if err := s.eventBus.Publish(ctx, bus.SubjectSessionUpdated, payload); err != nil {
			logger.Warn().Err(err).Msg("failed to publish session.updated event")
		}
	}

	if s.policyEnforcer != nil && sess.SimID != "" {
		simID, parseErr := uuid.Parse(sess.SimID)
		if parseErr == nil {
			sim, simErr := s.simCache.GetByIMSI(ctx, sess.IMSI)
			if simErr == nil && sim != nil && sim.PolicyVersionID != nil {
				totalUsage := int64(bytesIn + bytesOut)
				result, evalErr := s.policyEnforcer.RecordUsageCheck(ctx, sim, totalUsage)
				if evalErr == nil && result != nil && !result.Allow {
					logger.Info().
						Str("sim_id", simID.String()).
						Int64("usage_bytes", totalUsage).
						Msg("policy quota exceeded, disconnecting session")

					if s.dmSender != nil && sess.NASIP != "" && sess.AcctSessionID != "" {
						_, _ = s.dmSender.SendDM(ctx, session.DMRequest{
							NASIP:         sess.NASIP,
							AcctSessionID: sess.AcctSessionID,
							IMSI:          sess.IMSI,
						})
					}
					_ = s.sessionMgr.Terminate(ctx, sess.ID, "policy_quota_exceeded")

					sessUUID, _ := uuid.Parse(sess.ID)
					go s.policyEnforcer.RecordViolations(ctx, sim, result, &sessUUID)

					if s.eventBus != nil {
						_ = s.eventBus.Publish(ctx, bus.SubjectSessionEnded, map[string]interface{}{
							"session_id":      sess.ID,
							"sim_id":          sess.SimID,
							"tenant_id":       sess.TenantID,
							"operator_id":     sess.OperatorID,
							"imsi":            sess.IMSI,
							"terminate_cause": "policy_quota_exceeded",
							"ended_at":        time.Now().UTC().Format(time.RFC3339),
						})
					}
				}
			}
		}
	}

	logger.Debug().
		Str("session_id", sess.ID).
		Uint64("bytes_in", bytesIn).
		Uint64("bytes_out", bytesOut).
		Msg("session interim update")
}

func (s *Server) handleAcctStop(ctx context.Context, r *radius.Request, acctSessionID string, logger zerolog.Logger) {
	sess, err := s.sessionMgr.GetByAcctSessionID(ctx, acctSessionID)
	if err != nil {
		logger.Warn().Err(err).Msg("session not found for stop")
		return
	}

	bytesIn := uint64(rfc2866.AcctInputOctets_Get(r.Packet))
	bytesOut := uint64(rfc2866.AcctOutputOctets_Get(r.Packet))

	terminateCause := "user_request"
	if cause, err := rfc2866.AcctTerminateCause_Lookup(r.Packet); err == nil {
		terminateCause = cause.String()
	}

	if err := s.sessionMgr.TerminateWithCounters(ctx, sess.ID, terminateCause, bytesIn, bytesOut); err != nil {
		logger.Error().Err(err).Msg("failed to terminate session")
		return
	}

	if s.eventBus != nil {
		payload := map[string]interface{}{
			"session_id":      sess.ID,
			"sim_id":          sess.SimID,
			"tenant_id":       sess.TenantID,
			"operator_id":     sess.OperatorID,
			"imsi":            sess.IMSI,
			"terminate_cause": terminateCause,
			"bytes_in":        bytesIn,
			"bytes_out":       bytesOut,
			"ended_at":        time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.eventBus.Publish(ctx, bus.SubjectSessionEnded, payload); err != nil {
			logger.Warn().Err(err).Msg("failed to publish session.ended event")
		}
	}

	logger.Info().
		Str("session_id", sess.ID).
		Str("terminate_cause", terminateCause).
		Uint64("bytes_in", bytesIn).
		Uint64("bytes_out", bytesOut).
		Msg("session stopped")
}

func (s *Server) sendReject(w radius.ResponseWriter, request *radius.Packet, reason string) {
	reject := request.Response(radius.CodeAccessReject)
	rfc2865.ReplyMessage_SetString(reject, reason)
	if err := w.Write(reject); err != nil {
		s.logger.Error().Err(err).Str("reason", reason).Msg("failed to send Access-Reject")
	}
}

func (s *Server) getOperatorSecret(op *store.Operator) []byte {
	if op != nil && len(op.AdapterConfig) > 0 {
		var cfg map[string]interface{}
		if err := json.Unmarshal(op.AdapterConfig, &cfg); err == nil {
			if secret, ok := cfg["radius_secret"].(string); ok && secret != "" {
				return []byte(secret)
			}
		}
	}
	return []byte(s.defaultSecret)
}

const (
	vendorID3GPP         uint32 = 10415
	vendorType3GPPRATType uint8 = 21
)

func extract3GPPRATType(pkt *radius.Packet) string {
	vsaAttr, ok := pkt.Lookup(radius.Type(26))
	if !ok {
		return ""
	}
	raw := []byte(vsaAttr)
	if len(raw) < 7 {
		return ""
	}

	vendorID := binary.BigEndian.Uint32(raw[0:4])
	if vendorID != vendorID3GPP {
		return ""
	}

	vendorType := raw[4]
	if vendorType != vendorType3GPPRATType {
		return ""
	}

	vendorLen := int(raw[5])
	if vendorLen < 3 || 4+vendorLen > len(raw) {
		return ""
	}

	valueBytes := raw[6 : 4+vendorLen]
	if len(valueBytes) == 0 {
		return ""
	}

	var ratVal uint8
	if len(valueBytes) >= 4 {
		ratVal = uint8(binary.BigEndian.Uint32(valueBytes))
	} else {
		ratVal = valueBytes[len(valueBytes)-1]
	}

	return rattype.FromRADIUS(ratVal)
}
