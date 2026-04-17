// Command simulator synthesizes realistic RADIUS client traffic against
// Argus for development and demo purposes. See docs/stories/test-infra/
// STORY-082-plan.md for design + acceptance criteria.
//
// Strict opt-in: exits non-zero at startup if SIMULATOR_ENABLED is not
// set to a truthy value. Guards against accidental production activation.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/btopcu/argus/internal/simulator/diameter"
	"github.com/btopcu/argus/internal/simulator/discovery"
	"github.com/btopcu/argus/internal/simulator/engine"
	"github.com/btopcu/argus/internal/simulator/metrics"
	simradius "github.com/btopcu/argus/internal/simulator/radius"
	"github.com/btopcu/argus/internal/simulator/reactive"
	"github.com/btopcu/argus/internal/simulator/sba"
	"github.com/btopcu/argus/internal/simulator/scenario"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

const defaultConfigPath = "/etc/simulator/config.yaml"

func main() {
	if !isEnabled() {
		fmt.Fprintln(os.Stderr, "ERROR: SIMULATOR_ENABLED env var must be set to a truthy value (1/true/yes)")
		os.Exit(2)
	}

	logger := initLogger("info", "console") // will be reconfigured after config load

	cfg, err := config.Load(defaultConfigPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("load config")
	}
	logger = initLogger(cfg.Log.Level, cfg.Log.Format)
	logger.Info().
		Str("radius_host", cfg.Argus.RadiusHost).
		Int("operators", len(cfg.Operators)).
		Int("scenarios", len(cfg.Scenarios)).
		Msg("simulator starting")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Metrics HTTP server — start early so liveness probe works even if
	// discovery fails.
	metrics.MustRegister(prometheus.DefaultRegisterer)
	metricsSrv := &http.Server{
		Addr:    cfg.Metrics.Listen,
		Handler: metrics.Handler(),
	}
	go func() {
		logger.Info().Str("addr", cfg.Metrics.Listen).Msg("metrics server listening")
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error().Err(err).Msg("metrics server error")
		}
	}()

	// Discovery
	store, err := discovery.New(ctx, cfg.Argus.DBURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("discovery db connect")
	}
	defer store.Close()

	sims, err := store.ListActiveSIMs(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("list sims")
	}
	logger.Info().Int("sims", len(sims)).Msg("SIMs discovered")
	if len(sims) == 0 {
		logger.Warn().Msg("no active SIMs — nothing to simulate; exiting cleanly")
		_ = metricsSrv.Shutdown(context.Background())
		return
	}

	// Diameter clients — one per operator with diameter.enabled: true.
	dmClients := make(map[string]*diameter.Client)
	for _, op := range cfg.Operators {
		if op.Diameter == nil || !op.Diameter.Enabled {
			continue
		}
		c := diameter.New(op, cfg.Diameter, logger)
		ready := c.Start(ctx)
		connectDeadline := cfg.Diameter.ConnectTimeout + 5*time.Second
		select {
		case <-ready:
			logger.Info().Str("operator", op.Code).Msg("diameter peer ready")
		case <-time.After(connectDeadline):
			logger.Warn().
				Str("operator", op.Code).
				Dur("deadline", connectDeadline).
				Msg("diameter peer not ready within deadline; will retry in background")
		case <-ctx.Done():
		}
		dmClients[op.Code] = c
	}

	// SBA clients — one per operator with sba.enabled: true.
	sbaClients := make(map[string]*sba.Client)
	for _, op := range cfg.Operators {
		if op.SBA == nil || !op.SBA.Enabled {
			continue
		}
		c := sba.New(op, cfg.SBA, logger)
		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		if err := c.Ping(pingCtx); err != nil {
			logger.Warn().Err(err).Str("operator", op.Code).Msg("sba ping failed; will retry on first session")
		} else {
			logger.Info().Str("operator", op.Code).Msg("sba client ready")
		}
		pingCancel()
		sbaClients[op.Code] = c
	}

	// Reactive subsystem — process-wide (one listener, one tracker, one registry).
	var reactiveSub *reactive.Subsystem
	if cfg.Reactive.Enabled {
		reg := reactive.NewRegistry()
		tracker := reactive.NewRejectTracker(cfg.Reactive)

		var coaListener *reactive.Listener
		if cfg.Reactive.CoAListener.Enabled {
			coaListener = reactive.NewListener(reactive.ListenerConfig{
				Addr:     cfg.Reactive.CoAListener.ListenAddr,
				Secret:   []byte(cfg.Reactive.CoAListener.SharedSecret),
				Registry: reg,
				Logger:   logger.With().Str("component", "reactive-listener").Logger(),
			})
			if err := coaListener.Start(ctx); err != nil {
				logger.Warn().Err(err).Msg("reactive CoA listener failed to start — continuing with reactive enabled but listener disabled")
				coaListener = nil
			} else {
				// Start closes Ready() synchronously before returning nil, so a
				// select/timeout block here would be unreachable. Log directly.
				logger.Info().Str("addr", cfg.Reactive.CoAListener.ListenAddr).Msg("reactive CoA listener ready")
			}
		}

		reactiveSub = &reactive.Subsystem{
			Cfg:      cfg.Reactive,
			Rejects:  tracker,
			Registry: reg,
			Listener: coaListener,
		}
	}

	logger.Info().
		Bool("reactive", cfg.Reactive.Enabled).
		Bool("coa_listener", reactiveSub != nil && reactiveSub.Listener != nil).
		Msg("reactive subsystem ready")

	// Engine
	picker := scenario.New(cfg.Scenarios, 0) // 0 = wall-time seed
	client := simradius.New(
		cfg.Argus.RadiusHost,
		cfg.Argus.RadiusAuthPort,
		cfg.Argus.RadiusAccountingPort,
		cfg.Argus.RadiusSharedSecret,
	)
	eng := engine.New(cfg, picker, client, dmClients, sbaClients, reactiveSub, logger)

	// Signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info().Str("signal", sig.String()).Msg("shutdown signal received; draining")
		cancel()
	}()

	if err := eng.Run(ctx, sims); err != nil {
		logger.Error().Err(err).Msg("engine exited with error")
	}
	logger.Info().Int("active_at_exit", eng.ActiveCount()).Msg("engine drained")

	// Reactive listener shutdown — stop accepting CoA/DM packets before
	// tearing down SBA and Diameter so no new reactive events arrive during teardown.
	if reactiveSub != nil && reactiveSub.Listener != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := reactiveSub.Listener.Stop(stopCtx); err != nil {
			logger.Warn().Err(err).Msg("reactive listener stop returned error")
		}
		stopCancel()
	}

	// SBA client shutdown — close idle HTTP connections before Diameter and
	// metrics teardown so final metric writes are not lost.
	if len(sbaClients) > 0 {
		shutdownSBA, cancelSBA := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelSBA()
		for opCode, c := range sbaClients {
			if err := c.Stop(shutdownSBA); err != nil {
				logger.Warn().Err(err).Str("operator", opCode).Msg("sba stop error")
			}
		}
	}

	// Diameter client shutdown — send DPR and close TCP connections before
	// tearing down the metrics server so final state writes are not lost.
	if len(dmClients) > 0 {
		shutdownDm, cancelDm := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelDm()
		for opCode, c := range dmClients {
			if err := c.Stop(shutdownDm); err != nil {
				logger.Warn().Err(err).Str("operator", opCode).Msg("diameter stop error")
			}
		}
	}

	// Metrics shutdown
	shutdownCtx, stopMetrics := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopMetrics()
	_ = metricsSrv.Shutdown(shutdownCtx)
	logger.Info().Msg("simulator stopped cleanly")
}

func isEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SIMULATOR_ENABLED")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func initLogger(level, format string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	var w = os.Stdout
	if strings.ToLower(format) == "console" {
		return zerolog.New(zerolog.ConsoleWriter{Out: w, TimeFormat: time.RFC3339}).
			With().Timestamp().Str("service", "simulator").Logger()
	}
	return zerolog.New(w).With().Timestamp().Str("service", "simulator").Logger()
}
