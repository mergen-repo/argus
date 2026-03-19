package audit

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

type CreateEntryParams struct {
	TenantID   uuid.UUID
	UserID     *uuid.UUID
	Action     string
	EntityType string
	EntityID   string
	BeforeData json.RawMessage
	AfterData  json.RawMessage
	IPAddress  *string
	UserAgent  *string
}

type Entry struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserID     *uuid.UUID
	Action     string
	EntityType string
	EntityID   string
	BeforeData json.RawMessage
	AfterData  json.RawMessage
	IPAddress  *string
	UserAgent  *string
}

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) CreateEntry(_ context.Context, _ CreateEntryParams) (*Entry, error) {
	return &Entry{}, nil
}
