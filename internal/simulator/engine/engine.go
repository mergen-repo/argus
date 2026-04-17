// Package engine orchestrates one goroutine per discovered SIM. Each
// goroutine loops:
//
//	pick scenario → Access-Request → Accounting-Start → N × Interim → Stop
//	→ idle gap → repeat
//
// A global rate limiter throttles outgoing RADIUS packets to prevent
// startup bursts from overwhelming Argus. Shutdown is graceful: on
// context cancellation, every in-flight session sends Accounting-Stop
// before the goroutine exits.
//
// When an operator opts into 5G-SBA (sba.enabled: true), a configurable
// fraction of that operator's sessions skip RADIUS entirely and run the
// SBA lifecycle instead (POST authenticate → PUT confirm → PUT register).
// This is an ALTERNATIVE path, not additive: a session is either RADIUS
// or SBA, never both.
package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/btopcu/argus/internal/simulator/diameter"
	"github.com/btopcu/argus/internal/simulator/discovery"
	"github.com/btopcu/argus/internal/simulator/metrics"
	simradius "github.com/btopcu/argus/internal/simulator/radius"
	"github.com/btopcu/argus/internal/simulator/sba"
	"github.com/btopcu/argus/internal/simulator/scenario"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"layeh.com/radius"
	"layeh.com/radius/rfc2866"
)

const maxSessionDuration = 4 * time.Hour // safety cap independent of scenario

type Engine struct {
	cfg     *config.Config
	picker  *scenario.Picker
	client  *simradius.Client
	dm      map[string]*diameter.Client // operator code → Diameter client; nil = RADIUS-only
	sba     map[string]*sba.Client     // operator code → SBA client; nil = SBA disabled for that op
	limiter *rate.Limiter
	logger  zerolog.Logger

	mu       sync.Mutex
	active   map[string]*sessionState // key = SIM.ID
}

type sessionState struct {
	sc     *simradius.SessionContext
	cancel context.CancelFunc
}

// New creates an Engine. dmClients maps operator codes to Diameter clients for
// operators with Diameter enabled; sbaClients maps operator codes to SBA
// clients for operators with 5G-SBA enabled. Pass nil (or an empty map) for
// either to disable that protocol — backward compatible with pre-STORY-083 and
// pre-STORY-084 callers.
func New(cfg *config.Config, picker *scenario.Picker, client *simradius.Client, dmClients map[string]*diameter.Client, sbaClients map[string]*sba.Client, logger zerolog.Logger) *Engine {
	return &Engine{
		cfg:     cfg,
		picker:  picker,
		client:  client,
		dm:      dmClients,
		sba:     sbaClients,
		limiter: rate.NewLimiter(rate.Limit(cfg.Rate.MaxRadiusRequestsPerSecond), cfg.Rate.MaxRadiusRequestsPerSecond),
		logger:  logger.With().Str("component", "engine").Logger(),
		active:  make(map[string]*sessionState),
	}
}

// Run launches one goroutine per SIM and returns once ctx is cancelled AND
// every in-flight session has been cleanly stopped.
func (e *Engine) Run(ctx context.Context, sims []discovery.SIM) error {
	var wg sync.WaitGroup
	for i := range sims {
		sim := sims[i]
		op := e.cfg.OperatorByCode(sim.OperatorCode)
		if op == nil {
			e.logger.Warn().
				Str("sim_id", sim.ID).
				Str("operator", sim.OperatorCode).
				Msg("no operator config; skipping sim")
			continue
		}
		jitter := e.picker.JitterDuration(e.cfg.Rate.InitialJitterSeconds[0], e.cfg.Rate.InitialJitterSeconds[1])
		wg.Add(1)
		go func(sim discovery.SIM, op *config.OperatorConfig, jitter time.Duration) {
			defer wg.Done()
			select {
			case <-time.After(jitter):
			case <-ctx.Done():
				return
			}
			e.runSIM(ctx, sim, op)
		}(sim, op, jitter)
	}
	wg.Wait()
	return nil
}

// runSIM is the per-SIM scheduler. It runs scenarios back-to-back with
// small idle gaps until the context is cancelled.
func (e *Engine) runSIM(ctx context.Context, sim discovery.SIM, op *config.OperatorConfig) {
	log := e.logger.With().Str("sim_id", sim.ID).Str("imsi", sim.IMSI).Str("operator", sim.OperatorCode).Logger()

	for {
		if ctx.Err() != nil {
			return
		}
		sample := e.picker.Pick()
		metrics.ScenarioStartsTotal.WithLabelValues(sim.OperatorCode, sample.Name).Inc()
		e.runSession(ctx, sim, op, sample, log)

		// idle gap 5-60s between sessions (fixed range — scenario-agnostic)
		gap := e.picker.JitterDuration(5, 60)
		select {
		case <-time.After(gap):
		case <-ctx.Done():
			return
		}
	}
}

// runSession drives a single Auth → Acct-Start → Interim(s) → Acct-Stop
// lifecycle. If any step fails, the session is abandoned (no Stop sent
// for an un-Started session, which would confuse Argus).
//
// If the operator has SBA enabled and the per-session picker selects "sba",
// the function dispatches to runSBASession and returns immediately.
// 5G-SBA REPLACES the RADIUS+Diameter flow — UE attaches over SBA directly
// in a 5G SA deployment. The fork point is before any RADIUS packet is sent.
func (e *Engine) runSession(ctx context.Context, sim discovery.SIM, op *config.OperatorConfig, sample scenario.Sample, log zerolog.Logger) {
	sc := simradius.NewSessionContext(sim, op.NASIP, op.NASIdentifier)

	// Protocol fork — MUST be before any RADIUS packet is sent.
	// When sbaC is non-nil AND the deterministic picker selects "sba" for
	// this session-id, the entire RADIUS+Diameter path is skipped.
	if sbaC := e.sba[sim.OperatorCode]; sbaC != nil {
		pickerSC := sba.SessionContext{AcctSessionID: sc.AcctSessionID}
		rate := float64(0)
		if op.SBA != nil {
			rate = op.SBA.Rate
		}
		if sba.ShouldUseSBA(pickerSC, rate) {
			e.runSBASession(ctx, sim, op, sample, sbaC, log)
			return
		}
	}

	// Authenticate
	if err := e.limiter.Wait(ctx); err != nil {
		return
	}
	metrics.RadiusRequestsTotal.WithLabelValues(sim.OperatorCode, "auth").Inc()
	t0 := time.Now()
	resp, err := e.client.Auth(ctx, sc)
	metrics.RadiusLatencySeconds.WithLabelValues(sim.OperatorCode, "auth").Observe(time.Since(t0).Seconds())
	if err != nil {
		metrics.RadiusResponsesTotal.WithLabelValues(sim.OperatorCode, "auth", "error").Inc()
		log.Debug().Err(err).Msg("auth error")
		return
	}
	result := responseBucket(resp.Code)
	metrics.RadiusResponsesTotal.WithLabelValues(sim.OperatorCode, "auth", result).Inc()
	if resp.Code != radius.CodeAccessAccept {
		log.Debug().Str("code", resp.Code.String()).Msg("auth rejected")
		return
	}

	// Diameter open (if enabled for this operator) — must succeed before AcctStart.
	// On failure the session is aborted and DiameterSessionAbortedTotal gets
	// incremented exactly once with a reason classified from the error (F-A3
	// double-count fix): ErrPeerNotOpen → "peer_down", anything else (CCA
	// result != 2001, request timeout, transport error) → "ccr_i_failed".
	dmClient := e.dm[sim.OperatorCode]
	if dmClient != nil {
		if err := dmClient.OpenSession(ctx, sc); err != nil {
			reason := "ccr_i_failed"
			if errors.Is(err, diameter.ErrPeerNotOpen) {
				reason = "peer_down"
			}
			metrics.DiameterSessionAbortedTotal.WithLabelValues(sim.OperatorCode, reason).Inc()
			log.Debug().Err(err).Msg("diameter open-session failed; aborting session")
			return
		}
	}

	// Accounting-Start
	if err := e.limiter.Wait(ctx); err != nil {
		return
	}
	metrics.RadiusRequestsTotal.WithLabelValues(sim.OperatorCode, "acct_start").Inc()
	t0 = time.Now()
	_, err = e.client.AcctStart(ctx, sc)
	metrics.RadiusLatencySeconds.WithLabelValues(sim.OperatorCode, "acct_start").Observe(time.Since(t0).Seconds())
	if err != nil {
		metrics.RadiusResponsesTotal.WithLabelValues(sim.OperatorCode, "acct_start", "error").Inc()
		log.Debug().Err(err).Msg("acct-start error")
		return
	}
	metrics.RadiusResponsesTotal.WithLabelValues(sim.OperatorCode, "acct_start", "accept").Inc()
	metrics.ActiveSessions.WithLabelValues(sim.OperatorCode).Inc()
	defer metrics.ActiveSessions.WithLabelValues(sim.OperatorCode).Dec()
	e.mu.Lock()
	sessionCtx, cancel := context.WithCancel(ctx)
	e.active[sim.ID] = &sessionState{sc: sc, cancel: cancel}
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.active, sim.ID)
		e.mu.Unlock()
		cancel()
	}()

	// Interim loop
	duration := sample.SessionDuration
	if duration > maxSessionDuration {
		duration = maxSessionDuration
	}
	deadline := sc.StartedAt.Add(duration)
	ticker := time.NewTicker(sample.InterimInterval)
	defer ticker.Stop()

	stopCause := rfc2866.AcctTerminateCause_Value_UserRequest
	for {
		select {
		case <-sessionCtx.Done():
			// Graceful shutdown — still send Stop below
			stopCause = rfc2866.AcctTerminateCause_Value_AdminReboot
			goto stop
		case <-ticker.C:
			if time.Now().After(deadline) {
				goto stop
			}
			sc.BytesIn += uint64(sample.BytesPerInterimIn)
			sc.BytesOut += uint64(sample.BytesPerInterimOut)
			sc.PacketsIn += uint64(sample.BytesPerInterimIn / 1500)
			sc.PacketsOut += uint64(sample.BytesPerInterimOut / 1500)

			if err := e.limiter.Wait(sessionCtx); err != nil {
				goto stop
			}
			metrics.RadiusRequestsTotal.WithLabelValues(sim.OperatorCode, "acct_interim").Inc()
			t0 := time.Now()
			_, err := e.client.AcctInterim(sessionCtx, sc)
			metrics.RadiusLatencySeconds.WithLabelValues(sim.OperatorCode, "acct_interim").Observe(time.Since(t0).Seconds())
			if err != nil {
				metrics.RadiusResponsesTotal.WithLabelValues(sim.OperatorCode, "acct_interim", "error").Inc()
				log.Debug().Err(err).Msg("interim error")
				continue
			}
			metrics.RadiusResponsesTotal.WithLabelValues(sim.OperatorCode, "acct_interim", "accept").Inc()

			// Gy CCR-U — non-fatal; skip if peer is down.
			if dmClient != nil {
				deltaIn := uint64(sample.BytesPerInterimIn)
				deltaOut := uint64(sample.BytesPerInterimOut)
				deltaSec := uint32(sample.InterimInterval.Seconds())
				if err := dmClient.UpdateGy(sessionCtx, sc, deltaIn, deltaOut, deltaSec); err != nil {
					log.Debug().Err(err).Msg("diameter gy update failed (non-fatal)")
				}
			}
		}
	}

stop:
	// Final Stop — use a fresh context with small deadline so shutdown
	// doesn't block forever if Argus is unresponsive.
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	if err := e.limiter.Wait(stopCtx); err != nil {
		return
	}
	metrics.RadiusRequestsTotal.WithLabelValues(sim.OperatorCode, "acct_stop").Inc()
	t0 = time.Now()
	_, err = e.client.AcctStop(stopCtx, sc, stopCause)
	metrics.RadiusLatencySeconds.WithLabelValues(sim.OperatorCode, "acct_stop").Observe(time.Since(t0).Seconds())
	if err != nil {
		metrics.RadiusResponsesTotal.WithLabelValues(sim.OperatorCode, "acct_stop", "error").Inc()
		log.Debug().Err(err).Msg("acct-stop error")
	} else {
		metrics.RadiusResponsesTotal.WithLabelValues(sim.OperatorCode, "acct_stop", "accept").Inc()
	}

	// Diameter close — always attempt after AcctStop regardless of its result.
	if dmClient != nil {
		if closeErr := dmClient.CloseSession(stopCtx, sc); closeErr != nil {
			log.Debug().Err(closeErr).Msg("diameter close-session failed (non-fatal)")
		}
	}
}

// runSBASession executes the 5G-SBA lifecycle for one session:
//  1. Authenticate (POST /nausf-auth/…) + Confirm (PUT …/5g-aka-confirmation)
//  2. Register AMF (PUT /nudm-uecm/…/registrations/amf-3gpp-access)
//  3. Hold session for sample.SessionDuration (bounded by maxSessionDuration).
//
// This path REPLACES RADIUS for sessions selected by ShouldUseSBA. No RADIUS
// or Diameter packets are sent. The ActiveSessions gauge is managed identically
// to the RADIUS path. Session-abort metrics follow the single-writer pattern
// from STORY-083 F-A3.
func (e *Engine) runSBASession(ctx context.Context, sim discovery.SIM, op *config.OperatorConfig, sample scenario.Sample, sbaC *sba.Client, log zerolog.Logger) {
	sc := simradius.NewSessionContext(sim, op.NASIP, op.NASIdentifier)

	if err := sbaC.Authenticate(ctx, sc); err != nil {
		reason := "auth_failed"
		switch {
		case errors.Is(err, sba.ErrConfirmFailed):
			reason = "confirm_failed"
		case errors.Is(err, sba.ErrTimeout):
			reason = "timeout"
		case errors.Is(err, sba.ErrTransport):
			reason = "transport"
		}
		metrics.SBASessionAbortedTotal.WithLabelValues(sim.OperatorCode, reason).Inc()
		log.Debug().Err(err).Str("reason", reason).Msg("sba authenticate failed; aborting session")
		return
	}

	supi := "imsi-" + sc.SIM.IMSI
	if err := sbaC.Register(ctx, sc, supi); err != nil {
		metrics.SBASessionAbortedTotal.WithLabelValues(sim.OperatorCode, "register_failed").Inc()
		log.Debug().Err(err).Msg("sba register failed; aborting session")
		return
	}

	metrics.ActiveSessions.WithLabelValues(sim.OperatorCode).Inc()
	defer metrics.ActiveSessions.WithLabelValues(sim.OperatorCode).Dec()

	duration := sample.SessionDuration
	if duration > maxSessionDuration {
		duration = maxSessionDuration
	}

	select {
	case <-time.After(duration):
	case <-ctx.Done():
	}

	// Optional session-end auth-event POST (gated inside the client on
	// IncludeOptionalCalls + Bernoulli roll). Best-effort; any error is logged
	// and discarded inside RecordSessionEnd. Uses a bounded fresh context so
	// shutdown cannot hang on an unresponsive UDM.
	aeCtx, cancelAE := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelAE()
	sbaC.RecordSessionEnd(aeCtx, sc, true)
}

// ActiveCount returns the number of currently-active sessions.
func (e *Engine) ActiveCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.active)
}

// responseBucket maps a RADIUS response code into a metrics label value.
func responseBucket(code radius.Code) string {
	switch code {
	case radius.CodeAccessAccept, radius.CodeAccountingResponse:
		return "accept"
	case radius.CodeAccessReject:
		return "reject"
	default:
		return fmt.Sprintf("code_%d", code)
	}
}
