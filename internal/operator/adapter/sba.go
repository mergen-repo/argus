package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type SBAConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	TLSEnabled   bool   `json:"tls_enabled"`
	TimeoutMs    int    `json:"timeout_ms"`
	NfInstanceID string `json:"nf_instance_id"`
}

type SBAAdapter struct {
	mu     sync.RWMutex
	config SBAConfig
	client *http.Client
}

func NewSBAAdapter(raw json.RawMessage) (*SBAAdapter, error) {
	var cfg SBAConfig
	if raw == nil || len(raw) == 0 {
		return nil, fmt.Errorf("sba adapter config required")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse sba config: %w", err)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("sba host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 8443
	}
	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = 3000
	}

	client := &http.Client{
		Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,
		Transport: &http.Transport{
			TLSHandshakeTimeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return &SBAAdapter{config: cfg, client: client}, nil
}

func (s *SBAAdapter) Type() string {
	return "sba"
}

func (s *SBAAdapter) baseURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	scheme := "http"
	if s.config.TLSEnabled {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port)))
}

func (s *SBAAdapter) HealthCheck(ctx context.Context) HealthResult {
	start := time.Now()

	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return HealthResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     fmt.Sprintf("dial: %v", err),
		}
	}
	conn.Close()

	return HealthResult{
		Success:   true,
		LatencyMs: int(time.Since(start).Milliseconds()),
	}
}

func (s *SBAAdapter) ForwardAuth(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	resp, err := s.Authenticate(ctx, AuthenticateRequest{
		IMSI:   req.IMSI,
		MSISDN: req.MSISDN,
		APN:    req.APN,
	})
	if err != nil {
		return nil, err
	}

	code := AuthReject
	if resp.Success {
		code = AuthAccept
	}

	return &AuthResponse{
		Code:       code,
		Attributes: resp.Attributes,
	}, nil
}

func (s *SBAAdapter) ForwardAcct(ctx context.Context, req AcctRequest) error {
	return s.AccountingUpdate(ctx, AccountingUpdateRequest{
		IMSI:         req.IMSI,
		SessionID:    req.SessionID,
		StatusType:   req.StatusType,
		InputOctets:  req.InputOctets,
		OutputOctets: req.OutputOctets,
		SessionTime:  req.SessionTime,
	})
}

func (s *SBAAdapter) SendCoA(_ context.Context, _ CoARequest) error {
	return fmt.Errorf("%w: SBA does not support CoA", ErrUnsupportedProtocol)
}

func (s *SBAAdapter) SendDM(_ context.Context, _ DMRequest) error {
	return fmt.Errorf("%w: SBA does not support DM", ErrUnsupportedProtocol)
}

func (s *SBAAdapter) Authenticate(ctx context.Context, req AuthenticateRequest) (*AuthenticateResponse, error) {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := s.baseURL() + "/nausf-auth/v1/ue-authentications"

	body, err := json.Marshal(map[string]string{
		"supiOrSuci":         req.IMSI,
		"servingNetworkName": req.VisitedPLMN,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal auth request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, jsonReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrAdapterTimeout
		}
		return nil, fmt.Errorf("sba auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return &AuthenticateResponse{
			Success: false,
			Code:    AuthReject,
			Attributes: map[string]interface{}{
				"http_status": resp.StatusCode,
			},
		}, nil
	}

	return &AuthenticateResponse{
		Success:   true,
		Code:      AuthAccept,
		SessionID: fmt.Sprintf("sba-%s-%d", req.IMSI, time.Now().UnixNano()),
		Attributes: map[string]interface{}{
			"http_status": resp.StatusCode,
		},
	}, nil
}

func (s *SBAAdapter) AccountingUpdate(ctx context.Context, req AccountingUpdateRequest) error {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := s.baseURL() + "/npcf-smpolicycontrol/v1/sm-policies"

	body, err := json.Marshal(map[string]interface{}{
		"supi":        req.IMSI,
		"sessionId":   req.SessionID,
		"statusType":  req.StatusType,
		"inputOctets": req.InputOctets,
		"outputOctets": req.OutputOctets,
	})
	if err != nil {
		return fmt.Errorf("marshal acct request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, jsonReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ErrAdapterTimeout
		}
		return fmt.Errorf("sba acct request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("sba accounting failed: HTTP %d", resp.StatusCode)
	}

	return nil
}

func (s *SBAAdapter) FetchAuthVectors(ctx context.Context, imsi string, count int) ([]AuthVector, error) {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := fmt.Sprintf("%s/nudm-ueau/v1/%s/security-information/generate-auth-data", s.baseURL(), imsi)

	body, err := json.Marshal(map[string]interface{}{
		"servingNetworkName": "5G:mnc001.mcc001.3gppnetwork.org",
		"ausfInstanceId":     cfg.NfInstanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal vector request: %w", err)
	}

	vectors := make([]AuthVector, 0, count)
	for i := 0; i < count; i++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, jsonReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := s.client.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ErrAdapterTimeout
			}
			return nil, fmt.Errorf("sba vector request: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("sba vector fetch failed: HTTP %d", resp.StatusCode)
		}

		vectors = append(vectors, AuthVector{
			Type: VectorTypeQuintet,
			RAND: make([]byte, 16),
			AUTN: make([]byte, 16),
			XRES: make([]byte, 8),
			CK:   make([]byte, 16),
			IK:   make([]byte, 16),
		})
	}

	return vectors, nil
}

func jsonReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}
