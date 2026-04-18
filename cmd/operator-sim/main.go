package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/btopcu/argus/internal/operatorsim/config"
	"github.com/btopcu/argus/internal/operatorsim/server"
	"github.com/rs/zerolog"
)

const defaultConfigPath = "/etc/operator-sim/config.yaml"

func main() {
	configPath := flag.String("config", defaultConfigPath, "path to operator-sim config YAML")
	flag.Parse()

	// NOTE: ARGUS_OPERATOR_SIM_CONFIG env override is applied inside
	// config.Load (see internal/operatorsim/config/config.go). The env read
	// is intentionally NOT duplicated here — config.Load is the single
	// authoritative entrypoint.

	logger := initLogger("info", "console")

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal().Err(err).Msg("load config")
	}
	logger = initLogger(cfg.Log.Level, cfg.Log.Format)

	logger.Info().
		Str("listen", cfg.Server.Listen).
		Str("metrics_listen", cfg.Server.MetricsListen).
		Int("operators", len(cfg.Operators)).
		Msg("operator-sim starting")

	srv := server.New(cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info().Str("signal", sig.String()).Msg("shutdown signal received; draining")
		cancel()
	}()

	if err := srv.Run(ctx); err != nil {
		logger.Error().Err(err).Msg("server exited with error")
		os.Exit(1)
	}

	logger.Info().Msg("operator-sim stopped cleanly")
}

func initLogger(level, format string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	w := os.Stdout
	if strings.ToLower(format) == "console" {
		return zerolog.New(zerolog.ConsoleWriter{Out: w, TimeFormat: time.RFC3339}).
			With().Timestamp().Str("service", "operator-sim").Logger()
	}
	return zerolog.New(w).With().Timestamp().Str("service", "operator-sim").Logger()
}
