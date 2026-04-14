package radius

import (
	"testing"

	"github.com/btopcu/argus/internal/simulator/discovery"
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
