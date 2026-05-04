package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CostAnalyticsStore struct {
	db *pgxpool.Pool
}

func NewCostAnalyticsStore(db *pgxpool.Pool) *CostAnalyticsStore {
	return &CostAnalyticsStore{db: db}
}

type CostQueryParams struct {
	TenantID   uuid.UUID
	From       time.Time
	To         time.Time
	OperatorID *uuid.UUID
	APNID      *uuid.UUID
	RATType    *string
}

type CostTotals struct {
	TotalUsageCost   float64 `json:"total_usage_cost"`
	TotalCarrierCost float64 `json:"total_carrier_cost"`
	TotalBytes       int64   `json:"total_bytes"`
	CDRCount         int64   `json:"cdr_count"`
	UniqueSims       int64   `json:"unique_sims"`
}

type CostByOperator struct {
	OperatorID       uuid.UUID `json:"operator_id"`
	TotalUsageCost   float64   `json:"total_usage_cost"`
	TotalCarrierCost float64   `json:"total_carrier_cost"`
	TotalBytes       int64     `json:"total_bytes"`
	CDRCount         int64     `json:"cdr_count"`
	Percentage       float64   `json:"percentage"`
}

type CostPerMBRow struct {
	OperatorID uuid.UUID `json:"operator_id"`
	RATType    string    `json:"rat_type"`
	AvgCostMB  float64   `json:"avg_cost_per_mb"`
	TotalCost  float64   `json:"total_cost"`
	TotalMB    float64   `json:"total_mb"`
}

type TopExpensiveSIM struct {
	SimID          uuid.UUID `json:"sim_id"`
	TotalUsageCost float64   `json:"total_usage_cost"`
	TotalBytes     int64     `json:"total_bytes"`
	CDRCount       int64     `json:"cdr_count"`
	OperatorID     uuid.UUID `json:"operator_id"`
}

type CostTrendPoint struct {
	Bucket           time.Time `json:"bucket"`
	TotalUsageCost   float64   `json:"total_usage_cost"`
	TotalCarrierCost float64   `json:"total_carrier_cost"`
	TotalBytes       int64     `json:"total_bytes"`
	ActiveSims       int64     `json:"active_sims"`
}

type OperatorCostComparison struct {
	OperatorID uuid.UUID `json:"operator_id"`
	CostPerMB  *float64  `json:"cost_per_mb"`
	AvgCostMB  float64   `json:"avg_cost_per_mb"`
	TotalCost  float64   `json:"total_cost"`
	SimCount   int64     `json:"sim_count"`
}

type InactiveSIMCost struct {
	SimCount      int64   `json:"sim_count"`
	TotalCost     float64 `json:"total_cost"`
	AvgCostPerSIM float64 `json:"avg_cost_per_sim"`
}

type LowUsageSIM struct {
	SimCount   int64   `json:"sim_count"`
	TotalCost  float64 `json:"total_cost"`
	TotalBytes int64   `json:"total_bytes"`
	AvgCostMB  float64 `json:"avg_cost_per_mb"`
}

func (s *CostAnalyticsStore) buildConditions(p CostQueryParams) ([]string, []interface{}, int) {
	args := []interface{}{p.TenantID, p.From, p.To}
	conditions := []string{"tenant_id = $1", "timestamp >= $2", "timestamp < $3"}
	argIdx := 4

	if p.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *p.OperatorID)
		argIdx++
	}
	if p.APNID != nil {
		conditions = append(conditions, fmt.Sprintf("apn_id = $%d", argIdx))
		args = append(args, *p.APNID)
		argIdx++
	}
	if p.RATType != nil {
		conditions = append(conditions, fmt.Sprintf("rat_type = $%d", argIdx))
		args = append(args, *p.RATType)
		argIdx++
	}
	return conditions, args, argIdx
}

func (s *CostAnalyticsStore) GetCostTotals(ctx context.Context, p CostQueryParams) (*CostTotals, error) {
	conditions, args, _ := s.buildConditions(p)

	query := fmt.Sprintf(`SELECT
		COALESCE(SUM(usage_cost), 0),
		COALESCE(SUM(carrier_cost), 0),
		COALESCE(SUM(bytes_in + bytes_out), 0),
		COUNT(*),
		COUNT(DISTINCT sim_id)
		FROM cdrs WHERE %s`, strings.Join(conditions, " AND "))

	var t CostTotals
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&t.TotalUsageCost, &t.TotalCarrierCost, &t.TotalBytes, &t.CDRCount, &t.UniqueSims)
	if err != nil {
		return nil, fmt.Errorf("store: cost totals: %w", err)
	}
	return &t, nil
}

func (s *CostAnalyticsStore) GetCostByOperator(ctx context.Context, p CostQueryParams) ([]CostByOperator, error) {
	conditions, args, _ := s.buildConditions(p)

	query := fmt.Sprintf(`SELECT
		operator_id,
		COALESCE(SUM(usage_cost), 0) AS total_usage_cost,
		COALESCE(SUM(carrier_cost), 0) AS total_carrier_cost,
		COALESCE(SUM(bytes_in + bytes_out), 0) AS total_bytes,
		COUNT(*) AS cdr_count
		FROM cdrs WHERE %s
		GROUP BY operator_id
		ORDER BY total_usage_cost DESC`,
		strings.Join(conditions, " AND "))

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: cost by operator: %w", err)
	}
	defer rows.Close()

	var items []CostByOperator
	var grandTotal float64
	for rows.Next() {
		var item CostByOperator
		if err := rows.Scan(&item.OperatorID, &item.TotalUsageCost, &item.TotalCarrierCost, &item.TotalBytes, &item.CDRCount); err != nil {
			return nil, fmt.Errorf("store: scan cost by operator: %w", err)
		}
		grandTotal += item.TotalUsageCost
		items = append(items, item)
	}

	for i := range items {
		if grandTotal > 0 {
			items[i].Percentage = items[i].TotalUsageCost / grandTotal * 100.0
		}
	}

	return items, nil
}

func (s *CostAnalyticsStore) GetCostPerMB(ctx context.Context, p CostQueryParams) ([]CostPerMBRow, error) {
	conditions, args, _ := s.buildConditions(p)

	query := fmt.Sprintf(`SELECT
		operator_id,
		COALESCE(rat_type, 'unknown') AS rat,
		COALESCE(SUM(usage_cost), 0) AS total_cost,
		COALESCE(SUM(bytes_in + bytes_out), 0) AS total_bytes
		FROM cdrs WHERE %s
		GROUP BY operator_id, rat_type
		ORDER BY operator_id, rat`,
		strings.Join(conditions, " AND "))

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: cost per mb: %w", err)
	}
	defer rows.Close()

	var items []CostPerMBRow
	for rows.Next() {
		var item CostPerMBRow
		var totalBytes int64
		if err := rows.Scan(&item.OperatorID, &item.RATType, &item.TotalCost, &totalBytes); err != nil {
			return nil, fmt.Errorf("store: scan cost per mb: %w", err)
		}
		item.TotalMB = float64(totalBytes) / (1024.0 * 1024.0)
		if item.TotalMB > 0 {
			item.AvgCostMB = item.TotalCost / item.TotalMB
		}
		items = append(items, item)
	}

	return items, nil
}

func (s *CostAnalyticsStore) GetTopExpensiveSIMs(ctx context.Context, p CostQueryParams, limit int) ([]TopExpensiveSIM, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	conditions, args, argIdx := s.buildConditions(p)
	args = append(args, limit)

	query := fmt.Sprintf(`SELECT
		sim_id,
		COALESCE(SUM(usage_cost), 0) AS total_usage_cost,
		COALESCE(SUM(bytes_in + bytes_out), 0) AS total_bytes,
		COUNT(*) AS cdr_count,
		(array_agg(operator_id ORDER BY usage_cost DESC NULLS LAST))[1] AS primary_operator
		FROM cdrs WHERE %s
		GROUP BY sim_id
		ORDER BY total_usage_cost DESC
		LIMIT $%d`,
		strings.Join(conditions, " AND "), argIdx)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: top expensive sims: %w", err)
	}
	defer rows.Close()

	var items []TopExpensiveSIM
	for rows.Next() {
		var item TopExpensiveSIM
		if err := rows.Scan(&item.SimID, &item.TotalUsageCost, &item.TotalBytes, &item.CDRCount, &item.OperatorID); err != nil {
			return nil, fmt.Errorf("store: scan top expensive sim: %w", err)
		}
		items = append(items, item)
	}

	return items, nil
}

func (s *CostAnalyticsStore) GetCostTrend(ctx context.Context, p CostQueryParams, bucketInterval string) ([]CostTrendPoint, error) {
	args := []interface{}{p.TenantID, p.From, p.To}
	conditions := []string{"tenant_id = $1", "bucket >= $2", "bucket < $3"}
	argIdx := 4

	if p.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *p.OperatorID)
		argIdx++
	}
	_ = argIdx

	query := fmt.Sprintf(`SELECT
		time_bucket('%s', bucket) AS ts,
		COALESCE(SUM(total_cost), 0) AS total_usage_cost,
		COALESCE(SUM(total_carrier_cost), 0) AS total_carrier_cost,
		COALESCE(SUM(total_bytes), 0) AS total_bytes,
		COALESCE(SUM(active_sims), 0) AS active_sims
		FROM cdrs_daily
		WHERE %s
		GROUP BY ts
		ORDER BY ts`,
		bucketInterval, strings.Join(conditions, " AND "))

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: cost trend: %w", err)
	}
	defer rows.Close()

	var points []CostTrendPoint
	for rows.Next() {
		var pt CostTrendPoint
		if err := rows.Scan(&pt.Bucket, &pt.TotalUsageCost, &pt.TotalCarrierCost, &pt.TotalBytes, &pt.ActiveSims); err != nil {
			return nil, fmt.Errorf("store: scan cost trend: %w", err)
		}
		points = append(points, pt)
	}

	return points, nil
}

func (s *CostAnalyticsStore) GetOperatorCostComparison(ctx context.Context, tenantID uuid.UUID, from, to time.Time) ([]OperatorCostComparison, error) {
	query := `SELECT
		c.operator_id,
		g.cost_per_mb,
		CASE WHEN SUM(c.bytes_in + c.bytes_out) > 0
			THEN SUM(c.usage_cost) / (SUM(c.bytes_in + c.bytes_out)::decimal / (1024.0*1024.0))
			ELSE 0 END AS avg_cost_mb,
		COALESCE(SUM(c.usage_cost), 0) AS total_cost,
		COUNT(DISTINCT c.sim_id) AS sim_count
		FROM cdrs c
		LEFT JOIN operator_grants g ON g.operator_id = c.operator_id AND g.tenant_id = c.tenant_id
		WHERE c.tenant_id = $1 AND c.timestamp >= $2 AND c.timestamp < $3
		GROUP BY c.operator_id, g.cost_per_mb
		ORDER BY avg_cost_mb DESC`

	rows, err := s.db.Query(ctx, query, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: operator cost comparison: %w", err)
	}
	defer rows.Close()

	var items []OperatorCostComparison
	for rows.Next() {
		var item OperatorCostComparison
		if err := rows.Scan(&item.OperatorID, &item.CostPerMB, &item.AvgCostMB, &item.TotalCost, &item.SimCount); err != nil {
			return nil, fmt.Errorf("store: scan operator cost comparison: %w", err)
		}
		items = append(items, item)
	}

	return items, nil
}

func (s *CostAnalyticsStore) GetInactiveSIMsCost(ctx context.Context, tenantID uuid.UUID, from, to time.Time, inactivityDays int) (*InactiveSIMCost, error) {
	query := `SELECT
		COUNT(DISTINCT s.id) AS sim_count,
		COALESCE(SUM(c.usage_cost), 0) AS total_cost
		FROM sims s
		LEFT JOIN cdrs c ON c.sim_id = s.id AND c.tenant_id = s.tenant_id AND c.timestamp >= $2 AND c.timestamp < $3
		WHERE s.tenant_id = $1 AND s.state = 'active'
		AND s.id NOT IN (
			SELECT DISTINCT sim_id FROM cdrs
			WHERE tenant_id = $1 AND timestamp >= $4 AND timestamp < $3
			AND (bytes_in + bytes_out) > 0
		)`

	inactiveFrom := to.AddDate(0, 0, -inactivityDays)
	var result InactiveSIMCost
	err := s.db.QueryRow(ctx, query, tenantID, from, to, inactiveFrom).Scan(&result.SimCount, &result.TotalCost)
	if err != nil {
		return nil, fmt.Errorf("store: inactive sims cost: %w", err)
	}
	if result.SimCount > 0 {
		result.AvgCostPerSIM = result.TotalCost / float64(result.SimCount)
	}
	return &result, nil
}

func (s *CostAnalyticsStore) GetLowUsageSIMs(ctx context.Context, tenantID uuid.UUID, from, to time.Time, bytesThreshold int64) (*LowUsageSIM, error) {
	query := `SELECT
		COUNT(*) AS sim_count,
		COALESCE(SUM(total_cost), 0) AS total_cost,
		COALESCE(SUM(total_bytes), 0) AS total_bytes
		FROM (
			SELECT sim_id,
				SUM(usage_cost) AS total_cost,
				SUM(bytes_in + bytes_out) AS total_bytes
			FROM cdrs
			WHERE tenant_id = $1 AND timestamp >= $2 AND timestamp < $3
			GROUP BY sim_id
			HAVING SUM(bytes_in + bytes_out) > 0 AND SUM(bytes_in + bytes_out) < $4
		) sub`

	var result LowUsageSIM
	err := s.db.QueryRow(ctx, query, tenantID, from, to, bytesThreshold).Scan(
		&result.SimCount, &result.TotalCost, &result.TotalBytes)
	if err != nil {
		return nil, fmt.Errorf("store: low usage sims: %w", err)
	}
	totalMB := float64(result.TotalBytes) / (1024.0 * 1024.0)
	if totalMB > 0 {
		result.AvgCostMB = result.TotalCost / totalMB
	}
	return &result, nil
}
