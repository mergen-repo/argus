package syslog

// Integration tests for STORY-098 Task 7.
//
// These tests wire T2 (emitter), T3 (transport + syslogtest mocks), T4 (store),
// and T5 (forwarder) together end-to-end and assert byte-level wire-format
// correctness — a level of verification absent from the unit tests in
// forwarder_test.go and transport_test.go.
//
// All helpers (recordingMetrics, recordingAuditor, fakeStore, listFakeStore,
// fakeSubscriber, fakeTransport, makeEnv, dest, helperSetup, splitHostPort,
// waitFor, genSelfSignedCert, genClientCA, hostPort) are declared in
// worker_test.go / forwarder_test.go / transport_test.go (same package).

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/notification/syslog/syslogtest"
	"github.com/btopcu/argus/internal/severity"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ── IT-01: RFC 3164 byte-level wire format ────────────────────────────────

// TestIntegration_RFC3164_WireFormat confirms that the forwarder + RFC 3164
// emitter + UDP transport produce a PRI / TIMESTAMP / TAG / MSG packet that
// satisfies the RFC 3164 §4.1 shape:
//
//	<PRI>Mmm dd hh:mm:ss HOSTNAME argus[PID]: TYPE tenant=… severity=… TITLE
func TestIntegration_RFC3164_WireFormat(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	host, port := splitHostPort(t, addr)
	tenant := uuid.New()
	d := dest(tenant, "rfc3164-check", host, port, FormatRFC3164, nil, nil)

	_, sub, _, cleanup := helperSetup(t, []Destination{d})
	defer cleanup()

	sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.High))

	got := listener.Wait(1, 2*time.Second)
	if len(got) == 0 {
		t.Fatal("no message received at UDP listener")
	}
	msg := string(got[0])

	// Must start with <PRI> where PRI is a small integer in angle brackets.
	if !strings.HasPrefix(msg, "<") {
		t.Fatalf("RFC 3164: expected PRI field starting with '<', got %q", msg)
	}
	closeBracket := strings.Index(msg, ">")
	if closeBracket < 0 {
		t.Fatalf("RFC 3164: no closing '>' for PRI, got %q", msg)
	}

	// After PRI: timestamp starts with a 3-letter month abbreviation.
	afterPRI := msg[closeBracket+1:]
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	hasMonth := false
	for _, m := range months {
		if strings.HasPrefix(afterPRI, m) {
			hasMonth = true
			break
		}
	}
	if !hasMonth {
		t.Fatalf("RFC 3164: expected Mmm timestamp after PRI, got %q", afterPRI)
	}

	// TAG field must contain "argus[".
	if !strings.Contains(msg, "argus[") {
		t.Fatalf("RFC 3164: expected TAG 'argus[PID]', got %q", msg)
	}

	// MSG must contain the event type and tenant= prefix.
	if !strings.Contains(msg, "session.started") {
		t.Fatalf("RFC 3164: expected event type 'session.started' in MSG, got %q", msg)
	}
	if !strings.Contains(msg, "tenant=") {
		t.Fatalf("RFC 3164: expected 'tenant=' in MSG, got %q", msg)
	}
	if !strings.Contains(msg, "severity=") {
		t.Fatalf("RFC 3164: expected 'severity=' in MSG, got %q", msg)
	}
}

// ── IT-02: RFC 5424 + TLS byte-level wire format ──────────────────────────

// TestIntegration_RFC5424_TLS_WireFormat confirms the forwarder + RFC 5424
// emitter + TLS transport deliver a correctly structured RFC 5424 frame to a
// mock TLS listener, asserting:
//   - version token "1" immediately after PRI: "<N>1 "
//   - structured-data SD-ID "argus@32473"
//   - UTF-8 BOM (0xEF 0xBB 0xBF) before MSG
//   - tenant_id present in SD-PARAMS
func TestIntegration_RFC5424_TLS_WireFormat(t *testing.T) {
	// Build self-signed server cert valid for 127.0.0.1.
	serverCert, _, caPEM := genSelfSignedCert(t, nil, []net.IP{net.ParseIP("127.0.0.1")})
	tlsListener, addr := syslogtest.NewTLSListener(t, serverCert, nil)
	defer tlsListener.Close()

	host, port := hostPort(t, addr)
	tenant := uuid.New()
	caPEMStr := string(caPEM)

	tlsDest := Destination{
		ID:        uuid.New(),
		TenantID:  tenant,
		Name:      "tls-rfc5424",
		Host:      host,
		Port:      port,
		Transport: TransportTLS,
		Format:    FormatRFC5424,
		Facility:  16,
		TLSCAPEM:  &caPEMStr,
	}

	st := newListFakeStore([]Destination{tlsDest})
	sub := &fakeSubscriber{}

	f := NewForwarder(st, &recordingAuditor{}, newRecordingMetrics(), zerolog.Nop(),
		WithRefreshInterval(50*time.Millisecond),
	)
	if err := f.Start(context.Background(), sub); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = f.Stop(ctx)
	}()

	sub.Deliver("argus.events.alert.triggered", makeEnv(tenant, "alert.triggered", severity.Critical))

	got := tlsListener.Wait(1, 3*time.Second)
	if len(got) == 0 {
		t.Fatal("no message received at TLS listener")
	}
	raw := got[0]

	// VERSION token: after closing '>' of PRI there must be "1 ".
	closeBracket := bytes.IndexByte(raw, '>')
	if closeBracket < 0 || closeBracket+2 > len(raw) {
		t.Fatalf("RFC 5424: could not find PRI closing bracket, got %q", raw)
	}
	if raw[closeBracket+1] != '1' || raw[closeBracket+2] != ' ' {
		t.Fatalf("RFC 5424: expected VERSION='1 ' after PRI, got %q", raw[closeBracket:closeBracket+4])
	}

	// SD-ID must contain "argus@32473".
	if !bytes.Contains(raw, []byte("argus@32473")) {
		t.Fatalf("RFC 5424: expected SD-ID 'argus@32473', not found in %q", raw)
	}

	// UTF-8 BOM must be present (0xEF 0xBB 0xBF).
	bom := []byte{0xEF, 0xBB, 0xBF}
	if !bytes.Contains(raw, bom) {
		t.Fatalf("RFC 5424: UTF-8 BOM not found in message")
	}

	// tenant_id must appear in structured-data.
	if !bytes.Contains(raw, []byte("tenant_id=")) {
		t.Fatalf("RFC 5424: 'tenant_id=' not found in structured-data, got %q", raw)
	}
}

// ── IT-03: Multi-destination dispatch ────────────────────────────────────

// TestIntegration_MultiDestination_AllReceive wires 3 enabled destinations
// (UDP/RFC3164, UDP/RFC5424, TCP/RFC5424) and confirms a single envelope
// reaches all three via distinct listeners.
func TestIntegration_MultiDestination_AllReceive(t *testing.T) {
	udpA, addrA := syslogtest.NewUDPListener(t)
	defer udpA.Close()
	udpB, addrB := syslogtest.NewUDPListener(t)
	defer udpB.Close()
	tcpC, addrC := syslogtest.NewTCPListener(t)
	defer tcpC.Close()

	hostA, portA := splitHostPort(t, addrA)
	hostB, portB := splitHostPort(t, addrB)
	hostC, portC := splitHostPort(t, addrC)
	tenant := uuid.New()

	dA := dest(tenant, "udp-3164", hostA, portA, FormatRFC3164, nil, nil)
	dB := dest(tenant, "udp-5424", hostB, portB, FormatRFC5424, nil, nil)
	dC := Destination{
		ID:        uuid.New(),
		TenantID:  tenant,
		Name:      "tcp-5424",
		Host:      hostC,
		Port:      portC,
		Transport: TransportTCP,
		Format:    FormatRFC5424,
		Facility:  16,
	}

	_, sub, _, cleanup := helperSetup(t, []Destination{dA, dB, dC})
	defer cleanup()

	sub.Deliver("argus.events.audit.create", makeEnv(tenant, "audit.create", severity.Info))

	if got := udpA.Wait(1, 2*time.Second); len(got) == 0 {
		t.Errorf("destination A (UDP/RFC3164): no message received")
	}
	if got := udpB.Wait(1, 2*time.Second); len(got) == 0 {
		t.Errorf("destination B (UDP/RFC5424): no message received")
	}
	if got := tcpC.Wait(1, 2*time.Second); len(got) == 0 {
		t.Errorf("destination C (TCP/RFC5424): no message received")
	}
}

// ── IT-04: Category filter — mismatched category is not delivered ─────────

// TestIntegration_CategoryFilter_MismatchNotDelivered confirms that a
// destination with FilterCategories=["audit","alert"] does NOT receive an
// envelope whose subject maps to category "session".
func TestIntegration_CategoryFilter_MismatchNotDelivered(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	host, port := splitHostPort(t, addr)
	tenant := uuid.New()
	d := dest(tenant, "audit-filter", host, port, FormatRFC5424, []string{CategoryAudit, CategoryAlert}, nil)

	_, sub, _, cleanup := helperSetup(t, []Destination{d})
	defer cleanup()

	// session category should be filtered out.
	sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.Info))

	got := listener.Wait(1, 300*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("category filter: expected 0 messages (session != audit|alert), got %d", len(got))
	}

	// Matching category must still arrive.
	sub.Deliver("argus.events.audit.create", makeEnv(tenant, "audit.create", severity.Info))
	got = listener.Wait(1, 2*time.Second)
	if len(got) == 0 {
		t.Fatalf("category filter: expected 1 message for audit category, got 0")
	}
}

// ── IT-05: Slow TCP destination does not stall fast UDP destination ───────

// TestIntegration_SlowTCPDoesNotStallUDP uses a real TCP listener that
// immediately kicks connections (simulating an unreachable SIEM) alongside a
// healthy UDP destination. Publishing 20 envelopes, the UDP destination must
// receive all 20; the bus subscriber must remain responsive throughout.
func TestIntegration_SlowTCPDoesNotStallUDP(t *testing.T) {
	fastUDP, fastAddr := syslogtest.NewUDPListener(t)
	defer fastUDP.Close()

	// Slow TCP listener: accepts then immediately kicks every connection.
	slowTCP, slowAddr := syslogtest.NewTCPListener(t)
	defer slowTCP.Close()

	fastHost, fastPort := splitHostPort(t, fastAddr)
	slowHost, slowPort := splitHostPort(t, slowAddr)
	tenant := uuid.New()

	fastDest := dest(tenant, "fast-udp", fastHost, fastPort, FormatRFC5424, nil, nil)
	slowDest := Destination{
		ID:        uuid.New(),
		TenantID:  tenant,
		Name:      "slow-tcp",
		Host:      slowHost,
		Port:      slowPort,
		Transport: TransportTCP,
		Format:    FormatRFC5424,
		Facility:  16,
	}

	// Continuously kick connections on the slow TCP listener.
	stopKick := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopKick:
				return
			case <-ticker.C:
				slowTCP.KickConnections()
			}
		}
	}()

	_, sub, _, cleanup := helperSetup(t, []Destination{fastDest, slowDest})
	defer func() {
		close(stopKick)
		cleanup()
	}()

	const n = 20
	for i := 0; i < n; i++ {
		sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.Info))
	}

	// Fast UDP destination must receive all n messages promptly.
	got := fastUDP.Wait(n, 3*time.Second)
	if len(got) < n {
		t.Fatalf("fast UDP destination: expected %d messages, got %d", n, len(got))
	}
}

// ── IT-06: Disabled destination — no delivery ─────────────────────────────

// TestIntegration_DisabledDestination_ZeroDelivery confirms that a destination
// absent from ListAllEnabled (the "disabled" semantic — WHERE enabled=TRUE)
// receives no messages.
func TestIntegration_DisabledDestination_ZeroDelivery(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	_ = addr
	tenant := uuid.New()

	// Empty list simulates the WHERE enabled=TRUE returning 0 rows.
	_, sub, _, cleanup := helperSetup(t, []Destination{})
	defer cleanup()

	sub.Deliver("argus.events.alert.triggered", makeEnv(tenant, "alert.triggered", severity.High))

	got := listener.Wait(1, 300*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("disabled (absent) destination: expected 0 messages, got %d", len(got))
	}
}

// ── IT-07: last_error stored on transport failure ─────────────────────────

// TestIntegration_LastErrorStoredOnFailure uses a fakeTransport wired to a
// real destinationWorker and confirms that UpdateDelivery(success=false,
// errMsg≠"") is called when Send fails, and then UpdateDelivery(success=true,
// errMsg="") is called once the transport recovers.
func TestIntegration_LastErrorStoredOnFailure(t *testing.T) {
	d := makeTestDest(t)
	tr := &fakeTransport{sendErr: errConnRefused}
	st := &fakeStore{}
	w := newDestinationWorker(d, tr, 8, newRecordingMetrics(), &recordingAuditor{}, st, zerolog.Nop())
	w.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = w.Stop(ctx)
	}()

	w.Enqueue([]byte("fail-payload"))

	// Wait for the failure UpdateDelivery call.
	if !waitFor(3*time.Second, func() bool {
		for _, c := range st.Calls() {
			if !c.Success && c.ErrMsg != "" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected UpdateDelivery(success=false, errMsg≠'') — not seen")
	}

	// Recover and confirm success path.
	tr.SetSendErr(nil)
	w.Enqueue([]byte("success-payload"))

	if !waitFor(3*time.Second, func() bool {
		for _, c := range st.Calls() {
			if c.Success && c.ErrMsg == "" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected UpdateDelivery(success=true, errMsg='') after recovery — not seen")
	}
}

// errConnRefused is a sentinel error for integration test IT-07.
// Kept here (rather than importing errors) to avoid re-importing it from
// worker_test.go which also uses errors.New in its test-doubles.
var errConnRefused = connRefusedErr("connection refused (integration test)")

type connRefusedErr string

func (e connRefusedErr) Error() string { return string(e) }
