package job

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type CronEntry struct {
	Name     string
	Schedule string
	JobType  string
	TenantID *uuid.UUID
	Payload  json.RawMessage
}

type Scheduler struct {
	jobs     *store.JobStore
	eventBus *bus.EventBus
	rdb      *redis.Client
	entries  []CronEntry
	logger   zerolog.Logger
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewScheduler(jobs *store.JobStore, eventBus *bus.EventBus, rdb *redis.Client, logger zerolog.Logger) *Scheduler {
	return &Scheduler{
		jobs:     jobs,
		eventBus: eventBus,
		rdb:      rdb,
		logger:   logger.With().Str("component", "cron_scheduler").Logger(),
		stopCh:   make(chan struct{}),
	}
}

func (s *Scheduler) AddEntry(entry CronEntry) {
	s.entries = append(s.entries, entry)
	s.logger.Info().
		Str("name", entry.Name).
		Str("schedule", entry.Schedule).
		Str("job_type", entry.JobType).
		Msg("cron entry registered")
}

func (s *Scheduler) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run()
	}()
	s.logger.Info().Int("entries", len(s.entries)).Msg("cron scheduler started")
}

func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
	s.logger.Info().Msg("cron scheduler stopped")
}

func (s *Scheduler) run() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			for _, entry := range s.entries {
				if shouldFire(entry.Schedule, now) {
					s.fireEntry(entry, now)
				}
			}
		}
	}
}

func (s *Scheduler) fireEntry(entry CronEntry, now time.Time) {
	ctx := context.Background()

	dedupKey := fmt.Sprintf("argus:cron:last:%s:%s", entry.JobType, now.Truncate(time.Minute).Format("200601021504"))
	ok, err := s.rdb.SetNX(ctx, dedupKey, "1", 2*time.Minute).Result()
	if err != nil {
		s.logger.Error().Err(err).Str("entry", entry.Name).Msg("cron dedup check failed")
		return
	}
	if !ok {
		s.logger.Debug().Str("entry", entry.Name).Msg("cron entry already fired this minute")
		return
	}

	payload := entry.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}

	tenantID := uuid.Nil
	if entry.TenantID != nil {
		tenantID = *entry.TenantID
	}

	job, err := s.jobs.CreateWithTenantID(ctx, tenantID, store.CreateJobParams{
		Type:       entry.JobType,
		Priority:   3,
		Payload:    payload,
		TotalItems: 0,
	})
	if err != nil {
		s.logger.Error().Err(err).Str("entry", entry.Name).Msg("create scheduled job failed")
		return
	}

	if s.eventBus != nil {
		_ = s.eventBus.Publish(ctx, bus.SubjectJobQueue, JobMessage{
			JobID:    job.ID,
			TenantID: job.TenantID,
			Type:     entry.JobType,
		})
	}

	s.logger.Info().
		Str("entry", entry.Name).
		Str("job_id", job.ID.String()).
		Str("type", entry.JobType).
		Msg("scheduled job created")
}

func shouldFire(schedule string, now time.Time) bool {
	switch schedule {
	case "@hourly":
		return now.Minute() == 0
	case "@daily":
		return now.Hour() == 2 && now.Minute() == 0
	case "@weekly":
		return now.Weekday() == time.Monday && now.Hour() == 2 && now.Minute() == 0
	case "@monthly":
		return now.Day() == 1 && now.Hour() == 2 && now.Minute() == 0
	default:
		return matchCronExpr(schedule, now)
	}
}

func matchCronExpr(expr string, now time.Time) bool {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return false
	}

	minute := now.Minute()
	hour := now.Hour()
	dom := now.Day()
	month := int(now.Month())
	dow := int(now.Weekday())

	return fieldMatches(parts[0], minute, 0, 59) &&
		fieldMatches(parts[1], hour, 0, 23) &&
		fieldMatches(parts[2], dom, 1, 31) &&
		fieldMatches(parts[3], month, 1, 12) &&
		fieldMatches(parts[4], dow, 0, 6)
}

func fieldMatches(field string, value, min, max int) bool {
	if field == "*" {
		return true
	}

	if strings.Contains(field, "/") {
		parts := strings.SplitN(field, "/", 2)
		step, err := strconv.Atoi(parts[1])
		if err != nil || step <= 0 {
			return false
		}
		base := min
		if parts[0] != "*" {
			b, err := strconv.Atoi(parts[0])
			if err != nil {
				return false
			}
			base = b
		}
		if value < base {
			return false
		}
		return (value-base)%step == 0
	}

	if strings.Contains(field, ",") {
		for _, part := range strings.Split(field, ",") {
			v, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil {
				continue
			}
			if v == value {
				return true
			}
		}
		return false
	}

	if strings.Contains(field, "-") {
		parts := strings.SplitN(field, "-", 2)
		low, err1 := strconv.Atoi(parts[0])
		high, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return false
		}
		return value >= low && value <= high
	}

	v, err := strconv.Atoi(field)
	if err != nil {
		return false
	}
	return v == value
}
