package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	minAlertsRetentionDays = 30
	maxAlertsRetentionDays = 365
	// largeDeleteWarnThreshold — DEV-336 Risk R3: warn (and recommend
	// future batching) when a single tenant deletes more than this many
	// rows in one cron pass. The DB-level DELETE has no LIMIT today; for
	// tenants with millions of stale alerts this could lock the table for
	// long enough to matter.
	largeDeleteWarnThreshold int64 = 100_000
	// tenantPageSize — DEV-336 Risk R3: paginate tenants in 1000-row pages
	// so the cron job cannot OOM on installs with many tenants.
	tenantPageSize = 1000
)

// alertRetentionTenantLister is the subset of *store.TenantStore that the
// processor depends on. Defining it here lets unit tests inject fakes
// without spinning up Postgres.
type alertRetentionTenantLister interface {
	List(ctx context.Context, cursor string, limit int, stateFilter string) ([]store.Tenant, string, error)
}

// alertRetentionDeleter is the subset of *store.AlertStore that the
// processor depends on. Defining it here lets unit tests inject fakes
// (e.g. a fake that returns an error for one tenant) without a database.
type alertRetentionDeleter interface {
	DeleteOlderThanForTenant(ctx context.Context, tenantID uuid.UUID, cutoff time.Time) (int64, error)
}

type AlertsRetentionProcessor struct {
	jobs                 *store.JobStore
	alertStore           alertRetentionDeleter
	tenantStore          alertRetentionTenantLister
	eventBus             *bus.EventBus
	defaultRetentionDays int
	logger               zerolog.Logger
}

// NewAlertsRetentionProcessor wires the per-tenant alerts retention sweep.
//
// retentionDays is the FALLBACK applied when a tenant has no
// `alert_retention_days` key in its `tenants.settings` JSONB column. The
// per-tenant override is parsed by parseAlertRetentionDays at job time
// (DEV-335). The 30-day floor is enforced both here (config-time clamp)
// and in parseAlertRetentionDays (defence in depth). Callers should pass
// cfg.AlertsRetentionDays.
//
// PAT-017 config wiring trace: cfg.AlertsRetentionDays → this constructor
// → p.defaultRetentionDays → parseAlertRetentionDays(fallback) →
// effective days → DeleteOlderThanForTenant.
func NewAlertsRetentionProcessor(
	jobs *store.JobStore,
	alertStore *store.AlertStore,
	tenantStore *store.TenantStore,
	eventBus *bus.EventBus,
	retentionDays int,
	logger zerolog.Logger,
) *AlertsRetentionProcessor {
	if retentionDays < minAlertsRetentionDays {
		retentionDays = minAlertsRetentionDays
	}
	return &AlertsRetentionProcessor{
		jobs:                 jobs,
		alertStore:           alertStore,
		tenantStore:          tenantStore,
		eventBus:             eventBus,
		defaultRetentionDays: retentionDays,
		logger:               logger.With().Str("processor", JobTypeAlertsRetention).Logger(),
	}
}

func (p *AlertsRetentionProcessor) Type() string {
	return JobTypeAlertsRetention
}

// perTenantResult is the per-tenant entry in the aggregate result JSON
// (DEV-336). Error is empty on success and carries the error message
// when one tenant's DELETE failed but the job continued.
type perTenantResult struct {
	TenantID      uuid.UUID `json:"tenant_id"`
	RetentionDays int       `json:"retention_days"`
	Deleted       int64     `json:"deleted"`
	Cutoff        time.Time `json:"cutoff"`
	Error         string    `json:"error,omitempty"`
}

type alertsRetentionAggregateResult struct {
	TotalDeleted     int64             `json:"total_deleted"`
	TenantsProcessed int               `json:"tenants_processed"`
	TenantsErrored   int               `json:"tenants_errored"`
	PerTenant        []perTenantResult `json:"per_tenant"`
	Status           string            `json:"status"`
}

// parseAlertRetentionDays reads the per-tenant override from
// tenants.settings.alert_retention_days (DEV-335). The settings column is
// JSONB; numbers come through as float64 in encoding/json. Falls back to
// defaultDays when:
//   - raw is empty or JSON null
//   - the JSON does not parse as an object
//   - the alert_retention_days key is missing
//   - the value is not a number, or is a non-integer float
//
// Applies the 30-day floor and 365-day ceiling defensively (the API
// already validates 30..365, but we don't trust the database to have
// only ever been written through the API).
func parseAlertRetentionDays(rawSettings json.RawMessage, defaultDays int) int {
	if len(rawSettings) == 0 || string(rawSettings) == "null" {
		return clampRetentionDays(defaultDays)
	}
	var settings map[string]any
	if err := json.Unmarshal(rawSettings, &settings); err != nil {
		return clampRetentionDays(defaultDays)
	}
	raw, ok := settings["alert_retention_days"]
	if !ok {
		return clampRetentionDays(defaultDays)
	}
	f, ok := raw.(float64)
	if !ok {
		return clampRetentionDays(defaultDays)
	}
	// Reject non-integer floats (e.g. 90.5) — config integers only.
	if f != float64(int(f)) {
		return clampRetentionDays(defaultDays)
	}
	return clampRetentionDays(int(f))
}

func clampRetentionDays(days int) int {
	if days < minAlertsRetentionDays {
		return minAlertsRetentionDays
	}
	if days > maxAlertsRetentionDays {
		return maxAlertsRetentionDays
	}
	return days
}

func (p *AlertsRetentionProcessor) Process(ctx context.Context, j *store.Job) error {
	p.logger.Info().
		Str("job_id", j.ID.String()).
		Int("default_retention_days", p.defaultRetentionDays).
		Msg("starting per-tenant alerts retention purge")

	var (
		perTenant        []perTenantResult
		totalDeleted     int64
		tenantsProcessed int
		tenantsErrored   int
	)

	cursor := ""
	for {
		tenants, next, err := p.tenantStore.List(ctx, cursor, tenantPageSize, "")
		if err != nil {
			return fmt.Errorf("alerts_retention: list tenants: %w", err)
		}
		for _, t := range tenants {
			// Skip tenants that are not active. We never want to
			// reach into suspended/terminated tenants on a cron sweep
			// — their data may be subject to legal hold or deferred
			// purge under a different policy.
			if t.State != "active" {
				continue
			}
			tenantsProcessed++
			days := parseAlertRetentionDays(t.Settings, p.defaultRetentionDays)
			cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)

			deleted, derr := p.alertStore.DeleteOlderThanForTenant(ctx, t.ID, cutoff)
			res := perTenantResult{
				TenantID:      t.ID,
				RetentionDays: days,
				Deleted:       deleted,
				Cutoff:        cutoff,
			}
			if derr != nil {
				p.logger.Error().
					Err(derr).
					Str("tenant_id", t.ID.String()).
					Msg("alerts_retention: tenant delete failed; continuing with next tenant")
				res.Error = derr.Error()
				tenantsErrored++
			} else {
				if deleted > largeDeleteWarnThreshold {
					p.logger.Warn().
						Int64("deleted", deleted).
						Str("tenant_id", t.ID.String()).
						Int("retention_days", days).
						Msg("alerts_retention: large delete — consider batching for this tenant")
				}
				totalDeleted += deleted
			}
			perTenant = append(perTenant, res)
		}
		if next == "" {
			break
		}
		cursor = next
	}

	result := alertsRetentionAggregateResult{
		TotalDeleted:     totalDeleted,
		TenantsProcessed: tenantsProcessed,
		TenantsErrored:   tenantsErrored,
		PerTenant:        perTenant,
		Status:           "completed",
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal retention result: %w", err)
	}

	if err := p.jobs.Complete(ctx, j.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete alerts retention job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":            j.ID.String(),
			"tenant_id":         j.TenantID.String(),
			"type":              JobTypeAlertsRetention,
			"state":             "completed",
			"total_deleted":     totalDeleted,
			"tenants_processed": tenantsProcessed,
			"tenants_errored":   tenantsErrored,
		})
	}

	p.logger.Info().
		Int64("total_deleted", totalDeleted).
		Int("tenants_processed", tenantsProcessed).
		Int("tenants_errored", tenantsErrored).
		Msg("alerts retention purge completed")

	return nil
}
