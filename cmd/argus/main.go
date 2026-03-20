package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	apikeyapi "github.com/btopcu/argus/internal/api/apikey"
	apnapi "github.com/btopcu/argus/internal/api/apn"
	auditapi "github.com/btopcu/argus/internal/api/audit"
	authapi "github.com/btopcu/argus/internal/api/auth"
	ippoolapi "github.com/btopcu/argus/internal/api/ippool"
	operatorapi "github.com/btopcu/argus/internal/api/operator"
	tenantapi "github.com/btopcu/argus/internal/api/tenant"
	userapi "github.com/btopcu/argus/internal/api/user"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/cache"
	"github.com/btopcu/argus/internal/config"
	"github.com/btopcu/argus/internal/gateway"
	"github.com/btopcu/argus/internal/operator"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
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

	log.Info().Str("env", cfg.AppEnv).Int("port", cfg.AppPort).Msg("starting argus")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pg, err := store.NewPostgres(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns, cfg.DatabaseMaxIdleConns, cfg.DatabaseConnMaxLife)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect postgres")
	}
	defer pg.Close()
	log.Info().Msg("postgres connected")

	rdb, err := cache.NewRedis(ctx, cfg.RedisURL, cfg.RedisMaxConns, cfg.RedisReadTimeout, cfg.RedisWriteTimeout)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect redis")
	}
	defer rdb.Close()
	log.Info().Msg("redis connected")

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

	authSvc := auth.NewService(
		&userStoreAdapter{userStore},
		&sessionStoreAdapter{sessionStore},
		nil,
		auth.Config{
			JWTSecret:        cfg.JWTSecret,
			JWTExpiry:        cfg.JWTExpiry,
			JWTRefreshExpiry: cfg.JWTRefreshExpiry,
			JWTIssuer:        cfg.JWTIssuer,
			BcryptCost:       cfg.BcryptCost,
			MaxLoginAttempts: cfg.LoginMaxAttempts,
			LockoutDuration:  cfg.LoginLockoutDur,
		},
	)

	authHandler := authapi.NewAuthHandler(authSvc, cfg.JWTRefreshExpiry, !cfg.IsDev())

	tenantStore := store.NewTenantStore(pg.Pool)
	auditStore := store.NewAuditStore(pg.Pool)
	eventBus := bus.NewEventBus(ns)
	auditSvc := audit.NewFullService(auditStore, eventBus, log.Logger)

	if err := auditSvc.Start(ctx, &eventBusSubscriber{eventBus}); err != nil {
		log.Fatal().Err(err).Msg("failed to start audit consumer")
	}

	tenantHandler := tenantapi.NewHandler(tenantStore, auditSvc, log.Logger)
	userHandler := userapi.NewHandler(userStore, tenantStore, auditSvc, log.Logger)
	auditHandler := auditapi.NewHandler(auditStore, auditSvc, log.Logger)

	apiKeyStore := store.NewAPIKeyStore(pg.Pool)
	apiKeyHandler := apikeyapi.NewHandler(apiKeyStore, tenantStore, auditSvc, cfg.DefaultMaxAPIKeys, log.Logger)

	operatorStore := store.NewOperatorStore(pg.Pool)
	apnStore := store.NewAPNStore(pg.Pool)
	ippoolStore := store.NewIPPoolStore(pg.Pool)
	adapterRegistry := adapter.NewRegistry()
	operatorHandler := operatorapi.NewHandler(operatorStore, tenantStore, auditSvc, cfg.EncryptionKey, adapterRegistry, log.Logger)
	apnHandler := apnapi.NewHandler(apnStore, operatorStore, auditSvc, log.Logger)
	ippoolHandler := ippoolapi.NewHandler(ippoolStore, apnStore, auditSvc, log.Logger)
	healthChecker := operator.NewHealthChecker(operatorStore, adapterRegistry, rdb.Client, cfg.EncryptionKey, log.Logger)

	startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := healthChecker.Start(startCtx); err != nil {
		log.Warn().Err(err).Msg("failed to start health checker — continuing without health checks")
	}
	startCancel()

	health := gateway.NewHealthHandler(pg, rdb, ns)
	router := gateway.NewRouterWithDeps(gateway.RouterDeps{
		Health:             health,
		AuthHandler:        authHandler,
		TenantHandler:      tenantHandler,
		UserHandler:        userHandler,
		AuditHandler:       auditHandler,
		APIKeyHandler:      apiKeyHandler,
		OperatorHandler:    operatorHandler,
		APNHandler:         apnHandler,
		IPPoolHandler:      ippoolHandler,
		APIKeyStore:        apiKeyStore,
		RedisClient:        rdb.Client,
		RateLimitPerMinute: cfg.RateLimitPerMinute,
		RateLimitPerHour:   cfg.RateLimitPerHour,
		JWTSecret:          cfg.JWTSecret,
		Logger:             log.Logger,
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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	log.Info().Msg("shutting down http server")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("http server shutdown error")
	}

	log.Info().Msg("stopping health checker")
	healthChecker.Stop()

	log.Info().Msg("stopping audit consumer")
	auditSvc.Stop()

	log.Info().Msg("closing nats connection")
	ns.Close()

	log.Info().Msg("closing redis connection")
	if err := rdb.Close(); err != nil {
		log.Error().Err(err).Msg("redis close error")
	}

	log.Info().Msg("closing database connection")
	pg.Close()

	log.Info().Msg("argus stopped gracefully")
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

func storeUserToAuth(u *store.User) *auth.User {
	return &auth.User{
		ID:               u.ID,
		TenantID:         u.TenantID,
		Email:            u.Email,
		PasswordHash:     u.PasswordHash,
		Name:             u.Name,
		Role:             u.Role,
		TOTPSecret:       u.TOTPSecret,
		TOTPEnabled:      u.TOTPEnabled,
		State:            u.State,
		LastLoginAt:      u.LastLoginAt,
		FailedLoginCount: u.FailedLoginCount,
		LockedUntil:      u.LockedUntil,
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

func storeSessionToAuth(s *store.UserSession) *auth.UserSession {
	return &auth.UserSession{
		ID:               s.ID,
		UserID:           s.UserID,
		RefreshTokenHash: s.RefreshTokenHash,
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
