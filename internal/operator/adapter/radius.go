package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

type RADIUSConfig struct {
	Host         string `json:"host"`
	AuthPort     int    `json:"auth_port"`
	AcctPort     int    `json:"acct_port"`
	CoAPort      int    `json:"coa_port"`
	SharedSecret string `json:"shared_secret"`
	TimeoutMs    int    `json:"timeout_ms"`
	Retries      int    `json:"retries"`
}

type RADIUSAdapter struct {
	mu     sync.RWMutex
	config RADIUSConfig
}

func NewRADIUSAdapter(raw json.RawMessage) (*RADIUSAdapter, error) {
	var cfg RADIUSConfig
	if raw == nil || len(raw) == 0 {
		return nil, fmt.Errorf("radius adapter config required")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse radius config: %w", err)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("radius host is required")
	}
	if cfg.AuthPort == 0 {
		cfg.AuthPort = 1812
	}
	if cfg.AcctPort == 0 {
		cfg.AcctPort = 1813
	}
	if cfg.CoAPort == 0 {
		cfg.CoAPort = 3799
	}
	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = 3000
	}
	if cfg.Retries == 0 {
		cfg.Retries = 3
	}
	if cfg.SharedSecret == "" {
		return nil, fmt.Errorf("radius shared_secret is required")
	}

	return &RADIUSAdapter{config: cfg}, nil
}

func (r *RADIUSAdapter) HealthCheck(ctx context.Context) HealthResult {
	r.mu.RLock()
	cfg := r.config
	r.mu.RUnlock()

	start := time.Now()
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.AuthPort))
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond

	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return HealthResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     fmt.Sprintf("dial: %v", err),
		}
	}
	defer conn.Close()

	statusServerPacket := buildRADIUSStatusServer(cfg.SharedSecret)

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return HealthResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     fmt.Sprintf("set deadline: %v", err),
		}
	}

	if _, err := conn.Write(statusServerPacket); err != nil {
		return HealthResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     fmt.Sprintf("write: %v", err),
		}
	}

	buf := make([]byte, 4096)
	_, err = conn.Read(buf)
	latencyMs := int(time.Since(start).Milliseconds())

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return HealthResult{
				Success:   false,
				LatencyMs: latencyMs,
				Error:     "timeout waiting for response",
			}
		}
		return HealthResult{
			Success:   true,
			LatencyMs: latencyMs,
		}
	}

	return HealthResult{
		Success:   true,
		LatencyMs: latencyMs,
	}
}

func (r *RADIUSAdapter) ForwardAuth(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	r.mu.RLock()
	cfg := r.config
	r.mu.RUnlock()

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.AuthPort))
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond

	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial radius auth: %w", err)
	}
	defer conn.Close()

	packet := buildRADIUSAccessRequest(req, cfg.SharedSecret)

	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		return nil, fmt.Errorf("write radius packet: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, ErrAdapterTimeout
		}
		return nil, fmt.Errorf("read radius response: %w", err)
	}

	return parseRADIUSAuthResponse(buf[:n])
}

func (r *RADIUSAdapter) ForwardAcct(ctx context.Context, req AcctRequest) error {
	r.mu.RLock()
	cfg := r.config
	r.mu.RUnlock()

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.AcctPort))
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond

	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return fmt.Errorf("dial radius acct: %w", err)
	}
	defer conn.Close()

	packet := buildRADIUSAcctRequest(req, cfg.SharedSecret)

	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("write radius acct packet: %w", err)
	}

	buf := make([]byte, 4096)
	_, err = conn.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return ErrAdapterTimeout
		}
		return fmt.Errorf("read radius acct response: %w", err)
	}

	return nil
}

func (r *RADIUSAdapter) SendCoA(ctx context.Context, req CoARequest) error {
	r.mu.RLock()
	cfg := r.config
	r.mu.RUnlock()

	port := cfg.CoAPort
	if req.NASCoAPort > 0 {
		port = req.NASCoAPort
	}
	target := req.NASIP
	if target == "" {
		target = cfg.Host
	}
	addr := net.JoinHostPort(target, strconv.Itoa(port))
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond

	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return fmt.Errorf("dial coa: %w", err)
	}
	defer conn.Close()

	packet := buildRADIUSCoAPacket(req, cfg.SharedSecret)

	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("write coa packet: %w", err)
	}

	buf := make([]byte, 4096)
	_, err = conn.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return ErrAdapterTimeout
		}
		return fmt.Errorf("read coa response: %w", err)
	}

	return nil
}

func (r *RADIUSAdapter) SendDM(ctx context.Context, req DMRequest) error {
	r.mu.RLock()
	cfg := r.config
	r.mu.RUnlock()

	port := cfg.CoAPort
	if req.NASCoAPort > 0 {
		port = req.NASCoAPort
	}
	target := req.NASIP
	if target == "" {
		target = cfg.Host
	}
	addr := net.JoinHostPort(target, strconv.Itoa(port))
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond

	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return fmt.Errorf("dial dm: %w", err)
	}
	defer conn.Close()

	packet := buildRADIUSDMPacket(req, cfg.SharedSecret)

	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set deadline: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("write dm packet: %w", err)
	}

	buf := make([]byte, 4096)
	_, err = conn.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return ErrAdapterTimeout
		}
		return fmt.Errorf("read dm response: %w", err)
	}

	return nil
}

func (r *RADIUSAdapter) Authenticate(ctx context.Context, req AuthenticateRequest) (*AuthenticateResponse, error) {
	authReq := AuthRequest{
		IMSI:   req.IMSI,
		MSISDN: req.MSISDN,
		APN:    req.APN,
		NASId:  r.config.Host,
	}

	resp, err := r.ForwardAuth(ctx, authReq)
	if err != nil {
		return nil, err
	}

	return &AuthenticateResponse{
		Success:    resp.Code == AuthAccept,
		Code:       resp.Code,
		SessionID:  fmt.Sprintf("radius-%s-%d", req.IMSI, time.Now().UnixNano()),
		Attributes: resp.Attributes,
	}, nil
}

func (r *RADIUSAdapter) AccountingUpdate(ctx context.Context, req AccountingUpdateRequest) error {
	acctReq := AcctRequest{
		IMSI:         req.IMSI,
		SessionID:    req.SessionID,
		StatusType:   req.StatusType,
		InputOctets:  req.InputOctets,
		OutputOctets: req.OutputOctets,
		SessionTime:  req.SessionTime,
	}

	return r.ForwardAcct(ctx, acctReq)
}

func (r *RADIUSAdapter) FetchAuthVectors(_ context.Context, _ string, _ int) ([]AuthVector, error) {
	return nil, fmt.Errorf("%w: RADIUS does not support direct vector fetch", ErrUnsupportedProtocol)
}

func (r *RADIUSAdapter) Type() string {
	return "radius"
}

const (
	radiusCodeAccessRequest  byte = 1
	radiusCodeAccessAccept   byte = 2
	radiusCodeAccessReject   byte = 3
	radiusCodeAcctRequest    byte = 4
	radiusCodeStatusServer   byte = 12
	radiusCodeCoARequest     byte = 43
	radiusCodeDMRequest      byte = 40
)

const (
	radiusAttrUserName      byte = 1
	radiusAttrNASIPAddress  byte = 4
	radiusAttrAcctSessionID byte = 44
	radiusAttrAcctStatusType byte = 40
	radiusAttrNASIdentifier byte = 32
	radiusAttrSessionTimeout byte = 27
	radiusAttrIdleTimeout   byte = 28
	radiusAttrFilterID      byte = 11
	radiusAttrFramedIP      byte = 8
)

func buildRADIUSStatusServer(secret string) []byte {
	_ = secret
	packet := make([]byte, 20)
	packet[0] = radiusCodeStatusServer
	packet[1] = 1
	packet[2] = 0
	packet[3] = 20
	return packet
}

func buildRADIUSAccessRequest(req AuthRequest, secret string) []byte {
	_ = secret
	attrs := encodeRADIUSStringAttr(radiusAttrUserName, req.IMSI)
	if req.NASId != "" {
		attrs = append(attrs, encodeRADIUSStringAttr(radiusAttrNASIdentifier, req.NASId)...)
	}

	pktLen := 20 + len(attrs)
	packet := make([]byte, 20, pktLen)
	packet[0] = radiusCodeAccessRequest
	packet[1] = 1
	packet[2] = byte(pktLen >> 8)
	packet[3] = byte(pktLen)
	packet = append(packet, attrs...)
	return packet
}

func buildRADIUSAcctRequest(req AcctRequest, secret string) []byte {
	_ = secret
	attrs := encodeRADIUSStringAttr(radiusAttrUserName, req.IMSI)
	attrs = append(attrs, encodeRADIUSStringAttr(radiusAttrAcctSessionID, req.SessionID)...)

	pktLen := 20 + len(attrs)
	packet := make([]byte, 20, pktLen)
	packet[0] = radiusCodeAcctRequest
	packet[1] = 1
	packet[2] = byte(pktLen >> 8)
	packet[3] = byte(pktLen)
	packet = append(packet, attrs...)
	return packet
}

func buildRADIUSCoAPacket(req CoARequest, secret string) []byte {
	_ = secret
	attrs := encodeRADIUSStringAttr(radiusAttrUserName, req.IMSI)
	attrs = append(attrs, encodeRADIUSStringAttr(radiusAttrAcctSessionID, req.SessionID)...)

	pktLen := 20 + len(attrs)
	packet := make([]byte, 20, pktLen)
	packet[0] = radiusCodeCoARequest
	packet[1] = 1
	packet[2] = byte(pktLen >> 8)
	packet[3] = byte(pktLen)
	packet = append(packet, attrs...)
	return packet
}

func buildRADIUSDMPacket(req DMRequest, secret string) []byte {
	_ = secret
	attrs := encodeRADIUSStringAttr(radiusAttrUserName, req.IMSI)
	attrs = append(attrs, encodeRADIUSStringAttr(radiusAttrAcctSessionID, req.SessionID)...)

	pktLen := 20 + len(attrs)
	packet := make([]byte, 20, pktLen)
	packet[0] = radiusCodeDMRequest
	packet[1] = 1
	packet[2] = byte(pktLen >> 8)
	packet[3] = byte(pktLen)
	packet = append(packet, attrs...)
	return packet
}

func encodeRADIUSStringAttr(attrType byte, value string) []byte {
	attrLen := 2 + len(value)
	attr := make([]byte, attrLen)
	attr[0] = attrType
	attr[1] = byte(attrLen)
	copy(attr[2:], value)
	return attr
}

func parseRADIUSAuthResponse(data []byte) (*AuthResponse, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("radius response too short: %d bytes", len(data))
	}

	code := data[0]
	resp := &AuthResponse{
		Attributes: make(map[string]interface{}),
	}

	switch code {
	case radiusCodeAccessAccept:
		resp.Code = AuthAccept
	case radiusCodeAccessReject:
		resp.Code = AuthReject
	default:
		resp.Code = AuthReject
		resp.Attributes["raw_code"] = code
	}

	offset := 20
	pktLen := int(data[2])<<8 | int(data[3])
	if pktLen > len(data) {
		pktLen = len(data)
	}

	for offset+2 <= pktLen {
		attrType := data[offset]
		attrLen := int(data[offset+1])
		if attrLen < 2 || offset+attrLen > pktLen {
			break
		}
		value := data[offset+2 : offset+attrLen]

		switch attrType {
		case radiusAttrFramedIP:
			if len(value) == 4 {
				resp.FramedIP = fmt.Sprintf("%d.%d.%d.%d", value[0], value[1], value[2], value[3])
			}
		case radiusAttrSessionTimeout:
			if len(value) == 4 {
				resp.SessionTimeout = int(value[0])<<24 | int(value[1])<<16 | int(value[2])<<8 | int(value[3])
			}
		case radiusAttrIdleTimeout:
			if len(value) == 4 {
				resp.IdleTimeout = int(value[0])<<24 | int(value[1])<<16 | int(value[2])<<8 | int(value[3])
			}
		case radiusAttrFilterID:
			resp.FilterID = string(value)
		}

		offset += attrLen
	}

	return resp, nil
}
