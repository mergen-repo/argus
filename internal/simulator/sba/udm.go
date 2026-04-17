package sba

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	argussba "github.com/btopcu/argus/internal/aaa/sba"
	"github.com/btopcu/argus/internal/simulator/metrics"
)

// RegisterViaUDM sends PUT /nudm-uecm/v1/{supi}/registrations/amf-3gpp-access
// to register this simulator's AMF with the UDM.
//
// The server echoes the registration body with 201 Created on first registration
// or 200/204 on idempotent re-registration. Both are treated as success.
//
// See: internal/aaa/sba/udm.go HandleRegistration (line 131)
func (c *Client) RegisterViaUDM(ctx context.Context, supi, amfInstanceID string) error {
	start := time.Now()
	const service = "udm"
	const endpoint = "register"

	metrics.SBARequestsTotal.WithLabelValues(c.operatorCode, service, endpoint).Inc()

	body := argussba.Amf3GppAccessRegistration{
		AmfInstanceID:    amfInstanceID,
		DeregCallbackURI: c.deregCallbackURI,
		GUAMI: argussba.GUAMI{
			PlmnID: argussba.PlmnID{MCC: "286", MNC: "01"},
			AmfID:  "abc123",
		},
		RATType:       "NR",
		InitialRegInd: true,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return fmt.Errorf("%w: marshal registration body: %v", ErrTransport, err)
	}

	path := "/nudm-uecm/v1/" + url.PathEscape(supi) + "/registrations/amf-3gpp-access"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return fmt.Errorf("%w: build register request: %v", ErrTransport, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start).Seconds()
	metrics.SBALatencySeconds.WithLabelValues(c.operatorCode, service, endpoint).Observe(elapsed)

	if err != nil {
		result := classifyTransportError(err)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		if result == "timeout" {
			return fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer drainAndClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK, http.StatusNoContent:
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "success").Inc()
		return nil
	default:
		result := classifyStatusCode(resp.StatusCode)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		cause := decodeCause(resp)
		metrics.SBAServiceErrorsTotal.WithLabelValues(c.operatorCode, service, cause).Inc()

		if cause != "" && cause != "unknown" {
			return fmt.Errorf("%w: %s (http %d)", ErrServerError, cause, resp.StatusCode)
		}
		return fmt.Errorf("%w: registration returned %d", ErrServerError, resp.StatusCode)
	}
}

// GetSecurityInformation sends GET /nudm-ueau/v1/{supiOrSuci}/security-information?servingNetworkName=...
// to fetch a fresh authentication vector from the UDM. This is an optional call,
// gated on the IncludeOptionalCalls config flag + a per-session Bernoulli roll
// in the engine. The response body is not decoded — this call is pure traffic
// exercise to broaden SBA proxy coverage.
//
// See: internal/aaa/sba/udm.go HandleSecurityInfo (line 31)
func (c *Client) GetSecurityInformation(ctx context.Context, supiOrSuci, servingNetworkName string) error {
	start := time.Now()
	const service = "udm"
	const endpoint = "security-info"

	metrics.SBARequestsTotal.WithLabelValues(c.operatorCode, service, endpoint).Inc()

	path := "/nudm-ueau/v1/" + url.PathEscape(supiOrSuci) + "/security-information?servingNetworkName=" + url.QueryEscape(servingNetworkName)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return fmt.Errorf("%w: build security-info request: %v", ErrTransport, err)
	}

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start).Seconds()
	metrics.SBALatencySeconds.WithLabelValues(c.operatorCode, service, endpoint).Observe(elapsed)

	if err != nil {
		result := classifyTransportError(err)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		if result == "timeout" {
			return fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		result := classifyStatusCode(resp.StatusCode)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		cause := decodeCause(resp)
		metrics.SBAServiceErrorsTotal.WithLabelValues(c.operatorCode, service, cause).Inc()
		return fmt.Errorf("%w: security-information returned %d", ErrServerError, resp.StatusCode)
	}

	metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "success").Inc()
	return nil
}

// RecordAuthEvent sends POST /nudm-ueau/v1/{supiOrSuci}/auth-events to log a
// 5G-AKA auth event at the UDM. This is an optional call, gated on
// IncludeOptionalCalls + a per-session Bernoulli roll, issued by the engine at
// session end when a confirmation succeeded.
//
// See: internal/aaa/sba/udm.go HandleAuthEvents (line 77)
func (c *Client) RecordAuthEvent(ctx context.Context, supiOrSuci string, success bool) error {
	start := time.Now()
	const service = "udm"
	const endpoint = "auth-events"

	metrics.SBARequestsTotal.WithLabelValues(c.operatorCode, service, endpoint).Inc()

	event := argussba.AuthEvent{
		NfInstanceID:       c.amfInstanceID,
		Success:            success,
		TimeStamp:          time.Now().UTC().Format(time.RFC3339),
		AuthType:           "5G_AKA",
		ServingNetworkName: c.servingNetworkName,
	}
	buf, err := json.Marshal(event)
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return fmt.Errorf("%w: marshal auth event: %v", ErrTransport, err)
	}

	path := "/nudm-ueau/v1/" + url.PathEscape(supiOrSuci) + "/auth-events"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return fmt.Errorf("%w: build auth-event request: %v", ErrTransport, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start).Seconds()
	metrics.SBALatencySeconds.WithLabelValues(c.operatorCode, service, endpoint).Observe(elapsed)

	if err != nil {
		result := classifyTransportError(err)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		if result == "timeout" {
			return fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer drainAndClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK, http.StatusNoContent:
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "success").Inc()
		return nil
	default:
		result := classifyStatusCode(resp.StatusCode)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		cause := decodeCause(resp)
		metrics.SBAServiceErrorsTotal.WithLabelValues(c.operatorCode, service, cause).Inc()
		return fmt.Errorf("%w: auth-events returned %d", ErrServerError, resp.StatusCode)
	}
}
