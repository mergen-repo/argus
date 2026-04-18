package radius

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
)

func TestBuildAccessRejectPacket(t *testing.T) {
	secret := []byte("testing123")
	req := radius.New(radius.CodeAccessRequest, secret)
	rfc2865.UserName_SetString(req, "286010000000001")

	reject := req.Response(radius.CodeAccessReject)
	rfc2865.ReplyMessage_SetString(reject, "SIM_NOT_FOUND")

	msg, err := rfc2865.ReplyMessage_LookupString(reject)
	if err != nil {
		t.Fatalf("ReplyMessage_Lookup: %v", err)
	}
	if msg != "SIM_NOT_FOUND" {
		t.Errorf("ReplyMessage = %q, want SIM_NOT_FOUND", msg)
	}
	if reject.Code != radius.CodeAccessReject {
		t.Errorf("Code = %d, want %d", reject.Code, radius.CodeAccessReject)
	}
}

func TestBuildAccessAcceptPacket(t *testing.T) {
	secret := []byte("testing123")
	req := radius.New(radius.CodeAccessRequest, secret)
	rfc2865.UserName_SetString(req, "286010000000002")

	accept := req.Response(radius.CodeAccessAccept)
	rfc2865.FramedIPAddress_Set(accept, net.ParseIP("10.0.1.100").To4())
	rfc2865.SessionTimeout_Set(accept, rfc2865.SessionTimeout(86400))
	rfc2865.IdleTimeout_Set(accept, rfc2865.IdleTimeout(3600))
	rfc2865.FilterID_SetString(accept, "default")

	if accept.Code != radius.CodeAccessAccept {
		t.Errorf("Code = %d, want %d", accept.Code, radius.CodeAccessAccept)
	}

	ip, err := rfc2865.FramedIPAddress_Lookup(accept)
	if err != nil {
		t.Fatalf("FramedIPAddress_Lookup: %v", err)
	}
	if ip.String() != "10.0.1.100" {
		t.Errorf("FramedIPAddress = %s, want 10.0.1.100", ip.String())
	}

	timeout, err := rfc2865.SessionTimeout_Lookup(accept)
	if err != nil {
		t.Fatalf("SessionTimeout_Lookup: %v", err)
	}
	if timeout != 86400 {
		t.Errorf("SessionTimeout = %d, want 86400", timeout)
	}

	idle, err := rfc2865.IdleTimeout_Lookup(accept)
	if err != nil {
		t.Fatalf("IdleTimeout_Lookup: %v", err)
	}
	if idle != 3600 {
		t.Errorf("IdleTimeout = %d, want 3600", idle)
	}

	filter, err := rfc2865.FilterID_LookupString(accept)
	if err != nil {
		t.Fatalf("FilterID_Lookup: %v", err)
	}
	if filter != "default" {
		t.Errorf("FilterID = %q, want default", filter)
	}
}

func TestAccountingPacketParsing(t *testing.T) {
	secret := []byte("testing123")

	pkt := radius.New(radius.CodeAccountingRequest, secret)
	rfc2866.AcctStatusType_Set(pkt, rfc2866.AcctStatusType_Value_Start)
	rfc2866.AcctSessionID_SetString(pkt, "sess-001")
	rfc2865.UserName_SetString(pkt, "286010000000003")

	statusType, err := rfc2866.AcctStatusType_Lookup(pkt)
	if err != nil {
		t.Fatalf("AcctStatusType_Lookup: %v", err)
	}
	if statusType != rfc2866.AcctStatusType_Value_Start {
		t.Errorf("AcctStatusType = %d, want Start(1)", statusType)
	}

	acctSessID, err := rfc2866.AcctSessionID_LookupString(pkt)
	if err != nil {
		t.Fatalf("AcctSessionID_Lookup: %v", err)
	}
	if acctSessID != "sess-001" {
		t.Errorf("AcctSessionID = %q, want sess-001", acctSessID)
	}
}

func TestAccountingInterimParsing(t *testing.T) {
	secret := []byte("testing123")

	pkt := radius.New(radius.CodeAccountingRequest, secret)
	rfc2866.AcctStatusType_Set(pkt, rfc2866.AcctStatusType_Value_InterimUpdate)
	rfc2866.AcctSessionID_SetString(pkt, "sess-002")
	rfc2866.AcctInputOctets_Set(pkt, rfc2866.AcctInputOctets(1024000))
	rfc2866.AcctOutputOctets_Set(pkt, rfc2866.AcctOutputOctets(2048000))

	bytesIn := uint64(rfc2866.AcctInputOctets_Get(pkt))
	bytesOut := uint64(rfc2866.AcctOutputOctets_Get(pkt))

	if bytesIn != 1024000 {
		t.Errorf("AcctInputOctets = %d, want 1024000", bytesIn)
	}
	if bytesOut != 2048000 {
		t.Errorf("AcctOutputOctets = %d, want 2048000", bytesOut)
	}
}

func TestAccountingStopParsing(t *testing.T) {
	secret := []byte("testing123")

	pkt := radius.New(radius.CodeAccountingRequest, secret)
	rfc2866.AcctStatusType_Set(pkt, rfc2866.AcctStatusType_Value_Stop)
	rfc2866.AcctSessionID_SetString(pkt, "sess-003")
	rfc2866.AcctTerminateCause_Set(pkt, rfc2866.AcctTerminateCause_Value_UserRequest)

	cause, err := rfc2866.AcctTerminateCause_Lookup(pkt)
	if err != nil {
		t.Fatalf("AcctTerminateCause_Lookup: %v", err)
	}
	if cause != rfc2866.AcctTerminateCause_Value_UserRequest {
		t.Errorf("AcctTerminateCause = %d, want UserRequest(1)", cause)
	}
}

func TestServerLifecycle(t *testing.T) {
	authAddr := findFreeUDPPort(t)
	acctAddr := findFreeUDPPort(t)

	sessionMgr := session.NewManager(nil, nil, zerolog.Nop())
	simCache := NewSIMCache(nil, nil, zerolog.Nop())

	srv := NewServer(
		ServerConfig{
			AuthAddr:       authAddr,
			AcctAddr:       acctAddr,
			DefaultSecret:  "testing123",
			WorkerPoolSize: 4,
		},
		simCache,
		sessionMgr,
		store.NewOperatorStore(nil),
		store.NewIPPoolStore(nil),
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)

	if srv.Healthy() {
		t.Error("server should not be healthy before start")
	}

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if !srv.Healthy() {
		t.Error("server should be healthy after start")
	}

	count, err := srv.ActiveSessionCount(ctx)
	if err != nil {
		t.Fatalf("ActiveSessionCount: %v", err)
	}
	if count != 0 {
		t.Errorf("ActiveSessionCount = %d, want 0", count)
	}

	if err := srv.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if srv.Healthy() {
		t.Error("server should not be healthy after stop")
	}
}

func TestServerDoubleStart(t *testing.T) {
	authAddr := findFreeUDPPort(t)
	acctAddr := findFreeUDPPort(t)

	sessionMgr := session.NewManager(nil, nil, zerolog.Nop())
	simCache := NewSIMCache(nil, nil, zerolog.Nop())

	srv := NewServer(
		ServerConfig{
			AuthAddr:       authAddr,
			AcctAddr:       acctAddr,
			DefaultSecret:  "testing123",
			WorkerPoolSize: 4,
		},
		simCache,
		sessionMgr,
		store.NewOperatorStore(nil),
		store.NewIPPoolStore(nil),
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(ctx)

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("second Start should be no-op: %v", err)
	}
}

func TestSIMCacheNilRedis(t *testing.T) {
	cache := NewSIMCache(nil, nil, zerolog.Nop())

	_, err := cache.GetByIMSI(context.Background(), "286010000000001")
	if err == nil {
		t.Error("expected error with nil store")
	}
}

func TestAuthHandler_UnknownIMSI_AccessReject(t *testing.T) {
	secret := "testing123"
	authAddr := findFreeUDPPort(t)
	acctAddr := findFreeUDPPort(t)

	simCache := NewSIMCache(nil, nil, zerolog.Nop())
	sessionMgr := session.NewManager(nil, nil, zerolog.Nop())

	srv := NewServer(
		ServerConfig{
			AuthAddr:       authAddr,
			AcctAddr:       acctAddr,
			DefaultSecret:  secret,
			WorkerPoolSize: 4,
		},
		simCache,
		sessionMgr,
		store.NewOperatorStore(nil),
		store.NewIPPoolStore(nil),
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	resp := sendAuthRequest(t, authAddr, secret, "999999999999999")
	if resp.Code != radius.CodeAccessReject {
		t.Errorf("Code = %d, want AccessReject(%d)", resp.Code, radius.CodeAccessReject)
	}

	msg, err := rfc2865.ReplyMessage_LookupString(resp)
	if err != nil {
		t.Fatalf("ReplyMessage_Lookup: %v", err)
	}
	if msg != "SIM_NOT_FOUND" {
		t.Errorf("ReplyMessage = %q, want SIM_NOT_FOUND", msg)
	}
}

func TestAuthHandler_MissingIMSI_AccessReject(t *testing.T) {
	secret := "testing123"
	authAddr := findFreeUDPPort(t)
	acctAddr := findFreeUDPPort(t)

	simCache := NewSIMCache(nil, nil, zerolog.Nop())
	sessionMgr := session.NewManager(nil, nil, zerolog.Nop())

	srv := NewServer(
		ServerConfig{
			AuthAddr:       authAddr,
			AcctAddr:       acctAddr,
			DefaultSecret:  secret,
			WorkerPoolSize: 4,
		},
		simCache,
		sessionMgr,
		store.NewOperatorStore(nil),
		store.NewIPPoolStore(nil),
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	pkt := radius.New(radius.CodeAccessRequest, []byte(secret))
	resp := sendPacketUDP(t, authAddr, secret, pkt)
	if resp.Code != radius.CodeAccessReject {
		t.Errorf("Code = %d, want AccessReject(%d)", resp.Code, radius.CodeAccessReject)
	}

	msg, err := rfc2865.ReplyMessage_LookupString(resp)
	if err != nil {
		t.Fatalf("ReplyMessage_Lookup: %v", err)
	}
	if msg != "MISSING_IMSI" {
		t.Errorf("ReplyMessage = %q, want MISSING_IMSI", msg)
	}
}

func TestAccountingResponse(t *testing.T) {
	secret := "testing123"
	authAddr := findFreeUDPPort(t)
	acctAddr := findFreeUDPPort(t)

	simCache := NewSIMCache(nil, nil, zerolog.Nop())
	sessionMgr := session.NewManager(nil, nil, zerolog.Nop())

	srv := NewServer(
		ServerConfig{
			AuthAddr:       authAddr,
			AcctAddr:       acctAddr,
			DefaultSecret:  secret,
			WorkerPoolSize: 4,
		},
		simCache,
		sessionMgr,
		store.NewOperatorStore(nil),
		store.NewIPPoolStore(nil),
		nil,
		nil,
		nil,
		zerolog.Nop(),
	)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(ctx)

	time.Sleep(50 * time.Millisecond)

	pkt := radius.New(radius.CodeAccountingRequest, []byte(secret))
	rfc2866.AcctStatusType_Set(pkt, rfc2866.AcctStatusType_Value_Start)
	rfc2866.AcctSessionID_SetString(pkt, "test-acct-001")
	rfc2865.UserName_SetString(pkt, "286010000000001")

	resp := sendPacketUDP(t, acctAddr, secret, pkt)
	if resp.Code != radius.CodeAccountingResponse {
		t.Errorf("Code = %d, want AccountingResponse(%d)", resp.Code, radius.CodeAccountingResponse)
	}
}

func TestGetOperatorSecret_WithConfig(t *testing.T) {
	srv := &Server{defaultSecret: "global_secret"}

	op := &store.Operator{
		AdapterConfig: json.RawMessage(`{"radius_secret": "per_op_secret"}`),
	}

	secret := srv.getOperatorSecret(op)
	if string(secret) != "per_op_secret" {
		t.Errorf("getOperatorSecret = %q, want per_op_secret", string(secret))
	}
}

func TestGetOperatorSecret_FallbackToDefault(t *testing.T) {
	srv := &Server{defaultSecret: "global_secret"}

	op := &store.Operator{
		AdapterConfig: json.RawMessage(`{"host": "10.0.0.1"}`),
	}

	secret := srv.getOperatorSecret(op)
	if string(secret) != "global_secret" {
		t.Errorf("getOperatorSecret = %q, want global_secret", string(secret))
	}
}

func TestGetOperatorSecret_NilOperator(t *testing.T) {
	srv := &Server{defaultSecret: "global_secret"}

	secret := srv.getOperatorSecret(nil)
	if string(secret) != "global_secret" {
		t.Errorf("getOperatorSecret = %q, want global_secret", string(secret))
	}
}

func TestGetOperatorSecret_EmptyConfig(t *testing.T) {
	srv := &Server{defaultSecret: "global_secret"}

	op := &store.Operator{}

	secret := srv.getOperatorSecret(op)
	if string(secret) != "global_secret" {
		t.Errorf("getOperatorSecret = %q, want global_secret", string(secret))
	}
}

func sendAuthRequest(t *testing.T, addr, secret, imsi string) *radius.Packet {
	t.Helper()
	pkt := radius.New(radius.CodeAccessRequest, []byte(secret))
	rfc2865.UserName_SetString(pkt, imsi)
	return sendPacketUDP(t, addr, secret, pkt)
}

func sendPacketUDP(t *testing.T, addr, secret string, pkt *radius.Packet) *radius.Packet {
	t.Helper()
	encoded, err := pkt.Encode()
	if err != nil {
		t.Fatalf("encode packet: %v", err)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1"+addr)
	if err != nil {
		t.Fatalf("resolve addr: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Write(encoded); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	resp, err := radius.Parse(buf[:n], []byte(secret))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}

	return resp
}

func findFreeUDPPort(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()
	_, port, _ := net.SplitHostPort(addr)
	return fmt.Sprintf(":%s", port)
}

// Verify that UUID is unused import is resolved
var _ = uuid.New

// TestRADIUSAccessAccept_DynamicAllocation — STORY-092 Wave 1 AC-1.
//
// Happy path: a SIM whose sims.ip_address_id IS NULL but whose APN has an
// active ip_pool with available addresses MUST receive an Access-Accept
// with Framed-IP-Address, AND the allocation MUST be persisted to
// ip_addresses.state='allocated' and sims.ip_address_id.
//
// DB-gated integration test (skipped without DATABASE_URL). Builds a
// minimal fixture: tenant → operator → apn → pool + 5 ip_addresses → SIM.
// Calls handleDirectAuth directly (bypassing UDP) via an in-process
// writer so the test remains fast and deterministic.
func TestRADIUSAccessAccept_DynamicAllocation(t *testing.T) {
	testRADIUSDynamicAllocHappyPath(t)
}
