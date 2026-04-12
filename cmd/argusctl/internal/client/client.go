package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config configures a Client.
type Config struct {
	BaseURL    string
	Token      string
	CertFile   string
	KeyFile    string
	CAFile     string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client is a thin HTTP client for the Argus admin API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a Client from Config. If CertFile/KeyFile are supplied, an mTLS
// *http.Client is constructed. If HTTPClient is supplied in the config it
// overrides the default (useful for tests).
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("client: base URL is required")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")

	if cfg.HTTPClient != nil {
		return &Client{baseURL: base, token: cfg.Token, http: cfg.HTTPClient}, nil
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	transport := &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if cfg.CertFile != "" || cfg.KeyFile != "" || cfg.CAFile != "" {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

		if cfg.CertFile != "" && cfg.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("client: load mTLS keypair: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}

		if cfg.CAFile != "" {
			caBytes, err := os.ReadFile(cfg.CAFile)
			if err != nil {
				return nil, fmt.Errorf("client: read CA bundle: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caBytes) {
				return nil, fmt.Errorf("client: no valid PEM certs in CA bundle %s", cfg.CAFile)
			}
			tlsCfg.RootCAs = pool
		}

		transport.TLSClientConfig = tlsCfg
	}

	return &Client{
		baseURL: base,
		token:   cfg.Token,
		http: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}, nil
}

// APIError is returned when the server responds with a non-2xx status in the
// standard error envelope.
type APIError struct {
	Status     int
	Code       string
	Message    string
	Details    json.RawMessage
	RawBody    []byte
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("server returned %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("server returned %d: %s", e.Status, e.Message)
}

type successEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Meta   json.RawMessage `json:"meta,omitempty"`
}

type errorEnvelope struct {
	Status string `json:"status"`
	Error  struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Details json.RawMessage `json:"details,omitempty"`
	} `json:"error"`
}

// Do executes an HTTP request against the Argus API and, on a 2xx response,
// JSON-decodes the response envelope's `data` field into `out`. If `out` is
// nil the response body is discarded. On non-2xx responses an *APIError is
// returned.
func (c *Client) Do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	return c.do(ctx, method, path, nil, body, out)
}

// DoQuery is like Do but accepts URL query parameters.
func (c *Client) DoQuery(ctx context.Context, method, path string, query url.Values, body interface{}, out interface{}) error {
	return c.do(ctx, method, path, query, body, out)
}

// DoRaw executes the request and returns the raw response envelope data bytes
// plus meta bytes on success. Useful for commands that render the raw JSON.
func (c *Client) DoRaw(ctx context.Context, method, path string, body interface{}) (json.RawMessage, json.RawMessage, error) {
	env, err := c.doEnvelope(ctx, method, path, nil, body)
	if err != nil {
		return nil, nil, err
	}
	return env.Data, env.Meta, nil
}

// DoStream executes a request and writes the raw response body directly to out
// on a 2xx response. This is useful for binary or non-JSON endpoints (e.g.,
// PDF/CSV downloads) where the standard JSON envelope is not present.
// On non-2xx, the error envelope is parsed and an *APIError is returned.
func (c *Client) DoStream(ctx context.Context, method, path string, query url.Values, out io.Writer) error {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		if strings.Contains(reqURL, "?") {
			reqURL += "&" + query.Encode()
		} else {
			reqURL += "?" + query.Encode()
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return fmt.Errorf("client: build request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("client: request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if _, err := io.Copy(out, resp.Body); err != nil {
			return fmt.Errorf("client: stream response: %w", err)
		}
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("client: read error body: %w", err)
	}
	var errEnv errorEnvelope
	if err := json.Unmarshal(respBody, &errEnv); err == nil && errEnv.Error.Code != "" {
		return &APIError{
			Status:  resp.StatusCode,
			Code:    errEnv.Error.Code,
			Message: errEnv.Error.Message,
			Details: errEnv.Error.Details,
			RawBody: respBody,
		}
	}
	msg := strings.TrimSpace(string(respBody))
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &APIError{
		Status:  resp.StatusCode,
		Code:    "UNEXPECTED_RESPONSE",
		Message: msg,
		RawBody: respBody,
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body interface{}, out interface{}) error {
	env, err := c.doEnvelope(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("client: decode response data: %w", err)
	}
	return nil
}

func (c *Client) doEnvelope(ctx context.Context, method, path string, query url.Values, body interface{}) (*successEnvelope, error) {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		if strings.Contains(reqURL, "?") {
			reqURL += "&" + query.Encode()
		} else {
			reqURL += "?" + query.Encode()
		}
	}

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("client: encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("client: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("client: request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("client: read response body: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if len(respBody) == 0 {
			return &successEnvelope{Status: "success"}, nil
		}
		var env successEnvelope
		if err := json.Unmarshal(respBody, &env); err != nil {
			// Non-JSON body. Wrap it so callers can still read it.
			return &successEnvelope{Status: "success", Data: respBody}, nil
		}
		// If the body is valid JSON but does NOT match the standard
		// envelope (no `data` field), fall back to raw bytes so endpoints
		// like /health/ready round-trip cleanly through DoRaw.
		if env.Status != "success" || len(env.Data) == 0 {
			return &successEnvelope{Status: "success", Data: respBody}, nil
		}
		return &env, nil
	}

	// Error path.
	var errEnv errorEnvelope
	if err := json.Unmarshal(respBody, &errEnv); err == nil && errEnv.Error.Code != "" {
		return nil, &APIError{
			Status:  resp.StatusCode,
			Code:    errEnv.Error.Code,
			Message: errEnv.Error.Message,
			Details: errEnv.Error.Details,
			RawBody: respBody,
		}
	}
	// Non-enveloped error (e.g., gateway 502, nginx HTML).
	msg := strings.TrimSpace(string(respBody))
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return nil, &APIError{
		Status:  resp.StatusCode,
		Code:    "UNEXPECTED_RESPONSE",
		Message: msg,
		RawBody: respBody,
	}
}
