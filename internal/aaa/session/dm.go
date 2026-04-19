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
	radiusCodeDM    = radius.Code(40)
	dmTimeout       = 3 * time.Second
	DMResultACK     = "ack"
	DMResultNAK     = "nak"
	DMResultTimeout = "timeout"
	DMResultError   = "error"
)

type DMSender struct {
	secret  []byte
	port    int
	logger  zerolog.Logger
	auditor audit.Auditor
}

func NewDMSender(secret string, port int, logger zerolog.Logger, opts ...func(*DMSender)) *DMSender {
	if port == 0 {
		port = defaultCoAPort
	}
	s := &DMSender{
		secret: []byte(secret),
		port:   port,
		logger: logger.With().Str("component", "dm_sender").Logger(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func WithDMAuditor(a audit.Auditor) func(*DMSender) {
	return func(s *DMSender) { s.auditor = a }
}

type DMRequest struct {
	NASIP         string
	AcctSessionID string
	IMSI          string
	SessionID     string
	TenantID      uuid.UUID
}

type DMResult struct {
	Status  string
	Message string
}

func (d *DMSender) SendDM(ctx context.Context, req DMRequest) (*DMResult, error) {
	packet := radius.New(radiusCodeDM, d.secret)
	rfc2866.AcctSessionID_SetString(packet, req.AcctSessionID)
	rfc2865.UserName_SetString(packet, req.IMSI)

	addr := fmt.Sprintf("%s:%d", req.NASIP, d.port)
	result, err := d.sendPacket(ctx, addr, packet)
	if err != nil {
		d.logger.Error().Err(err).
			Str("nas_ip", req.NASIP).
			Str("acct_session_id", req.AcctSessionID).
			Msg("DM send failed")
		errResult := &DMResult{Status: DMResultError, Message: err.Error()}
		d.emitAudit(ctx, req, errResult)
		return errResult, err
	}

	d.logger.Info().
		Str("nas_ip", req.NASIP).
		Str("acct_session_id", req.AcctSessionID).
		Str("result", result.Status).
		Msg("DM sent")

	d.emitAudit(ctx, req, result)
	return result, nil
}

func (d *DMSender) emitAudit(ctx context.Context, req DMRequest, result *DMResult) {
	if d.auditor == nil || req.TenantID == uuid.Nil {
		return
	}
	afterData, _ := json.Marshal(map[string]interface{}{
		"nas_ip":          req.NASIP,
		"acct_session_id": req.AcctSessionID,
		"imsi":            req.IMSI,
		"status":          result.Status,
		"message":         result.Message,
	})
	_, err := d.auditor.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:   req.TenantID,
		Action:     "session.dm_sent",
		EntityType: "session",
		EntityID:   req.SessionID,
		AfterData:  afterData,
	})
	if err != nil {
		d.logger.Warn().Err(err).Str("session_id", req.SessionID).Msg("audit dm_sent failed")
	}
}

func (d *DMSender) sendPacket(ctx context.Context, addr string, packet *radius.Packet) (*DMResult, error) {
	encoded, err := packet.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode DM packet: %w", err)
	}

	conn, err := net.DialTimeout("udp", addr, dmTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial NAS %s: %w", addr, err)
	}
	defer conn.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(dmTimeout)
	}
	conn.SetDeadline(deadline)

	if _, err := conn.Write(encoded); err != nil {
		return nil, fmt.Errorf("write DM to %s: %w", addr, err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return &DMResult{Status: DMResultTimeout, Message: "NAS did not respond"}, nil
		}
		return nil, fmt.Errorf("read DM response from %s: %w", addr, err)
	}

	resp, err := radius.Parse(buf[:n], d.secret)
	if err != nil {
		return nil, fmt.Errorf("parse DM response: %w", err)
	}

	if resp.Code == radius.Code(41) {
		return &DMResult{Status: DMResultACK, Message: "DM-ACK"}, nil
	}
	return &DMResult{Status: DMResultNAK, Message: fmt.Sprintf("DM-NAK (code=%d)", resp.Code)}, nil
}
