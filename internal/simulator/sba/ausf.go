package sba

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	argussba "github.com/btopcu/argus/internal/aaa/sba"
	"github.com/btopcu/argus/internal/simulator/metrics"
)

// AuthenticateViaAUSF sends POST /nausf-auth/v1/ue-authentications for the
// given IMSI and serving network name, returning the auth-context confirmation
// href on success.
//
// The href value comes from Links["5g-aka"].Href in the server response and is
// used as the path for ConfirmAUSF.
func (c *Client) AuthenticateViaAUSF(ctx context.Context, imsi, servingNetworkName string) (authCtxHref string, err error) {
	start := time.Now()
	const service = "ausf"
	const endpoint = "authenticate"

	metrics.SBARequestsTotal.WithLabelValues(c.operatorCode, service, endpoint).Inc()

	reqBody := argussba.AuthenticationRequest{
		SUPIOrSUCI:         "imsi-" + imsi,
		ServingNetworkName: servingNetworkName,
		RequestedNSSAI:     c.slices,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return "", fmt.Errorf("%w: marshal auth request: %v", ErrTransport, err)
	}

	url := c.baseURL + "/nausf-auth/v1/ue-authentications"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return "", fmt.Errorf("%w: build request: %v", ErrTransport, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start).Seconds()
	metrics.SBALatencySeconds.WithLabelValues(c.operatorCode, service, endpoint).Observe(elapsed)

	if err != nil {
		result := classifyTransportError(err)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		if result == "timeout" {
			return "", fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return "", fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		result := classifyStatusCode(resp.StatusCode)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		cause := decodeCause(resp)
		metrics.SBAServiceErrorsTotal.WithLabelValues(c.operatorCode, service, cause).Inc()

		if cause != "" && cause != "unknown" {
			return "", fmt.Errorf("%w: %s (http %d)", ErrAuthFailed, cause, resp.StatusCode)
		}
		return "", fmt.Errorf("%w: unexpected status %d", ErrAuthFailed, resp.StatusCode)
	}

	var authResp argussba.AuthenticationResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return "", fmt.Errorf("%w: decode auth response: %v", ErrTransport, err)
	}

	link, ok := authResp.Links["5g-aka"]
	if !ok || link.Href == "" {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return "", fmt.Errorf("%w: missing 5g-aka link in response", ErrTransport)
	}

	metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "success").Inc()
	return link.Href, nil
}

// ConfirmAUSF sends PUT <authCtxHref> with the locally derived xresStar,
// returning the SUPI and Kseaf on success.
//
// xresStar is computed via generate5GAV (duplicated from internal/aaa/sba/ausf.go
// lines 340–348) — see crypto helpers below.
func (c *Client) ConfirmAUSF(ctx context.Context, authCtxHref, imsi, servingNetworkName string) (supi string, kseaf []byte, err error) {
	start := time.Now()
	const service = "ausf"
	const endpoint = "confirm"

	metrics.SBARequestsTotal.WithLabelValues(c.operatorCode, service, endpoint).Inc()

	_, _, xresStar, _ := generate5GAVSim("imsi-"+imsi, servingNetworkName)

	reqBody := argussba.ConfirmationRequest{
		ResStar: base64.StdEncoding.EncodeToString(xresStar),
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return "", nil, fmt.Errorf("%w: marshal confirm request: %v", ErrTransport, err)
	}

	url := c.baseURL + authCtxHref
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(buf))
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return "", nil, fmt.Errorf("%w: build confirm request: %v", ErrTransport, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start).Seconds()
	metrics.SBALatencySeconds.WithLabelValues(c.operatorCode, service, endpoint).Observe(elapsed)

	if err != nil {
		result := classifyTransportError(err)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		if result == "timeout" {
			return "", nil, fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return "", nil, fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		result := classifyStatusCode(resp.StatusCode)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		cause := decodeCause(resp)
		metrics.SBAServiceErrorsTotal.WithLabelValues(c.operatorCode, service, cause).Inc()

		if cause != "" && cause != "unknown" {
			return "", nil, fmt.Errorf("%w: %s (http %d)", ErrConfirmFailed, cause, resp.StatusCode)
		}
		return "", nil, fmt.Errorf("%w: unexpected status %d", ErrConfirmFailed, resp.StatusCode)
	}

	var confirmResp argussba.ConfirmationResponse
	if err := json.NewDecoder(resp.Body).Decode(&confirmResp); err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		return "", nil, fmt.Errorf("%w: decode confirm response: %v", ErrTransport, err)
	}

	// HTTP 200 is a success at the response-bucket level regardless of the
	// embedded AuthResult value. The session-layer abort (AuthResult != "SUCCESS")
	// is classified exactly once by the engine via ErrConfirmFailed
	// (single-writer pattern per STORY-083 F-A3).
	metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "success").Inc()

	if confirmResp.AuthResult != "SUCCESS" {
		return "", nil, fmt.Errorf("%w: auth result %q", ErrConfirmFailed, confirmResp.AuthResult)
	}

	kseafBytes, err := base64.StdEncoding.DecodeString(confirmResp.Kseaf)
	if err != nil {
		return "", nil, fmt.Errorf("%w: decode kseaf: %v", ErrTransport, err)
	}

	return confirmResp.SUPI, kseafBytes, nil
}

// --- Crypto helpers (duplicated from internal/aaa/sba/ausf.go lines 340–375) ---
// Source of truth: internal/aaa/sba/ausf.go
// Rationale: server helpers are unexported; the golden canary test in
// ausf_test.go catches any drift between this copy and the server implementation.

func generate5GAVSim(supi, servingNetwork string) (rand, autn, xresStar, kausf []byte) {
	seed := sha256SumSim([]byte("5g-av:" + supi + ":" + servingNetwork))
	rand = derivePseudoRandomSim(seed[:], 0, 16)
	autn = derivePseudoRandomSim(seed[:], 1, 16)
	xresStar = derivePseudoRandomSim(seed[:], 2, 16)
	kausf = derivePseudoRandomSim(seed[:], 3, 32)
	return
}

func sha256SumSim(data []byte) [32]byte {
	return sha256.Sum256(data)
}

func derivePseudoRandomSim(seed []byte, index int, length int) []byte {
	input := make([]byte, len(seed)+1)
	copy(input, seed)
	input[len(seed)] = byte(index)
	h := sha256.Sum256(input)
	if length > 32 {
		length = 32
	}
	return h[:length]
}

// --- Transport helpers ---

// classifyTransportError returns a metric result string for network-layer errors.
func classifyTransportError(err error) string {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return "timeout"
	}
	return "transport"
}

// classifyStatusCode maps HTTP status codes to the disjoint response-bucket
// enum specified in the plan: success | error_4xx | error_5xx | timeout | transport.
// Success is handled by the caller; this helper is only called on non-2xx codes.
func classifyStatusCode(status int) string {
	switch {
	case status >= 500:
		return "error_5xx"
	case status >= 400:
		return "error_4xx"
	default:
		// 1xx/3xx unexpectedly arriving here — treat as transport oddity.
		return "transport"
	}
}

// decodeCause extracts ProblemDetails.Cause from a non-2xx response body.
// Returns "unknown" when the body is not application/problem+json or the
// cause field is empty. Bounded by the server's own enum (MANDATORY_IE_INCORRECT,
// AUTH_REJECTED, SNSSAI_NOT_ALLOWED, AUTH_CONTEXT_NOT_FOUND, METHOD_NOT_ALLOWED,
// RESOURCE_NOT_FOUND, etc).
func decodeCause(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return "unknown"
	}
	var prob argussba.ProblemDetails
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		return "unknown"
	}
	if prob.Cause == "" {
		return "unknown"
	}
	return prob.Cause
}

// drainAndClose reads the remaining response body before closing so that
// keep-alive connections are returned to the pool.
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
