package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	sessionKeyPrefix     = "session:"
	sessionAcctKeyPrefix = "session:acct:"
	defaultIdleTimeout   = 1800
	defaultHardTimeout   = 86400
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
	AuthMethod     string    `json:"auth_method,omitempty"`
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

type Manager struct {
	sessionStore *store.RadiusSessionStore
	redisClient  *redis.Client
	logger       zerolog.Logger
}

func NewManager(sessionStore *store.RadiusSessionStore, redisClient *redis.Client, logger zerolog.Logger) *Manager {
	return &Manager{
		sessionStore: sessionStore,
		redisClient:  redisClient,
		logger:       logger.With().Str("component", "session_manager").Logger(),
	}
}

func (m *Manager) Create(ctx context.Context, sess *Session) error {
	if m.sessionStore != nil {
		simID, _ := uuid.Parse(sess.SimID)
		tenantID, _ := uuid.Parse(sess.TenantID)
		operatorID, _ := uuid.Parse(sess.OperatorID)

		var apnID *uuid.UUID
		if sess.APNID != "" {
			id, err := uuid.Parse(sess.APNID)
			if err == nil {
				apnID = &id
			}
		}

		nasIP := nilIfEmpty(sess.NASIP)
		framedIP := nilIfEmpty(sess.FramedIP)
		acctSessionID := nilIfEmpty(sess.AcctSessionID)

		authMethod := nilIfEmpty(sess.AuthMethod)

		dbSess, err := m.sessionStore.Create(ctx, store.CreateRadiusSessionParams{
			SimID:         simID,
			TenantID:      tenantID,
			OperatorID:    operatorID,
			APNID:         apnID,
			NASIP:         nasIP,
			FramedIP:      framedIP,
			AcctSessionID: acctSessionID,
			AuthMethod:    authMethod,
		})
		if err != nil {
			return fmt.Errorf("session manager: create: %w", err)
		}

		sess.ID = dbSess.ID.String()
		sess.StartedAt = dbSess.StartedAt
	}

	ttl := time.Duration(sess.SessionTimeout) * time.Second
	if ttl <= 0 {
		ttl = time.Duration(defaultHardTimeout) * time.Second
	}

	if m.redisClient != nil {
		if encoded, err := json.Marshal(sess); err == nil {
			key := sessionKeyPrefix + sess.ID
			if err := m.redisClient.Set(ctx, key, encoded, ttl).Err(); err != nil {
				m.logger.Warn().Err(err).Str("session_id", sess.ID).Msg("failed to cache session in Redis")
			}
			if sess.AcctSessionID != "" {
				acctKey := sessionAcctKeyPrefix + sess.AcctSessionID
				if err := m.redisClient.Set(ctx, acctKey, sess.ID, ttl).Err(); err != nil {
					m.logger.Warn().Err(err).Str("acct_session_id", sess.AcctSessionID).Msg("failed to cache acct session index")
				}
			}
		}
	}

	return nil
}

func (m *Manager) Get(ctx context.Context, id string) (*Session, error) {
	if m.redisClient != nil {
		key := sessionKeyPrefix + id
		data, err := m.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			var sess Session
			if err := json.Unmarshal(data, &sess); err == nil {
				return &sess, nil
			}
		}
	}

	if m.sessionStore == nil {
		return nil, nil
	}

	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("session manager: invalid session id: %w", err)
	}

	dbSess, err := m.sessionStore.GetByID(ctx, uid)
	if err != nil {
		return nil, err
	}

	return radiusSessionToSession(dbSess), nil
}

func (m *Manager) GetByAcctSessionID(ctx context.Context, acctSessionID string) (*Session, error) {
	if m.redisClient != nil {
		acctKey := sessionAcctKeyPrefix + acctSessionID
		sessionID, err := m.redisClient.Get(ctx, acctKey).Result()
		if err == nil && sessionID != "" {
			sess, err := m.Get(ctx, sessionID)
			if err == nil {
				return sess, nil
			}
		}
	}

	if m.sessionStore == nil {
		return nil, nil
	}

	dbSess, err := m.sessionStore.GetByAcctSessionID(ctx, acctSessionID)
	if err != nil {
		return nil, err
	}

	return radiusSessionToSession(dbSess), nil
}

func (m *Manager) ListActive(_ context.Context, _ string, _ int, _ SessionFilter) ([]*Session, string, error) {
	return nil, "", nil
}

func (m *Manager) Stats(_ context.Context) (*SessionStats, error) {
	return &SessionStats{}, nil
}

func (m *Manager) GetSessionsForSIM(ctx context.Context, simID string) ([]*Session, error) {
	if m.sessionStore == nil {
		return nil, nil
	}
	uid, err := uuid.Parse(simID)
	if err != nil {
		return nil, fmt.Errorf("session manager: invalid sim_id: %w", err)
	}
	dbSessions, err := m.sessionStore.ListActiveBySIM(ctx, uid)
	if err != nil {
		return nil, err
	}
	var result []*Session
	for i := range dbSessions {
		result = append(result, radiusSessionToSession(&dbSessions[i]))
	}
	return result, nil
}

func (m *Manager) UpdateCounters(ctx context.Context, id string, bytesIn, bytesOut uint64) error {
	if m.sessionStore != nil {
		uid, err := uuid.Parse(id)
		if err != nil {
			return fmt.Errorf("session manager: invalid session id: %w", err)
		}

		if err := m.sessionStore.UpdateCounters(ctx, uid, int64(bytesIn), int64(bytesOut), 0, 0); err != nil {
			return err
		}
	}

	if m.redisClient != nil {
		key := sessionKeyPrefix + id
		data, err := m.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			var sess Session
			if err := json.Unmarshal(data, &sess); err == nil {
				sess.BytesIn = bytesIn
				sess.BytesOut = bytesOut
				sess.LastInterimAt = time.Now().UTC()
				if encoded, err := json.Marshal(sess); err == nil {
					ttl := m.redisClient.TTL(ctx, key).Val()
					if ttl <= 0 {
						ttl = time.Duration(defaultHardTimeout) * time.Second
					}
					m.redisClient.Set(ctx, key, encoded, ttl)
				}
			}
		}
	}

	return nil
}

func (m *Manager) TerminateWithCounters(ctx context.Context, id string, cause string, bytesIn, bytesOut uint64) error {
	if m.sessionStore != nil {
		uid, err := uuid.Parse(id)
		if err != nil {
			return fmt.Errorf("session manager: invalid session id: %w", err)
		}

		if err := m.sessionStore.Finalize(ctx, uid, cause, int64(bytesIn), int64(bytesOut), 0, 0); err != nil {
			return err
		}
	}

	if m.redisClient != nil {
		var acctSessionID string
		key := sessionKeyPrefix + id
		data, err := m.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			var sess Session
			if err := json.Unmarshal(data, &sess); err == nil {
				acctSessionID = sess.AcctSessionID
			}
		}

		m.redisClient.Del(ctx, key)
		if acctSessionID != "" {
			m.redisClient.Del(ctx, sessionAcctKeyPrefix+acctSessionID)
		}
	}

	return nil
}

func (m *Manager) Terminate(ctx context.Context, id string, cause string) error {
	if m.sessionStore != nil {
		uid, err := uuid.Parse(id)
		if err != nil {
			return fmt.Errorf("session manager: invalid session id: %w", err)
		}

		if err := m.sessionStore.Finalize(ctx, uid, cause, 0, 0, 0, 0); err != nil {
			return err
		}
	}

	if m.redisClient != nil {
		var acctSessionID string
		key := sessionKeyPrefix + id
		data, err := m.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			var sess Session
			if err := json.Unmarshal(data, &sess); err == nil {
				acctSessionID = sess.AcctSessionID
			}
		}

		m.redisClient.Del(ctx, key)
		if acctSessionID != "" {
			m.redisClient.Del(ctx, sessionAcctKeyPrefix+acctSessionID)
		}
	}

	return nil
}

func (m *Manager) CountActive(ctx context.Context) (int64, error) {
	if m.sessionStore == nil {
		return 0, nil
	}
	return m.sessionStore.CountActive(ctx)
}

func radiusSessionToSession(rs *store.RadiusSession) *Session {
	sess := &Session{
		ID:           rs.ID.String(),
		SimID:        rs.SimID.String(),
		TenantID:     rs.TenantID.String(),
		OperatorID:   rs.OperatorID.String(),
		SessionState: rs.SessionState,
		BytesIn:      uint64(rs.BytesIn),
		BytesOut:     uint64(rs.BytesOut),
		StartedAt:    rs.StartedAt,
	}
	if rs.APNID != nil {
		sess.APNID = rs.APNID.String()
	}
	if rs.NASIP != nil {
		sess.NASIP = *rs.NASIP
	}
	if rs.AcctSessionID != nil {
		sess.AcctSessionID = *rs.AcctSessionID
	}
	if rs.FramedIP != nil {
		sess.FramedIP = *rs.FramedIP
	}
	if rs.AuthMethod != nil {
		sess.AuthMethod = *rs.AuthMethod
	}
	if rs.RATType != nil {
		sess.RATType = *rs.RATType
	}
	if rs.EndedAt != nil {
		sess.EndedAt = *rs.EndedAt
	}
	if rs.TerminateCause != nil {
		sess.TerminateCause = *rs.TerminateCause
	}
	if rs.LastInterimAt != nil {
		sess.LastInterimAt = *rs.LastInterimAt
	}
	return sess
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func isSessionDataKey(key string) bool {
	prefix := sessionKeyPrefix
	return len(key) > len(prefix) && key[:len(prefix)] == prefix
}
