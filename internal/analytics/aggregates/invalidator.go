package aggregates

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/btopcu/argus/internal/bus"
)

const invalidatorQueue = "aggregates-invalidator"

// RegisterInvalidator wires the Aggregates cache to NATS write events.
// Call once at startup, AFTER bus.EventBus is ready and rdb is connected.
func RegisterInvalidator(eb *bus.EventBus, rdb *redis.Client, logger zerolog.Logger) error {
	l := logger.With().Str("component", "aggregates_invalidator").Logger()

	inv := &invalidator{rdb: rdb, logger: l}

	subjects := []struct {
		subject string
		handler bus.MessageHandlerCtx
	}{
		{bus.SubjectSIMUpdated, inv.onSIMUpdated},
		{bus.SubjectPolicyChanged, inv.onPolicyChanged},
		{bus.SubjectSessionStarted, inv.onSessionActivity},
		{bus.SubjectSessionEnded, inv.onSessionActivity},
	}

	for _, s := range subjects {
		subj := s.subject
		h := s.handler
		_, err := eb.QueueSubscribeCtx(subj, invalidatorQueue, h)
		if err != nil {
			return fmt.Errorf("aggregates: register invalidator for %s: %w", subj, err)
		}
		l.Info().Str("subject", subj).Msg("aggregates cache invalidator subscribed")
	}
	return nil
}

type invalidator struct {
	rdb    *redis.Client
	logger zerolog.Logger
}

func (i *invalidator) onSIMUpdated(ctx context.Context, subject string, data []byte) {
	tid, ok := parseTenantIDFromPayload(data)
	if !ok {
		i.logger.Debug().Str("subject", subject).Msg("aggregates invalidator: no tenant_id in payload, skipping")
		return
	}
	keys := []string{
		fmt.Sprintf("%s:%s:%s", keyPrefix, tid.String(), "sim_count_by_tenant"),
		fmt.Sprintf("%s:%s:%s", keyPrefix, tid.String(), "sim_count_by_operator"),
		fmt.Sprintf("%s:%s:%s", keyPrefix, tid.String(), "sim_count_by_apn"),
		fmt.Sprintf("%s:%s:%s", keyPrefix, tid.String(), "sim_count_by_state"),
	}
	if err := i.rdb.Del(ctx, keys...).Err(); err != nil {
		i.logger.Warn().Err(err).Str("subject", subject).Str("tenant_id", tid.String()).
			Msg("aggregates invalidator: DEL sim keys failed")
	}
	if err := i.unlinkPolicyKeys(ctx, tid); err != nil {
		i.logger.Warn().Err(err).Str("subject", subject).Str("tenant_id", tid.String()).
			Msg("aggregates invalidator: UNLINK policy keys failed")
	}
}

func (i *invalidator) onPolicyChanged(ctx context.Context, subject string, data []byte) {
	tid, ok := parseTenantIDFromPayload(data)
	if !ok {
		i.logger.Debug().Str("subject", subject).Msg("aggregates invalidator: no tenant_id in payload, skipping")
		return
	}
	if err := i.unlinkPolicyKeys(ctx, tid); err != nil {
		i.logger.Warn().Err(err).Str("subject", subject).Str("tenant_id", tid.String()).
			Msg("aggregates invalidator: UNLINK policy keys failed")
	}
}

func (i *invalidator) onSessionActivity(ctx context.Context, subject string, data []byte) {
	tid, ok := parseTenantIDFromPayload(data)
	if !ok {
		i.logger.Debug().Str("subject", subject).Msg("aggregates invalidator: no tenant_id in payload, skipping")
		return
	}
	keys := []string{
		fmt.Sprintf("%s:%s:%s", keyPrefix, tid.String(), "active_session_stats"),
		fmt.Sprintf("%s:%s:%s", keyPrefix, tid.String(), "traffic_by_operator"),
	}
	if err := i.rdb.Del(ctx, keys...).Err(); err != nil {
		i.logger.Warn().Err(err).Str("subject", subject).Str("tenant_id", tid.String()).
			Msg("aggregates invalidator: DEL session keys failed")
	}
	// CDR stats are downstream of session activity — drop filter-keyed variants.
	if err := i.unlinkCDRStatKeys(ctx, tid); err != nil {
		i.logger.Warn().Err(err).Str("subject", subject).Str("tenant_id", tid.String()).
			Msg("aggregates invalidator: UNLINK cdr stats keys failed")
	}
}

func (i *invalidator) unlinkCDRStatKeys(ctx context.Context, tid uuid.UUID) error {
	pattern := fmt.Sprintf("%s:%s:cdr_stats_in_window:*", keyPrefix, tid.String())
	iter := i.rdb.Scan(ctx, 0, pattern, 100).Iterator()
	var batch []string
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("scan cdr stats keys: %w", err)
	}
	if len(batch) == 0 {
		return nil
	}
	return i.rdb.Unlink(ctx, batch...).Err()
}

func (i *invalidator) unlinkPolicyKeys(ctx context.Context, tid uuid.UUID) error {
	pattern := fmt.Sprintf("%s:%s:sim_count_by_policy:*", keyPrefix, tid.String())
	iter := i.rdb.Scan(ctx, 0, pattern, 100).Iterator()
	var batch []string
	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("scan policy keys: %w", err)
	}
	if len(batch) == 0 {
		return nil
	}
	return i.rdb.Unlink(ctx, batch...).Err()
}

func parseTenantIDFromPayload(data []byte) (uuid.UUID, bool) {
	var payload struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.TenantID == "" {
		return uuid.Nil, false
	}
	tid, err := uuid.Parse(payload.TenantID)
	if err != nil {
		return uuid.Nil, false
	}
	return tid, true
}
