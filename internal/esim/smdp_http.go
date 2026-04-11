package esim

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

type HTTPSMDPConfig struct {
	BaseURL        string
	APIKey         string
	ClientCertPath string
	ClientKeyPath  string
	Timeout        time.Duration
}

type HTTPSMDPAdapter struct {
	cfg    HTTPSMDPConfig
	client *http.Client
	logger zerolog.Logger
}

func NewHTTPSMDPAdapter(cfg HTTPSMDPConfig, logger zerolog.Logger) (*HTTPSMDPAdapter, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if cfg.ClientCertPath != "" && cfg.ClientKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertPath, cfg.ClientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("smdp http: load client keypair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	transport := &http.Transport{
		TLSClientConfig: tlsCfg,
	}

	return &HTTPSMDPAdapter{
		cfg: cfg,
		client: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
		logger: logger.With().Str("component", "http_smdp").Logger(),
	}, nil
}

func (a *HTTPSMDPAdapter) do(ctx context.Context, method, path string, reqBody, respBody interface{}) (int, error) {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("smdp http: marshal request: %w", err)
	}

	var (
		lastStatus int
		lastErr    error
		delays     = []time.Duration{250 * time.Millisecond, 750 * time.Millisecond, 2 * time.Second}
	)

	url := a.cfg.BaseURL + path

	for attempt := 1; attempt <= 3; attempt++ {
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("smdp http: %w", ctx.Err())
		default:
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
		if err != nil {
			return 0, fmt.Errorf("smdp http: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("User-Agent", "argus-esim/1.0")
		if a.cfg.APIKey != "" {
			req.Header.Set("X-Api-Key", a.cfg.APIKey)
		}

		resp, err := a.client.Do(req)
		if err != nil {
			a.logger.Warn().
				Str("method", method).
				Str("path", path).
				Int("attempt", attempt).
				Err(err).
				Msg("smdp http: network error")
			lastErr = err
			if attempt < 3 {
				if !sleep(ctx, delays[attempt-1]) {
					return 0, fmt.Errorf("smdp http: %w", ctx.Err())
				}
			}
			continue
		}

		lastStatus = resp.StatusCode

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		a.logger.Info().
			Str("method", method).
			Str("path", path).
			Int("status", resp.StatusCode).
			Int("attempt", attempt).
			Msg("smdp http: response")

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusNoContent {
			if respBody != nil && len(body) > 0 {
				if err := json.Unmarshal(body, respBody); err != nil {
					return resp.StatusCode, fmt.Errorf("smdp http: unmarshal response: %w", err)
				}
			}
			return resp.StatusCode, nil
		}

		if resp.StatusCode == http.StatusNotFound {
			return resp.StatusCode, ErrSMDPProfileNotFound
		}

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			if readErr == nil {
				return resp.StatusCode, fmt.Errorf("%w: %s", ErrSMDPOperationFailed, string(body))
			}
			return resp.StatusCode, ErrSMDPOperationFailed
		}

		lastErr = fmt.Errorf("smdp http: server error %d", resp.StatusCode)
		if attempt < 3 {
			if !sleep(ctx, delays[attempt-1]) {
				return resp.StatusCode, fmt.Errorf("smdp http: %w", ctx.Err())
			}
		}
	}

	if lastErr != nil {
		return lastStatus, fmt.Errorf("%w: %w", ErrSMDPConnectionFailed, lastErr)
	}
	return lastStatus, ErrSMDPConnectionFailed
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (a *HTTPSMDPAdapter) DownloadProfile(ctx context.Context, req DownloadProfileRequest) (*DownloadProfileResponse, error) {
	a.logger.Info().
		Str("method", "DownloadProfile").
		Str("iccid", req.ICCID).
		Msg("smdp http: download profile")

	body := map[string]interface{}{
		"eid":        req.EID,
		"iccid":      req.ICCID,
		"smdpPlusId": req.SMDPPlusID,
		"operatorId": req.OperatorID.String(),
	}

	var result struct {
		ProfileID  string `json:"profileId"`
		ICCID      string `json:"iccid"`
		SMDPPlusID string `json:"smdpPlusId"`
	}

	_, err := a.do(ctx, http.MethodPost, "/gsma/rsp2/es9plus/downloadOrder", body, &result)
	if err != nil {
		return nil, err
	}

	return &DownloadProfileResponse{
		ProfileID:  result.ProfileID,
		ICCID:      result.ICCID,
		SMDPPlusID: result.SMDPPlusID,
	}, nil
}

func (a *HTTPSMDPAdapter) EnableProfile(ctx context.Context, req EnableProfileRequest) error {
	a.logger.Info().
		Str("method", "EnableProfile").
		Str("iccid", req.ICCID).
		Msg("smdp http: enable profile")

	body := map[string]interface{}{
		"eid":        req.EID,
		"iccid":      req.ICCID,
		"smdpPlusId": req.SMDPPlusID,
		"profileId":  req.ProfileID.String(),
	}

	_, err := a.do(ctx, http.MethodPost, "/gsma/rsp2/es9plus/confirmOrder", body, nil)
	return err
}

func (a *HTTPSMDPAdapter) DisableProfile(ctx context.Context, req DisableProfileRequest) error {
	a.logger.Info().
		Str("method", "DisableProfile").
		Str("iccid", req.ICCID).
		Msg("smdp http: disable profile")

	body := map[string]interface{}{
		"eid":        req.EID,
		"iccid":      req.ICCID,
		"smdpPlusId": req.SMDPPlusID,
		"profileId":  req.ProfileID.String(),
	}

	_, err := a.do(ctx, http.MethodPost, "/gsma/rsp2/es9plus/cancelOrder", body, nil)
	return err
}

func (a *HTTPSMDPAdapter) DeleteProfile(ctx context.Context, req DeleteProfileRequest) error {
	a.logger.Info().
		Str("method", "DeleteProfile").
		Str("iccid", req.ICCID).
		Msg("smdp http: delete profile")

	body := map[string]interface{}{
		"eid":        req.EID,
		"iccid":      req.ICCID,
		"smdpPlusId": req.SMDPPlusID,
		"profileId":  req.ProfileID.String(),
	}

	_, err := a.do(ctx, http.MethodPost, "/gsma/rsp2/es9plus/releaseProfile", body, nil)
	return err
}

func (a *HTTPSMDPAdapter) GetProfileInfo(ctx context.Context, req GetProfileInfoRequest) (*GetProfileInfoResponse, error) {
	a.logger.Info().
		Str("method", "GetProfileInfo").
		Str("iccid", req.ICCID).
		Msg("smdp http: get profile info")

	body := map[string]interface{}{
		"eid":       req.EID,
		"iccid":     req.ICCID,
		"profileId": req.ProfileID,
	}

	var result struct {
		State      string    `json:"state"`
		ICCID      string    `json:"iccid"`
		SMDPPlusID string    `json:"smdpPlusId"`
		LastSeenAt time.Time `json:"lastSeenAt"`
	}

	_, err := a.do(ctx, http.MethodPost, "/gsma/rsp2/es9plus/getProfileInfo", body, &result)
	if err != nil {
		return nil, err
	}

	return &GetProfileInfoResponse{
		State:      result.State,
		ICCID:      result.ICCID,
		SMDPPlusID: result.SMDPPlusID,
		LastSeenAt: result.LastSeenAt,
	}, nil
}
