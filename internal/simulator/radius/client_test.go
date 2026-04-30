package radius

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/simulator/discovery"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

func TestNewSessionContext_FieldsPopulated(t *testing.T) {
	apnName := "iot.xyz.local"
	msisdn := "+905310000001"
	sim := discovery.SIM{
		ID:           "s1",
		TenantID:     "t1",
		OperatorID:   "o1",
		OperatorCode: "turkcell",
		MCC:          "286",
		MNC:          "01",
		APNName:      &apnName,
		IMSI:         "2860100001",
		MSISDN:       &msisdn,
		ICCID:        "8990286010000100001001",
	}
	sc := NewSessionContext(sim, "10.99.0.1", "sim-turkcell")
	if sc.AcctSessionID == "" {
		t.Error("AcctSessionID must be generated")
	}
	if sc.NASIP != "10.99.0.1" || sc.NASIdentifier != "sim-turkcell" {
		t.Errorf("NAS fields not set: ip=%q id=%q", sc.NASIP, sc.NASIdentifier)
	}
	if sc.SIM.IMSI != "2860100001" {
		t.Errorf("IMSI not copied: %q", sc.SIM.IMSI)
	}
	if sc.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
}

func TestNewSessionContext_UniqueSessionIDs(t *testing.T) {
	sim := discovery.SIM{OperatorCode: "turkcell", IMSI: "x"}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		sc := NewSessionContext(sim, "10.0.0.1", "nas")
		if seen[sc.AcctSessionID] {
			t.Fatalf("duplicate AcctSessionID: %s", sc.AcctSessionID)
		}
		seen[sc.AcctSessionID] = true
	}
}

func TestNew_ClientAddressesFormatted(t *testing.T) {
	c := New("argus-app", 1812, 1813, "secret")
	if c.authAddr != "argus-app:1812" {
		t.Errorf("auth addr: %q", c.authAddr)
	}
	if c.acctAddr != "argus-app:1813" {
		t.Errorf("acct addr: %q", c.acctAddr)
	}
	if string(c.secret) != "secret" {
		t.Errorf("secret: %q", string(c.secret))
	}
}

// startMockAuthServer binds a UDP socket and runs a RADIUS auth server in the
// background. The handler fn builds the response for each request. The server
// shuts down when ctx is cancelled. Returns the bound address.
func startMockAuthServer(t *testing.T, ctx context.Context, secret []byte, handler radius.HandlerFunc) string {
	t.Helper()
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve udp addr: %v", err)
	}
	pc, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	srv := &radius.PacketServer{
		SecretSource: radius.StaticSecretSource(secret),
		Handler:      handler,
	}
	go func() {
		<-ctx.Done()
		_ = pc.Close()
		_ = srv.Shutdown(context.Background())
	}()
	go func() { _ = srv.Serve(pc) }()
	return pc.LocalAddr().String()
}

// TestAuth_AcceptSessionTimeoutAndReplyMessage verifies that when the mock
// RADIUS server returns Access-Accept with Session-Timeout=120 and
// Reply-Message="test", Auth() populates the corresponding SessionContext fields.
func TestAuth_AcceptSessionTimeoutAndReplyMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	secret := []byte("testsecret")
	addr := startMockAuthServer(t, ctx, secret, radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		resp := r.Response(radius.CodeAccessAccept)
		_ = rfc2865.SessionTimeout_Set(resp, rfc2865.SessionTimeout(120))
		_ = rfc2865.ReplyMessage_SetString(resp, "test")
		_ = rfc2865.FramedIPAddress_Set(resp, net.ParseIP("10.1.2.3"))
		w.Write(resp)
	}))

	c := &Client{
		authAddr:  addr,
		acctAddr:  addr,
		secret:    secret,
		dialer:    &net.Dialer{Timeout: 3 * time.Second},
		rwTimeout: 3 * time.Second,
		retries:   0,
	}
	sc := NewSessionContext(discovery.SIM{OperatorCode: "turkcell", IMSI: "test-imsi"}, "10.0.0.1", "nas")

	resp, err := c.Auth(ctx, sc)
	if err != nil {
		t.Fatalf("Auth returned error: %v", err)
	}
	if resp.Code != radius.CodeAccessAccept {
		t.Fatalf("expected Access-Accept, got %s", resp.Code)
	}
	if sc.ServerSessionTimeout != 120*time.Second {
		t.Errorf("ServerSessionTimeout: got %v, want %v", sc.ServerSessionTimeout, 120*time.Second)
	}
	if sc.ReplyMessage != "test" {
		t.Errorf("ReplyMessage: got %q, want %q", sc.ReplyMessage, "test")
	}
	if sc.FramedIP == nil || sc.FramedIP.String() != "10.1.2.3" {
		t.Errorf("FramedIP: got %v, want 10.1.2.3", sc.FramedIP)
	}
}

// TestAuth_RejectReplyMessage verifies that Reply-Message is extracted even on
// Access-Reject responses (some NAS deployments include it on reject).
func TestAuth_RejectReplyMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	secret := []byte("testsecret")
	addr := startMockAuthServer(t, ctx, secret, radius.HandlerFunc(func(w radius.ResponseWriter, r *radius.Request) {
		resp := r.Response(radius.CodeAccessReject)
		_ = rfc2865.ReplyMessage_SetString(resp, "rejected-reason")
		w.Write(resp)
	}))

	c := &Client{
		authAddr:  addr,
		acctAddr:  addr,
		secret:    secret,
		dialer:    &net.Dialer{Timeout: 3 * time.Second},
		rwTimeout: 3 * time.Second,
		retries:   0,
	}
	sc := NewSessionContext(discovery.SIM{OperatorCode: "turkcell", IMSI: "test-imsi"}, "10.0.0.1", "nas")

	resp, err := c.Auth(ctx, sc)
	if err != nil {
		t.Fatalf("Auth returned error: %v", err)
	}
	if resp.Code != radius.CodeAccessReject {
		t.Fatalf("expected Access-Reject, got %s", resp.Code)
	}
	if sc.ReplyMessage != "rejected-reason" {
		t.Errorf("ReplyMessage on Reject: got %q, want %q", sc.ReplyMessage, "rejected-reason")
	}
	if sc.ServerSessionTimeout != 0 {
		t.Errorf("ServerSessionTimeout should be zero on Reject, got %v", sc.ServerSessionTimeout)
	}
}

