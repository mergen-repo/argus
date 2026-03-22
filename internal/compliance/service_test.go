package compliance

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeriveTenantSalt_Deterministic(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	salt1 := deriveTenantSalt(tenantID)
	salt2 := deriveTenantSalt(tenantID)

	if salt1 != salt2 {
		t.Fatalf("deriveTenantSalt not deterministic: %s != %s", salt1, salt2)
	}

	if len(salt1) != 32 {
		t.Fatalf("salt length = %d, want 32", len(salt1))
	}
}

func TestDeriveTenantSalt_UniquePerTenant(t *testing.T) {
	t1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	t2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	salt1 := deriveTenantSalt(t1)
	salt2 := deriveTenantSalt(t2)

	if salt1 == salt2 {
		t.Fatal("different tenants should have different salts")
	}
}

func TestUniqueTenantIDs(t *testing.T) {
	t1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	t2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	tests := []struct {
		name string
		sims []PurgableSIMInput
		want int
	}{
		{"empty", nil, 0},
		{"single", []PurgableSIMInput{{TenantID: t1}}, 1},
		{"duplicates", []PurgableSIMInput{{TenantID: t1}, {TenantID: t1}}, 1},
		{"multiple", []PurgableSIMInput{{TenantID: t1}, {TenantID: t2}}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sims []purgableSIM
			for _, s := range tt.sims {
				sims = append(sims, purgableSIM{TenantID: s.TenantID})
			}
			got := uniqueTenantIDsFromPurgable(sims)
			if len(got) != tt.want {
				t.Fatalf("uniqueTenantIDs() = %d items, want %d", len(got), tt.want)
			}
		})
	}
}

type PurgableSIMInput struct {
	TenantID uuid.UUID
}

type purgableSIM struct {
	TenantID uuid.UUID
}

func uniqueTenantIDsFromPurgable(sims []purgableSIM) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	var result []uuid.UUID
	for _, s := range sims {
		if !seen[s.TenantID] {
			seen[s.TenantID] = true
			result = append(result, s.TenantID)
		}
	}
	return result
}
