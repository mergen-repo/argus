package audit

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type AuditStore interface {
	Create(ctx context.Context, entry *Entry) (*Entry, error)
	GetLastHash(ctx context.Context, tenantID uuid.UUID) (string, error)
	GetRange(ctx context.Context, tenantID uuid.UUID, count int) ([]Entry, error)
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
	store      AuditStore
	publisher  EventPublisher
	logger     zerolog.Logger
	tenantMu   sync.Map
	sub        Subscription
	auditSubj  string
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

func (s *FullService) getTenantMutex(tenantID uuid.UUID) *sync.Mutex {
	val, _ := s.tenantMu.LoadOrStore(tenantID, &sync.Mutex{})
	return val.(*sync.Mutex)
}

func (s *FullService) ProcessEntry(ctx context.Context, event AuditEvent) error {
	mu := s.getTenantMutex(event.TenantID)
	mu.Lock()
	defer mu.Unlock()

	prevHash, err := s.store.GetLastHash(ctx, event.TenantID)
	if err != nil {
		return err
	}

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
		PrevHash:      prevHash,
		CreatedAt:     time.Now().UTC(),
	}

	entry.Hash = ComputeHash(*entry, prevHash)

	_, err = s.store.Create(ctx, entry)
	return err
}

func (s *FullService) PublishAuditEvent(ctx context.Context, event AuditEvent) error {
	if s.publisher == nil {
		return nil
	}
	return s.publisher.Publish(ctx, s.auditSubj, event)
}

func (s *FullService) VerifyChain(ctx context.Context, tenantID uuid.UUID, count int) (*VerifyResult, error) {
	entries, err := s.store.GetRange(ctx, tenantID, count)
	if err != nil {
		return nil, err
	}
	return VerifyChain(entries), nil
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
