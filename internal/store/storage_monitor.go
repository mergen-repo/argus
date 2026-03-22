package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type StorageMonitorStore struct {
	db *pgxpool.Pool
}

func NewStorageMonitorStore(db *pgxpool.Pool) *StorageMonitorStore {
	return &StorageMonitorStore{db: db}
}

type TableSizeInfo struct {
	TableName       string `json:"table_name"`
	TotalSize       int64  `json:"total_size"`
	TotalSizeHuman  string `json:"total_size_human"`
	TableSize       int64  `json:"table_size"`
	TableSizeHuman  string `json:"table_size_human"`
	IndexSize       int64  `json:"index_size"`
	IndexSizeHuman  string `json:"index_size_human"`
	RowEstimate     int64  `json:"row_estimate"`
}

type ChunkInfo struct {
	ChunkName      string    `json:"chunk_name"`
	HypertableName string    `json:"hypertable_name"`
	RangeStart     time.Time `json:"range_start"`
	RangeEnd       time.Time `json:"range_end"`
	TotalBytes     int64     `json:"total_bytes"`
	IsCompressed   bool      `json:"is_compressed"`
}

type CompressionStats struct {
	HypertableName       string  `json:"hypertable_name"`
	UncompressedBytes    int64   `json:"uncompressed_bytes"`
	CompressedBytes      int64   `json:"compressed_bytes"`
	CompressionRatio     float64 `json:"compression_ratio"`
	CompressedChunks     int64   `json:"compressed_chunks"`
	UncompressedChunks   int64   `json:"uncompressed_chunks"`
}

type DiskUsageInfo struct {
	TotalBytes     int64   `json:"total_bytes"`
	UsedBytes      int64   `json:"used_bytes"`
	AvailableBytes int64   `json:"available_bytes"`
	UsagePct       float64 `json:"usage_pct"`
}

type DatabaseStats struct {
	DatabaseName  string `json:"database_name"`
	DatabaseSize  int64  `json:"database_size"`
	DatabaseHuman string `json:"database_size_human"`
	ActiveConns   int64  `json:"active_connections"`
	MaxConns      int64  `json:"max_connections"`
	ConnUsagePct  float64 `json:"connection_usage_pct"`
}

func (s *StorageMonitorStore) GetTableSizes(ctx context.Context) ([]TableSizeInfo, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			relname AS table_name,
			pg_total_relation_size(c.oid) AS total_size,
			pg_size_pretty(pg_total_relation_size(c.oid)) AS total_size_human,
			pg_relation_size(c.oid) AS table_size,
			pg_size_pretty(pg_relation_size(c.oid)) AS table_size_human,
			pg_indexes_size(c.oid) AS index_size,
			pg_size_pretty(pg_indexes_size(c.oid)) AS index_size_human,
			COALESCE(reltuples, 0)::bigint AS row_estimate
		FROM pg_class c
		LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
			AND c.relkind = 'r'
			AND c.relname NOT LIKE '_timescaledb%'
		ORDER BY pg_total_relation_size(c.oid) DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: get table sizes: %w", err)
	}
	defer rows.Close()

	var results []TableSizeInfo
	for rows.Next() {
		var t TableSizeInfo
		if err := rows.Scan(
			&t.TableName, &t.TotalSize, &t.TotalSizeHuman,
			&t.TableSize, &t.TableSizeHuman,
			&t.IndexSize, &t.IndexSizeHuman,
			&t.RowEstimate,
		); err != nil {
			return nil, fmt.Errorf("store: scan table size: %w", err)
		}
		results = append(results, t)
	}
	return results, nil
}

func (s *StorageMonitorStore) GetHypertableChunks(ctx context.Context, hypertable string) ([]ChunkInfo, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			ch.chunk_name,
			ch.hypertable_name::text,
			ch.range_start::timestamptz,
			ch.range_end::timestamptz,
			COALESCE(pg_total_relation_size(format('%I.%I', ch.chunk_schema, ch.chunk_name)), 0) AS total_bytes,
			ch.is_compressed
		FROM timescaledb_information.chunks ch
		WHERE ch.hypertable_name = $1
		ORDER BY ch.range_start DESC
	`, hypertable)
	if err != nil {
		return nil, fmt.Errorf("store: get hypertable chunks: %w", err)
	}
	defer rows.Close()

	var results []ChunkInfo
	for rows.Next() {
		var c ChunkInfo
		if err := rows.Scan(
			&c.ChunkName, &c.HypertableName,
			&c.RangeStart, &c.RangeEnd,
			&c.TotalBytes, &c.IsCompressed,
		); err != nil {
			return nil, fmt.Errorf("store: scan chunk info: %w", err)
		}
		results = append(results, c)
	}
	return results, nil
}

func (s *StorageMonitorStore) GetCompressionStats(ctx context.Context) ([]CompressionStats, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			hypertable_name::text,
			COALESCE(before_compression_total_bytes, 0) AS uncompressed_bytes,
			COALESCE(after_compression_total_bytes, 0) AS compressed_bytes,
			CASE
				WHEN COALESCE(after_compression_total_bytes, 0) > 0
				THEN ROUND((before_compression_total_bytes::numeric / after_compression_total_bytes::numeric), 2)
				ELSE 0
			END AS compression_ratio,
			COALESCE(number_compressed_chunks, 0) AS compressed_chunks,
			(total_chunks - COALESCE(number_compressed_chunks, 0)) AS uncompressed_chunks
		FROM timescaledb_information.hypertable_compression_stats
		ORDER BY hypertable_name
	`)
	if err != nil {
		return nil, fmt.Errorf("store: get compression stats: %w", err)
	}
	defer rows.Close()

	var results []CompressionStats
	for rows.Next() {
		var cs CompressionStats
		if err := rows.Scan(
			&cs.HypertableName,
			&cs.UncompressedBytes, &cs.CompressedBytes,
			&cs.CompressionRatio,
			&cs.CompressedChunks, &cs.UncompressedChunks,
		); err != nil {
			return nil, fmt.Errorf("store: scan compression stats: %w", err)
		}
		results = append(results, cs)
	}
	return results, nil
}

func (s *StorageMonitorStore) GetDatabaseStats(ctx context.Context) (*DatabaseStats, error) {
	var stats DatabaseStats
	err := s.db.QueryRow(ctx, `
		SELECT
			current_database() AS database_name,
			pg_database_size(current_database()) AS database_size,
			pg_size_pretty(pg_database_size(current_database())) AS database_size_human,
			(SELECT count(*) FROM pg_stat_activity WHERE state = 'active') AS active_conns,
			(SELECT setting::bigint FROM pg_settings WHERE name = 'max_connections') AS max_conns
	`).Scan(&stats.DatabaseName, &stats.DatabaseSize, &stats.DatabaseHuman, &stats.ActiveConns, &stats.MaxConns)
	if err != nil {
		return nil, fmt.Errorf("store: get database stats: %w", err)
	}
	if stats.MaxConns > 0 {
		stats.ConnUsagePct = float64(stats.ActiveConns) / float64(stats.MaxConns) * 100.0
	}
	return &stats, nil
}

func (s *StorageMonitorStore) GetDiskUsage(ctx context.Context) (*DiskUsageInfo, error) {
	var info DiskUsageInfo
	err := s.db.QueryRow(ctx, `
		SELECT
			pg_database_size(current_database()) AS used_bytes
	`).Scan(&info.UsedBytes)
	if err != nil {
		return nil, fmt.Errorf("store: get disk usage: %w", err)
	}
	info.TotalBytes = info.UsedBytes
	info.AvailableBytes = 0
	return &info, nil
}

func (s *StorageMonitorStore) GetChunksOlderThan(ctx context.Context, hypertable string, olderThan time.Time) ([]ChunkInfo, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			ch.chunk_name,
			ch.hypertable_name::text,
			ch.range_start::timestamptz,
			ch.range_end::timestamptz,
			COALESCE(pg_total_relation_size(format('%I.%I', ch.chunk_schema, ch.chunk_name)), 0) AS total_bytes,
			ch.is_compressed
		FROM timescaledb_information.chunks ch
		WHERE ch.hypertable_name = $1
			AND ch.range_end < $2
		ORDER BY ch.range_start ASC
	`, hypertable, olderThan)
	if err != nil {
		return nil, fmt.Errorf("store: get chunks older than: %w", err)
	}
	defer rows.Close()

	var results []ChunkInfo
	for rows.Next() {
		var c ChunkInfo
		if err := rows.Scan(
			&c.ChunkName, &c.HypertableName,
			&c.RangeStart, &c.RangeEnd,
			&c.TotalBytes, &c.IsCompressed,
		); err != nil {
			return nil, fmt.Errorf("store: scan old chunk: %w", err)
		}
		results = append(results, c)
	}
	return results, nil
}

func (s *StorageMonitorStore) DropChunk(ctx context.Context, hypertable string, olderThan time.Time) (int64, error) {
	var dropped int64
	err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM drop_chunks($1::regclass, older_than => $2::timestamptz)
	`, hypertable, olderThan).Scan(&dropped)
	if err != nil {
		return 0, fmt.Errorf("store: drop chunks: %w", err)
	}
	return dropped, nil
}

func (s *StorageMonitorStore) GetCDRCountByTenantRange(ctx context.Context, olderThan time.Time) ([]TenantDataRange, error) {
	rows, err := s.db.Query(ctx, `
		SELECT tenant_id, COUNT(*) AS row_count, MIN(timestamp) AS oldest, MAX(timestamp) AS newest
		FROM cdrs
		WHERE timestamp < $1
		GROUP BY tenant_id
	`, olderThan)
	if err != nil {
		return nil, fmt.Errorf("store: get cdr count by tenant range: %w", err)
	}
	defer rows.Close()

	var results []TenantDataRange
	for rows.Next() {
		var t TenantDataRange
		if err := rows.Scan(&t.TenantID, &t.RowCount, &t.OldestRecord, &t.NewestRecord); err != nil {
			return nil, fmt.Errorf("store: scan tenant data range: %w", err)
		}
		results = append(results, t)
	}
	return results, nil
}

type TenantDataRange struct {
	TenantID     string    `json:"tenant_id"`
	RowCount     int64     `json:"row_count"`
	OldestRecord time.Time `json:"oldest_record"`
	NewestRecord time.Time `json:"newest_record"`
}
