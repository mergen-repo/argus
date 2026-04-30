package job

// KVKKPurgeProcessor implements the daily KVKK (Turkish Personal Data Protection Law)
// pseudonymization sweep. It locates PII in cdrs, sessions, user_sessions, and
// audit_logs, then either reports counts (DryRun=true) or executes the mutations.
//
// PRODUCTION SAFETY: The first production run MUST be executed with DryRun=true to
// validate row counts before any writes occur. The cron registration in main.go
// (Task 26) controls this; this processor does NOT default DryRun=true so that
// dry-run vs. live is an explicit caller decision.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const (
	kvkkDefaultRetentionDays = 365

	kvkkTableSessions     = "sessions"
	kvkkTableUserSessions = "user_sessions"
	kvkkTableAuditLogs    = "audit_logs"
)

// KVKKPurgeJobPayload is the JSON payload for a kvkk_purge_daily job.
type KVKKPurgeJobPayload struct {
	// DryRun=true substitutes SELECT COUNT(*) for all UPDATE statements.
	// No rows are mutated when DryRun=true.
	DryRun bool `json:"dry_run,omitempty"`
	// TenantID is optional. When nil, all tenants are swept.
	TenantID *string `json:"tenant_id,omitempty"`
}

// KVKKPurgeResult is written to jobs.result after the processor completes.
type KVKKPurgeResult struct {
	DryRun    bool                  `json:"dry_run"`
	PerTenant []kvkkPerTenantResult `json:"per_tenant"`
}

type kvkkPerTenantResult struct {
	TenantID      string         `json:"tenant_id"`
	Purged        map[string]int `json:"purged"`
	RetentionDays map[string]int `json:"retention_days"`
	Errors        []string       `json:"errors,omitempty"`
}

// KVKKPurgeProcessor runs the KVKK daily purge sweep.
type KVKKPurgeProcessor struct {
	db        *pgxpool.Pool
	lifecycle *store.DataLifecycleStore
	tenants   *store.TenantStore
	auditSt   *store.AuditStore
	jobs      *store.JobStore
	eventBus  *bus.EventBus
	reg       *metrics.Registry
	logger    zerolog.Logger
}

func NewKVKKPurgeProcessor(
	db *pgxpool.Pool,
	lifecycle *store.DataLifecycleStore,
	tenants *store.TenantStore,
	auditSt *store.AuditStore,
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	reg *metrics.Registry,
	logger zerolog.Logger,
) *KVKKPurgeProcessor {
	return &KVKKPurgeProcessor{
		db:        db,
		lifecycle: lifecycle,
		tenants:   tenants,
		auditSt:   auditSt,
		jobs:      jobs,
		eventBus:  eventBus,
		reg:       reg,
		logger:    logger.With().Str("processor", JobTypeKVKKPurgeDaily).Logger(),
	}
}

func (p *KVKKPurgeProcessor) Type() string {
	return JobTypeKVKKPurgeDaily
}

func (p *KVKKPurgeProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload KVKKPurgeJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("kvkk_purge: unmarshal payload: %w", err)
	}

	p.logger.Info().
		Str("job_id", job.ID.String()).
		Bool("dry_run", payload.DryRun).
		Msg("starting KVKK purge sweep")

	tenantList, err := p.resolveTenants(ctx, payload.TenantID)
	if err != nil {
		return fmt.Errorf("kvkk_purge: resolve tenants: %w", err)
	}

	result := KVKKPurgeResult{DryRun: payload.DryRun}

	for _, tenant := range tenantList {
		tenantResult := p.processTenant(ctx, tenant, payload.DryRun)
		result.PerTenant = append(result.PerTenant, tenantResult)

		for tbl, count := range tenantResult.Purged {
			p.reg.IncKVKKPurgeRows(tbl, payload.DryRun, count)
		}

		if err := p.writeAuditLog(ctx, tenant, tenantResult, payload.DryRun); err != nil {
			p.logger.Warn().Err(err).
				Str("tenant_id", tenant.ID.String()).
				Msg("kvkk_purge: audit log write failed")
		}
	}

	resultJSON, _ := json.Marshal(result)
	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("kvkk_purge: complete job: %w", err)
	}

	if p.eventBus != nil && !payload.DryRun {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":    job.ID.String(),
			"tenant_id": job.TenantID.String(),
			"type":      JobTypeKVKKPurgeDaily,
			"state":     "completed",
			"dry_run":   payload.DryRun,
			"event":     "kvkk_purge_completed",
		})
	}

	p.logger.Info().
		Bool("dry_run", payload.DryRun).
		Int("tenants", len(result.PerTenant)).
		Msg("KVKK purge sweep completed")

	return nil
}

// resolveTenants returns the list of tenants to process.
// When tenantIDStr is set, only that tenant is returned.
// Otherwise, all active tenants are fetched via cursor pagination.
func (p *KVKKPurgeProcessor) resolveTenants(ctx context.Context, tenantIDStr *string) ([]store.Tenant, error) {
	if tenantIDStr != nil && *tenantIDStr != "" {
		id, err := uuid.Parse(*tenantIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid tenant_id: %w", err)
		}
		t, err := p.tenants.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		return []store.Tenant{*t}, nil
	}

	var all []store.Tenant
	cursor := ""
	for {
		batch, next, err := p.tenants.List(ctx, cursor, 100, "active")
		if err != nil {
			return nil, fmt.Errorf("list tenants: %w", err)
		}
		all = append(all, batch...)
		if next == "" {
			break
		}
		cursor = next
	}
	return all, nil
}

// retentionForTable returns the retention days to apply for a given table, using
// the per-table config, the tenant scalar fallback, then the default 365 days.
//
// tableKey is one of "cdr", "session", "audit".
func retentionForTable(cfg *store.TenantRetentionConfig, tableKey string, tenantFallback int) int {
	if cfg != nil {
		switch tableKey {
		case "cdr":
			if cfg.CDRRetentionDays > 0 {
				return cfg.CDRRetentionDays
			}
		case "session":
			if cfg.SessionRetentionDays > 0 {
				return cfg.SessionRetentionDays
			}
		case "audit":
			if cfg.AuditRetentionDays > 0 {
				return cfg.AuditRetentionDays
			}
		}
	}
	if tenantFallback > 0 {
		return tenantFallback
	}
	return kvkkDefaultRetentionDays
}

// processTenant executes (or simulates) the KVKK purge for a single tenant.
func (p *KVKKPurgeProcessor) processTenant(ctx context.Context, tenant store.Tenant, dryRun bool) kvkkPerTenantResult {
	res := kvkkPerTenantResult{
		TenantID:      tenant.ID.String(),
		Purged:        make(map[string]int),
		RetentionDays: make(map[string]int),
	}

	cfg, err := p.lifecycle.GetRetentionConfig(ctx, tenant.ID)
	if err != nil && !errors.Is(err, store.ErrRetentionConfigNotFound) {
		res.Errors = append(res.Errors, fmt.Sprintf("get retention config: %v", err))
	}
	if errors.Is(err, store.ErrRetentionConfigNotFound) {
		cfg = nil
	}

	sessionDays := retentionForTable(cfg, "session", tenant.PurgeRetentionDays)
	auditDays := retentionForTable(cfg, "audit", tenant.PurgeRetentionDays)

	res.RetentionDays[kvkkTableSessions] = sessionDays
	res.RetentionDays[kvkkTableUserSessions] = sessionDays
	res.RetentionDays[kvkkTableAuditLogs] = auditDays

	// sessions: pseudonymize calling_station_id (MSISDN in RADIUS context)
	// Guard: length < 64 prevents double-hashing already-pseudonymized values.
	n, err := p.sweepSessions(ctx, tenant.ID, sessionDays, dryRun)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("sessions: %v", err))
	} else {
		res.Purged[kvkkTableSessions] = n
	}

	// user_sessions: set ip_address=NULL, user_agent='redacted'
	n, err = p.sweepUserSessions(ctx, tenant.ID, sessionDays, dryRun)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("user_sessions: %v", err))
	} else {
		res.Purged[kvkkTableUserSessions] = n
	}

	// audit_logs: NULL ip_address, 'redacted' user_agent, scrub PII from JSONB columns
	n, err = p.sweepAuditLogs(ctx, tenant.ID, auditDays, dryRun)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("audit_logs: %v", err))
	} else {
		res.Purged[kvkkTableAuditLogs] = n
	}

	return res
}

// sweepSessions pseudonymizes calling_station_id in sessions older than retentionDays.
// The calling_station_id column holds the RADIUS Calling-Station-Id (typically MSISDN).
// Guard: length(calling_station_id) < 64 prevents double-hashing.
func (p *KVKKPurgeProcessor) sweepSessions(ctx context.Context, tenantID uuid.UUID, retentionDays int, dryRun bool) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	if dryRun {
		var count int
		err := p.db.QueryRow(ctx, `
			SELECT COUNT(*) FROM sessions
			WHERE tenant_id = $1
			  AND started_at < $2
			  AND calling_station_id IS NOT NULL
			  AND length(calling_station_id) < 64
		`, tenantID, cutoff).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("count sessions: %w", err)
		}
		return count, nil
	}

	tag, err := p.db.Exec(ctx, `
		UPDATE sessions
		SET calling_station_id = encode(sha256(calling_station_id::bytea), 'hex')
		WHERE tenant_id = $1
		  AND started_at < $2
		  AND calling_station_id IS NOT NULL
		  AND length(calling_station_id) < 64
	`, tenantID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("update sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// sweepUserSessions pseudonymizes user_sessions older than retentionDays
// by nulling ip_address and setting user_agent='redacted'.
// Guard: user_agent != 'redacted' prevents double-processing.
func (p *KVKKPurgeProcessor) sweepUserSessions(ctx context.Context, tenantID uuid.UUID, retentionDays int, dryRun bool) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	// user_sessions has no tenant_id; join through users.
	if dryRun {
		var count int
		err := p.db.QueryRow(ctx, `
			SELECT COUNT(*) FROM user_sessions us
			JOIN users u ON u.id = us.user_id
			WHERE u.tenant_id = $1
			  AND us.created_at < $2
			  AND (us.user_agent IS NULL OR us.user_agent != 'redacted')
		`, tenantID, cutoff).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("count user_sessions: %w", err)
		}
		return count, nil
	}

	tag, err := p.db.Exec(ctx, `
		UPDATE user_sessions us
		SET user_agent = 'redacted', ip_address = NULL
		FROM users u
		WHERE u.id = us.user_id
		  AND u.tenant_id = $1
		  AND us.created_at < $2
		  AND (us.user_agent IS NULL OR us.user_agent != 'redacted')
	`, tenantID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("update user_sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// sweepAuditLogs pseudonymizes audit_logs older than retentionDays by:
//   - Setting ip_address = NULL
//   - Setting user_agent = 'redacted'
//   - Replacing known PII keys (ip_address, user_agent, email, phone) in
//     before_data, after_data, and diff JSONB columns with "redacted".
//
// Guard: user_agent != 'redacted' prevents double-processing.
func (p *KVKKPurgeProcessor) sweepAuditLogs(ctx context.Context, tenantID uuid.UUID, retentionDays int, dryRun bool) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	if dryRun {
		var count int
		err := p.db.QueryRow(ctx, `
			SELECT COUNT(*) FROM audit_logs
			WHERE tenant_id = $1
			  AND created_at < $2
			  AND (user_agent IS NULL OR user_agent != 'redacted')
		`, tenantID, cutoff).Scan(&count)
		if err != nil {
			return 0, fmt.Errorf("count audit_logs: %w", err)
		}
		return count, nil
	}

	tag, err := p.db.Exec(ctx, `
		UPDATE audit_logs
		SET ip_address  = NULL,
		    user_agent  = 'redacted',
		    before_data = CASE WHEN before_data IS NOT NULL THEN
		                   before_data
		                   #- '{ip_address}' #- '{user_agent}' #- '{email}' #- '{phone}'
		                   || jsonb_build_object(
		                        'ip_address', CASE WHEN before_data ? 'ip_address' THEN '"redacted"'::jsonb ELSE NULL END,
		                        'user_agent', CASE WHEN before_data ? 'user_agent' THEN '"redacted"'::jsonb ELSE NULL END,
		                        'email',      CASE WHEN before_data ? 'email'      THEN '"redacted"'::jsonb ELSE NULL END,
		                        'phone',      CASE WHEN before_data ? 'phone'      THEN '"redacted"'::jsonb ELSE NULL END
		                      ) - 'null'
		                 ELSE NULL END,
		    after_data  = CASE WHEN after_data IS NOT NULL THEN
		                   after_data
		                   #- '{ip_address}' #- '{user_agent}' #- '{email}' #- '{phone}'
		                   || jsonb_build_object(
		                        'ip_address', CASE WHEN after_data ? 'ip_address' THEN '"redacted"'::jsonb ELSE NULL END,
		                        'user_agent', CASE WHEN after_data ? 'user_agent' THEN '"redacted"'::jsonb ELSE NULL END,
		                        'email',      CASE WHEN after_data ? 'email'      THEN '"redacted"'::jsonb ELSE NULL END,
		                        'phone',      CASE WHEN after_data ? 'phone'      THEN '"redacted"'::jsonb ELSE NULL END
		                      ) - 'null'
		                 ELSE NULL END,
		    diff        = CASE WHEN diff IS NOT NULL THEN
		                   diff
		                   #- '{ip_address}' #- '{user_agent}' #- '{email}' #- '{phone}'
		                   || jsonb_build_object(
		                        'ip_address', CASE WHEN diff ? 'ip_address' THEN '"redacted"'::jsonb ELSE NULL END,
		                        'user_agent', CASE WHEN diff ? 'user_agent' THEN '"redacted"'::jsonb ELSE NULL END,
		                        'email',      CASE WHEN diff ? 'email'      THEN '"redacted"'::jsonb ELSE NULL END,
		                        'phone',      CASE WHEN diff ? 'phone'      THEN '"redacted"'::jsonb ELSE NULL END
		                      ) - 'null'
		                 ELSE NULL END
		WHERE tenant_id = $1
		  AND created_at < $2
		  AND (user_agent IS NULL OR user_agent != 'redacted')
	`, tenantID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("update audit_logs: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// writeAuditLog records a tamper-proof audit entry for the purge run.
func (p *KVKKPurgeProcessor) writeAuditLog(ctx context.Context, tenant store.Tenant, res kvkkPerTenantResult, dryRun bool) error {
	details, _ := json.Marshal(map[string]interface{}{
		"dry_run": dryRun,
		"counts":  res.Purged,
		"errors":  res.Errors,
	})

	entry := &audit.Entry{
		TenantID:   tenant.ID,
		Action:     "kvkk.purge.run",
		EntityType: "tenant",
		EntityID:   tenant.ID.String(),
		AfterData:  json.RawMessage(details),
		CreatedAt:  time.Now().UTC(),
	}

	if _, err := p.auditSt.CreateWithChain(ctx, entry); err != nil {
		return fmt.Errorf("create audit entry: %w", err)
	}
	return nil
}
