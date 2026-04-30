package aggregates

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ErrInvalidTenant is returned when a zero-value tenantID is passed.
var ErrInvalidTenant = errors.New("aggregates: invalid tenant")

// Aggregates is the canonical facade over cross-surface counting/summing queries.
// Every handler that displays aggregate metrics MUST consume via this interface
// instead of calling store methods directly. This guarantees:
//   - Identical numbers across all UI surfaces for the same logical metric
//   - Single canonical source per aggregate (see doc.go and F-125 decision)
//   - Centralized caching + NATS-based invalidation
type Aggregates interface {
	SIMCountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error)
	SIMCountByOperator(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int, error)
	SIMCountByAPN(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int64, error)
	SIMCountByPolicy(ctx context.Context, tenantID, policyID uuid.UUID) (int, error)
	SIMCountByState(ctx context.Context, tenantID uuid.UUID) (total int, byState []store.SIMStateCount, err error)
	ActiveSessionStats(ctx context.Context, tenantID uuid.UUID) (*store.SessionStatsResult, error)
	TrafficByOperator(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int64, error)
	// CDR aggregates (FIX-214). Window is (from, to]; filters narrow the set.
	CDRStatsInWindow(ctx context.Context, tenantID uuid.UUID, f CDRFilter) (*store.CDRStats, error)
}

// CDRFilter is the narrow subset of list predicates the stats facade accepts.
// Mirrors store.ListCDRParams but excludes cursor/limit and is passed by value
// so it is safe to key the cache on its stable JSON form.
type CDRFilter struct {
	SimID      *uuid.UUID `json:"sim_id,omitempty"`
	OperatorID *uuid.UUID `json:"operator_id,omitempty"`
	APNID      *uuid.UUID `json:"apn_id,omitempty"`
	SessionID  *uuid.UUID `json:"session_id,omitempty"`
	RecordType string     `json:"record_type,omitempty"`
	RATType    string     `json:"rat_type,omitempty"`
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
	MinCost    *float64   `json:"min_cost,omitempty"`
}

// toListParams adapts CDRFilter to the store filter struct.
func (f CDRFilter) toListParams() store.ListCDRParams {
	return store.ListCDRParams{
		SimID:      f.SimID,
		OperatorID: f.OperatorID,
		APNID:      f.APNID,
		SessionID:  f.SessionID,
		RecordType: f.RecordType,
		RATType:    f.RATType,
		From:       f.From,
		To:         f.To,
		MinCost:    f.MinCost,
	}
}

type dbAggregates struct {
	simStore     *store.SIMStore
	sessionStore *store.RadiusSessionStore
	cdrStore     *store.CDRStore
	logger       zerolog.Logger
}

// NewDB returns a pure-delegation Aggregates backed directly by the database.
// No caching; use Task 4's cached constructor for production use.
// cdrStore may be nil; callers that do not use CDR aggregates can pass nil.
func NewDB(simStore *store.SIMStore, sessionStore *store.RadiusSessionStore, cdrStore *store.CDRStore, logger zerolog.Logger) Aggregates {
	return &dbAggregates{simStore: simStore, sessionStore: sessionStore, cdrStore: cdrStore, logger: logger}
}

func (d *dbAggregates) SIMCountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	if tenantID == uuid.Nil {
		return 0, ErrInvalidTenant
	}
	count, err := d.simStore.CountByTenant(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("aggregates: SIMCountByTenant: %w", err)
	}
	return count, nil
}

func (d *dbAggregates) SIMCountByOperator(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int, error) {
	if tenantID == uuid.Nil {
		return nil, ErrInvalidTenant
	}
	result, err := d.simStore.CountByOperator(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("aggregates: SIMCountByOperator: %w", err)
	}
	return result, nil
}

func (d *dbAggregates) SIMCountByAPN(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int64, error) {
	if tenantID == uuid.Nil {
		return nil, ErrInvalidTenant
	}
	result, err := d.simStore.CountByAPN(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("aggregates: SIMCountByAPN: %w", err)
	}
	return result, nil
}

func (d *dbAggregates) SIMCountByPolicy(ctx context.Context, tenantID, policyID uuid.UUID) (int, error) {
	if tenantID == uuid.Nil {
		return 0, ErrInvalidTenant
	}
	if policyID == uuid.Nil {
		return 0, ErrInvalidTenant
	}
	count, err := d.simStore.CountByPolicyID(ctx, tenantID, policyID)
	if err != nil {
		return 0, fmt.Errorf("aggregates: SIMCountByPolicy: %w", err)
	}
	return count, nil
}

func (d *dbAggregates) SIMCountByState(ctx context.Context, tenantID uuid.UUID) (int, []store.SIMStateCount, error) {
	if tenantID == uuid.Nil {
		return 0, nil, ErrInvalidTenant
	}
	total, byState, err := d.simStore.CountByState(ctx, tenantID)
	if err != nil {
		return 0, nil, fmt.Errorf("aggregates: SIMCountByState: %w", err)
	}
	return total, byState, nil
}

func (d *dbAggregates) ActiveSessionStats(ctx context.Context, tenantID uuid.UUID) (*store.SessionStatsResult, error) {
	if tenantID == uuid.Nil {
		return nil, ErrInvalidTenant
	}
	tid := tenantID
	result, err := d.sessionStore.GetActiveStats(ctx, &tid)
	if err != nil {
		return nil, fmt.Errorf("aggregates: ActiveSessionStats: %w", err)
	}
	return result, nil
}

func (d *dbAggregates) TrafficByOperator(ctx context.Context, tenantID uuid.UUID) (map[uuid.UUID]int64, error) {
	if tenantID == uuid.Nil {
		return nil, ErrInvalidTenant
	}
	tid := tenantID
	result, err := d.sessionStore.TrafficByOperator(ctx, &tid)
	if err != nil {
		return nil, fmt.Errorf("aggregates: TrafficByOperator: %w", err)
	}
	return result, nil
}

func (d *dbAggregates) CDRStatsInWindow(ctx context.Context, tenantID uuid.UUID, f CDRFilter) (*store.CDRStats, error) {
	if tenantID == uuid.Nil {
		return nil, ErrInvalidTenant
	}
	if d.cdrStore == nil {
		return nil, fmt.Errorf("aggregates: CDRStatsInWindow: cdr store not configured")
	}
	result, err := d.cdrStore.StatsInWindow(ctx, tenantID, f.toListParams())
	if err != nil {
		return nil, fmt.Errorf("aggregates: CDRStatsInWindow: %w", err)
	}
	return result, nil
}
