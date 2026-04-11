package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type StorageMonitorProcessor struct {
	jobs         *store.JobStore
	storageStore *store.StorageMonitorStore
	eventBus     *bus.EventBus
	alertPct     float64
	logger       zerolog.Logger
}

func NewStorageMonitorProcessor(
	jobs *store.JobStore,
	storageStore *store.StorageMonitorStore,
	eventBus *bus.EventBus,
	alertPct float64,
	logger zerolog.Logger,
) *StorageMonitorProcessor {
	if alertPct <= 0 {
		alertPct = 80.0
	}
	return &StorageMonitorProcessor{
		jobs:         jobs,
		storageStore: storageStore,
		eventBus:     eventBus,
		alertPct:     alertPct,
		logger:       logger.With().Str("processor", JobTypeStorageMonitor).Logger(),
	}
}

func (p *StorageMonitorProcessor) Type() string {
	return JobTypeStorageMonitor
}

type storageMonitorResult struct {
	DatabaseSize       int64              `json:"database_size"`
	DatabaseSizeHuman  string             `json:"database_size_human"`
	ActiveConnections  int64              `json:"active_connections"`
	MaxConnections     int64              `json:"max_connections"`
	ConnectionUsagePct float64            `json:"connection_usage_pct"`
	CompressionStats   []compressionInfo  `json:"compression_stats"`
	LargestTables      []tableInfo        `json:"largest_tables"`
	AlertsTriggered    int                `json:"alerts_triggered"`
}

type compressionInfo struct {
	Hypertable       string  `json:"hypertable"`
	CompressionRatio float64 `json:"compression_ratio"`
	CompressedChunks int64   `json:"compressed_chunks"`
	TotalChunks      int64   `json:"total_chunks"`
}

type tableInfo struct {
	Name     string `json:"name"`
	Size     string `json:"size"`
	Rows     int64  `json:"rows"`
}

func (p *StorageMonitorProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().
		Str("job_id", job.ID.String()).
		Float64("alert_pct", p.alertPct).
		Msg("starting storage monitoring check")

	result := storageMonitorResult{}

	dbStats, err := p.storageStore.GetDatabaseStats(ctx)
	if err != nil {
		p.logger.Error().Err(err).Msg("get database stats failed")
	} else {
		result.DatabaseSize = dbStats.DatabaseSize
		result.DatabaseSizeHuman = dbStats.DatabaseHuman
		result.ActiveConnections = dbStats.ActiveConns
		result.MaxConnections = dbStats.MaxConns
		result.ConnectionUsagePct = dbStats.ConnUsagePct
	}

	compressionStats, err := p.storageStore.GetCompressionStats(ctx)
	if err != nil {
		p.logger.Warn().Err(err).Msg("get compression stats failed")
	} else {
		for _, cs := range compressionStats {
			result.CompressionStats = append(result.CompressionStats, compressionInfo{
				Hypertable:       cs.HypertableName,
				CompressionRatio: cs.CompressionRatio,
				CompressedChunks: cs.CompressedChunks,
				TotalChunks:      cs.CompressedChunks + cs.UncompressedChunks,
			})
		}
	}

	tableSizes, err := p.storageStore.GetTableSizes(ctx)
	if err != nil {
		p.logger.Warn().Err(err).Msg("get table sizes failed")
	} else {
		maxTables := 10
		if len(tableSizes) < maxTables {
			maxTables = len(tableSizes)
		}
		for _, t := range tableSizes[:maxTables] {
			result.LargestTables = append(result.LargestTables, tableInfo{
				Name: t.TableName,
				Size: t.TotalSizeHuman,
				Rows: t.RowEstimate,
			})
		}
	}

	alertsTriggered := 0

	if dbStats != nil && dbStats.ConnUsagePct > p.alertPct {
		alertsTriggered++
		p.sendStorageAlert(ctx, "connection_pool_high",
			fmt.Sprintf("Database connection usage at %.1f%% (threshold: %.0f%%)", dbStats.ConnUsagePct, p.alertPct),
			"warning",
		)
	}

	for _, cs := range compressionStats {
		if cs.CompressionRatio > 0 && cs.CompressionRatio < 5.0 && cs.CompressedChunks > 0 {
			alertsTriggered++
			p.sendStorageAlert(ctx, "low_compression_ratio",
				fmt.Sprintf("Hypertable %s has low compression ratio: %.1f:1", cs.HypertableName, cs.CompressionRatio),
				"info",
			)
		}
	}

	result.AlertsTriggered = alertsTriggered

	resultJSON, _ := json.Marshal(result)
	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete storage monitor job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":            job.ID.String(),
			"tenant_id":        job.TenantID.String(),
			"type":             JobTypeStorageMonitor,
			"state":            "completed",
			"database_size":    result.DatabaseSizeHuman,
			"alerts_triggered": alertsTriggered,
		})
	}

	p.logger.Info().
		Str("db_size", result.DatabaseSizeHuman).
		Int64("active_conns", result.ActiveConnections).
		Int("alerts", alertsTriggered).
		Msg("storage monitoring completed")

	return nil
}

func (p *StorageMonitorProcessor) sendStorageAlert(ctx context.Context, alertType, description, severity string) {
	if p.eventBus == nil {
		return
	}

	_ = p.eventBus.Publish(ctx, bus.SubjectAlertTriggered, map[string]interface{}{
		"alert_type":  "storage." + alertType,
		"tenant_id":   nil,
		"severity":    severity,
		"title":       "Storage Alert: " + alertType,
		"description": description,
		"entity_type": "system",
	})
}
