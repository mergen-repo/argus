package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/policy/binding"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// bindingGraceScannerWindow is the look-ahead window AC-6 mandates: SIMs whose
// binding_grace_expires_at falls within the next 24 hours are surfaced.
const bindingGraceScannerWindow = 24 * time.Hour

// bindingGraceDedupTTL matches the 24h window so a SIM is notified at most
// once per look-ahead window even if the scanner runs hourly.
const bindingGraceDedupTTL = 24 * time.Hour

// bindingGraceGraceScannerSource is the Source field on the published bus
// envelope so downstream sinks can attribute the event to the cron job.
const bindingGraceScannerSource = "job.binding_grace_scanner"

// bindingGraceScannerTitle is the user-facing envelope title; the
// notification dispatcher renders the localized template, this is the
// default envelope.Title that satisfies bus.Envelope.Validate.
const bindingGraceScannerTitle = "Device binding grace expiring"

// graceSimScanner is the narrow store dependency the scanner needs. Defined
// as an interface (PAT-019) so unit tests can inject a fake without spinning
// up Postgres.
type graceSimScanner interface {
	ListSIMsApproachingGraceExpiry(ctx context.Context, lower, upper time.Time) ([]store.SIMApproachingGraceExpiry, error)
}

// graceDedupCache abstracts the Redis SETNX check so tests can use a fake.
// The concrete production implementation wraps *redis.Client.SetNX.
//
// Returning (notified=true) means "this SIM has already been notified within
// the dedup window; skip publishing". Returning (notified=false) means
// "newly recorded; safe to publish". On error, the scanner FAILS OPEN
// (treats the SIM as not-yet-notified and publishes anyway) — the trade-off
// is over-notification on Redis outage rather than missing a critical
// pre-expiry warning.
type graceDedupCache interface {
	MarkNotified(ctx context.Context, simID uuid.UUID, ttl time.Duration) (alreadyNotified bool, err error)
}

// bindingGraceRedisDedup is the production implementation of graceDedupCache.
// Key shape: "binding:grace_notified:{sim_id}". TTL = 24h. SETNX semantics
// give us atomic check-and-set.
type bindingGraceRedisDedup struct {
	rdb *redis.Client
}

// NewBindingGraceRedisDedup wraps a *redis.Client into a graceDedupCache. A
// nil client is permitted — MarkNotified then returns (false, nil) so the
// scanner publishes every iteration (same behaviour as fail-open on error).
func NewBindingGraceRedisDedup(rdb *redis.Client) graceDedupCache {
	return &bindingGraceRedisDedup{rdb: rdb}
}

func (d *bindingGraceRedisDedup) MarkNotified(ctx context.Context, simID uuid.UUID, ttl time.Duration) (bool, error) {
	if d == nil || d.rdb == nil {
		return false, nil
	}
	key := fmt.Sprintf("binding:grace_notified:%s", simID)
	ok, err := d.rdb.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, err
	}
	// SETNX returns ok=true when the key was newly set (not previously
	// notified). ok=false means the key already existed (already notified).
	return !ok, nil
}

// BindingGraceScanner is the SVC-09 hourly cron processor that publishes
// device.binding_grace_expiring notifications for SIMs whose grace window is
// within 24h of expiring (STORY-097 AC-6). The scanner is cross-tenant —
// each published envelope carries the owning SIM's tenant_id so downstream
// per-tenant filtering still applies.
type BindingGraceScanner struct {
	jobs     jobProgressTracker
	sims     graceSimScanner
	dedup    graceDedupCache
	eventBus busPublisher
	logger   zerolog.Logger

	// now is injectable for deterministic tests; defaults to time.Now in
	// the constructor.
	now func() time.Time
}

// NewBindingGraceScanner wires the production scanner. Pass a nil
// graceDedupCache to disable dedup entirely (every run notifies every
// in-window SIM). Pass NewBindingGraceRedisDedup(rdb) for the standard
// Redis-backed dedup.
func NewBindingGraceScanner(
	jobs *store.JobStore,
	sims *store.SIMStore,
	dedup graceDedupCache,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BindingGraceScanner {
	return &BindingGraceScanner{
		jobs:     jobs,
		sims:     sims,
		dedup:    dedup,
		eventBus: eventBus,
		logger:   logger.With().Str("processor", JobTypeBindingGraceScanner).Logger(),
		now:      time.Now,
	}
}

// Type satisfies the job.Processor contract.
func (s *BindingGraceScanner) Type() string {
	return JobTypeBindingGraceScanner
}

// bindingGraceScannerResult is the JSON written to jobs.result on completion.
type bindingGraceScannerResult struct {
	Scanned      int `json:"scanned"`
	Notified     int `json:"notified"`
	SkippedDedup int `json:"skipped_dedup"`
	Errors       int `json:"errors"`
}

// Process executes the AC-6 scan: fetch all in-window grace-period SIMs,
// dedup-check via Redis, and publish a device.binding_grace_expiring envelope
// per surviving SIM. Errors on individual SIMs do NOT abort the loop — the
// scanner records the count and moves on (AC-12: must not block auth path).
func (s *BindingGraceScanner) Process(ctx context.Context, j *store.Job) error {
	now := s.now().UTC()
	upper := now.Add(bindingGraceScannerWindow)

	s.logger.Info().
		Str("job_id", j.ID.String()).
		Time("window_start", now).
		Time("window_end", upper).
		Msg("starting binding grace scan")

	rows, err := s.sims.ListSIMsApproachingGraceExpiry(ctx, now, upper)
	if err != nil {
		return fmt.Errorf("binding_grace_scanner: list: %w", err)
	}

	res := bindingGraceScannerResult{Scanned: len(rows)}

	for _, r := range rows {
		// Dedup check. Fail-open on error: log, then publish.
		if s.dedup != nil {
			already, dErr := s.dedup.MarkNotified(ctx, r.ID, bindingGraceDedupTTL)
			if dErr != nil {
				s.logger.Warn().
					Err(dErr).
					Str("sim_id", r.ID.String()).
					Msg("binding_grace_scanner: dedup error; failing open and publishing anyway")
			} else if already {
				res.SkippedDedup++
				continue
			}
		}

		if err := s.publish(ctx, r); err != nil {
			s.logger.Error().
				Err(err).
				Str("sim_id", r.ID.String()).
				Str("tenant_id", r.TenantID.String()).
				Msg("binding_grace_scanner: publish failed; continuing")
			res.Errors++
			continue
		}
		res.Notified++
	}

	resultJSON, _ := json.Marshal(res)

	if s.jobs != nil {
		if err := s.jobs.Complete(ctx, j.ID, nil, resultJSON); err != nil {
			return fmt.Errorf("binding_grace_scanner: complete job: %w", err)
		}
	}

	if s.eventBus != nil {
		_ = s.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":        j.ID.String(),
			"tenant_id":     j.TenantID.String(),
			"type":          JobTypeBindingGraceScanner,
			"state":         "completed",
			"scanned":       res.Scanned,
			"notified":      res.Notified,
			"skipped_dedup": res.SkippedDedup,
			"errors":        res.Errors,
		})
	}

	s.logger.Info().
		Int("scanned", res.Scanned).
		Int("notified", res.Notified).
		Int("skipped_dedup", res.SkippedDedup).
		Int("errors", res.Errors).
		Msg("binding grace scan completed")

	return nil
}

// publish builds the bus.Envelope for one in-window SIM and dispatches it on
// bus.SubjectNotification (the same subject the binding orchestrator already
// uses for device.binding_* events). The envelope's Type is
// NotifSubjectBindingGraceExpiring so dispatcher routing keys remain
// intact (FIX-212 wire contract).
func (s *BindingGraceScanner) publish(ctx context.Context, r store.SIMApproachingGraceExpiry) error {
	if s.eventBus == nil {
		return nil
	}
	dedupKey := fmt.Sprintf("binding:grace_notified:%s", r.ID)
	env := bus.NewEnvelope(
		binding.NotifSubjectBindingGraceExpiring,
		r.TenantID.String(),
		severity.Medium,
	).
		WithSource(bindingGraceScannerSource).
		WithTitle(bindingGraceScannerTitle).
		SetEntity("sim", r.ID.String(), r.ICCID).
		WithDedupKey(dedupKey).
		WithMeta("sim_id", r.ID.String()).
		WithMeta("iccid", r.ICCID).
		WithMeta("binding_grace_expires_at", r.BindingGraceExpiresAt.UTC().Format(time.RFC3339Nano))

	return s.eventBus.Publish(ctx, bus.SubjectNotification, env)
}
