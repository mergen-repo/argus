package adapter

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/aaa/diameter"
)

// fakeDiameterPeer accepts TCP connections, reads the first Diameter
// request, and replies with a DWA (command code 280) carrying the
// configured Result-Code. Supports drop (timeout) and close-on-accept
// (read error) modes for failure-path coverage.
type fakeDiameterPeer struct {
	listener   net.Listener
	wg         sync.WaitGroup
	stop       chan struct{}
	resultCode uint32
	dropReply  bool
}

type fakeDiameterOpt func(*fakeDiameterPeer)

func withDiameterResultCode(code uint32) fakeDiameterOpt {
	return func(p *fakeDiameterPeer) { p.resultCode = code }
}
func withDiameterDrop() fakeDiameterOpt {
	return func(p *fakeDiameterPeer) { p.dropReply = true }
}

func newFakeDiameterPeer(t *testing.T, opts ...fakeDiameterOpt) *fakeDiameterPeer {
	t.Helper()
	lc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	p := &fakeDiameterPeer{
		listener:   lc,
		resultCode: diameter.ResultCodeSuccess,
		stop:       make(chan struct{}),
	}
	for _, o := range opts {
		o(p)
	}
	p.wg.Add(1)
	go p.loop()
	return p
}

func (p *fakeDiameterPeer) loop() {
	defer p.wg.Done()
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		_ = p.listener.(*net.TCPListener).SetDeadline(time.Now().Add(50 * time.Millisecond))
		conn, err := p.listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		go p.handle(conn)
	}
}

func (p *fakeDiameterPeer) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	// Read the DWR: 4-byte header preamble + remainder.
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	msgLen, err := diameter.ReadMessageLength(header)
	if err != nil {
		return
	}
	rest := make([]byte, msgLen-4)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return
	}
	full := append(header, rest...)
	req, err := diameter.DecodeMessage(full)
	if err != nil {
		return
	}
	if p.dropReply {
		return
	}

	// Build a DWA answer. Per RFC 6733 the DWA reuses the DWR command
	// code (280) with the Request flag cleared.
	dwa := diameter.NewAnswer(req)
	dwa.AddAVP(diameter.NewAVPUint32(diameter.AVPCodeResultCode, diameter.AVPFlagMandatory, 0, p.resultCode))
	dwa.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginHost, diameter.AVPFlagMandatory, 0, "peer.test"))
	dwa.AddAVP(diameter.NewAVPString(diameter.AVPCodeOriginRealm, diameter.AVPFlagMandatory, 0, "test.local"))
	dwaData, err := dwa.Encode()
	if err != nil {
		return
	}
	_, _ = conn.Write(dwaData)
}

func (p *fakeDiameterPeer) Close() {
	close(p.stop)
	p.listener.Close()
	p.wg.Wait()
}

func (p *fakeDiameterPeer) Addr() (string, int) {
	addr := p.listener.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func TestDiameterAdapter_HealthCheck_DWR_DWA(t *testing.T) {
	peer := newFakeDiameterPeer(t)
	defer peer.Close()

	host, port := peer.Addr()
	cfg, _ := json.Marshal(DiameterConfig{
		Host:        host,
		Port:        port,
		OriginHost:  "argus.local",
		OriginRealm: "argus.test",
		TimeoutMs:   1500,
	})
	a, err := NewDiameterAdapter(cfg)
	if err != nil {
		t.Fatalf("new diameter adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if !result.Success {
		t.Fatalf("healthy peer should succeed, got error: %s", result.Error)
	}
}

func TestDiameterAdapter_HealthCheck_TimeoutWhenPeerDrops(t *testing.T) {
	peer := newFakeDiameterPeer(t, withDiameterDrop())
	defer peer.Close()

	host, port := peer.Addr()
	cfg, _ := json.Marshal(DiameterConfig{
		Host:        host,
		Port:        port,
		OriginHost:  "argus.local",
		OriginRealm: "argus.test",
		TimeoutMs:   200,
	})
	a, err := NewDiameterAdapter(cfg)
	if err != nil {
		t.Fatalf("new diameter adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure when peer drops DWR")
	}
	if !strings.Contains(result.Error, "read dwa") {
		t.Errorf("expected 'read dwa' in error, got %q", result.Error)
	}
}

func TestDiameterAdapter_HealthCheck_NonSuccessResult(t *testing.T) {
	peer := newFakeDiameterPeer(t, withDiameterResultCode(5012)) // DIAMETER_UNABLE_TO_COMPLY
	defer peer.Close()

	host, port := peer.Addr()
	cfg, _ := json.Marshal(DiameterConfig{
		Host:        host,
		Port:        port,
		OriginHost:  "argus.local",
		OriginRealm: "argus.test",
		TimeoutMs:   1500,
	})
	a, err := NewDiameterAdapter(cfg)
	if err != nil {
		t.Fatalf("new diameter adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure on non-success DWA result code")
	}
	if !strings.Contains(result.Error, "5012") {
		t.Errorf("error should mention result code, got %q", result.Error)
	}
}

func TestDiameterAdapter_HealthCheck_DialFailsWhenPeerDown(t *testing.T) {
	// Listen on a random port then close so dial hits a dead socket.
	lc, _ := net.Listen("tcp", "127.0.0.1:0")
	host := lc.Addr().(*net.TCPAddr).IP.String()
	port := lc.Addr().(*net.TCPAddr).Port
	lc.Close()

	cfg, _ := json.Marshal(DiameterConfig{
		Host:        host,
		Port:        port,
		OriginHost:  "argus.local",
		OriginRealm: "argus.test",
		TimeoutMs:   200,
	})
	a, err := NewDiameterAdapter(cfg)
	if err != nil {
		t.Fatalf("new diameter adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure when peer port is closed")
	}
	if !strings.Contains(result.Error, "dial") {
		t.Errorf("expected 'dial' in error, got %q", result.Error)
	}
}
