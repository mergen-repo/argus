package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ComplianceStore struct {
	db *pgxpool.Pool
}

func NewComplianceStore(db *pgxpool.Pool) *ComplianceStore {
	return &ComplianceStore{db: db}
}

type PurgableSIM struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	ICCID    string
	IMSI     string
	MSISDN   *string
}

func (s *ComplianceStore) FindPurgableSIMs(ctx context.Context, batchSize int) ([]PurgableSIM, error) {
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = 100
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, iccid, imsi, msisdn
		FROM sims
		WHERE state = 'terminated' AND purge_at IS NOT NULL AND purge_at < NOW()
		ORDER BY purge_at ASC
		LIMIT $1
	`, batchSize)
	if err != nil {
		return nil, fmt.Errorf("store: find purgable sims: %w", err)
	}
	defer rows.Close()

	var results []PurgableSIM
	for rows.Next() {
		var s PurgableSIM
		if err := rows.Scan(&s.ID, &s.TenantID, &s.ICCID, &s.IMSI, &s.MSISDN); err != nil {
			return nil, fmt.Errorf("store: scan purgable sim: %w", err)
		}
		results = append(results, s)
	}
	return results, nil
}

func (s *ComplianceStore) PurgeSIM(ctx context.Context, simID uuid.UUID, tenantSalt string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin purge tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var iccid, imsi string
	var msisdn *string
	var currentState string
	err = tx.QueryRow(ctx,
		`SELECT state, iccid, imsi, msisdn FROM sims WHERE id = $1 FOR UPDATE`,
		simID,
	).Scan(&currentState, &iccid, &imsi, &msisdn)
	if err != nil {
		return fmt.Errorf("store: lock sim for purge: %w", err)
	}

	if currentState != "terminated" {
		return fmt.Errorf("store: sim not in terminated state (current: %s)", currentState)
	}

	hashedIMSI := hashWithSalt(imsi, tenantSalt)
	hashedICCID := hashWithSalt(iccid, tenantSalt)
	var hashedMSISDN *string
	if msisdn != nil && *msisdn != "" {
		h := hashWithSalt(*msisdn, tenantSalt)
		hashedMSISDN = &h
	}

	_, err = tx.Exec(ctx, `
		UPDATE sims SET
			state = 'purged',
			iccid = $2,
			imsi = $3,
			msisdn = $4,
			metadata = '{}',
			ip_address_id = NULL,
			policy_version_id = NULL,
			esim_profile_id = NULL,
			updated_at = NOW()
		WHERE id = $1
	`, simID, hashedICCID, hashedIMSI, hashedMSISDN)
	if err != nil {
		return fmt.Errorf("store: purge sim data: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by)
		VALUES ($1, 'terminated', 'purged', 'auto-purge: retention period expired', 'system')
	`, simID)
	if err != nil {
		return fmt.Errorf("store: insert purge history: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit purge: %w", err)
	}
	return nil
}

func (s *ComplianceStore) EarlyPurgeSIM(ctx context.Context, tenantID, simID uuid.UUID, tenantSalt string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin early purge tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var iccid, imsi string
	var msisdn *string
	var currentState string
	err = tx.QueryRow(ctx,
		`SELECT state, iccid, imsi, msisdn FROM sims WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		simID, tenantID,
	).Scan(&currentState, &iccid, &imsi, &msisdn)
	if err != nil {
		return fmt.Errorf("store: lock sim for early purge: %w", err)
	}

	if currentState != "terminated" {
		return fmt.Errorf("store: sim must be terminated for erasure (current: %s)", currentState)
	}

	hashedIMSI := hashWithSalt(imsi, tenantSalt)
	hashedICCID := hashWithSalt(iccid, tenantSalt)
	var hashedMSISDN *string
	if msisdn != nil && *msisdn != "" {
		h := hashWithSalt(*msisdn, tenantSalt)
		hashedMSISDN = &h
	}

	_, err = tx.Exec(ctx, `
		UPDATE sims SET
			state = 'purged',
			iccid = $2,
			imsi = $3,
			msisdn = $4,
			metadata = '{}',
			ip_address_id = NULL,
			policy_version_id = NULL,
			esim_profile_id = NULL,
			updated_at = NOW()
		WHERE id = $1
	`, simID, hashedICCID, hashedIMSI, hashedMSISDN)
	if err != nil {
		return fmt.Errorf("store: early purge sim data: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by)
		VALUES ($1, 'terminated', 'purged', 'GDPR/KVKK right to erasure', 'system')
	`, simID)
	if err != nil {
		return fmt.Errorf("store: insert early purge history: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit early purge: %w", err)
	}
	return nil
}

type StateCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

func (s *ComplianceStore) CountSIMsByState(ctx context.Context, tenantID uuid.UUID) ([]StateCount, error) {
	rows, err := s.db.Query(ctx, `
		SELECT state, COUNT(*) FROM sims
		WHERE tenant_id = $1
		GROUP BY state
		ORDER BY COUNT(*) DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: count sims by state: %w", err)
	}
	defer rows.Close()

	var results []StateCount
	for rows.Next() {
		var sc StateCount
		if err := rows.Scan(&sc.State, &sc.Count); err != nil {
			return nil, fmt.Errorf("store: scan state count: %w", err)
		}
		results = append(results, sc)
	}
	return results, nil
}

func (s *ComplianceStore) CountPendingPurges(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM sims
		WHERE tenant_id = $1 AND state = 'terminated' AND purge_at IS NOT NULL
	`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count pending purges: %w", err)
	}
	return count, nil
}

func (s *ComplianceStore) CountOverduePurges(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM sims
		WHERE tenant_id = $1 AND state = 'terminated' AND purge_at IS NOT NULL AND purge_at < NOW()
	`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count overdue purges: %w", err)
	}
	return count, nil
}

type BTKOperatorStats struct {
	OperatorName    string `json:"operator_name"`
	OperatorCode    string `json:"operator_code"`
	ActiveCount     int    `json:"active_count"`
	SuspendedCount  int    `json:"suspended_count"`
	TerminatedCount int    `json:"terminated_count"`
	TotalCount      int    `json:"total_count"`
}

func (s *ComplianceStore) BTKMonthlyStats(ctx context.Context, tenantID uuid.UUID) ([]BTKOperatorStats, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			o.name,
			o.code,
			COUNT(*) FILTER (WHERE s.state = 'active') AS active_count,
			COUNT(*) FILTER (WHERE s.state = 'suspended') AS suspended_count,
			COUNT(*) FILTER (WHERE s.state = 'terminated') AS terminated_count,
			COUNT(*) AS total_count
		FROM sims s
		JOIN operators o ON s.operator_id = o.id
		WHERE s.tenant_id = $1 AND s.state != 'purged'
		GROUP BY o.id, o.name, o.code
		ORDER BY o.name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: btk monthly stats: %w", err)
	}
	defer rows.Close()

	var results []BTKOperatorStats
	for rows.Next() {
		var bs BTKOperatorStats
		if err := rows.Scan(&bs.OperatorName, &bs.OperatorCode,
			&bs.ActiveCount, &bs.SuspendedCount, &bs.TerminatedCount, &bs.TotalCount); err != nil {
			return nil, fmt.Errorf("store: scan btk stats: %w", err)
		}
		results = append(results, bs)
	}
	return results, nil
}

type DataSubjectExport struct {
	SIM          json.RawMessage `json:"sim"`
	StateHistory json.RawMessage `json:"state_history"`
	AuditLogs    json.RawMessage `json:"audit_logs"`
}

func (s *ComplianceStore) ExportSIMData(ctx context.Context, tenantID, simID uuid.UUID) (*DataSubjectExport, error) {
	var simJSON json.RawMessage
	err := s.db.QueryRow(ctx, `
		SELECT row_to_json(sub) FROM (
			SELECT id, tenant_id, operator_id, iccid, imsi, msisdn, sim_type, state,
				rat_type, metadata, activated_at, suspended_at, terminated_at, purge_at,
				created_at, updated_at
			FROM sims WHERE id = $1 AND tenant_id = $2
		) sub
	`, simID, tenantID).Scan(&simJSON)
	if err != nil {
		return nil, fmt.Errorf("store: export sim data: %w", err)
	}

	var historyJSON json.RawMessage
	err = s.db.QueryRow(ctx, `
		SELECT COALESCE(json_agg(sub ORDER BY sub.created_at), '[]'::json) FROM (
			SELECT id, from_state, to_state, reason, triggered_by, user_id, created_at
			FROM sim_state_history WHERE sim_id = $1
		) sub
	`, simID).Scan(&historyJSON)
	if err != nil {
		return nil, fmt.Errorf("store: export sim history: %w", err)
	}

	var auditJSON json.RawMessage
	err = s.db.QueryRow(ctx, `
		SELECT COALESCE(json_agg(sub ORDER BY sub.created_at), '[]'::json) FROM (
			SELECT id, action, entity_type, entity_id, before_data, after_data, diff, created_at
			FROM audit_logs WHERE tenant_id = $1 AND entity_type = 'sim' AND entity_id = $2::text
		) sub
	`, tenantID, simID.String()).Scan(&auditJSON)
	if err != nil {
		return nil, fmt.Errorf("store: export sim audit logs: %w", err)
	}

	return &DataSubjectExport{
		SIM:          simJSON,
		StateHistory: historyJSON,
		AuditLogs:    auditJSON,
	}, nil
}

func (s *ComplianceStore) GetRetentionDays(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var days int
	err := s.db.QueryRow(ctx, `
		SELECT purge_retention_days FROM tenants WHERE id = $1
	`, tenantID).Scan(&days)
	if err != nil {
		return 90, fmt.Errorf("store: get retention days: %w", err)
	}
	if days < 30 {
		days = 90
	}
	return days, nil
}

func (s *ComplianceStore) UpdateRetentionDays(ctx context.Context, tenantID uuid.UUID, days int) error {
	if days < 30 || days > 365 {
		return fmt.Errorf("store: retention days must be between 30 and 365")
	}

	_, err := s.db.Exec(ctx, `
		UPDATE tenants SET purge_retention_days = $2, updated_at = NOW()
		WHERE id = $1
	`, tenantID, days)
	if err != nil {
		return fmt.Errorf("store: update retention days: %w", err)
	}
	return nil
}

func (s *ComplianceStore) ListActiveTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, domain, contact_email, contact_phone, max_sims, max_apns, max_users,
			purge_retention_days, settings, state, created_at, updated_at, created_by, updated_by
		FROM tenants
		WHERE state = 'active'
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list active tenants: %w", err)
	}
	defer rows.Close()

	var results []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Domain, &t.ContactEmail, &t.ContactPhone,
			&t.MaxSims, &t.MaxApns, &t.MaxUsers, &t.PurgeRetentionDays,
			&t.Settings, &t.State, &t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy); err != nil {
			return nil, fmt.Errorf("store: scan active tenant: %w", err)
		}
		results = append(results, t)
	}
	return results, nil
}

func (s *ComplianceStore) PseudonymizeAuditLogs(ctx context.Context, tenantID uuid.UUID, retentionDays int, tenantSalt string) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	rows, err := s.db.Query(ctx, `
		SELECT id, before_data, after_data, diff
		FROM audit_logs
		WHERE tenant_id = $1 AND created_at < $2
		  AND NOT (
			COALESCE(before_data::text, '{}') LIKE '%pseudonymized%'
			AND COALESCE(after_data::text, '{}') LIKE '%pseudonymized%'
		  )
	`, tenantID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("store: find logs to pseudonymize: %w", err)
	}
	defer rows.Close()

	sensitiveFields := []string{"imsi", "msisdn", "iccid", "email", "ip_address", "user_agent", "phone"}
	count := 0

	for rows.Next() {
		var id int64
		var beforeData, afterData, diff json.RawMessage

		if err := rows.Scan(&id, &beforeData, &afterData, &diff); err != nil {
			return count, fmt.Errorf("store: scan log for pseudonymize: %w", err)
		}

		beforeData = anonymizeJSONWithSalt(beforeData, sensitiveFields, tenantSalt)
		afterData = anonymizeJSONWithSalt(afterData, sensitiveFields, tenantSalt)
		diff = anonymizeJSONWithSalt(diff, sensitiveFields, tenantSalt)

		_, err := s.db.Exec(ctx, `
			UPDATE audit_logs SET before_data = $1, after_data = $2, diff = $3
			WHERE id = $4
		`, beforeData, afterData, diff, id)
		if err != nil {
			return count, fmt.Errorf("store: pseudonymize log id=%d: %w", id, err)
		}
		count++
	}
	return count, nil
}

func hashWithSalt(value, salt string) string {
	h := sha256.Sum256([]byte(salt + "|" + value))
	return hex.EncodeToString(h[:])
}

func anonymizeJSONWithSalt(data json.RawMessage, fields []string, salt string) json.RawMessage {
	if len(data) == 0 {
		return data
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return data
	}

	changed := false
	for _, field := range fields {
		if val, ok := m[field]; ok {
			if strVal, ok := val.(string); ok && strVal != "" {
				m[field] = hashWithSalt(strVal, salt)
				changed = true
			}
		}
	}

	if !changed {
		return data
	}

	result, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return result
}
