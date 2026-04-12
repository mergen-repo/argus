package ws

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

type EventEnvelope struct {
	Type      string      `json:"type"`
	ID        string      `json:"id"`
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data"`
}

type Connection struct {
	TenantID  uuid.UUID
	UserID    uuid.UUID
	SendCh    chan []byte
	Filters   []string
	ws        *websocket.Conn
	done      chan struct{}
	mu        sync.Mutex
	createdAt time.Time
}

func (c *Connection) MatchesFilter(eventType string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.Filters) == 0 {
		return true
	}
	for _, f := range c.Filters {
		if f == "*" || f == eventType {
			return true
		}
	}
	return false
}

func (c *Connection) SetFilters(filters []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Filters = filters
}

type Subscriber interface {
	QueueSubscribe(subject, queue string, handler func(string, []byte)) (Subscription, error)
}

type Subscription interface {
	Unsubscribe() error
}

type Hub struct {
	mu    sync.RWMutex
	conns map[uuid.UUID]map[*Connection]struct{}
	subs  []Subscription

	dropped uint64

	logger zerolog.Logger
}

func (h *Hub) safeSend(conn *Connection, msg []byte) {
	select {
	case conn.SendCh <- msg:
		return
	default:
	}
	select {
	case <-conn.SendCh:
		atomic.AddUint64(&h.dropped, 1)
		h.logger.Warn().
			Str("tenant_id", conn.TenantID.String()).
			Msg("ws send buffer full, dropped oldest message")
	default:
	}
	select {
	case conn.SendCh <- msg:
	default:
		atomic.AddUint64(&h.dropped, 1)
	}
}

func (h *Hub) DroppedMessageCount() uint64 {
	return atomic.LoadUint64(&h.dropped)
}

func NewHub(logger zerolog.Logger) *Hub {
	return &Hub{
		conns:  make(map[uuid.UUID]map[*Connection]struct{}),
		logger: logger.With().Str("component", "ws_hub").Logger(),
	}
}

func (h *Hub) Register(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conns[conn.TenantID] == nil {
		h.conns[conn.TenantID] = make(map[*Connection]struct{})
	}
	h.conns[conn.TenantID][conn] = struct{}{}

	h.logger.Debug().
		Str("tenant_id", conn.TenantID.String()).
		Str("user_id", conn.UserID.String()).
		Msg("ws connection registered")
}

func (h *Hub) Unregister(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if conns, ok := h.conns[conn.TenantID]; ok {
		delete(conns, conn)
		if len(conns) == 0 {
			delete(h.conns, conn.TenantID)
		}
	}

	h.logger.Debug().
		Str("tenant_id", conn.TenantID.String()).
		Str("user_id", conn.UserID.String()).
		Msg("ws connection unregistered")
}

func (h *Hub) BroadcastAll(eventType string, data interface{}) {
	envelope := EventEnvelope{
		Type:      eventType,
		ID:        "evt_" + uuid.New().String()[:8],
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Data:      data,
	}

	msg, err := json.Marshal(envelope)
	if err != nil {
		h.logger.Error().Err(err).Msg("marshal ws event")
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, conns := range h.conns {
		for conn := range conns {
			if conn.MatchesFilter(eventType) {
				h.safeSend(conn, msg)
			}
		}
	}
}

func (h *Hub) BroadcastToTenant(tenantID uuid.UUID, eventType string, data interface{}) {
	envelope := EventEnvelope{
		Type:      eventType,
		ID:        "evt_" + uuid.New().String()[:8],
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Data:      data,
	}

	msg, err := json.Marshal(envelope)
	if err != nil {
		h.logger.Error().Err(err).Msg("marshal ws event")
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	conns, ok := h.conns[tenantID]
	if !ok {
		return
	}

	for conn := range conns {
		if conn.MatchesFilter(eventType) {
			h.safeSend(conn, msg)
		}
	}
}

func (h *Hub) SubscribeToNATS(subscriber Subscriber, subjects []string) error {
	for _, subject := range subjects {
		sub, err := subscriber.QueueSubscribe(subject, "ws-hub", func(subj string, data []byte) {
			h.relayNATSEvent(subj, data)
		})
		if err != nil {
			h.Stop()
			return err
		}
		h.subs = append(h.subs, sub)
	}

	h.logger.Info().
		Strs("subjects", subjects).
		Msg("ws hub subscribed to NATS")
	return nil
}

func (h *Hub) relayNATSEvent(subject string, data []byte) {
	eventType := natsSubjectToWSType(subject)
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		h.logger.Error().Err(err).Str("subject", subject).Msg("unmarshal NATS event for WS relay")
		return
	}

	tenantID, ok := extractTenantID(payload)
	if !ok {
		h.BroadcastAll(eventType, payload)
		return
	}
	h.BroadcastToTenant(tenantID, eventType, payload)
}

func extractTenantID(payload map[string]interface{}) (uuid.UUID, bool) {
	raw, present := payload["tenant_id"]
	if !present || raw == nil {
		return uuid.Nil, false
	}
	switch v := raw.(type) {
	case string:
		if v == "" {
			return uuid.Nil, false
		}
		id, err := uuid.Parse(v)
		if err != nil || id == uuid.Nil {
			return uuid.Nil, false
		}
		return id, true
	default:
		return uuid.Nil, false
	}
}

func natsSubjectToWSType(subject string) string {
	mapping := map[string]string{
		"argus.events.operator.health":       "operator.health_changed",
		"argus.events.alert.triggered":       "alert.new",
		"argus.events.session.started":       "session.started",
		"argus.events.session.ended":         "session.ended",
		"argus.events.sim.updated":           "sim.state_changed",
		"argus.events.notification.dispatch": "notification.new",
		"argus.jobs.progress":                       "job.progress",
		"argus.jobs.completed":                      "job.completed",
		"argus.events.policy.rollout_progress":      "policy.rollout_progress",
	}
	if t, ok := mapping[subject]; ok {
		return t
	}
	return subject
}

func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for _, conns := range h.conns {
		count += len(conns)
	}
	return count
}

func (h *Hub) TenantConnectionCount(tenantID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[tenantID])
}

func (h *Hub) UserConnectionCount(tenantID, userID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for conn := range h.conns[tenantID] {
		if conn.UserID == userID {
			n++
		}
	}
	return n
}

func (h *Hub) EvictOldestByUser(tenantID, userID uuid.UUID) *Connection {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var oldest *Connection
	for conn := range h.conns[tenantID] {
		if conn.UserID == userID {
			if oldest == nil || conn.createdAt.Before(oldest.createdAt) {
				oldest = conn
			}
		}
	}
	return oldest
}

func (h *Hub) BroadcastReconnect(reason string, afterMs int) {
	payload := map[string]interface{}{"reason": reason, "after_ms": afterMs}
	msg, _ := json.Marshal(map[string]interface{}{"type": "reconnect", "data": payload})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, conns := range h.conns {
		for conn := range conns {
			h.safeSend(conn, msg)
		}
	}
}

func (h *Hub) DropUser(userID uuid.UUID) {
	h.mu.RLock()
	var toClose []*Connection
	for _, conns := range h.conns {
		for conn := range conns {
			if conn.UserID == userID {
				toClose = append(toClose, conn)
			}
		}
	}
	h.mu.RUnlock()

	for _, conn := range toClose {
		conn.ws.Close()
		h.Unregister(conn)
		h.logger.Debug().
			Str("user_id", userID.String()).
			Msg("ws connection dropped for user session revocation")
	}
}

// DisconnectTenant forcibly closes all WebSocket connections belonging to tenantID
// and removes them from the hub. Returns the unique user IDs that were disconnected.
func (h *Hub) DisconnectTenant(tenantID uuid.UUID) []uuid.UUID {
	h.mu.Lock()
	tenantConns := h.conns[tenantID]
	toClose := make([]*Connection, 0, len(tenantConns))
	for conn := range tenantConns {
		toClose = append(toClose, conn)
	}
	delete(h.conns, tenantID)
	h.mu.Unlock()

	seen := make(map[uuid.UUID]struct{})
	var userIDs []uuid.UUID
	for _, conn := range toClose {
		conn.ws.Close()
		if _, ok := seen[conn.UserID]; !ok {
			seen[conn.UserID] = struct{}{}
			userIDs = append(userIDs, conn.UserID)
		}
		h.logger.Debug().
			Str("tenant_id", tenantID.String()).
			Str("user_id", conn.UserID.String()).
			Msg("ws connection dropped for tenant session revocation")
	}
	return userIDs
}

func (h *Hub) Stop() {
	for _, sub := range h.subs {
		sub.Unsubscribe()
	}
	h.subs = nil
	h.logger.Info().Msg("ws hub stopped")
}
