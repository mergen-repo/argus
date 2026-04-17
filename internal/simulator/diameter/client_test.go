package diameter

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/btopcu/argus/internal/simulator/discovery"
	"github.com/btopcu/argus/internal/simulator/metrics"
	"github.com/btopcu/argus/internal/simulator/radius"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
)

func init() {
	metrics.MustRegister(prometheus.DefaultRegisterer)
}

// testOperatorCfg returns an OperatorConfig with both Gx and Gy enabled.
func testOperatorCfg(port int) config.OperatorConfig {
	return config.OperatorConfig{
		Code:          "test-op",
		NASIdentifier: "test-nas",
		NASIP:         "127.0.0.1",
		Diameter: &config.OperatorDiameterConfig{
			Enabled:      true,
			OriginHost:   "sim-test.sim.argus.test",
			Applications: []string{"gx", "gy"},
		},
	}
}

func testDefaults(port int) config.DiameterDefaults {
	return config.DiameterDefaults{
		Host:                "127.0.0.1",
		Port:                port,
		OriginRealm:         "sim.argus.test",
		DestinationRealm:    "argus.local",
		WatchdogInterval:    200 * time.Millisecond,
		ConnectTimeout:      2 * time.Second,
		RequestTimeout:      2 * time.Second,
		ReconnectBackoffMin: 50 * time.Millisecond,
		ReconnectBackoffMax: 200 * time.Millisecond,
	}
}

func testSessionContext() *radius.SessionContext {
	msisdn := "905001234567"
	return &radius.SessionContext{
		SIM: discovery.SIM{
			IMSI:   "286010000000001",
			MSISDN: &msisdn,
		},
		NASIP:         "127.0.0.1",
		NASIdentifier: "test-nas",
		AcctSessionID: "sess-test-001",
		FramedIP:      net.ParseIP("10.0.0.1"),
		StartedAt:     time.Now(),
	}
}

// startClientAndWait builds a Client, starts it, and waits for the peer to open.
func startClientAndWait(t *testing.T, cfg config.OperatorConfig, defaults config.DiameterDefaults) *Client {
	t.Helper()
	c := New(cfg, defaults, zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)

	ready := c.Start(ctx)
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not reach Open within 3s")
	}

	if c.peer.State() != PeerStateOpen {
		t.Fatalf("peer not open; state=%s", c.peer.State())
	}
	return c
}

// mockServer handles CER+CEA then dispatches CCR with CCA replies carrying result 2001.
// Tracks each CCR-I/U/T received on the seen channel.
type mockServer struct {
	fs   *fakeServer
	seen chan *argusdiameter.Message
}

func newMockServer(t *testing.T) *mockServer {
	t.Helper()
	return &mockServer{
		fs:   newFakeServer(t),
		seen: make(chan *argusdiameter.Message, 64),
	}
}

func (ms *mockServer) port() int { return ms.fs.port() }
func (ms *mockServer) close()    { ms.fs.close() }

// serveOne accepts one connection and handles CER + all CCRs + DPR.
func (ms *mockServer) serveOne(t *testing.T) {
	t.Helper()
	ms.fs.acceptOne(t, func(conn net.Conn) {
		defer conn.Close()
		ms.handleConn(t, conn)
	})
}

func (ms *mockServer) handleConn(t *testing.T, conn net.Conn) {
	t.Helper()
	for {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		msg, err := readMsgErr(conn)
		if err != nil {
			return
		}

		if msg.IsRequest() {
			switch msg.CommandCode {
			case argusdiameter.CommandCER:
				sendCEA(t, conn, msg)
			case argusdiameter.CommandCCR:
				ms.seen <- msg
				cca := argusdiameter.NewAnswer(msg)
				cca.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ResultCodeSuccess))
				cca.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, "argus.test"))
				cca.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, "argus.local"))
				writeMsg(t, conn, cca)
			case argusdiameter.CommandDWR:
				dwa := argusdiameter.NewAnswer(msg)
				dwa.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ResultCodeSuccess))
				dwa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, "argus.test"))
				dwa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, "argus.local"))
				writeMsg(t, conn, dwa)
			case argusdiameter.CommandDPR:
				dpa := argusdiameter.NewAnswer(msg)
				dpa.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ResultCodeSuccess))
				dpa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, "argus.test"))
				dpa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, "argus.local"))
				writeMsg(t, conn, dpa)
				return
			}
		}
	}
}

// readMsgErr reads one Diameter message, returning error instead of calling t.Fatal.
func readMsgErr(conn net.Conn) (*argusdiameter.Message, error) {
	headerBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		return nil, err
	}
	msgLen, err := argusdiameter.ReadMessageLength(headerBuf)
	if err != nil {
		return nil, err
	}
	msgBuf := make([]byte, msgLen)
	copy(msgBuf[:4], headerBuf)
	if _, err := io.ReadFull(conn, msgBuf[4:]); err != nil {
		return nil, err
	}
	return argusdiameter.DecodeMessage(msgBuf)
}

// TestClient_OpenSession_SendsCorrectCCRI verifies that OpenSession sends a
// Gx CCR-I with the expected AVPs (Session-Id, CC-Request-Type=1, CC-Request-Number=0).
func TestClient_OpenSession_SendsCorrectCCRI(t *testing.T) {
	ms := newMockServer(t)
	defer ms.close()
	ms.serveOne(t)

	c := startClientAndWait(t, testOperatorCfg(ms.port()), testDefaults(ms.port()))
	sc := testSessionContext()

	ctx := context.Background()
	if err := c.OpenSession(ctx, sc); err != nil {
		t.Fatalf("OpenSession failed: %v", err)
	}

	// Expect two CCRs: Gx CCR-I and Gy CCR-I.
	var gxCCRI, gyCCRI *argusdiameter.Message
	timeout := time.After(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case msg := <-ms.seen:
			if msg.ApplicationID == argusdiameter.ApplicationIDGx {
				gxCCRI = msg
			} else if msg.ApplicationID == argusdiameter.ApplicationIDGy {
				gyCCRI = msg
			}
		case <-timeout:
			t.Fatal("did not receive 2 CCRs within 2s")
		}
	}

	if gxCCRI == nil {
		t.Fatal("did not receive Gx CCR-I")
	}
	if gyCCRI == nil {
		t.Fatal("did not receive Gy CCR-I")
	}

	// Verify Gx CCR-I AVPs.
	sessionID := gxCCRI.GetSessionID()
	if sessionID != sc.AcctSessionID {
		t.Errorf("Gx CCR-I: Session-Id=%q, want %q", sessionID, sc.AcctSessionID)
	}
	ccType := gxCCRI.GetCCRequestType()
	if ccType != argusdiameter.CCRequestTypeInitial {
		t.Errorf("Gx CCR-I: CC-Request-Type=%d, want %d", ccType, argusdiameter.CCRequestTypeInitial)
	}
	ccNum := gxCCRI.GetCCRequestNumber()
	if ccNum != 0 {
		t.Errorf("Gx CCR-I: CC-Request-Number=%d, want 0", ccNum)
	}
	if gxCCRI.ApplicationID != argusdiameter.ApplicationIDGx {
		t.Errorf("Gx CCR-I: ApplicationID=%d, want %d", gxCCRI.ApplicationID, argusdiameter.ApplicationIDGx)
	}
}

// TestClient_UpdateGy_IncrementsReqNum verifies that successive UpdateGy calls
// produce monotonically increasing CC-Request-Number values.
func TestClient_UpdateGy_IncrementsReqNum(t *testing.T) {
	ms := newMockServer(t)
	defer ms.close()
	ms.serveOne(t)

	c := startClientAndWait(t, testOperatorCfg(ms.port()), testDefaults(ms.port()))
	sc := testSessionContext()

	ctx := context.Background()
	if err := c.OpenSession(ctx, sc); err != nil {
		t.Fatalf("OpenSession failed: %v", err)
	}

	// Drain the CCR-I messages (Gx + Gy).
	drainN(t, ms.seen, 2, time.Second)

	const N = 3
	for i := 0; i < N; i++ {
		if err := c.UpdateGy(ctx, sc, 1024, 512, 10); err != nil {
			t.Fatalf("UpdateGy[%d] failed: %v", i, err)
		}
	}

	// Collect N CCR-U messages.
	ccrus := collectN(t, ms.seen, N, 2*time.Second)

	var lastNum uint32
	for i, msg := range ccrus {
		num := msg.GetCCRequestNumber()
		if num != uint32(i+1) {
			t.Errorf("CCR-U[%d]: CC-Request-Number=%d, want %d", i, num, i+1)
		}
		if i > 0 && num <= lastNum {
			t.Errorf("CC-Request-Number not monotonic: [%d]=%d, [%d]=%d", i-1, lastNum, i, num)
		}
		lastNum = num
	}
}

// TestClient_CloseSession_SendsCCRT verifies that CloseSession emits both
// Gx CCR-T and Gy CCR-T messages.
func TestClient_CloseSession_SendsCCRT(t *testing.T) {
	ms := newMockServer(t)
	defer ms.close()
	ms.serveOne(t)

	c := startClientAndWait(t, testOperatorCfg(ms.port()), testDefaults(ms.port()))
	sc := testSessionContext()

	ctx := context.Background()
	if err := c.OpenSession(ctx, sc); err != nil {
		t.Fatalf("OpenSession failed: %v", err)
	}
	drainN(t, ms.seen, 2, time.Second)

	if err := c.CloseSession(ctx, sc); err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	// Expect Gx CCR-T and Gy CCR-T.
	ccrs := collectN(t, ms.seen, 2, 2*time.Second)

	hasGxT := false
	hasGyT := false
	for _, msg := range ccrs {
		if msg.GetCCRequestType() != argusdiameter.CCRequestTypeTermination {
			t.Errorf("expected CCR-T but got type=%d", msg.GetCCRequestType())
		}
		switch msg.ApplicationID {
		case argusdiameter.ApplicationIDGx:
			hasGxT = true
		case argusdiameter.ApplicationIDGy:
			hasGyT = true
		}
	}

	if !hasGxT {
		t.Error("did not receive Gx CCR-T")
	}
	if !hasGyT {
		t.Error("did not receive Gy CCR-T")
	}
}

// TestClient_PeerDown_ReturnsSentinel verifies that OpenSession returns an
// error wrapping ErrPeerNotOpen when the peer cannot connect. The engine
// (not Client) is responsible for classifying this into
// DiameterSessionAbortedTotal{reason=peer_down}; Client only owes the wrapped
// sentinel so errors.Is(err, ErrPeerNotOpen) succeeds at the call site.
//
// Post-F-A3: Client no longer increments the counter itself — doing so
// double-counted when the engine also incremented on the returned error.
// The engine-side counter-classification is exercised by an engine test
// (and by plan AC-5/AC-6 integration).
func TestClient_PeerDown_ReturnsSentinel(t *testing.T) {
	// Point at an unreachable port (no listener).
	defaults := testDefaults(19998)
	defaults.ConnectTimeout = 100 * time.Millisecond
	defaults.RequestTimeout = 100 * time.Millisecond
	defaults.ReconnectBackoffMin = 50 * time.Millisecond
	defaults.ReconnectBackoffMax = 100 * time.Millisecond

	c := New(testOperatorCfg(19998), defaults, zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start but don't wait for open (peer won't connect).
	go c.peer.Run(ctx)

	sc := testSessionContext()

	err := c.OpenSession(ctx, sc)
	if err == nil {
		t.Fatal("expected error from OpenSession with unreachable peer, got nil")
	}

	if !errors.Is(err, ErrPeerNotOpen) {
		t.Errorf("error chain does not contain ErrPeerNotOpen: %v", err)
	}
}

// TestClient_GyCCRIFailure_DeletesCounter is the regression test for F-A2.
// Before this fix, when Gy CCR-I failed (peer down mid-OpenSession), the
// per-session counter inserted at client.go:140 was left in the map forever,
// producing unbounded growth over the Client's lifetime. The fix deletes the
// counter on the Gy CCR-I error path under the same mutex.
func TestClient_GyCCRIFailure_DeletesCounter(t *testing.T) {
	// Unreachable port; peer will never open, Gy CCR-I will fail with
	// ErrPeerNotOpen. (Gx CCR-I fails first with the same error, so we
	// exercise the Gx path here — to exercise the Gy path, temporarily
	// disable Gx via Applications=["gy"].)
	defaults := testDefaults(19997)
	defaults.ConnectTimeout = 100 * time.Millisecond
	defaults.RequestTimeout = 100 * time.Millisecond

	gyOnlyCfg := config.OperatorConfig{
		Code:          "test-op-gyonly",
		NASIdentifier: "test-nas",
		NASIP:         "127.0.0.1",
		Diameter: &config.OperatorDiameterConfig{
			Enabled:      true,
			OriginHost:   "sim-test.sim.argus.test",
			Applications: []string{"gy"},
		},
	}

	c := New(gyOnlyCfg, defaults, zerolog.Nop())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go c.peer.Run(ctx)

	sc := testSessionContext()

	// Drive many failed OpenSession calls; each inserted a counter before
	// the fix. Post-fix the map must stay at size 0.
	for i := 0; i < 50; i++ {
		if err := c.OpenSession(ctx, sc); err == nil {
			t.Fatalf("expected OpenSession to fail with unreachable peer, iter=%d", i)
		}
	}

	c.gyCountersMu.Lock()
	n := len(c.gyCounters)
	c.gyCountersMu.Unlock()

	if n != 0 {
		t.Errorf("gyCounters leak: expected 0 entries after 50 failed OpenSessions, got %d", n)
	}
}

// TestClient_GyDisabled_NoGyTraffic verifies that with Applications=["gx"] only,
// UpdateGy is a no-op and no Gy CCR-U is sent.
func TestClient_GyDisabled_NoGyTraffic(t *testing.T) {
	ms := newMockServer(t)
	defer ms.close()
	ms.serveOne(t)

	gxOnlyCfg := config.OperatorConfig{
		Code:          "test-op",
		NASIdentifier: "test-nas",
		NASIP:         "127.0.0.1",
		Diameter: &config.OperatorDiameterConfig{
			Enabled:      true,
			OriginHost:   "sim-test.sim.argus.test",
			Applications: []string{"gx"},
		},
	}

	c := startClientAndWait(t, gxOnlyCfg, testDefaults(ms.port()))
	sc := testSessionContext()

	ctx := context.Background()
	if err := c.OpenSession(ctx, sc); err != nil {
		t.Fatalf("OpenSession failed: %v", err)
	}

	// Only one CCR-I should have been sent (Gx only).
	gxMsgs := collectN(t, ms.seen, 1, time.Second)
	if len(gxMsgs) != 1 {
		t.Fatalf("expected 1 CCR-I, got %d", len(gxMsgs))
	}
	if gxMsgs[0].ApplicationID != argusdiameter.ApplicationIDGx {
		t.Errorf("expected Gx CCR-I, got app-id %d", gxMsgs[0].ApplicationID)
	}

	// UpdateGy should be a no-op.
	if err := c.UpdateGy(ctx, sc, 1024, 512, 10); err != nil {
		t.Fatalf("UpdateGy returned error (should be no-op): %v", err)
	}

	// No additional messages should have been sent.
	select {
	case msg := <-ms.seen:
		t.Errorf("unexpected message received after UpdateGy (Gy disabled): cmd=%d app=%d",
			msg.CommandCode, msg.ApplicationID)
	case <-time.After(200 * time.Millisecond):
		// Good — nothing extra sent.
	}
}

// Helpers

// drainN reads exactly n messages from ch within timeout, ignoring them.
func drainN(t *testing.T, ch <-chan *argusdiameter.Message, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for i := 0; i < n; i++ {
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("drainN: only drained %d/%d within timeout", i, n)
		}
	}
}

// collectN collects exactly n messages from ch within timeout.
func collectN(t *testing.T, ch <-chan *argusdiameter.Message, n int, timeout time.Duration) []*argusdiameter.Message {
	t.Helper()
	out := make([]*argusdiameter.Message, 0, n)
	deadline := time.After(timeout)
	for i := 0; i < n; i++ {
		select {
		case msg := <-ch:
			out = append(out, msg)
		case <-deadline:
			t.Fatalf("collectN: only got %d/%d within timeout", len(out), n)
		}
	}
	return out
}

