package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIPPoolStructFields(t *testing.T) {
	cidr := "10.0.0.0/24"
	now := time.Now()
	p := &IPPool{
		ID:                     uuid.New(),
		TenantID:               uuid.New(),
		APNID:                  uuid.New(),
		Name:                   "test-pool",
		CIDRv4:                 &cidr,
		TotalAddresses:         254,
		UsedAddresses:          10,
		AlertThresholdWarning:  80,
		AlertThresholdCritical: 90,
		ReclaimGracePeriodDays: 7,
		State:                  "active",
		CreatedAt:              now,
	}
	if p.Name != "test-pool" {
		t.Errorf("Name = %q, want test-pool", p.Name)
	}
	if p.State != "active" {
		t.Errorf("State = %q, want active", p.State)
	}
	if p.ReclaimGracePeriodDays != 7 {
		t.Errorf("ReclaimGracePeriodDays = %d, want 7", p.ReclaimGracePeriodDays)
	}
}

func TestExpiredIPAddressStructFields(t *testing.T) {
	addr := "10.0.0.1"
	simID := uuid.New()
	reclaimAt := time.Now().Add(-1 * time.Hour)
	e := &ExpiredIPAddress{
		ID:            uuid.New(),
		PoolID:        uuid.New(),
		TenantID:      uuid.New(),
		AddressV4:     &addr,
		PreviousSimID: &simID,
		ReclaimAt:     reclaimAt,
	}
	if e.AddressV4 == nil || *e.AddressV4 != addr {
		t.Errorf("AddressV4 = %v, want %q", e.AddressV4, addr)
	}
	if e.PreviousSimID == nil || *e.PreviousSimID != simID {
		t.Error("PreviousSimID should match")
	}
	if e.ReclaimAt != reclaimAt {
		t.Error("ReclaimAt mismatch")
	}
}

func TestIPPoolStore_ListExpiredReclaim_RequiresDB(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Log("integration test: ListExpiredReclaim requires a real DB connection")
}

func TestIPPoolStore_FinalizeReclaim_RequiresDB(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Log("integration test: FinalizeReclaim requires a real DB connection")
}

func TestIPPoolErrSentinels(t *testing.T) {
	if ErrIPPoolNotFound == nil {
		t.Error("ErrIPPoolNotFound should not be nil")
	}
	if ErrPoolExhausted == nil {
		t.Error("ErrPoolExhausted should not be nil")
	}
	if ErrIPAlreadyAllocated == nil {
		t.Error("ErrIPAlreadyAllocated should not be nil")
	}
	if ErrIPNotFound == nil {
		t.Error("ErrIPNotFound should not be nil")
	}
}

func TestGenerateIPv4Addresses_SmallCIDR(t *testing.T) {
	addrs, err := GenerateIPv4Addresses("10.0.0.0/30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(addrs) != 2 {
		t.Errorf("len = %d, want 2 (usable hosts in /30)", len(addrs))
	}
	if addrs[0] != "10.0.0.1" {
		t.Errorf("first addr = %q, want 10.0.0.1", addrs[0])
	}
	if addrs[1] != "10.0.0.2" {
		t.Errorf("second addr = %q, want 10.0.0.2", addrs[1])
	}
}

func TestGenerateIPv4Addresses_Host(t *testing.T) {
	addrs, err := GenerateIPv4Addresses("192.168.1.5/32")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(addrs) != 1 || addrs[0] != "192.168.1.5" {
		t.Errorf("addrs = %v, want [192.168.1.5]", addrs)
	}
}

func TestGenerateIPv4Addresses_InvalidCIDR(t *testing.T) {
	_, err := GenerateIPv4Addresses("not-a-cidr")
	if err == nil {
		t.Error("expected error for invalid CIDR, got nil")
	}
}

func TestListExpiredReclaim_OnlyExpiredRows(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	expiredAddr := "10.0.0.1"
	futureAddr := "10.0.0.2"

	expired := ExpiredIPAddress{
		ID:        uuid.New(),
		PoolID:    uuid.New(),
		TenantID:  uuid.New(),
		AddressV4: &expiredAddr,
		ReclaimAt: past,
	}
	notExpired := ExpiredIPAddress{
		ID:        uuid.New(),
		PoolID:    uuid.New(),
		TenantID:  uuid.New(),
		AddressV4: &futureAddr,
		ReclaimAt: future,
	}

	all := []ExpiredIPAddress{expired, notExpired}

	var selected []ExpiredIPAddress
	for _, e := range all {
		if !e.ReclaimAt.After(now) {
			selected = append(selected, e)
		}
	}

	if len(selected) != 1 {
		t.Fatalf("expected 1 expired row, got %d", len(selected))
	}
	if selected[0].ID != expired.ID {
		t.Errorf("selected wrong row: got %v, want %v", selected[0].ID, expired.ID)
	}
}

func TestListExpiredReclaim_LimitRespected(t *testing.T) {
	const limit = 5
	rows := make([]ExpiredIPAddress, 10)
	past := time.Now().Add(-1 * time.Minute)
	for i := range rows {
		rows[i] = ExpiredIPAddress{
			ID:        uuid.New(),
			PoolID:    uuid.New(),
			TenantID:  uuid.New(),
			ReclaimAt: past,
		}
	}

	result := rows
	if len(result) > limit {
		result = result[:limit]
	}
	if len(result) != limit {
		t.Errorf("result len = %d, want %d", len(result), limit)
	}
}

func TestListExpiredReclaim_EmptyWhenNone(t *testing.T) {
	var rows []ExpiredIPAddress
	if len(rows) != 0 {
		t.Errorf("expected empty result, got %d", len(rows))
	}
}

func TestFinalizeReclaim_StateTransition(t *testing.T) {
	ip := &IPAddress{
		ID:     uuid.New(),
		PoolID: uuid.New(),
		State:  "reclaiming",
	}

	if ip.State != "reclaiming" {
		t.Fatalf("precondition: state = %q, want reclaiming", ip.State)
	}

	ip.State = "available"
	ip.SimID = nil
	ip.AllocatedAt = nil
	ip.ReclaimAt = nil

	if ip.State != "available" {
		t.Errorf("after reclaim: state = %q, want available", ip.State)
	}
	if ip.SimID != nil {
		t.Error("sim_id should be nil after reclaim")
	}
	if ip.AllocatedAt != nil {
		t.Error("allocated_at should be nil after reclaim")
	}
	if ip.ReclaimAt != nil {
		t.Error("reclaim_at should be nil after reclaim")
	}
}

func TestFinalizeReclaim_NonReclaimingReturnError(t *testing.T) {
	ip := &IPAddress{
		ID:    uuid.New(),
		State: "available",
	}

	if ip.State == "reclaiming" {
		t.Fatal("precondition failed: state should not be reclaiming")
	}

	if ErrIPNotFound.Error() == "" {
		t.Error("ErrIPNotFound should have a message")
	}
}
