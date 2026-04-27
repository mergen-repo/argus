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

// stockAlerterStockStore is the minimal EsimProfileStockStore subset the alerter depends on (PAT-019).
type stockAlerterStockStore interface {
	ListSummary(ctx context.Context, tenantID uuid.UUID) ([]store.EsimProfileStock, error)
}

// stockAlerterTenantStore is the minimal TenantStore subset the alerter depends on (PAT-019).
type stockAlerterTenantStore interface {
	List(ctx context.Context, cursor string, limit int, stateFilter string) ([]store.Tenant, string, error)
}

// stockAlerterAlertStore is the minimal AlertStore subset the alerter depends on (PAT-019).
type stockAlerterAlertStore interface {
	UpsertWithDedup(ctx context.Context, p store.CreateAlertParams, severityOrdinal int) (*store.Alert, store.UpsertResult, error)
	FindActiveByDedupKey(ctx context.Context, tenantID uuid.UUID, dedupKey string) (*store.Alert, error)
	UpdateState(ctx context.Context, tenantID, id uuid.UUID, newState string, userID *uuid.UUID, cooldownMinutes int) (*store.Alert, error)
}

// stockAlerterResult is the aggregate result JSON written to jobs.result.
type stockAlerterResult struct {
	Alerted    int `json:"alerted"`
	Deduped    int `json:"deduped"`
	Resolved   int `json:"resolved"`
	AlertFails int `json:"alert_fails"`
}

const (
	esimStockLowThreshold      = 0.10 // <10% → medium alert
	esimStockCriticalThreshold = 0.05 // <5%  → high alert
)

// ESimStockAlerterProcessor runs on cron (*/15 * * * *), checks every active
// tenant's eSIM stock utilization, and raises deduplicated alerts when available
// stock falls below 10% of total.
//
// JobType: JobTypeESimStockAlerter
// Cron: */15 * * * * (env CRON_ESIM_STOCK_ALERT)
//
// Dedup key: esim_stock:{tenant_id}:{operator_id}
// Severity: high if <5%, medium if <10%
// Source: "system" (PAT-024 — required by chk_alerts_source CHECK constraint)
type ESimStockAlerterProcessor struct {
	jobs        *store.JobStore
	stockStore  stockAlerterStockStore
	tenantStore stockAlerterTenantStore
	alertStore  stockAlerterAlertStore
	logger      zerolog.Logger
}

// NewESimStockAlerterProcessor wires the stock alerter.
func NewESimStockAlerterProcessor(
	jobs *store.JobStore,
	stockStore stockAlerterStockStore,
	tenantStore stockAlerterTenantStore,
	alertStore stockAlerterAlertStore,
	logger zerolog.Logger,
) *ESimStockAlerterProcessor {
	return &ESimStockAlerterProcessor{
		jobs:        jobs,
		stockStore:  stockStore,
		tenantStore: tenantStore,
		alertStore:  alertStore,
		logger:      logger.With().Str("processor", JobTypeESimStockAlerter).Logger(),
	}
}

func (p *ESimStockAlerterProcessor) Type() string {
	return JobTypeESimStockAlerter
}

func (p *ESimStockAlerterProcessor) Process(ctx context.Context, j *store.Job) error {
	p.logger.Info().Str("job_id", j.ID.String()).Msg("esim_stock_alerter: starting sweep")

	result, err := p.runSweep(ctx)
	if err != nil {
		return err
	}

	resultJSON, mErr := json.Marshal(result)
	if mErr != nil {
		return fmt.Errorf("esim_stock_alerter: marshal result: %w", mErr)
	}

	if p.jobs != nil {
		if cErr := p.jobs.Complete(ctx, j.ID, nil, resultJSON); cErr != nil {
			return fmt.Errorf("esim_stock_alerter: complete job: %w", cErr)
		}
	}

	p.logger.Info().
		Int("alerted", result.Alerted).
		Int("deduped", result.Deduped).
		Int("resolved", result.Resolved).
		Int("alert_fails", result.AlertFails).
		Msg("esim_stock_alerter sweep completed")

	return nil
}

// runSweep is the core logic, separated for unit testing without a real Job or JobStore.
func (p *ESimStockAlerterProcessor) runSweep(ctx context.Context) (stockAlerterResult, error) {
	var alerted, deduped, resolved, alertFails int

	cursor := ""
	for {
		tenants, nextCursor, err := p.tenantStore.List(ctx, cursor, 100, "active")
		if err != nil {
			return stockAlerterResult{}, fmt.Errorf("esim_stock_alerter: list tenants: %w", err)
		}

		for _, t := range tenants {
			if err := p.checkTenant(ctx, t, &alerted, &deduped, &resolved, &alertFails); err != nil {
				p.logger.Error().Err(err).Str("tenant_id", t.ID.String()).Msg("esim_stock_alerter: tenant check failed; continuing")
			}
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return stockAlerterResult{
		Alerted:    alerted,
		Deduped:    deduped,
		Resolved:   resolved,
		AlertFails: alertFails,
	}, nil
}

func (p *ESimStockAlerterProcessor) checkTenant(
	ctx context.Context,
	t store.Tenant,
	alerted, deduped, resolved, alertFails *int,
) error {
	stocks, err := p.stockStore.ListSummary(ctx, t.ID)
	if err != nil {
		return fmt.Errorf("list stock summary: %w", err)
	}

	for _, s := range stocks {
		if s.Total <= 0 {
			continue
		}

		ratio := float64(s.Available) / float64(s.Total)
		dedupKey := fmt.Sprintf("esim_stock:%s:%s", t.ID, s.OperatorID)

		var sev string
		if ratio < esimStockCriticalThreshold {
			sev = severity.High
		} else if ratio < esimStockLowThreshold {
			sev = severity.Medium
		}

		if sev != "" {
			p.upsertStockAlert(ctx, t, s, sev, ratio, dedupKey, alerted, deduped, alertFails)
		} else {
			p.resolveAlertByDedupKey(ctx, t.ID, dedupKey, resolved)
		}
	}

	return nil
}

func (p *ESimStockAlerterProcessor) upsertStockAlert(
	ctx context.Context,
	t store.Tenant,
	s store.EsimProfileStock,
	sev string,
	ratio float64,
	dedupKey string,
	alerted, deduped, alertFails *int,
) {
	title := fmt.Sprintf("eSIM stock low: operator %s at %.0f%%", s.OperatorID, ratio*100)
	desc := fmt.Sprintf(
		"Tenant %q operator %s has %.1f%% eSIM stock available (%d/%d). Severity: %s.",
		t.Name, s.OperatorID, ratio*100, s.Available, s.Total, sev,
	)

	params := store.CreateAlertParams{
		TenantID:    t.ID,
		Type:        "esim_stock_low",
		Severity:    sev,
		Source:      "system", // PAT-024: chk_alerts_source CHECK requires "system" for background jobs
		Title:       title,
		Description: desc,
		DedupKey:    &dedupKey,
	}

	_, res, err := p.alertStore.UpsertWithDedup(ctx, params, severity.Ordinal(sev))
	if err != nil {
		p.logger.Error().
			Err(err).
			Str("tenant_id", t.ID.String()).
			Str("operator_id", s.OperatorID.String()).
			Msg("esim_stock_alerter: upsert alert failed; continuing")
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

func (p *ESimStockAlerterProcessor) resolveAlertByDedupKey(
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
		p.logger.Warn().Err(err).Str("dedup_key", dedupKey).Msg("esim_stock_alerter: find active alert failed")
		return
	}

	if _, err := p.alertStore.UpdateState(ctx, tenantID, alert.ID, alertstate.StateResolved, nil, 0); err != nil {
		p.logger.Warn().Err(err).Str("dedup_key", dedupKey).Msg("esim_stock_alerter: resolve alert failed")
		return
	}
	*resolved++
}
