package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	aaadiameter "github.com/btopcu/argus/internal/aaa/diameter"
	aaaradius "github.com/btopcu/argus/internal/aaa/radius"
	aaasba "github.com/btopcu/argus/internal/aaa/sba"
	aaasession "github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/analytics/aggregates"
	anomalysvc "github.com/btopcu/argus/internal/analytics/anomaly"
	"github.com/btopcu/argus/internal/analytics/digest"
	cdrsvc "github.com/btopcu/argus/internal/analytics/cdr"
	costsvc "github.com/btopcu/argus/internal/analytics/cost"
	analyticmetrics "github.com/btopcu/argus/internal/analytics/metrics"
	adminapi "github.com/btopcu/argus/internal/api/admin"
	alertapi "github.com/btopcu/argus/internal/api/alert"
	analyticsapi "github.com/btopcu/argus/internal/api/analytics"
	announcementapi "github.com/btopcu/argus/internal/api/announcement"
	anomalyapi "github.com/btopcu/argus/internal/api/anomaly"
	apikeyapi "github.com/btopcu/argus/internal/api/apikey"
	apnapi "github.com/btopcu/argus/internal/api/apn"
	auditapi "github.com/btopcu/argus/internal/api/audit"
	authapi "github.com/btopcu/argus/internal/api/auth"
	cdrapi "github.com/btopcu/argus/internal/api/cdr"
	dashboardapi "github.com/btopcu/argus/internal/api/dashboard"
	diagapi "github.com/btopcu/argus/internal/api/diagnostics"
	esimapi "github.com/btopcu/argus/internal/api/esim"
	eventsapi "github.com/btopcu/argus/internal/api/events"
	ippoolapi "github.com/btopcu/argus/internal/api/ippool"
	jobapi "github.com/btopcu/argus/internal/api/job"
	metricsapi "github.com/btopcu/argus/internal/api/metrics"
	msisdnapi "github.com/btopcu/argus/internal/api/msisdn"
	notifapi "github.com/btopcu/argus/internal/api/notification"
	onboardingapi "github.com/btopcu/argus/internal/api/onboarding"
	operatorapi "github.com/btopcu/argus/internal/api/operator"
	opsapi "github.com/btopcu/argus/internal/api/ops"
	otaapi "github.com/btopcu/argus/internal/api/ota"
	policyapi "github.com/btopcu/argus/internal/api/policy"
	reportsapi "github.com/btopcu/argus/internal/api/reports"
	searchapi "github.com/btopcu/argus/internal/api/search"
	segmentapi "github.com/btopcu/argus/internal/api/segment"
	sessionapi "github.com/btopcu/argus/internal/api/session"
	simapi "github.com/btopcu/argus/internal/api/sim"
	slaapi "github.com/btopcu/argus/internal/api/sla"
	smsapi "github.com/btopcu/argus/internal/api/sms"
	systemapi "github.com/btopcu/argus/internal/api/system"
	tenantapi "github.com/btopcu/argus/internal/api/tenant"
	undoapi "github.com/btopcu/argus/internal/api/undo"
	userapi "github.com/btopcu/argus/internal/api/user"
	violationapi "github.com/btopcu/argus/internal/api/violation"
	webhookapi "github.com/btopcu/argus/internal/api/webhooks"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/cache"
	"github.com/btopcu/argus/internal/compliance"
	"github.com/btopcu/argus/internal/config"
	diagnosticspkg "github.com/btopcu/argus/internal/diagnostics"
	esimpkg "github.com/btopcu/argus/internal/esim"
	"github.com/btopcu/argus/internal/events"
	"github.com/btopcu/argus/internal/gateway"
	"github.com/btopcu/argus/internal/geoip"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/killswitch"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/observability"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/operator"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/btopcu/argus/internal/ota"
	"github.com/btopcu/argus/internal/policy"
	"github.com/btopcu/argus/internal/policy/dryrun"
	policyenforcer "github.com/btopcu/argus/internal/policy/enforcer"
	"github.com/btopcu/argus/internal/policy/rollout"
	"github.com/btopcu/argus/internal/report"
	"github.com/btopcu/argus/internal/smsr"
	"github.com/btopcu/argus/internal/storage"
	"github.com/btopcu/argus/internal/store"
	"github.com/btopcu/argus/internal/store/schemacheck"
	"github.com/btopcu/argus/internal/undo"
	"github.com/btopcu/argus/internal/ws"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	version   = "dev"
	gitSHA    = "unknown"
	buildTime = "unknown"
)

func main() {
	sub, subArgs, err := parseSubcommand(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "argus: "+err.Error())
		fmt.Fprintln(os.Stderr, "usage: argus [serve|migrate|seed|version|--help]")
		os.Exit(1)
	}

	switch sub {
	case "help":
		fmt.Println("usage: argus <subcommand> [args]")
		fmt.Println("")
		fmt.Println("subcommands:")
		fmt.Println("  serve              start the HTTP/RADIUS/Diameter/SBA servers (default)")
		fmt.Println("  migrate up         run all pending migrations")
		fmt.Println("  migrate down [N]   roll back N migrations (default 1; -all = all)")
		fmt.Println("  seed [file]        execute seed SQL files")
		fmt.Println("  version            print build version and exit")
		fmt.Println("  --help / -h        show this help")
		return
	case "version":
		fmt.Println("version:", version)
		fmt.Println("git_sha:", gitSHA)
		fmt.Println("build_time:", buildTime)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	lvl, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)
	zerolog.TimeFieldFormat = time.RFC3339

	if cfg.IsDev() {
		log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
			With().Timestamp().Str("service", "argus").Logger()
	} else {
		log.Logger = zerolog.New(os.Stdout).
			With().Timestamp().Str("service", "argus").Logger()
	}

	switch sub {
	case "migrate":
		runMigrate(cfg, subArgs)
		return
	case "seed":
		runSeed(cfg, subArgs)
		return
	case "repair-audit":
		runRepairAudit(cfg)
		return
	}

	runServe(cfg)
}

// parseSubcommand parses os.Args[1:] into a subcommand name and its remaining
// args. It is a standalone function so it can be unit-tested without os.Args
// munging.
func parseSubcommand(args []string) (sub string, subArgs []string, err error) {
	if len(args) == 0 {
		return "serve", nil, nil
	}
	switch args[0] {
	case "serve":
		return "serve", args[1:], nil
	case "migrate":
		return "migrate", args[1:], nil
	case "seed":
		return "seed", args[1:], nil
	case "repair-audit":
		return "repair-audit", nil, nil
	case "version":
		return "version", nil, nil
	case "--help", "-h":
		return "help", nil, nil
	default:
		return "", nil, fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// runMigrationsInProcess applies pending migrations (up-only) before the
// application pool opens. FIX-301: eliminates the boot race where pgx caches
// pre-migration relation OIDs in connection-level prepared-statement caches,
// then DDL DROP/CREATE leaves stale OIDs and produces "could not open relation
// with OID" until restart.
//
// golang-migrate uses a Postgres advisory lock (keyed on the DSN), so multi-
// replica boots are safe — losers wait for the winner and observe ErrNoChange.
// Up-only here: down migrations remain exclusive to `argus migrate down`.
// Returns nil on success or migrate.ErrNoChange. Caller decides whether to fatal.
func runMigrationsInProcess(cfg *config.Config) error {
	migrationsPath := os.Getenv("ARGUS_MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "file:///app/migrations"
	}

	m, err := migrate.New(migrationsPath, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("auto-migrate: create migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			log.Error().Err(srcErr).Msg("auto-migrate: source close error")
		}
		if dbErr != nil {
			log.Error().Err(dbErr).Msg("auto-migrate: db close error")
		}
	}()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info().Msg("auto-migrate: schema already at latest version")
			return nil
		}
		return fmt.Errorf("auto-migrate: up failed: %w", err)
	}

	v, dirty, vErr := m.Version()
	if vErr != nil {
		log.Info().Msg("auto-migrate: applied (version unavailable)")
		return nil
	}
	log.Info().Uint("version", v).Bool("dirty", dirty).Msg("auto-migrate: applied")
	return nil
}

// runMigrate executes database migrations using golang-migrate.
func runMigrate(cfg *config.Config, args []string) {
	migrationsPath := os.Getenv("ARGUS_MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "file:///app/migrations"
	}

	m, err := migrate.New(migrationsPath, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("migrate: failed to create migrator")
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			log.Error().Err(srcErr).Msg("migrate: source close error")
		}
		if dbErr != nil {
			log.Error().Err(dbErr).Msg("migrate: db close error")
		}
	}()

	direction := "up"
	if len(args) > 0 {
		direction = args[0]
	}

	switch direction {
	case "up":
		if err := m.Up(); err != nil {
			if errors.Is(err, migrate.ErrNoChange) {
				log.Info().Msg("migrate: no change — already at latest version")
				return
			}
			log.Fatal().Err(err).Msg("migrate: up failed")
		}
	case "down":
		if len(args) > 1 && args[1] == "-all" {
			if err := m.Down(); err != nil {
				if errors.Is(err, migrate.ErrNoChange) {
					log.Info().Msg("migrate: no change — already at base")
					return
				}
				log.Fatal().Err(err).Msg("migrate: down all failed")
			}
		} else {
			n := 1
			if len(args) > 1 {
				parsed, parseErr := strconv.Atoi(args[1])
				if parseErr != nil {
					log.Fatal().Err(parseErr).Str("arg", args[1]).Msg("migrate: invalid step count")
				}
				n = parsed
			}
			if err := m.Steps(-n); err != nil {
				if errors.Is(err, migrate.ErrNoChange) {
					log.Info().Msg("migrate: no change — already at base")
					return
				}
				log.Fatal().Err(err).Msg("migrate: down failed")
			}
		}
	default:
		log.Fatal().Str("direction", direction).Msg("migrate: unknown direction (use 'up' or 'down')")
	}

	v, dirty, err := m.Version()
	if err != nil {
		log.Info().Msg("migrate: completed (version unavailable)")
		return
	}
	log.Info().Uint("version", v).Bool("dirty", dirty).Msg("migrate: completed")
}

// runSeed executes seed SQL files against the database.
func runSeed(cfg *config.Config, args []string) {
	seedPath := os.Getenv("ARGUS_SEED_PATH")
	if seedPath == "" {
		seedPath = "/app/migrations/seed"
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("seed: failed to connect to database")
	}
	defer pool.Close()

	var files []string
	if len(args) > 0 && args[0] != "" {
		f := args[0]
		if !filepath.IsAbs(f) {
			f = filepath.Join(seedPath, f)
		}
		files = []string{f}
	} else {
		matches, err := filepath.Glob(filepath.Join(seedPath, "*.sql"))
		if err != nil {
			log.Fatal().Err(err).Msg("seed: failed to glob seed directory")
		}
		sort.Strings(matches)
		files = matches
	}

	if len(files) == 0 {
		log.Info().Str("path", seedPath).Msg("seed: no SQL files found")
		return
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Fatal().Err(err).Str("file", f).Msg("seed: failed to read file")
		}

		tag, err := pool.Exec(ctx, string(data))
		if err != nil {
			log.Fatal().Err(err).Str("file", filepath.Base(f)).Msg("seed: execution failed")
		}
		log.Info().Str("file", filepath.Base(f)).Int64("rows_affected", tag.RowsAffected()).Msg("seed: file executed")
	}

	log.Info().Int("files", len(files)).Msg("seed: completed")
}

func runRepairAudit(cfg *config.Config) {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("repair-audit: failed to connect to database")
	}
	defer pool.Close()

	auditStore := store.NewAuditStore(pool)

	log.Info().Msg("repair-audit: repairing hash chain...")
	if err := auditStore.RepairChain(ctx); err != nil {
		log.Fatal().Err(err).Msg("repair-audit: failed")
	}

	svc := audit.NewFullService(auditStore, nil, log.Logger)
	result, err := svc.VerifyChain(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("repair-audit: verify read failed")
	}
	if !result.Verified {
		log.Fatal().Int64("first_invalid", *result.FirstInvalid).Msg("repair-audit: chain still invalid after repair")
	}

	log.Info().Int("entries", result.TotalRows).Msg("repair-audit: chain verified successfully")
}

func runServe(cfg *config.Config) {
	if cfg.GOGC != 100 {
		debug.SetGCPercent(cfg.GOGC)
		log.Info().Int("gogc", cfg.GOGC).Msg("GOGC tuned")
	}

	if cfg.PprofEnabled || cfg.IsDev() {
		go func() {
			pprofAddr := cfg.PprofAddr
			mux := http.NewServeMux()
			mux.Handle("/debug/pprof/", http.DefaultServeMux)
			var pprofHandler http.Handler = mux
			if cfg.IsDev() {
				log.Info().Str("addr", pprofAddr).Str("mode", "open").Msg("pprof server starting (endpoints: /debug/pprof/)")
			} else {
				pprofHandler = gateway.PprofGuard(cfg.PprofToken)(mux)
				log.Info().Str("addr", pprofAddr).Str("mode", "guarded").Msg("pprof server starting (endpoints: /debug/pprof/)")
			}
			if err := http.ListenAndServe(pprofAddr, pprofHandler); err != nil {
				log.Error().Err(err).Msg("pprof server error")
			}
		}()
	}

	bootID := uuid.New().String()

	log.Info().Str("env", cfg.AppEnv).Int("port", cfg.AppPort).Msg("starting argus")
	// FIX-235 T15: SMSR callback secret is required in production.
	if cfg.AppEnv == "production" && cfg.SMSRCallbackSecret == "" {
		log.Fatal().Msg("SMSR_CALLBACK_SECRET must be set in production environment")
	}

	// appCtx is a long-lived context for background goroutines (pool gauge,
	// health pollers) that must outlive one-shot init timeouts. Cancelled in
	// the graceful shutdown block below before closing infrastructure.
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// --- Observability init (STORY-065) ---
	metricsReg := obsmetrics.NewRegistry()
	metricsReg.BuildInfo.WithLabelValues(version, gitSHA, buildTime).Set(1)
	log.Info().Str("version", version).Str("git_sha", gitSHA).Str("build_time", buildTime).Msg("argus build info")
	auth.JWTVerifyHook = metricsReg.IncJWTVerify

	otelInitCtx, otelInitCancel := context.WithTimeout(appCtx, 10*time.Second)
	otelShutdown, err := observability.Init(otelInitCtx, observability.Config{
		Endpoint:         cfg.OTELExporterOTLPEndpoint,
		SamplerRatio:     cfg.OTELSamplerRatio,
		ServiceName:      cfg.OTELServiceName,
		ServiceVersion:   cfg.OTELServiceVersion,
		DeploymentEnv:    cfg.OTELDeploymentEnvironment,
		ExportTimeoutSec: cfg.OTELBSPExportTimeoutSec,
	}, log.Logger)
	otelInitCancel()
	if err != nil {
		log.Fatal().Err(err).Msg("otel init failed")
	}
	// NOTE: otelShutdown is intentionally NOT deferred — it must run before
	// NATS/Redis/DB close so in-flight spans flush with infra still alive.
	// See graceful shutdown block below.

	ctx, cancel := context.WithTimeout(appCtx, 30*time.Second)
	defer cancel()

	// FIX-301: Run pending migrations in-process BEFORE opening the application
	// pool. Eliminates the boot race where boot tasks warm the pool against the
	// pre-migration schema and pgx caches stale relation OIDs.
	if cfg.AutoMigrate {
		if err := runMigrationsInProcess(cfg); err != nil {
			log.Fatal().Err(err).Msg("auto-migrate failed at boot")
		}
	} else {
		log.Info().Msg("auto-migrate disabled (ARGUS_AUTO_MIGRATE=false); assuming schema is current")
	}

	pg, err := store.NewPostgresWithMetrics(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns, cfg.DatabaseMaxIdleConns, cfg.DatabaseConnMaxLife, metricsReg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect postgres")
	}
	defer pg.Close()
	log.Info().Msg("postgres connected")

	if err := schemacheck.Verify(ctx, pg.Pool, schemacheck.CriticalTables); err != nil {
		log.Fatal().Err(err).Strs("expected_tables", schemacheck.CriticalTables).Msg("boot: schema integrity check failed — run 'argus migrate up' or inspect schema drift")
	}
	log.Info().Int("tables", len(schemacheck.CriticalTables)).Msg("schema integrity check passed")

	store.StartPoolGauge(appCtx, pg.Pool, metricsReg, 10*time.Second)

	var pgReadReplica *store.Postgres
	if cfg.DatabaseReadReplicaURL != "" {
		pgReadReplica, err = store.NewPostgres(ctx, cfg.DatabaseReadReplicaURL, cfg.DatabaseMaxConns/2, cfg.DatabaseMaxIdleConns/2, cfg.DatabaseConnMaxLife)
		if err != nil {
			log.Warn().Err(err).Msg("failed to connect read replica — analytics will use primary")
		} else {
			defer pgReadReplica.Close()
			log.Info().Msg("read replica connected")
		}
	}

	rdb, err := cache.NewRedis(ctx, cfg.RedisURL, cfg.RedisMaxConns, cfg.RedisReadTimeout, cfg.RedisWriteTimeout)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect redis")
	}
	defer rdb.Close()
	log.Info().Msg("redis connected")

	cache.RegisterRedisMetrics(rdb.Client, metricsReg)

	ns, err := bus.NewNATS(ctx, cfg.NATSURL, cfg.NATSMaxReconnect, cfg.NATSReconnectWait, log.Logger)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect nats")
	}
	defer ns.Close()

	if err := ns.EnsureStreams(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to create nats streams")
	}

	userStore := store.NewUserStore(pg.Pool)
	sessionStore := store.NewSessionStore(pg.Pool)
	passwordHistoryStore := store.NewPasswordHistoryStore(pg.Pool)
	backupCodeStore := store.NewBackupCodeStore(pg.Pool)

	authSvc := auth.NewService(
		&userStoreAdapter{userStore},
		&sessionStoreAdapter{sessionStore},
		nil,
		auth.Config{
			JWTSecret:           cfg.JWTSecret,
			JWTExpiry:           cfg.JWTExpiry,
			JWTRefreshExpiry:    cfg.JWTRefreshExpiry,
			JWTRememberMeExpiry: cfg.JWTRememberMeExpiry,
			JWTIssuer:           cfg.JWTIssuer,
			BcryptCost:          cfg.BcryptCost,
			MaxLoginAttempts:    cfg.LoginMaxAttempts,
			LockoutDuration:     cfg.LoginLockoutDur,
			EncryptionKey:       cfg.EncryptionKey,
			Policy: auth.PasswordPolicy{
				MinLength:     cfg.PasswordMinLength,
				RequireUpper:  cfg.PasswordRequireUpper,
				RequireLower:  cfg.PasswordRequireLower,
				RequireDigit:  cfg.PasswordRequireDigit,
				RequireSymbol: cfg.PasswordRequireSymbol,
				MaxRepeating:  cfg.PasswordMaxRepeating,
			},
			PasswordHistoryCount: cfg.PasswordHistoryCount,
			PasswordMaxAgeDays:   cfg.PasswordMaxAgeDays,
		},
	).WithPasswordHistory(passwordHistoryStore).WithBackupCodes(backupCodeStore)

	if migrated, err := userStore.MigrateTOTPSecretsToEncrypted(ctx, cfg.EncryptionKey); err != nil {
		log.Warn().Err(err).Msg("totp secret encryption migration failed — continuing")
	} else if migrated > 0 {
		log.Info().Int("migrated", migrated).Msg("encrypted plaintext totp secrets at rest")
	}

	authHandler := authapi.NewAuthHandler(authSvc, cfg.JWTRefreshExpiry, !cfg.IsDev())
	authHandler.WithRedis(rdb.Client)

	tenantStore := store.NewTenantStore(pg.Pool).WithRedis(rdb.Client)
	auditStore := store.NewAuditStore(pg.Pool)
	eventBus := bus.NewEventBus(ns)
	eventBus.SetMetrics(metricsReg)
	auditSvc := audit.NewFullService(auditStore, eventBus, log.Logger)

	if err := auditSvc.Start(ctx, &eventBusSubscriber{eventBus}); err != nil {
		log.Fatal().Err(err).Msg("failed to start audit consumer")
	}

	if err := auth.CheckAndAuditRotation(ctx, cfg, auditSvc, bootID, log.Logger); err != nil {
		log.Warn().Err(err).Msg("jwt key rotation audit failed")
	}

	// FIX-238 AC-10: boot-time one-shot — archive policy_versions whose DSL
	// contains the 'roaming' keyword. Idempotent; second run finds zero rows.
	if archivedCount, archiveErr := job.ArchiveRoamingKeywordPolicyVersions(ctx, pg.Pool, auditSvc, log.Logger); archiveErr != nil {
		log.Warn().Err(archiveErr).Msg("roaming_keyword_archiver: sweep failed; continuing")
	} else {
		log.Info().Int("archived", archivedCount).Msg("roaming_keyword_archiver: boot sweep complete")
	}

	tenantHandler := tenantapi.NewHandler(tenantStore, auditSvc, log.Logger)
	auditHandler := auditapi.NewHandler(auditStore, auditSvc, log.Logger)
	var userHandler *userapi.Handler

	apiKeyStore := store.NewAPIKeyStore(pg.Pool)
	authHandler.WithAPIKeyStore(apiKeyStore)
	authHandler.WithJWTSecret(cfg.JWTSecret, cfg.JWTExpiry)
	authHandler.WithUserStore(userStore)
	authHandler.WithAudit(auditSvc)

	// FIX-228 wiring: password reset
	passwordResetStore := store.NewPasswordResetStore(pg.Pool)
	if cfg.SMTPHost != "" {
		smtpEmailSender := notification.NewSMTPEmailSender(notification.SMTPConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			User: cfg.SMTPUser, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom, TLS: cfg.SMTPTLS,
		})
		authHandler.WithPasswordReset(
			passwordResetStore,
			smtpEmailSender,
			cfg.PasswordResetRateLimitPerHour,
			time.Duration(cfg.PasswordResetTokenTTLMinutes)*time.Minute,
			cfg.PublicBaseURL,
		)
	} else {
		// SMTP not configured: pass nil interface explicitly to avoid typed-nil
		// pointer being wrapped in a non-nil interface value (which would defeat
		// the nil-check in RequestPasswordReset and panic on SendTo).
		authHandler.WithPasswordReset(
			passwordResetStore,
			nil,
			cfg.PasswordResetRateLimitPerHour,
			time.Duration(cfg.PasswordResetTokenTTLMinutes)*time.Minute,
			cfg.PublicBaseURL,
		)
	}

	apiKeyHandler := apikeyapi.NewHandler(apiKeyStore, tenantStore, auditSvc, cfg.DefaultMaxAPIKeys, log.Logger)

	operatorStore := store.NewOperatorStore(pg.Pool)
	apnStore := store.NewAPNStore(pg.Pool)
	ippoolStore := store.NewIPPoolStore(pg.Pool)
	adapterRegistry := adapter.NewRegistry()
	simStore := store.NewSIMStore(pg.Pool)
	operatorMetricsSessionStore := store.NewRadiusSessionStore(pg.Pool)
	operatorMetricsCDRStore := store.NewCDRStore(pg.Pool)
	aggSessionStore := store.NewRadiusSessionStore(pg.Pool)
	aggCDRStore := store.NewCDRStore(pg.Pool)
	aggSvc := aggregates.New(
		simStore, aggSessionStore, aggCDRStore,
		rdb.Client,
		metricsReg,
		log.Logger,
		aggregates.WithTTL(60*time.Second),
	)
	if err := aggregates.RegisterInvalidator(eventBus, rdb.Client, log.Logger); err != nil {
		log.Fatal().Err(err).Msg("register aggregates invalidator")
	}
	operatorHandler := operatorapi.NewHandler(operatorStore, tenantStore, auditSvc, cfg.EncryptionKey, adapterRegistry, log.Logger,
		operatorapi.WithSIMStore(simStore),
		operatorapi.WithSessionStore(operatorMetricsSessionStore),
		operatorapi.WithCDRStore(operatorMetricsCDRStore),
		operatorapi.WithAggregates(aggSvc),
	)
	apnCDRStore := store.NewCDRStore(pg.Pool)
	apnHandler := apnapi.NewHandler(apnStore, operatorStore, auditSvc, log.Logger,
		apnapi.WithSIMStore(simStore),
		apnapi.WithCDRStore(apnCDRStore),
		apnapi.WithIPPoolStore(ippoolStore),
		apnapi.WithAggregates(aggSvc),
	)
	// policyStore for apnHandler wired after policyStore construction (see below)
	ippoolHandler := ippoolapi.NewHandler(ippoolStore, apnStore, auditSvc, log.Logger)
	esimStore := store.NewESimProfileStore(pg.Pool)
	var smdpAdapter esimpkg.SMDPAdapter
	switch cfg.ESIMProvider {
	case "", "mock":
		smdpAdapter = esimpkg.NewMockSMDPAdapter(log.Logger)
	default:
		httpAdapter, esimErr := esimpkg.NewHTTPSMDPAdapter(esimpkg.HTTPSMDPConfig{
			BaseURL:        cfg.ESIMSMDPBaseURL,
			APIKey:         cfg.ESIMSMDPAPIKey,
			ClientCertPath: cfg.ESIMSMDPClientCert,
			ClientKeyPath:  cfg.ESIMSMDPClientKey,
			Timeout:        10 * time.Second,
		}, log.Logger)
		if esimErr != nil {
			log.Logger.Fatal().Err(esimErr).Msg("failed to initialize SM-DP+ adapter")
		}
		smdpAdapter = httpAdapter
	}
	esimHandler := esimapi.NewHandler(esimStore, simStore, smdpAdapter, auditSvc, log.Logger)
	esimHandler.SetIPPoolStore(ippoolStore)
	esimHandler.SetEventBus(eventBus)
	segmentStore := store.NewSegmentStore(pg.Pool)
	segmentHandler := segmentapi.NewHandler(segmentStore, auditSvc, log.Logger)
	msisdnStore := store.NewMSISDNStore(pg.Pool)
	msisdnHandler := msisdnapi.NewHandler(msisdnStore, auditSvc, log.Logger)

	jobStore := store.NewJobStore(pg.Pool)

	policyStore := store.NewPolicyStore(pg.Pool)
	apnHandler.SetPolicyStore(policyStore)
	nameCache := cache.NewNameCache(rdb.Client)
	simSessionStore := store.NewRadiusSessionStore(pg.Pool)
	cdrStore := store.NewCDRStore(pg.Pool)
	simHandler := simapi.NewHandler(simStore, apnStore, operatorStore, ippoolStore, tenantStore, auditSvc, log.Logger, simapi.WithPolicyStore(policyStore), simapi.WithNameCache(nameCache), simapi.WithSessionStore(simSessionStore), simapi.WithCDRStore(cdrStore), simapi.WithIMSIStrictValidation(cfg.IMSIStrictValidation), simapi.WithEventBus(eventBus))
	dryRunSvc := dryrun.NewService(policyStore, simStore, pg.Pool, rdb.Client, log.Logger)
	rolloutSvc := rollout.NewService(policyStore, simStore, nil, nil, eventBus, jobStore, log.Logger)
	policyHandler := policyapi.NewHandler(policyStore, dryRunSvc, rolloutSvc, jobStore, eventBus, auditSvc, log.Logger,
		policyapi.WithAggregates(aggSvc),
		policyapi.WithSIMStore(simStore),
	)
	bulkHandler := simapi.NewBulkHandler(jobStore, segmentStore, eventBus, log.Logger)
	bulkHandler.SetSIMStore(simStore)
	jobHandler := jobapi.NewHandler(jobStore, eventBus, auditSvc, log.Logger)
	searchHandler := searchapi.NewHandler(simStore, apnStore, operatorStore, policyStore, userStore, pg.Pool, log.Logger)

	otaStore := store.NewOTAStore(pg.Pool)
	otaRateLimiter := ota.NewRateLimiter(rdb.Client, ota.DefaultMaxOTAPerSimPerHour)
	otaHandler := otaapi.NewHandler(otaStore, simStore, jobStore, eventBus, otaRateLimiter, auditSvc, log.Logger)

	diagSessionStore := store.NewRadiusSessionStore(pg.Pool)
	diagService := diagnosticspkg.NewService(simStore, diagSessionStore, operatorStore, apnStore, policyStore, ippoolStore, log.Logger)
	diagHandler := diagapi.NewHandler(diagService, rdb.Client, log.Logger)

	analyticsPool := pg.Pool
	if pgReadReplica != nil {
		analyticsPool = pgReadReplica.Pool
		log.Info().Msg("analytics queries routed to read replica")
	}
	usageAnalyticsStore := store.NewUsageAnalyticsStore(analyticsPool)
	costAnalyticsStore := store.NewCostAnalyticsStore(analyticsPool)
	costService := costsvc.NewService(costAnalyticsStore, log.Logger)
	analyticsHandler := analyticsapi.NewHandler(usageAnalyticsStore, log.Logger)
	analyticsHandler.SetCostService(costService)
	analyticsHandler.WithStores(simStore, operatorStore, apnStore, ippoolStore)
	chartAnnotationStore := store.NewChartAnnotationStore(pg.Pool)
	analyticsHandler.SetChartAnnotationStore(chartAnnotationStore)
	cdrConsumer := cdrsvc.NewConsumer(cdrStore, operatorStore, log.Logger, metricsReg, cfg.IMSIStrictValidation)
	if err := cdrConsumer.Start(&eventBusCDRSubscriber{eventBus}); err != nil {
		log.Fatal().Err(err).Msg("failed to start cdr consumer")
	}
	cdrHandler := cdrapi.NewHandler(cdrStore, jobStore, eventBus, auditSvc, log.Logger, cdrapi.WithAggregates(aggSvc))

	lagPoller := bus.NewLagPoller(
		bus.NewJSStreamLookup(ns.JetStream),
		metricsReg,
		[]string{bus.StreamEvents, bus.StreamJobs},
		time.Duration(cfg.NATSConsumerLagPollSec)*time.Second,
		cfg.NATSConsumerLagAlertThreshold,
		eventBus,
		log.Logger,
	)
	lagPoller.Start(appCtx)

	anomalyStore := store.NewAnomalyStore(pg.Pool)
	anomalyThresholds := anomalysvc.DefaultThresholds()
	realtimeDetector := anomalysvc.NewRealtimeDetector(rdb.Client, anomalyThresholds, log.Logger)
	anomalyEngine := anomalysvc.NewEngine(
		realtimeDetector,
		anomalyStore,
		&simSuspenderAdapter{simStore},
		eventBus,
		anomalysvc.EngineConfig{
			Thresholds:     anomalyThresholds,
			AlertSubject:   bus.SubjectAlertTriggered,
			AnomalySubject: bus.SubjectAnomalyDetected,
		},
		log.Logger,
	)
	if err := anomalyEngine.Start(&eventBusAnomalySubscriber{eventBus}); err != nil {
		log.Warn().Err(err).Msg("failed to start anomaly engine")
	}
	anomalyCommentStore := store.NewAnomalyCommentStore(pg.Pool)
	anomalyHandler := anomalyapi.NewHandler(anomalyStore, auditSvc, log.Logger,
		anomalyapi.WithCommentStore(anomalyCommentStore),
		anomalyapi.WithUserStore(userStore),
	)

	// FIX-209 Task 4 — unified alerts table + API + retention job.
	// FIX-229 Task 8 — alert suppressions store, chained onto both
	// alertStore (for trigger-time mute matching, DEV-334) and alertHandler
	// (for AC-1 + AC-5 CRUD endpoints).
	alertStore := store.NewAlertStore(pg.Pool)
	alertSuppressionStore := store.NewAlertSuppressionStore(pg.Pool)
	alertStore.WithSuppressionStore(alertSuppressionStore)
	alertHandler := alertapi.NewHandler(alertStore, auditSvc, log.Logger, cfg.AlertCooldownMinutes).
		WithSuppressionStore(alertSuppressionStore)
	eventsCatalogHandler := eventsapi.NewHandler(log.Logger)

	// FIX-212 AC-6: shared name resolver for non-hot-path publishers that
	// embed entity.display_name into envelopes. Backed by Redis (10-min TTL)
	// + singleflight coalescing + negative-cache sentinel. Falls back to
	// empty display_name gracefully when Redis/DB is unavailable — the
	// envelope still validates, FE just renders entity.id instead of a name.
	// Kept as a package-level value so future publishers can opt in via a
	// WithResolver option without another cmd/argus wiring edit.
	eventResolver := events.NewRedisResolver(
		rdb.Client,
		events.NewSimNameLookup(simStore),
		events.NewOperatorNameLookup(operatorStore),
		events.NewAPNNameLookup(apnStore),
		metricsReg,
		log.Logger,
	)
	_ = eventResolver // reserved for future publisher constructors (FIX-240)

	anomalyStoreAdapter := anomalysvc.NewAnomalyStoreAdapter(anomalyStore)
	batchDetector := anomalysvc.NewBatchDetector(
		anomalyStoreAdapter,
		eventBus,
		&simSuspenderAdapter{simStore},
		anomalyThresholds,
		bus.SubjectAlertTriggered,
		bus.SubjectAnomalyDetected,
		log.Logger,
	)

	readPool := pg.Pool
	if pgReadReplica != nil {
		readPool = pgReadReplica.Pool
		log.Info().Msg("bulk reads + CDR export routed to read replica")
	}

	distLock := job.NewDistributedLock(rdb.Client, log.Logger)
	importProcessor := job.NewBulkImportProcessor(jobStore, simStore, operatorStore, apnStore, ippoolStore, eventBus, auditSvc, nil, policyStore, log.Logger)
	dryRunProcessor := job.NewDryRunProcessor(dryRunSvc, jobStore, eventBus, log.Logger)
	rolloutStageProc := job.NewRolloutStageProcessor(rolloutSvc, policyStore, jobStore, eventBus, log.Logger)
	jobRunner := job.NewRunner(jobStore, eventBus, distLock, job.RunnerConfig{
		MaxConcurrentPerTenant: cfg.JobMaxConcurrentPerTenant,
		LockRenewInterval:      cfg.JobLockRenewInterval,
	}, log.Logger)
	jobRunner.SetMetrics(metricsReg)
	jobRunner.Register(importProcessor)
	jobRunner.Register(dryRunProcessor)
	jobRunner.Register(rolloutStageProc)

	complianceStore := store.NewComplianceStore(pg.Pool)
	complianceSvc := compliance.NewService(complianceStore, auditStore, auditSvc, log.Logger)
	purgeSweepProc := job.NewPurgeSweepProcessor(jobStore, complianceSvc, eventBus, log.Logger)
	slaReportStore := store.NewSLAReportStore(pg.Pool)
	slaRadiusSessionStore := store.NewRadiusSessionStore(pg.Pool)
	ipReclaimProc := job.NewIPReclaimProcessor(jobStore, ippoolStore, eventBus, &auditRecorderAdapter{svc: auditSvc}, log.Logger)
	slaReportProc := job.NewSLAReportProcessor(jobStore, slaReportStore, operatorStore, tenantStore, slaRadiusSessionStore, eventBus, log.Logger)
	readSegmentStore := store.NewSegmentStore(readPool)
	bulkStateChangeProc := job.NewBulkStateChangeProcessor(jobStore, simStore, segmentStore, readSegmentStore, distLock, eventBus, log.Logger)
	bulkStateChangeProc.SetAuditor(auditSvc)
	bulkPolicyAssignProc := job.NewBulkPolicyAssignProcessor(jobStore, simStore, segmentStore, distLock, eventBus, log.Logger)
	bulkPolicyAssignProc.SetAuditor(auditSvc)
	otaProcessor := job.NewOTAProcessor(jobStore, otaStore, simStore, otaRateLimiter, eventBus, log.Logger)
	otaCommandStore := store.NewEsimOTACommandStore(pg.Pool)
	esimStockStore := store.NewEsimProfileStockStore(pg.Pool)
	bulkEsimSwitchProc := job.NewBulkEsimSwitchProcessor(jobStore, simStore, segmentStore, esimStore, otaCommandStore, esimStockStore, distLock, eventBus, log.Logger)
	bulkEsimSwitchProc.SetAuditor(auditSvc)
	smsrClient := smsr.NewMockClient()
	// FIX-235 Gate (PAT-017): plumb cfg.ESimOTA* through to constructors (5-hop chain).
	esimOTADispatcherProc := job.NewESimOTADispatcherProcessor(
		jobStore, otaCommandStore, esimStore, smsrClient, auditSvc, eventBus,
		cfg.ESimOTARateLimitPerSec, cfg.ESimOTABatchSize, cfg.ESimOTAMaxRetries,
		log.Logger,
	)
	esimOTATimeoutReaperProc := job.NewESimOTATimeoutReaperProcessor(
		jobStore, otaCommandStore, esimStore, auditSvc, eventBus,
		cfg.ESimOTATimeoutMinutes,
		log.Logger,
	)
	esimStockAlerterProc := job.NewESimStockAlerterProcessor(jobStore, esimStockStore, tenantStore, alertStore, log.Logger)
	// FIX-235 T15: wire OTA handler deps (PAT-017 hop 3 of 5).
	esimHandler.SetOTADeps(operatorStore, otaCommandStore, esimStockStore, jobStore, cfg.SMSRCallbackSecret)
	jobRunner.Register(purgeSweepProc)
	jobRunner.Register(ipReclaimProc)
	jobRunner.Register(slaReportProc)
	jobRunner.Register(bulkStateChangeProc)
	jobRunner.Register(bulkPolicyAssignProc)
	jobRunner.Register(otaProcessor)
	jobRunner.Register(bulkEsimSwitchProc)
	jobRunner.Register(esimOTADispatcherProc)
	jobRunner.Register(esimOTATimeoutReaperProc)
	jobRunner.Register(esimStockAlerterProc)

	readCDRStore := store.NewCDRStore(readPool)
	cdrExportProc := job.NewCDRExportProcessor(jobStore, cdrStore, readCDRStore, eventBus, log.Logger)
	jobRunner.Register(cdrExportProc)

	anomalyBatchProc := job.NewAnomalyBatchProcessor(batchDetector, jobStore, eventBus, log.Logger)
	safeAnomalyProc := job.NewCrashSafeProcessor(anomalyBatchProc, eventBus, log.Logger)
	jobRunner.Register(safeAnomalyProc)

	storageMonitorStore := store.NewStorageMonitorStore(pg.Pool)
	dataLifecycleStore := store.NewDataLifecycleStore(pg.Pool)

	storageMonitorProc := job.NewStorageMonitorProcessor(jobStore, storageMonitorStore, eventBus, cfg.StorageAlertPct, log.Logger)
	jobRunner.Register(storageMonitorProc)

	// FIX-209 Task 4 / FIX-229 DEV-336 — per-tenant purge of alerts older than
	// tenants.settings.alert_retention_days (DEV-335), falling back to
	// cfg.AlertsRetentionDays (default 180) when a tenant has no override.
	alertsRetentionProc := job.NewAlertsRetentionProcessor(jobStore, alertStore, tenantStore, eventBus, cfg.AlertsRetentionDays, log.Logger)
	jobRunner.Register(alertsRetentionProc)

	// FIX-231 PAT-017 — stuck-rollout reaper: force-completes rollouts that have
	// been stuck longer than cfg.StuckRolloutGraceMinutes (env ARGUS_STUCK_ROLLOUT_GRACE_MINUTES, default 10).
	stuckRolloutProc := job.NewStuckRolloutReaperProcessor(jobStore, policyStore, eventBus, cfg.StuckRolloutGraceMinutes, log.Logger)
	jobRunner.Register(stuckRolloutProc)

	// FIX-234 AC-7/AC-8 — CoA failure alerter: sweeps policy_assignments for
	// long-lived coa_status='failed' rows (>5min) and raises deduplicated alerts.
	// Also refreshes the argus_coa_status_by_state Prometheus gauge every sweep.
	coaFailureAlerterProc := job.NewCoAFailureAlerterProcessor(jobStore, policyStore, alertStore, metricsReg, eventBus, log.Logger)
	jobRunner.Register(coaFailureAlerterProc)

	// FIX-246 D-5 — quota breach checker: runs hourly, raises deduplicated alerts
	// for tenants at >=80% (medium) or >=95% (critical) quota utilization across
	// sims, sessions, api_rps, and storage_bytes. Auto-resolves when below threshold.
	quotaBreachProc := job.NewQuotaBreachCheckerProcessor(jobStore, tenantStore, alertStore, log.Logger)
	jobRunner.Register(quotaBreachProc)

	dataRetentionProc := job.NewDataRetentionProcessor(jobStore, dataLifecycleStore, storageMonitorStore, eventBus, cfg.DefaultCDRRetentionDays, log.Logger)
	jobRunner.Register(dataRetentionProc)

	partitionCreatorProc := job.NewPartitionCreatorProcessor(pg.Pool, jobStore, eventBus, 3, log.Logger)
	jobRunner.Register(partitionCreatorProc)

	var s3Uploader job.S3Uploader
	var s3Impl *storage.S3Uploader
	if cfg.S3Bucket != "" {
		var s3Err error
		s3Impl, s3Err = storage.NewS3Uploader(ctx, storage.S3Config{
			Endpoint:  cfg.S3Endpoint,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Bucket:    cfg.S3Bucket,
			Region:    cfg.S3Region,
			PathStyle: cfg.S3PathStyle,
		}, log.Logger)
		if s3Err != nil {
			log.Logger.Warn().Err(s3Err).Msg("S3 uploader initialization failed; archival jobs will be skipped")
		} else {
			s3Uploader = s3Impl
		}
	}
	s3ArchivalProc := job.NewS3ArchivalProcessor(jobStore, dataLifecycleStore, storageMonitorStore, cdrStore, s3Uploader, eventBus, cfg.S3Bucket, log.Logger)
	jobRunner.Register(s3ArchivalProc)

	var backupDailyProc, backupWeeklyProc, backupMonthlyProc *job.BackupProcessor
	var backupVerifyProc *job.BackupVerifyProcessor
	var backupCleanupProc *job.BackupCleanupProcessor
	if cfg.BackupEnabled {
		backupStore := store.NewBackupStore(pg.Pool)
		backupTimeout := time.Duration(cfg.BackupTimeoutSec) * time.Second

		// Guard against nil-pointer-wrapped-in-interface: only assign when s3Impl != nil.
		var backupS3 job.BackupS3Client
		if s3Impl != nil {
			backupS3 = s3Impl
		}

		backupDailyProc = job.NewBackupProcessor(job.BackupProcessorOpts{
			Store: backupStore, Uploader: backupS3, Bucket: cfg.BackupBucket,
			TempDir: "/tmp", Timeout: backupTimeout, Kind: "daily",
			DatabaseURL: cfg.DatabaseURL, Reg: metricsReg, Logger: log.Logger, EventBus: eventBus,
		})
		backupWeeklyProc = job.NewBackupProcessor(job.BackupProcessorOpts{
			Store: backupStore, Uploader: backupS3, Bucket: cfg.BackupBucket,
			TempDir: "/tmp", Timeout: backupTimeout, Kind: "weekly",
			DatabaseURL: cfg.DatabaseURL, Reg: metricsReg, Logger: log.Logger, EventBus: eventBus,
		})
		backupMonthlyProc = job.NewBackupProcessor(job.BackupProcessorOpts{
			Store: backupStore, Uploader: backupS3, Bucket: cfg.BackupBucket,
			TempDir: "/tmp", Timeout: backupTimeout, Kind: "monthly",
			DatabaseURL: cfg.DatabaseURL, Reg: metricsReg, Logger: log.Logger, EventBus: eventBus,
		})
		backupVerifyProc = job.NewBackupVerifyProcessor(job.BackupVerifyProcessorOpts{
			Store: backupStore, Uploader: backupS3, Bucket: cfg.BackupBucket,
			TempDir: "/tmp", Timeout: backupTimeout,
			DatabaseURL: cfg.DatabaseURL, EventBus: eventBus, Logger: log.Logger,
		})
		backupCleanupProc = job.NewBackupCleanupProcessor(job.BackupCleanupProcessorOpts{
			Store: backupStore, Uploader: backupS3, Bucket: cfg.BackupBucket,
			RetentionDaily: cfg.BackupRetentionDaily, RetentionWeekly: cfg.BackupRetentionWeekly,
			RetentionMonthly: cfg.BackupRetentionMonthly, Logger: log.Logger,
		})

		jobRunner.Register(backupDailyProc)
		jobRunner.Register(backupWeeklyProc)
		jobRunner.Register(backupMonthlyProc)
		jobRunner.Register(backupVerifyProc)
		jobRunner.Register(backupCleanupProc)
		log.Info().Msg("backup processors registered")
	}

	if err := jobRunner.Start(); err != nil {
		log.Fatal().Err(err).Msg("failed to start job runner")
	}

	jobHandler.SetCanceller(jobRunner)

	timeoutDetector := job.NewTimeoutDetector(jobStore, eventBus,
		time.Duration(cfg.JobTimeoutMinutes)*time.Minute,
		cfg.JobTimeoutCheckInterval,
		log.Logger,
	)
	timeoutDetector.Start()

	orphanDetector := job.NewOrphanSessionDetector(pg.Pool, log.Logger)
	orphanDetector.Start()

	var cronScheduler *job.Scheduler
	if cfg.CronEnabled {
		cronScheduler = job.NewScheduler(jobStore, eventBus, rdb.Client, log.Logger)
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "purge_sweep",
			Schedule: cfg.CronPurgeSweep,
			JobType:  job.JobTypePurgeSweep,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "ip_reclaim",
			Schedule: cfg.CronIPReclaim,
			JobType:  job.JobTypeIPReclaim,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "sla_report",
			Schedule: cfg.CronSLAReport,
			JobType:  job.JobTypeSLAReport,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "anomaly_batch_detection",
			Schedule: "@hourly",
			JobType:  job.JobTypeAnomalyBatch,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "storage_monitor",
			Schedule: cfg.CronStorageMonitor,
			JobType:  job.JobTypeStorageMonitor,
		})
		// FIX-209 — retention: purge alerts older than ALERTS_RETENTION_DAYS (default 180).
		// 03:15 UTC offset avoids collision with storage_monitor (@hourly on :00).
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "alerts_retention",
			Schedule: "15 3 * * *",
			JobType:  job.JobTypeAlertsRetention,
		})
		// FIX-231 PAT-017 — stuck-rollout reaper: runs every 5 minutes.
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "stuck_rollout_reaper",
			Schedule: "*/5 * * * *",
			JobType:  job.JobTypeStuckRolloutReaper,
		})
		// FIX-234 AC-7 — CoA failure alerter: runs every minute to keep alert
		// latency ≤ 60s for long-lived coa_status='failed' rows.
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "coa_failure_alerter",
			Schedule: "* * * * *",
			JobType:  job.JobTypeCoAFailureAlerter,
		})
		// FIX-246 D-5 — quota breach checker: hourly sweep across all active tenants.
		// Env CRON_QUOTA_BREACH_CHECK overrides default (@hourly).
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "quota_breach_checker",
			Schedule: cfg.CronQuotaBreachCheck,
			JobType:  job.JobTypeQuotaBreachChecker,
		})
		// FIX-235 T15: eSIM OTA pipeline cron entries.
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "esim_ota_dispatcher",
			Schedule: cfg.CronESimOTADispatcher,
			JobType:  job.JobTypeOTACommand,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "esim_ota_timeout_reaper",
			Schedule: cfg.CronESimOTATimeoutReaper,
			JobType:  job.JobTypeESimOTATimeoutReaper,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "esim_stock_alert",
			Schedule: cfg.CronESimStockAlert,
			JobType:  job.JobTypeESimStockAlerter,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "data_retention",
			Schedule: cfg.CronDataRetention,
			JobType:  job.JobTypeDataRetention,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "s3_archival",
			Schedule: cfg.CronS3Archival,
			JobType:  job.JobTypeS3Archival,
		})
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "partition_creator",
			Schedule: "0 2 * * *",
			JobType:  job.JobTypePartitionCreate,
		})

		if cfg.BackupEnabled && backupDailyProc != nil {
			cronScheduler.AddEntry(job.CronEntry{Name: "backup-daily", Schedule: cfg.BackupDailyCron, JobType: backupDailyProc.Type()})
			cronScheduler.AddEntry(job.CronEntry{Name: "backup-weekly", Schedule: "0 2 * * 0", JobType: backupWeeklyProc.Type()})
			cronScheduler.AddEntry(job.CronEntry{Name: "backup-monthly", Schedule: "0 2 1 * *", JobType: backupMonthlyProc.Type()})
			cronScheduler.AddEntry(job.CronEntry{Name: "backup-verify", Schedule: cfg.BackupVerifyCron, JobType: backupVerifyProc.Type()})
			cronScheduler.AddEntry(job.CronEntry{Name: "backup-cleanup", Schedule: cfg.BackupCleanupCron, JobType: backupCleanupProc.Type()})
		}

		cronScheduler.Start()
	}

	operatorRouter := operator.NewOperatorRouterFromConfig(cfg, adapterRegistry, log.Logger)
	_ = operatorRouter

	healthChecker := operator.NewHealthChecker(operatorStore, adapterRegistry, rdb.Client, cfg.EncryptionKey, log.Logger)
	healthChecker.SetEventPublisher(eventBus, bus.SubjectOperatorHealthChanged, bus.SubjectAlertTriggered)
	healthChecker.SetMetricsRegistry(metricsReg)

	slaTracker := operator.NewSLATracker(rdb.Client, log.Logger)
	healthChecker.SetSLATracker(slaTracker)

	startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := healthChecker.Start(startCtx); err != nil {
		log.Warn().Err(err).Msg("failed to start health checker — continuing without health checks")
	}
	startCancel()

	notifStore := store.NewNotificationStore(pg.Pool)
	notifConfigStore := store.NewNotificationConfigStore(pg.Pool)

	notifChannels := []notification.Channel{notification.ChannelInApp}
	var emailSender notification.EmailSender
	if cfg.SMTPHost != "" {
		notifChannels = append(notifChannels, notification.ChannelEmail)
		emailSender = notification.NewSMTPEmailSender(notification.SMTPConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			User: cfg.SMTPUser, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom, TLS: cfg.SMTPTLS,
		})
	}
	var telegramSender notification.TelegramSender
	if cfg.TelegramBotToken != "" {
		notifChannels = append(notifChannels, notification.ChannelTelegram)
		telegramSender = notification.NewTelegramBotSender(notification.TelegramConfig{
			BotToken:      cfg.TelegramBotToken,
			DefaultChatID: cfg.TelegramDefaultChat,
		})
	}
	notifSvc := notification.NewService(emailSender, telegramSender, &inAppStoreAdapter{s: notifStore}, notifChannels, log.Logger)
	notifSvc.SetNotifStore(&notifStoreAdapter{notifStore})
	notifSvc.SetEventPublisher(eventBus, bus.SubjectNotification)
	notifSvc.SetAlertStore(alertStore)
	// FIX-210 Task 4: wire the metrics registry so handleAlertPersist
	// emits argus_alerts_deduplicated_total / _cooldown_dropped_total.
	notifSvc.SetMetricsRegistry(metricsReg)

	notifRedisRL := notification.NewRedisRateLimiter(rdb.Client, cfg.NotificationRateLimitPerMin)
	notifDelivery := notification.NewDeliveryTracker(notifRedisRL, log.Logger)
	notifSvc.SetDeliveryTracker(notifDelivery)
	importProcessor.SetNotifier(notifSvc)

	if err := notifSvc.Start(&eventBusNotifSubscriber{eventBus}, bus.SubjectOperatorHealthChanged, bus.SubjectAlertTriggered); err != nil {
		log.Warn().Err(err).Msg("failed to start notification service")
	}

	wsHub := ws.NewHub(log.Logger)
	wsHub.SetMetrics(metricsReg)
	if err := wsHub.SubscribeToNATS(&eventBusWSSubscriber{eventBus}, []string{
		bus.SubjectSessionStarted,
		bus.SubjectSessionUpdated,
		bus.SubjectSessionEnded,
		bus.SubjectSIMUpdated,
		bus.SubjectOperatorHealthChanged,
		bus.SubjectAlertTriggered,
		bus.SubjectPolicyRolloutProgress,
		bus.SubjectJobProgress,
		bus.SubjectJobCompleted,
		bus.SubjectNotification,
		bus.SubjectFleetMassOffline,
		bus.SubjectFleetTrafficSpike,
		bus.SubjectFleetQuotaBreachCount,
		bus.SubjectFleetViolationSurge,
	}); err != nil {
		log.Warn().Err(err).Msg("failed to subscribe ws hub to NATS")
	}

	wsServer := ws.NewServer(wsHub, ws.ServerConfig{
		Addr:              fmt.Sprintf(":%d", cfg.WSPort),
		JWTSecret:         cfg.JWTSecret,
		MaxConnsPerTenant: cfg.WSMaxConnsPerTenant,
		MaxConnsPerUser:   cfg.WSMaxConnsPerUser,
		PongTimeout:       cfg.WSPongTimeout,
	}, log.Logger)
	if err := wsServer.Start(); err != nil {
		log.Fatal().Err(err).Msg("failed to start ws server")
	}

	userViewStore := store.NewUserViewStore(pg.Pool)
	userColumnPrefStore := store.NewUserColumnPrefStore(pg.Pool)
	userHandler = userapi.NewHandler(userStore, tenantStore, auditSvc, log.Logger,
		userapi.WithSessionStore(sessionStore),
		userapi.WithAPIKeyStore(apiKeyStore),
		userapi.WithWSHub(wsHub),
		userapi.WithAuditStore(auditStore),
		userapi.WithViewStore(userViewStore),
		userapi.WithColumnPrefStore(userColumnPrefStore),
		userapi.WithPasswordPolicy(auth.PasswordPolicy{
			MinLength:     cfg.PasswordMinLength,
			RequireUpper:  cfg.PasswordRequireUpper,
			RequireLower:  cfg.PasswordRequireLower,
			RequireDigit:  cfg.PasswordRequireDigit,
			RequireSymbol: cfg.PasswordRequireSymbol,
			MaxRepeating:  cfg.PasswordMaxRepeating,
		}, cfg.BcryptCost),
	)

	var radiusServer *aaaradius.Server
	var sessionHandler *sessionapi.Handler
	var sessionSweeper *aaasession.TimeoutSweeper
	if cfg.RadiusSecret != "" {
		radiusSessionStore := store.NewRadiusSessionStore(pg.Pool)
		simCache := aaaradius.NewSIMCache(rdb.Client, simStore, log.Logger)
		sessionMgr := aaasession.NewManager(radiusSessionStore, rdb.Client, log.Logger,
			aaasession.WithSIMStore(simStore),
			aaasession.WithIPPoolStore(ippoolStore),
			aaasession.WithMetrics(metricsReg),
			aaasession.WithAuditService(auditSvc),
		)
		coaSender := aaasession.NewCoASender(cfg.RadiusSecret, cfg.RadiusCoAPort, log.Logger, aaasession.WithCoAAuditor(auditSvc))
		dmSender := aaasession.NewDMSender(cfg.RadiusSecret, cfg.RadiusCoAPort, log.Logger, aaasession.WithDMAuditor(auditSvc))

		esimHandler.SetSessionDeps(radiusSessionStore, dmSender)

		radiusServer = aaaradius.NewServer(
			aaaradius.ServerConfig{
				AuthAddr:       fmt.Sprintf(":%d", cfg.RadiusAuthPort),
				AcctAddr:       fmt.Sprintf(":%d", cfg.RadiusAcctPort),
				DefaultSecret:  cfg.RadiusSecret,
				WorkerPoolSize: cfg.RadiusWorkerPoolSize,
			},
			simCache,
			sessionMgr,
			operatorStore,
			ippoolStore,
			eventBus,
			coaSender,
			dmSender,
			log.Logger,
		)

		radiusStartCtx, radiusStartCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := radiusServer.Start(radiusStartCtx); err != nil {
			log.Fatal().Err(err).Msg("failed to start RADIUS server")
		}
		radiusStartCancel()

		sessionHandler = sessionapi.NewHandler(sessionMgr, dmSender, eventBus, auditSvc, jobStore, log.Logger,
			sessionapi.WithSIMStore(simStore),
			sessionapi.WithOperatorStore(operatorStore),
			sessionapi.WithAPNStore(apnStore),
			sessionapi.WithPolicyStore(policyStore),
			sessionapi.WithAuditStore(auditStore),
		)

		sessionSweeper = aaasession.NewTimeoutSweeper(sessionMgr, dmSender, eventBus, rdb.Client, log.Logger)
		sessionSweeper.Start()

		disconnectProcessor := job.NewBulkDisconnectProcessor(jobStore, sessionMgr, dmSender, eventBus, log.Logger)
		jobRunner.Register(disconnectProcessor)

		rolloutSvc.SetSessionProvider(&rolloutSessionAdapter{mgr: sessionMgr})
		rolloutSvc.SetCoADispatcher(&rolloutCoAAdapter{sender: coaSender})

		bulkPolicyAssignProc.SetSessionProvider(&bulkPolicySessionAdapter{mgr: sessionMgr})
		bulkPolicyAssignProc.SetCoADispatcher(&bulkPolicyCoAAdapter{sender: coaSender})
		bulkPolicyAssignProc.SetPolicyCoAUpdater(policyStore)

		if cfg.RadSecCertPath != "" && cfg.RadSecKeyPath != "" {
			radSecServer := aaaradius.NewRadSecServer(aaaradius.RadSecConfig{
				Addr:     fmt.Sprintf(":%d", cfg.RadSecPort),
				CertPath: cfg.RadSecCertPath,
				KeyPath:  cfg.RadSecKeyPath,
				CAPath:   cfg.RadSecCAPath,
			}, radiusServer, log.Logger)
			if err := radSecServer.Start(); err != nil {
				log.Warn().Err(err).Msg("failed to start RadSec server")
			} else {
				log.Info().Int("port", cfg.RadSecPort).Msg("RadSec (RADIUS/TLS) server started")
			}
		}
	}

	var diameterServer *aaadiameter.Server
	if cfg.DiameterOriginHost != "" {
		var diamSimResolver aaadiameter.SIMResolver
		if simStore != nil {
			diamSimResolver = aaaradius.NewSIMCache(rdb.Client, simStore, log.Logger)
		}

		radiusSessionStore := store.NewRadiusSessionStore(pg.Pool)
		diamSessionMgr := aaasession.NewManager(radiusSessionStore, rdb.Client, log.Logger,
			aaasession.WithSIMStore(simStore),
			aaasession.WithIPPoolStore(ippoolStore),
			aaasession.WithMetrics(metricsReg),
			aaasession.WithAuditService(auditSvc),
		)

		diameterServer = aaadiameter.NewServer(aaadiameter.ServerConfig{
			Port:        cfg.DiameterPort,
			OriginHost:  cfg.DiameterOriginHost,
			OriginRealm: cfg.DiameterOriginRealm,
		}, aaadiameter.ServerDeps{
			SessionMgr:  diamSessionMgr,
			EventBus:    eventBus,
			SIMResolver: diamSimResolver,
			IPPoolStore: ippoolStore,
			SIMStore:    simStore,
			Logger:      log.Logger,
		})

		if cfg.DiameterTLSEnabled && cfg.DiameterTLSCert != "" {
			if err := diameterServer.StartWithTLS(aaadiameter.TLSConfig{
				Enabled:  true,
				CertPath: cfg.DiameterTLSCert,
				KeyPath:  cfg.DiameterTLSKey,
				CAPath:   cfg.DiameterTLSCA,
			}); err != nil {
				log.Fatal().Err(err).Msg("failed to start Diameter server with TLS")
			}
		} else {
			if err := diameterServer.Start(); err != nil {
				log.Fatal().Err(err).Msg("failed to start Diameter server")
			}
		}
	}

	var sbaServer *aaasba.Server
	if cfg.SBAEnabled {
		sbaRadiusSessionStore := store.NewRadiusSessionStore(pg.Pool)
		sbaSessionMgr := aaasession.NewManager(sbaRadiusSessionStore, rdb.Client, log.Logger,
			aaasession.WithSIMStore(simStore),
			aaasession.WithIPPoolStore(ippoolStore),
			aaasession.WithMetrics(metricsReg),
			aaasession.WithAuditService(auditSvc),
		)

		// STORY-092 Wave 3 (D3-B): thread SIMStore + IPPoolStore + SIMCache
		// into the SBA server so the mock Nsmf_PDUSession handler can allocate
		// UE IPs via the same store-layer pipeline RADIUS and Gx use. The
		// SIMCache doubles as the SIMResolver (it implements GetByIMSI).
		sbaSIMCache := aaaradius.NewSIMCache(rdb.Client, simStore, log.Logger)

		sbaServer = aaasba.NewServer(aaasba.ServerConfig{
			Port:        cfg.SBAPort,
			TLSCertPath: cfg.TLSCertPath,
			TLSKeyPath:  cfg.TLSKeyPath,
			EnableMTLS:  cfg.SBAEnableMTLS,
			NRFConfig: aaasba.NRFConfig{
				NRFURL:       cfg.SBANRFURL,
				NFInstanceID: cfg.SBANFInstanceID,
				HeartbeatSec: cfg.SBANRFHeartbeatSec,
			},
		}, aaasba.ServerDeps{
			SessionMgr:  sbaSessionMgr,
			EventBus:    eventBus,
			Logger:      log.Logger,
			SIMResolver: sbaSIMCache,
			SIMStore:    simStore,
			IPPoolStore: ippoolStore,
			SIMCache:    sbaSIMCache,
		})

		if err := sbaServer.Start(); err != nil {
			log.Fatal().Err(err).Msg("failed to start SBA server")
		}

		if err := sbaServer.NRFRegistration().Register(); err != nil {
			log.Warn().Err(err).Msg("NRF registration failed")
		}
	}

	metricsCollector := analyticmetrics.NewCollector(rdb.Client, log.Logger)

	radiusSessionStore2 := store.NewRadiusSessionStore(pg.Pool)
	metricsCollector.SetSessionCounter(radiusSessionStore2)

	violationStore := store.NewPolicyViolationStore(pg.Pool, log.Logger)

	// TODO(D-038): wire a policy/cache.Cache with a PolicyLoader impl backed
	// by policyStore. Enforcer is nil-safe (falls through to direct DB fetch
	// per request) so this is a performance improvement, not a correctness
	// requirement. Tracked in ROUTEMAP Tech Debt.
	policyEnforcer := policyenforcer.New(
		nil,
		policyStore,
		violationStore,
		eventBus,
		rdb.Client,
		log.Logger,
	)
	// FIX-210 Task 4: wire metrics registry so the rate-limited publish
	// path increments argus_alerts_rate_limited_publishes_total{publisher="enforcer"}.
	policyEnforcer.SetMetricsRegistry(metricsReg)

	if radiusServer != nil {
		promAAARecorder := obsmetrics.NewPromAAARecorder(metricsReg, "radius")
		compositeRecorder := obsmetrics.NewCompositeRecorder(metricsCollector, promAAARecorder)
		radiusServer.SetMetricsRecorder(compositeRecorder)
		radiusServer.SetRegistry(metricsReg)
		radiusServer.SetPolicyEnforcer(policyEnforcer)
		// STORY-092 Wave 1: wire the SIM store so Access-Accept can persist
		// the dynamically allocated ip_address_id back to the sims row.
		radiusServer.SetSIMStore(simStore)
		radiusServer.SetIMSIStrictValidation(cfg.IMSIStrictValidation)
	}
	// Diameter and SBA servers do not currently expose SetMetricsRecorder —
	// protocol-labelled Prom metrics for those will be wired when those
	// servers grow the hook (tracked for a follow-up story).

	activeOps, activeOpsErr := operatorStore.ListActive(context.Background())
	if activeOpsErr == nil {
		opIDs := make([]uuid.UUID, 0, len(activeOps))
		for _, op := range activeOps {
			opIDs = append(opIDs, op.ID)
		}
		metricsCollector.SetOperatorIDs(opIDs)
	}

	metricsPusher := analyticmetrics.NewPusher(metricsCollector, wsHub, log.Logger)
	metricsPusher.Start()

	// NATS pending messages gauge (argus_nats_pending_messages) is a no-op
	// until EventBus exposes a PendingByConsumer-style API. Tracked as a
	// follow-up; the gauge stays at 0 meanwhile. Intentionally no goroutine.

	metricsHandler := metricsapi.NewHandler(metricsCollector, log.Logger)

	// FIX-229 Task 7: extend the report provider with alert + operator stores
	// so ReportAlertsExport can hydrate operator names and SIM ICCIDs through
	// the unified Engine.Build path. The same engine drives the SLA monthly
	// PDF (its original consumer).
	slaReportEngine := report.NewEngine(
		report.NewStoreProvider(complianceStore, cdrStore, auditStore, simStore, slaReportStore).
			WithAlertStore(alertStore).
			WithOperatorStore(operatorStore),
	)
	slaHandler := slaapi.NewHandler(slaReportStore, operatorStore, slaReportEngine, log.Logger)
	alertHandler.WithReportEngine(slaReportEngine)
	notifHandler := notifapi.NewHandler(notifStore, notifConfigStore, auditSvc, log.Logger)
	violationHandler := violationapi.NewHandler(violationStore, log.Logger,
		violationapi.WithAuditSvc(auditSvc),
		violationapi.WithSIMStore(simStore),
	)

	dashboardSessionStore := store.NewRadiusSessionStore(pg.Pool)

	sessionCounter, err := aaasession.RegisterSessionCounter(eventBus, rdb.Client, dashboardSessionStore, log.Logger)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to register session counter")
	}
	sessionCounter.Start(appCtx)

	dashboardHandler := dashboardapi.NewHandler(simStore, dashboardSessionStore, operatorStore, anomalyStore, alertStore, apnStore, log.Logger,
		dashboardapi.WithRedisClient(rdb.Client),
		dashboardapi.WithCDRStore(cdrStore),
		dashboardapi.WithSessionCounter(sessionCounter),
		dashboardapi.WithIPPoolStore(ippoolStore),
		dashboardapi.WithMetricsCollector(metricsCollector),
		dashboardapi.WithAggregates(aggSvc),
	)

	if err := dashboardapi.RegisterDashboardInvalidator(eventBus, rdb.Client, log.Logger); err != nil {
		log.Fatal().Err(err).Msg("failed to register dashboard cache invalidator")
	}

	policyMatcher := policy.NewMatcher(policyStore, simStore, apnStore, log.Logger)
	if err := policyMatcher.Register(eventBus); err != nil {
		log.Fatal().Err(err).Msg("failed to register policy matcher")
	}

	webhookConfigStore := store.NewWebhookConfigStore(pg.Pool, cfg.EncryptionKey)
	webhookDeliveryStore := store.NewWebhookDeliveryStore(pg.Pool)
	webhookDispatcher := notification.NewDispatcher(webhookConfigStore, webhookDeliveryStore, nil)
	webhookHandler := webhookapi.NewHandler(webhookConfigStore, webhookDeliveryStore, webhookDispatcher, auditSvc, log.Logger)

	// STORY-069 — wire onboarding/reports/sms/notifications-prefs/data-portability
	scheduledReportStore := store.NewScheduledReportStore(pg.Pool)
	notifPrefStore := store.NewNotificationPreferenceStore(pg.Pool)
	notifTemplateStore := store.NewNotificationTemplateStore(pg.Pool)
	smsOutboundStore := store.NewSMSOutboundStore(pg.Pool)
	onboardingSessionStore := store.NewOnboardingSessionStore(pg.Pool)

	// FIX-303: wire onboarding-session lookup into auth service so login
	// responses populate UserInfo.OnboardingCompleted. The FE first-login
	// redirect guard reads this field; without it the flag is always false
	// (fail-safe) and brand-new tenants always go to the wizard.
	authSvc.WithOnboardingSessions(onboardingSessionStore)

	notifSvc.SetPrefStore(&notifPrefAdapter{store: notifPrefStore})
	notifSvc.SetTemplateStore(&notifTemplateAdapter{store: notifTemplateStore})
	notifHandler.SetPrefStore(notifPrefStore)
	notifHandler.SetTemplateStore(notifTemplateStore)

	smsRateLimit := cfg.NotificationRateLimitPerMin
	if smsRateLimit <= 0 {
		smsRateLimit = 60
	}
	smsHandler := smsapi.NewHandler(simStore, smsOutboundStore, jobStore, eventBus, rdb.Client, auditSvc, smsRateLimit, log.Logger)

	reportsHandler := reportsapi.NewHandler(scheduledReportStore, jobStore, eventBus, auditSvc, log.Logger)

	onboardingHandler := onboardingapi.New(
		onboardingSessionStore,
		tenantStore,
		userStore,
		operatorStore,
		apnStore,
		&onboardingBulkImportAdapter{jobs: jobStore, eventBus: eventBus},
		nil,
		&onboardingNotifierAdapter{svc: notifSvc},
		auditSvc,
		log.Logger,
	)

	// STORY-069 — register processors + cron entries
	smsGatewaySender := notification.NewSMSGatewaySender(notification.SMSConfig{
		Provider:          cfg.SMSProvider,
		AccountID:         cfg.SMSAccountID,
		AuthToken:         cfg.SMSAuthToken,
		FromPhone:         cfg.SMSFromNumber,
		StatusCallbackURL: cfg.SMSStatusCallbackURL,
	}, log.Logger)

	kvkkPurgeProc := job.NewKVKKPurgeProcessor(pg.Pool, dataLifecycleStore, tenantStore, auditStore, jobStore, eventBus, metricsReg, log.Logger)
	ipGraceReleaseProc := job.NewIPGraceReleaseProcessor(jobStore, ippoolStore, eventBus, &auditRecorderAdapter{svc: auditSvc}, metricsReg, log.Logger)
	webhookRetryProc := job.NewWebhookRetryProcessor(webhookDeliveryStore, webhookConfigStore, jobStore, eventBus, metricsReg, log.Logger)
	scheduledReportEngine := report.NewEngine(report.NewStoreProvider(complianceStore, cdrStore, auditStore, simStore, slaReportStore))
	reportStorage := selectReportStorage(cfg, s3Impl, log.Logger)
	scheduledReportProc := job.NewScheduledReportProcessor(jobStore, scheduledReportStore, scheduledReportEngine, reportStorage, eventBus, metricsReg, log.Logger)
	scheduledReportSweeper := job.NewScheduledReportSweeper(jobStore, scheduledReportStore, jobStore, eventBus, log.Logger)
	smsGatewayProc := job.NewSMSGatewayProcessor(smsOutboundStore, smsGatewaySender, rdb.Client, eventBus, log.Logger)

	jobRunner.Register(kvkkPurgeProc)
	jobRunner.Register(ipGraceReleaseProc)
	jobRunner.Register(webhookRetryProc)
	jobRunner.Register(scheduledReportProc)
	jobRunner.Register(scheduledReportSweeper)
	jobRunner.Register(smsGatewayProc)
	log.Info().Msg("STORY-069 processors registered (kvkk_purge, ip_grace_release, webhook_retry, scheduled_report+sweeper, sms_outbound_send)")

	if cronScheduler != nil {
		cronScheduler.AddEntry(job.CronEntry{Name: "kvkk_purge_daily", Schedule: "@daily", JobType: job.JobTypeKVKKPurgeDaily})
		cronScheduler.AddEntry(job.CronEntry{Name: "ip_grace_release", Schedule: "@hourly", JobType: job.JobTypeIPGraceRelease})
		cronScheduler.AddEntry(job.CronEntry{Name: "webhook_retry_sweep", Schedule: "*/1 * * * *", JobType: job.JobTypeWebhookRetry})
		cronScheduler.AddEntry(job.CronEntry{Name: "scheduled_report_sweeper", Schedule: "*/1 * * * *", JobType: job.JobTypeScheduledReportSweeper})
	}

	// FIX-237 — Fleet digest worker (Tier 2 aggregator). Runs every 15 min.
	digestThresholds := digest.LoadThresholds()
	digestWorker := digest.NewWorker(
		simStore,
		cdrStore,
		violationStore,
		notifSvc,
		eventBus,
		digestThresholds,
		log.Logger,
		nil,
	)
	jobRunner.Register(digestWorker)
	if cronScheduler != nil {
		cronScheduler.AddEntry(job.CronEntry{
			Name:     "fleet_digest",
			Schedule: cfg.CronFleetDigest,
			JobType:  digest.JobTypeFleetDigest,
		})
	}

	// Kill-switch service — env-backed, no DB dependency.
	killSwitchSvc := killswitch.NewService(log.Logger)
	adminHandler := adminapi.NewHandler(
		tenantStore,
		sessionStore,
		apiKeyStore,
		jobStore,
		webhookDeliveryStore,
		notifStore,
		smsOutboundStore,
		auditStore,
		auditSvc,
		pg.Pool,
		rdb.Client,
		log.Logger,
	)
	// Wire kill-switch into enforcement points
	if radiusServer != nil {
		radiusServer.SetKillSwitch(killSwitchSvc)
	}
	bulkHandler.SetKillSwitch(killSwitchSvc)
	notifSvc.SetKillSwitch(killSwitchSvc)
	log.Info().Msg("STORY-073 admin compliance handlers + kill-switch enforcement wired")

	// STORY-077 — Enterprise UX Polish: GeoIP, impersonation, announcements, undo
	var geoipLookup *geoip.Lookup
	if cfg.GeoIPDBPath != "" {
		gl, geoipErr := geoip.New(cfg.GeoIPDBPath)
		if geoipErr != nil {
			log.Warn().Err(geoipErr).Msg("geoip: failed to open DB, location lookup disabled")
		} else {
			geoipLookup = gl
			log.Info().Str("path", cfg.GeoIPDBPath).Msg("geoip: GeoLite2 DB loaded")
		}
	}
	adminHandler.WithUserStore(userStore)
	adminHandler.WithJWTSecret(cfg.JWTSecret)
	if geoipLookup != nil {
		adminHandler.WithGeoIP(geoipLookup)
	}
	adminHandler.WithCDRStore(cdrStore)

	announcementStore := store.NewAnnouncementStore(pg.Pool)
	announcementHandler := announcementapi.NewHandler(announcementStore, auditSvc, log.Logger)
	adminHandler.WithAnnouncementStore(announcementStore)

	undoRegistry := undo.NewRegistry(rdb.Client)
	undoHandler := undoapi.NewHandler(undoRegistry, auditSvc, log.Logger)
	simapi.WithUndoRegistry(undoRegistry)(simHandler)
	policyHandler.WithUndoRegistry(undoRegistry)
	undoHandler.RegisterExecutor("sim_state_restore", func(ctx context.Context, tenantID, _ uuid.UUID, payload json.RawMessage) error {
		var req struct {
			SimID string `json:"sim_id"`
			State string `json:"state"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			return err
		}
		simID, err := uuid.Parse(req.SimID)
		if err != nil {
			return err
		}
		_, err = simStore.RestoreState(ctx, tenantID, simID, req.State)
		return err
	})
	undoHandler.RegisterExecutor("policy_restore", func(ctx context.Context, tenantID, _ uuid.UUID, payload json.RawMessage) error {
		var req struct {
			PolicyID string `json:"policy_id"`
			State    string `json:"state"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			return err
		}
		policyID, err := uuid.Parse(req.PolicyID)
		if err != nil {
			return err
		}
		_, err = policyStore.RestoreState(ctx, tenantID, policyID, req.State)
		return err
	})
	_ = undoHandler
	_ = announcementHandler
	log.Info().Msg("STORY-077 UX polish handlers wired")

	dataIntegrityProc := job.NewDataIntegrityDetector(pg.Pool, jobStore, eventBus, metricsReg, log.Logger)
	jobRunner.Register(dataIntegrityProc)
	log.Info().Msg("FIX-207 data_integrity_scan processor registered")

	if cronScheduler != nil {
		cronScheduler.AddEntry(job.CronEntry{Name: "data_integrity_scan", Schedule: "17 3 * * *", JobType: job.JobTypeDataIntegrityScan})
	}

	health := gateway.NewHealthHandler(pg, rdb, ns)
	if radiusServer != nil {
		health.SetAAAChecker(radiusServer)
	}
	if diameterServer != nil {
		health.SetDiameterChecker(diameterServer)
	}
	if sbaServer != nil {
		health.SetSBAChecker(sbaServer)
	}
	diskMountsRaw := strings.Split(cfg.DiskProbeMount, ",")
	diskMounts := make([]string, 0, len(diskMountsRaw))
	for _, m := range diskMountsRaw {
		if m = strings.TrimSpace(m); m != "" {
			diskMounts = append(diskMounts, m)
		}
	}
	health.SetDiskConfig(diskMounts, cfg.DiskDegradedPct, cfg.DiskUnhealthyPct)
	health.SetMetricsRegistry(metricsReg)
	log.Logger.Info().Strs("mounts", diskMounts).Int("degraded_pct", cfg.DiskDegradedPct).Int("unhealthy_pct", cfg.DiskUnhealthyPct).Msg("disk probe configured")
	secHeadersCfg := gateway.DefaultSecurityHeadersConfig()
	if cfg.CSPDirectives != "" {
		secHeadersCfg.CSPDirectives = cfg.CSPDirectives
	}

	corsCfg := gateway.DefaultCORSConfig()
	if cfg.DevCORSAllowAll && cfg.IsDev() {
		corsCfg.AllowAllOrigins = true
	} else if cfg.CORSAllowedOrigins != "" {
		corsCfg.AllowedOrigins = strings.Split(cfg.CORSAllowedOrigins, ",")
	}

	bfCfg := gateway.DefaultBruteForceConfig()
	if cfg.BruteForceMaxAttempts > 0 {
		bfCfg.MaxAttempts = cfg.BruteForceMaxAttempts
	}
	if cfg.BruteForceWindowSeconds > 0 {
		bfCfg.WindowSeconds = cfg.BruteForceWindowSeconds
	}

	reliabilityBackupStore := store.NewBackupStore(pg.Pool)
	reliabilityHandler := systemapi.NewReliabilityHandler(reliabilityBackupStore, auditStore, cfg, log.Logger)

	opsHandler := opsapi.NewHandler(
		metricsReg,
		pg.Pool,
		rdb.Client,
		ns.JetStream,
		auditStore,
		anomalyStore,
		log.Logger,
	)

	statusHandler := systemapi.NewStatusHandler(health, tenantStore, metricsReg, version, gitSHA, buildTime)
	systemConfigHandler := systemapi.NewConfigHandler(cfg, version, gitSHA, buildTime)

	capacitySimStore := store.NewSIMStore(pg.Pool)
	capacitySessionStore := store.NewRadiusSessionStore(pg.Pool)
	capacityIPPoolStore := store.NewIPPoolStore(pg.Pool)
	capacityCDRStore := store.NewCDRStore(pg.Pool)
	capacityHandler := systemapi.NewCapacityHandler(
		systemapi.CapacityConfig{
			SIMs:          cfg.CapacitySIMs,
			Sessions:      cfg.CapacitySessions,
			AuthPerSec:    cfg.CapacityAuthPerSec,
			MonthlyGrowth: cfg.CapacityMonthlyGrowth,
		},
		capacitySimStore,
		capacitySessionStore,
		capacityIPPoolStore,
		capacityCDRStore,
		aggSvc,
	)

	bulkRateLimiter := gateway.NewBulkRateLimiter(1.0, 2)
	defer bulkRateLimiter.Shutdown()

	tenantLimits := gateway.NewTenantLimitsMiddleware(
		tenantStore,
		map[gateway.LimitKey]gateway.CountFn{
			gateway.LimitSIMs:    aggSvc.SIMCountByTenant,
			gateway.LimitAPNs:    apnStore.CountByTenant,
			gateway.LimitUsers:   userStore.CountByTenant,
			gateway.LimitAPIKeys: apiKeyStore.CountByTenant,
		},
		rdb.Client,
		5*time.Minute,
		log.Logger,
	)

	router := gateway.NewRouterWithDeps(gateway.RouterDeps{
		Health:               health,
		AuthHandler:          authHandler,
		TenantHandler:        tenantHandler,
		UserHandler:          userHandler,
		AuditHandler:         auditHandler,
		APIKeyHandler:        apiKeyHandler,
		OperatorHandler:      operatorHandler,
		APNHandler:           apnHandler,
		IPPoolHandler:        ippoolHandler,
		SIMHandler:           simHandler,
		ESimHandler:          esimHandler,
		SegmentHandler:       segmentHandler,
		BulkHandler:          bulkHandler,
		JobHandler:           jobHandler,
		MSISDNHandler:        msisdnHandler,
		SessionHandler:       sessionHandler,
		PolicyHandler:        policyHandler,
		OTAHandler:           otaHandler,
		DiagnosticsHandler:   diagHandler,
		CDRHandler:           cdrHandler,
		AnalyticsHandler:     analyticsHandler,
		AnomalyHandler:       anomalyHandler,
		AlertHandler:         alertHandler,
		EventsCatalogHandler: eventsCatalogHandler,
		NotificationHandler:  notifHandler,
		MetricsHandler:       metricsHandler,
		ViolationHandler:     violationHandler,
		DashboardHandler:     dashboardHandler,
		SLAHandler:           slaHandler,
		ReportsHandler:       reportsHandler,
		ReportDownload:       buildReportDownloadDeps(reportStorage, cfg, log.Logger),
		ReliabilityHandler:   reliabilityHandler,
		StatusHandler:        statusHandler,
		SystemConfigHandler:  systemConfigHandler,
		CapacityHandler:      capacityHandler,
		OnboardingHandler:    onboardingHandler,
		WebhookHandler:       webhookHandler,
		SMSHandler:           smsHandler,
		OpsHandler:           opsHandler,
		AdminHandler:         adminHandler,
		SearchHandler:        searchHandler,
		AnnouncementHandler:  announcementHandler,
		UndoHandler:          undoHandler,
		KillSwitchSvc:        killSwitchSvc,
		APIKeyStore:          apiKeyStore,
		TenantLimits:         tenantLimits,
		BulkRateLimiter:      bulkRateLimiter,
		RedisClient:          rdb.Client,
		RateLimitPerMinute:   cfg.RateLimitPerMinute,
		RateLimitPerHour:     cfg.RateLimitPerHour,
		JWTSecret:            cfg.JWTSecret,
		JWTSecretPrevious:    cfg.JWTSecretPrevious,
		Logger:               log.Logger,
		SecurityHeadersCfg:   &secHeadersCfg,
		CORSConfig:           &corsCfg,
		BruteForceCfg:        &bfCfg,
		EnableInputSanitizer: true,
		MetricsReg:           metricsReg,
	})

	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", srv.Addr).Msg("http server listening")
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Info().Str("signal", sig.String()).Msg("shutting down")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}

	gracefulShutdown(
		appCtx,
		appCancel,
		cfg,
		srv,
		radiusServer,
		diameterServer,
		sbaServer,
		wsServer,
		wsHub,
		sessionSweeper,
		cronScheduler,
		timeoutDetector,
		orphanDetector,
		jobRunner,
		metricsPusher,
		notifSvc,
		healthChecker,
		anomalyEngine,
		lagPoller,
		cdrConsumer,
		auditSvc,
		otelShutdown,
		ns,
		rdb,
		pg,
		log.Logger,
	)
}

// AC-5: Ordered graceful shutdown.
// Order rationale: ingress drains first so no new work arrives; then control plane so no new
// background tasks spawn; then data plane so in-flight work completes; then observability
// flush so traces/metrics for shutdown itself land; THEN infra close so flush operations have
// live connections. Per-subsystem timeout from cfg.Shutdown*Sec.
func gracefulShutdown(
	appCtx context.Context,
	appCancel context.CancelFunc,
	cfg *config.Config,
	srv *http.Server,
	radiusServer *aaaradius.Server,
	diameterServer *aaadiameter.Server,
	sbaServer *aaasba.Server,
	wsServer *ws.Server,
	wsHub *ws.Hub,
	sessionSweeper *aaasession.TimeoutSweeper,
	cronScheduler *job.Scheduler,
	timeoutDetector *job.TimeoutDetector,
	orphanDetector *job.OrphanSessionDetector,
	jobRunner *job.Runner,
	metricsPusher *analyticmetrics.Pusher,
	notifSvc *notification.Service,
	healthChecker *operator.HealthChecker,
	anomalyEngine *anomalysvc.Engine,
	lagPoller *bus.LagPoller,
	cdrConsumer *cdrsvc.Consumer,
	auditSvc *audit.FullService,
	otelShutdown func(context.Context) error,
	ns *bus.NATS,
	rdb *cache.Redis,
	pg *store.Postgres,
	logger zerolog.Logger,
) {
	// 1. HTTP server — ingress drain.
	t := time.Now()
	httpCtx, httpCancel := context.WithTimeout(appCtx, time.Duration(cfg.ShutdownHTTPSec)*time.Second)
	defer httpCancel()
	logger.Info().Str("subsystem", "http").Msg("shutdown step starting")
	if err := srv.Shutdown(httpCtx); err != nil {
		logger.Error().Err(err).Str("subsystem", "http").Msg("shutdown error")
	}
	logger.Info().Str("subsystem", "http").Dur("duration", time.Since(t)).Msg("shutdown step done")

	// 2. RADIUS server stop.
	if radiusServer != nil {
		t = time.Now()
		radCtx, radCancel := context.WithTimeout(appCtx, time.Duration(cfg.ShutdownRADIUSSec)*time.Second)
		defer radCancel()
		logger.Info().Str("subsystem", "radius").Msg("shutdown step starting")
		if err := radiusServer.Stop(radCtx); err != nil {
			logger.Error().Err(err).Str("subsystem", "radius").Msg("shutdown error")
		}
		logger.Info().Str("subsystem", "radius").Dur("duration", time.Since(t)).Msg("shutdown step done")
	}

	// 3. Diameter server stop.
	if diameterServer != nil {
		t = time.Now()
		logger.Info().Str("subsystem", "diameter").Msg("shutdown step starting")
		diameterServer.Stop()
		logger.Info().Str("subsystem", "diameter").Dur("duration", time.Since(t)).Msg("shutdown step done")
	}

	// 4. SBA server stop (NRF deregister first).
	if sbaServer != nil {
		t = time.Now()
		logger.Info().Str("subsystem", "sba").Msg("shutdown step starting")
		_ = sbaServer.NRFRegistration().Deregister()
		sbaServer.Stop()
		logger.Info().Str("subsystem", "sba").Dur("duration", time.Since(t)).Msg("shutdown step done")
	}

	// 5. WebSocket server stop (broadcast reconnect hint first, then drain).
	t = time.Now()
	logger.Info().Str("subsystem", "ws").Msg("shutdown step starting")
	wsHub.BroadcastReconnect("server shutting down", 2000)
	time.Sleep(500 * time.Millisecond)
	wsCtx, wsCancel := context.WithTimeout(appCtx, time.Duration(cfg.ShutdownWSSec)*time.Second)
	defer wsCancel()
	if err := wsServer.Stop(wsCtx); err != nil {
		logger.Error().Err(err).Str("subsystem", "ws").Msg("shutdown error")
	}
	wsHub.Stop()
	logger.Info().Str("subsystem", "ws").Dur("duration", time.Since(t)).Msg("shutdown step done")

	// 6. Control-plane services — sync stop (no per-subsystem timeout needed).
	if sessionSweeper != nil {
		t = time.Now()
		logger.Info().Str("subsystem", "session_sweeper").Msg("shutdown step starting")
		sessionSweeper.Stop()
		logger.Info().Str("subsystem", "session_sweeper").Dur("duration", time.Since(t)).Msg("shutdown step done")
	}
	if cronScheduler != nil {
		t = time.Now()
		logger.Info().Str("subsystem", "cron_scheduler").Msg("shutdown step starting")
		cronScheduler.Stop()
		logger.Info().Str("subsystem", "cron_scheduler").Dur("duration", time.Since(t)).Msg("shutdown step done")
	}
	t = time.Now()
	logger.Info().Str("subsystem", "timeout_detector").Msg("shutdown step starting")
	timeoutDetector.Stop()
	logger.Info().Str("subsystem", "timeout_detector").Dur("duration", time.Since(t)).Msg("shutdown step done")

	t = time.Now()
	logger.Info().Str("subsystem", "orphan_session_detector").Msg("shutdown step starting")
	orphanDetector.Stop()
	logger.Info().Str("subsystem", "orphan_session_detector").Dur("duration", time.Since(t)).Msg("shutdown step done")

	// 7. Job runner — allow in-flight jobs to complete within ShutdownJobSec budget.
	t = time.Now()
	logger.Info().Str("subsystem", "job_runner").Msg("shutdown step starting")
	jobRunner.Stop()
	logger.Info().Str("subsystem", "job_runner").Dur("duration", time.Since(t)).Msg("shutdown step done")

	// 8. Data-plane services — stop in declared order.
	t = time.Now()
	logger.Info().Str("subsystem", "metrics_pusher").Msg("shutdown step starting")
	metricsPusher.Stop()
	logger.Info().Str("subsystem", "metrics_pusher").Dur("duration", time.Since(t)).Msg("shutdown step done")

	t = time.Now()
	logger.Info().Str("subsystem", "notification").Msg("shutdown step starting")
	notifSvc.Stop()
	logger.Info().Str("subsystem", "notification").Dur("duration", time.Since(t)).Msg("shutdown step done")

	t = time.Now()
	logger.Info().Str("subsystem", "health_checker").Msg("shutdown step starting")
	healthChecker.Stop()
	logger.Info().Str("subsystem", "health_checker").Dur("duration", time.Since(t)).Msg("shutdown step done")

	t = time.Now()
	logger.Info().Str("subsystem", "anomaly_engine").Msg("shutdown step starting")
	anomalyEngine.Stop()
	logger.Info().Str("subsystem", "anomaly_engine").Dur("duration", time.Since(t)).Msg("shutdown step done")

	t = time.Now()
	logger.Info().Str("subsystem", "lag_poller").Msg("shutdown step starting")
	lagPoller.Stop()
	logger.Info().Str("subsystem", "lag_poller").Dur("duration", time.Since(t)).Msg("shutdown step done")

	t = time.Now()
	logger.Info().Str("subsystem", "cdr_consumer").Msg("shutdown step starting")
	cdrConsumer.Stop()
	logger.Info().Str("subsystem", "cdr_consumer").Dur("duration", time.Since(t)).Msg("shutdown step done")

	t = time.Now()
	logger.Info().Str("subsystem", "audit").Msg("shutdown step starting")
	auditSvc.Stop()
	logger.Info().Str("subsystem", "audit").Dur("duration", time.Since(t)).Msg("shutdown step done")

	// 9. OTel flush — MUST run BEFORE infra close so in-flight spans flush with
	// NATS/Redis/DB connections still alive. See STORY-065 rationale.
	t = time.Now()
	logger.Info().Str("subsystem", "otel").Msg("shutdown step starting")
	otelCtx, otelCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := otelShutdown(otelCtx); err != nil {
		logger.Error().Err(err).Str("subsystem", "otel").Msg("shutdown error")
	}
	otelCancel()
	logger.Info().Str("subsystem", "otel").Dur("duration", time.Since(t)).Msg("shutdown step done")

	// 10. Cancel appCtx so long-lived background goroutines (pool gauge etc.)
	// exit before their underlying resources are torn down.
	appCancel()

	// 11. NATS flush then close.
	t = time.Now()
	logger.Info().Str("subsystem", "nats").Msg("shutdown step starting")
	if err := ns.Conn.FlushTimeout(time.Duration(cfg.ShutdownNATSSec) * time.Second); err != nil {
		logger.Error().Err(err).Str("subsystem", "nats").Msg("flush error")
	}
	ns.Close()
	logger.Info().Str("subsystem", "nats").Dur("duration", time.Since(t)).Msg("shutdown step done")

	// 12. Redis close.
	t = time.Now()
	logger.Info().Str("subsystem", "redis").Msg("shutdown step starting")
	if err := rdb.Close(); err != nil {
		logger.Error().Err(err).Str("subsystem", "redis").Msg("shutdown error")
	}
	logger.Info().Str("subsystem", "redis").Dur("duration", time.Since(t)).Msg("shutdown step done")

	// 13. PostgreSQL close.
	t = time.Now()
	logger.Info().Str("subsystem", "postgres").Msg("shutdown step starting")
	pg.Close()
	logger.Info().Str("subsystem", "postgres").Dur("duration", time.Since(t)).Msg("shutdown step done")

	logger.Info().Msg("argus stopped gracefully")
}

type userStoreAdapter struct {
	s *store.UserStore
}

func (a *userStoreAdapter) GetByEmail(ctx context.Context, email string) (*auth.User, error) {
	u, err := a.s.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return storeUserToAuth(u), nil
}

func (a *userStoreAdapter) GetByID(ctx context.Context, id uuid.UUID) (*auth.User, error) {
	u, err := a.s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return storeUserToAuth(u), nil
}

func (a *userStoreAdapter) UpdateLoginSuccess(ctx context.Context, id uuid.UUID) error {
	return a.s.UpdateLoginSuccess(ctx, id)
}

func (a *userStoreAdapter) IncrementFailedLogin(ctx context.Context, id uuid.UUID, lockUntil *time.Time) error {
	return a.s.IncrementFailedLogin(ctx, id, lockUntil)
}

func (a *userStoreAdapter) SetTOTPSecret(ctx context.Context, id uuid.UUID, secret string) error {
	return a.s.SetTOTPSecret(ctx, id, secret)
}

func (a *userStoreAdapter) EnableTOTP(ctx context.Context, id uuid.UUID) error {
	return a.s.EnableTOTP(ctx, id)
}

func (a *userStoreAdapter) SetPasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	return a.s.SetPasswordHash(ctx, id, hash)
}

func (a *userStoreAdapter) SetPasswordChangeRequired(ctx context.Context, id uuid.UUID, required bool) error {
	return a.s.SetPasswordChangeRequired(ctx, id, required)
}

func (a *userStoreAdapter) ClearLockout(ctx context.Context, id uuid.UUID) error {
	return a.s.ClearLockout(ctx, id)
}

func storeUserToAuth(u *store.User) *auth.User {
	return &auth.User{
		ID:                     u.ID,
		TenantID:               u.TenantID,
		Email:                  u.Email,
		PasswordHash:           u.PasswordHash,
		Name:                   u.Name,
		Role:                   u.Role,
		TOTPSecret:             u.TOTPSecret,
		TOTPEnabled:            u.TOTPEnabled,
		State:                  u.State,
		LastLoginAt:            u.LastLoginAt,
		FailedLoginCount:       u.FailedLoginCount,
		LockedUntil:            u.LockedUntil,
		PasswordChangeRequired: u.PasswordChangeRequired,
		PasswordChangedAt:      u.PasswordChangedAt,
	}
}

type sessionStoreAdapter struct {
	s *store.SessionStore
}

func (a *sessionStoreAdapter) Create(ctx context.Context, params auth.CreateSessionParams) (*auth.UserSession, error) {
	sess, err := a.s.Create(ctx, store.CreateSessionParams{
		UserID:           params.UserID,
		RefreshTokenHash: params.RefreshTokenHash,
		IPAddress:        params.IPAddress,
		UserAgent:        params.UserAgent,
		ExpiresAt:        params.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	return storeSessionToAuth(sess), nil
}

func (a *sessionStoreAdapter) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	return a.s.RevokeSession(ctx, sessionID)
}

func (a *sessionStoreAdapter) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	return a.s.RevokeAllUserSessions(ctx, userID)
}

func (a *sessionStoreAdapter) GetByID(ctx context.Context, id uuid.UUID) (*auth.UserSession, error) {
	sess, err := a.s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return storeSessionToAuth(sess), nil
}

func (a *sessionStoreAdapter) GetActiveByUserID(ctx context.Context, userID uuid.UUID) ([]auth.UserSession, error) {
	sessions, err := a.s.GetActiveByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]auth.UserSession, len(sessions))
	for i, sess := range sessions {
		result[i] = *storeSessionToAuth(&sess)
	}
	return result, nil
}

func (a *sessionStoreAdapter) ListActiveByUserID(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]auth.UserSession, string, error) {
	sessions, nextCursor, err := a.s.ListActiveByUserID(ctx, userID, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	result := make([]auth.UserSession, len(sessions))
	for i, sess := range sessions {
		result[i] = *storeSessionToAuth(&sess)
	}
	return result, nextCursor, nil
}

func storeSessionToAuth(s *store.UserSession) *auth.UserSession {
	return &auth.UserSession{
		ID:               s.ID,
		UserID:           s.UserID,
		RefreshTokenHash: s.RefreshTokenHash,
		IPAddress:        s.IPAddress,
		UserAgent:        s.UserAgent,
		CreatedAt:        s.CreatedAt,
		ExpiresAt:        s.ExpiresAt,
		RevokedAt:        s.RevokedAt,
	}
}

type eventBusSubscriber struct {
	eb *bus.EventBus
}

func (a *eventBusSubscriber) QueueSubscribe(subject, queue string, handler func(string, []byte)) (audit.Subscription, error) {
	return a.eb.QueueSubscribe(subject, queue, handler)
}

type natsSubWrapper struct {
	sub *nats.Subscription
}

func (s *natsSubWrapper) Unsubscribe() error {
	return s.sub.Unsubscribe()
}

type eventBusNotifSubscriber struct {
	eb *bus.EventBus
}

func (a *eventBusNotifSubscriber) QueueSubscribe(subject, queue string, handler func(string, []byte)) (notification.Subscription, error) {
	sub, err := a.eb.QueueSubscribe(subject, queue, handler)
	if err != nil {
		return nil, err
	}
	return &natsSubWrapper{sub: sub}, nil
}

type eventBusWSSubscriber struct {
	eb *bus.EventBus
}

func (a *eventBusWSSubscriber) QueueSubscribe(subject, queue string, handler func(string, []byte)) (ws.Subscription, error) {
	sub, err := a.eb.QueueSubscribe(subject, queue, handler)
	if err != nil {
		return nil, err
	}
	return &natsSubWrapper{sub: sub}, nil
}

type eventBusCDRSubscriber struct {
	eb *bus.EventBus
}

func (a *eventBusCDRSubscriber) QueueSubscribe(subject, queue string, handler func(string, []byte)) (cdrsvc.Subscription, error) {
	sub, err := a.eb.QueueSubscribe(subject, queue, handler)
	if err != nil {
		return nil, err
	}
	return &natsSubWrapper{sub: sub}, nil
}

type rolloutSessionAdapter struct {
	mgr *aaasession.Manager
}

func (a *rolloutSessionAdapter) GetSessionsForSIM(ctx context.Context, simID string) ([]rollout.SessionInfo, error) {
	sessions, err := a.mgr.GetSessionsForSIM(ctx, simID)
	if err != nil {
		return nil, err
	}
	result := make([]rollout.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, rollout.SessionInfo{
			ID:            s.ID,
			SimID:         s.SimID,
			TenantID:      s.TenantID,
			NASIP:         s.NASIP,
			AcctSessionID: s.AcctSessionID,
			IMSI:          s.IMSI,
		})
	}
	return result, nil
}

type rolloutCoAAdapter struct {
	sender *aaasession.CoASender
}

func (a *rolloutCoAAdapter) SendCoA(ctx context.Context, req rollout.CoARequest) (*rollout.CoAResult, error) {
	tenantID, _ := uuid.Parse(req.TenantID)
	result, err := a.sender.SendCoA(ctx, aaasession.CoARequest{
		NASIP:         req.NASIP,
		AcctSessionID: req.AcctSessionID,
		IMSI:          req.IMSI,
		SessionID:     req.SessionID,
		TenantID:      tenantID,
		Attributes:    req.Attributes,
	})
	if err != nil {
		return nil, err
	}
	return &rollout.CoAResult{
		Status:  result.Status,
		Message: result.Message,
	}, nil
}

type bulkPolicySessionAdapter struct {
	mgr *aaasession.Manager
}

func (a *bulkPolicySessionAdapter) GetSessionsForSIM(ctx context.Context, simID string) ([]job.BulkSessionInfo, error) {
	sessions, err := a.mgr.GetSessionsForSIM(ctx, simID)
	if err != nil {
		return nil, err
	}
	result := make([]job.BulkSessionInfo, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, job.BulkSessionInfo{
			ID:            s.ID,
			SimID:         s.SimID,
			TenantID:      s.TenantID,
			NASIP:         s.NASIP,
			AcctSessionID: s.AcctSessionID,
			IMSI:          s.IMSI,
		})
	}
	return result, nil
}

type bulkPolicyCoAAdapter struct {
	sender *aaasession.CoASender
}

func (a *bulkPolicyCoAAdapter) SendCoA(ctx context.Context, req job.BulkCoARequest) (*job.BulkCoAResult, error) {
	tenantID, _ := uuid.Parse(req.TenantID)
	result, err := a.sender.SendCoA(ctx, aaasession.CoARequest{
		NASIP:         req.NASIP,
		AcctSessionID: req.AcctSessionID,
		IMSI:          req.IMSI,
		SessionID:     req.SessionID,
		TenantID:      tenantID,
		Attributes:    req.Attributes,
	})
	if err != nil {
		return nil, err
	}
	return &job.BulkCoAResult{
		Status:  result.Status,
		Message: result.Message,
	}, nil
}

type eventBusAnomalySubscriber struct {
	eb *bus.EventBus
}

func (a *eventBusAnomalySubscriber) QueueSubscribe(subject, queue string, handler func(string, []byte)) (anomalysvc.Subscription, error) {
	sub, err := a.eb.QueueSubscribe(subject, queue, handler)
	if err != nil {
		return nil, err
	}
	return &natsSubWrapper{sub: sub}, nil
}

type simSuspenderAdapter struct {
	s *store.SIMStore
}

func (a *simSuspenderAdapter) Suspend(ctx context.Context, tenantID, simID uuid.UUID, userID *uuid.UUID, reason *string) error {
	_, err := a.s.Suspend(ctx, tenantID, simID, userID, reason)
	return err
}

type notifStoreAdapter struct {
	s *store.NotificationStore
}

func (a *notifStoreAdapter) Create(ctx context.Context, p notification.NotifCreateParams) (*notification.NotifRow, error) {
	row, err := a.s.Create(ctx, store.CreateNotificationParams{
		TenantID:     p.TenantID,
		UserID:       p.UserID,
		EventType:    p.EventType,
		ScopeType:    p.ScopeType,
		ScopeRefID:   p.ScopeRefID,
		Title:        p.Title,
		Body:         p.Body,
		Severity:     p.Severity,
		ChannelsSent: p.ChannelsSent,
	})
	if err != nil {
		return nil, err
	}
	return &notification.NotifRow{
		ID:        row.ID,
		TenantID:  row.TenantID,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (a *notifStoreAdapter) UpdateDelivery(ctx context.Context, id uuid.UUID, sentAt, deliveredAt, failedAt *time.Time, retryCount int, channelsSent []string) error {
	return a.s.UpdateDelivery(ctx, id, sentAt, deliveredAt, failedAt, retryCount, channelsSent)
}

type inAppStoreAdapter struct {
	s *store.NotificationStore
}

func (a *inAppStoreAdapter) CreateNotification(ctx context.Context, n notification.InAppNotification) error {
	if n.EntityID == uuid.Nil {
		return nil
	}
	_, err := a.s.Create(ctx, store.CreateNotificationParams{
		TenantID:     n.EntityID,
		EventType:    n.AlertType,
		ScopeType:    n.EntityType,
		ScopeRefID:   &n.EntityID,
		Title:        n.Title,
		Body:         n.Body,
		Severity:     n.Severity,
		ChannelsSent: n.ChannelsSent,
	})
	return err
}

// ── STORY-069 adapters ──────────────────────────────────────────────────────

type notifPrefAdapter struct {
	store *store.NotificationPreferenceStore
}

func (a *notifPrefAdapter) Get(ctx context.Context, tenantID uuid.UUID, eventType string) (*notification.Preference, error) {
	row, err := a.store.Get(ctx, tenantID, eventType)
	if err != nil || row == nil {
		return nil, err
	}
	return &notification.Preference{
		Channels:          row.Channels,
		SeverityThreshold: row.SeverityThreshold,
		Enabled:           row.Enabled,
	}, nil
}

type notifTemplateAdapter struct {
	store *store.NotificationTemplateStore
}

func (a *notifTemplateAdapter) Get(ctx context.Context, eventType, locale string) (*notification.Template, error) {
	row, err := a.store.Get(ctx, eventType, locale)
	if err != nil {
		return nil, err
	}
	return &notification.Template{
		Subject:  row.Subject,
		BodyText: row.BodyText,
		BodyHTML: row.BodyHTML,
	}, nil
}

type onboardingNotifierAdapter struct {
	svc *notification.Service
}

func (a *onboardingNotifierAdapter) Notify(ctx context.Context, req onboardingapi.NotifyRequest) error {
	return a.svc.Notify(ctx, notification.NotifyRequest{
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		EventType: notification.EventType(req.EventType),
		Title:     req.Title,
		Body:      req.Body,
		Severity:  req.Severity,
	})
}

type onboardingBulkImportAdapter struct {
	jobs     *store.JobStore
	eventBus *bus.EventBus
}

func (a *onboardingBulkImportAdapter) EnqueueImport(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, csvS3Key string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"csv_s3_key": csvS3Key})
	j, err := a.jobs.CreateWithTenantID(ctx, tenantID, store.CreateJobParams{
		Type:      job.JobTypeBulkImport,
		Priority:  5,
		Payload:   payload,
		CreatedBy: userID,
	})
	if err != nil {
		return "", err
	}
	if a.eventBus != nil {
		_ = a.eventBus.Publish(ctx, bus.SubjectJobQueue, job.JobMessage{
			JobID:    j.ID,
			TenantID: j.TenantID,
			Type:     job.JobTypeBulkImport,
		})
	}
	return j.ID.String(), nil
}

// FIX-248 DEV-558: backend selector for report storage. Reads `REPORT_STORAGE`
// env (set via `cfg.ReportStorage`) and instantiates either the LocalFS or
// S3 backend. Both implement `storage.Storage` so downstream consumers
// (scheduled_report) accept the interface and don't care
// which backend is wired.
//
// Defaults to LocalFS for Docker dev environments without S3 access; the
// pre-FIX-248 `nullReportStorage` no-op wrapper is gone — silent no-ops
// were the root cause of "report generation appears to succeed but no file
// exists" reports.
//
// If REPORT_SIGNING_KEY is empty, a 32-byte random key is auto-generated on
// boot and a warning is logged — multi-instance deploys MUST configure a
// stable signing key (otherwise each instance mints incompatible URLs).
func selectReportStorage(cfg *config.Config, s3Impl *storage.S3Uploader, logger zerolog.Logger) storage.Storage {
	backend := strings.ToLower(strings.TrimSpace(cfg.ReportStorage))
	if backend == "" {
		backend = "local"
	}

	switch backend {
	case "s3":
		if s3Impl == nil {
			logger.Error().Msg("REPORT_STORAGE=s3 but S3 not configured; falling back to LocalFS")
			return mustLocalFS(cfg, logger)
		}
		logger.Info().Msg("report storage backend: S3")
		return s3Impl

	case "local":
		fallthrough
	default:
		return mustLocalFS(cfg, logger)
	}
}

// FIX-248 DEV-561: glue function — only the LocalFS backend exposes a
// streamable Open(); when REPORT_STORAGE=s3 the download endpoint is not
// registered (S3 has its own presigned URL).
func buildReportDownloadDeps(s storage.Storage, cfg *config.Config, logger zerolog.Logger) *reportsapi.DownloadDeps {
	local, ok := s.(*storage.LocalFSUploader)
	if !ok {
		logger.Info().Msg("report download endpoint disabled (S3 backend serves its own presigned URLs)")
		return nil
	}
	signingKey, err := hex.DecodeString(cfg.ReportSigningKeyHex)
	if err != nil || len(signingKey) < 16 {
		signingKey = local.SigningKey
	}
	return &reportsapi.DownloadDeps{
		Opener: reportsapi.NewLocalFileOpener(local),
		Verifier: func(key, expires, sig string) error {
			exp, err := storage.ParseExpiresQS(expires)
			if err != nil {
				return err
			}
			return storage.VerifyKey(key, exp, sig, signingKey)
		},
	}
}

func mustLocalFS(cfg *config.Config, logger zerolog.Logger) storage.Storage {
	signingKey, err := hex.DecodeString(cfg.ReportSigningKeyHex)
	if err != nil || len(signingKey) < 16 {
		// Auto-generate a one-shot key. Multi-instance deploys MUST set
		// REPORT_SIGNING_KEY explicitly — different keys yield URLs that
		// only verify on the minting instance.
		signingKey = make([]byte, 32)
		if _, randErr := rand.Read(signingKey); randErr != nil {
			panic(fmt.Sprintf("FIX-248 selectReportStorage: rand: %v", randErr))
		}
		logger.Warn().Msg("REPORT_SIGNING_KEY missing or <16 bytes; using ephemeral key (multi-instance deploys must set this env)")
	}
	uploader, err := storage.NewLocalFSUploader(cfg.ReportStoragePath, signingKey, cfg.ReportPublicBaseURL, logger)
	if err != nil {
		panic(fmt.Sprintf("FIX-248 selectReportStorage: %v", err))
	}
	logger.Info().Str("path", cfg.ReportStoragePath).Msg("report storage backend: LocalFS")
	return uploader
}

// emptyReportProvider is a stub DataProvider that returns empty result sets for
// every report type. It exists so the scheduled-report processor can build a
// (mostly empty) artifact end-to-end. A real provider that wires
// compliance/SLA/usage/cost/audit/SIM-inventory data will replace this in a
// follow-up; documented in decisions.md (DEV-193).
type emptyReportProvider struct{}

func (emptyReportProvider) KVKK(_ context.Context, tenantID uuid.UUID, _ map[string]any) (*report.KVKKData, error) {
	return &report.KVKKData{TenantID: tenantID, GeneratedAt: time.Now().UTC()}, nil
}
func (emptyReportProvider) GDPR(_ context.Context, tenantID uuid.UUID, _ map[string]any) (*report.GDPRData, error) {
	return &report.GDPRData{TenantID: tenantID, GeneratedAt: time.Now().UTC()}, nil
}
func (emptyReportProvider) BTK(_ context.Context, tenantID uuid.UUID, _ map[string]any) (*report.BTKData, error) {
	return &report.BTKData{TenantID: tenantID, GeneratedAt: time.Now().UTC()}, nil
}
func (emptyReportProvider) SLAMonthly(_ context.Context, _ uuid.UUID, _ map[string]any) (*report.SLAData, error) {
	return &report.SLAData{Columns: []string{"period", "uptime_pct"}, Summary: map[string]string{}}, nil
}
func (emptyReportProvider) UsageSummary(_ context.Context, _ uuid.UUID, _ map[string]any) (*report.UsageData, error) {
	return &report.UsageData{Columns: []string{"period", "bytes"}, Summary: map[string]string{}}, nil
}
func (emptyReportProvider) CostAnalysis(_ context.Context, _ uuid.UUID, _ map[string]any) (*report.CostData, error) {
	return &report.CostData{Columns: []string{"operator", "cost"}, Summary: map[string]string{}}, nil
}
func (emptyReportProvider) AuditExport(_ context.Context, _ uuid.UUID, _ map[string]any) (*report.AuditExportData, error) {
	return &report.AuditExportData{Columns: []string{"timestamp", "action", "actor"}, Summary: map[string]string{}}, nil
}
func (emptyReportProvider) SIMInventory(_ context.Context, _ uuid.UUID, _ map[string]any) (*report.SIMInventoryData, error) {
	return &report.SIMInventoryData{Columns: []string{"iccid", "state"}, Summary: map[string]string{}}, nil
}

type auditRecorderAdapter struct {
	svc *audit.FullService
}

func (a *auditRecorderAdapter) Record(ctx context.Context, tenantID uuid.UUID, action, entityType, entityID string, before, after any) error {
	var beforeJSON, afterJSON json.RawMessage
	if before != nil {
		b, err := json.Marshal(before)
		if err != nil {
			return err
		}
		beforeJSON = b
	}
	if after != nil {
		b, err := json.Marshal(after)
		if err != nil {
			return err
		}
		afterJSON = b
	}
	_, err := a.svc.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:   tenantID,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		BeforeData: beforeJSON,
		AfterData:  afterJSON,
	})
	return err
}
