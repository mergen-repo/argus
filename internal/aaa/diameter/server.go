package diameter

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type ServerConfig struct {
	Port             int
	OriginHost       string
	OriginRealm      string
	VendorID         uint32
	WatchdogInterval time.Duration
	ProductName      string
}

type ServerDeps struct {
	SessionMgr  *session.Manager
	EventBus    *bus.EventBus
	SIMResolver SIMResolver
	// IPPoolStore and SIMStore are used by the Gx handler for Framed-IP-Address
	// allocation on CCR-I and release on CCR-T (STORY-092 Wave 2). Optional —
	// when either is nil the handler skips IP handling and the CCA-I is built
	// without a Framed-IP-Address AVP (matches pre-STORY-092 behaviour).
	IPPoolStore *store.IPPoolStore
	SIMStore    *store.SIMStore
	// MetricsReg forwards the Prometheus registry into the S6a handler so the
	// argus_imei_capture_parse_errors_total{protocol="diameter_s6a"} counter
	// fires on malformed Terminal-Information AVP input (STORY-094 D-182).
	// Nil-safe: when absent the counter is not incremented (dev/test builds).
	MetricsReg *obsmetrics.Registry
	Logger     zerolog.Logger

	// BindingGate is the STORY-096 IMEI/SIM binding pre-check gate forwarded
	// to the S6a handler via WithBindingGate. Nil (the default) preserves
	// pre-STORY-096 behaviour on ULR/NTR (AC-17). The handler also requires
	// SIMResolver (already on this struct) to look up the SIM view by IMSI.
	BindingGate BindingGate
}

type PeerState int

const (
	PeerStateIdle PeerState = iota
	PeerStateOpen
	PeerStateClosing
	PeerStateClosed
)

func (ps PeerState) String() string {
	switch ps {
	case PeerStateIdle:
		return "idle"
	case PeerStateOpen:
		return "open"
	case PeerStateClosing:
		return "closing"
	case PeerStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

type Peer struct {
	mu           sync.Mutex
	conn         net.Conn
	originHost   string
	originRealm  string
	state        PeerState
	lastActivity time.Time
	apps         map[uint32]bool
}

func (p *Peer) SetState(s PeerState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = s
}

func (p *Peer) GetState() PeerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *Peer) Touch() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastActivity = time.Now()
}

func (p *Peer) LastActivity() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastActivity
}

type Server struct {
	cfg        ServerConfig
	sessionMgr *session.Manager
	eventBus   *bus.EventBus
	logger     zerolog.Logger

	gxHandler  *GxHandler
	gyHandler  *GyHandler
	s6aHandler *S6aHandler
	stateMap   *SessionStateMap

	listener net.Listener
	peers    sync.Map
	hopID    atomic.Uint32
	endID    atomic.Uint32

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func NewServer(cfg ServerConfig, deps ServerDeps) *Server {
	if cfg.ProductName == "" {
		cfg.ProductName = "Argus"
	}
	if cfg.WatchdogInterval == 0 {
		cfg.WatchdogInterval = 30 * time.Second
	}
	if cfg.VendorID == 0 {
		cfg.VendorID = 99999
	}

	logger := deps.Logger.With().Str("component", "diameter_server").Logger()

	stateMap := NewSessionStateMap()

	s := &Server{
		cfg:        cfg,
		sessionMgr: deps.SessionMgr,
		eventBus:   deps.EventBus,
		logger:     logger,
		stateMap:   stateMap,
		stopCh:     make(chan struct{}),
	}

	s.gxHandler = NewGxHandler(deps.SessionMgr, deps.EventBus, deps.SIMResolver, deps.IPPoolStore, deps.SIMStore, stateMap, logger)
	s.gyHandler = NewGyHandler(deps.SessionMgr, deps.EventBus, deps.SIMResolver, stateMap, logger)
	// STORY-096 Task 7: forward the binding gate + SIM resolver into the S6a
	// handler. Both are options on the handler — nil values are no-ops, so
	// pre-STORY-096 behaviour is unchanged when either is absent (AC-17).
	s6aOpts := []S6aOption{}
	if deps.BindingGate != nil {
		s6aOpts = append(s6aOpts, WithBindingGate(deps.BindingGate))
	}
	if deps.SIMResolver != nil {
		s6aOpts = append(s6aOpts, WithS6aSIMResolver(deps.SIMResolver))
	}
	s.s6aHandler = NewS6aHandler(deps.SessionMgr, deps.EventBus, deps.MetricsReg, logger, s6aOpts...)

	s.hopID.Store(uint32(time.Now().UnixNano() & 0xFFFFFFFF))
	s.endID.Store(uint32(time.Now().UnixNano()>>32) & 0xFFFFFFFF)

	return s
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("diameter server already running")
	}

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen tcp %s: %w", addr, err)
	}
	s.listener = ln
	s.running = true

	s.wg.Add(1)
	go s.acceptLoop()

	s.logger.Info().
		Int("port", s.cfg.Port).
		Str("origin_host", s.cfg.OriginHost).
		Str("origin_realm", s.cfg.OriginRealm).
		Msg("diameter server started")

	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	if s.listener != nil {
		s.listener.Close()
	}

	s.peers.Range(func(key, value interface{}) bool {
		peer := value.(*Peer)
		peer.SetState(PeerStateClosed)
		peer.conn.Close()
		return true
	})

	s.wg.Wait()
	s.logger.Info().Msg("diameter server stopped")
}

func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Server) Healthy() bool {
	return s.IsRunning()
}

func (s *Server) ActiveSessionCount(ctx context.Context) (int64, error) {
	if s.sessionMgr != nil {
		return s.sessionMgr.CountActive(ctx)
	}
	return int64(s.stateMap.ActiveCount()), nil
}

func (s *Server) HealthCheck() error {
	if !s.IsRunning() {
		return fmt.Errorf("diameter server not running")
	}
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("diameter port unreachable: %w", err)
	}
	conn.Close()
	return nil
}

func (s *Server) PeerCount() int {
	count := 0
	s.peers.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

func (s *Server) SessionManager() *session.Manager {
	return s.sessionMgr
}

func (s *Server) SessionStateMap() *SessionStateMap {
	return s.stateMap
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				s.logger.Error().Err(err).Msg("accept error")
				continue
			}
		}
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	peer := &Peer{
		conn:         conn,
		state:        PeerStateIdle,
		lastActivity: time.Now(),
		apps:         make(map[uint32]bool),
	}

	s.peers.Store(remoteAddr, peer)
	defer s.peers.Delete(remoteAddr)

	s.logger.Debug().Str("remote", remoteAddr).Msg("new diameter connection")

	watchdogDone := make(chan struct{})
	go s.watchdogLoop(peer, watchdogDone)
	defer close(watchdogDone)

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(s.cfg.WatchdogInterval * 3))

		headerBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, headerBuf); err != nil {
			if err != io.EOF {
				select {
				case <-s.stopCh:
				default:
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						s.logger.Warn().Str("remote", remoteAddr).Msg("peer watchdog timeout, closing")
					} else {
						s.logger.Debug().Err(err).Str("remote", remoteAddr).Msg("connection read error")
					}
				}
			}
			return
		}

		msgLen, err := ReadMessageLength(headerBuf)
		if err != nil {
			s.logger.Warn().Err(err).Str("remote", remoteAddr).Msg("invalid message header")
			return
		}

		msgBuf := make([]byte, msgLen)
		copy(msgBuf[:4], headerBuf)
		if _, err := io.ReadFull(conn, msgBuf[4:]); err != nil {
			s.logger.Warn().Err(err).Str("remote", remoteAddr).Msg("incomplete message")
			return
		}

		msg, err := DecodeMessage(msgBuf)
		if err != nil {
			s.logger.Warn().Err(err).Str("remote", remoteAddr).Msg("failed to decode message")
			continue
		}

		peer.Touch()
		s.handleMessage(peer, msg)
	}
}

func (s *Server) handleMessage(peer *Peer, msg *Message) {
	if !msg.IsRequest() {
		s.logger.Debug().
			Uint32("cmd", msg.CommandCode).
			Uint32("app_id", msg.ApplicationID).
			Msg("received answer (ignored)")
		return
	}

	switch msg.CommandCode {
	case CommandCER:
		s.handleCER(peer, msg)
	case CommandDWR:
		s.handleDWR(peer, msg)
	case CommandDPR:
		s.handleDPR(peer, msg)
	case CommandCCR:
		s.handleCCR(peer, msg)
	case CommandRAR:
		s.logger.Debug().Msg("received RAR — not handled as server")
	case CommandULR:
		ans := s.s6aHandler.HandleULR(msg)
		s.addOriginAVPs(ans)
		s.sendMessage(peer, ans)
	case CommandNTR:
		ans := s.s6aHandler.HandleNTR(msg)
		s.addOriginAVPs(ans)
		s.sendMessage(peer, ans)
	default:
		s.logger.Warn().
			Uint32("cmd", msg.CommandCode).
			Msg("unknown command code")
		ans := NewErrorAnswer(msg, ResultCodeUnableToComply)
		s.addOriginAVPs(ans)
		s.sendMessage(peer, ans)
	}
}

func (s *Server) handleCER(peer *Peer, msg *Message) {
	peer.originHost = msg.GetOriginHost()
	peer.originRealm = msg.GetOriginRealm()

	for _, a := range msg.AVPs {
		if a.Code == AVPCodeAuthApplicationID || a.Code == AVPCodeAcctApplicationID {
			appID, err := a.GetUint32()
			if err == nil {
				peer.apps[appID] = true
			}
		}
	}

	cea := NewAnswer(msg)
	cea.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	s.addOriginAVPs(cea)
	cea.AddAVP(NewAVPAddress(AVPCodeHostIPAddress, AVPFlagMandatory, 0, [4]byte{0, 0, 0, 0}))
	cea.AddAVP(NewAVPUint32(AVPCodeVendorID, AVPFlagMandatory, 0, s.cfg.VendorID))
	cea.AddAVP(NewAVPString(AVPCodeProductName, 0, 0, s.cfg.ProductName))
	cea.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	cea.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	cea.AddAVP(NewAVPUint32(AVPCodeSupportedVendorID, AVPFlagMandatory, 0, VendorID3GPP))
	cea.AddAVP(NewAVPUint32(AVPCodeFirmwareRevision, 0, 0, 1))

	if err := s.sendMessage(peer, cea); err != nil {
		s.logger.Error().Err(err).Str("peer", peer.originHost).Msg("failed to send CEA")
		return
	}

	peer.SetState(PeerStateOpen)
	s.logger.Info().
		Str("peer_host", peer.originHost).
		Str("peer_realm", peer.originRealm).
		Int("apps", len(peer.apps)).
		Msg("CER/CEA exchange complete, peer open")
}

func (s *Server) handleDWR(peer *Peer, msg *Message) {
	dwa := NewAnswer(msg)
	dwa.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	s.addOriginAVPs(dwa)

	if err := s.sendMessage(peer, dwa); err != nil {
		s.logger.Error().Err(err).Str("peer", peer.originHost).Msg("failed to send DWA")
	}
}

func (s *Server) handleDPR(peer *Peer, msg *Message) {
	peer.SetState(PeerStateClosing)

	dpa := NewAnswer(msg)
	dpa.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	s.addOriginAVPs(dpa)

	if err := s.sendMessage(peer, dpa); err != nil {
		s.logger.Error().Err(err).Str("peer", peer.originHost).Msg("failed to send DPA")
	}

	s.logger.Info().Str("peer_host", peer.originHost).Msg("peer disconnecting (DPR/DPA)")
	peer.SetState(PeerStateClosed)
	peer.conn.Close()
}

func (s *Server) handleCCR(peer *Peer, msg *Message) {
	if peer.GetState() != PeerStateOpen {
		s.logger.Warn().
			Str("peer", peer.originHost).
			Str("state", peer.GetState().String()).
			Msg("CCR received on non-open peer")
		ans := NewErrorAnswer(msg, ResultCodeUnableToComply)
		s.addOriginAVPs(ans)
		s.sendMessage(peer, ans)
		return
	}

	switch msg.ApplicationID {
	case ApplicationIDGx:
		ans := s.gxHandler.HandleCCR(msg)
		s.addOriginAVPs(ans)
		s.sendMessage(peer, ans)
	case ApplicationIDGy:
		ans := s.gyHandler.HandleCCR(msg)
		s.addOriginAVPs(ans)
		s.sendMessage(peer, ans)
	default:
		s.logger.Warn().
			Uint32("app_id", msg.ApplicationID).
			Msg("unsupported application ID in CCR")
		ans := NewErrorAnswer(msg, ResultCodeApplicationUnsupported)
		s.addOriginAVPs(ans)
		s.sendMessage(peer, ans)
	}
}

func (s *Server) addOriginAVPs(msg *Message) {
	msg.AddAVP(NewAVPString(AVPCodeOriginHost, AVPFlagMandatory, 0, s.cfg.OriginHost))
	msg.AddAVP(NewAVPString(AVPCodeOriginRealm, AVPFlagMandatory, 0, s.cfg.OriginRealm))
}

func (s *Server) sendMessage(peer *Peer, msg *Message) error {
	data, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}

	peer.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := peer.conn.Write(data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

func (s *Server) watchdogLoop(peer *Peer, done chan struct{}) {
	ticker := time.NewTicker(s.cfg.WatchdogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			if peer.GetState() != PeerStateOpen {
				continue
			}
			idle := time.Since(peer.LastActivity())
			if idle > s.cfg.WatchdogInterval*3 {
				s.logger.Warn().
					Str("peer", peer.originHost).
					Dur("idle", idle).
					Msg("peer watchdog timeout")
				peer.SetState(PeerStateClosed)
				peer.conn.Close()

				if s.eventBus != nil {
					// FIX-212 AC-2: migrate raw-map publish to canonical bus.Envelope.
					// Watchdog-timeout events are infra-global (no owning tenant),
					// so tenant_id=SystemTenantID per D5 (publisher-authored
					// sentinel). The peer remains the primary entity; the
					// originRealm travels via meta.
					env := bus.NewEnvelope("operator.health_changed", bus.SystemTenantID.String(), "high").
						WithSource("operator").
						WithTitle(fmt.Sprintf("Diameter peer %s watchdog timeout", peer.originHost)).
						WithMessage("Peer marked DOWN after exceeding watchdog interval").
						SetEntity("operator", peer.originHost, peer.originHost).
						WithMeta("peer_host", peer.originHost).
						WithMeta("peer_realm", peer.originRealm).
						WithMeta("current_status", "down").
						WithMeta("previous_status", "up").
						WithMeta("reason", "watchdog_timeout")
					if pubErr := s.eventBus.Publish(context.Background(), bus.SubjectOperatorHealthChanged, env); pubErr != nil {
						s.logger.Warn().Err(pubErr).Str("peer", peer.originHost).Msg("publish peer watchdog timeout event failed")
					}
				}
				return
			}
		}
	}
}

func (s *Server) SendRAR(peerHost, sessionID string, avps []*AVP) error {
	var targetPeer *Peer
	s.peers.Range(func(_, value interface{}) bool {
		p := value.(*Peer)
		if p.originHost == peerHost && p.GetState() == PeerStateOpen {
			targetPeer = p
			return false
		}
		return true
	})

	if targetPeer == nil {
		return fmt.Errorf("peer not found or not open: %s", peerHost)
	}

	rar := NewRequest(CommandRAR, ApplicationIDGx, s.nextHopID(), s.nextEndID())
	rar.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	s.addOriginAVPs(rar)
	rar.AddAVP(NewAVPString(AVPCodeDestinationHost, AVPFlagMandatory, 0, targetPeer.originHost))
	rar.AddAVP(NewAVPString(AVPCodeDestinationRealm, AVPFlagMandatory, 0, targetPeer.originRealm))
	rar.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))
	for _, a := range avps {
		rar.AddAVP(a)
	}

	return s.sendMessage(targetPeer, rar)
}

func (s *Server) nextHopID() uint32 {
	return s.hopID.Add(1)
}

func (s *Server) nextEndID() uint32 {
	return s.endID.Add(1)
}
