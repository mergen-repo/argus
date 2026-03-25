package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	ID                       uuid.UUID       `json:"id"`
	Name                     string          `json:"name"`
	Code                     string          `json:"code"`
	MCC                      string          `json:"mcc"`
	MNC                      string          `json:"mnc"`
	AdapterType              string          `json:"adapter_type"`
	AdapterConfig            json.RawMessage `json:"adapter_config"`
	SMDPPlusURL              *string         `json:"sm_dp_plus_url"`
	SMDPPlusConfig           json.RawMessage `json:"sm_dp_plus_config"`
	SupportedRATTypes        []string        `json:"supported_rat_types"`
	HealthStatus             string          `json:"health_status"`
	HealthCheckIntervalSec   int             `json:"health_check_interval_sec"`
	FailoverPolicy           string          `json:"failover_policy"`
	FailoverTimeoutMs        int             `json:"failover_timeout_ms"`
	CircuitBreakerThreshold  int             `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverySec int            `json:"circuit_breaker_recovery_sec"`
	SLAUptimeTarget          *float64        `json:"sla_uptime_target"`
	State                    string          `json:"state"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
}

type OperatorGrant struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	OperatorID uuid.UUID  `json:"operator_id"`
	Enabled    bool       `json:"enabled"`
	SoRPriority int       `json:"sor_priority"`
	CostPerMB  *float64   `json:"cost_per_mb"`
	Region     *string    `json:"region"`
	GrantedAt  time.Time  `json:"granted_at"`
	GrantedBy  *uuid.UUID `json:"granted_by"`
}

type GrantWithOperator struct {
	OperatorGrant
	OperatorName      string   `json:"operator_name"`
	OperatorCode      string   `json:"operator_code"`
	MCC               string   `json:"mcc"`
	MNC               string   `json:"mnc"`
	SupportedRATTypes []string `json:"supported_rat_types"`
	HealthStatus      string   `json:"health_status"`
	OperatorState     string   `json:"operator_state"`
}

type UpdateGrantParams struct {
	SoRPriority *int
	CostPerMB   *float64
	Region      *string
	Enabled     *bool
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

type CreateOperatorParams struct {
	Name                      string
	Code                      string
	MCC                       string
	MNC                       string
	AdapterType               string
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
	db *pgxpool.Pool
}

func NewOperatorStore(db *pgxpool.Pool) *OperatorStore {
	return &OperatorStore{db: db}
}

var operatorColumns = `id, name, code, mcc, mnc, adapter_type, adapter_config, sm_dp_plus_url,
	sm_dp_plus_config, supported_rat_types, health_status, health_check_interval_sec,
	failover_policy, failover_timeout_ms, circuit_breaker_threshold, circuit_breaker_recovery_sec,
	sla_uptime_target, state, created_at, updated_at`

func scanOperator(row pgx.Row) (*Operator, error) {
	var o Operator
	err := row.Scan(
		&o.ID, &o.Name, &o.Code, &o.MCC, &o.MNC,
		&o.AdapterType, &o.AdapterConfig, &o.SMDPPlusURL,
		&o.SMDPPlusConfig, &o.SupportedRATTypes,
		&o.HealthStatus, &o.HealthCheckIntervalSec,
		&o.FailoverPolicy, &o.FailoverTimeoutMs,
		&o.CircuitBreakerThreshold, &o.CircuitBreakerRecoverySec,
		&o.SLAUptimeTarget, &o.State, &o.CreatedAt, &o.UpdatedAt,
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
		INSERT INTO operators (name, code, mcc, mnc, adapter_type, adapter_config,
			sm_dp_plus_url, sm_dp_plus_config, supported_rat_types,
			failover_policy, failover_timeout_ms,
			circuit_breaker_threshold, circuit_breaker_recovery_sec,
			health_check_interval_sec, sla_uptime_target)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+operatorColumns,
		p.Name, p.Code, p.MCC, p.MNC, p.AdapterType, adapterConfig,
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
			&o.AdapterType, &o.AdapterConfig, &o.SMDPPlusURL,
			&o.SMDPPlusConfig, &o.SupportedRATTypes,
			&o.HealthStatus, &o.HealthCheckIntervalSec,
			&o.FailoverPolicy, &o.FailoverTimeoutMs,
			&o.CircuitBreakerThreshold, &o.CircuitBreakerRecoverySec,
			&o.SLAUptimeTarget, &o.State, &o.CreatedAt, &o.UpdatedAt,
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
			&o.AdapterType, &o.AdapterConfig, &o.SMDPPlusURL,
			&o.SMDPPlusConfig, &o.SupportedRATTypes,
			&o.HealthStatus, &o.HealthCheckIntervalSec,
			&o.FailoverPolicy, &o.FailoverTimeoutMs,
			&o.CircuitBreakerThreshold, &o.CircuitBreakerRecoverySec,
			&o.SLAUptimeTarget, &o.State, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan active operator: %w", err)
		}
		results = append(results, o)
	}
	return results, nil
}

func (s *OperatorStore) CreateGrant(ctx context.Context, tenantID, operatorID uuid.UUID, grantedBy *uuid.UUID) (*OperatorGrant, error) {
	var g OperatorGrant
	err := s.db.QueryRow(ctx, `
		INSERT INTO operator_grants (tenant_id, operator_id, granted_by)
		VALUES ($1, $2, $3)
		RETURNING id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, granted_at, granted_by
	`, tenantID, operatorID, grantedBy).
		Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.GrantedAt, &g.GrantedBy)
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
		SELECT id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, granted_at, granted_by
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
		if err := rows.Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.GrantedAt, &g.GrantedBy); err != nil {
			return nil, fmt.Errorf("store: scan operator grant: %w", err)
		}
		results = append(results, g)
	}
	return results, nil
}

func (s *OperatorStore) GetGrantByID(ctx context.Context, id uuid.UUID) (*OperatorGrant, error) {
	var g OperatorGrant
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, granted_at, granted_by
		FROM operator_grants WHERE id = $1
	`, id).Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.GrantedAt, &g.GrantedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGrantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get operator grant: %w", err)
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

func (s *OperatorStore) ListGrantsWithOperators(ctx context.Context, tenantID uuid.UUID) ([]GrantWithOperator, error) {
	rows, err := s.db.Query(ctx, `
		SELECT g.id, g.tenant_id, g.operator_id, g.enabled, g.sor_priority, g.cost_per_mb, g.region,
			g.granted_at, g.granted_by,
			o.name, o.code, o.mcc, o.mnc, o.supported_rat_types, o.health_status, o.state
		FROM operator_grants g
		JOIN operators o ON o.id = g.operator_id
		WHERE g.tenant_id = $1 AND g.enabled = true AND o.state = 'active'
		ORDER BY g.sor_priority ASC, g.cost_per_mb ASC NULLS LAST
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: list grants with operators: %w", err)
	}
	defer rows.Close()

	var results []GrantWithOperator
	for rows.Next() {
		var gw GrantWithOperator
		if err := rows.Scan(
			&gw.ID, &gw.TenantID, &gw.OperatorID, &gw.Enabled,
			&gw.SoRPriority, &gw.CostPerMB, &gw.Region,
			&gw.GrantedAt, &gw.GrantedBy,
			&gw.OperatorName, &gw.OperatorCode, &gw.MCC, &gw.MNC,
			&gw.SupportedRATTypes, &gw.HealthStatus, &gw.OperatorState,
		); err != nil {
			return nil, fmt.Errorf("store: scan grant with operator: %w", err)
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

	if len(sets) == 0 {
		return s.GetGrantByID(ctx, id)
	}

	query := fmt.Sprintf(`UPDATE operator_grants SET %s WHERE id = $1
		RETURNING id, tenant_id, operator_id, enabled, sor_priority, cost_per_mb, region, granted_at, granted_by`,
		strings.Join(sets, ", "))

	var g OperatorGrant
	err := s.db.QueryRow(ctx, query, args...).
		Scan(&g.ID, &g.TenantID, &g.OperatorID, &g.Enabled, &g.SoRPriority, &g.CostPerMB, &g.Region, &g.GrantedAt, &g.GrantedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGrantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: update operator grant: %w", err)
	}
	return &g, nil
}
