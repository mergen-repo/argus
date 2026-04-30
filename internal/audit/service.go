package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog"
)

type AuditStore interface {
	CreateWithChain(ctx context.Context, entry *Entry) (*Entry, error)
	GetBatch(ctx context.Context, afterID int64, limit int) ([]Entry, error)
}

type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type MessageSubscriber interface {
	QueueSubscribe(subject, queue string, handler func(subject string, data []byte)) (Subscription, error)
}

type Subscription interface {
	Unsubscribe() error
}

type FullService struct {
	store     AuditStore
	publisher EventPublisher
	logger    zerolog.Logger
	sub       Subscription
	auditSubj string
}

func NewFullService(store AuditStore, publisher EventPublisher, logger zerolog.Logger) *FullService {
	return &FullService{
		store:     store,
		publisher: publisher,
		logger:    logger.With().Str("component", "audit_service").Logger(),
		auditSubj: "argus.events.audit.create",
	}
}

func (s *FullService) Start(ctx context.Context, subscriber MessageSubscriber) error {
	sub, err := subscriber.QueueSubscribe(s.auditSubj, "audit-writers", func(subject string, data []byte) {
		s.handleAuditEvent(subject, data)
	})
	if err != nil {
		return err
	}
	s.sub = sub
	s.logger.Info().Str("subject", s.auditSubj).Msg("audit consumer started")
	return nil
}

func (s *FullService) Stop() {
	if s.sub != nil {
		s.sub.Unsubscribe()
	}
}

func (s *FullService) handleAuditEvent(subject string, data []byte) {
	var event AuditEvent
	if err := json.Unmarshal(data, &event); err != nil {
		s.logger.Error().Err(err).Msg("unmarshal audit event")
		return
	}

	ctx := context.Background()
	if err := s.ProcessEntry(ctx, event); err != nil {
		s.logger.Error().Err(err).
			Str("action", event.Action).
			Str("entity_type", event.EntityType).
			Str("entity_id", event.EntityID).
			Msg("process audit entry failed")
	}
}

func (s *FullService) ProcessEntry(ctx context.Context, event AuditEvent) error {
	var beforeData, afterData json.RawMessage
	if len(event.BeforeData) > 0 {
		beforeData = event.BeforeData
	}
	if len(event.AfterData) > 0 {
		afterData = event.AfterData
	}

	diff := ComputeDiff(beforeData, afterData)

	entry := &Entry{
		TenantID:      event.TenantID,
		UserID:        event.UserID,
		APIKeyID:      event.APIKeyID,
		Action:        event.Action,
		EntityType:    event.EntityType,
		EntityID:      event.EntityID,
		BeforeData:    beforeData,
		AfterData:     afterData,
		Diff:          diff,
		IPAddress:     event.IPAddress,
		UserAgent:     event.UserAgent,
		CorrelationID: event.CorrelationID,
		CreatedAt:     time.Now().UTC(),
	}

	_, err := s.store.CreateWithChain(ctx, entry)
	return err
}

func (s *FullService) PublishAuditEvent(ctx context.Context, event AuditEvent) error {
	if s.publisher == nil {
		return nil
	}
	return s.publisher.Publish(ctx, s.auditSubj, event)
}

const verifyBatchSize = 5000

func (s *FullService) VerifyChain(ctx context.Context) (*VerifyResult, error) {
	result := &VerifyResult{Verified: true}
	prevHash := GenesisHash
	var afterID int64
	isFirst := true

	for {
		batch, err := s.store.GetBatch(ctx, afterID, verifyBatchSize)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}

		for _, entry := range batch {
			result.TotalRows++
			result.EntriesChecked++

			if isFirst {
				if entry.PrevHash != GenesisHash {
					result.Verified = false
					result.FirstInvalid = &entry.ID
					return result, nil
				}
				isFirst = false
			} else {
				if entry.PrevHash != prevHash {
					result.Verified = false
					result.FirstInvalid = &entry.ID
					return result, nil
				}
			}

			expectedHash := ComputeHash(entry, prevHash)
			if entry.Hash != expectedHash {
				result.Verified = false
				result.FirstInvalid = &entry.ID
				return result, nil
			}

			prevHash = entry.Hash
		}

		afterID = batch[len(batch)-1].ID
	}

	return result, nil
}

func (s *FullService) CreateEntry(_ context.Context, p CreateEntryParams) (*Entry, error) {
	event := AuditEvent{
		TenantID:      p.TenantID,
		UserID:        p.UserID,
		APIKeyID:      p.APIKeyID,
		Action:        p.Action,
		EntityType:    p.EntityType,
		EntityID:      p.EntityID,
		BeforeData:    p.BeforeData,
		AfterData:     p.AfterData,
		IPAddress:     p.IPAddress,
		UserAgent:     p.UserAgent,
		CorrelationID: p.CorrelationID,
	}

	ctx := context.Background()
	if s.publisher != nil {
		if err := s.PublishAuditEvent(ctx, event); err != nil {
			s.logger.Warn().Err(err).Msg("publish audit event failed, processing inline")
			if processErr := s.ProcessEntry(ctx, event); processErr != nil {
				return nil, processErr
			}
		}
	} else {
		if err := s.ProcessEntry(ctx, event); err != nil {
			return nil, err
		}
	}

	return &Entry{}, nil
}
