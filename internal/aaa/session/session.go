package session

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const (
	sessionKeyPrefix   = "session:"
	defaultIdleTimeout = 1800
	defaultHardTimeout = 86400
)

type Session struct {
	ID             string    `json:"id"`
	SimID          string    `json:"sim_id"`
	TenantID       string    `json:"tenant_id"`
	OperatorID     string    `json:"operator_id"`
	APNID          string    `json:"apn_id,omitempty"`
	IMSI           string    `json:"imsi"`
	MSISDN         string    `json:"msisdn"`
	APN            string    `json:"apn"`
	NASIP          string    `json:"nas_ip"`
	AcctSessionID  string    `json:"acct_session_id"`
	FramedIP       string    `json:"framed_ip"`
	SessionState   string    `json:"session_state"`
	SessionTimeout int       `json:"session_timeout"`
	IdleTimeout    int       `json:"idle_timeout"`
	RATType        string    `json:"rat_type,omitempty"`
	BytesIn        uint64    `json:"bytes_in"`
	BytesOut       uint64    `json:"bytes_out"`
	StartedAt      time.Time `json:"started_at"`
	LastInterimAt  time.Time `json:"last_interim_at"`
	EndedAt        time.Time `json:"ended_at,omitempty"`
	TerminateCause string    `json:"terminate_cause,omitempty"`
}

type SessionFilter struct {
	SimID      string
	OperatorID string
	APNID      string
}

type SessionStats struct {
	TotalActive    int64            `json:"total_active"`
	ByOperator     map[string]int64 `json:"by_operator"`
	ByAPN          map[string]int64 `json:"by_apn"`
	ByRATType      map[string]int64 `json:"by_rat_type"`
	AvgDurationSec float64          `json:"avg_duration_sec"`
	AvgBytes       float64          `json:"avg_bytes"`
}

type SessionCounters struct {
	InputOctets   uint64
	OutputOctets  uint64
	InputPackets  uint64
	OutputPackets uint64
}

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Create(_ context.Context, _ *Session) error {
	return nil
}

func (m *Manager) Get(_ context.Context, _ string) (*Session, error) {
	return nil, nil
}

func (m *Manager) GetByAcctSessionID(_ context.Context, _ string) (*Session, error) {
	return nil, nil
}

func (m *Manager) ListActive(_ context.Context, _ string, _ int, _ SessionFilter) ([]*Session, string, error) {
	return nil, "", nil
}

func (m *Manager) Stats(_ context.Context) (*SessionStats, error) {
	return &SessionStats{}, nil
}

func (m *Manager) GetSessionsForSIM(_ context.Context, _ string) ([]*Session, error) {
	return nil, nil
}

func (m *Manager) UpdateCounters(_ context.Context, _ string, _ uint64, _ uint64) error {
	return nil
}

func (m *Manager) Terminate(_ context.Context, _ string, _ string) error {
	return nil
}

func isSessionDataKey(key string) bool {
	prefix := sessionKeyPrefix
	return len(key) > len(prefix) && key[:len(prefix)] == prefix
}

// Ensure uuid import is used (referenced by sweep.go indirectly through TenantID etc)
var _ = uuid.Nil
