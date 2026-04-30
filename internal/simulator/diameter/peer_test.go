package diameter

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
	"github.com/rs/zerolog"
)

// fakeServer is an in-process Diameter server for testing.
type fakeServer struct {
	ln          net.Listener
	acceptCount atomic.Int32
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return &fakeServer{ln: ln}
}

func (fs *fakeServer) addr() string {
	return fs.ln.Addr().String()
}

func (fs *fakeServer) port() int {
	return fs.ln.Addr().(*net.TCPAddr).Port
}

func (fs *fakeServer) close() {
	fs.ln.Close()
}

// acceptOne accepts one connection, runs handler in a goroutine.
func (fs *fakeServer) acceptOne(t *testing.T, handler func(net.Conn)) {
	t.Helper()
	go func() {
		conn, err := fs.ln.Accept()
		if err != nil {
			return
		}
		fs.acceptCount.Add(1)
		handler(conn)
	}()
}

// acceptN accepts up to n connections sequentially using handlerFn(i, conn).
func (fs *fakeServer) acceptN(t *testing.T, n int, handler func(i int, conn net.Conn)) {
	t.Helper()
	go func() {
		for i := 0; i < n; i++ {
			conn, err := fs.ln.Accept()
			if err != nil {
				return
			}
			fs.acceptCount.Add(1)
			handler(i, conn)
		}
	}()
}

// readMsg reads one Diameter message from conn.
func readMsg(t *testing.T, conn net.Conn) *argusdiameter.Message {
	t.Helper()
	headerBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		t.Fatalf("read header: %v", err)
	}
	msgLen, err := argusdiameter.ReadMessageLength(headerBuf)
	if err != nil {
		t.Fatalf("parse header: %v", err)
	}
	msgBuf := make([]byte, msgLen)
	copy(msgBuf[:4], headerBuf)
	if _, err := io.ReadFull(conn, msgBuf[4:]); err != nil {
		t.Fatalf("read body: %v", err)
	}
	msg, err := argusdiameter.DecodeMessage(msgBuf)
	if err != nil {
		t.Fatalf("decode message: %v", err)
	}
	return msg
}

// writeMsg encodes and writes a Diameter message to conn.
func writeMsg(t *testing.T, conn net.Conn, msg *argusdiameter.Message) {
	t.Helper()
	data, err := msg.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// sendCEA responds to a CER with a success CEA.
func sendCEA(t *testing.T, conn net.Conn, cer *argusdiameter.Message) {
	t.Helper()
	cea := argusdiameter.NewAnswer(cer)
	cea.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ResultCodeSuccess))
	cea.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, "argus.test"))
	cea.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, "argus.local"))
	cea.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeAuthApplicationID, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ApplicationIDGx))
	cea.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeAuthApplicationID, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ApplicationIDGy))
	writeMsg(t, conn, cea)
}

func makeTestPeer(host string, port int) *Peer {
	cfg := PeerConfig{
		OperatorCode:        "test-op",
		Host:                host,
		Port:                port,
		OriginHost:          "sim-test.sim.argus.test",
		OriginRealm:         "sim.argus.test",
		DestinationRealm:    "argus.local",
		AppIDs:              []uint32{argusdiameter.ApplicationIDGx, argusdiameter.ApplicationIDGy},
		WatchdogInterval:    200 * time.Millisecond,
		ConnectTimeout:      2 * time.Second,
		RequestTimeout:      2 * time.Second,
		ReconnectBackoffMin: 50 * time.Millisecond,
		ReconnectBackoffMax: 200 * time.Millisecond,
	}
	return NewPeer(cfg, zerolog.Nop())
}

// waitState polls until the peer reaches the target state or timeout.
func waitState(t *testing.T, p *Peer, target PeerState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.State() == target {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("peer did not reach state %s within %s; current=%s", target, timeout, p.State())
}

// TestPeerSendRejectsWhenNotOpen verifies ErrPeerNotOpen is returned when
// the peer has not reached the Open state.
func TestPeerSendRejectsWhenNotOpen(t *testing.T) {
	// No server needed — peer never connects.
	p := makeTestPeer("127.0.0.1", 19999) // non-listening port

	msg := argusdiameter.NewRequest(argusdiameter.CommandCCR, argusdiameter.ApplicationIDGx, 1, 1)
	_, err := p.Send(context.Background(), msg)
	if err != ErrPeerNotOpen {
		t.Fatalf("expected ErrPeerNotOpen, got %v", err)
	}
}

// TestPeerCERCEAExchange verifies the CER/CEA handshake transitions peer to Open.
func TestPeerCERCEAExchange(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	fs.acceptOne(t, func(conn net.Conn) {
		defer conn.Close()
		cer := readMsg(t, conn)
		if cer.CommandCode != argusdiameter.CommandCER {
			t.Errorf("expected CER (257), got %d", cer.CommandCode)
		}
		if !cer.IsRequest() {
			t.Error("CER must have request flag set")
		}
		// Verify origin-host AVP is present.
		if cer.GetOriginHost() == "" {
			t.Error("CER missing Origin-Host")
		}
		sendCEA(t, conn, cer)
		// Keep the connection alive while test verifies state.
		time.Sleep(500 * time.Millisecond)
	})

	p := makeTestPeer("127.0.0.1", fs.port())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.Run(ctx)

	waitState(t, p, PeerStateOpen, 2*time.Second)
}

// TestPeerDWRDWAExchange verifies the watchdog DWR/DWA round-trip keeps peer Open.
func TestPeerDWRDWAExchange(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	dwrReceived := make(chan struct{}, 1)

	fs.acceptOne(t, func(conn net.Conn) {
		defer conn.Close()

		// CER/CEA
		cer := readMsg(t, conn)
		sendCEA(t, conn, cer)

		// Read and respond to DWR.
		dwr := readMsg(t, conn)
		if dwr.CommandCode != argusdiameter.CommandDWR {
			t.Errorf("expected DWR (280), got %d", dwr.CommandCode)
		}
		close(dwrReceived)

		dwa := argusdiameter.NewAnswer(dwr)
		dwa.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ResultCodeSuccess))
		dwa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, "argus.test"))
		dwa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, "argus.local"))
		writeMsg(t, conn, dwa)

		// Stay alive.
		time.Sleep(500 * time.Millisecond)
	})

	p := makeTestPeer("127.0.0.1", fs.port())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.Run(ctx)

	waitState(t, p, PeerStateOpen, 2*time.Second)

	select {
	case <-dwrReceived:
	case <-time.After(time.Second):
		t.Fatal("DWR not received within 1s")
	}

	// Peer must remain Open after DWA.
	time.Sleep(50 * time.Millisecond)
	if p.State() != PeerStateOpen {
		t.Errorf("peer dropped from Open after DWA; state=%s", p.State())
	}
}

// TestPeerReconnectAfterEOF verifies the peer reconnects when the server
// drops the connection.
func TestPeerReconnectAfterEOF(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	secondOpen := make(chan struct{})

	// Accept two connections in sequence: first drops immediately, second stays alive.
	fs.acceptN(t, 2, func(i int, conn net.Conn) {
		defer conn.Close()
		cer := readMsg(t, conn)
		sendCEA(t, conn, cer)
		if i == 0 {
			// Drop immediately → EOF on peer.
			return
		}
		// Second: stay alive and signal.
		close(secondOpen)
		time.Sleep(500 * time.Millisecond)
	})

	p := makeTestPeer("127.0.0.1", fs.port())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.Run(ctx)

	// Wait for second connection to be accepted.
	select {
	case <-secondOpen:
	case <-time.After(3 * time.Second):
		t.Fatal("peer did not reconnect within 3s")
	}

	waitState(t, p, PeerStateOpen, 2*time.Second)

	if fs.acceptCount.Load() < 2 {
		t.Errorf("expected at least 2 accepts, got %d", fs.acceptCount.Load())
	}
}

// TestPeerGracefulClose verifies DPR/DPA exchange on Close().
func TestPeerGracefulClose(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	dprReceived := make(chan struct{})

	fs.acceptOne(t, func(conn net.Conn) {
		defer conn.Close()

		// CER/CEA
		cer := readMsg(t, conn)
		sendCEA(t, conn, cer)

		// Read DPR.
		dpr := readMsg(t, conn)
		if dpr.CommandCode != argusdiameter.CommandDPR {
			t.Errorf("expected DPR (282), got %d", dpr.CommandCode)
		}
		close(dprReceived)

		// Send DPA.
		dpa := argusdiameter.NewAnswer(dpr)
		dpa.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ResultCodeSuccess))
		dpa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, "argus.test"))
		dpa.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, "argus.local"))
		writeMsg(t, conn, dpa)
	})

	p := makeTestPeer("127.0.0.1", fs.port())
	ctx, cancel := context.WithCancel(context.Background())

	go p.Run(ctx)

	waitState(t, p, PeerStateOpen, 2*time.Second)

	p.Close()
	cancel()

	select {
	case <-dprReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("DPR not received within 2s")
	}

	if p.State() != PeerStateClosed {
		t.Errorf("expected Closed after Close(), got %s", p.State())
	}
}

// TestPeerSendCorrelatesHopByHop verifies two consecutive requests have
// different Hop-by-Hop IDs and replies are correctly correlated.
func TestPeerSendCorrelatesHopByHop(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	// Track observed HBH IDs.
	type hbhRecord struct{ req, rep uint32 }
	records := make(chan hbhRecord, 2)

	fs.acceptOne(t, func(conn net.Conn) {
		defer conn.Close()

		cer := readMsg(t, conn)
		sendCEA(t, conn, cer)

		for i := 0; i < 2; i++ {
			req := readMsg(t, conn)
			hbh := req.HopByHopID
			// Echo back an answer with the same HBH.
			ans := argusdiameter.NewAnswer(req)
			ans.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ResultCodeSuccess))
			writeMsg(t, conn, ans)
			records <- hbhRecord{req: hbh, rep: hbh}
		}

		time.Sleep(200 * time.Millisecond)
	})

	p := makeTestPeer("127.0.0.1", fs.port())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.Run(ctx)
	waitState(t, p, PeerStateOpen, 2*time.Second)

	send := func() (uint32, error) {
		msg := argusdiameter.NewRequest(argusdiameter.CommandCCR, argusdiameter.ApplicationIDGx, 0, 0)
		reply, err := p.Send(ctx, msg)
		if err != nil {
			return 0, err
		}
		return reply.HopByHopID, nil
	}

	hbh1, err := send()
	if err != nil {
		t.Fatalf("first Send failed: %v", err)
	}
	hbh2, err := send()
	if err != nil {
		t.Fatalf("second Send failed: %v", err)
	}

	if hbh1 == hbh2 {
		t.Errorf("expected different HBH IDs, both = %d", hbh1)
	}

	<-records
	<-records
}

// TestPeerStateValues verifies numeric gauge values match plan spec.
func TestPeerStateValues(t *testing.T) {
	if int(PeerStateClosed) != 0 {
		t.Errorf("PeerStateClosed should be 0, got %d", PeerStateClosed)
	}
	if int(PeerStateConnecting) != 1 {
		t.Errorf("PeerStateConnecting should be 1, got %d", PeerStateConnecting)
	}
	if int(PeerStateWaitCEA) != 2 {
		t.Errorf("PeerStateWaitCEA should be 2, got %d", PeerStateWaitCEA)
	}
	if int(PeerStateOpen) != 3 {
		t.Errorf("PeerStateOpen should be 3, got %d", PeerStateOpen)
	}
}

// TestPeerCEABadResultCode verifies that a CEA with a non-2001 result code
// causes the peer to fall back to Closed and retry.
func TestPeerCEABadResultCode(t *testing.T) {
	fs := newFakeServer(t)
	defer fs.close()

	secondOpen := make(chan struct{})

	// Accept two connections in sequence: first sends bad CEA, second good.
	fs.acceptN(t, 2, func(i int, conn net.Conn) {
		defer conn.Close()
		cer := readMsg(t, conn)
		if i == 0 {
			cea := argusdiameter.NewAnswer(cer)
			cea.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeResultCode, argusdiameter.AVPFlagMandatory, 0, 5012))
			writeMsg(t, conn, cea)
			return
		}
		sendCEA(t, conn, cer)
		close(secondOpen)
		time.Sleep(500 * time.Millisecond)
	})

	p := makeTestPeer("127.0.0.1", fs.port())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.Run(ctx)

	select {
	case <-secondOpen:
	case <-time.After(3 * time.Second):
		t.Fatal("peer did not retry after bad CEA within 3s")
	}

	waitState(t, p, PeerStateOpen, 2*time.Second)

	if fs.acceptCount.Load() < 2 {
		t.Errorf("expected ≥2 accepts after bad CEA, got %d", fs.acceptCount.Load())
	}
}
