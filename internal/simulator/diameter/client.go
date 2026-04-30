package diameter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/btopcu/argus/internal/simulator/metrics"
	"github.com/btopcu/argus/internal/simulator/radius"
	"github.com/rs/zerolog"
)

// Client is the high-level façade over a single Diameter Peer for one operator.
// It provides session lifecycle methods (OpenSession/UpdateGy/CloseSession) that
// build and send CCR messages and wire Prometheus metrics automatically.
type Client struct {
	peer         *Peer
	operatorCode string
	originHost   string
	originRealm  string
	destRealm    string
	hasGx        bool
	hasGy        bool
	logger       zerolog.Logger

	// Per-session Gy CC-Request-Number counters keyed by AcctSessionID.
	// Counter starts at 0; first UpdateGy increments to 1, etc.
	// CloseSession reads the final value for CCR-T and removes the entry.
	gyCounters   map[string]*atomic.Uint32
	gyCountersMu sync.Mutex
}

// New creates a Client for the given operator config. The underlying Peer is
// constructed but not yet connected; call Start(ctx) to initiate the connection.
func New(cfg config.OperatorConfig, defaults config.DiameterDefaults, logger zerolog.Logger) *Client {
	log := logger.With().
		Str("component", "diameter_client").
		Str("operator", cfg.Code).
		Logger()

	hasGx, hasGy := false, false
	var appIDs []uint32
	if cfg.Diameter != nil && cfg.Diameter.Enabled {
		for _, app := range cfg.Diameter.Applications {
			switch strings.ToLower(app) {
			case "gx":
				hasGx = true
				appIDs = append(appIDs, argusdiameter.ApplicationIDGx)
			case "gy":
				hasGy = true
				appIDs = append(appIDs, argusdiameter.ApplicationIDGy)
			}
		}
	}

	originHost := ""
	if cfg.Diameter != nil {
		originHost = cfg.Diameter.OriginHost
	}

	c := &Client{
		operatorCode: cfg.Code,
		originHost:   originHost,
		originRealm:  defaults.OriginRealm,
		destRealm:    defaults.DestinationRealm,
		hasGx:        hasGx,
		hasGy:        hasGy,
		logger:       log,
		gyCounters:   make(map[string]*atomic.Uint32),
	}

	peerCfg := PeerConfig{
		OperatorCode:        cfg.Code,
		Host:                defaults.Host,
		Port:                defaults.Port,
		OriginHost:          originHost,
		OriginRealm:         defaults.OriginRealm,
		DestinationRealm:    defaults.DestinationRealm,
		AppIDs:              appIDs,
		WatchdogInterval:    defaults.WatchdogInterval,
		ConnectTimeout:      defaults.ConnectTimeout,
		RequestTimeout:      defaults.RequestTimeout,
		ReconnectBackoffMin: defaults.ReconnectBackoffMin,
		ReconnectBackoffMax: defaults.ReconnectBackoffMax,
		OnStateChange: func(s PeerState) {
			metrics.DiameterPeerState.WithLabelValues(cfg.Code).Set(float64(s))
		},
	}

	c.peer = NewPeer(peerCfg, logger)
	return c
}

// Start spawns the peer goroutine (using ctx for its lifetime) and returns a
// channel that is closed once the peer reaches Open for the first time, or when
// ctx is cancelled. Callers check peer.State() after close to distinguish success
// from timeout/cancel.
func (c *Client) Start(ctx context.Context) <-chan struct{} {
	go c.peer.Run(ctx)

	ready := make(chan struct{})
	go func() {
		defer close(ready)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if c.peer.State() == PeerStateOpen {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return ready
}

// OpenSession sends Gx CCR-I (always) then Gy CCR-I (if gy enabled).
// The returned error (wrapping ErrPeerNotOpen or a ResultCode failure) is
// what the engine inspects to classify DiameterSessionAbortedTotal. Client
// does NOT increment the counter itself — classification happens once at the
// engine boundary (engine.go) to prevent double-counting a single peer-down
// event under both "peer_down" and "ccr_i_failed" labels.
func (c *Client) OpenSession(ctx context.Context, sc *radius.SessionContext) error {
	if c.hasGx {
		if err := c.sendGxCCRI(ctx, sc); err != nil {
			return fmt.Errorf("gx ccr-i: %w", err)
		}
	}

	if c.hasGy {
		counter := &atomic.Uint32{}
		c.gyCountersMu.Lock()
		c.gyCounters[sc.AcctSessionID] = counter
		c.gyCountersMu.Unlock()

		if err := c.sendGyCCRI(ctx, sc); err != nil {
			// Session aborts before CloseSession is ever called — clean up
			// the counter here to prevent unbounded map growth over the
			// client's lifetime (F-A2).
			c.gyCountersMu.Lock()
			delete(c.gyCounters, sc.AcctSessionID)
			c.gyCountersMu.Unlock()
			return fmt.Errorf("gy ccr-i: %w", err)
		}
	}

	return nil
}

// UpdateGy sends a Gy CCR-U with a monotonically increasing CC-Request-Number.
// No-op if Gy is not enabled for this operator.
func (c *Client) UpdateGy(ctx context.Context, sc *radius.SessionContext, deltaIn, deltaOut uint64, deltaSec uint32) error {
	if !c.hasGy {
		return nil
	}

	c.gyCountersMu.Lock()
	counter := c.gyCounters[sc.AcctSessionID]
	c.gyCountersMu.Unlock()

	if counter == nil {
		c.logger.Warn().Str("session", sc.AcctSessionID).Msg("UpdateGy: no counter found, skipping")
		return nil
	}

	reqNum := counter.Add(1)
	hopID := c.peer.hopID.Add(1)
	endID := c.peer.NextEndID()
	msg := BuildGyCCRU(sc, c.originHost, c.originRealm, c.destRealm, hopID, endID, reqNum, deltaIn, deltaOut, deltaSec)
	return c.sendAndRecord(ctx, msg, "gy", "ccr_u")
}

// CloseSession sends Gx CCR-T and Gy CCR-T (whichever are applicable) and
// removes the per-session Gy counter. Returns the first error encountered.
func (c *Client) CloseSession(ctx context.Context, sc *radius.SessionContext) error {
	var firstErr error

	if c.hasGx {
		hopID := c.peer.hopID.Add(1)
		endID := c.peer.NextEndID()
		// Gx CC-Request-Number for CCR-T is literally 1 because Gx has only
		// Initial (0) and Termination (next) phases — no intermediate Update
		// phase exists (RFC 4006 §8.2; Argus never emits Gx CCR-U). If Gx
		// Update is ever added, replace this with a per-session counter like
		// Gy uses.
		msg := BuildGxCCRT(sc, c.originHost, c.originRealm, c.destRealm, hopID, endID, 1)
		if err := c.sendAndRecord(ctx, msg, "gx", "ccr_t"); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if c.hasGy {
		c.gyCountersMu.Lock()
		counter := c.gyCounters[sc.AcctSessionID]
		delete(c.gyCounters, sc.AcctSessionID)
		c.gyCountersMu.Unlock()

		var reqNum uint32
		if counter != nil {
			reqNum = counter.Add(1)
		} else {
			reqNum = 1
		}

		hopID := c.peer.hopID.Add(1)
		endID := c.peer.NextEndID()
		msg := BuildGyCCRT(sc, c.originHost, c.originRealm, c.destRealm, hopID, endID, reqNum, 0, 0, 0)
		if err := c.sendAndRecord(ctx, msg, "gy", "ccr_t"); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// GyEnabled reports whether this client is configured to send Gy CCR messages.
func (c *Client) GyEnabled() bool {
	return c.hasGy
}

// Stop closes the peer (DPR → DPA → TCP close).
func (c *Client) Stop(_ context.Context) error {
	c.peer.Close()
	return nil
}

// sendGxCCRI builds and sends a Gx CCR-I, recording metrics.
func (c *Client) sendGxCCRI(ctx context.Context, sc *radius.SessionContext) error {
	hopID := c.peer.hopID.Add(1)
	endID := c.peer.NextEndID()
	msg := BuildGxCCRI(sc, c.originHost, c.originRealm, c.destRealm, hopID, endID)
	return c.sendAndRecord(ctx, msg, "gx", "ccr_i")
}

// sendGyCCRI builds and sends a Gy CCR-I, recording metrics.
func (c *Client) sendGyCCRI(ctx context.Context, sc *radius.SessionContext) error {
	hopID := c.peer.hopID.Add(1)
	endID := c.peer.NextEndID()
	msg := BuildGyCCRI(sc, c.originHost, c.originRealm, c.destRealm, hopID, endID, argusdiameter.DefaultGrantedOctets)
	return c.sendAndRecord(ctx, msg, "gy", "ccr_i")
}

// sendAndRecord sends a Diameter message via the Peer and records all three
// metric families: DiameterRequestsTotal, DiameterLatencySeconds,
// DiameterResponsesTotal.
func (c *Client) sendAndRecord(ctx context.Context, msg *argusdiameter.Message, app, msgType string) error {
	start := time.Now()
	metrics.DiameterRequestsTotal.WithLabelValues(c.operatorCode, app, msgType).Inc()

	reply, err := c.peer.Send(ctx, msg)

	elapsed := time.Since(start).Seconds()
	metrics.DiameterLatencySeconds.WithLabelValues(c.operatorCode, app, msgType).Observe(elapsed)

	if err != nil {
		result := classifyError(err)
		metrics.DiameterResponsesTotal.WithLabelValues(c.operatorCode, app, msgType, result).Inc()
		return err
	}

	if reply == nil {
		metrics.DiameterResponsesTotal.WithLabelValues(c.operatorCode, app, msgType, "timeout").Inc()
		return ErrTimeout
	}

	rc := reply.GetResultCode()
	result := classifyResultCode(rc)
	metrics.DiameterResponsesTotal.WithLabelValues(c.operatorCode, app, msgType, result).Inc()

	if rc != argusdiameter.ResultCodeSuccess {
		return fmt.Errorf("diameter %s %s: result-code %d", app, msgType, rc)
	}
	return nil
}

// classifyError maps a Peer.Send error to a DiameterResponsesTotal result label.
func classifyError(err error) string {
	if errors.Is(err, ErrPeerNotOpen) {
		return "peer_down"
	}
	return "timeout"
}

// classifyResultCode maps a Diameter Result-Code to a result label string.
func classifyResultCode(rc uint32) string {
	if rc == argusdiameter.ResultCodeSuccess {
		return "success"
	}
	return fmt.Sprintf("error_%d", rc)
}
