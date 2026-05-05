package cdr

import (
	"context"
	"encoding/json"
	"time"

	"github.com/btopcu/argus/internal/aaa/validator"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type MessageSubscriber interface {
	QueueSubscribe(subject, queue string, handler func(subject string, data []byte)) (Subscription, error)
}

type Subscription interface {
	Unsubscribe() error
}

type Consumer struct {
	cdrStore      *store.CDRStore
	operatorStore *store.OperatorStore
	reg           *obsmetrics.Registry
	imsiStrict    bool
	logger        zerolog.Logger
	subs          []Subscription
}

func NewConsumer(cdrStore *store.CDRStore, operatorStore *store.OperatorStore, logger zerolog.Logger, reg *obsmetrics.Registry, imsiStrict bool) *Consumer {
	return &Consumer{
		cdrStore:      cdrStore,
		operatorStore: operatorStore,
		reg:           reg,
		imsiStrict:    imsiStrict,
		logger:        logger.With().Str("component", "cdr_consumer").Logger(),
	}
}

func (c *Consumer) Start(subscriber MessageSubscriber) error {
	subjects := []string{
		"argus.events.session.started",
		"argus.events.session.updated",
		"argus.events.session.ended",
	}

	for _, subj := range subjects {
		sub, err := subscriber.QueueSubscribe(subj, "cdr-consumer", func(subject string, data []byte) {
			c.handleEvent(subject, data)
		})
		if err != nil {
			return err
		}
		c.subs = append(c.subs, sub)
	}

	c.logger.Info().Strs("subjects", subjects).Msg("cdr consumer started")
	return nil
}

func (c *Consumer) Stop() {
	for _, sub := range c.subs {
		sub.Unsubscribe()
	}
	c.logger.Info().Msg("cdr consumer stopped")
}

type sessionEvent struct {
	SessionID      string `json:"session_id"`
	SimID          string `json:"sim_id"`
	TenantID       string `json:"tenant_id"`
	OperatorID     string `json:"operator_id"`
	APNID          string `json:"apn_id,omitempty"`
	IMSI           string `json:"imsi,omitempty"`
	RATType        string `json:"rat_type,omitempty"`
	BytesIn        int64  `json:"bytes_in"`
	BytesOut       int64  `json:"bytes_out"`
	DurationSec    int    `json:"duration_sec"`
	TerminateCause string `json:"terminate_cause,omitempty"`
	ProtocolType   string `json:"protocol_type,omitempty"`
	Timestamp      string `json:"timestamp,omitempty"`
	EndedAt        string `json:"ended_at,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
}

func (c *Consumer) handleEvent(subject string, data []byte) {
	var evt sessionEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		c.logger.Error().Err(err).Str("subject", subject).Msg("unmarshal session event")
		return
	}

	// FIX-207 AC-4: drop events with malformed IMSI when strict validation is active.
	if evt.IMSI != "" && c.imsiStrict && !validator.IsIMSIFormatValid(evt.IMSI) {
		c.logger.Warn().Str("imsi", evt.IMSI).Msg("cdr event: malformed imsi — dropping")
		c.reg.IncIMSIInvalid("cdr")
		return
	}

	var recordType string
	switch subject {
	case "argus.events.session.started":
		recordType = "start"
	case "argus.events.session.updated":
		recordType = "interim"
	case "argus.events.session.ended":
		recordType = "stop"
	default:
		c.logger.Warn().Str("subject", subject).Msg("unknown subject")
		return
	}

	sessionID, err := uuid.Parse(evt.SessionID)
	if err != nil {
		c.logger.Error().Err(err).Str("session_id", evt.SessionID).Msg("parse session_id")
		return
	}

	simID, err := uuid.Parse(evt.SimID)
	if err != nil {
		c.logger.Error().Err(err).Str("sim_id", evt.SimID).Msg("parse sim_id")
		return
	}

	tenantID, err := uuid.Parse(evt.TenantID)
	if err != nil {
		c.logger.Error().Err(err).Str("tenant_id", evt.TenantID).Msg("parse tenant_id")
		return
	}

	operatorID, err := uuid.Parse(evt.OperatorID)
	if err != nil {
		c.logger.Error().Err(err).Str("operator_id", evt.OperatorID).Msg("parse operator_id")
		return
	}

	var apnID *uuid.UUID
	if evt.APNID != "" {
		if parsed, parseErr := uuid.Parse(evt.APNID); parseErr == nil {
			apnID = &parsed
		}
	}

	var ratType *string
	if evt.RATType != "" {
		ratType = &evt.RATType
	}

	ts := time.Now().UTC()
	if evt.Timestamp != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, evt.Timestamp); parseErr == nil {
			ts = parsed
		}
	}
	if evt.EndedAt != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, evt.EndedAt); parseErr == nil {
			ts = parsed
		}
	}
	if evt.StartedAt != "" && recordType == "start" {
		if parsed, parseErr := time.Parse(time.RFC3339, evt.StartedAt); parseErr == nil {
			ts = parsed
		}
	}

	var usageCost, carrierCost, ratePerMB, ratMult *float64

	if recordType != "start" && (evt.BytesIn > 0 || evt.BytesOut > 0) {
		rating := c.calculateCost(tenantID, operatorID, evt.BytesIn, evt.BytesOut, evt.RATType, ts, sessionID)
		if rating != nil {
			usageCost = &rating.UsageCost
			carrierCost = &rating.CarrierCost
			ratePerMB = &rating.RatePerMB
			ratMult = &rating.RATMultiplier
		}
	}

	params := store.CreateCDRParams{
		SessionID:     sessionID,
		SimID:         simID,
		TenantID:      tenantID,
		OperatorID:    operatorID,
		APNID:         apnID,
		RATType:       ratType,
		RecordType:    recordType,
		BytesIn:       evt.BytesIn,
		BytesOut:      evt.BytesOut,
		DurationSec:   evt.DurationSec,
		UsageCost:     usageCost,
		CarrierCost:   carrierCost,
		RatePerMB:     ratePerMB,
		RATMultiplier: ratMult,
		Timestamp:     ts,
	}

	ctx := context.Background()
	_, err = c.cdrStore.CreateIdempotent(ctx, params)
	if err != nil {
		c.logger.Error().Err(err).
			Str("session_id", evt.SessionID).
			Str("record_type", recordType).
			Msg("create cdr failed")
		return
	}

	c.logger.Debug().
		Str("session_id", evt.SessionID).
		Str("record_type", recordType).
		Int64("bytes_in", evt.BytesIn).
		Int64("bytes_out", evt.BytesOut).
		Msg("cdr created")
}

func (c *Consumer) calculateCost(tenantID, operatorID uuid.UUID, bytesIn, bytesOut int64, ratType string, ts time.Time, sessionID uuid.UUID) *RatingResult {
	ctx := context.Background()

	costPerMB := 0.0
	grants, err := c.operatorStore.ListGrants(ctx, tenantID)
	if err != nil {
		c.logger.Warn().Err(err).Str("tenant_id", tenantID.String()).Msg("lookup operator grants for rating")
	} else {
		for _, g := range grants {
			if g.OperatorID == operatorID && g.CostPerMB != nil {
				costPerMB = *g.CostPerMB
				break
			}
		}
	}

	if costPerMB == 0 {
		return &RatingResult{
			UsageCost:     0,
			CarrierCost:   0,
			RatePerMB:     0,
			RATMultiplier: 1.0,
			TotalMB:       float64(bytesIn+bytesOut) / (1024.0 * 1024.0),
		}
	}

	cumulativeBytes, err := c.cdrStore.GetCumulativeSessionBytes(ctx, sessionID)
	if err != nil {
		c.logger.Warn().Err(err).Str("session_id", sessionID.String()).Msg("get cumulative bytes for rating")
		cumulativeBytes = 0
	}

	rc := NewRatingConfig(costPerMB)
	return rc.Calculate(bytesIn, bytesOut, ratType, ts, cumulativeBytes)
}
