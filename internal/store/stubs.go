package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrICCIDExists = errors.New("store: iccid already exists")
	ErrIMSIExists  = errors.New("store: imsi already exists")
)

type SIM struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	ICCID      string
	IMSI       string
	MSISDN     string
	OperatorID uuid.UUID
	State      string
}

type SIMStore struct {
	db *pgxpool.Pool
}

func NewSIMStore(db *pgxpool.Pool) *SIMStore {
	return &SIMStore{db: db}
}

type CreateSIMParams struct {
	ICCID      string
	IMSI       string
	MSISDN     *string
	SimType    string
	OperatorID uuid.UUID
	APNID      *uuid.UUID
}

func (s *SIMStore) Create(_ context.Context, _ CreateSIMParams) (*SIM, error) {
	return &SIM{}, nil
}

func (s *SIMStore) InsertHistory(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ string, _ string, _ interface{}, _ interface{}) error {
	return nil
}

func (s *SIMStore) TransitionState(_ context.Context, _ uuid.UUID, _ string, _ *uuid.UUID, _ string, _ interface{}, _ int) (*SIM, error) {
	return &SIM{}, nil
}

func (s *SIMStore) SetIPAndPolicy(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ *uuid.UUID) error {
	return nil
}


type APN struct {
	ID              uuid.UUID
	Name            string
	DefaultPolicyID *uuid.UUID
}

type APNStore struct {
	db *pgxpool.Pool
}

func NewAPNStore(db *pgxpool.Pool) *APNStore {
	return &APNStore{db: db}
}

func (s *APNStore) GetByName(_ context.Context, _ string) (*APN, error) {
	return &APN{}, nil
}

type IPPool struct {
	ID uuid.UUID
}

type IPAddress struct {
	ID uuid.UUID
	IP string
}

type IPAllocation struct {
	ID      uuid.UUID
	Address IPAddress
}

type IPPoolStore struct {
	db *pgxpool.Pool
}

func NewIPPoolStore(db *pgxpool.Pool) *IPPoolStore {
	return &IPPoolStore{db: db}
}

func (s *IPPoolStore) List(_ context.Context, _ string, _ int, _ *uuid.UUID) ([]IPPool, string, error) {
	return nil, "", nil
}

func (s *IPPoolStore) AllocateIP(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*IPAllocation, error) {
	return &IPAllocation{}, nil
}
