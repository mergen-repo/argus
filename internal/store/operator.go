package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrOperatorNotFound   = errors.New("store: operator not found")
	ErrOperatorCodeExists = errors.New("store: operator code already exists")
	ErrMCCMNCExists       = errors.New("store: operator mcc+mnc already exists")
	ErrGrantNotFound      = errors.New("store: operator grant not found")
	ErrGrantExists        = errors.New("store: operator grant already exists")
)

type Operator struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Code string    `json:"code"`
	MCC  string    `json:"mcc"`
	MNC  string    `json:"mnc"`
	// AdapterType was removed in STORY-090 Wave 2 D2-B. The per-
	// protocol enablement flags now live in AdapterConfig (nested
	// JSONB shape). Callers that need a single-protocol label use
	// adapterschema.DerivePrimaryProtocol(parsed) on the decrypted
	// config. Historical audit rows retain the old field as a JSONB
	// attribute — not backfilled.
	AdapterConfig             json.RawMessage `json:"adapter_config"`
	SMDPPlusURL               *string         `json:"sm_dp_plus_url"`
	SMDPPlusConfig            json.RawMessage `json:"sm_dp_plus_config"`
	SupportedRATTypes         []string        `json:"supported_rat_types"`
	HealthStatus              string          `json:"health_status"`
	CircuitState              string          `json:"circuit_state"` // FIX-308
	HealthCheckIntervalSec    int             `json:"health_check_interval_sec"`
	FailoverPolicy            string          `json:"failover_policy"`
	FailoverTimeoutMs         int             `json:"failover_timeout_ms"`
	CircuitBreakerThreshold   int             `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverySec int             `json:"circuit_breaker_recovery_sec"`
	SLAUptimeTarget           *float64        `json:"sla_uptime_target"`
	SLALatencyThresholdMs     int             `json:"sla_latency_threshold_ms"`
	State                     string          `json:"state"`
	CreatedAt                 time.Time       `json:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at"`
}

type OperatorGrant struct {
	ID                uuid.UUID  `json:"id"`
	TenantID          uuid.UUID  `json:"tenant_id"`
	OperatorID        uuid.UUID  `json:"operator_id"`
	Enabled           bool       `json:"enabled"`
	SoRPriority       int        `json:"sor_priority"`
	CostPerMB         *float64   `json:"cost_per_mb"`
	Region            *string    `json:"region"`
	SupportedRATTypes []string   `json:"supported_rat_types"`
	GrantedAt         time.Time  `json:"granted_at"`
	GrantedBy         *uuid.UUID `json:"granted_by"`
}

type GrantWithOperator struct {
	OperatorGrant
	OperatorName              string    `json:"operator_name"`
	OperatorCode              string    `json:"operator_code"`
	MCC                       string    `json:"mcc"`
	MNC                       string    `json:"mnc"`
	OperatorSupportedRATTypes []string  `json:"operator_supported_rat_types"`
	HealthStatus              string    `json:"health_status"`
	OperatorState             string    `json:"operator_state"`
	SLATarget                 *float64  `json:"sla_target,omitempty"`
	OperatorUpdatedAt         time.Time `json:"operator_updated_at"`
}

type UpdateGrantParams struct {
	SoRPriority       *int
	CostPerMB         *float64
	Region            *string
	Enabled           *bool
	SupportedRATTypes []string
}

type OperatorHealthLog struct {
	ID           int64     `json:"id"`
	OperatorID   uuid.UUID `json:"operator_id"`
	CheckedAt    time.Time `json:"checked_at"`
	Status       string    `json:"status"`
	LatencyMs    *int      `json:"latency_ms"`
	ErrorMessage *string   `json:"error_message"`
	CircuitState string    `json:"circuit_state"`
}

type OperatorHealthSnapshot struct {
	OperatorID   uuid.UUID
	CheckedAt    time.Time
	Status       string
	LatencyMs    *int
	CircuitState string
}

type CreateOperatorParams struct {
	Name string
	Code string
	MCC  string
	MNC  string
	// AdapterType removed in STORY-090 Wave 2 D2-B — the nested
	// AdapterConfig carries per-protocol enablement flags.
	AdapterConfig             json.RawMessage
	SMDPPlusURL               *string
	SMDPPlusConfig            json.RawMessage
	SupportedRATTypes         []string
	FailoverPolicy            *string
	FailoverTimeoutMs         *int
	CircuitBreakerThreshold   *int
	CircuitBreakerRecoverySec *int
	HealthCheckIntervalSec    *int
	SLAUptimeTarget           *float64
}

type UpdateOperatorParams struct {
	Name                      *string
	AdapterConfig             json.RawMessage
	SMDPPlusURL               *string
	SMDPPlusConfig            json.RawMessage
	SupportedRATTypes         []string
	FailoverPolicy            *string
	FailoverTimeoutMs         *int
	CircuitBreakerThreshold   *int
	CircuitBreakerRecoverySec *int
	HealthCheckIntervalSec    *int
	SLAUptimeTarget           *float64
	State                     *string
}

type OperatorStore struct {
	db         *pgxpool.Pool
	auditStore *AuditStore
}

func NewOperatorStore(db *pgxpool.Pool) *OperatorStore {
	return &OperatorStore{db: db}
}

func (s *OperatorStore) WithAuditStore(a *AuditStore) *OperatorStore {
	s.auditStore = a
	return s
}

var operatorColumns = `id, name, code, mcc, mnc, adapter_config, sm_dp_plus_url,
	sm_dp_plus_config, supported_rat_types, health_status, circuit_state, health_check_interval_sec,
	failover_policy, failover_timeout_ms, circuit_breaker_threshold, circuit_breaker_recovery_sec,
	sla_uptime_target, sla_latency_threshold_ms, state, created_at, updated_at`

func scanOperator(row pgx.Row) (*Operator, error) {
	var o Operator
	err := row.Scan(
		&o.ID, &o.Name, &o.Code, &o.MCC, &o.MNC,
		&o.AdapterConfig, &o.SMDPPlusURL,
		&o.SMDPPlusConfig, &o.SupportedRATTypes,
		&o.HealthStatus, &o.CircuitState, &o.HealthCheckIntervalSec,
		&o.FailoverPolicy, &o.FailoverTimeoutMs,
		&o.CircuitBreakerThreshold, &o.CircuitBreakerRecoverySec,
		&o.SLAUptimeTarget, &o.SLALatencyThresholdMs, &o.State, &o.CreatedAt, &o.UpdatedAt,
	)
	return &o, err
}

func (s *OperatorStore) Create(ctx context.Context, p CreateOperatorParams) (*Operator, error) {
	failoverPolicy := "reject"
	if p.FailoverPolicy != nil {
		failoverPolicy = *p.FailoverPolicy
	}
	failoverTimeoutMs := 5000
	if p.FailoverTimeoutMs != nil {
		failoverTimeoutMs = *p.FailoverTimeoutMs
	}
	cbThreshold := 5
	if p.CircuitBreakerThreshold != nil {
		cbThreshold = *p.CircuitBreakerThreshold
	}
	cbRecoverySec := 60
	if p.CircuitBreakerRecoverySec != nil {
		cbRecoverySec = *p.CircuitBreakerRecoverySec
	}
	healthInterval := 30
	if p.HealthCheckIntervalSec != nil {
		healthInterval = *p.HealthCheckIntervalSec
	}

	adapterConfig := json.RawMessage(`{}`)
	if p.AdapterConfig != nil && len(p.AdapterConfig) > 0 {
		adapterConfig = p.AdapterConfig
	}
	smDPConfig := json.RawMessage(`{}`)
	if p.SMDPPlusConfig != nil && len(p.SMDPPlusConfig) > 0 {
		smDPConfig = p.SMDPPlusConfig
	}
	ratTypes := p.SupportedRATTypes
	if ratTypes == nil {
		ratTypes = []string{}
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO operators (name, code, mcc, mnc, adapter_config,
			sm_dp_plus_url, sm_dp_plus_config, supported_rat_types,
			failover_policy, failover_timeout_ms,
			circuit_breaker_threshold, circuit_breaker_recovery_sec,
			health_check_interval_sec, sla_uptime_target)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+operatorColumns,
		p.Name, p.Code, p.MCC, p.MNC, adapterConfig,
		p.SMDPPlusURL, smDPConfig, ratTypes,
		failoverPolicy, failoverTimeoutMs,
		cbThreshold, cbRecoverySec,
		healthInterval, p.SLAUptimeTarget,
	)

	o, err := scanOperator(row)
	if err != nil {
		if isDuplicateKeyError(err) {
			if strings.Contains(err.Error(), "idx_operators_code") || strings.Contains(err.Error(), "operators_code_key") {
				return nil, ErrOperatorCodeExists
			}
			if strings.Contains(err.Error(), "idx_operators_mcc_mnc") {
				return nil, ErrMCCMNCExists
			}
			if strings.Contains(err.Error(), "operators_name_key") {
				return nil, ErrOperatorCodeExists
			}
			return nil, ErrOperatorCodeExists
		}
		return nil, fmt.Errorf("store: create operator: %w", err)
	}
	return o, nil
}

func (s *OperatorStore) GetByID(ctx context.Context, id uuid.UUID) (*Operator, error) {
	row := s.db.QueryRow(ctx, `SELECT `+operatorColumns+` FROM operators WHERE id = $1`, id)
	o, err := scanOperator(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOperatorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get operator: %w", err)
	}
	return o, nil
}

// ListNamesByIDs returns a map of operator id → name for the supplied id set.
// Single round-trip via WHERE id = ANY($1) — replaces N+1 GetByID loops in
// hot batch hydration paths (e.g. report.AlertsExport — FIX-229 Gate F-A5).
// Missing IDs are simply absent from the map (no error). Empty input returns
// an empty map without hitting the DB.
func (s *OperatorStore) ListNamesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.db.Query(ctx, `SELECT id, name FROM operators WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("store: list operator names by ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("store: scan operator name: %w", err)
		}
		out[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate operator names: %w", err)
	}
	return out, nil
}

func (s *OperatorStore) GetByCode(ctx context.Context, code string) (*Operator, error) {
	row := s.db.QueryRow(ctx, `SELECT `+operatorColumns+` FROM operators WHERE code = $1`, code)
	o, err := scanOperator(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOperatorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get operator by code: %w", err)
	}
	return o, nil
}

func (s *OperatorStore) List(ctx context.Context, cursor string, limit int, stateFilter string) ([]Operator, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	conditions := []string{}
	argIdx := 1

	if stateFilter != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, stateFilter)
		argIdx++
	}

	if cursor != "" {
		cursorID, parseErr := uuid.Parse(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM operators %s ORDER BY created_at DESC, id DESC LIMIT %s`,
		operatorColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list operators: %w", err)
	}
	defer rows.Close()

	var results []Operator
	for rows.Next() {
		var o Operator
		if err := rows.Scan(
			&o.ID, &o.Name, &o.Code, &o.MCC, &o.MNC,
			&o.AdapterConfig, &o.SMDPPlusURL,
			&o.SMDPPlusConfig, &o.SupportedRATTypes,
			&o.HealthStatus, &o.CircuitState, &o.HealthCheckIntervalSec,
			&o.FailoverPolicy, &o.FailoverTimeoutMs,
			&o.CircuitBreakerThreshold, &o.CircuitBreakerRecoverySec,
			&o.SLAUptimeTarget, &o.SLALatencyThresholdMs, &o.State, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan operator: %w", err)
		}
		results = append(results, o)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *OperatorStore) Update(ctx context.Context, id uuid.UUID, p UpdateOperatorParams) (*Operator, error) {
	sets := []string{}
	args := []interface{}{id}
	argIdx := 2

	if p.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *p.Name)
		argIdx++
	}
	if p.AdapterConfig != nil && len(p.AdapterConfig) > 0 {
		sets = append(sets, fmt.Sprintf("adapter_config = $%d", argIdx))
		args = append(args, p.AdapterConfig)
		argIdx++
	}
	if p.SMDPPlusURL != nil {
		sets = append(sets, fmt.Sprintf("sm_dp_plus_url = $%d", argIdx))
		args = append(args, *p.SMDPPlusURL)
		argIdx++
	}
	if p.SMDPPlusConfig != nil && len(p.SMDPPlusConfig) > 0 {
		sets = append(sets, fmt.Sprintf("sm_dp_plus_config = $%d", argIdx))
		args = append(args, p.SMDPPlusConfig)
		argIdx++
	}
	if p.SupportedRATTypes != nil {
		sets = append(sets, fmt.Sprintf("supported_rat_types = $%d", argIdx))
		args = append(args, p.SupportedRATTypes)
		argIdx++
	}
	if p.FailoverPolicy != nil {
		sets = append(sets, fmt.Sprintf("failover_policy = $%d", argIdx))
		args = append(args, *p.FailoverPolicy)
		argIdx++
	}
	if p.FailoverTimeoutMs != nil {
		sets = append(sets, fmt.Sprintf("failover_timeout_ms = $%d", argIdx))
		args = append(args, *p.FailoverTimeoutMs)
		argIdx++
	}
	if p.CircuitBreakerThreshold != nil {
		sets = append(sets, fmt.Sprintf("circuit_breaker_threshold = $%d", argIdx))
		args = append(args, *p.CircuitBreakerThreshold)
		argIdx++
	}
	if p.CircuitBreakerRecoverySec != nil {
		sets = append(sets, fmt.Sprintf("circuit_breaker_recovery_sec = $%d", argIdx))
		args = append(args, *p.CircuitBreakerRecoverySec)
		argIdx++
	}
	if p.HealthCheckIntervalSec != nil {
		sets = append(sets, fmt.Sprintf("health_check_interval_sec = $%d", argIdx))
		args = append(args, *p.HealthCheckIntervalSec)
		argIdx++
	}
	if p.SLAUptimeTarget != nil {
		sets = append(sets, fmt.Sprintf("sla_uptime_target = $%d", argIdx))
		args = append(args, *p.SLAUptimeTarget)
		argIdx++
	}
	if p.State != nil {
		sets = append(sets, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, *p.State)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, id)
	}

	query := fmt.Sprintf(`UPDATE operators SET %s WHERE id = $1 RETURNING %s`,
		strings.Join(sets, ", "), operatorColumns)

	row := s.db.QueryRow(ctx, query, args...)
	o, err := scanOperator(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOperatorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: update operator: %w", err)
	}
	return o, nil
}

func (s *OperatorStore) UpdateHealthStatus(ctx context.Context, id uuid.UUID, status string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE operators SET health_status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("store: update operator health status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}
	return nil
}

// FIX-308: write the circuit-breaker state to operators.circuit_state alongside
// the per-tick log row in operator_health_logs. Pre-fix the column was always
// NULL despite log inserts recording transitions, so UAT-005/020 saw no live
// CB indicator on the operators page. Idempotent — same UPDATE re-runs on every
// tick are cheap (NOOP when state hasn't changed).
func (s *OperatorStore) UpdateCircuitState(ctx context.Context, id uuid.UUID, circuitState string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE operators SET circuit_state = $2 WHERE id = $1`, id, circuitState)
	if err != nil {
		return fmt.Errorf("store: update operator circuit state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}
	return nil
}

func (s *OperatorStore) ListActive(ctx context.Context) ([]Operator, error) {
	rows, err := s.db.Query(ctx, `SELECT `+operatorColumns+` FROM operators WHERE state = 'active' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: list active operators: %w", err)
	}
	defer rows.Close()

	var results []Operator
	for rows.Next() {
		var o Operator
		if err := rows.Scan(
			&o.ID, &o.Name, &o.Code, &o.MCC, &o.MNC,
			&o.AdapterConfig, &o.SMDPPlusURL,
			&o.SMDPPlusConfig, &o.SupportedRATTypes,
			&o.HealthStatus, &o.CircuitState, &o.HealthCheckIntervalSec,
			&o.FailoverPolicy, &o.FailoverTimeoutMs,
			&o.CircuitBreakerThreshold, &o.CircuitBreakerRecoverySec,
			&o.SLAUptimeTarget, &o.SLALatencyThresholdMs, &o.State, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan active operator: %w", err)
		}
		results = append(results, o)
	}
	return results, nil
}

func (s *OperatorStore) CreateGrant(ctx context.Context, tenantID, operatorID uuid.UUID, grantedBy *uuid.UUID, supportedRATTypes []string) (*OperatorGrant, error) {
	ratTypes := supportedRATTypes
	if ratTypes == nil {
		ratTypes = []string{}
	}
	var g OperatorGrant
	err := s.db.QueryRow(ctx, `
		INSERT INTO operator_grants (tenant_id, operator_id, granted_by, supported_rat_types)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, supported_rat_types, granted_at, granted_by
	`, tenantID, operatorID, grantedBy, ratTypes).
		Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.SupportedRATTypes, &g.GrantedAt, &g.GrantedBy)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrGrantExists
		}
		return nil, fmt.Errorf("store: create operator grant: %w", err)
	}
	return &g, nil
}

func (s *OperatorStore) ListGrants(ctx context.Context, tenantID uuid.UUID) ([]OperatorGrant, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, supported_rat_types, granted_at, granted_by
		FROM operator_grants
		WHERE tenant_id = $1
		ORDER BY granted_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: list operator grants: %w", err)
	}
	defer rows.Close()

	var results []OperatorGrant
	for rows.Next() {
		var g OperatorGrant
		if err := rows.Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.SupportedRATTypes, &g.GrantedAt, &g.GrantedBy); err != nil {
			return nil, fmt.Errorf("store: scan operator grant: %w", err)
		}
		results = append(results, g)
	}
	return results, nil
}

func (s *OperatorStore) GetGrantByID(ctx context.Context, id uuid.UUID) (*OperatorGrant, error) {
	var g OperatorGrant
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, supported_rat_types, granted_at, granted_by
		FROM operator_grants WHERE id = $1
	`, id).Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.SupportedRATTypes, &g.GrantedAt, &g.GrantedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGrantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get operator grant: %w", err)
	}
	return &g, nil
}

func (s *OperatorStore) GetGrantByTenantOperator(ctx context.Context, tenantID, operatorID uuid.UUID) (*OperatorGrant, error) {
	var g OperatorGrant
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, supported_rat_types, granted_at, granted_by
		FROM operator_grants WHERE tenant_id = $1 AND operator_id = $2
	`, tenantID, operatorID).Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.SupportedRATTypes, &g.GrantedAt, &g.GrantedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGrantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get grant by tenant+operator: %w", err)
	}
	return &g, nil
}

func (s *OperatorStore) DeleteGrant(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM operator_grants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: delete operator grant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrGrantNotFound
	}
	return nil
}

func (s *OperatorStore) InsertHealthLog(ctx context.Context, operatorID uuid.UUID, status string, latencyMs *int, errorMsg *string, circuitState string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO operator_health_logs (operator_id, status, latency_ms, error_message, circuit_state)
		VALUES ($1, $2, $3, $4, $5)
	`, operatorID, status, latencyMs, errorMsg, circuitState)
	if err != nil {
		return fmt.Errorf("store: insert health log: %w", err)
	}
	return nil
}

func (s *OperatorStore) GetLatestHealth(ctx context.Context, operatorID uuid.UUID) (*OperatorHealthLog, error) {
	var h OperatorHealthLog
	err := s.db.QueryRow(ctx, `
		SELECT id, operator_id, checked_at, status, latency_ms, error_message, circuit_state
		FROM operator_health_logs
		WHERE operator_id = $1
		ORDER BY checked_at DESC
		LIMIT 1
	`, operatorID).Scan(&h.ID, &h.OperatorID, &h.CheckedAt, &h.Status, &h.LatencyMs, &h.ErrorMessage, &h.CircuitState)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get latest health: %w", err)
	}
	return &h, nil
}

func (s *OperatorStore) GetHealthLogs(ctx context.Context, operatorID uuid.UUID, limit int) ([]OperatorHealthLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, operator_id, checked_at, status, latency_ms, error_message, circuit_state
		FROM operator_health_logs
		WHERE operator_id = $1
		ORDER BY checked_at DESC
		LIMIT $2
	`, operatorID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: get health logs: %w", err)
	}
	defer rows.Close()

	var results []OperatorHealthLog
	for rows.Next() {
		var h OperatorHealthLog
		if err := rows.Scan(&h.ID, &h.OperatorID, &h.CheckedAt, &h.Status, &h.LatencyMs, &h.ErrorMessage, &h.CircuitState); err != nil {
			return nil, fmt.Errorf("store: scan health log: %w", err)
		}
		results = append(results, h)
	}
	return results, nil
}

func (s *OperatorStore) CountFailures24h(ctx context.Context, operatorID uuid.UUID) (int, int, error) {
	var total, failures int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status != 'healthy')
		FROM operator_health_logs
		WHERE operator_id = $1 AND checked_at > NOW() - INTERVAL '24 hours'
	`, operatorID).Scan(&total, &failures)
	if err != nil {
		return 0, 0, fmt.Errorf("store: count failures 24h: %w", err)
	}
	return total, failures, nil
}

type SLAAggregate struct {
	UptimePct     float64
	LatencyP95Ms  int
	IncidentCount int
	MTTRSec       int
}

func (s *OperatorStore) AggregateHealthForSLA(ctx context.Context, operatorID uuid.UUID, from, to time.Time) (*SLAAggregate, error) {
	var agg SLAAggregate

	err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(100.0 * SUM(CASE WHEN status = 'healthy' THEN 1 ELSE 0 END)::numeric / NULLIF(COUNT(*), 0), 0) AS uptime_pct,
			COALESCE(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms), 0)::INTEGER AS latency_p95,
			COALESCE(SUM(CASE WHEN status = 'down' THEN 1 ELSE 0 END), 0)::INTEGER AS incident_count
		FROM operator_health_logs
		WHERE operator_id = $1 AND checked_at >= $2 AND checked_at < $3
	`, operatorID, from, to).Scan(&agg.UptimePct, &agg.LatencyP95Ms, &agg.IncidentCount)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("store: aggregate health for sla: %w", err)
	}

	if agg.IncidentCount > 0 {
		var mttrSec *float64
		err = s.db.QueryRow(ctx, `
			WITH incidents AS (
				SELECT
					checked_at,
					status,
					LAG(status) OVER (ORDER BY checked_at) AS prev_status
				FROM operator_health_logs
				WHERE operator_id = $1 AND checked_at >= $2 AND checked_at < $3
			),
			incident_starts AS (
				SELECT checked_at AS down_at
				FROM incidents
				WHERE status = 'down' AND (prev_status IS NULL OR prev_status != 'down')
			),
			incident_recoveries AS (
				SELECT checked_at AS up_at
				FROM incidents
				WHERE status != 'down' AND prev_status = 'down'
			),
			paired AS (
				SELECT
					d.down_at,
					MIN(r.up_at) AS up_at
				FROM incident_starts d
				LEFT JOIN incident_recoveries r ON r.up_at > d.down_at
				GROUP BY d.down_at
			)
			SELECT AVG(EXTRACT(EPOCH FROM (up_at - down_at)))
			FROM paired
			WHERE up_at IS NOT NULL
		`, operatorID, from, to).Scan(&mttrSec)
		if err == nil && mttrSec != nil {
			agg.MTTRSec = int(*mttrSec)
		}
	}

	return &agg, nil
}

func (s *OperatorStore) LatestHealthByOperator(ctx context.Context) (map[uuid.UUID]time.Time, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (operator_id) operator_id, checked_at
		FROM operator_health_logs
		ORDER BY operator_id, checked_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: latest health by operator: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]time.Time)
	for rows.Next() {
		var opID uuid.UUID
		var checkedAt time.Time
		if err := rows.Scan(&opID, &checkedAt); err != nil {
			return nil, fmt.Errorf("store: scan latest health: %w", err)
		}
		result[opID] = checkedAt
	}
	return result, nil
}

func (s *OperatorStore) LatestHealthWithLatencyByOperator(ctx context.Context) (map[uuid.UUID]OperatorHealthSnapshot, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (operator_id) operator_id, checked_at, status, latency_ms, circuit_state
		FROM operator_health_logs
		ORDER BY operator_id, checked_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: latest health with latency by operator: %w", err)
	}
	defer rows.Close()

	result := make(map[uuid.UUID]OperatorHealthSnapshot)
	for rows.Next() {
		var snap OperatorHealthSnapshot
		var latency sql.NullInt64
		if err := rows.Scan(&snap.OperatorID, &snap.CheckedAt, &snap.Status, &latency, &snap.CircuitState); err != nil {
			return nil, fmt.Errorf("store: scan latest health with latency: %w", err)
		}
		if latency.Valid {
			v := int(latency.Int64)
			snap.LatencyMs = &v
		}
		result[snap.OperatorID] = snap
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: rows error latest health with latency: %w", err)
	}
	return result, nil
}

func (s *OperatorStore) GetLatencyTrend(ctx context.Context, operatorID uuid.UUID, since time.Duration, bucket time.Duration) ([]float64, error) {
	if bucket <= 0 {
		return nil, fmt.Errorf("store: bucket duration must be positive")
	}
	n := int(since / bucket)
	if n <= 0 {
		return nil, fmt.Errorf("store: since must be greater than bucket")
	}

	bucketSec := int64(bucket.Seconds())
	sinceSec := int64(since.Seconds())

	rows, err := s.db.Query(ctx, `
		SELECT
			time_bucket($1::interval, checked_at) AS bin,
			ROUND(AVG(latency_ms)::numeric, 1)::float8 AS avg_latency
		FROM operator_health_logs
		WHERE operator_id = $2
		  AND checked_at > NOW() - ($3 * INTERVAL '1 second')
		  AND latency_ms IS NOT NULL
		GROUP BY bin
		ORDER BY bin
	`, fmt.Sprintf("%d seconds", bucketSec), operatorID, sinceSec)
	if err != nil {
		return nil, fmt.Errorf("store: get latency trend: %w", err)
	}
	defer rows.Close()

	type bucketRow struct {
		bin   time.Time
		avgMs float64
	}
	var populated []bucketRow
	for rows.Next() {
		var br bucketRow
		if err := rows.Scan(&br.bin, &br.avgMs); err != nil {
			return nil, fmt.Errorf("store: scan latency trend row: %w", err)
		}
		populated = append(populated, br)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: rows error latency trend: %w", err)
	}

	now := time.Now().UTC()
	origin := now.Add(-since).Truncate(bucket)

	result := make([]float64, n)
	for _, br := range populated {
		idx := int(br.bin.Sub(origin) / bucket)
		if idx >= 0 && idx < n {
			result[idx] = math.Round(br.avgMs*10) / 10
		}
	}
	return result, nil
}

func (s *OperatorStore) ListGrantsWithOperators(ctx context.Context, tenantID uuid.UUID) ([]GrantWithOperator, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g.id, g.tenant_id, g.operator_id, g.enabled, g.sor_priority, g.cost_per_mb, g.region,
			g.supported_rat_types, g.granted_at, g.granted_by,
			o.name, o.code, o.mcc, o.mnc, o.supported_rat_types, o.health_status, o.state,
			o.sla_uptime_target, o.updated_at
		FROM operator_grants g
		LEFT JOIN operators o ON o.id = g.operator_id
		WHERE g.tenant_id = $1 AND g.enabled = true AND (o.state = 'active' OR o.state IS NULL)
		ORDER BY g.sor_priority ASC, g.cost_per_mb ASC NULLS LAST
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: list grants with operators: %w", err)
	}
	defer rows.Close()

	var results []GrantWithOperator
	for rows.Next() {
		var gw GrantWithOperator
		var opName, opCode, opMCC, opMNC, opHealth, opState *string
		var opRATTypes []string
		var opSLATarget *float64
		var opUpdatedAt *time.Time
		if err := rows.Scan(
			&gw.ID, &gw.TenantID, &gw.OperatorID, &gw.Enabled,
			&gw.SoRPriority, &gw.CostPerMB, &gw.Region,
			&gw.SupportedRATTypes, &gw.GrantedAt, &gw.GrantedBy,
			&opName, &opCode, &opMCC, &opMNC,
			&opRATTypes, &opHealth, &opState,
			&opSLATarget, &opUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan grant with operator: %w", err)
		}
		if opName != nil {
			gw.OperatorName = *opName
		}
		if opCode != nil {
			gw.OperatorCode = *opCode
		}
		if opMCC != nil {
			gw.MCC = *opMCC
		}
		if opMNC != nil {
			gw.MNC = *opMNC
		}
		if opRATTypes != nil {
			gw.OperatorSupportedRATTypes = opRATTypes
		}
		if opHealth != nil {
			gw.HealthStatus = *opHealth
		}
		if opState != nil {
			gw.OperatorState = *opState
		}
		gw.SLATarget = opSLATarget
		if opUpdatedAt != nil {
			gw.OperatorUpdatedAt = *opUpdatedAt
		}
		results = append(results, gw)
	}
	return results, nil
}

func (s *OperatorStore) UpdateGrant(ctx context.Context, id uuid.UUID, p UpdateGrantParams) (*OperatorGrant, error) {
	sets := []string{}
	args := []interface{}{id}
	argIdx := 2

	if p.SoRPriority != nil {
		sets = append(sets, fmt.Sprintf("sor_priority = $%d", argIdx))
		args = append(args, *p.SoRPriority)
		argIdx++
	}
	if p.CostPerMB != nil {
		sets = append(sets, fmt.Sprintf("cost_per_mb = $%d", argIdx))
		args = append(args, *p.CostPerMB)
		argIdx++
	}
	if p.Region != nil {
		sets = append(sets, fmt.Sprintf("region = $%d", argIdx))
		args = append(args, *p.Region)
		argIdx++
	}
	if p.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *p.Enabled)
		argIdx++
	}
	if p.SupportedRATTypes != nil {
		sets = append(sets, fmt.Sprintf("supported_rat_types = $%d", argIdx))
		args = append(args, p.SupportedRATTypes)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetGrantByID(ctx, id)
	}

	query := fmt.Sprintf(`UPDATE operator_grants SET %s WHERE id = $1
		RETURNING id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, supported_rat_types, granted_at, granted_by`,
		strings.Join(sets, ", "))

	var g OperatorGrant
	err := s.db.QueryRow(ctx, query, args...).
		Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.SupportedRATTypes, &g.GrantedAt, &g.GrantedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGrantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: update operator grant: %w", err)
	}
	return &g, nil
}

type Breach struct {
	StartedAt           time.Time `json:"started_at"`
	EndedAt             time.Time `json:"ended_at"`
	DurationSec         int       `json:"duration_sec"`
	Cause               string    `json:"cause"`
	SamplesCount        int       `json:"samples_count"`
	AffectedSessionsEst int64     `json:"affected_sessions_est"`
}

func (s *OperatorStore) BreachesForOperatorMonth(ctx context.Context, operatorID uuid.UUID, year, month int, latencyThresholdMs int) ([]Breach, error) {
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	rows, err := s.db.Query(ctx, `
		WITH breach_samples AS (
			SELECT checked_at, status, latency_ms,
				LAG(checked_at) OVER (ORDER BY checked_at) AS prev_at
			FROM operator_health_logs
			WHERE operator_id = $1
			  AND checked_at >= $2
			  AND checked_at <  $3
			  AND (status = 'down' OR latency_ms > $4)
		),
		runs AS (
			SELECT checked_at, status, latency_ms,
				SUM(CASE WHEN prev_at IS NULL OR checked_at - prev_at > interval '120 seconds'
				         THEN 1 ELSE 0 END) OVER (ORDER BY checked_at) AS run_id
			FROM breach_samples
		),
		rollups AS (
			SELECT run_id,
				MIN(checked_at)                                              AS started_at,
				MAX(checked_at)                                              AS ended_at,
				EXTRACT(EPOCH FROM (MAX(checked_at) - MIN(checked_at)))::int AS duration_sec,
				COUNT(*)                                                     AS samples_count,
				BOOL_AND(status = 'down')                                    AS all_down,
				BOOL_AND(status = 'up' AND latency_ms > $4)                  AS all_latency
			FROM runs
			GROUP BY run_id
		)
		SELECT started_at, ended_at, duration_sec,
			CASE WHEN all_down    THEN 'down'
			     WHEN all_latency THEN 'latency'
			     ELSE 'mixed' END AS cause,
			samples_count
		FROM rollups
		WHERE duration_sec >= 300
		ORDER BY started_at ASC
	`, operatorID, from, to, latencyThresholdMs)
	if err != nil {
		return nil, fmt.Errorf("store: breach query: %w", err)
	}
	defer rows.Close()

	var results []Breach
	for rows.Next() {
		var b Breach
		if err := rows.Scan(&b.StartedAt, &b.EndedAt, &b.DurationSec, &b.Cause, &b.SamplesCount); err != nil {
			return nil, fmt.Errorf("store: scan breach: %w", err)
		}
		results = append(results, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: breach rows: %w", err)
	}
	return results, nil
}

func (s *OperatorStore) UpdateSLATargets(ctx context.Context, operatorID uuid.UUID, uptimeTarget float64, latencyThresholdMs int, actorUserID uuid.UUID) error {
	var before struct {
		UptimeTarget       *float64
		LatencyThresholdMs int
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin sla targets tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx,
		`SELECT sla_uptime_target, sla_latency_threshold_ms FROM operators WHERE id = $1`,
		operatorID,
	).Scan(&before.UptimeTarget, &before.LatencyThresholdMs)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOperatorNotFound
	}
	if err != nil {
		return fmt.Errorf("store: read operator for sla update: %w", err)
	}

	tag, err := tx.Exec(ctx,
		`UPDATE operators SET sla_uptime_target = $2, sla_latency_threshold_ms = $3, updated_at = NOW() WHERE id = $1`,
		operatorID, uptimeTarget, latencyThresholdMs,
	)
	if err != nil {
		return fmt.Errorf("store: update sla targets: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOperatorNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit sla targets: %w", err)
	}

	if s.auditStore == nil {
		return nil
	}

	var tenantID uuid.UUID
	err = s.db.QueryRow(ctx,
		`SELECT tenant_id FROM users WHERE id = $1`, actorUserID,
	).Scan(&tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("store: resolve tenant for audit: %w", err)
	}

	beforeJSON, _ := json.Marshal(before)
	afterData := struct {
		UptimeTarget       float64 `json:"sla_uptime_target"`
		LatencyThresholdMs int     `json:"sla_latency_threshold_ms"`
	}{UptimeTarget: uptimeTarget, LatencyThresholdMs: latencyThresholdMs}
	afterJSON, _ := json.Marshal(afterData)

	entry := &audit.Entry{
		TenantID:   tenantID,
		UserID:     &actorUserID,
		Action:     "operator.updated",
		EntityType: "operator",
		EntityID:   operatorID.String(),
		BeforeData: beforeJSON,
		AfterData:  afterJSON,
		CreatedAt:  time.Now().UTC(),
	}
	if _, err := s.auditStore.CreateWithChain(ctx, entry); err != nil {
		return fmt.Errorf("store: audit sla targets update: %w", err)
	}

	return nil
}
