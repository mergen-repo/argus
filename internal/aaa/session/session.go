package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type ipPoolStore interface {
	GetIPAddressByID(ctx context.Context, id uuid.UUID) (*store.IPAddress, error)
	ListByAPN(ctx context.Context, tenantID, apnID uuid.UUID) ([]store.IPPool, error)
}

const (
	ProtocolTypeRadius   = "radius"
	ProtocolTypeDiameter = "diameter"
	ProtocolType5GSBA    = "5g_sba"
)

const (
	sessionKeyPrefix     = "session:"
	sessionAcctKeyPrefix = "session:acct:"
	defaultIdleTimeout   = 1800
	defaultHardTimeout   = 86400
)

const (
	statsActiveKey   = "session:stats:active"
	statsAvgDurKey   = "session:stats:avg_duration"
	statsAvgBytesKey = "session:stats:avg_bytes"
)

type Session struct {
	ID    string `json:"id"`
	SimID string `json:"sim_id"`
	// ICCID is the pre-resolved SIM ICCID embedded at session-create so the
	// hot-path session publishers (radius/diameter/sba) can set
	// entity.display_name without a Redis/DB lookup on every Interim / CCR-U.
	// Populated by Manager.Create from the in-memory SIM struct already
	// loaded for framed-IP validation. The Redis-cached session blob carries
	// this field so GetByAcctSessionID restores it. The DB row
	// (store.RadiusSession) is unchanged — ICCID lives only at the Session
	// (Redis) layer (FIX-212 AC-6).
	ICCID          string          `json:"iccid,omitempty"`
	TenantID       string          `json:"tenant_id"`
	OperatorID     string          `json:"operator_id"`
	APNID          string          `json:"apn_id,omitempty"`
	IMSI           string          `json:"imsi"`
	MSISDN         string          `json:"msisdn"`
	APN            string          `json:"apn"`
	NASIP          string          `json:"nas_ip"`
	AcctSessionID  string          `json:"acct_session_id"`
	FramedIP       string          `json:"framed_ip"`
	SessionState   string          `json:"session_state"`
	AuthMethod     string          `json:"auth_method,omitempty"`
	SessionTimeout int             `json:"session_timeout"`
	IdleTimeout    int             `json:"idle_timeout"`
	RATType        string          `json:"rat_type,omitempty"`
	BytesIn        uint64          `json:"bytes_in"`
	BytesOut       uint64          `json:"bytes_out"`
	StartedAt      time.Time       `json:"started_at"`
	LastInterimAt  time.Time       `json:"last_interim_at"`
	EndedAt        time.Time       `json:"ended_at,omitempty"`
	TerminateCause string          `json:"terminate_cause,omitempty"`
	ProtocolType   string          `json:"protocol_type,omitempty"`
	SliceInfo      json.RawMessage `json:"slice_info,omitempty"`
	// SorDecision holds the JSONB payload written by the SoR engine when it
	// selects an operator for this session. Engine wiring is deferred to D-148
	// (FIX-24x); until then this field is nil and the sessions.sor_decision DB
	// column is NULL. The expected shape once the engine is wired:
	//
	//   {
	//     "chosen_operator_id": "<uuid>",
	//     "scoring": [
	//       {"operator_id": "<uuid>", "score": 0.95, "reason": "best latency"},
	//       {"operator_id": "<uuid>", "score": 0.78, "reason": "lowest cost"}
	//     ],
	//     "decided_at": "<iso8601>"
	//   }
	//
	// The handler's sorDecisionDTO must match this shape exactly (DEV-405).
	SorDecision json.RawMessage `json:"sor_decision,omitempty"`
}

type SessionFilter struct {
	TenantID    string
	SimID       string
	OperatorID  string
	APNID       string
	MinDuration *int
	MinUsage    *int64
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
	simStore     *store.SIMStore
	ipPoolStore  ipPoolStore
	metricsReg   *metrics.Registry
	redisClient  *redis.Client
	logger       zerolog.Logger
	auditService audit.Auditor
}

func NewManager(sessionStore *store.RadiusSessionStore, redisClient *redis.Client, logger zerolog.Logger, opts ...ManagerOption) *Manager {
	m := &Manager{
		sessionStore: sessionStore,
		redisClient:  redisClient,
		logger:       logger.With().Str("component", "session_manager").Logger(),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

type ManagerOption func(*Manager)

func WithSIMStore(simStore *store.SIMStore) ManagerOption {
	return func(m *Manager) {
		m.simStore = simStore
	}
}

func WithIPPoolStore(s *store.IPPoolStore) ManagerOption {
	return func(m *Manager) {
		m.ipPoolStore = s
	}
}

func WithMetrics(reg *metrics.Registry) ManagerOption {
	return func(m *Manager) {
		m.metricsReg = reg
	}
}

func WithAuditService(svc audit.Auditor) ManagerOption {
	return func(m *Manager) {
		m.auditService = svc
	}
}

func (m *Manager) validateFramedIP(ctx context.Context, sim *store.SIM, framedIPStr string) (bool, string) {
	if framedIPStr == "" {
		return true, ""
	}

	framedIP := net.ParseIP(framedIPStr)
	if framedIP == nil {
		return false, "unparseable_framed_ip"
	}

	if sim.IPAddressID != nil {
		addr, err := m.ipPoolStore.GetIPAddressByID(ctx, *sim.IPAddressID)
		if err != nil {
			m.logger.Warn().Err(err).
				Str("sim_id", sim.ID.String()).
				Str("framed_ip", framedIPStr).
				Msg("validateFramedIP: failed to fetch assigned ip address; skipping validation")
			return true, ""
		}
		var assignedStr string
		if addr.AddressV4 != nil {
			assignedStr = net.ParseIP(*addr.AddressV4).String()
		} else if addr.AddressV6 != nil {
			assignedStr = net.ParseIP(*addr.AddressV6).String()
		}
		if assignedStr != "" && framedIP.String() != assignedStr {
			return false, "mismatch_assigned_address"
		}
		return true, ""
	}

	if sim.APNID == nil {
		return true, ""
	}

	pools, err := m.ipPoolStore.ListByAPN(ctx, sim.TenantID, *sim.APNID)
	if err != nil {
		m.logger.Warn().Err(err).
			Str("sim_id", sim.ID.String()).
			Str("framed_ip", framedIPStr).
			Msg("validateFramedIP: failed to list apn pools; skipping validation")
		return true, ""
	}

	for _, pool := range pools {
		if pool.CIDRv4 != nil {
			_, ipNet, err := net.ParseCIDR(*pool.CIDRv4)
			if err == nil && ipNet.Contains(framedIP) {
				return true, ""
			}
		}
		if pool.CIDRv6 != nil {
			_, ipNet, err := net.ParseCIDR(*pool.CIDRv6)
			if err == nil && ipNet.Contains(framedIP) {
				return true, ""
			}
		}
	}

	return false, "outside_apn_pools"
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

		if m.ipPoolStore != nil && m.simStore != nil && simID != uuid.Nil && tenantID != uuid.Nil {
			sim, err := m.simStore.GetByID(ctx, tenantID, simID)
			if err != nil {
				m.logger.Warn().Err(err).
					Str("sim_id", sess.SimID).
					Msg("session create: failed to fetch SIM for framed_ip validation; skipping")
			} else if sim != nil {
				// FIX-212 AC-6: embed ICCID into the Redis-cached Session blob
				// so interim/update/end publishers can set entity.display_name
				// without a hot-path lookup. Only populate when caller hasn't
				// already pre-set it (test harnesses may pass a fixture).
				if sess.ICCID == "" {
					sess.ICCID = sim.ICCID
				}
				if ok, reason := m.validateFramedIP(ctx, sim, sess.FramedIP); !ok {
					m.logger.Warn().
						Str("sim_id", sess.SimID).
						Str("framed_ip", sess.FramedIP).
						Str("apn_id", sess.APNID).
						Str("reason", reason).
						Msg("session create: framed_ip pool mismatch (AC-3); session allowed to proceed")
					m.metricsReg.IncFramedIPPoolMismatch(reason)
				}
			}
		}

		nasIP := nilIfEmpty(sess.NASIP)
		framedIP := nilIfEmpty(sess.FramedIP)
		acctSessionID := nilIfEmpty(sess.AcctSessionID)

		authMethod := nilIfEmpty(sess.AuthMethod)

		ratType := nilIfEmpty(sess.RATType)

		dbSess, err := m.sessionStore.Create(ctx, store.CreateRadiusSessionParams{
			SimID:         simID,
			TenantID:      tenantID,
			OperatorID:    operatorID,
			APNID:         apnID,
			NASIP:         nasIP,
			FramedIP:      framedIP,
			AcctSessionID: acctSessionID,
			AuthMethod:    authMethod,
			RATType:       ratType,
			ProtocolType:  sess.ProtocolType,
			SliceInfo:     sess.SliceInfo,
			SoRDecision:   sess.SorDecision, // D-148: nil until SoR engine is wired (FIX-24x)
		})
		if err != nil {
			return fmt.Errorf("session manager: create: %w", err)
		}

		sess.ID = dbSess.ID.String()
		sess.StartedAt = dbSess.StartedAt

		// FIX-242 AC-5 / F-161: session lifecycle audit row (DEV-402: inline publisher).
		if m.auditService != nil {
			afterData, _ := json.Marshal(map[string]interface{}{
				"sim_id":      sess.SimID,
				"operator_id": sess.OperatorID,
				"apn_id":      sess.APNID,
				"ip_address":  sess.FramedIP,
				"rat_type":    sess.RATType,
			})
			_, _ = m.auditService.CreateEntry(ctx, audit.CreateEntryParams{
				TenantID:   tenantID,
				Action:     "session.started",
				EntityType: "session",
				EntityID:   sess.ID,
				AfterData:  afterData,
			})
		}

		if sess.RATType != "" && m.simStore != nil && simID != uuid.Nil {
			if err := m.simStore.UpdateLastRATType(ctx, simID, operatorID, sess.RATType); err != nil {
				m.logger.Warn().Err(err).
					Str("sim_id", sess.SimID).
					Str("rat_type", sess.RATType).
					Msg("failed to update SIM last_rat_type")
			}
		}
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

		pipe := m.redisClient.Pipeline()
		pipe.HIncrBy(ctx, statsActiveKey, "total", 1)
		pipe.HIncrBy(ctx, statsActiveKey, "op:"+sess.OperatorID, 1)
		if sess.APNID != "" {
			pipe.HIncrBy(ctx, statsActiveKey, "apn:"+sess.APNID, 1)
		}
		if sess.RATType != "" {
			pipe.HIncrBy(ctx, statsActiveKey, "rat:"+sess.RATType, 1)
		}
		pipe.Expire(ctx, statsActiveKey, 48*time.Hour)
		if _, err := pipe.Exec(ctx); err != nil {
			m.logger.Warn().Err(err).Msg("failed to update session stats counters")
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

func (m *Manager) ListActive(ctx context.Context, cursor string, limit int, filter SessionFilter) ([]*Session, string, error) {
	if m.sessionStore == nil {
		return m.listActiveFromRedis(ctx, cursor, limit, filter)
	}

	var simID, operatorID, apnID *uuid.UUID
	if filter.SimID != "" {
		if id, err := uuid.Parse(filter.SimID); err == nil {
			simID = &id
		}
	}
	if filter.OperatorID != "" {
		if id, err := uuid.Parse(filter.OperatorID); err == nil {
			operatorID = &id
		}
	}
	if filter.APNID != "" {
		if id, err := uuid.Parse(filter.APNID); err == nil {
			apnID = &id
		}
	}

	var tenantID *uuid.UUID
	if filter.TenantID != "" {
		if id, err := uuid.Parse(filter.TenantID); err == nil {
			tenantID = &id
		}
	}

	dbSessions, nextCursor, err := m.sessionStore.ListActiveFiltered(ctx, store.ListActiveSessionsParams{
		TenantID:    tenantID,
		Cursor:      cursor,
		Limit:       limit,
		SimID:       simID,
		OperatorID:  operatorID,
		APNID:       apnID,
		MinDuration: filter.MinDuration,
		MinUsage:    filter.MinUsage,
	})
	if err != nil {
		return nil, "", fmt.Errorf("session manager: list active: %w", err)
	}

	var result []*Session
	for i := range dbSessions {
		result = append(result, radiusSessionToSession(&dbSessions[i]))
	}
	return result, nextCursor, nil
}

func (m *Manager) listActiveFromRedis(ctx context.Context, _ string, limit int, filter SessionFilter) ([]*Session, string, error) {
	if m.redisClient == nil {
		return nil, "", nil
	}

	var redisCursor uint64
	var result []*Session

	for {
		keys, nextCursor, err := m.redisClient.Scan(ctx, redisCursor, sessionKeyPrefix+"*", 200).Result()
		if err != nil {
			return nil, "", fmt.Errorf("session manager: redis scan: %w", err)
		}

		for _, key := range keys {
			if !isSessionDataKey(key) {
				continue
			}
			data, err := m.redisClient.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}
			var sess Session
			if err := json.Unmarshal(data, &sess); err != nil {
				continue
			}
			if sess.SessionState != "active" {
				continue
			}
			if filter.SimID != "" && sess.SimID != filter.SimID {
				continue
			}
			if filter.OperatorID != "" && sess.OperatorID != filter.OperatorID {
				continue
			}
			if filter.APNID != "" && sess.APNID != filter.APNID {
				continue
			}
			result = append(result, &sess)
			if limit > 0 && len(result) > limit {
				break
			}
		}

		redisCursor = nextCursor
		if redisCursor == 0 || (limit > 0 && len(result) > limit) {
			break
		}
	}

	nextCursor := ""
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nextCursor, nil
}

func (m *Manager) Stats(ctx context.Context, tenantID string) (*SessionStats, error) {
	if m.sessionStore == nil {
		return m.statsFromRedis(ctx)
	}

	var tid *uuid.UUID
	if tenantID != "" {
		if id, err := uuid.Parse(tenantID); err == nil {
			tid = &id
		}
	}

	dbStats, err := m.sessionStore.GetActiveStats(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("session manager: stats: %w", err)
	}

	return &SessionStats{
		TotalActive:    dbStats.TotalActive,
		ByOperator:     dbStats.ByOperator,
		ByAPN:          dbStats.ByAPN,
		ByRATType:      make(map[string]int64),
		AvgDurationSec: dbStats.AvgDurationSec,
		AvgBytes:       dbStats.AvgBytes,
	}, nil
}

func (m *Manager) decrementSessionStats(ctx context.Context, sess *Session) {
	if sess.OperatorID == "" {
		return
	}
	pipe := m.redisClient.Pipeline()
	pipe.HIncrBy(ctx, statsActiveKey, "total", -1)
	pipe.HIncrBy(ctx, statsActiveKey, "op:"+sess.OperatorID, -1)
	if sess.APNID != "" {
		pipe.HIncrBy(ctx, statsActiveKey, "apn:"+sess.APNID, -1)
	}
	if sess.RATType != "" {
		pipe.HIncrBy(ctx, statsActiveKey, "rat:"+sess.RATType, -1)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		m.logger.Warn().Err(err).Msg("failed to decrement session stats counters")
	}
}

func (m *Manager) statsFromRedis(ctx context.Context) (*SessionStats, error) {
	stats := &SessionStats{
		ByOperator: make(map[string]int64),
		ByAPN:      make(map[string]int64),
		ByRATType:  make(map[string]int64),
	}
	if m.redisClient == nil {
		return stats, nil
	}

	var cursor uint64
	var totalDuration float64
	var totalBytes float64
	now := time.Now()

	for {
		keys, nextCursor, err := m.redisClient.Scan(ctx, cursor, sessionKeyPrefix+"*", 200).Result()
		if err != nil {
			return stats, fmt.Errorf("session stats: redis scan: %w", err)
		}

		for _, key := range keys {
			if !isSessionDataKey(key) {
				continue
			}
			data, err := m.redisClient.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}
			var sess Session
			if err := json.Unmarshal(data, &sess); err != nil {
				continue
			}
			if sess.SessionState != "active" {
				continue
			}

			stats.TotalActive++
			if sess.OperatorID != "" {
				stats.ByOperator[sess.OperatorID]++
			}
			if sess.APNID != "" {
				stats.ByAPN[sess.APNID]++
			}
			if sess.RATType != "" {
				stats.ByRATType[sess.RATType]++
			}
			totalDuration += now.Sub(sess.StartedAt).Seconds()
			totalBytes += float64(sess.BytesIn + sess.BytesOut)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	if stats.TotalActive > 0 {
		stats.AvgDurationSec = totalDuration / float64(stats.TotalActive)
		stats.AvgBytes = totalBytes / float64(stats.TotalActive)
	}

	return stats, nil
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
		var sess Session
		key := sessionKeyPrefix + id
		data, err := m.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			if err := json.Unmarshal(data, &sess); err == nil {
				acctSessionID = sess.AcctSessionID
			}
		}

		// FIX-242 AC-5 / F-161: session lifecycle audit row (DEV-402: inline publisher).
		if m.auditService != nil && sess.TenantID != "" {
			tenantID, _ := uuid.Parse(sess.TenantID)
			afterData, _ := json.Marshal(map[string]interface{}{
				"bytes_in":           bytesIn,
				"bytes_out":          bytesOut,
				"termination_reason": cause,
				"duration_sec":       int64(time.Since(sess.StartedAt).Seconds()),
			})
			_, _ = m.auditService.CreateEntry(ctx, audit.CreateEntryParams{
				TenantID:   tenantID,
				Action:     "session.ended",
				EntityType: "session",
				EntityID:   id,
				AfterData:  afterData,
			})
		}

		m.redisClient.Del(ctx, key)
		if acctSessionID != "" {
			m.redisClient.Del(ctx, sessionAcctKeyPrefix+acctSessionID)
		}

		m.decrementSessionStats(ctx, &sess)
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
		var sess Session
		key := sessionKeyPrefix + id
		data, err := m.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			if err := json.Unmarshal(data, &sess); err == nil {
				acctSessionID = sess.AcctSessionID
			}
		}

		// FIX-242 AC-5 / F-161: session lifecycle audit row (DEV-402: inline publisher).
		if m.auditService != nil && sess.TenantID != "" {
			tenantID, _ := uuid.Parse(sess.TenantID)
			afterData, _ := json.Marshal(map[string]interface{}{
				"bytes_in":           sess.BytesIn,
				"bytes_out":          sess.BytesOut,
				"termination_reason": cause,
				"duration_sec":       int64(time.Since(sess.StartedAt).Seconds()),
			})
			_, _ = m.auditService.CreateEntry(ctx, audit.CreateEntryParams{
				TenantID:   tenantID,
				Action:     "session.ended",
				EntityType: "session",
				EntityID:   id,
				AfterData:  afterData,
			})
		}

		m.redisClient.Del(ctx, key)
		if acctSessionID != "" {
			m.redisClient.Del(ctx, sessionAcctKeyPrefix+acctSessionID)
		}

		m.decrementSessionStats(ctx, &sess)
	}

	return nil
}

func (m *Manager) CountActive(ctx context.Context) (int64, error) {
	if m.sessionStore == nil {
		return 0, nil
	}
	return m.sessionStore.CountActive(ctx)
}

func (m *Manager) CheckConcurrentLimit(ctx context.Context, simID string, maxSessions int) (bool, *Session, error) {
	if maxSessions <= 0 {
		return true, nil, nil
	}

	if m.sessionStore == nil {
		return true, nil, nil
	}

	uid, err := uuid.Parse(simID)
	if err != nil {
		return false, nil, fmt.Errorf("session manager: invalid sim_id: %w", err)
	}

	count, err := m.sessionStore.CountActiveForSIM(ctx, uid)
	if err != nil {
		return false, nil, fmt.Errorf("session manager: count active for sim: %w", err)
	}

	if count < int64(maxSessions) {
		return true, nil, nil
	}

	oldest, err := m.sessionStore.GetOldestActiveForSIM(ctx, uid)
	if err != nil {
		m.logger.Warn().Err(err).Str("sim_id", simID).Msg("failed to get oldest session for eviction")
		return false, nil, nil
	}

	return false, radiusSessionToSession(oldest), nil
}

func (m *Manager) CountActiveForSIM(ctx context.Context, simID string) (int64, error) {
	if m.sessionStore == nil {
		return 0, nil
	}
	uid, err := uuid.Parse(simID)
	if err != nil {
		return 0, fmt.Errorf("session manager: invalid sim_id: %w", err)
	}
	return m.sessionStore.CountActiveForSIM(ctx, uid)
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
	sess.ProtocolType = rs.ProtocolType
	sess.SliceInfo = rs.SliceInfo
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
	acctPrefix := sessionAcctKeyPrefix
	if len(key) > len(acctPrefix) && key[:len(acctPrefix)] == acctPrefix {
		return false
	}
	return len(key) > len(prefix) && key[:len(prefix)] == prefix
}
