package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// coAFailureAlerterPolicyStore is the minimal PolicyStore subset the alerter depends on.
// Defining it as an interface (PAT-019) lets unit tests inject fakes without Postgres.
type coAFailureAlerterPolicyStore interface {
	ListStuckCoAFailures(ctx context.Context, age time.Duration) ([]store.StuckCoAFailure, error)
	CoAStatusCounts(ctx context.Context) (map[string]int64, error)
}

// coAFailureAlerterAlertStore is the minimal AlertStore subset the alerter depends on.
type coAFailureAlerterAlertStore interface {
	UpsertWithDedup(ctx context.Context, p store.CreateAlertParams, severityOrdinal int) (*store.Alert, store.UpsertResult, error)
}

// coAFailureAlerterAge is the minimum age a coa_status='failed' row must be before
// the alerter fires an alert for it. Conservative at 5 minutes so transient failures
// during a rollout stage do not immediately flood the alert feed.
const coAFailureAlerterAge = 5 * time.Minute

// CoAFailureAlerterProcessor sweeps policy_assignments for long-lived coa_status='failed'
// rows, raises a deduplicated alert per SIM via AlertStore.UpsertWithDedup, and refreshes
// the argus_coa_status_by_state Prometheus gauge for all 6 canonical coa_status values.
//
// AC-7 (FIX-234): background sweep + dedup alert per stuck failure.
// AC-8 (FIX-234): Prometheus gauge argus_coa_status_by_state{state}.
type CoAFailureAlerterProcessor struct {
	jobs        *store.JobStore
	policyStore coAFailureAlerterPolicyStore
	alertStore  coAFailureAlerterAlertStore
	reg         *metrics.Registry
	eventBus    *bus.EventBus
	logger      zerolog.Logger
}

// NewCoAFailureAlerterProcessor wires the CoA failure alerter.
func NewCoAFailureAlerterProcessor(
	jobs *store.JobStore,
	policyStore coAFailureAlerterPolicyStore,
	alertStore coAFailureAlerterAlertStore,
	reg *metrics.Registry,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *CoAFailureAlerterProcessor {
	return &CoAFailureAlerterProcessor{
		jobs:        jobs,
		policyStore: policyStore,
		alertStore:  alertStore,
		reg:         reg,
		eventBus:    eventBus,
		logger:      logger.With().Str("processor", JobTypeCoAFailureAlerter).Logger(),
	}
}

func (p *CoAFailureAlerterProcessor) Type() string {
	return JobTypeCoAFailureAlerter
}

// coaAlerterResult is the aggregate result JSON written to jobs.result.
type coaAlerterResult struct {
	Alerted    int `json:"alerted"`
	Deduped    int `json:"deduped"`
	CoolDown   int `json:"cooldown"`
	AlertFails int `json:"alert_fails"`
}

func (p *CoAFailureAlerterProcessor) Process(ctx context.Context, j *store.Job) error {
	p.logger.Info().
		Str("job_id", j.ID.String()).
		Dur("age_threshold", coAFailureAlerterAge).
		Msg("starting coa_failure_alerter sweep")

	failures, err := p.policyStore.ListStuckCoAFailures(ctx, coAFailureAlerterAge)
	if err != nil {
		return fmt.Errorf("coa_failure_alerter: list: %w", err)
	}

	var alerted, deduped, cooldown, alertFails int
	severityOrdinalHigh := severity.Ordinal(severity.High)

	for _, f := range failures {
		simIDStr := f.SimID.String()
		dedupKey := "coa_failed:" + simIDStr
		params := store.CreateAlertParams{
			TenantID:    f.TenantID,
			Type:        "coa_delivery_failed",
			Severity:    severity.High,
			Source:      "rollout",
			Title:       "CoA delivery failed",
			Description: fmt.Sprintf("CoA failed for SIM %s; last attempt %s", simIDStr, f.FailedAt.Format(time.RFC3339)),
			SimID:       ptrUUID(f.SimID),
			DedupKey:    &dedupKey,
		}

		_, res, uErr := p.alertStore.UpsertWithDedup(ctx, params, severityOrdinalHigh)
		if uErr != nil {
			p.logger.Error().
				Err(uErr).
				Str("sim_id", simIDStr).
				Msg("coa_failure_alerter: upsert alert failed; continuing")
			alertFails++
			continue
		}
		switch res {
		case store.UpsertInserted:
			alerted++
		case store.UpsertDeduplicated:
			deduped++
		case store.UpsertCoolingDown:
			cooldown++
		}
	}

	counts, cErr := p.policyStore.CoAStatusCounts(ctx)
	if cErr != nil {
		p.logger.Warn().Err(cErr).Msg("coa_failure_alerter: coa status counts failed; gauge not updated")
	} else if p.reg != nil && p.reg.CoAStatusByState != nil {
		for state, count := range counts {
			p.reg.CoAStatusByState.WithLabelValues(state).Set(float64(count))
		}
	}

	result := coaAlerterResult{
		Alerted:    alerted,
		Deduped:    deduped,
		CoolDown:   cooldown,
		AlertFails: alertFails,
	}
	resultJSON, mErr := json.Marshal(result)
	if mErr != nil {
		return fmt.Errorf("coa_failure_alerter: marshal result: %w", mErr)
	}

	var errorReport json.RawMessage
	if alertFails > 0 {
		errPayload := map[string]interface{}{
			"message":     fmt.Sprintf("coa_failure_alerter: %d alert upsert(s) failed during sweep", alertFails),
			"alert_fails": alertFails,
			"alerted":     alerted,
		}
		erBytes, erErr := json.Marshal(errPayload)
		if erErr != nil {
			return fmt.Errorf("coa_failure_alerter: marshal error report: %w", erErr)
		}
		errorReport = erBytes
	}

	if err := p.jobs.Complete(ctx, j.ID, errorReport, resultJSON); err != nil {
		return fmt.Errorf("coa_failure_alerter: complete job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":      j.ID.String(),
			"type":        JobTypeCoAFailureAlerter,
			"state":       "completed",
			"alerted":     alerted,
			"deduped":     deduped,
			"cooldown":    cooldown,
			"alert_fails": alertFails,
		})
	}

	p.logger.Info().
		Int("alerted", alerted).
		Int("deduped", deduped).
		Int("cooldown", cooldown).
		Int("alert_fails", alertFails).
		Msg("coa_failure_alerter sweep completed")

	return nil
}

func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}
