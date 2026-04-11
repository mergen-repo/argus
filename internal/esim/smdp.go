package esim

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var (
	ErrSMDPConnectionFailed = errors.New("smdp: connection failed")
	ErrSMDPProfileNotFound  = errors.New("smdp: profile not found")
	ErrSMDPOperationFailed  = errors.New("smdp: operation failed")
)

type DownloadProfileRequest struct {
	EID        string
	OperatorID uuid.UUID
	ICCID      string
	SMDPPlusID string
}

type DownloadProfileResponse struct {
	ProfileID  string
	ICCID      string
	SMDPPlusID string
}

type EnableProfileRequest struct {
	EID        string
	ICCID      string
	SMDPPlusID string
	ProfileID  uuid.UUID
}

type DisableProfileRequest struct {
	EID        string
	ICCID      string
	SMDPPlusID string
	ProfileID  uuid.UUID
}

type DeleteProfileRequest struct {
	EID        string
	ICCID      string
	SMDPPlusID string
	ProfileID  uuid.UUID
}

type GetProfileInfoRequest struct {
	EID       string
	ICCID     string
	ProfileID string
}

type GetProfileInfoResponse struct {
	State      string
	ICCID      string
	SMDPPlusID string
	LastSeenAt time.Time
}

type SMDPAdapter interface {
	DownloadProfile(ctx context.Context, req DownloadProfileRequest) (*DownloadProfileResponse, error)
	EnableProfile(ctx context.Context, req EnableProfileRequest) error
	DisableProfile(ctx context.Context, req DisableProfileRequest) error
	DeleteProfile(ctx context.Context, req DeleteProfileRequest) error
	GetProfileInfo(ctx context.Context, req GetProfileInfoRequest) (*GetProfileInfoResponse, error)
}

type MockSMDPAdapter struct {
	logger    zerolog.Logger
	latencyMs int
}

func NewMockSMDPAdapter(logger zerolog.Logger) *MockSMDPAdapter {
	return &MockSMDPAdapter{
		logger:    logger.With().Str("component", "mock_smdp").Logger(),
		latencyMs: 50,
	}
}

func (m *MockSMDPAdapter) simulateLatency() {
	if m.latencyMs > 0 {
		time.Sleep(time.Duration(m.latencyMs) * time.Millisecond)
	}
}

func (m *MockSMDPAdapter) DownloadProfile(ctx context.Context, req DownloadProfileRequest) (*DownloadProfileResponse, error) {
	m.logger.Info().
		Str("eid", req.EID).
		Str("iccid", req.ICCID).
		Str("operator_id", req.OperatorID.String()).
		Msg("mock SM-DP+: download profile")

	m.simulateLatency()

	return &DownloadProfileResponse{
		ProfileID:  uuid.New().String(),
		ICCID:      req.ICCID,
		SMDPPlusID: "mock-smdp-" + req.EID,
	}, nil
}

func (m *MockSMDPAdapter) EnableProfile(ctx context.Context, req EnableProfileRequest) error {
	m.logger.Info().
		Str("eid", req.EID).
		Str("profile_id", req.ProfileID.String()).
		Msg("mock SM-DP+: enable profile")

	m.simulateLatency()
	return nil
}

func (m *MockSMDPAdapter) DisableProfile(ctx context.Context, req DisableProfileRequest) error {
	m.logger.Info().
		Str("eid", req.EID).
		Str("profile_id", req.ProfileID.String()).
		Msg("mock SM-DP+: disable profile")

	m.simulateLatency()
	return nil
}

func (m *MockSMDPAdapter) DeleteProfile(ctx context.Context, req DeleteProfileRequest) error {
	m.logger.Info().
		Str("eid", req.EID).
		Str("profile_id", req.ProfileID.String()).
		Msg("mock SM-DP+: delete profile")

	m.simulateLatency()
	return nil
}

func (m *MockSMDPAdapter) GetProfileInfo(ctx context.Context, req GetProfileInfoRequest) (*GetProfileInfoResponse, error) {
	m.logger.Info().
		Str("eid", req.EID).
		Str("iccid", req.ICCID).
		Str("profile_id", req.ProfileID).
		Msg("mock SM-DP+: get profile info")

	m.simulateLatency()

	return &GetProfileInfoResponse{
		State:      "enabled",
		ICCID:      req.ICCID,
		SMDPPlusID: "mock-smdp-" + req.EID,
		LastSeenAt: time.Now(),
	}, nil
}
