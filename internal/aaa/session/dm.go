package session

import (
	"context"
	"fmt"
	"net"
	"time"

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
	secret []byte
	port   int
	logger zerolog.Logger
}

func NewDMSender(secret string, port int, logger zerolog.Logger) *DMSender {
	if port == 0 {
		port = defaultCoAPort
	}
	return &DMSender{
		secret: []byte(secret),
		port:   port,
		logger: logger.With().Str("component", "dm_sender").Logger(),
	}
}

type DMRequest struct {
	NASIP         string
	AcctSessionID string
	IMSI          string
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
		return &DMResult{Status: DMResultError, Message: err.Error()}, err
	}

	d.logger.Info().
		Str("nas_ip", req.NASIP).
		Str("acct_session_id", req.AcctSessionID).
		Str("result", result.Status).
		Msg("DM sent")

	return result, nil
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
