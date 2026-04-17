package reactive

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc3576"

	"github.com/btopcu/argus/internal/simulator/metrics"
)

// Listener accepts RADIUS CoA-Request (code 43) and Disconnect-Request (code 40)
// packets on a UDP socket, looks up the referenced session in the Registry, and
// performs the appropriate action (cancel for DM, update deadline for CoA).
//
// The listener is process-wide — one listener per simulator process, not per
// operator. An unknown-session DM returns NAK with Error-Cause
// "Session-Context-Not-Found" (503). A wrong-secret packet is silently dropped
// and a bad_secret metric is emitted.
type Listener struct {
	addr     string
	secret   []byte
	registry *Registry
	logger   zerolog.Logger

	conn  *net.UDPConn
	wg    sync.WaitGroup
	ready chan struct{}

	readBufSize int
}

type ListenerConfig struct {
	Addr     string
	Secret   []byte
	Registry *Registry
	Logger   zerolog.Logger
}

func NewListener(cfg ListenerConfig) *Listener {
	return &Listener{
		addr:        cfg.Addr,
		secret:      cfg.Secret,
		registry:    cfg.Registry,
		logger:      cfg.Logger,
		ready:       make(chan struct{}),
		readBufSize: 4096,
	}
}

// Ready returns a channel that is closed once Start has successfully bound
// the UDP socket. If Start fails to bind, the channel is NOT closed.
func (l *Listener) Ready() <-chan struct{} { return l.ready }

// Start binds the UDP socket and spawns a single reader goroutine.
// Returns error on bind failure. Cancellation via ctx triggers socket close
// and graceful shutdown.
func (l *Listener) Start(ctx context.Context) error {
	udpAddr, err := net.ResolveUDPAddr("udp", l.addr)
	if err != nil {
		return fmt.Errorf("resolve udp addr %q: %w", l.addr, err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen udp %q: %w", l.addr, err)
	}
	l.conn = conn
	close(l.ready)
	l.wg.Add(1)
	go l.readLoop(ctx)
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	return nil
}

func (l *Listener) readLoop(ctx context.Context) {
	defer l.wg.Done()
	buf := make([]byte, l.readBufSize)
	for {
		n, src, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			l.logger.Warn().Err(err).Msg("reactive listener read error")
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		l.handlePacket(pkt, src)
	}
}

func (l *Listener) handlePacket(data []byte, src *net.UDPAddr) {
	defer func() {
		if r := recover(); r != nil {
			l.logger.Error().Interface("panic", r).Msg("reactive listener: panic recovered in handlePacket")
		}
	}()

	if len(data) < 20 {
		metrics.SimulatorReactiveIncomingTotal.WithLabelValues("unknown", "unknown", "malformed").Inc()
		l.logger.Debug().Str("src", src.String()).Msg("reactive listener: packet too short")
		return
	}

	codeVal := radius.Code(data[0])
	kind := kindLabel(codeVal)

	if !radius.IsAuthenticRequest(data, l.secret) {
		metrics.SimulatorReactiveIncomingTotal.WithLabelValues("unknown", kind, "bad_secret").Inc()
		l.logger.Debug().Str("src", src.String()).Str("kind", kind).Msg("reactive listener: bad secret / invalid authenticator")
		return
	}

	pkt, err := radius.Parse(data, l.secret)
	if err != nil {
		metrics.SimulatorReactiveIncomingTotal.WithLabelValues("unknown", kind, "malformed").Inc()
		l.logger.Debug().Str("src", src.String()).Err(err).Msg("reactive listener: packet parse failed")
		return
	}

	switch pkt.Code {
	case radius.CodeDisconnectRequest:
		l.handleDM(pkt, src)
	case radius.CodeCoARequest:
		l.handleCoA(pkt, src)
	default:
		metrics.SimulatorReactiveIncomingTotal.WithLabelValues("unknown", "unknown", "unsupported").Inc()
		l.logger.Debug().Stringer("code", pkt.Code).Msg("reactive listener: unsupported code")
	}
}

func (l *Listener) handleDM(req *radius.Packet, src *net.UDPAddr) {
	acct := rfc2866.AcctSessionID_GetString(req)
	sess := l.registry.Lookup(acct)
	operator := operatorFromSession(sess)
	if sess == nil {
		l.writeResponse(req, radius.CodeDisconnectNAK, rfc3576.ErrorCause_Value_SessionContextNotFound, src)
		metrics.SimulatorReactiveIncomingTotal.WithLabelValues(operator, "dm", "unknown_session").Inc()
		return
	}
	sess.SetDisconnectCause(CauseDM)
	if sess.CancelFn != nil {
		sess.CancelFn()
	}
	l.writeResponse(req, radius.CodeDisconnectACK, 0, src)
	metrics.SimulatorReactiveIncomingTotal.WithLabelValues(operator, "dm", "ack").Inc()
}

func (l *Listener) handleCoA(req *radius.Packet, src *net.UDPAddr) {
	acct := rfc2866.AcctSessionID_GetString(req)
	sess := l.registry.Lookup(acct)
	operator := operatorFromSession(sess)
	if sess == nil {
		l.writeResponse(req, radius.CodeCoANAK, rfc3576.ErrorCause_Value_SessionContextNotFound, src)
		metrics.SimulatorReactiveIncomingTotal.WithLabelValues(operator, "coa", "unknown_session").Inc()
		return
	}
	newTimeout := rfc2865.SessionTimeout_Get(req)
	if newTimeout > 0 {
		newDeadline := time.Now().Add(time.Duration(newTimeout) * time.Second)
		// F-A5: only classify as CauseCoADeadline when the CoA actually
		// SHORTENS the remaining lifetime (i.e. the new deadline is earlier
		// than the currently-scheduled one). Otherwise the session would
		// terminate naturally at its original deadline and we must not
		// bias the cause label.
		cur := sess.CurrentDeadline()
		sess.UpdateDeadline(newDeadline)
		if cur.IsZero() || newDeadline.Before(cur) {
			sess.SetDisconnectCause(CauseCoADeadline)
		}
	}
	l.writeResponse(req, radius.CodeCoAACK, 0, src)
	metrics.SimulatorReactiveIncomingTotal.WithLabelValues(operator, "coa", "ack").Inc()
}

func (l *Listener) writeResponse(req *radius.Packet, code radius.Code, errorCause rfc3576.ErrorCause, src *net.UDPAddr) {
	resp := req.Response(code)
	if errorCause != 0 {
		_ = rfc3576.ErrorCause_Set(resp, errorCause)
	}
	raw, err := resp.Encode()
	if err != nil {
		l.logger.Warn().Err(err).Msg("reactive listener: encode response failed")
		return
	}
	if _, err := l.conn.WriteToUDP(raw, src); err != nil {
		l.logger.Warn().Err(err).Msg("reactive listener: write response failed")
	}
}

// Stop closes the UDP socket and waits for the reader goroutine to exit.
func (l *Listener) Stop(ctx context.Context) error {
	if l.conn != nil {
		_ = l.conn.Close()
	}
	done := make(chan struct{})
	go func() { l.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// LocalAddr returns the bound UDP address. Safe after Ready() has closed.
func (l *Listener) LocalAddr() *net.UDPAddr {
	if l.conn == nil {
		return nil
	}
	addr, _ := l.conn.LocalAddr().(*net.UDPAddr)
	return addr
}

func operatorFromSession(s *Session) string {
	if s == nil {
		return "unknown"
	}
	return s.OperatorCode
}

func kindLabel(code radius.Code) string {
	switch code {
	case radius.CodeDisconnectRequest:
		return "dm"
	case radius.CodeCoARequest:
		return "coa"
	default:
		return "unknown"
	}
}
