package ws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/btopcu/argus/internal/auth"
)

const testJWTSecret = "test-secret-key-that-is-long-enough-32chars"

func validToken(t *testing.T, tenantID, userID uuid.UUID) string {
	t.Helper()
	tok, err := auth.GenerateToken(testJWTSecret, userID, tenantID, "admin", 5*time.Minute, false)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tok
}

func expiredToken(t *testing.T) string {
	t.Helper()
	tok, err := auth.GenerateToken(testJWTSecret, uuid.New(), uuid.New(), "admin", -1*time.Minute, false)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tok
}

func setupTestServer(t *testing.T, maxConns int) (*Server, *httptest.Server) {
	t.Helper()
	hub := NewHub(zerolog.Nop())
	if maxConns <= 0 {
		maxConns = 100
	}
	srv := NewServer(hub, ServerConfig{
		Addr:              ":0",
		JWTSecret:         testJWTSecret,
		MaxConnsPerTenant: maxConns,
	}, zerolog.Nop())

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/v1/events", srv.handleWS)
	ts := httptest.NewServer(mux)
	return srv, ts
}

func wsURL(ts *httptest.Server, token string) string {
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/v1/events"
	if token != "" {
		url += "?token=" + token
	}
	return url
}

func dialWS(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	return conn
}

func readJSON(t *testing.T, conn *websocket.Conn) map[string]interface{} {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(msg, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return result
}

func TestServer_QueryParamAuth_Valid(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	userID := uuid.New()
	token := validToken(t, tenantID, userID)

	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()

	msg := readJSON(t, conn)
	if msg["type"] != "auth.ok" {
		t.Errorf("expected auth.ok, got %v", msg["type"])
	}
	data := msg["data"].(map[string]interface{})
	if data["tenant_id"] != tenantID.String() {
		t.Errorf("tenant_id = %v, want %v", data["tenant_id"], tenantID.String())
	}
	if data["user_id"] != userID.String() {
		t.Errorf("user_id = %v, want %v", data["user_id"], userID.String())
	}
}

func TestServer_QueryParamAuth_Invalid(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	_, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, "invalid-token"), nil)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServer_QueryParamAuth_Expired(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	token := expiredToken(t)
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, token), nil)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServer_FirstMessageAuth_Valid(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	userID := uuid.New()
	token := validToken(t, tenantID, userID)

	conn := dialWS(t, wsURL(ts, ""))
	defer conn.Close()

	authMsg := map[string]string{"type": "auth", "token": token}
	data, _ := json.Marshal(authMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write auth msg: %v", err)
	}

	msg := readJSON(t, conn)
	if msg["type"] != "auth.ok" {
		t.Errorf("expected auth.ok, got %v", msg["type"])
	}
}

func TestServer_FirstMessageAuth_Invalid(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	conn := dialWS(t, wsURL(ts, ""))
	defer conn.Close()

	authMsg := map[string]string{"type": "auth", "token": "bad-token"}
	data, _ := json.Marshal(authMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write auth msg: %v", err)
	}

	msg := readJSON(t, conn)
	if msg["type"] != "auth.error" {
		t.Errorf("expected auth.error, got %v", msg["type"])
	}
	errData := msg["data"].(map[string]interface{})
	if errData["code"] != "TOKEN_INVALID" {
		t.Errorf("code = %v, want TOKEN_INVALID", errData["code"])
	}
}

func TestServer_FirstMessageAuth_Expired(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	token := expiredToken(t)

	conn := dialWS(t, wsURL(ts, ""))
	defer conn.Close()

	authMsg := map[string]string{"type": "auth", "token": token}
	data, _ := json.Marshal(authMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write auth msg: %v", err)
	}

	msg := readJSON(t, conn)
	if msg["type"] != "auth.error" {
		t.Errorf("expected auth.error, got %v", msg["type"])
	}
	errData := msg["data"].(map[string]interface{})
	if errData["code"] != "TOKEN_EXPIRED" {
		t.Errorf("code = %v, want TOKEN_EXPIRED", errData["code"])
	}
}

func TestServer_TenantIsolation(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantA := uuid.New()
	tenantB := uuid.New()
	tokenA := validToken(t, tenantA, uuid.New())
	tokenB := validToken(t, tenantB, uuid.New())

	connA := dialWS(t, wsURL(ts, tokenA))
	defer connA.Close()
	readJSON(t, connA) // auth.ok

	connB := dialWS(t, wsURL(ts, tokenB))
	defer connB.Close()
	readJSON(t, connB) // auth.ok

	time.Sleep(50 * time.Millisecond)

	srv.hub.BroadcastToTenant(tenantA, "alert.new", map[string]string{"msg": "tenant-a-only"})

	msgA := readJSON(t, connA)
	if msgA["type"] != "alert.new" {
		t.Errorf("tenant A should receive alert.new, got %v", msgA["type"])
	}

	connB.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, _, err := connB.ReadMessage()
	if err == nil {
		t.Error("tenant B should not receive tenant A events")
	}
}

func TestServer_EventDelivery(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	token := validToken(t, tenantID, uuid.New())

	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn) // auth.ok

	time.Sleep(50 * time.Millisecond)

	srv.hub.BroadcastToTenant(tenantID, "session.started", map[string]string{
		"session_id": uuid.New().String(),
	})

	msg := readJSON(t, conn)
	if msg["type"] != "session.started" {
		t.Errorf("type = %v, want session.started", msg["type"])
	}
}

func TestServer_SubscribeFilter(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	token := validToken(t, tenantID, uuid.New())

	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn) // auth.ok

	subMsg := map[string]interface{}{
		"type":   "subscribe",
		"events": []string{"session.started", "alert.new"},
	}
	data, _ := json.Marshal(subMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	subResp := readJSON(t, conn)
	if subResp["type"] != "subscribe.ok" {
		t.Errorf("expected subscribe.ok, got %v", subResp["type"])
	}

	time.Sleep(50 * time.Millisecond)

	srv.hub.BroadcastToTenant(tenantID, "operator.health_changed", map[string]string{"status": "down"})
	srv.hub.BroadcastToTenant(tenantID, "session.started", map[string]string{"id": "s1"})

	msg := readJSON(t, conn)
	if msg["type"] != "session.started" {
		t.Errorf("expected session.started (filtered), got %v", msg["type"])
	}
}

func TestServer_MaxConnectionsPerTenant(t *testing.T) {
	_, ts := setupTestServer(t, 2)
	defer ts.Close()

	tenantID := uuid.New()

	conn1 := dialWS(t, wsURL(ts, validToken(t, tenantID, uuid.New())))
	defer conn1.Close()
	readJSON(t, conn1) // auth.ok

	conn2 := dialWS(t, wsURL(ts, validToken(t, tenantID, uuid.New())))
	defer conn2.Close()
	readJSON(t, conn2) // auth.ok

	time.Sleep(50 * time.Millisecond)

	conn3, _, err := websocket.DefaultDialer.Dial(wsURL(ts, validToken(t, tenantID, uuid.New())), nil)
	if err != nil {
		return
	}
	defer conn3.Close()

	readJSON(t, conn3) // auth.ok

	conn3.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, _, readErr := conn3.ReadMessage()
	if readErr == nil {
		t.Error("3rd connection should be closed due to max connections")
	}
	if closeErr, ok := readErr.(*websocket.CloseError); ok {
		if closeErr.Code != CloseCodeMaxConns {
			t.Errorf("close code = %d, want %d", closeErr.Code, CloseCodeMaxConns)
		}
	}
}

func TestServer_ConnectionCount(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()

	if srv.hub.ConnectionCount() != 0 {
		t.Errorf("initial count = %d, want 0", srv.hub.ConnectionCount())
	}

	conn := dialWS(t, wsURL(ts, validToken(t, tenantID, uuid.New())))
	readJSON(t, conn) // auth.ok

	time.Sleep(50 * time.Millisecond)

	if srv.hub.ConnectionCount() != 1 {
		t.Errorf("count after connect = %d, want 1", srv.hub.ConnectionCount())
	}

	conn.Close()
	time.Sleep(100 * time.Millisecond)

	if srv.hub.ConnectionCount() != 0 {
		t.Errorf("count after disconnect = %d, want 0", srv.hub.ConnectionCount())
	}
}

func TestServer_TenantConnectionCount(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantA := uuid.New()
	tenantB := uuid.New()

	connA := dialWS(t, wsURL(ts, validToken(t, tenantA, uuid.New())))
	defer connA.Close()
	readJSON(t, connA)

	connB := dialWS(t, wsURL(ts, validToken(t, tenantB, uuid.New())))
	defer connB.Close()
	readJSON(t, connB)

	time.Sleep(50 * time.Millisecond)

	if srv.hub.TenantConnectionCount(tenantA) != 1 {
		t.Errorf("tenant A count = %d, want 1", srv.hub.TenantConnectionCount(tenantA))
	}
	if srv.hub.TenantConnectionCount(tenantB) != 1 {
		t.Errorf("tenant B count = %d, want 1", srv.hub.TenantConnectionCount(tenantB))
	}
}

func TestServer_UnknownMessageType(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	_ = srv
	defer ts.Close()

	tenantID := uuid.New()
	token := validToken(t, tenantID, uuid.New())

	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn) // auth.ok

	unknownMsg := map[string]string{"type": "unknown"}
	data, _ := json.Marshal(unknownMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	msg := readJSON(t, conn)
	if msg["type"] != "error" {
		t.Errorf("expected error, got %v", msg["type"])
	}
	errData := msg["data"].(map[string]interface{})
	if errData["code"] != "UNKNOWN_MESSAGE" {
		t.Errorf("code = %v, want UNKNOWN_MESSAGE", errData["code"])
	}
}

func TestServer_InvalidJSON(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	token := validToken(t, uuid.New(), uuid.New())
	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn) // auth.ok

	if err := conn.WriteMessage(websocket.TextMessage, []byte("{invalid")); err != nil {
		t.Fatalf("write: %v", err)
	}

	msg := readJSON(t, conn)
	if msg["type"] != "error" {
		t.Errorf("expected error, got %v", msg["type"])
	}
}

func TestServer_MultipleEventTypes(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	token := validToken(t, tenantID, uuid.New())

	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn) // auth.ok

	time.Sleep(50 * time.Millisecond)

	eventTypes := []string{
		"session.started", "session.ended", "sim.state_changed",
		"operator.health_changed", "alert.new", "job.progress",
		"job.completed", "notification.new", "policy.rollout_progress",
		"metrics.realtime",
	}

	for _, et := range eventTypes {
		srv.hub.BroadcastToTenant(tenantID, et, map[string]string{"test": et})
	}

	received := make(map[string]bool)
	for i := 0; i < len(eventTypes); i++ {
		msg := readJSON(t, conn)
		received[msg["type"].(string)] = true
	}

	for _, et := range eventTypes {
		if !received[et] {
			t.Errorf("missing event type: %s", et)
		}
	}
}

func TestServer_GracefulDisconnect(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	token := validToken(t, uuid.New(), uuid.New())
	conn := dialWS(t, wsURL(ts, token))
	readJSON(t, conn) // auth.ok

	time.Sleep(50 * time.Millisecond)
	if srv.hub.ConnectionCount() != 1 {
		t.Errorf("count = %d, want 1", srv.hub.ConnectionCount())
	}

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")
	_ = conn.WriteMessage(websocket.CloseMessage, closeMsg)
	conn.Close()

	time.Sleep(100 * time.Millisecond)
	if srv.hub.ConnectionCount() != 0 {
		t.Errorf("count after close = %d, want 0", srv.hub.ConnectionCount())
	}
}

func TestServer_BroadcastAll_ReachesMultipleTenants(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantA := uuid.New()
	tenantB := uuid.New()

	connA := dialWS(t, wsURL(ts, validToken(t, tenantA, uuid.New())))
	defer connA.Close()
	readJSON(t, connA)

	connB := dialWS(t, wsURL(ts, validToken(t, tenantB, uuid.New())))
	defer connB.Close()
	readJSON(t, connB)

	time.Sleep(50 * time.Millisecond)

	srv.hub.BroadcastAll("metrics.realtime", map[string]string{"auth_per_sec": "100"})

	msgA := readJSON(t, connA)
	if msgA["type"] != "metrics.realtime" {
		t.Errorf("tenant A: expected metrics.realtime, got %v", msgA["type"])
	}

	msgB := readJSON(t, connB)
	if msgB["type"] != "metrics.realtime" {
		t.Errorf("tenant B: expected metrics.realtime, got %v", msgB["type"])
	}
}

func TestNATSSubjectToWSType_WithNewMappings(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"argus.events.policy.rollout_progress", "policy.rollout_progress"},
	}

	for _, tt := range tests {
		got := natsSubjectToWSType(tt.subject)
		if got != tt.want {
			t.Errorf("natsSubjectToWSType(%q) = %q, want %q", tt.subject, got, tt.want)
		}
	}
}

func TestServer_EmptySubscribeEvents(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	token := validToken(t, uuid.New(), uuid.New())
	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn) // auth.ok

	subMsg := map[string]interface{}{
		"type":   "subscribe",
		"events": []string{},
	}
	data, _ := json.Marshal(subMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	msg := readJSON(t, conn)
	if msg["type"] != "error" {
		t.Errorf("expected error for empty subscribe, got %v", msg["type"])
	}
}

func TestServer_SubscribeWildcard(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	token := validToken(t, tenantID, uuid.New())

	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn) // auth.ok

	subMsg := map[string]interface{}{
		"type":   "subscribe",
		"events": []string{"*"},
	}
	data, _ := json.Marshal(subMsg)
	_ = conn.WriteMessage(websocket.TextMessage, data)
	readJSON(t, conn) // subscribe.ok

	time.Sleep(50 * time.Millisecond)

	srv.hub.BroadcastToTenant(tenantID, "alert.new", map[string]string{"x": "1"})
	srv.hub.BroadcastToTenant(tenantID, "session.ended", map[string]string{"x": "2"})

	msg1 := readJSON(t, conn)
	msg2 := readJSON(t, conn)

	types := map[string]bool{
		msg1["type"].(string): true,
		msg2["type"].(string): true,
	}
	if !types["alert.new"] || !types["session.ended"] {
		t.Errorf("wildcard should receive all events, got %v", types)
	}
}

func TestServer_MaxConnections_DifferentTenants(t *testing.T) {
	_, ts := setupTestServer(t, 1)
	defer ts.Close()

	tenantA := uuid.New()
	tenantB := uuid.New()

	connA := dialWS(t, wsURL(ts, validToken(t, tenantA, uuid.New())))
	defer connA.Close()
	readJSON(t, connA)

	connB := dialWS(t, wsURL(ts, validToken(t, tenantB, uuid.New())))
	defer connB.Close()
	readJSON(t, connB)

	time.Sleep(50 * time.Millisecond)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts, validToken(t, tenantA, uuid.New())), nil)
	if err != nil {
		return
	}
	defer conn.Close()

	readJSON(t, conn) // auth.ok
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, _, readErr := conn.ReadMessage()
	if readErr == nil {
		t.Error("2nd connection for tenant A should be rejected")
	}
}

func TestServer_AuthTimeoutNoMessage(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	conn := dialWS(t, wsURL(ts, ""))
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(7 * time.Second))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Error("should close connection after auth timeout")
	}
}

func TestServer_FirstMessageAuth_NotAuthType(t *testing.T) {
	_, ts := setupTestServer(t, 0)
	defer ts.Close()

	conn := dialWS(t, wsURL(ts, ""))
	defer conn.Close()

	badMsg := map[string]string{"type": "subscribe"}
	data, _ := json.Marshal(badMsg)
	_ = conn.WriteMessage(websocket.TextMessage, data)

	msg := readJSON(t, conn)
	if msg["type"] != "auth.error" {
		t.Errorf("expected auth.error, got %v", msg["type"])
	}
}

func TestServer_EventEnvelopeFormat(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	token := validToken(t, tenantID, uuid.New())

	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn)

	time.Sleep(50 * time.Millisecond)

	srv.hub.BroadcastToTenant(tenantID, "session.started", map[string]string{"session_id": "test"})

	msg := readJSON(t, conn)
	if _, ok := msg["type"]; !ok {
		t.Error("envelope missing type")
	}
	if _, ok := msg["id"]; !ok {
		t.Error("envelope missing id")
	}
	if _, ok := msg["timestamp"]; !ok {
		t.Error("envelope missing timestamp")
	}
	if _, ok := msg["data"]; !ok {
		t.Error("envelope missing data")
	}

	id := msg["id"].(string)
	if !strings.HasPrefix(id, "evt_") {
		t.Errorf("id should start with evt_, got %s", id)
	}
}

func TestServer_MultipleClientsPerTenant(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	token1 := validToken(t, tenantID, uuid.New())
	token2 := validToken(t, tenantID, uuid.New())

	conn1 := dialWS(t, wsURL(ts, token1))
	defer conn1.Close()
	readJSON(t, conn1)

	conn2 := dialWS(t, wsURL(ts, token2))
	defer conn2.Close()
	readJSON(t, conn2)

	time.Sleep(50 * time.Millisecond)

	if srv.hub.TenantConnectionCount(tenantID) != 2 {
		t.Errorf("tenant count = %d, want 2", srv.hub.TenantConnectionCount(tenantID))
	}

	srv.hub.BroadcastToTenant(tenantID, "alert.new", map[string]string{"msg": "test"})

	msg1 := readJSON(t, conn1)
	msg2 := readJSON(t, conn2)

	if msg1["type"] != "alert.new" || msg2["type"] != "alert.new" {
		t.Error("both clients should receive the event")
	}
}

func TestServer_SequentialSubscribeUpdatesFilter(t *testing.T) {
	srv, ts := setupTestServer(t, 0)
	defer ts.Close()

	tenantID := uuid.New()
	token := validToken(t, tenantID, uuid.New())

	conn := dialWS(t, wsURL(ts, token))
	defer conn.Close()
	readJSON(t, conn)

	sub1 := map[string]interface{}{"type": "subscribe", "events": []string{"alert.new"}}
	d1, _ := json.Marshal(sub1)
	_ = conn.WriteMessage(websocket.TextMessage, d1)
	readJSON(t, conn) // subscribe.ok

	time.Sleep(50 * time.Millisecond)

	srv.hub.BroadcastToTenant(tenantID, "session.started", map[string]string{"x": "1"})
	srv.hub.BroadcastToTenant(tenantID, "alert.new", map[string]string{"x": "marker"})

	msg1 := readJSON(t, conn)
	if msg1["type"] != "alert.new" {
		t.Errorf("expected alert.new (session.started should be filtered), got %v", msg1["type"])
	}

	sub2 := map[string]interface{}{"type": "subscribe", "events": []string{"session.started"}}
	d2, _ := json.Marshal(sub2)
	_ = conn.WriteMessage(websocket.TextMessage, d2)
	readJSON(t, conn) // subscribe.ok

	time.Sleep(50 * time.Millisecond)

	srv.hub.BroadcastToTenant(tenantID, "session.started", map[string]string{"x": "2"})

	msg := readJSON(t, conn)
	if msg["type"] != "session.started" {
		t.Errorf("expected session.started after re-subscribe, got %v", msg["type"])
	}
}

func TestServer_ManyConnections(t *testing.T) {
	srv, ts := setupTestServer(t, 50)
	defer ts.Close()

	tenantID := uuid.New()
	conns := make([]*websocket.Conn, 0, 20)

	for i := 0; i < 20; i++ {
		c := dialWS(t, wsURL(ts, validToken(t, tenantID, uuid.New())))
		readJSON(t, c)
		conns = append(conns, c)
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	time.Sleep(100 * time.Millisecond)

	if srv.hub.TenantConnectionCount(tenantID) != 20 {
		t.Errorf("count = %d, want 20", srv.hub.TenantConnectionCount(tenantID))
	}
	if srv.hub.ConnectionCount() != 20 {
		t.Errorf("total count = %d, want 20", srv.hub.ConnectionCount())
	}

	srv.hub.BroadcastToTenant(tenantID, "alert.new", map[string]string{"msg": "mass"})

	for i, c := range conns {
		msg := readJSON(t, c)
		if msg["type"] != "alert.new" {
			t.Errorf("conn %d: expected alert.new, got %v", i, msg["type"])
		}
	}
}

func TestServer_SlowClientBackpressure(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenantID := uuid.New()
	conn := &Connection{
		TenantID: tenantID,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
	}
	hub.Register(conn)

	for i := 0; i < 300; i++ {
		hub.BroadcastToTenant(tenantID, "job.progress", map[string]string{
			"i": fmt.Sprintf("%d", i),
		})
	}

	count := len(conn.SendCh)
	if count > 256 {
		t.Errorf("should have dropped messages, got %d in buffer (max=256)", count)
	}
	if count != 256 {
		t.Errorf("buffer should be full (256), got %d", count)
	}
}
