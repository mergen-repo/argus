package sba

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	argussba "github.com/btopcu/argus/internal/aaa/sba"
	"github.com/btopcu/argus/internal/simulator/metrics"
)

// NsmfPath is the base path for the mock Nsmf_PDUSession endpoint exposed by
// the Argus SBA server (internal/aaa/sba/server.go, STORY-092 Wave 3). Keep
// in sync with the server-side mount.
const NsmfPath = "/nsmf-pdusession/v1"

// createSMContextResponse mirrors argussba.CreateSMContextResponse but is
// decoded locally to avoid a dependency cycle on the server's request body
// type. Only the fields the simulator consumes are declared — extra fields
// in the wire response are ignored by json.Decoder.
type createSMContextResponse struct {
	SUPI          string              `json:"supi"`
	DNN           string              `json:"dnn"`
	SNSSAI        argussba.SNSSAI     `json:"sNssai"`
	UEIPv4Address string              `json:"ueIpv4Address"`
}

// CreatePDUSession POSTs /nsmf-pdusession/v1/sm-contexts with the given SUPI /
// DNN / sNssai and returns the smContextRef (for later Release) plus the
// allocated UE IPv4 address.
//
// Errors returned are sentinel-wrapped (errors.Is matches):
//   - ErrPDUSessionFailed  — server returned non-201 (with ProblemDetails cause surfaced)
//   - ErrTimeout           — context timeout on the request
//   - ErrTransport         — network-layer failure or decode error
//
// Metrics emitted (delta pattern, labels per plan §Task 7b):
//   - SBARequestsTotal{operator,service="nsmf",endpoint="create"}
//   - SBAResponsesTotal{operator,service="nsmf",endpoint="create",result}
//   - SBALatencySeconds{operator,service="nsmf",endpoint="create"}
//   - SBAPDUSessionsTotal{operator,result}   (ok / pool_exhausted / user_not_found / transport_error / timeout)
//   - SBAServiceErrorsTotal{operator,service="nsmf",cause} on non-2xx with ProblemDetails body.
func (c *Client) CreatePDUSession(ctx context.Context, supi, dnn string, sNssai argussba.SNSSAI) (smContextRef string, ueIpv4 string, err error) {
	start := time.Now()
	const service = "nsmf"
	const endpoint = "create"

	metrics.SBARequestsTotal.WithLabelValues(c.operatorCode, service, endpoint).Inc()

	reqBody := argussba.CreateSMContextRequest{
		SUPI:           supi,
		DNN:            dnn,
		SNSSAI:         sNssai,
		PDUSessionID:   1,
		ServingNetwork: c.servingNetworkName,
		ANType:         "3GPP_ACCESS",
		RATType:        "NR",
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "transport_error").Inc()
		return "", "", fmt.Errorf("%w: marshal create-sm-context: %v", ErrTransport, err)
	}

	url := c.baseURL + NsmfPath + "/sm-contexts"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "transport_error").Inc()
		return "", "", fmt.Errorf("%w: build create-sm-context: %v", ErrTransport, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start).Seconds()
	metrics.SBALatencySeconds.WithLabelValues(c.operatorCode, service, endpoint).Observe(elapsed)

	if err != nil {
		result := classifyTransportError(err)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		pduResult := "transport_error"
		if result == "timeout" {
			pduResult = "timeout"
		}
		metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, pduResult).Inc()
		if result == "timeout" {
			return "", "", fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return "", "", fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		result := classifyStatusCode(resp.StatusCode)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		cause := decodeCause(resp)
		metrics.SBAServiceErrorsTotal.WithLabelValues(c.operatorCode, service, cause).Inc()

		pduResult := classifyPDUCause(cause)
		metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, pduResult).Inc()

		if cause != "" && cause != "unknown" {
			return "", "", fmt.Errorf("%w: %s (http %d)", ErrPDUSessionFailed, cause, resp.StatusCode)
		}
		return "", "", fmt.Errorf("%w: unexpected status %d", ErrPDUSessionFailed, resp.StatusCode)
	}

	// Prefer the Location header over the body for the smContextRef, per
	// TS 29.502 §6.1.3.2.3.1 (response to CreateSMContext includes a Location
	// header with the resource URI). Fall back to the trailing path segment
	// if the server omits it.
	smContextRef = extractRefFromLocation(resp.Header.Get("Location"))

	var body createSMContextResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "transport_error").Inc()
		return "", "", fmt.Errorf("%w: decode create-sm-context response: %v", ErrTransport, err)
	}

	metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "success").Inc()
	metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "ok").Inc()
	return smContextRef, body.UEIPv4Address, nil
}

// ReleasePDUSession DELETEs /nsmf-pdusession/v1/sm-contexts/{smContextRef} to
// return the UE IP to the pool. Errors:
//   - ErrPDUSessionFailed  — server returned non-204 (404 for unknown ref, 4xx/5xx otherwise)
//   - ErrTimeout           — context timeout
//   - ErrTransport         — network-layer failure
//
// Release is best-effort at the lifecycle level: the engine logs failures but
// does not abort shutdown on them. Metrics emitted mirror CreatePDUSession
// with endpoint="release".
func (c *Client) ReleasePDUSession(ctx context.Context, smContextRef string) error {
	if smContextRef == "" {
		// Nothing to release — not an error condition; the create leg either
		// didn't run or already failed, and its error path counted the abort.
		return nil
	}

	start := time.Now()
	const service = "nsmf"
	const endpoint = "release"

	metrics.SBARequestsTotal.WithLabelValues(c.operatorCode, service, endpoint).Inc()

	url := c.baseURL + NsmfPath + "/sm-contexts/" + smContextRef
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "transport").Inc()
		metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "transport_error").Inc()
		return fmt.Errorf("%w: build release-sm-context: %v", ErrTransport, err)
	}

	resp, err := c.httpClient.Do(httpReq)
	elapsed := time.Since(start).Seconds()
	metrics.SBALatencySeconds.WithLabelValues(c.operatorCode, service, endpoint).Observe(elapsed)

	if err != nil {
		result := classifyTransportError(err)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		pduResult := "transport_error"
		if result == "timeout" {
			pduResult = "timeout"
		}
		metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, pduResult).Inc()
		if result == "timeout" {
			return fmt.Errorf("%w: %v", ErrTimeout, err)
		}
		return fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusNoContent {
		result := classifyStatusCode(resp.StatusCode)
		metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, result).Inc()
		cause := decodeCause(resp)
		metrics.SBAServiceErrorsTotal.WithLabelValues(c.operatorCode, service, cause).Inc()
		metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, classifyPDUCause(cause)).Inc()

		if cause != "" && cause != "unknown" {
			return fmt.Errorf("%w: %s (http %d)", ErrPDUSessionFailed, cause, resp.StatusCode)
		}
		return fmt.Errorf("%w: unexpected status %d", ErrPDUSessionFailed, resp.StatusCode)
	}

	metrics.SBAResponsesTotal.WithLabelValues(c.operatorCode, service, endpoint, "success").Inc()
	metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "ok").Inc()
	return nil
}

// classifyPDUCause maps a ProblemDetails cause string into the disjoint
// SBAPDUSessionsTotal result enum. Unknown causes collapse to "transport_error"
// so dashboards still surface aberrant server states.
func classifyPDUCause(cause string) string {
	switch cause {
	case "INSUFFICIENT_RESOURCES":
		return "pool_exhausted"
	case "USER_NOT_FOUND":
		return "user_not_found"
	default:
		return "transport_error"
	}
}

// extractRefFromLocation pulls the trailing smContextRef path segment from a
// Location header like "/nsmf-pdusession/v1/sm-contexts/{smContextRef}". Returns
// "" when the header is missing or malformed.
func extractRefFromLocation(loc string) string {
	const prefix = NsmfPath + "/sm-contexts/"
	if loc == "" {
		return ""
	}
	// Handle both absolute URLs ("http://host/…") and path-only values.
	if idx := indexStrPrefix(loc, prefix); idx >= 0 {
		rest := loc[idx+len(prefix):]
		// Strip query / fragment if any.
		for i := 0; i < len(rest); i++ {
			if rest[i] == '?' || rest[i] == '#' || rest[i] == '/' {
				return rest[:i]
			}
		}
		return rest
	}
	return ""
}

// indexStrPrefix finds the first occurrence of sub inside s. Duplicates
// strings.Index without the import cycle on strings (file already uses
// bytes). Local helper — 8 LoC, no stdlib dep added.
func indexStrPrefix(s, sub string) int {
	if len(sub) == 0 || len(sub) > len(s) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

