package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/btopcu/argus/internal/alertstate"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// quotaBreachTenantStore is the minimal TenantStore subset the checker depends on.
type quotaBreachTenantStore interface {
	List(ctx context.Context, cursor string, limit int, stateFilter string) ([]store.Tenant, string, error)
	GetStats(ctx context.Context, tenantID uuid.UUID) (*store.TenantStats, error)
	EstimateAPIRPS(ctx context.Context, tenantID uuid.UUID) float64
}

// quotaBreachAlertStore is the minimal AlertStore subset the checker depends on.
type quotaBreachAlertStore interface {
	UpsertWithDedup(ctx context.Context, p store.CreateAlertParams, severityOrdinal int) (*store.Alert, store.UpsertResult, error)
	FindActiveByDedupKey(ctx context.Context, tenantID uuid.UUID, dedupKey string) (*store.Alert, error)
	UpdateState(ctx context.Context, tenantID, id uuid.UUID, newState string, userID *uuid.UUID, cooldownMinutes int) (*store.Alert, error)
}

// quotaBreachCheckerResult is the aggregate result JSON written to jobs.result.
type quotaBreachCheckerResult struct {
	Alerted    int `json:"alerted"`
	Deduped    int `json:"deduped"`
	Resolved   int `json:"resolved"`
	AlertFails int `json:"alert_fails"`
}

// QuotaBreachCheckerProcessor runs hourly, checks every active tenant's resource
// utilization against their quota ceilings, and raises deduplicated alerts via
// AlertStore.UpsertWithDedup (FIX-246 D-5):
//
//   - utilization >= 95% → alert type="quota.breach" severity=critical
//   - utilization >= 80% → alert type="quota.breach" severity=medium
//   - utilization  < 80% → auto-resolve any open quota alert for that tenant×metric
//
// Dedup key format: quota:{tenant_id}:{metric}:{severity}
// Metrics: sims, sessions, api_rps, storage_bytes
type QuotaBreachCheckerProcessor struct {
	jobs        *store.JobStore
	tenantStore quotaBreachTenantStore
	alertStore  quotaBreachAlertStore
	logger      zerolog.Logger
}

// NewQuotaBreachCheckerProcessor wires the quota breach checker.
func NewQuotaBreachCheckerProcessor(
	jobs *store.JobStore,
	tenantStore quotaBreachTenantStore,
	alertStore quotaBreachAlertStore,
	logger zerolog.Logger,
) *QuotaBreachCheckerProcessor {
	return &QuotaBreachCheckerProcessor{
		jobs:        jobs,
		tenantStore: tenantStore,
		alertStore:  alertStore,
		logger:      logger.With().Str("processor", JobTypeQuotaBreachChecker).Logger(),
	}
}

func (p *QuotaBreachCheckerProcessor) Type() string {
	return JobTypeQuotaBreachChecker
}

// quotaMetricSpec describes one quota metric: its name, max value (from the tenant
// record), and how to obtain the current value from TenantStats.
type quotaMetricSpec struct {
	name    string
	max     int64
	current int64
}

func (p *QuotaBreachCheckerProcessor) Process(ctx context.Context, j *store.Job) error {
	p.logger.Info().
		Str("job_id", j.ID.String()).
		Msg("starting quota_breach_checker sweep")

	var alerted, deduped, resolved, alertFails int

	cursor := ""
	for {
		tenants, nextCursor, err := p.tenantStore.List(ctx, cursor, 100, "active")
		if err != nil {
			return fmt.Errorf("quota_breach_checker: list tenants: %w", err)
		}

		for _, t := range tenants {
			if err := p.checkTenant(ctx, t, &alerted, &deduped, &resolved, &alertFails); err != nil {
				p.logger.Error().Err(err).Str("tenant_id", t.ID.String()).Msg("quota_breach_checker: tenant check failed; continuing")
			}
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	result := quotaBreachCheckerResult{
		Alerted:    alerted,
		Deduped:    deduped,
		Resolved:   resolved,
		AlertFails: alertFails,
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("quota_breach_checker: marshal result: %w", err)
	}

	if err := p.jobs.Complete(ctx, j.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("quota_breach_checker: complete job: %w", err)
	}

	p.logger.Info().
		Int("alerted", alerted).
		Int("deduped", deduped).
		Int("resolved", resolved).
		Int("alert_fails", alertFails).
		Msg("quota_breach_checker sweep completed")

	return nil
}

func (p *QuotaBreachCheckerProcessor) checkTenant(
	ctx context.Context,
	t store.Tenant,
	alerted, deduped, resolved, alertFails *int,
) error {
	stats, err := p.tenantStore.GetStats(ctx, t.ID)
	if err != nil {
		return fmt.Errorf("get stats: %w", err)
	}

	currentAPIRPS := p.tenantStore.EstimateAPIRPS(ctx, t.ID)

	metrics := []quotaMetricSpec{
		{name: "sims", max: int64(t.MaxSims), current: int64(stats.SimCount)},
		{name: "sessions", max: int64(t.MaxSessions), current: int64(stats.ActiveSessions)},
		{name: "api_rps", max: int64(t.MaxAPIRPS), current: int64(currentAPIRPS)},
		{name: "storage_bytes", max: t.MaxStorageBytes, current: int64(stats.StorageBytes)},
	}

	for _, m := range metrics {
		if m.max <= 0 {
			continue
		}

		pct := float64(m.current) / float64(m.max)

		var sev string
		if pct >= 0.95 {
			sev = severity.Critical
		} else if pct >= 0.80 {
			sev = severity.Medium
		}

		if sev != "" {
			p.upsertBreachAlert(ctx, t, m, sev, pct, alerted, deduped, alertFails)
			// If critical, also auto-resolve any stale medium alert for this metric.
			if sev == severity.Critical {
				p.resolveAlertByDedupKey(ctx, t.ID, fmt.Sprintf("quota:%s:%s:%s", t.ID, m.name, severity.Medium), resolved)
			}
		} else {
			// Below 80%: resolve both alert tiers if open.
			p.resolveAlertByDedupKey(ctx, t.ID, fmt.Sprintf("quota:%s:%s:%s", t.ID, m.name, severity.Critical), resolved)
			p.resolveAlertByDedupKey(ctx, t.ID, fmt.Sprintf("quota:%s:%s:%s", t.ID, m.name, severity.Medium), resolved)
		}
	}

	return nil
}

func (p *QuotaBreachCheckerProcessor) upsertBreachAlert(
	ctx context.Context,
	t store.Tenant,
	m quotaMetricSpec,
	sev string,
	pct float64,
	alerted, deduped, alertFails *int,
) {
	dedupKey := fmt.Sprintf("quota:%s:%s:%s", t.ID, m.name, sev)
	title := fmt.Sprintf("Quota breach: %s at %.0f%%", m.name, pct*100)
	desc := fmt.Sprintf(
		"Tenant %q metric %q is at %.1f%% utilization (%d/%d). Threshold: %s.",
		t.Name, m.name, pct*100, m.current, m.max, sev,
	)

	params := store.CreateAlertParams{
		TenantID:    t.ID,
		Type:        "quota.breach",
		Severity:    sev,
		Source:      "system",
		Title:       title,
		Description: desc,
		DedupKey:    &dedupKey,
	}

	_, res, err := p.alertStore.UpsertWithDedup(ctx, params, severity.Ordinal(sev))
	if err != nil {
		p.logger.Error().
			Err(err).
			Str("tenant_id", t.ID.String()).
			Str("metric", m.name).
			Str("severity", sev).
			Msg("quota_breach_checker: upsert alert failed; continuing")
		*alertFails++
		return
	}

	switch res {
	case store.UpsertInserted:
		*alerted++
	case store.UpsertDeduplicated:
		*deduped++
	}
}

func (p *QuotaBreachCheckerProcessor) resolveAlertByDedupKey(
	ctx context.Context,
	tenantID uuid.UUID,
	dedupKey string,
	resolved *int,
) {
	alert, err := p.alertStore.FindActiveByDedupKey(ctx, tenantID, dedupKey)
	if errors.Is(err, store.ErrAlertNotFound) {
		return
	}
	if err != nil {
		p.logger.Warn().Err(err).Str("dedup_key", dedupKey).Msg("quota_breach_checker: find active alert failed")
		return
	}

	if _, err := p.alertStore.UpdateState(ctx, tenantID, alert.ID, alertstate.StateResolved, nil, 0); err != nil {
		p.logger.Warn().Err(err).Str("dedup_key", dedupKey).Msg("quota_breach_checker: resolve alert failed")
		return
	}
	*resolved++
}
