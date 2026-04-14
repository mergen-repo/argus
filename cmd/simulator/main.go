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
	"github.com/btopcu/argus/internal/simulator/discovery"
	"github.com/btopcu/argus/internal/simulator/engine"
	"github.com/btopcu/argus/internal/simulator/metrics"
	simradius "github.com/btopcu/argus/internal/simulator/radius"
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

	// Engine
	picker := scenario.New(cfg.Scenarios, 0) // 0 = wall-time seed
	client := simradius.New(
		cfg.Argus.RadiusHost,
		cfg.Argus.RadiusAuthPort,
		cfg.Argus.RadiusAccountingPort,
		cfg.Argus.RadiusSharedSecret,
	)
	eng := engine.New(cfg, picker, client, logger)

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
