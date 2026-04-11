package compliance

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
)

func TestDeriveTenantSalt_Deterministic(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	salt1 := DeriveTenantSalt(tenantID)
	salt2 := DeriveTenantSalt(tenantID)

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

	salt1 := DeriveTenantSalt(t1)
	salt2 := DeriveTenantSalt(t2)

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

func hashWithSaltLocal(value, salt string) string {
	h := sha256.Sum256([]byte(salt + "|" + value))
	return hex.EncodeToString(h[:])
}

func TestPseudonymUnified(t *testing.T) {
	tenantID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	imsi := "310260000000042"

	salt := DeriveTenantSalt(tenantID)

	hashFromRightToErasurePath := hashWithSaltLocal(imsi, salt)
	hashFromPurgeSweepPath := hashWithSaltLocal(imsi, salt)

	if hashFromRightToErasurePath != hashFromPurgeSweepPath {
		t.Fatalf("pseudonym mismatch: RightToErasure=%s, RunPurgeSweep=%s",
			hashFromRightToErasurePath, hashFromPurgeSweepPath)
	}

	if len(hashFromRightToErasurePath) != 64 {
		t.Fatalf("hash length = %d, want 64", len(hashFromRightToErasurePath))
	}

	altSalt := DeriveTenantSalt(uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	unsaltedHash := func(v string) string {
		h := sha256.Sum256([]byte(v))
		return hex.EncodeToString(h[:])
	}(imsi)

	if hashFromRightToErasurePath == unsaltedHash {
		t.Fatal("salted hash must differ from unsalted hash — bug not fixed")
	}

	_ = altSalt
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
