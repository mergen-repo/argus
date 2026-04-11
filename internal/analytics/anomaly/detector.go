package anomaly

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	keyPrefixAuthIMSI    = "anomaly:auth:imsi:"
	keyPrefixAuthNAS     = "anomaly:auth:nas:"
	keyPrefixCloningIMSI = "anomaly:cloning:imsi:"
)

type RealtimeDetector struct {
	redis      *redis.Client
	thresholds ThresholdConfig
	logger     zerolog.Logger
}

func NewRealtimeDetector(rdb *redis.Client, thresholds ThresholdConfig, logger zerolog.Logger) *RealtimeDetector {
	return &RealtimeDetector{
		redis:      rdb,
		thresholds: thresholds,
		logger:     logger.With().Str("component", "anomaly_realtime_detector").Logger(),
	}
}

func (d *RealtimeDetector) SetThresholds(t ThresholdConfig) {
	d.thresholds = t
}

type DetectionResult struct {
	Type     string
	Severity string
	SimID    *uuid.UUID
	TenantID uuid.UUID
	Details  map[string]interface{}
}

func (d *RealtimeDetector) CheckAuth(ctx context.Context, evt AuthEvent) ([]DetectionResult, error) {
	if d.redis == nil {
		return nil, nil
	}

	if d.thresholds.FilterBulkJobs && evt.Source == "bulk_job" {
		return nil, nil
	}

	var results []DetectionResult

	cloningResult, err := d.checkSIMCloning(ctx, evt)
	if err != nil {
		d.logger.Warn().Err(err).Str("imsi", evt.IMSI).Msg("cloning check failed")
	} else if cloningResult != nil {
		results = append(results, *cloningResult)
	}

	authFloodResult, err := d.checkAuthFlood(ctx, evt)
	if err != nil {
		d.logger.Warn().Err(err).Str("imsi", evt.IMSI).Msg("auth flood check failed")
	} else if authFloodResult != nil {
		results = append(results, *authFloodResult)
	}

	nasFloodResult, err := d.checkNASFlood(ctx, evt)
	if err != nil {
		d.logger.Warn().Err(err).Str("nas_ip", evt.NASIP).Msg("NAS flood check failed")
	} else if nasFloodResult != nil {
		results = append(results, *nasFloodResult)
	}

	return results, nil
}

func (d *RealtimeDetector) checkSIMCloning(ctx context.Context, evt AuthEvent) (*DetectionResult, error) {
	if evt.IMSI == "" || evt.NASIP == "" {
		return nil, nil
	}

	windowSec := d.thresholds.CloningWindowSec
	if windowSec <= 0 {
		windowSec = 300
	}

	key := keyPrefixCloningIMSI + evt.IMSI
	now := time.Now()
	member := fmt.Sprintf("%s:%d", evt.NASIP, now.UnixNano())

	pipe := d.redis.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixNano()), Member: member})
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", now.Add(-time.Duration(windowSec)*time.Second).UnixNano()))
	pipe.Expire(ctx, key, time.Duration(windowSec*2)*time.Second)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloning pipeline: %w", err)
	}

	members, err := d.redis.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%d", now.Add(-time.Duration(windowSec)*time.Second).UnixNano()),
		Max: fmt.Sprintf("%d", now.UnixNano()),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("cloning zrange: %w", err)
	}

	nasIPs := make(map[string]bool)
	for _, m := range members {
		ip := extractIP(m)
		if ip != "" {
			nasIPs[ip] = true
		}
	}

	if len(nasIPs) >= 2 {
		ipList := make([]string, 0, len(nasIPs))
		for ip := range nasIPs {
			ipList = append(ipList, ip)
		}

		return &DetectionResult{
			Type:     TypeSIMCloning,
			Severity: SeverityCritical,
			SimID:    &evt.SimID,
			TenantID: evt.TenantID,
			Details: map[string]interface{}{
				"imsi":         evt.IMSI,
				"nas_ips":      ipList,
				"window_sec":   windowSec,
				"ip_count":     len(nasIPs),
				"current_ip":   evt.NASIP,
			},
		}, nil
	}

	return nil, nil
}

func (d *RealtimeDetector) checkAuthFlood(ctx context.Context, evt AuthEvent) (*DetectionResult, error) {
	if evt.IMSI == "" {
		return nil, nil
	}

	maxAuth := d.thresholds.AuthFloodMax
	windowSec := d.thresholds.AuthFloodWindowSec
	if maxAuth <= 0 {
		maxAuth = 100
	}
	if windowSec <= 0 {
		windowSec = 60
	}

	key := keyPrefixAuthIMSI + evt.IMSI
	count, err := d.incrementSlidingWindow(ctx, key, windowSec)
	if err != nil {
		return nil, fmt.Errorf("auth flood sliding window: %w", err)
	}

	if count > int64(maxAuth) {
		return &DetectionResult{
			Type:     TypeAuthFlood,
			Severity: SeverityHigh,
			SimID:    &evt.SimID,
			TenantID: evt.TenantID,
			Details: map[string]interface{}{
				"imsi":       evt.IMSI,
				"count":      count,
				"threshold":  maxAuth,
				"window_sec": windowSec,
			},
		}, nil
	}

	return nil, nil
}

func (d *RealtimeDetector) checkNASFlood(ctx context.Context, evt AuthEvent) (*DetectionResult, error) {
	if evt.NASIP == "" {
		return nil, nil
	}

	maxAuth := d.thresholds.NASFloodMax
	windowSec := d.thresholds.NASFloodWindowSec
	if maxAuth <= 0 {
		maxAuth = 1000
	}
	if windowSec <= 0 {
		windowSec = 60
	}

	key := keyPrefixAuthNAS + evt.NASIP
	count, err := d.incrementSlidingWindow(ctx, key, windowSec)
	if err != nil {
		return nil, fmt.Errorf("NAS flood sliding window: %w", err)
	}

	if count > int64(maxAuth) {
		return &DetectionResult{
			Type:     TypeNASFlood,
			Severity: SeverityHigh,
			TenantID: evt.TenantID,
			Details: map[string]interface{}{
				"nas_ip":     evt.NASIP,
				"count":      count,
				"threshold":  maxAuth,
				"window_sec": windowSec,
			},
		}, nil
	}

	return nil, nil
}

func (d *RealtimeDetector) incrementSlidingWindow(ctx context.Context, key string, windowSec int) (int64, error) {
	now := time.Now()
	member := fmt.Sprintf("%d", now.UnixNano())
	cutoff := now.Add(-time.Duration(windowSec) * time.Second)

	pipe := d.redis.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixNano()), Member: member})
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", cutoff.UnixNano()))
	countCmd := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, time.Duration(windowSec*2)*time.Second)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}

	return countCmd.Val(), nil
}

func extractIP(member string) string {
	host, _, err := net.SplitHostPort(member)
	if err == nil {
		return host
	}
	if ip := net.ParseIP(member); ip != nil {
		return ip.String()
	}
	return member
}
