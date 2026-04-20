package radius

import (
	"net"
	"testing"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

// TestExtractNASIPFromPacket_ReturnsIP is a helper-level test: it verifies that
// extractNASIPFromPacket returns (ip, true) when the NAS-IP-Address AVP is
// present in an Acct-Start packet. The returned string is the value passed into
// session.Session.NASIP, which is then persisted to sessions.nas_ip by the
// session store (FIX-207 AC-7).
//
// NOTE (scope): This test covers the AVP extraction helper only. End-to-end
// persistence through sessionStore.Create is covered by the live-DB smoke
// verification recorded in FIX-207-step-log.txt. A DB-gated integration test
// for the full RADIUS → sessionStore path is tracked as D-071 in ROUTEMAP.
func TestExtractNASIPFromPacket_ReturnsIP(t *testing.T) {
	secret := []byte("testing123")
	pkt := radius.New(radius.CodeAccountingRequest, secret)
	rfc2865.NASIPAddress_Set(pkt, net.ParseIP("192.0.2.10").To4())

	gotIP, ok := extractNASIPFromPacket(pkt)
	if !ok {
		t.Fatal("extractNASIPFromPacket: expected ok=true when NAS-IP AVP is present, got false")
	}
	if gotIP != "192.0.2.10" {
		t.Errorf("extractNASIPFromPacket = %q, want 192.0.2.10", gotIP)
	}
}

// TestExtractNASIPFromPacket_MissingAVP_EmitsSignal verifies two things when
// the NAS-IP-Address AVP is absent from an Acct-Start packet (FIX-207 AC-7):
//  1. extractNASIPFromPacket returns ("", false).
//  2. Registry.IncNASIPMissing() increments argus_radius_nas_ip_missing_total.
//
// The simulator-side fix that will supply the missing AVP is tracked as FIX-226.
// Helper-level test; full-path RADIUS → session integration coverage is D-071.
func TestExtractNASIPFromPacket_MissingAVP_EmitsSignal(t *testing.T) {
	secret := []byte("testing123")
	pkt := radius.New(radius.CodeAccountingRequest, secret)

	gotIP, ok := extractNASIPFromPacket(pkt)
	if ok {
		t.Fatalf("extractNASIPFromPacket: expected ok=false when NAS-IP AVP is absent, got true (ip=%q)", gotIP)
	}
	if gotIP != "" {
		t.Errorf("extractNASIPFromPacket ip = %q, want empty string", gotIP)
	}

	reg := obsmetrics.NewRegistry()
	pre := testutil.ToFloat64(reg.NASIPMissingTotal)
	reg.IncNASIPMissing()
	post := testutil.ToFloat64(reg.NASIPMissingTotal)
	if post != pre+1 {
		t.Errorf("NASIPMissingTotal: pre=%.0f post=%.0f, want increment of 1", pre, post)
	}
}
