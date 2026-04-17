package sba

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	argussba "github.com/btopcu/argus/internal/aaa/sba"
	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/btopcu/argus/internal/simulator/radius"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
)

// Sentinel errors returned by Client methods. The engine classifies session
// aborts by checking errors.Is against these values; no HTTP status codes leak
// outside this package.
var (
	ErrAuthFailed    = errors.New("sba: authentication failed")
	ErrConfirmFailed = errors.New("sba: confirmation failed")
	ErrTimeout       = errors.New("sba: request timeout")
	ErrTransport     = errors.New("sba: transport error")
	ErrServerError   = errors.New("sba: server error")
)

// Client is the high-level façade over the 5G SBA endpoints for one operator.
// It holds a single *http.Client shared across all sessions for that operator,
// providing connection-pool reuse.
//
// Wave 2 will fill in Authenticate, RegisterAMF, and related methods;
// for Wave 1 the stubs exist so the package compiles and the picker tests pass.
type Client struct {
	baseURL            string
	httpClient         *http.Client
	operatorCode       string
	servingNetworkName string
	amfInstanceID      string
	deregCallbackURI   string
	slices             []argussba.SNSSAI
	includeOptional    bool
	logger             zerolog.Logger
	rnd                *rand.Rand // for optional-call Bernoulli roll
}

// New constructs a Client for the given operator. The HTTP transport is
// configured once at construction time: HTTP/1.1 cleartext when TLSEnabled is
// false (matching the dev compose default); HTTP/2 (via ALPN) when TLSEnabled
// is true.
//
// If the operator config supplies per-operator slices, they override the
// default [{SST:1, SD:"000001"}]. Applied by config.validateSBA when operator
// opt-in is present and Slices is empty.
//
// No connection is made during construction. Ping(ctx) warms up the first
// connection and surfaces DNS/TLS issues at startup.
func New(op config.OperatorConfig, defaults config.SBADefaults, logger zerolog.Logger) *Client {
	scheme := "http"
	var transport http.RoundTripper

	if defaults.TLSEnabled {
		scheme = "https"
		t := &http.Transport{
			MaxIdleConnsPerHost: 10,
			ForceAttemptHTTP2:   true,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: defaults.TLSSkipVerify,
				NextProtos:         []string{"h2", "http/1.1"},
			},
		}
		if err := http2.ConfigureTransport(t); err != nil {
			logger.Warn().Err(err).Msg("sba: failed to configure HTTP/2 transport; falling back to HTTP/1.1")
		}
		transport = t
	} else {
		transport = &http.Transport{
			MaxIdleConnsPerHost: 10,
		}
	}

	baseURL := fmt.Sprintf("%s://%s:%d", scheme, defaults.Host, defaults.Port)

	log := logger.With().
		Str("component", "sba_client").
		Str("operator", op.Code).
		Str("base_url", baseURL).
		Logger()

	// Slices: per-operator override (if set) else default. validateSBA applies
	// [{SST:1, SD:"000001"}] when operator opts in and leaves Slices empty, so
	// this fallback is defensive for direct constructor callers.
	slices := []argussba.SNSSAI{{SST: 1, SD: "000001"}}
	if op.SBA != nil && len(op.SBA.Slices) > 0 {
		slices = slices[:0]
		for _, s := range op.SBA.Slices {
			slices = append(slices, argussba.SNSSAI{SST: s.SST, SD: s.SD})
		}
	}

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   defaults.RequestTimeout,
		},
		operatorCode:       op.Code,
		servingNetworkName: defaults.ServingNetworkName,
		amfInstanceID:      defaults.AMFInstanceID,
		deregCallbackURI:   defaults.DeregCallbackURI,
		slices:             slices,
		includeOptional:    defaults.IncludeOptionalCalls,
		logger:             log,
		rnd:                rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Ping sends a GET /health to verify connectivity and warm up the connection
// pool. Returns a classified sentinel error on failure.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("%w: build health request: %v", ErrTransport, err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: health returned %d", ErrServerError, resp.StatusCode)
	}
	return nil
}

// Stop closes idle connections in the transport's pool. The engine calls this
// during graceful shutdown after all sessions have finished.
func (c *Client) Stop(_ context.Context) error {
	if t, ok := c.httpClient.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	return nil
}

// Authenticate performs the full 5G-AKA authentication flow (POST authenticate
// + PUT confirm) for the given session. On success the SUPI and Kseaf are
// discarded at this layer; Wave 3 engine wiring will thread them further.
// Returns a classified sentinel error on failure.
//
// When IncludeOptionalCalls is true, a per-session 20% Bernoulli roll prepends
// GET /nudm-ueau/v1/{supi}/security-information to the flow. Failure of the
// optional call is logged and non-fatal — it does not abort the session.
func (c *Client) Authenticate(ctx context.Context, sc *radius.SessionContext) error {
	if ctx == nil || sc == nil {
		return nil
	}

	supi := "imsi-" + sc.SIM.IMSI

	if c.includeOptional && c.rnd != nil && c.rnd.Float64() < 0.2 {
		if err := c.GetSecurityInformation(ctx, supi, c.servingNetworkName); err != nil {
			c.logger.Debug().Err(err).Msg("sba: optional security-information call failed (non-fatal)")
		}
	}

	href, err := c.AuthenticateViaAUSF(ctx, sc.SIM.IMSI, c.servingNetworkName)
	if err != nil {
		return err
	}
	_, _, err = c.ConfirmAUSF(ctx, href, sc.SIM.IMSI, c.servingNetworkName)
	return err
}

// ConfirmAuth sends the 5G-AKA confirmation PUT for an already-initiated auth
// context. authCtxHref is the href returned by AuthenticateViaAUSF. Returns
// the SUPI and Kseaf on success.
func (c *Client) ConfirmAuth(ctx context.Context, authCtxHref, imsi string) (supi string, kseaf []byte, err error) {
	return c.ConfirmAUSF(ctx, authCtxHref, imsi, c.servingNetworkName)
}

// Register performs AMF registration at the UDM for the given session's SIM.
// It calls RegisterViaUDM with the SIM's IMSI as the SUPI and the configured
// amfInstanceID. Returns a wrapped sentinel error on failure.
func (c *Client) Register(ctx context.Context, sc *radius.SessionContext, supi string) error {
	if ctx == nil || sc == nil {
		return nil
	}
	return c.RegisterViaUDM(ctx, supi, c.amfInstanceID)
}

// RecordSessionEnd emits an optional auth-events POST at session end when
// IncludeOptionalCalls is true and the per-session Bernoulli roll triggers.
// Failure is logged and discarded — this is pure traffic exercise.
func (c *Client) RecordSessionEnd(ctx context.Context, sc *radius.SessionContext, success bool) {
	if ctx == nil || sc == nil || !c.includeOptional {
		return
	}
	if c.rnd == nil || c.rnd.Float64() >= 0.2 {
		return
	}
	supi := "imsi-" + sc.SIM.IMSI
	if err := c.RecordAuthEvent(ctx, supi, success); err != nil {
		c.logger.Debug().Err(err).Msg("sba: optional auth-events call failed (non-fatal)")
	}
}

// RunSession executes the complete 5G-SBA lifecycle for one SIM:
//  1. Authenticate via AUSF (POST authenticate + PUT confirm).
//  2. Register AMF at UDM (PUT registration).
//  3. Hold the session for sample duration OR until context cancellation.
//
// Session-abort metrics are emitted by the engine (single-writer pattern per
// STORY-083 F-A3). RunSession returns wrapped sentinel errors so the caller
// can classify. The caller is responsible for bounding the hold duration via
// context.WithTimeout or context.WithDeadline.
func (c *Client) RunSession(ctx context.Context, sc *radius.SessionContext) error {
	if ctx == nil || sc == nil {
		return nil
	}

	if err := c.Authenticate(ctx, sc); err != nil {
		return err
	}

	supi := "imsi-" + sc.SIM.IMSI
	if err := c.Register(ctx, sc, supi); err != nil {
		// Engine is the single writer for SBASessionAbortedTotal; RunSession
		// returns the wrapped sentinel error and lets the caller classify.
		return err
	}

	<-ctx.Done()

	// Best-effort optional auth-events at session end.
	aeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.RecordSessionEnd(aeCtx, sc, true)

	return ctx.Err()
}
