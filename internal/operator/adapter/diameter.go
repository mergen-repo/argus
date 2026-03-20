package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/aaa/diameter"
)

type DiameterConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	OriginHost  string `json:"origin_host"`
	OriginRealm string `json:"origin_realm"`
	TLSEnabled  bool   `json:"tls_enabled"`
	TimeoutMs   int    `json:"timeout_ms"`
}

type DiameterAdapter struct {
	mu       sync.Mutex
	config   DiameterConfig
	conn     net.Conn
	hopID    uint32
	cerDone  bool
	watchdog bool
	stopWd   chan struct{}
}

func NewDiameterAdapter(raw json.RawMessage) (*DiameterAdapter, error) {
	var cfg DiameterConfig
	if raw == nil || len(raw) == 0 {
		return nil, fmt.Errorf("diameter adapter config required")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse diameter config: %w", err)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("diameter host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 3868
	}
	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = 3000
	}

	return &DiameterAdapter{config: cfg, hopID: 1}, nil
}

func (d *DiameterAdapter) HealthCheck(ctx context.Context) HealthResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	start := time.Now()
	addr := net.JoinHostPort(d.config.Host, strconv.Itoa(d.config.Port))
	timeout := time.Duration(d.config.TimeoutMs) * time.Millisecond

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return HealthResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     fmt.Sprintf("dial: %v", err),
		}
	}
	defer conn.Close()

	return HealthResult{
		Success:   true,
		LatencyMs: int(time.Since(start).Milliseconds()),
	}
}

func (d *DiameterAdapter) ForwardAuth(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	conn, err := d.getConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("diameter connect: %w", err)
	}

	hopID := d.nextHopID()
	endID := hopID

	ccr := diameter.NewRequest(diameter.CommandCCR, diameter.ApplicationIDGx, hopID, endID)
	sessionID := fmt.Sprintf("argus;%s;%d", req.IMSI, time.Now().UnixNano())
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeSessionID, diameter.AVPFlagMandatory, 0, sessionID))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginHost, diameter.AVPFlagMandatory, 0, d.config.OriginHost))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeDestinationRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeAuthApplicationID, diameter.AVPFlagMandatory, 0, diameter.ApplicationIDGx))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeCCRequestType, diameter.AVPFlagMandatory, 0, diameter.CCRequestTypeInitial))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeCCRequestNumber, diameter.AVPFlagMandatory, 0, 0))

	for _, sub := range diameter.BuildSubscriptionID(req.IMSI, req.MSISDN) {
		ccr.AddAVP(sub)
	}

	ccrData, err := ccr.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode ccr: %w", err)
	}

	timeout := time.Duration(d.config.TimeoutMs) * time.Millisecond
	conn.SetDeadline(time.Now().Add(timeout))

	if _, err := conn.Write(ccrData); err != nil {
		d.closeConnection()
		return nil, fmt.Errorf("write ccr: %w", err)
	}

	cca, err := d.readMessage(conn, timeout)
	if err != nil {
		d.closeConnection()
		return nil, fmt.Errorf("read cca: %w", err)
	}

	resultCode := cca.GetResultCode()
	resp := &AuthResponse{
		Attributes: make(map[string]interface{}),
	}

	switch resultCode {
	case diameter.ResultCodeSuccess:
		resp.Code = AuthAccept
	case diameter.ResultCodeAuthenticationRejected:
		resp.Code = AuthReject
	default:
		resp.Code = AuthReject
		resp.Attributes["diameter_result_code"] = resultCode
	}

	return resp, nil
}

func (d *DiameterAdapter) ForwardAcct(ctx context.Context, req AcctRequest) error {
	conn, err := d.getConnection(ctx)
	if err != nil {
		return fmt.Errorf("diameter connect: %w", err)
	}

	hopID := d.nextHopID()

	var ccReqType uint32
	switch req.StatusType {
	case AcctStart:
		ccReqType = diameter.CCRequestTypeInitial
	case AcctInterim:
		ccReqType = diameter.CCRequestTypeUpdate
	case AcctStop:
		ccReqType = diameter.CCRequestTypeTermination
	default:
		ccReqType = diameter.CCRequestTypeEvent
	}

	ccr := diameter.NewRequest(diameter.CommandCCR, diameter.ApplicationIDGy, hopID, hopID)
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("argus;%s;acct", req.IMSI)
	}
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeSessionID, diameter.AVPFlagMandatory, 0, sessionID))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginHost, diameter.AVPFlagMandatory, 0, d.config.OriginHost))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeDestinationRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeAuthApplicationID, diameter.AVPFlagMandatory, 0, diameter.ApplicationIDGy))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeCCRequestType, diameter.AVPFlagMandatory, 0, ccReqType))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeCCRequestNumber, diameter.AVPFlagMandatory, 0, 0))

	for _, sub := range diameter.BuildSubscriptionID(req.IMSI, "") {
		ccr.AddAVP(sub)
	}

	if req.InputOctets > 0 || req.OutputOctets > 0 {
		usu := diameter.NewAVPGrouped(diameter.AVPCodeUsedServiceUnit, diameter.AVPFlagMandatory, 0, []*diameter.AVP{
			diameter.NewAVPUint64(diameter.AVPCodeCCInputOctets, diameter.AVPFlagMandatory, 0, req.InputOctets),
			diameter.NewAVPUint64(diameter.AVPCodeCCOutputOctets, diameter.AVPFlagMandatory, 0, req.OutputOctets),
		})
		ccr.AddAVP(usu)
	}

	ccrData, err := ccr.Encode()
	if err != nil {
		return fmt.Errorf("encode ccr: %w", err)
	}

	timeout := time.Duration(d.config.TimeoutMs) * time.Millisecond
	conn.SetDeadline(time.Now().Add(timeout))

	if _, err := conn.Write(ccrData); err != nil {
		d.closeConnection()
		return fmt.Errorf("write ccr: %w", err)
	}

	_, err = d.readMessage(conn, timeout)
	if err != nil {
		d.closeConnection()
		return fmt.Errorf("read cca: %w", err)
	}

	return nil
}

func (d *DiameterAdapter) SendCoA(ctx context.Context, req CoARequest) error {
	conn, err := d.getConnection(ctx)
	if err != nil {
		return fmt.Errorf("diameter connect: %w", err)
	}

	hopID := d.nextHopID()
	rar := diameter.NewRequest(diameter.CommandRAR, diameter.ApplicationIDGx, hopID, hopID)
	rar.AddAVP(diameter.NewAVPString(diameter.AVPCodeSessionID, diameter.AVPFlagMandatory, 0, req.SessionID))
	rar.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginHost, diameter.AVPFlagMandatory, 0, d.config.OriginHost))
	rar.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))
	rar.AddAVP(diameter.NewAVPString(diameter.AVPCodeDestinationRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))
	rar.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeAuthApplicationID, diameter.AVPFlagMandatory, 0, diameter.ApplicationIDGx))

	if req.SessionTimeout != nil {
		rar.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeSessionTimeout, diameter.AVPFlagMandatory, 0, uint32(*req.SessionTimeout)))
	}

	rarData, err := rar.Encode()
	if err != nil {
		return fmt.Errorf("encode rar: %w", err)
	}

	timeout := time.Duration(d.config.TimeoutMs) * time.Millisecond
	conn.SetDeadline(time.Now().Add(timeout))

	if _, err := conn.Write(rarData); err != nil {
		d.closeConnection()
		return fmt.Errorf("write rar: %w", err)
	}

	_, err = d.readMessage(conn, timeout)
	if err != nil {
		d.closeConnection()
		return fmt.Errorf("read raa: %w", err)
	}

	return nil
}

func (d *DiameterAdapter) SendDM(ctx context.Context, req DMRequest) error {
	conn, err := d.getConnection(ctx)
	if err != nil {
		return fmt.Errorf("diameter connect: %w", err)
	}

	hopID := d.nextHopID()
	ccr := diameter.NewRequest(diameter.CommandCCR, diameter.ApplicationIDGx, hopID, hopID)
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeSessionID, diameter.AVPFlagMandatory, 0, req.SessionID))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginHost, diameter.AVPFlagMandatory, 0, d.config.OriginHost))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))
	ccr.AddAVP(diameter.NewAVPString(diameter.AVPCodeDestinationRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeAuthApplicationID, diameter.AVPFlagMandatory, 0, diameter.ApplicationIDGx))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeCCRequestType, diameter.AVPFlagMandatory, 0, diameter.CCRequestTypeTermination))
	ccr.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeCCRequestNumber, diameter.AVPFlagMandatory, 0, 0))

	for _, sub := range diameter.BuildSubscriptionID(req.IMSI, "") {
		ccr.AddAVP(sub)
	}

	ccrData, err := ccr.Encode()
	if err != nil {
		return fmt.Errorf("encode ccr-t: %w", err)
	}

	timeout := time.Duration(d.config.TimeoutMs) * time.Millisecond
	conn.SetDeadline(time.Now().Add(timeout))

	if _, err := conn.Write(ccrData); err != nil {
		d.closeConnection()
		return fmt.Errorf("write ccr-t: %w", err)
	}

	_, err = d.readMessage(conn, timeout)
	if err != nil {
		d.closeConnection()
		return fmt.Errorf("read cca-t: %w", err)
	}

	return nil
}

func (d *DiameterAdapter) Authenticate(ctx context.Context, req AuthenticateRequest) (*AuthenticateResponse, error) {
	authReq := AuthRequest{
		IMSI:   req.IMSI,
		MSISDN: req.MSISDN,
		APN:    req.APN,
	}

	resp, err := d.ForwardAuth(ctx, authReq)
	if err != nil {
		return nil, err
	}

	return &AuthenticateResponse{
		Success:    resp.Code == AuthAccept,
		Code:       resp.Code,
		SessionID:  fmt.Sprintf("diameter-%s-%d", req.IMSI, time.Now().UnixNano()),
		Attributes: resp.Attributes,
	}, nil
}

func (d *DiameterAdapter) AccountingUpdate(ctx context.Context, req AccountingUpdateRequest) error {
	acctReq := AcctRequest{
		IMSI:         req.IMSI,
		SessionID:    req.SessionID,
		StatusType:   req.StatusType,
		InputOctets:  req.InputOctets,
		OutputOctets: req.OutputOctets,
		SessionTime:  req.SessionTime,
	}

	return d.ForwardAcct(ctx, acctReq)
}

func (d *DiameterAdapter) FetchAuthVectors(_ context.Context, _ string, _ int) ([]AuthVector, error) {
	return nil, fmt.Errorf("%w: Diameter does not support direct vector fetch", ErrUnsupportedProtocol)
}

func (d *DiameterAdapter) Type() string {
	return "diameter"
}

func (d *DiameterAdapter) getConnection(ctx context.Context) (net.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.conn != nil {
		return d.conn, nil
	}

	addr := net.JoinHostPort(d.config.Host, strconv.Itoa(d.config.Port))
	timeout := time.Duration(d.config.TimeoutMs) * time.Millisecond

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	d.conn = conn

	if err := d.performCER(conn, timeout); err != nil {
		conn.Close()
		d.conn = nil
		return nil, fmt.Errorf("CER handshake: %w", err)
	}
	d.cerDone = true

	if !d.watchdog {
		d.stopWd = make(chan struct{})
		d.watchdog = true
		go d.runWatchdog()
	}

	return conn, nil
}

func (d *DiameterAdapter) performCER(conn net.Conn, timeout time.Duration) error {
	hopID := d.hopID + 1
	d.hopID = hopID

	cer := diameter.NewRequest(diameter.CommandCER, diameter.ApplicationIDDiameterBase, hopID, hopID)
	cer.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginHost, diameter.AVPFlagMandatory, 0, d.config.OriginHost))
	cer.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))

	hostIP := net.ParseIP("127.0.0.1").To4()
	if hostIP != nil {
		ipData := make([]byte, 6)
		ipData[0] = 0
		ipData[1] = 1
		copy(ipData[2:], hostIP)
		cer.AddAVP(&diameter.AVP{Code: diameter.AVPCodeHostIPAddress, Flags: diameter.AVPFlagMandatory, Data: ipData})
	}

	cer.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeVendorID, diameter.AVPFlagMandatory, 0, 0))
	cer.AddAVP(diameter.NewAVPString(diameter.AVPCodeProductName, 0, 0, "argus"))
	cer.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeAuthApplicationID, diameter.AVPFlagMandatory, 0, diameter.ApplicationIDGx))
	cer.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeAcctApplicationID, diameter.AVPFlagMandatory, 0, diameter.ApplicationIDGy))

	cerData, err := cer.Encode()
	if err != nil {
		return fmt.Errorf("encode CER: %w", err)
	}

	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(cerData); err != nil {
		return fmt.Errorf("write CER: %w", err)
	}

	cea, err := d.readMessage(conn, timeout)
	if err != nil {
		return fmt.Errorf("read CEA: %w", err)
	}

	resultCode := cea.GetResultCode()
	if resultCode != diameter.ResultCodeSuccess {
		return fmt.Errorf("CEA result code: %d", resultCode)
	}

	return nil
}

func (d *DiameterAdapter) runWatchdog() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopWd:
			return
		case <-ticker.C:
			d.sendDWR()
		}
	}
}

func (d *DiameterAdapter) sendDWR() {
	d.mu.Lock()
	conn := d.conn
	if conn == nil {
		d.mu.Unlock()
		return
	}
	hopID := d.hopID + 1
	d.hopID = hopID
	d.mu.Unlock()

	dwr := diameter.NewRequest(diameter.CommandDWR, diameter.ApplicationIDDiameterBase, hopID, hopID)
	dwr.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginHost, diameter.AVPFlagMandatory, 0, d.config.OriginHost))
	dwr.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginRealm, diameter.AVPFlagMandatory, 0, d.config.OriginRealm))

	dwrData, err := dwr.Encode()
	if err != nil {
		return
	}

	timeout := time.Duration(d.config.TimeoutMs) * time.Millisecond
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(dwrData); err != nil {
		d.closeConnection()
		return
	}

	if _, err := d.readMessage(conn, timeout); err != nil {
		d.closeConnection()
	}
}

func (d *DiameterAdapter) closeConnection() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
		d.cerDone = false
	}
	if d.watchdog && d.stopWd != nil {
		close(d.stopWd)
		d.watchdog = false
	}
}

func (d *DiameterAdapter) nextHopID() uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hopID++
	return d.hopID
}

func (d *DiameterAdapter) readMessage(conn net.Conn, timeout time.Duration) (*diameter.Message, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))

	headerBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, ErrAdapterTimeout
		}
		return nil, fmt.Errorf("read header: %w", err)
	}

	if headerBuf[0] != 1 {
		return nil, fmt.Errorf("unsupported diameter version: %d", headerBuf[0])
	}
	msgLen := int(headerBuf[1])<<16 | int(headerBuf[2])<<8 | int(headerBuf[3])
	if msgLen < 20 {
		return nil, fmt.Errorf("invalid message length: %d", msgLen)
	}

	buf := make([]byte, msgLen)
	copy(buf[:4], headerBuf)
	if _, err := io.ReadFull(conn, buf[4:]); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return diameter.DecodeMessage(buf)
}
