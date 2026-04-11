package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	aaadiameter "github.com/btopcu/argus/internal/aaa/diameter"
	aaaradius "github.com/btopcu/argus/internal/aaa/radius"
	aaasba "github.com/btopcu/argus/internal/aaa/sba"
	aaasession "github.com/btopcu/argus/internal/aaa/session"
	anomalysvc "github.com/btopcu/argus/internal/analytics/anomaly"
	cdrsvc "github.com/btopcu/argus/internal/analytics/cdr"
	costsvc "github.com/btopcu/argus/internal/analytics/cost"
	analyticmetrics "github.com/btopcu/argus/internal/analytics/metrics"
	apikeyapi "github.com/btopcu/argus/internal/api/apikey"
	apnapi "github.com/btopcu/argus/internal/api/apn"
	auditapi "github.com/btopcu/argus/internal/api/audit"
	authapi "github.com/btopcu/argus/internal/api/auth"
	analyticsapi "github.com/btopcu/argus/internal/api/analytics"
	anomalyapi "github.com/btopcu/argus/internal/api/anomaly"
	cdrapi "github.com/btopcu/argus/internal/api/cdr"
	dashboardapi "github.com/btopcu/argus/internal/api/dashboard"
	complianceapi "github.com/btopcu/argus/internal/api/compliance"
	violationapi "github.com/btopcu/argus/internal/api/violation"
	diagapi "github.com/btopcu/argus/internal/api/diagnostics"
	esimapi "github.com/btopcu/argus/internal/api/esim"
	metricsapi "github.com/btopcu/argus/internal/api/metrics"
	notifapi "github.com/btopcu/argus/internal/api/notification"
	ippoolapi "github.com/btopcu/argus/internal/api/ippool"
	jobapi "github.com/btopcu/argus/internal/api/job"
	msisdnapi "github.com/btopcu/argus/internal/api/msisdn"
	operatorapi "github.com/btopcu/argus/internal/api/operator"
	otaapi "github.com/btopcu/argus/internal/api/ota"
	policyapi "github.com/btopcu/argus/internal/api/policy"
	"github.com/btopcu/argus/internal/policy/dryrun"
	policyenforcer "github.com/btopcu/argus/internal/policy/enforcer"
	"github.com/btopcu/argus/internal/policy/rollout"
	segmentapi "github.com/btopcu/argus/internal/api/segment"
	sessionapi "github.com/btopcu/argus/internal/api/session"
	simapi "github.com/btopcu/argus/internal/api/sim"
	tenantapi "github.com/btopcu/argus/internal/api/tenant"
	userapi "github.com/btopcu/argus/internal/api/user"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/compliance"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/cache"
	diagnosticspkg "github.com/btopcu/argus/internal/diagnostics"
	esimpkg "github.com/btopcu/argus/internal/esim"
	"github.com/btopcu/argus/internal/config"
	"github.com/btopcu/argus/internal/gateway"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/ota"
	"github.com/btopcu/argus/internal/operator"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/btopcu/argus/internal/ws"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
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

	if cfg.GOGC != 100 {
		debug.SetGCPercent(cfg.GOGC)
		log.Info().Int("gogc", cfg.GOGC).Msg("GOGC tuned")
	}

	if cfg.PprofEnabled || cfg.IsDev() {
		go func() {
			pprofAddr := cfg.PprofAddr
			log.Info().Str("addr", pprofAddr).Msg("pprof server starting (endpoints: /debug/pprof/)")
			if err := http.ListenAndServe(pprofAddr, nil); err != nil {
				log.Error().Err(err).Msg("pprof server error")
			}
		}()
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
			JWTSecret:           cfg.JWTSecret,
			JWTExpiry:           cfg.JWTExpiry,
			JWTRefreshExpiry:    cfg.JWTRefreshExpiry,
			JWTRememberMeExpiry: cfg.JWTRememberMeExpiry,
			JWTIssuer:           cfg.JWTIssuer,
			BcryptCost:          cfg.BcryptCost,
			MaxLoginAttempts:    cfg.LoginMaxAttempts,
			LockoutDuration:     cfg.LoginLockoutDur,
			EncryptionKey:       cfg.EncryptionKey,
		},
	)

	if migrated, err := userStore.MigrateTOTPSecretsToEncrypted(ctx, cfg.EncryptionKey); err != nil {
		log.Warn().Err(err).Msg("totp secret encryption migration failed — continuing")
	} else if migrated > 0 {
		log.Info().Int("migrated", migrated).Msg("encrypted plaintext totp secrets at rest")
	}

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
	simStore := store.NewSIMStore(pg.Pool)
	operatorMetricsSessionStore := store.NewRadiusSessionStore(pg.Pool)
	operatorHandler := operatorapi.NewHandler(operatorStore, tenantStore, auditSvc, cfg.EncryptionKey, adapterRegistry, log.Logger,
		operatorapi.WithSIMStore(simStore),
		operatorapi.WithSessionStore(operatorMetricsSessionStore),
	)
	apnHandler := apnapi.NewHandler(apnStore, operatorStore, auditSvc, log.Logger, apnapi.WithSIMStore(simStore))
	ippoolHandler := ippoolapi.NewHandler(ippoolStore, apnStore, auditSvc, log.Logger)
	esimStore := store.NewESimProfileStore(pg.Pool)
	smdpAdapter := esimpkg.NewMockSMDPAdapter(log.Logger)
	esimHandler := esimapi.NewHandler(esimStore, simStore, smdpAdapter, auditSvc, log.Logger)
	segmentStore := store.NewSegmentStore(pg.Pool)
	segmentHandler := segmentapi.NewHandler(segmentStore, log.Logger)
	msisdnStore := store.NewMSISDNStore(pg.Pool)
	msisdnHandler := msisdnapi.NewHandler(msisdnStore, log.Logger)

	jobStore := store.NewJobStore(pg.Pool)

	policyStore := store.NewPolicyStore(pg.Pool)
	nameCache := cache.NewNameCache(rdb.Client)
	simSessionStore := store.NewRadiusSessionStore(pg.Pool)
	cdrStore := store.NewCDRStore(pg.Pool)
	simHandler := simapi.NewHandler(simStore, apnStore, operatorStore, ippoolStore, tenantStore, auditSvc, log.Logger, simapi.WithPolicyStore(policyStore), simapi.WithNameCache(nameCache), simapi.WithSessionStore(simSessionStore), simapi.WithCDRStore(cdrStore))
	dryRunSvc := dryrun.NewService(policyStore, simStore, pg.Pool, rdb.Client, log.Logger)
	rolloutSvc := rollout.NewService(policyStore, simStore, nil, nil, eventBus, jobStore, log.Logger)
	policyHandler := policyapi.NewHandler(policyStore, dryRunSvc, rolloutSvc, jobStore, eventBus, auditSvc, log.Logger)
	bulkHandler := simapi.NewBulkHandler(jobStore, segmentStore, eventBus, log.Logger)
	jobHandler := jobapi.NewHandler(jobStore, eventBus, log.Logger)

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
	cdrConsumer := cdrsvc.NewConsumer(cdrStore, operatorStore, log.Logger)
	if err := cdrConsumer.Start(&eventBusCDRSubscriber{eventBus}); err != nil {
		log.Fatal().Err(err).Msg("failed to start cdr consumer")
	}
	cdrHandler := cdrapi.NewHandler(cdrStore, jobStore, eventBus, log.Logger)

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
	anomalyHandler := anomalyapi.NewHandler(anomalyStore, log.Logger)

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

	distLock := job.NewDistributedLock(rdb.Client, log.Logger)
	importProcessor := job.NewBulkImportProcessor(jobStore, simStore, operatorStore, apnStore, ippoolStore, eventBus, log.Logger)
	dryRunProcessor := job.NewDryRunProcessor(dryRunSvc, jobStore, eventBus, log.Logger)
	rolloutStageProc := job.NewRolloutStageProcessor(rolloutSvc, policyStore, jobStore, eventBus, log.Logger)
	jobRunner := job.NewRunner(jobStore, eventBus, distLock, job.RunnerConfig{
		MaxConcurrentPerTenant: cfg.JobMaxConcurrentPerTenant,
		LockRenewInterval:     cfg.JobLockRenewInterval,
	}, log.Logger)
	jobRunner.Register(importProcessor)
	jobRunner.Register(dryRunProcessor)
	jobRunner.Register(rolloutStageProc)

	complianceStore := store.NewComplianceStore(pg.Pool)
	complianceSvc := compliance.NewService(complianceStore, auditStore, auditSvc, log.Logger)
	purgeSweepProc := job.NewPurgeSweepProcessor(jobStore, complianceSvc, eventBus, log.Logger)
	ipReclaimStub := job.NewStubProcessor(job.JobTypeIPReclaim, jobStore, eventBus, log.Logger)
	slaReportStub := job.NewStubProcessor(job.JobTypeSLAReport, jobStore, eventBus, log.Logger)
	bulkStateChangeProc := job.NewBulkStateChangeProcessor(jobStore, simStore, segmentStore, distLock, eventBus, log.Logger)
	bulkPolicyAssignProc := job.NewBulkPolicyAssignProcessor(jobStore, simStore, segmentStore, distLock, eventBus, log.Logger)
	otaProcessor := job.NewOTAProcessor(jobStore, otaStore, simStore, otaRateLimiter, eventBus, log.Logger)
	bulkEsimSwitchProc := job.NewBulkEsimSwitchProcessor(jobStore, simStore, segmentStore, esimStore, distLock, eventBus, log.Logger)
	jobRunner.Register(purgeSweepProc)
	jobRunner.Register(ipReclaimStub)
	jobRunner.Register(slaReportStub)
	jobRunner.Register(bulkStateChangeProc)
	jobRunner.Register(bulkPolicyAssignProc)
	jobRunner.Register(otaProcessor)
	jobRunner.Register(bulkEsimSwitchProc)

	cdrExportProc := job.NewCDRExportProcessor(jobStore, cdrStore, eventBus, log.Logger)
	jobRunner.Register(cdrExportProc)

	anomalyBatchProc := job.NewAnomalyBatchProcessor(batchDetector, jobStore, eventBus, log.Logger)
	jobRunner.Register(anomalyBatchProc)

	storageMonitorStore := store.NewStorageMonitorStore(pg.Pool)
	dataLifecycleStore := store.NewDataLifecycleStore(pg.Pool)

	storageMonitorProc := job.NewStorageMonitorProcessor(jobStore, storageMonitorStore, eventBus, cfg.StorageAlertPct, log.Logger)
	jobRunner.Register(storageMonitorProc)

	dataRetentionProc := job.NewDataRetentionProcessor(jobStore, dataLifecycleStore, storageMonitorStore, eventBus, cfg.DefaultCDRRetentionDays, log.Logger)
	jobRunner.Register(dataRetentionProc)

	var s3Uploader job.S3Uploader
	s3ArchivalProc := job.NewS3ArchivalProcessor(jobStore, dataLifecycleStore, storageMonitorStore, cdrStore, s3Uploader, eventBus, cfg.S3Bucket, log.Logger)
	jobRunner.Register(s3ArchivalProc)

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
		cronScheduler.Start()
	}

	healthChecker := operator.NewHealthChecker(operatorStore, adapterRegistry, rdb.Client, cfg.EncryptionKey, log.Logger)
	healthChecker.SetEventPublisher(eventBus, bus.SubjectOperatorHealthChanged, bus.SubjectAlertTriggered)

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
	notifSvc := notification.NewService(emailSender, telegramSender, nil, notifChannels, log.Logger)
	notifSvc.SetNotifStore(&notifStoreAdapter{notifStore})
	notifSvc.SetEventPublisher(eventBus, bus.SubjectNotification)

	notifRedisRL := notification.NewRedisRateLimiter(rdb.Client, cfg.NotificationRateLimitPerMin)
	notifDelivery := notification.NewDeliveryTracker(notifRedisRL, log.Logger)
	notifSvc.SetDeliveryTracker(notifDelivery)

	if err := notifSvc.Start(&eventBusNotifSubscriber{eventBus}, bus.SubjectOperatorHealthChanged, bus.SubjectAlertTriggered); err != nil {
		log.Warn().Err(err).Msg("failed to start notification service")
	}

	wsHub := ws.NewHub(log.Logger)
	if err := wsHub.SubscribeToNATS(&eventBusWSSubscriber{eventBus}, []string{
		bus.SubjectSessionStarted,
		bus.SubjectSessionEnded,
		bus.SubjectSIMUpdated,
		bus.SubjectOperatorHealthChanged,
		bus.SubjectAlertTriggered,
		bus.SubjectPolicyRolloutProgress,
		bus.SubjectJobProgress,
		bus.SubjectJobCompleted,
		bus.SubjectNotification,
	}); err != nil {
		log.Warn().Err(err).Msg("failed to subscribe ws hub to NATS")
	}

	wsServer := ws.NewServer(wsHub, ws.ServerConfig{
		Addr:              fmt.Sprintf(":%d", cfg.WSPort),
		JWTSecret:         cfg.JWTSecret,
		MaxConnsPerTenant: cfg.WSMaxConnsPerTenant,
	}, log.Logger)
	if err := wsServer.Start(); err != nil {
		log.Fatal().Err(err).Msg("failed to start ws server")
	}

	var radiusServer *aaaradius.Server
	var sessionHandler *sessionapi.Handler
	var sessionSweeper *aaasession.TimeoutSweeper
	if cfg.RadiusSecret != "" {
		radiusSessionStore := store.NewRadiusSessionStore(pg.Pool)
		simCache := aaaradius.NewSIMCache(rdb.Client, simStore, log.Logger)
		sessionMgr := aaasession.NewManager(radiusSessionStore, rdb.Client, log.Logger, aaasession.WithSIMStore(simStore))
		coaSender := aaasession.NewCoASender(cfg.RadiusSecret, cfg.RadiusCoAPort, log.Logger)
		dmSender := aaasession.NewDMSender(cfg.RadiusSecret, cfg.RadiusCoAPort, log.Logger)

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
		)

		sessionSweeper = aaasession.NewTimeoutSweeper(sessionMgr, dmSender, eventBus, rdb.Client, log.Logger)
		sessionSweeper.Start()

		disconnectProcessor := job.NewBulkDisconnectProcessor(jobStore, sessionMgr, dmSender, eventBus, log.Logger)
		jobRunner.Register(disconnectProcessor)

		rolloutSvc.SetSessionProvider(&rolloutSessionAdapter{mgr: sessionMgr})
		rolloutSvc.SetCoADispatcher(&rolloutCoAAdapter{sender: coaSender})

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
		diamSessionMgr := aaasession.NewManager(radiusSessionStore, rdb.Client, log.Logger, aaasession.WithSIMStore(simStore))

		diameterServer = aaadiameter.NewServer(aaadiameter.ServerConfig{
			Port:        cfg.DiameterPort,
			OriginHost:  cfg.DiameterOriginHost,
			OriginRealm: cfg.DiameterOriginRealm,
		}, aaadiameter.ServerDeps{
			SessionMgr:  diamSessionMgr,
			EventBus:    eventBus,
			SIMResolver: diamSimResolver,
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
		sbaSessionMgr := aaasession.NewManager(sbaRadiusSessionStore, rdb.Client, log.Logger, aaasession.WithSIMStore(simStore))

		sbaServer = aaasba.NewServer(aaasba.ServerConfig{
			Port:        cfg.SBAPort,
			TLSCertPath: cfg.TLSCertPath,
			TLSKeyPath:  cfg.TLSKeyPath,
			EnableMTLS:  cfg.SBAEnableMTLS,
		}, aaasba.ServerDeps{
			SessionMgr: sbaSessionMgr,
			EventBus:   eventBus,
			Logger:     log.Logger,
		})

		if err := sbaServer.Start(); err != nil {
			log.Fatal().Err(err).Msg("failed to start SBA server")
		}

		if err := sbaServer.NRFRegistration().Register(); err != nil {
			log.Warn().Err(err).Msg("NRF registration failed (placeholder)")
		}
	}

	metricsCollector := analyticmetrics.NewCollector(rdb.Client, log.Logger)

	radiusSessionStore2 := store.NewRadiusSessionStore(pg.Pool)
	metricsCollector.SetSessionCounter(radiusSessionStore2)

	violationStore := store.NewPolicyViolationStore(pg.Pool, log.Logger)

	policyEnforcer := policyenforcer.New(
		nil,
		policyStore,
		violationStore,
		eventBus,
		rdb.Client,
		log.Logger,
	)

	if radiusServer != nil {
		radiusServer.SetMetricsRecorder(metricsCollector)
		radiusServer.SetPolicyEnforcer(policyEnforcer)
	}

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

	metricsHandler := metricsapi.NewHandler(metricsCollector, log.Logger)

	notifHandler := notifapi.NewHandler(notifStore, notifConfigStore, log.Logger)
	complianceHandler := complianceapi.NewHandler(complianceSvc, tenantStore, log.Logger)
	violationHandler := violationapi.NewHandler(violationStore, log.Logger)

	dashboardSessionStore := store.NewRadiusSessionStore(pg.Pool)
	dashboardHandler := dashboardapi.NewHandler(simStore, dashboardSessionStore, operatorStore, anomalyStore, apnStore, log.Logger, dashboardapi.WithRedisClient(rdb.Client), dashboardapi.WithCDRStore(cdrStore))

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
		SIMHandler:         simHandler,
		ESimHandler:        esimHandler,
		SegmentHandler:     segmentHandler,
		BulkHandler:        bulkHandler,
		JobHandler:         jobHandler,
		MSISDNHandler:      msisdnHandler,
		SessionHandler:     sessionHandler,
		PolicyHandler:      policyHandler,
		OTAHandler:         otaHandler,
		DiagnosticsHandler: diagHandler,
		CDRHandler:         cdrHandler,
		AnalyticsHandler:   analyticsHandler,
		AnomalyHandler:      anomalyHandler,
		NotificationHandler: notifHandler,
		MetricsHandler:      metricsHandler,
		ComplianceHandler:   complianceHandler,
		ViolationHandler:    violationHandler,
		DashboardHandler:    dashboardHandler,
		APIKeyStore:        apiKeyStore,
		RedisClient:        rdb.Client,
		RateLimitPerMinute: cfg.RateLimitPerMinute,
		RateLimitPerHour:   cfg.RateLimitPerHour,
		JWTSecret:          cfg.JWTSecret,
		Logger:             log.Logger,
		SecurityHeadersCfg:   &secHeadersCfg,
		CORSConfig:           &corsCfg,
		BruteForceCfg:        &bfCfg,
		EnableInputSanitizer: true,
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

	if radiusServer != nil {
		log.Info().Msg("stopping RADIUS server")
		if err := radiusServer.Stop(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("RADIUS server shutdown error")
		}
	}

	if diameterServer != nil {
		log.Info().Msg("stopping Diameter server")
		diameterServer.Stop()
	}

	if sbaServer != nil {
		log.Info().Msg("stopping SBA server")
		_ = sbaServer.NRFRegistration().Deregister()
		sbaServer.Stop()
	}

	if sessionSweeper != nil {
		log.Info().Msg("stopping session sweeper")
		sessionSweeper.Stop()
	}

	if cronScheduler != nil {
		log.Info().Msg("stopping cron scheduler")
		cronScheduler.Stop()
	}

	log.Info().Msg("stopping timeout detector")
	timeoutDetector.Stop()

	log.Info().Msg("stopping job runner")
	jobRunner.Stop()

	log.Info().Msg("stopping metrics pusher")
	metricsPusher.Stop()

	log.Info().Msg("stopping ws server")
	if err := wsServer.Stop(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("ws server shutdown error")
	}

	log.Info().Msg("stopping ws hub")
	wsHub.Stop()

	log.Info().Msg("stopping notification service")
	notifSvc.Stop()

	log.Info().Msg("stopping health checker")
	healthChecker.Stop()

	log.Info().Msg("stopping anomaly engine")
	anomalyEngine.Stop()

	log.Info().Msg("stopping cdr consumer")
	cdrConsumer.Stop()

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
	result, err := a.sender.SendCoA(ctx, aaasession.CoARequest{
		NASIP:         req.NASIP,
		AcctSessionID: req.AcctSessionID,
		IMSI:          req.IMSI,
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
