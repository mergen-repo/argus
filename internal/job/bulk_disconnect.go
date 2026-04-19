package job

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)


type BulkDisconnectPayload struct {
	SimIDs    []string `json:"sim_ids"`
	SegmentID *string  `json:"segment_id,omitempty"`
	Reason    string   `json:"reason"`
}

type BulkDisconnectResult struct {
	TotalSessions     int `json:"total_sessions"`
	DisconnectedCount int `json:"disconnected_count"`
	FailedCount       int `json:"failed_count"`
}

type BulkDisconnectProcessor struct {
	jobs       *store.JobStore
	sessionMgr *session.Manager
	dmSender   *session.DMSender
	eventBus   *bus.EventBus
	logger     zerolog.Logger
}

func NewBulkDisconnectProcessor(
	jobs *store.JobStore,
	sessionMgr *session.Manager,
	dmSender *session.DMSender,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *BulkDisconnectProcessor {
	return &BulkDisconnectProcessor{
		jobs:       jobs,
		sessionMgr: sessionMgr,
		dmSender:   dmSender,
		eventBus:   eventBus,
		logger:     logger.With().Str("processor", JobTypeBulkDisconnect).Logger(),
	}
}

func (p *BulkDisconnectProcessor) Type() string {
	return JobTypeBulkDisconnect
}

func (p *BulkDisconnectProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload BulkDisconnectPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal disconnect payload: %w", err)
	}

	if payload.Reason == "" {
		payload.Reason = "bulk_disconnect"
	}

	totalSessions := 0
	disconnected := 0
	failed := 0

	for i, simID := range payload.SimIDs {
		if (i+1)%100 == 0 {
			cancelled, checkErr := p.jobs.CheckCancelled(ctx, job.ID)
			if checkErr == nil && cancelled {
				p.logger.Info().Int("index", i).Msg("job cancelled, stopping disconnect")
				break
			}
		}

		sessions, err := p.sessionMgr.GetSessionsForSIM(ctx, simID)
		if err != nil {
			p.logger.Warn().Err(err).Str("sim_id", simID).Msg("get sessions for sim")
			failed++
			continue
		}

		for _, sess := range sessions {
			if sess.SessionState != "active" {
				continue
			}
			totalSessions++

			if p.dmSender != nil && sess.NASIP != "" && sess.AcctSessionID != "" {
				nasIP := sess.NASIP
				if idx := strings.Index(nasIP, ":"); idx > 0 {
					nasIP = nasIP[:idx]
				}
				sessTenantID, _ := uuid.Parse(sess.TenantID)
				_, _ = p.dmSender.SendDM(ctx, session.DMRequest{
					NASIP:         nasIP,
					AcctSessionID: sess.AcctSessionID,
					IMSI:          sess.IMSI,
					SessionID:     sess.ID,
					TenantID:      sessTenantID,
				})
			}

			if err := p.sessionMgr.Terminate(ctx, sess.ID, payload.Reason); err != nil {
				p.logger.Warn().Err(err).Str("session_id", sess.ID).Msg("terminate session")
				failed++
				continue
			}

			if p.eventBus != nil {
				_ = p.eventBus.Publish(ctx, bus.SubjectSessionEnded, map[string]interface{}{
					"session_id":      sess.ID,
					"sim_id":          sess.SimID,
					"tenant_id":       sess.TenantID,
					"operator_id":     sess.OperatorID,
					"imsi":            sess.IMSI,
					"terminate_cause": payload.Reason,
				})
			}
			disconnected++
		}

		if (i+1)%100 == 0 || i == len(payload.SimIDs)-1 {
			_ = p.jobs.UpdateProgress(ctx, job.ID, disconnected, failed, len(payload.SimIDs))
		}
	}

	resultJSON, _ := json.Marshal(BulkDisconnectResult{
		TotalSessions:     totalSessions,
		DisconnectedCount: disconnected,
		FailedCount:       failed,
	})

	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":             job.ID.String(),
			"tenant_id":         job.TenantID.String(),
			"type":              JobTypeBulkDisconnect,
			"state":             "completed",
			"disconnected_count": disconnected,
			"failed_count":      failed,
		})
	}

	return nil
}
