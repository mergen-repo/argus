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
	ID                      uuid.UUID  `json:"id"`
	TenantID                uuid.UUID  `json:"tenant_id"`
	APNID                   uuid.UUID  `json:"apn_id"`
	Name                    string     `json:"name"`
	CIDRv4                  *string    `json:"cidr_v4"`
	CIDRv6                  *string    `json:"cidr_v6"`
	TotalAddresses          int        `json:"total_addresses"`
	UsedAddresses           int        `json:"used_addresses"`
	AlertThresholdWarning   int        `json:"alert_threshold_warning"`
	AlertThresholdCritical  int        `json:"alert_threshold_critical"`
	ReclaimGracePeriodDays  int        `json:"reclaim_grace_period_days"`
	State                   string     `json:"state"`
	CreatedAt               time.Time  `json:"created_at"`
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
}

type CreateIPPoolParams struct {
	APNID                   uuid.UUID
	Name                    string
	CIDRv4                  *string
	CIDRv6                  *string
	AlertThresholdWarning   *int
	AlertThresholdCritical  *int
	ReclaimGracePeriodDays  *int
}

type UpdateIPPoolParams struct {
	Name                    *string
	AlertThresholdWarning   *int
	AlertThresholdCritical  *int
	ReclaimGracePeriodDays  *int
	State                   *string
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

var ipAddressColumns = `id, pool_id, address_v4::text, address_v6::text,
	allocation_type, sim_id, state, allocated_at, reclaim_at`

func scanIPAddress(row pgx.Row) (*IPAddress, error) {
	var a IPAddress
	err := row.Scan(
		&a.ID, &a.PoolID, &a.AddressV4, &a.AddressV6,
		&a.AllocationType, &a.SimID, &a.State, &a.AllocatedAt, &a.ReclaimAt,
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

func (s *IPPoolStore) ListAddresses(ctx context.Context, poolID uuid.UUID, cursor string, limit int, stateFilter string) ([]IPAddress, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{poolID}
	conditions := []string{"pool_id = $1"}
	argIdx := 2

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

	where := "WHERE " + strings.Join(conditions, " AND ")

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM ip_addresses %s ORDER BY id DESC LIMIT %s`,
		ipAddressColumns, where, limitPlaceholder)

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
		); err != nil {
			return nil, "", fmt.Errorf("store: scan ip address: %w", err)
		}
		results = append(results, a)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
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
