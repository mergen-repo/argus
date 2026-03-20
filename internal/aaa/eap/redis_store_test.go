package eap

import (
	"context"
	"testing"
	"time"
)

func TestRedisStateStore_WithMemoryStore_Comparison(t *testing.T) {
	ctx := context.Background()

	memStore := NewMemoryStateStore()

	session := &EAPSession{
		ID:         "redis-test-1",
		IMSI:       "286010123456789",
		State:      StateIdentity,
		Method:     MethodSIM,
		Identifier: 1,
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(30 * time.Second),
	}

	if err := memStore.Save(ctx, session); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := memStore.Get(ctx, "redis-test-1")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.IMSI != session.IMSI {
		t.Errorf("IMSI = %q, want %q", got.IMSI, session.IMSI)
	}
	if got.State != StateIdentity {
		t.Errorf("State = %q, want %q", got.State, StateIdentity)
	}
	if got.Method != MethodSIM {
		t.Errorf("Method = %d, want %d", got.Method, MethodSIM)
	}

	if err := memStore.Delete(ctx, "redis-test-1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	got, err = memStore.Get(ctx, "redis-test-1")
	if err != nil {
		t.Fatalf("Get after delete error: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestRedisStateStore_SaveWithSIMData(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStateStore()

	session := &EAPSession{
		ID:         "sim-data-test",
		IMSI:       "286010123456789",
		State:      StateChallenge,
		Method:     MethodSIM,
		Identifier: 2,
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(30 * time.Second),
		SIMData: &SIMChallengeData{
			MSK: []byte("test-msk-data-for-eap-sim"),
		},
	}

	session.SIMData.RAND[0] = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	session.SIMData.SRES[0] = [4]byte{1, 2, 3, 4}
	session.SIMData.Kc[0] = [8]byte{1, 2, 3, 4, 5, 6, 7, 8}

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := store.Get(ctx, "sim-data-test")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.SIMData == nil {
		t.Fatal("SIMData is nil")
	}
	if got.SIMData.RAND[0] != session.SIMData.RAND[0] {
		t.Errorf("RAND mismatch")
	}
	if got.SIMData.SRES[0] != session.SIMData.SRES[0] {
		t.Errorf("SRES mismatch")
	}
	if string(got.SIMData.MSK) != string(session.SIMData.MSK) {
		t.Errorf("MSK mismatch")
	}
}

func TestRedisStateStore_SaveWithAKAData(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStateStore()

	session := &EAPSession{
		ID:         "aka-data-test",
		IMSI:       "286010123456789",
		State:      StateChallenge,
		Method:     MethodAKA,
		Identifier: 3,
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(30 * time.Second),
		AKAData: &AKAChallengeData{
			XRES: []byte{1, 2, 3, 4, 5, 6, 7, 8},
			MSK:  []byte("test-msk-data-for-eap-aka"),
		},
	}

	session.AKAData.RAND = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	session.AKAData.AUTN = [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	session.AKAData.CK = [16]byte{1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4}
	session.AKAData.IK = [16]byte{5, 5, 5, 5, 6, 6, 6, 6, 7, 7, 7, 7, 8, 8, 8, 8}

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := store.Get(ctx, "aka-data-test")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.AKAData == nil {
		t.Fatal("AKAData is nil")
	}
	if got.AKAData.RAND != session.AKAData.RAND {
		t.Errorf("RAND mismatch")
	}
	if got.AKAData.AUTN != session.AKAData.AUTN {
		t.Errorf("AUTN mismatch")
	}
	if string(got.AKAData.MSK) != string(session.AKAData.MSK) {
		t.Errorf("MSK mismatch")
	}
}

func TestRedisStateStore_GetNonExistent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStateStore()

	got, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestRedisStateStore_SaveWithSIMStartData(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStateStore()

	session := &EAPSession{
		ID:         "sim-start-test",
		IMSI:       "286010123456789",
		State:      StateSIMStart,
		Method:     MethodSIM,
		Identifier: 1,
		CreatedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(30 * time.Second),
		SIMStartData: &SIMStartData{
			NonceMT:         [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SelectedVersion: 1,
		},
	}

	if err := store.Save(ctx, session); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	got, err := store.Get(ctx, "sim-start-test")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.SIMStartData == nil {
		t.Fatal("SIMStartData is nil")
	}
	if got.SIMStartData.NonceMT != session.SIMStartData.NonceMT {
		t.Errorf("NonceMT mismatch")
	}
	if got.SIMStartData.SelectedVersion != 1 {
		t.Errorf("SelectedVersion = %d, want 1", got.SIMStartData.SelectedVersion)
	}
}
