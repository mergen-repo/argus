package diameter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
	"github.com/rs/zerolog"
)

// PeerState enumerates the states of the simulator's Diameter client peer.
// The numeric values intentionally match the DiameterPeerState gauge:
// 0=Closed, 1=Connecting, 2=WaitCEA, 3=Open, 4=Closing.
type PeerState int32

const (
	PeerStateClosed     PeerState = 0
	PeerStateConnecting PeerState = 1
	PeerStateWaitCEA    PeerState = 2
	PeerStateOpen       PeerState = 3
	PeerStateClosing    PeerState = 4
)

func (ps PeerState) String() string {
	switch ps {
	case PeerStateClosed:
		return "Closed"
	case PeerStateConnecting:
		return "Connecting"
	case PeerStateWaitCEA:
		return "WaitCEA"
	case PeerStateOpen:
		return "Open"
	case PeerStateClosing:
		return "Closing"
	default:
		return "Unknown"
	}
}

var (
	ErrPeerNotOpen = errors.New("diameter: peer not open")
	ErrTimeout     = errors.New("diameter: request timed out")
)

// pendingReq holds the reply channel for an in-flight request correlated
// by Hop-by-Hop ID.
type pendingReq struct {
	ch chan *argusdiameter.Message
}

// PeerConfig holds all constructor parameters for a Peer.
type PeerConfig struct {
	OperatorCode        string
	Host                string
	Port                int
	OriginHost          string
	OriginRealm         string
	DestinationRealm    string
	AppIDs              []uint32
	WatchdogInterval    time.Duration
	ConnectTimeout      time.Duration
	RequestTimeout      time.Duration
	ReconnectBackoffMin time.Duration
	ReconnectBackoffMax time.Duration
	OnStateChange       func(PeerState) // optional; called on every state transition
}

// Peer manages one TCP connection to the Argus Diameter server for a single
// operator. It drives the state machine: Closed → Connecting → WaitCEA →
// Open → (Closing) → Closed, and reconnects with exponential backoff on
// transport failure.
type Peer struct {
	cfg    PeerConfig
	logger zerolog.Logger

	state   atomic.Int32
	hopID   atomic.Uint32
	endID   atomic.Uint32

	mu       sync.Mutex
	conn     net.Conn
	pending  map[uint32]*pendingReq

	dwrMu        sync.Mutex
	dwrInFlight  bool

	closeCh  chan struct{}
	closeOnce sync.Once
}

// NewPeer creates a Peer but does NOT connect. Call Run(ctx) to start.
func NewPeer(cfg PeerConfig, logger zerolog.Logger) *Peer {
	if cfg.WatchdogInterval == 0 {
		cfg.WatchdogInterval = 30 * time.Second
	}
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 5 * time.Second
	}
	if cfg.ReconnectBackoffMin == 0 {
		cfg.ReconnectBackoffMin = time.Second
	}
	if cfg.ReconnectBackoffMax == 0 {
		cfg.ReconnectBackoffMax = 30 * time.Second
	}

	p := &Peer{
		cfg:     cfg,
		logger:  logger.With().Str("component", "diameter_peer").Str("operator", cfg.OperatorCode).Logger(),
		pending: make(map[uint32]*pendingReq),
		closeCh: make(chan struct{}),
	}
	p.state.Store(int32(PeerStateClosed))

	// Seed hop/end IDs from time similar to Argus server.go:139-140.
	now := time.Now().UnixNano()
	p.hopID.Store(uint32(now & 0xFFFFFFFF))
	p.endID.Store(uint32((now >> 32) & 0xFFFFFFFF))

	return p
}

// State returns the current peer state.
func (p *Peer) State() PeerState {
	return PeerState(p.state.Load())
}

// Run is the peer lifecycle goroutine. It connects, performs CER/CEA, enters
// Open, runs the watchdog, and reconnects on failure. It exits when ctx is
// cancelled or Close() is called.
func (p *Peer) Run(ctx context.Context) {
	backoff := p.cfg.ReconnectBackoffMin
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.closeCh:
			return
		default:
		}

		p.setState(PeerStateConnecting)
		conn, err := p.dial(ctx)
		if err != nil {
			p.logger.Warn().Err(err).Dur("backoff", backoff).Msg("dial failed, backing off")
			p.setState(PeerStateClosed)
			if !p.sleep(ctx, backoff) {
				return
			}
			backoff = minDuration(backoff*2, p.cfg.ReconnectBackoffMax)
			continue
		}

		p.mu.Lock()
		p.conn = conn
		p.mu.Unlock()

		p.setState(PeerStateWaitCEA)

		// Start read loop.
		readErrCh := make(chan error, 1)
		go p.readLoop(conn, readErrCh)

		// Send CER and wait for CEA.
		if err := p.doCER(ctx, conn); err != nil {
			p.logger.Warn().Err(err).Msg("CER/CEA failed")
			conn.Close()
			p.drainPending(err)
			p.setState(PeerStateClosed)
			if !p.sleep(ctx, backoff) {
				return
			}
			backoff = minDuration(backoff*2, p.cfg.ReconnectBackoffMax)
			continue
		}

		p.setState(PeerStateOpen)
		backoff = p.cfg.ReconnectBackoffMin

		p.logger.Info().
			Str("host", p.cfg.Host).
			Int("port", p.cfg.Port).
			Msg("peer open")

		// Watchdog loop. Blocks until connection dies or close/ctx.
		reason := p.watchdog(ctx, conn, readErrCh)

		conn.Close()
		p.drainPending(ErrPeerNotOpen)

		if reason == "close" || reason == "ctx" {
			p.setState(PeerStateClosed)
			return
		}

		p.logger.Info().Str("reason", reason).Dur("backoff", backoff).Msg("peer lost, reconnecting")
		p.setState(PeerStateClosed)
		if !p.sleep(ctx, backoff) {
			return
		}
		backoff = minDuration(backoff*2, p.cfg.ReconnectBackoffMax)
	}
}

// Send sends msg to the peer and waits for the correlated answer. Returns
// ErrPeerNotOpen if the peer is not in Open state.
func (p *Peer) Send(ctx context.Context, msg *argusdiameter.Message) (*argusdiameter.Message, error) {
	if p.State() != PeerStateOpen {
		return nil, ErrPeerNotOpen
	}

	hbh := p.hopID.Add(1)
	msg.HopByHopID = hbh

	ch := make(chan *argusdiameter.Message, 1)
	req := &pendingReq{ch: ch}

	p.mu.Lock()
	if p.State() != PeerStateOpen {
		p.mu.Unlock()
		return nil, ErrPeerNotOpen
	}
	conn := p.conn
	p.pending[hbh] = req
	p.mu.Unlock()

	if err := p.writeMessage(conn, msg); err != nil {
		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()
		return nil, fmt.Errorf("send: %w", err)
	}

	timeout := time.NewTimer(p.cfg.RequestTimeout)
	defer timeout.Stop()

	select {
	case reply := <-ch:
		return reply, nil
	case <-timeout.C:
		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()
		return nil, ErrTimeout
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Close gracefully disconnects the peer: sends DPR, waits for DPA (max 1s),
// then closes the connection.
//
// Ordering is significant: closeCh is closed ONLY after the DPR write (and
// optional DPA wait) complete. Closing closeCh earlier races Run's watchdog —
// which exits immediately on <-p.closeCh and calls conn.Close() — against
// the DPR write here, producing a broken-pipe flake under -race.
func (p *Peer) Close() {
	p.closeOnce.Do(func() {
		p.setState(PeerStateClosing)

		p.mu.Lock()
		conn := p.conn
		p.mu.Unlock()

		if conn == nil {
			close(p.closeCh)
			p.setState(PeerStateClosed)
			return
		}

		dpr := p.buildDPR()
		hbh := p.hopID.Add(1)
		dpr.HopByHopID = hbh

		ch := make(chan *argusdiameter.Message, 1)
		p.mu.Lock()
		p.pending[hbh] = &pendingReq{ch: ch}
		p.mu.Unlock()

		if err := p.writeMessage(conn, dpr); err != nil {
			p.logger.Warn().Err(err).Msg("failed to send DPR")
		} else {
			select {
			case <-ch:
			case <-time.After(time.Second):
				p.logger.Warn().Msg("DPA timeout")
			}
		}

		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()

		// Signal Run/watchdog to exit AFTER DPR write + DPA wait are done.
		// Run will then close conn itself; our conn.Close() below is
		// idempotent and protects against Run having already exited.
		close(p.closeCh)
		conn.Close()
		p.setState(PeerStateClosed)
	})
}

// nextEndID returns the next end-to-end ID.
func (p *Peer) nextEndID() uint32 {
	return p.endID.Add(1)
}

func (p *Peer) setState(s PeerState) {
	p.state.Store(int32(s))
	if p.cfg.OnStateChange != nil {
		p.cfg.OnStateChange(s)
	}
}

// NextEndID returns the next end-to-end ID for external callers (e.g. Client).
func (p *Peer) NextEndID() uint32 {
	return p.endID.Add(1)
}

func (p *Peer) dial(ctx context.Context) (net.Conn, error) {
	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)
	dialer := &net.Dialer{Timeout: p.cfg.ConnectTimeout}
	return dialer.DialContext(ctx, "tcp", addr)
}

// doCER sends a Capabilities-Exchange-Request and waits for CEA.
func (p *Peer) doCER(ctx context.Context, conn net.Conn) error {
	hbh := p.hopID.Add(1)
	cer := p.buildCER(hbh)

	ch := make(chan *argusdiameter.Message, 1)
	p.mu.Lock()
	p.pending[hbh] = &pendingReq{ch: ch}
	p.mu.Unlock()

	if err := p.writeMessage(conn, cer); err != nil {
		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()
		return fmt.Errorf("write CER: %w", err)
	}

	timeout := time.NewTimer(p.cfg.ConnectTimeout)
	defer timeout.Stop()

	select {
	case cea := <-ch:
		rc := cea.GetResultCode()
		if rc != argusdiameter.ResultCodeSuccess {
			return fmt.Errorf("CEA result-code %d", rc)
		}
		return nil
	case <-timeout.C:
		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()
		return fmt.Errorf("CEA timeout")
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()
		return ctx.Err()
	case <-p.closeCh:
		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()
		return errors.New("peer closed")
	}
}

// watchdog runs while the peer is Open. It sends DWR every WatchdogInterval
// and checks for DWA response. Returns reason string: "transport", "watchdog",
// "close", "ctx".
func (p *Peer) watchdog(ctx context.Context, conn net.Conn, readErrCh <-chan error) string {
	ticker := time.NewTicker(p.cfg.WatchdogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "ctx"

		case <-p.closeCh:
			return "close"

		case err := <-readErrCh:
			p.logger.Debug().Err(err).Msg("read loop exited")
			return "transport"

		case <-ticker.C:
			p.dwrMu.Lock()
			if p.dwrInFlight {
				p.dwrMu.Unlock()
				p.logger.Warn().Msg("DWR timeout — no DWA received")
				return "watchdog"
			}
			p.dwrInFlight = true
			p.dwrMu.Unlock()

			if err := p.sendDWR(ctx, conn); err != nil {
				p.logger.Warn().Err(err).Msg("DWR send failed")
				return "transport"
			}
		}
	}
}

// sendDWR sends a Device-Watchdog-Request. The DWA clears dwrInFlight via
// the read loop dispatch path.
func (p *Peer) sendDWR(ctx context.Context, conn net.Conn) error {
	hbh := p.hopID.Add(1)
	dwr := argusdiameter.NewRequest(argusdiameter.CommandDWR, argusdiameter.ApplicationIDDiameterBase, hbh, p.nextEndID())
	dwr.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, p.cfg.OriginHost))
	dwr.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, p.cfg.OriginRealm))

	ch := make(chan *argusdiameter.Message, 1)
	p.mu.Lock()
	p.pending[hbh] = &pendingReq{ch: ch}
	p.mu.Unlock()

	if err := p.writeMessage(conn, dwr); err != nil {
		p.mu.Lock()
		delete(p.pending, hbh)
		p.mu.Unlock()
		p.dwrMu.Lock()
		p.dwrInFlight = false
		p.dwrMu.Unlock()
		return err
	}

	// DWA is handled asynchronously in readLoop → dispatchAnswer which calls
	// handleDWA to clear dwrInFlight. We don't block here.
	_ = ch // ch will receive DWA; dispatchAnswer will also call handleDWA
	return nil
}

// readLoop reads Diameter messages from conn and dispatches answers to pending
// channels. It exits on any read error.
func (p *Peer) readLoop(conn net.Conn, errCh chan<- error) {
	for {
		headerBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, headerBuf); err != nil {
			errCh <- err
			return
		}

		msgLen, err := argusdiameter.ReadMessageLength(headerBuf)
		if err != nil {
			errCh <- fmt.Errorf("invalid header: %w", err)
			return
		}

		msgBuf := make([]byte, msgLen)
		copy(msgBuf[:4], headerBuf)
		if _, err := io.ReadFull(conn, msgBuf[4:]); err != nil {
			errCh <- err
			return
		}

		msg, err := argusdiameter.DecodeMessage(msgBuf)
		if err != nil {
			p.logger.Warn().Err(err).Msg("failed to decode message")
			continue
		}

		if msg.IsRequest() {
			// Handle server-initiated requests (e.g., DPR from server).
			p.handleInboundRequest(conn, msg)
		} else {
			p.dispatchAnswer(msg)
		}
	}
}

// dispatchAnswer routes an answer to the pending request channel matched by
// Hop-by-Hop ID. If the answer is a DWA, it also clears dwrInFlight.
func (p *Peer) dispatchAnswer(msg *argusdiameter.Message) {
	if msg.CommandCode == argusdiameter.CommandDWA {
		p.dwrMu.Lock()
		p.dwrInFlight = false
		p.dwrMu.Unlock()
	}

	p.mu.Lock()
	req, ok := p.pending[msg.HopByHopID]
	if ok {
		delete(p.pending, msg.HopByHopID)
	}
	p.mu.Unlock()

	if ok {
		select {
		case req.ch <- msg:
		default:
		}
	}
}

// handleInboundRequest handles server-initiated requests such as DPR.
func (p *Peer) handleInboundRequest(conn net.Conn, msg *argusdiameter.Message) {
	switch msg.CommandCode {
	case argusdiameter.CommandDPR:
		p.logger.Info().Msg("received DPR from server, sending DPA")
		dpa := argusdiameter.NewAnswer(msg)
		dpa.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ResultCodeSuccess))
		dpa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, p.cfg.OriginHost))
		dpa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, p.cfg.OriginRealm))
		if err := p.writeMessage(conn, dpa); err != nil {
			p.logger.Warn().Err(err).Msg("failed to send DPA")
		}
	default:
		p.logger.Warn().Uint32("cmd", msg.CommandCode).Msg("unexpected inbound request from server")
	}
}

// drainPending closes all pending request channels with the given error by
// sending a nil (callers check if reply is nil).
func (p *Peer) drainPending(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for hbh, req := range p.pending {
		select {
		case req.ch <- nil:
		default:
		}
		delete(p.pending, hbh)
	}
}

// writeMessage encodes and writes a Diameter message to conn.
func (p *Peer) writeMessage(conn net.Conn, msg *argusdiameter.Message) error {
	data, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write(data)
	return err
}

// buildCER constructs a Capabilities-Exchange-Request.
func (p *Peer) buildCER(hbh uint32) *argusdiameter.Message {
	cer := argusdiameter.NewRequest(argusdiameter.CommandCER, argusdiameter.ApplicationIDDiameterBase, hbh, p.nextEndID())
	cer.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, p.cfg.OriginHost))
	cer.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, p.cfg.OriginRealm))
	cer.AddAVP(argusdiameter.NewAVPAddress(argusdiameter.AVPCodeHostIPAddress, argusdiameter.AVPFlagMandatory, 0, [4]byte{0, 0, 0, 0}))
	cer.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeVendorID, argusdiameter.AVPFlagMandatory, 0, 99999))
	cer.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeProductName, 0, 0, "argus-simulator"))

	for _, appID := range p.cfg.AppIDs {
		cer.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeAuthApplicationID, argusdiameter.AVPFlagMandatory, 0, appID))
	}

	if len(p.cfg.AppIDs) > 0 {
		cer.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeSupportedVendorID, argusdiameter.AVPFlagMandatory, 0, argusdiameter.VendorID3GPP))
	}
	cer.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeFirmwareRevision, 0, 0, 1))
	return cer
}

// buildDPR constructs a Disconnect-Peer-Request.
func (p *Peer) buildDPR() *argusdiameter.Message {
	endID := p.nextEndID()
	dpr := argusdiameter.NewRequest(argusdiameter.CommandDPR, argusdiameter.ApplicationIDDiameterBase, 0, endID)
	dpr.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, p.cfg.OriginHost))
	dpr.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, p.cfg.OriginRealm))
	dpr.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeDisconnectCause, argusdiameter.AVPFlagMandatory, 0, argusdiameter.DisconnectCauseDoNotWant))
	return dpr
}

// sleep sleeps for d or until ctx is cancelled or closeCh fires.
// Returns false if the sleep was interrupted by close/ctx.
func (p *Peer) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	case <-p.closeCh:
		return false
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
