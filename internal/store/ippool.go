package store

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrIPPoolNotFound     = errors.New("store: ip pool not found")
	ErrPoolExhausted      = errors.New("store: ip pool exhausted")
	ErrIPAlreadyAllocated = errors.New("store: ip already allocated")
	ErrIPNotFound         = errors.New("store: ip address not found")
)

type IPPool struct {
	ID                     uuid.UUID `json:"id"`
	TenantID               uuid.UUID `json:"tenant_id"`
	APNID                  uuid.UUID `json:"apn_id"`
	Name                   string    `json:"name"`
	CIDRv4                 *string   `json:"cidr_v4"`
	CIDRv6                 *string   `json:"cidr_v6"`
	TotalAddresses         int       `json:"total_addresses"`
	UsedAddresses          int       `json:"used_addresses"`
	AlertThresholdWarning  int       `json:"alert_threshold_warning"`
	AlertThresholdCritical int       `json:"alert_threshold_critical"`
	ReclaimGracePeriodDays int       `json:"reclaim_grace_period_days"`
	State                  string    `json:"state"`
	CreatedAt              time.Time `json:"created_at"`
}

// RecountUsedAddresses deterministically rewrites ip_pools.used_addresses
// from the real ip_addresses rows, scoped to a single tenant (or ALL pools
// when tenantID is uuid.Nil). AllocateIP / ReleaseIP / ReserveStaticIP
// maintain the counter in-transaction, but app-level bugs (or a legacy
// seed write that back-dates used_addresses without the matching
// ip_addresses rows) can leave drift. This method is the reconciliation
// knob behind the STORY-092 admin sweep — and is safe to run on live data.
//
// Returns the number of pools updated (pgx.CommandTag.RowsAffected).
// A pool with zero allocated/reserved addresses is rewritten to 0 by the
// LEFT JOIN fallback, not skipped — so re-running is idempotent and will
// converge used_addresses = COUNT(ip_addresses WHERE state IN
// ('allocated','reserved')).
func (s *IPPoolStore) RecountUsedAddresses(ctx context.Context, tenantID uuid.UUID) (int64, error) {
	var query string
	var args []interface{}
	if tenantID == uuid.Nil {
		query = `
			UPDATE ip_pools p
			SET used_addresses = COALESCE(sub.cnt, 0)
			FROM (
				SELECT p2.id AS pool_id,
				       (SELECT COUNT(*) FROM ip_addresses ipa
				        WHERE ipa.pool_id = p2.id
				          AND ipa.state IN ('allocated', 'reserved')) AS cnt
				FROM ip_pools p2
			) sub
			WHERE p.id = sub.pool_id
		`
	} else {
		query = `
			UPDATE ip_pools p
			SET used_addresses = COALESCE(sub.cnt, 0)
			FROM (
				SELECT p2.id AS pool_id,
				       (SELECT COUNT(*) FROM ip_addresses ipa
				        WHERE ipa.pool_id = p2.id
				          AND ipa.state IN ('allocated', 'reserved')) AS cnt
				FROM ip_pools p2
				WHERE p2.tenant_id = $1
			) sub
			WHERE p.id = sub.pool_id
		`
		args = append(args, tenantID)
	}
	tag, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("store: recount used_addresses: %w", err)
	}
	return tag.RowsAffected(), nil
}

// TenantPoolUsage returns the tenant-wide IP pool utilization percentage.
// (SUM(used_addresses) / SUM(total_addresses)) * 100. Returns 0 when no
// pools exist or total_addresses sums to zero. Used by the dashboard KPI.
func (s *IPPoolStore) TenantPoolUsage(ctx context.Context, tenantID uuid.UUID) (float64, error) {
	var used, total int64
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(used_addresses),0), COALESCE(SUM(total_addresses),0)
		FROM ip_pools
		WHERE tenant_id = $1 AND state = 'active'
	`, tenantID).Scan(&used, &total)
	if err != nil {
		return 0, fmt.Errorf("store: tenant pool usage: %w", err)
	}
	if total == 0 {
		return 0, nil
	}
	return float64(used) / float64(total) * 100, nil
}

// TopPoolSummary holds the identity and utilization of a single IP pool.
type TopPoolSummary struct {
	ID       uuid.UUID
	Name     string
	UsagePct float64
}

// TopPoolUsage returns the single most-utilized active IP pool for the tenant.
// Returns (nil, nil) when the tenant has zero active pools with total_addresses > 0.
func (s *IPPoolStore) TopPoolUsage(ctx context.Context, tenantID uuid.UUID) (*TopPoolSummary, error) {
	var summary TopPoolSummary
	err := s.db.QueryRow(ctx, `
		SELECT id, name,
		       (used_addresses::float / total_addresses::float) * 100 AS pct
		FROM ip_pools
		WHERE tenant_id = $1 AND state = 'active' AND total_addresses > 0
		ORDER BY pct DESC NULLS LAST
		LIMIT 1
	`, tenantID).Scan(&summary.ID, &summary.Name, &summary.UsagePct)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: top pool usage: %w", err)
	}
	return &summary, nil
}

type IPAddress struct {
	ID             uuid.UUID  `json:"id"`
	PoolID         uuid.UUID  `json:"pool_id"`
	AddressV4      *string    `json:"address_v4"`
	AddressV6      *string    `json:"address_v6"`
	AllocationType string     `json:"allocation_type"`
	SimID          *uuid.UUID `json:"sim_id"`
	State          string     `json:"state"`
	AllocatedAt    *time.Time `json:"allocated_at"`
	ReclaimAt      *time.Time `json:"reclaim_at"`
	LastSeenAt     *time.Time `json:"last_seen_at"`
	SimICCID       *string    `json:"sim_iccid,omitempty"`
	SimIMSI        *string    `json:"sim_imsi,omitempty"`
	SimMSISDN      *string    `json:"sim_msisdn,omitempty"`
}

type ExpiredIPAddress struct {
	ID            uuid.UUID  `json:"id"`
	PoolID        uuid.UUID  `json:"pool_id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	AddressV4     *string    `json:"address_v4"`
	AddressV6     *string    `json:"address_v6"`
	PreviousSimID *uuid.UUID `json:"previous_sim_id"`
	ReclaimAt     time.Time  `json:"reclaim_at"`
}

type GraceExpiredIPAddress struct {
	ID        uuid.UUID `json:"id"`
	PoolID    uuid.UUID `json:"pool_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	AddressV4 *string   `json:"address_v4"`
	AddressV6 *string   `json:"address_v6"`
}

type CreateIPPoolParams struct {
	APNID                  uuid.UUID
	Name                   string
	CIDRv4                 *string
	CIDRv6                 *string
	AlertThresholdWarning  *int
	AlertThresholdCritical *int
	ReclaimGracePeriodDays *int
}

type UpdateIPPoolParams struct {
	Name                   *string
	AlertThresholdWarning  *int
	AlertThresholdCritical *int
	ReclaimGracePeriodDays *int
	State                  *string
}

type IPPoolStore struct {
	db *pgxpool.Pool
}

func NewIPPoolStore(db *pgxpool.Pool) *IPPoolStore {
	return &IPPoolStore{db: db}
}

var ippoolColumns = `id, tenant_id, apn_id, name, cidr_v4::text, cidr_v6::text,
	total_addresses, used_addresses, alert_threshold_warning, alert_threshold_critical,
	reclaim_grace_period_days, state, created_at`

func scanIPPool(row pgx.Row) (*IPPool, error) {
	var p IPPool
	err := row.Scan(
		&p.ID, &p.TenantID, &p.APNID, &p.Name, &p.CIDRv4, &p.CIDRv6,
		&p.TotalAddresses, &p.UsedAddresses, &p.AlertThresholdWarning,
		&p.AlertThresholdCritical, &p.ReclaimGracePeriodDays, &p.State, &p.CreatedAt,
	)
	return &p, err
}

// ipAddressColumns is used for mutation/single-row lookups (no SIM JOIN).
// Must match scanIPAddress exactly (10 columns).
var ipAddressColumns = `id, pool_id, address_v4::text, address_v6::text,
	allocation_type, sim_id, state, allocated_at, reclaim_at, last_seen_at`

// ipAddressColumnsJoined is used in ListAddresses with a LEFT JOIN on sims.
// Returns 13 columns; scanned inline in ListAddresses (NOT via scanIPAddress).
const ipAddressColumnsJoined = `
	ip.id, ip.pool_id, ip.address_v4::text, ip.address_v6::text,
	ip.allocation_type, ip.sim_id, ip.state, ip.allocated_at, ip.reclaim_at,
	ip.last_seen_at,
	s.iccid, s.imsi, s.msisdn`

func scanIPAddress(row pgx.Row) (*IPAddress, error) {
	var a IPAddress
	err := row.Scan(
		&a.ID, &a.PoolID, &a.AddressV4, &a.AddressV6,
		&a.AllocationType, &a.SimID, &a.State, &a.AllocatedAt, &a.ReclaimAt,
		&a.LastSeenAt,
	)
	return &a, err
}

func (s *IPPoolStore) Create(ctx context.Context, tenantID uuid.UUID, p CreateIPPoolParams) (*IPPool, error) {
	alertWarning := 80
	if p.AlertThresholdWarning != nil {
		alertWarning = *p.AlertThresholdWarning
	}
	alertCritical := 90
	if p.AlertThresholdCritical != nil {
		alertCritical = *p.AlertThresholdCritical
	}
	reclaimDays := 7
	if p.ReclaimGracePeriodDays != nil {
		reclaimDays = *p.ReclaimGracePeriodDays
	}

	var ipv4Addrs []string
	var ipv6Addrs []string
	totalAddresses := 0

	if p.CIDRv4 != nil && *p.CIDRv4 != "" {
		addrs, err := GenerateIPv4Addresses(*p.CIDRv4)
		if err != nil {
			return nil, fmt.Errorf("store: generate ipv4 addresses: %w", err)
		}
		ipv4Addrs = addrs
		totalAddresses += len(addrs)
	}

	if p.CIDRv6 != nil && *p.CIDRv6 != "" {
		addrs, total, err := GenerateIPv6Addresses(*p.CIDRv6)
		if err != nil {
			return nil, fmt.Errorf("store: generate ipv6 addresses: %w", err)
		}
		ipv6Addrs = addrs
		if total > len(addrs) {
			totalAddresses += total
		} else {
			totalAddresses += len(addrs)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for create pool: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO ip_pools (tenant_id, apn_id, name, cidr_v4, cidr_v6,
			total_addresses, alert_threshold_warning, alert_threshold_critical,
			reclaim_grace_period_days)
		VALUES ($1, $2, $3, $4::cidr, $5::cidr, $6, $7, $8, $9)
		RETURNING `+ippoolColumns,
		tenantID, p.APNID, p.Name, p.CIDRv4, p.CIDRv6,
		totalAddresses, alertWarning, alertCritical, reclaimDays,
	)

	pool, err := scanIPPool(row)
	if err != nil {
		return nil, fmt.Errorf("store: insert ip pool: %w", err)
	}

	if len(ipv4Addrs) > 0 {
		if err := s.bulkInsertAddresses(ctx, tx, pool.ID, ipv4Addrs, nil); err != nil {
			return nil, fmt.Errorf("store: bulk insert ipv4: %w", err)
		}
	}

	if len(ipv6Addrs) > 0 {
		if err := s.bulkInsertAddresses(ctx, tx, pool.ID, nil, ipv6Addrs); err != nil {
			return nil, fmt.Errorf("store: bulk insert ipv6: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit create pool: %w", err)
	}

	return pool, nil
}

func (s *IPPoolStore) bulkInsertAddresses(ctx context.Context, tx pgx.Tx, poolID uuid.UUID, ipv4Addrs, ipv6Addrs []string) error {
	const batchSize = 1000

	if len(ipv4Addrs) > 0 {
		for i := 0; i < len(ipv4Addrs); i += batchSize {
			end := i + batchSize
			if end > len(ipv4Addrs) {
				end = len(ipv4Addrs)
			}
			batch := ipv4Addrs[i:end]

			valueStrings := make([]string, 0, len(batch))
			args := []interface{}{poolID}
			argIdx := 2
			for _, addr := range batch {
				valueStrings = append(valueStrings, fmt.Sprintf("($1, $%d::inet)", argIdx))
				args = append(args, addr)
				argIdx++
			}

			query := fmt.Sprintf(
				`INSERT INTO ip_addresses (pool_id, address_v4) VALUES %s`,
				strings.Join(valueStrings, ", "),
			)
			if _, err := tx.Exec(ctx, query, args...); err != nil {
				return fmt.Errorf("batch insert ipv4 addresses: %w", err)
			}
		}
	}

	if len(ipv6Addrs) > 0 {
		for i := 0; i < len(ipv6Addrs); i += batchSize {
			end := i + batchSize
			if end > len(ipv6Addrs) {
				end = len(ipv6Addrs)
			}
			batch := ipv6Addrs[i:end]

			valueStrings := make([]string, 0, len(batch))
			args := []interface{}{poolID}
			argIdx := 2
			for _, addr := range batch {
				valueStrings = append(valueStrings, fmt.Sprintf("($1, $%d::inet)", argIdx))
				args = append(args, addr)
				argIdx++
			}

			query := fmt.Sprintf(
				`INSERT INTO ip_addresses (pool_id, address_v6) VALUES %s`,
				strings.Join(valueStrings, ", "),
			)
			if _, err := tx.Exec(ctx, query, args...); err != nil {
				return fmt.Errorf("batch insert ipv6 addresses: %w", err)
			}
		}
	}

	return nil
}

func (s *IPPoolStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*IPPool, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+ippoolColumns+` FROM ip_pools WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	p, err := scanIPPool(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIPPoolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get ip pool: %w", err)
	}
	return p, nil
}

func (s *IPPoolStore) List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int, apnIDFilter *uuid.UUID) ([]IPPool, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if apnIDFilter != nil {
		conditions = append(conditions, fmt.Sprintf("apn_id = $%d", argIdx))
		args = append(args, *apnIDFilter)
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

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM ip_pools %s ORDER BY created_at DESC, id DESC LIMIT %s`,
		ippoolColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list ip pools: %w", err)
	}
	defer rows.Close()

	var results []IPPool
	for rows.Next() {
		var p IPPool
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.APNID, &p.Name, &p.CIDRv4, &p.CIDRv6,
			&p.TotalAddresses, &p.UsedAddresses, &p.AlertThresholdWarning,
			&p.AlertThresholdCritical, &p.ReclaimGracePeriodDays, &p.State, &p.CreatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan ip pool: %w", err)
		}
		results = append(results, p)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *IPPoolStore) Update(ctx context.Context, tenantID, id uuid.UUID, p UpdateIPPoolParams) (*IPPool, error) {
	sets := []string{}
	args := []interface{}{id, tenantID}
	argIdx := 3

	if p.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *p.Name)
		argIdx++
	}
	if p.AlertThresholdWarning != nil {
		sets = append(sets, fmt.Sprintf("alert_threshold_warning = $%d", argIdx))
		args = append(args, *p.AlertThresholdWarning)
		argIdx++
	}
	if p.AlertThresholdCritical != nil {
		sets = append(sets, fmt.Sprintf("alert_threshold_critical = $%d", argIdx))
		args = append(args, *p.AlertThresholdCritical)
		argIdx++
	}
	if p.ReclaimGracePeriodDays != nil {
		sets = append(sets, fmt.Sprintf("reclaim_grace_period_days = $%d", argIdx))
		args = append(args, *p.ReclaimGracePeriodDays)
		argIdx++
	}
	if p.State != nil {
		sets = append(sets, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, *p.State)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, tenantID, id)
	}

	query := fmt.Sprintf(`UPDATE ip_pools SET %s WHERE id = $1 AND tenant_id = $2 RETURNING %s`,
		strings.Join(sets, ", "), ippoolColumns)

	row := s.db.QueryRow(ctx, query, args...)
	pool, err := scanIPPool(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIPPoolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: update ip pool: %w", err)
	}
	return pool, nil
}

func (s *IPPoolStore) GetAddressByID(ctx context.Context, id uuid.UUID) (*IPAddress, error) {
	var a IPAddress
	err := s.db.QueryRow(ctx,
		`SELECT id, pool_id, address_v4::text, address_v6::text, allocation_type, sim_id, state, allocated_at, reclaim_at, last_seen_at
		 FROM ip_addresses WHERE id = $1`, id).
		Scan(&a.ID, &a.PoolID, &a.AddressV4, &a.AddressV6, &a.AllocationType, &a.SimID, &a.State, &a.AllocatedAt, &a.ReclaimAt, &a.LastSeenAt)
	if err != nil {
		return nil, fmt.Errorf("store: get ip address: %w", err)
	}
	return &a, nil
}

func (s *IPPoolStore) ListAddresses(ctx context.Context, poolID uuid.UUID, cursor string, limit int, stateFilter string, q string) ([]IPAddress, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{poolID}
	conditions := []string{"ip.pool_id = $1"}
	argIdx := 2

	if stateFilter != "" {
		conditions = append(conditions, fmt.Sprintf("ip.state = $%d", argIdx))
		args = append(args, stateFilter)
		argIdx++
	}

	if cursor != "" {
		conditions = append(conditions, fmt.Sprintf("ip.address_v4 > $%d::inet", argIdx))
		args = append(args, cursor)
		argIdx++
	}

	if q != "" {
		like := "%" + q + "%"
		conditions = append(conditions, fmt.Sprintf(
			`(ip.address_v4::text ILIKE $%d OR COALESCE(s.iccid,'') ILIKE $%d OR COALESCE(s.imsi,'') ILIKE $%d OR COALESCE(s.msisdn,'') ILIKE $%d)`,
			argIdx, argIdx, argIdx, argIdx,
		))
		args = append(args, like)
		argIdx++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s
		FROM ip_addresses ip
		LEFT JOIN sims s ON s.id = ip.sim_id
		%s ORDER BY ip.address_v4 ASC NULLS LAST, ip.address_v6 ASC NULLS LAST LIMIT %s`,
		ipAddressColumnsJoined, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list ip addresses: %w", err)
	}
	defer rows.Close()

	var results []IPAddress
	for rows.Next() {
		var a IPAddress
		if err := rows.Scan(
			&a.ID, &a.PoolID, &a.AddressV4, &a.AddressV6,
			&a.AllocationType, &a.SimID, &a.State, &a.AllocatedAt, &a.ReclaimAt,
			&a.LastSeenAt,
			&a.SimICCID, &a.SimIMSI, &a.SimMSISDN,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan ip address: %w", err)
		}
		results = append(results, a)
	}

	nextCursor := ""
	if len(results) > limit {
		last := results[limit-1]
		if last.AddressV4 != nil {
			nextCursor = *last.AddressV4
		} else {
			nextCursor = last.ID.String()
		}
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *IPPoolStore) ReserveStaticIP(ctx context.Context, poolID, simID uuid.UUID, addressV4 *string) (*IPAddress, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for reserve: %w", err)
	}
	defer tx.Rollback(ctx)

	var poolState string
	err = tx.QueryRow(ctx,
		`SELECT state FROM ip_pools WHERE id = $1 FOR UPDATE`, poolID,
	).Scan(&poolState)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIPPoolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock pool for reserve: %w", err)
	}
	if poolState == "exhausted" || poolState == "disabled" {
		return nil, ErrPoolExhausted
	}

	var ipRow pgx.Row
	if addressV4 != nil && *addressV4 != "" {
		ipRow = tx.QueryRow(ctx, `
			SELECT `+ipAddressColumns+`
			FROM ip_addresses
			WHERE pool_id = $1 AND address_v4::text = $2 AND state = 'available'
			FOR UPDATE`, poolID, *addressV4)
	} else {
		ipRow = tx.QueryRow(ctx, `
			SELECT `+ipAddressColumns+`
			FROM ip_addresses
			WHERE pool_id = $1 AND state = 'available'
			ORDER BY address_v4 ASC NULLS LAST, address_v6 ASC NULLS LAST
			LIMIT 1
			FOR UPDATE SKIP LOCKED`, poolID)
	}

	addr, err := scanIPAddress(ipRow)
	if errors.Is(err, pgx.ErrNoRows) {
		if addressV4 != nil {
			return nil, ErrIPAlreadyAllocated
		}
		_, _ = tx.Exec(ctx, `UPDATE ip_pools SET state = 'exhausted' WHERE id = $1`, poolID)
		_ = tx.Commit(ctx)
		return nil, ErrPoolExhausted
	}
	if err != nil {
		return nil, fmt.Errorf("store: find ip for reserve: %w", err)
	}

	row := tx.QueryRow(ctx, `
		UPDATE ip_addresses SET state = 'reserved', allocation_type = 'static',
			sim_id = $2, allocated_at = NOW()
		WHERE id = $1
		RETURNING `+ipAddressColumns, addr.ID, simID)
	reserved, err := scanIPAddress(row)
	if err != nil {
		return nil, fmt.Errorf("store: update ip reserve: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE ip_pools SET used_addresses = used_addresses + 1 WHERE id = $1`, poolID)
	if err != nil {
		return nil, fmt.Errorf("store: increment used_addresses: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit reserve: %w", err)
	}

	return reserved, nil
}

func (s *IPPoolStore) AllocateIP(ctx context.Context, poolID, simID uuid.UUID) (*IPAddress, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx for allocate: %w", err)
	}
	defer tx.Rollback(ctx)

	var poolState string
	var poolUsed, poolTotal, alertWarning, alertCritical int
	err = tx.QueryRow(ctx, `
		SELECT state, used_addresses, total_addresses, alert_threshold_warning, alert_threshold_critical
		FROM ip_pools WHERE id = $1 FOR UPDATE`, poolID,
	).Scan(&poolState, &poolUsed, &poolTotal, &alertWarning, &alertCritical)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIPPoolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: lock pool for allocate: %w", err)
	}
	if poolState == "exhausted" || poolState == "disabled" {
		return nil, ErrPoolExhausted
	}

	ipRow := tx.QueryRow(ctx, `
		SELECT `+ipAddressColumns+`
		FROM ip_addresses
		WHERE pool_id = $1 AND state = 'available'
		ORDER BY address_v4 ASC NULLS LAST, address_v6 ASC NULLS LAST
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, poolID)

	addr, err := scanIPAddress(ipRow)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = tx.Exec(ctx, `UPDATE ip_pools SET state = 'exhausted' WHERE id = $1`, poolID)
		_ = tx.Commit(ctx)
		return nil, ErrPoolExhausted
	}
	if err != nil {
		return nil, fmt.Errorf("store: find available ip: %w", err)
	}

	row := tx.QueryRow(ctx, `
		UPDATE ip_addresses SET state = 'allocated', sim_id = $2, allocated_at = NOW()
		WHERE id = $1
		RETURNING `+ipAddressColumns, addr.ID, simID)
	allocated, err := scanIPAddress(row)
	if err != nil {
		return nil, fmt.Errorf("store: update ip allocate: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE ip_pools SET used_addresses = used_addresses + 1 WHERE id = $1`, poolID)
	if err != nil {
		return nil, fmt.Errorf("store: increment used_addresses: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit allocate: %w", err)
	}

	return allocated, nil
}

func (s *IPPoolStore) ReleaseIP(ctx context.Context, poolID, simID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin tx for release: %w", err)
	}
	defer tx.Rollback(ctx)

	var ipID uuid.UUID
	var allocType string
	var reclaimDays int
	err = tx.QueryRow(ctx, `
		SELECT a.id, a.allocation_type, p.reclaim_grace_period_days
		FROM ip_addresses a JOIN ip_pools p ON a.pool_id = p.id
		WHERE a.pool_id = $1 AND a.sim_id = $2 AND a.state IN ('allocated', 'reserved')
		FOR UPDATE OF a`, poolID, simID,
	).Scan(&ipID, &allocType, &reclaimDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIPNotFound
	}
	if err != nil {
		return fmt.Errorf("store: find ip for release: %w", err)
	}

	if allocType == "static" {
		_, err = tx.Exec(ctx, `
			UPDATE ip_addresses SET state = 'reclaiming',
				reclaim_at = NOW() + ($2 || ' days')::interval
			WHERE id = $1`, ipID, fmt.Sprintf("%d", reclaimDays))
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE ip_addresses SET state = 'available', sim_id = NULL,
				allocated_at = NULL, reclaim_at = NULL
			WHERE id = $1`, ipID)
		if err == nil {
			_, err = tx.Exec(ctx,
				`UPDATE ip_pools SET used_addresses = GREATEST(used_addresses - 1, 0) WHERE id = $1`, poolID)
			if err == nil {
				_, _ = tx.Exec(ctx,
					`UPDATE ip_pools SET state = 'active' WHERE id = $1 AND state = 'exhausted'`, poolID)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("store: release ip: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit release: %w", err)
	}
	return nil
}

func (s *IPPoolStore) GetIPAddressByID(ctx context.Context, id uuid.UUID) (*IPAddress, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+ipAddressColumns+` FROM ip_addresses WHERE id = $1`,
		id,
	)
	addr, err := scanIPAddress(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrIPNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get ip address by id: %w", err)
	}
	return addr, nil
}

func (s *IPPoolStore) ListExpiredReclaim(ctx context.Context, now time.Time, limit int) ([]ExpiredIPAddress, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(ctx, `
		SELECT a.id, a.pool_id, p.tenant_id, a.address_v4::text, a.address_v6::text, a.sim_id, a.reclaim_at
		FROM ip_addresses a
		JOIN ip_pools p ON a.pool_id = p.id
		WHERE a.state = 'reclaiming' AND a.reclaim_at <= $1
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list expired reclaim: %w", err)
	}
	defer rows.Close()

	var results []ExpiredIPAddress
	for rows.Next() {
		var e ExpiredIPAddress
		if err := rows.Scan(&e.ID, &e.PoolID, &e.TenantID, &e.AddressV4, &e.AddressV6, &e.PreviousSimID, &e.ReclaimAt); err != nil {
			return nil, fmt.Errorf("store: scan expired reclaim: %w", err)
		}
		results = append(results, e)
	}
	return results, nil
}

func (s *IPPoolStore) FinalizeReclaim(ctx context.Context, ipID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin tx for finalize reclaim: %w", err)
	}
	defer tx.Rollback(ctx)

	var poolID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE ip_addresses SET state = 'available', sim_id = NULL, allocated_at = NULL, reclaim_at = NULL
		WHERE id = $1 AND state = 'reclaiming'
		RETURNING pool_id
	`, ipID).Scan(&poolID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIPNotFound
	}
	if err != nil {
		return fmt.Errorf("store: finalize reclaim update ip: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE ip_pools SET used_addresses = GREATEST(used_addresses - 1, 0) WHERE id = $1`, poolID)
	if err != nil {
		return fmt.Errorf("store: finalize reclaim update pool: %w", err)
	}

	_, _ = tx.Exec(ctx,
		`UPDATE ip_pools SET state = 'active' WHERE id = $1 AND state = 'exhausted'`, poolID)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit finalize reclaim: %w", err)
	}
	return nil
}

func (s *IPPoolStore) ListGraceExpired(ctx context.Context, limit int) ([]GraceExpiredIPAddress, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Query(ctx, `
		SELECT ip.id, ip.pool_id, p.tenant_id, ip.address_v4::text, ip.address_v6::text
		FROM ip_addresses ip
		JOIN ip_pools p ON p.id = ip.pool_id
		JOIN sims s ON s.id = ip.sim_id
		WHERE ip.released_at IS NULL
		  AND ip.grace_expires_at IS NOT NULL
		  AND ip.grace_expires_at < NOW()
		  AND s.state = 'terminated'
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list grace expired: %w", err)
	}
	defer rows.Close()

	var results []GraceExpiredIPAddress
	for rows.Next() {
		var e GraceExpiredIPAddress
		if err := rows.Scan(&e.ID, &e.PoolID, &e.TenantID, &e.AddressV4, &e.AddressV6); err != nil {
			return nil, fmt.Errorf("store: scan grace expired: %w", err)
		}
		results = append(results, e)
	}
	return results, nil
}

func (s *IPPoolStore) ReleaseGraceIP(ctx context.Context, ipID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin tx for release grace ip: %w", err)
	}
	defer tx.Rollback(ctx)

	var poolID uuid.UUID
	err = tx.QueryRow(ctx, `
		UPDATE ip_addresses
		SET state = 'available',
		    sim_id = NULL,
		    grace_expires_at = NULL,
		    released_at = NOW(),
		    allocated_at = NULL
		WHERE id = $1
		  AND released_at IS NULL
		RETURNING pool_id
	`, ipID).Scan(&poolID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIPNotFound
	}
	if err != nil {
		return fmt.Errorf("store: release grace ip update: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE ip_pools SET used_addresses = GREATEST(used_addresses - 1, 0) WHERE id = $1`, poolID)
	if err != nil {
		return fmt.Errorf("store: release grace ip update pool: %w", err)
	}

	_, _ = tx.Exec(ctx,
		`UPDATE ip_pools SET state = 'active' WHERE id = $1 AND state = 'exhausted'`, poolID)

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit release grace ip: %w", err)
	}
	return nil
}

func GenerateIPv4Addresses(cidr string) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid IPv4 CIDR: %w", err)
	}

	ip = ip.To4()
	if ip == nil {
		return nil, fmt.Errorf("not an IPv4 CIDR: %s", cidr)
	}

	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("not an IPv4 CIDR: %s", cidr)
	}

	totalHosts := int(math.Pow(2, float64(bits-ones)))

	if totalHosts <= 0 {
		return nil, fmt.Errorf("invalid prefix length: /%d", ones)
	}

	networkIP := binary.BigEndian.Uint32(ipNet.IP.To4())

	var addresses []string

	switch {
	case ones == 32:
		addr := intToIPv4(networkIP)
		addresses = append(addresses, addr)
	case ones == 31:
		for i := 0; i < 2; i++ {
			addr := intToIPv4(networkIP + uint32(i))
			addresses = append(addresses, addr)
		}
	default:
		for i := 1; i < totalHosts-1; i++ {
			addr := intToIPv4(networkIP + uint32(i))
			addresses = append(addresses, addr)
		}
	}

	return addresses, nil
}

func intToIPv4(n uint32) string {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip.String()
}

func GenerateIPv6Addresses(cidr string) ([]string, int, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid IPv6 CIDR: %w", err)
	}

	ones, bits := ipNet.Mask.Size()
	if bits != 128 {
		return nil, 0, fmt.Errorf("not an IPv6 CIDR: %s", cidr)
	}

	hostBits := bits - ones
	if hostBits > 48 {
		return nil, 0, fmt.Errorf("IPv6 prefix too large (/%d): max /%d supported", ones, 128-48)
	}

	totalBig := new(big.Int).Lsh(big.NewInt(1), uint(hostBits))
	total := 0
	if totalBig.IsInt64() {
		total = int(totalBig.Int64())
	} else {
		total = math.MaxInt32
	}

	cap := 65536
	if total > cap {
		total = cap
	}

	networkIP := new(big.Int).SetBytes(ipNet.IP.To16())

	addresses := make([]string, 0, total)
	for i := 0; i < total; i++ {
		addr := new(big.Int).Add(networkIP, big.NewInt(int64(i)))
		ipBytes := addr.Bytes()
		if len(ipBytes) < 16 {
			padded := make([]byte, 16)
			copy(padded[16-len(ipBytes):], ipBytes)
			ipBytes = padded
		}
		ip := net.IP(ipBytes)
		addresses = append(addresses, ip.String())
	}

	return addresses, total, nil
}

// ListByAPN returns all active IP pools associated with the given APN in the given tenant.
// Used by AC-3 framed_ip validation to determine the legal IP boundary for a SIM's session.
func (s *IPPoolStore) ListByAPN(ctx context.Context, tenantID, apnID uuid.UUID) ([]IPPool, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+ippoolColumns+` FROM ip_pools
		 WHERE tenant_id = $1 AND apn_id = $2 AND state = 'active'
		 ORDER BY id`,
		tenantID, apnID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list ip pools by apn: %w", err)
	}
	defer rows.Close()

	var results []IPPool
	for rows.Next() {
		var p IPPool
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.APNID, &p.Name, &p.CIDRv4, &p.CIDRv6,
			&p.TotalAddresses, &p.UsedAddresses, &p.AlertThresholdWarning,
			&p.AlertThresholdCritical, &p.ReclaimGracePeriodDays, &p.State, &p.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan ip pool by apn: %w", err)
		}
		results = append(results, p)
	}
	return results, nil
}

type PoolAPNStats struct {
	APNID uuid.UUID
	Used  int
	Total int
}

func (s *IPPoolStore) SumByAPN(ctx context.Context, tenantID uuid.UUID) ([]PoolAPNStats, error) {
	rows, err := s.db.Query(ctx, `
		SELECT apn_id, COALESCE(SUM(used_addresses),0), COALESCE(SUM(total_addresses),0)
		FROM ip_pools
		WHERE tenant_id = $1
		GROUP BY apn_id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: sum ip pools by apn: %w", err)
	}
	defer rows.Close()

	var results []PoolAPNStats
	for rows.Next() {
		var s PoolAPNStats
		if err := rows.Scan(&s.APNID, &s.Used, &s.Total); err != nil {
			return nil, fmt.Errorf("store: scan pool apn stats: %w", err)
		}
		results = append(results, s)
	}
	return results, nil
}

type PoolCapacityRow struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	CIDR            string    `json:"cidr"`
	Total           int       `json:"total"`
	Used            int       `json:"used"`
	Available       int       `json:"available"`
	UtilizationPct  float64   `json:"utilization_pct"`
	AllocationRate  float64   `json:"allocation_rate"`
	ExhaustionHours *float64  `json:"exhaustion_hours"`
	UsedYesterday   int       `json:"-"`
}

func (s *IPPoolStore) GetCapacitySummary(ctx context.Context, tenantID uuid.UUID) ([]PoolCapacityRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			p.id,
			p.name,
			COALESCE(p.cidr_v4::text, p.cidr_v6::text, '') AS cidr,
			p.total_addresses,
			p.used_addresses,
			COALESCE(
				(SELECT COUNT(*) FROM ip_addresses ia
				 WHERE ia.pool_id = p.id AND ia.state = 'allocated'
				   AND ia.allocated_at < NOW() - INTERVAL '24 hours'),
				0
			) AS used_yesterday
		FROM ip_pools p
		WHERE p.tenant_id = $1
		ORDER BY p.name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: get capacity summary: %w", err)
	}
	defer rows.Close()

	var results []PoolCapacityRow
	for rows.Next() {
		var r PoolCapacityRow
		if err := rows.Scan(&r.ID, &r.Name, &r.CIDR, &r.Total, &r.Used, &r.UsedYesterday); err != nil {
			return nil, fmt.Errorf("store: scan capacity row: %w", err)
		}
		r.Available = r.Total - r.Used
		if r.Total > 0 {
			r.UtilizationPct = float64(r.Used) / float64(r.Total) * 100
		}
		delta := float64(r.Used - r.UsedYesterday)
		r.AllocationRate = delta / 86400.0
		if r.AllocationRate > 0 && r.Available > 0 {
			hours := float64(r.Available) / r.AllocationRate / 3600.0
			r.ExhaustionHours = &hours
		}
		results = append(results, r)
	}
	return results, nil
}
