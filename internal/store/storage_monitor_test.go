package store

import (
	"testing"
	"time"
)

func TestTableSizeInfo_Fields(t *testing.T) {
	info := TableSizeInfo{
		TableName:      "cdrs",
		TotalSize:      1024 * 1024 * 100,
		TotalSizeHuman: "100 MB",
		TableSize:      1024 * 1024 * 80,
		TableSizeHuman: "80 MB",
		IndexSize:      1024 * 1024 * 20,
		IndexSizeHuman: "20 MB",
		RowEstimate:    1000000,
	}

	if info.TableName != "cdrs" {
		t.Errorf("expected table name cdrs, got %s", info.TableName)
	}
	if info.TotalSize != 104857600 {
		t.Errorf("expected total size 104857600, got %d", info.TotalSize)
	}
	if info.RowEstimate != 1000000 {
		t.Errorf("expected row estimate 1000000, got %d", info.RowEstimate)
	}
}

func TestChunkInfo_Fields(t *testing.T) {
	now := time.Now()
	info := ChunkInfo{
		ChunkName:      "_hyper_1_1_chunk",
		HypertableName: "cdrs",
		RangeStart:     now.Add(-24 * time.Hour),
		RangeEnd:       now,
		TotalBytes:     1024 * 1024 * 50,
		IsCompressed:   true,
	}

	if info.ChunkName != "_hyper_1_1_chunk" {
		t.Errorf("expected chunk name _hyper_1_1_chunk, got %s", info.ChunkName)
	}
	if !info.IsCompressed {
		t.Error("expected chunk to be compressed")
	}
	if info.HypertableName != "cdrs" {
		t.Errorf("expected hypertable cdrs, got %s", info.HypertableName)
	}
}

func TestCompressionStats_Fields(t *testing.T) {
	stats := CompressionStats{
		HypertableName:     "cdrs",
		UncompressedBytes:  1024 * 1024 * 1000,
		CompressedBytes:    1024 * 1024 * 80,
		CompressionRatio:   12.5,
		CompressedChunks:   10,
		UncompressedChunks: 2,
	}

	if stats.CompressionRatio < 10.0 {
		t.Errorf("expected compression ratio > 10:1, got %.1f", stats.CompressionRatio)
	}
	if stats.CompressedChunks+stats.UncompressedChunks != 12 {
		t.Errorf("expected total 12 chunks, got %d", stats.CompressedChunks+stats.UncompressedChunks)
	}
}

func TestDatabaseStats_ConnUsage(t *testing.T) {
	stats := DatabaseStats{
		DatabaseName:  "argus",
		DatabaseSize:  1024 * 1024 * 500,
		DatabaseHuman: "500 MB",
		ActiveConns:   40,
		MaxConns:      200,
	}

	if stats.MaxConns > 0 {
		stats.ConnUsagePct = float64(stats.ActiveConns) / float64(stats.MaxConns) * 100.0
	}

	if stats.ConnUsagePct != 20.0 {
		t.Errorf("expected connection usage 20%%, got %.1f%%", stats.ConnUsagePct)
	}
}

func TestTenantDataRange_Fields(t *testing.T) {
	r := TenantDataRange{
		TenantID:     "550e8400-e29b-41d4-a716-446655440000",
		RowCount:     150000,
		OldestRecord: time.Now().Add(-365 * 24 * time.Hour),
		NewestRecord: time.Now(),
	}

	if r.RowCount != 150000 {
		t.Errorf("expected 150000 rows, got %d", r.RowCount)
	}
}
