package job

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestStorageMonitorProcessor_Type(t *testing.T) {
	p := &StorageMonitorProcessor{}
	if p.Type() != JobTypeStorageMonitor {
		t.Errorf("expected type %s, got %s", JobTypeStorageMonitor, p.Type())
	}
}

func TestStorageMonitorResult_Fields(t *testing.T) {
	result := storageMonitorResult{
		DatabaseSize:       1024 * 1024 * 500,
		DatabaseSizeHuman:  "500 MB",
		ActiveConnections:  42,
		MaxConnections:     200,
		ConnectionUsagePct: 21.0,
		CompressionStats: []compressionInfo{
			{
				Hypertable:       "cdrs",
				CompressionRatio: 12.5,
				CompressedChunks: 10,
				TotalChunks:      12,
			},
		},
		LargestTables: []tableInfo{
			{Name: "cdrs", Size: "200 MB", Rows: 1000000},
			{Name: "sessions", Size: "100 MB", Rows: 500000},
		},
		AlertsTriggered: 0,
	}

	if result.ConnectionUsagePct != 21.0 {
		t.Errorf("expected connection usage 21%%, got %.1f%%", result.ConnectionUsagePct)
	}
	if len(result.CompressionStats) != 1 {
		t.Errorf("expected 1 compression stat, got %d", len(result.CompressionStats))
	}
	if result.CompressionStats[0].CompressionRatio < 10.0 {
		t.Errorf("expected compression ratio > 10:1, got %.1f", result.CompressionStats[0].CompressionRatio)
	}
	if len(result.LargestTables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(result.LargestTables))
	}
}

func TestStorageMonitorProcessor_DefaultAlertPct(t *testing.T) {
	p := NewStorageMonitorProcessor(nil, nil, nil, 0, zerolog.Nop())
	if p.alertPct != 80.0 {
		t.Errorf("expected default alert pct 80, got %.1f", p.alertPct)
	}
}

func TestStorageMonitorProcessor_CustomAlertPct(t *testing.T) {
	p := NewStorageMonitorProcessor(nil, nil, nil, 90.0, zerolog.Nop())
	if p.alertPct != 90.0 {
		t.Errorf("expected alert pct 90, got %.1f", p.alertPct)
	}
}
