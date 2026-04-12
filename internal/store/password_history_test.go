package store

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPasswordHistoryPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("skip: cannot connect to postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Logf("skip: postgres ping failed: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestNewPasswordHistoryStore(t *testing.T) {
	s := NewPasswordHistoryStore(nil)
	if s == nil {
		t.Fatal("expected non-nil PasswordHistoryStore")
	}
}

func TestPasswordHistoryStore_Integration(t *testing.T) {
	pool := testPasswordHistoryPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	ctx := context.Background()
	s := NewPasswordHistoryStore(pool)

	userID := uuid.New()

	if err := s.Insert(ctx, userID, "hash1"); err != nil {
		t.Fatalf("Insert hash1: %v", err)
	}
	if err := s.Insert(ctx, userID, "hash2"); err != nil {
		t.Fatalf("Insert hash2: %v", err)
	}
	if err := s.Insert(ctx, userID, "hash3"); err != nil {
		t.Fatalf("Insert hash3: %v", err)
	}

	hashes, err := s.GetLastN(ctx, userID, 10)
	if err != nil {
		t.Fatalf("GetLastN: %v", err)
	}
	if len(hashes) != 3 {
		t.Fatalf("expected 3 hashes, got %d", len(hashes))
	}
	if hashes[0] != "hash3" {
		t.Errorf("expected newest first (hash3), got %s", hashes[0])
	}
	if hashes[2] != "hash1" {
		t.Errorf("expected oldest last (hash1), got %s", hashes[2])
	}

	if err := s.Trim(ctx, userID, 2); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	hashes, err = s.GetLastN(ctx, userID, 10)
	if err != nil {
		t.Fatalf("GetLastN after trim: %v", err)
	}
	if len(hashes) != 2 {
		t.Fatalf("expected 2 hashes after trim, got %d", len(hashes))
	}
	if hashes[0] != "hash3" {
		t.Errorf("expected hash3 first after trim, got %s", hashes[0])
	}
	if hashes[1] != "hash2" {
		t.Errorf("expected hash2 second after trim, got %s", hashes[1])
	}
}

func TestPasswordHistoryStore_UserIsolation(t *testing.T) {
	pool := testPasswordHistoryPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	ctx := context.Background()
	s := NewPasswordHistoryStore(pool)

	userA := uuid.New()
	userB := uuid.New()

	if err := s.Insert(ctx, userA, "hashA1"); err != nil {
		t.Fatalf("Insert userA: %v", err)
	}
	if err := s.Insert(ctx, userA, "hashA2"); err != nil {
		t.Fatalf("Insert userA: %v", err)
	}

	hashesB, err := s.GetLastN(ctx, userB, 10)
	if err != nil {
		t.Fatalf("GetLastN userB: %v", err)
	}
	if len(hashesB) != 0 {
		t.Errorf("expected 0 hashes for userB, got %d (isolation breach)", len(hashesB))
	}

	hashesA, err := s.GetLastN(ctx, userA, 10)
	if err != nil {
		t.Fatalf("GetLastN userA: %v", err)
	}
	if len(hashesA) != 2 {
		t.Fatalf("expected 2 hashes for userA, got %d", len(hashesA))
	}
}

func TestPasswordHistoryStore_GetLastN_LimitRespected(t *testing.T) {
	pool := testPasswordHistoryPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}

	ctx := context.Background()
	s := NewPasswordHistoryStore(pool)

	userID := uuid.New()

	for i := 0; i < 5; i++ {
		if err := s.Insert(ctx, userID, "hash"+string(rune('a'+i))); err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
	}

	hashes, err := s.GetLastN(ctx, userID, 3)
	if err != nil {
		t.Fatalf("GetLastN(3): %v", err)
	}
	if len(hashes) != 3 {
		t.Fatalf("expected 3 hashes (limit=3), got %d", len(hashes))
	}
}
