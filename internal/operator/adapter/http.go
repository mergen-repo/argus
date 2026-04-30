// Package adapter — HTTP adapter (STORY-090 Wave 2 Task 4).
//
// The HTTP adapter is a "health-checkable" operator façade for HTTP-
// based Systems-of-Record calls. It implements the Adapter interface
// minimally: HealthCheck probes a configurable health endpoint; all
// AAA-level methods (ForwardAuth, ForwardAcct, SendCoA, SendDM,
// Authenticate, AccountingUpdate, FetchAuthVectors) return
// ErrUnsupportedProtocol. HTTP is intended for config/metadata sync
// with an external HTTP backend, not for RADIUS/Diameter/SBA AAA
// traffic — those protocols have their own dedicated adapters.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type HTTPConfig struct {
	BaseURL     string `json:"base_url"`
	AuthType    string `json:"auth_type"`
	BearerToken string `json:"bearer_token"`
	AuthToken   string `json:"auth_token"`
	BasicUser   string `json:"basic_user"`
	BasicPass   string `json:"basic_pass"`
	HealthPath  string `json:"health_path"`
	TimeoutMs   int    `json:"timeout_ms"`
}

type HTTPAdapter struct {
	mu     sync.RWMutex
	config HTTPConfig
	client *http.Client
}

// NewHTTPAdapter constructs an HTTPAdapter from a JSON config blob.
// Validation: base_url must be present and parse; defaults are
// health_path="/health" and timeout=2s.
func NewHTTPAdapter(raw json.RawMessage) (*HTTPAdapter, error) {
	var cfg HTTPConfig
	if raw == nil || len(raw) == 0 {
		return nil, fmt.Errorf("http adapter config required")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse http config: %w", err)
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("http base_url is required")
	}
	if _, err := url.Parse(cfg.BaseURL); err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	if cfg.HealthPath == "" {
		cfg.HealthPath = "/health"
	}
	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = 2000
	}

	client := &http.Client{
		Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,
	}

	return &HTTPAdapter{config: cfg, client: client}, nil
}

func (h *HTTPAdapter) Type() string {
	return "http"
}

// HealthCheck issues an HTTP GET against BaseURL + HealthPath, applying
// the Authorization header derived from AuthType. Response codes 200-
// 299 are Success=true; any other status, timeout, or transport error
// is Success=false with a classified Error string.
func (h *HTTPAdapter) HealthCheck(ctx context.Context) HealthResult {
	h.mu.RLock()
	cfg := h.config
	client := h.client
	h.mu.RUnlock()

	start := time.Now()

	// Build target URL: handle BaseURL with or without trailing slash.
	target := strings.TrimRight(cfg.BaseURL, "/") + cfg.HealthPath
	if !strings.HasPrefix(cfg.HealthPath, "/") {
		target = strings.TrimRight(cfg.BaseURL, "/") + "/" + cfg.HealthPath
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return HealthResult{
			Success:   false,
			LatencyMs: int(time.Since(start).Milliseconds()),
			Error:     fmt.Sprintf("build request: %v", err),
		}
	}

	applyHTTPAuth(req, cfg)

	resp, err := client.Do(req)
	latencyMs := int(time.Since(start).Milliseconds())
	if err != nil {
		if ctx.Err() != nil {
			return HealthResult{Success: false, LatencyMs: latencyMs, Error: "timeout"}
		}
		return HealthResult{Success: false, LatencyMs: latencyMs, Error: fmt.Sprintf("request: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return HealthResult{Success: true, LatencyMs: latencyMs}
	}
	return HealthResult{
		Success:   false,
		LatencyMs: latencyMs,
		Error:     fmt.Sprintf("http status %d", resp.StatusCode),
	}
}

// applyHTTPAuth sets the Authorization header on req based on cfg.
// Supported auth types:
//   - "bearer" (or auth_type empty when bearer_token/auth_token set):
//     Bearer token (RFC 6750).
//   - "basic":       HTTP Basic auth (RFC 7617).
//   - "none" / "":   no Authorization header.
func applyHTTPAuth(req *http.Request, cfg HTTPConfig) {
	authType := strings.ToLower(strings.TrimSpace(cfg.AuthType))
	token := cfg.BearerToken
	if token == "" {
		token = cfg.AuthToken
	}
	switch authType {
	case "bearer":
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	case "basic":
		if cfg.BasicUser != "" {
			req.SetBasicAuth(cfg.BasicUser, cfg.BasicPass)
		}
	case "", "none":
		if token != "" {
			// Legacy field presence with no explicit auth_type → treat
			// as bearer for backward-compat.
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}

// ForwardAuth is not supported on HTTP adapters. Use a protocol-native
// adapter (RADIUS / Diameter / SBA) for AAA traffic.
func (h *HTTPAdapter) ForwardAuth(_ context.Context, _ AuthRequest) (*AuthResponse, error) {
	return nil, fmt.Errorf("%w: http adapter does not support ForwardAuth", ErrUnsupportedProtocol)
}

func (h *HTTPAdapter) ForwardAcct(_ context.Context, _ AcctRequest) error {
	return fmt.Errorf("%w: http adapter does not support ForwardAcct", ErrUnsupportedProtocol)
}

func (h *HTTPAdapter) SendCoA(_ context.Context, _ CoARequest) error {
	return fmt.Errorf("%w: http adapter does not support SendCoA", ErrUnsupportedProtocol)
}

func (h *HTTPAdapter) SendDM(_ context.Context, _ DMRequest) error {
	return fmt.Errorf("%w: http adapter does not support SendDM", ErrUnsupportedProtocol)
}

func (h *HTTPAdapter) Authenticate(_ context.Context, _ AuthenticateRequest) (*AuthenticateResponse, error) {
	return nil, fmt.Errorf("%w: http adapter does not support Authenticate", ErrUnsupportedProtocol)
}

func (h *HTTPAdapter) AccountingUpdate(_ context.Context, _ AccountingUpdateRequest) error {
	return fmt.Errorf("%w: http adapter does not support AccountingUpdate", ErrUnsupportedProtocol)
}

func (h *HTTPAdapter) FetchAuthVectors(_ context.Context, _ string, _ int) ([]AuthVector, error) {
	return nil, fmt.Errorf("%w: http adapter does not support FetchAuthVectors", ErrUnsupportedProtocol)
}
