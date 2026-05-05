package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

const (
	radiusCodeCoA    = radius.Code(43)
	defaultCoAPort   = 3799
	coaTimeout       = 3 * time.Second
	CoAResultACK     = "ack"
	CoAResultNAK     = "nak"
	CoAResultTimeout = "timeout"
	CoAResultError   = "error"
)

type CoASender struct {
	secret  []byte
	port    int
	logger  zerolog.Logger
	auditor audit.Auditor
}

func NewCoASender(secret string, port int, logger zerolog.Logger, opts ...func(*CoASender)) *CoASender {
	if port == 0 {
		port = defaultCoAPort
	}
	s := &CoASender{
		secret: []byte(secret),
		port:   port,
		logger: logger.With().Str("component", "coa_sender").Logger(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func WithCoAAuditor(a audit.Auditor) func(*CoASender) {
	return func(s *CoASender) { s.auditor = a }
}

type CoARequest struct {
	NASIP         string
	AcctSessionID string
	IMSI          string
	SessionID     string
	TenantID      uuid.UUID
	Attributes    map[string]interface{}
}

type CoAResult struct {
	Status  string
	Message string
}

func (c *CoASender) SendCoA(ctx context.Context, req CoARequest) (*CoAResult, error) {
	packet := radius.New(radiusCodeCoA, c.secret)
	rfc2866.AcctSessionID_SetString(packet, req.AcctSessionID)
	rfc2865.UserName_SetString(packet, req.IMSI)

	if v, ok := req.Attributes["Session-Timeout"]; ok {
		if timeout, ok := v.(int); ok {
			rfc2865.SessionTimeout_Set(packet, rfc2865.SessionTimeout(timeout))
		}
	}
	if v, ok := req.Attributes["Idle-Timeout"]; ok {
		if timeout, ok := v.(int); ok {
			rfc2865.IdleTimeout_Set(packet, rfc2865.IdleTimeout(timeout))
		}
	}

	addr := fmt.Sprintf("%s:%d", req.NASIP, c.port)
	result, err := c.sendPacket(ctx, addr, packet)
	if err != nil {
		c.logger.Error().Err(err).
			Str("nas_ip", req.NASIP).
			Str("acct_session_id", req.AcctSessionID).
			Msg("CoA send failed")
		errResult := &CoAResult{Status: CoAResultError, Message: err.Error()}
		c.emitAudit(ctx, req, errResult)
		return errResult, err
	}

	c.logger.Info().
		Str("nas_ip", req.NASIP).
		Str("acct_session_id", req.AcctSessionID).
		Str("result", result.Status).
		Msg("CoA sent")

	c.emitAudit(ctx, req, result)
	return result, nil
}

func (c *CoASender) emitAudit(ctx context.Context, req CoARequest, result *CoAResult) {
	if c.auditor == nil || req.TenantID == uuid.Nil {
		return
	}
	afterData, _ := json.Marshal(map[string]interface{}{
		"nas_ip":          req.NASIP,
		"acct_session_id": req.AcctSessionID,
		"imsi":            req.IMSI,
		"status":          result.Status,
		"message":         result.Message,
	})
	_, err := c.auditor.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:   req.TenantID,
		Action:     "session.coa_sent",
		EntityType: "session",
		EntityID:   req.SessionID,
		AfterData:  afterData,
	})
	if err != nil {
		c.logger.Warn().Err(err).Str("session_id", req.SessionID).Msg("audit coa_sent failed")
	}
}

func (c *CoASender) sendPacket(ctx context.Context, addr string, packet *radius.Packet) (*CoAResult, error) {
	encoded, err := packet.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode CoA packet: %w", err)
	}

	conn, err := net.DialTimeout("udp", addr, coaTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial NAS %s: %w", addr, err)
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(coaTimeout)
	}
	conn.SetDeadline(deadline)

	if _, err := conn.Write(encoded); err != nil {
		return nil, fmt.Errorf("write CoA to %s: %w", addr, err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return &CoAResult{Status: CoAResultTimeout, Message: "NAS did not respond"}, nil
		}
		return nil, fmt.Errorf("read CoA response from %s: %w", addr, err)
	}

	resp, err := radius.Parse(buf[:n], c.secret)
	if err != nil {
		return nil, fmt.Errorf("parse CoA response: %w", err)
	}

	if resp.Code == radius.Code(44) {
		return &CoAResult{Status: CoAResultACK, Message: "CoA-ACK"}, nil
	}
	return &CoAResult{Status: CoAResultNAK, Message: fmt.Sprintf("CoA-NAK (code=%d)", resp.Code)}, nil
}
