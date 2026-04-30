package store

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var backupCodePattern = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)

func testBackupCodePool(t *testing.T) *pgxpool.Pool {
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

func TestNewBackupCodeStore(t *testing.T) {
	s := NewBackupCodeStore(nil)
	if s == nil {
		t.Fatal("expected non-nil BackupCodeStore")
	}
}

func TestBackupCodeStore_Integration(t *testing.T) {
	pool := testBackupCodePool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	s := NewBackupCodeStore(pool)
	userID := uuid.New()

	cleanupUser(t, pool, userID)

	t.Run("GenerateAndStore returns 10 plaintext codes", func(t *testing.T) {
		codes, err := s.GenerateAndStore(ctx, userID, 10, 4)
		if err != nil {
			t.Fatalf("GenerateAndStore: %v", err)
		}
		if len(codes) != 10 {
			t.Fatalf("expected 10 codes, got %d", len(codes))
		}
		for _, c := range codes {
			if !backupCodePattern.MatchString(c) {
				t.Errorf("code %q does not match pattern", c)
			}
		}
	})

	t.Run("CountUnused returns 10 after generation", func(t *testing.T) {
		count, err := s.CountUnused(ctx, userID)
		if err != nil {
			t.Fatalf("CountUnused: %v", err)
		}
		if count != 10 {
			t.Errorf("expected 10 unused, got %d", count)
		}
	})

	var firstCode string

	t.Run("GenerateAndStore second time for valid code", func(t *testing.T) {
		codes, err := s.GenerateAndStore(ctx, userID, 10, 4)
		if err != nil {
			t.Fatalf("GenerateAndStore: %v", err)
		}
		firstCode = codes[0]
	})

	t.Run("ConsumeIfMatch with valid code returns (true, 9)", func(t *testing.T) {
		matched, remaining, err := s.ConsumeIfMatch(ctx, userID, firstCode)
		if err != nil {
			t.Fatalf("ConsumeIfMatch: %v", err)
		}
		if !matched {
			t.Fatal("expected matched=true")
		}
		if remaining != 9 {
			t.Errorf("expected 9 remaining, got %d", remaining)
		}
	})

	t.Run("ConsumeIfMatch with same code returns (false, 9)", func(t *testing.T) {
		matched, remaining, err := s.ConsumeIfMatch(ctx, userID, firstCode)
		if err != nil {
			t.Fatalf("ConsumeIfMatch: %v", err)
		}
		if matched {
			t.Fatal("expected matched=false for already-used code")
		}
		if remaining != 9 {
			t.Errorf("expected 9 remaining, got %d", remaining)
		}
	})

	t.Run("ConsumeIfMatch with wrong code returns (false, 9)", func(t *testing.T) {
		matched, remaining, err := s.ConsumeIfMatch(ctx, userID, "XXXX-YYYY")
		if err != nil {
			t.Fatalf("ConsumeIfMatch: %v", err)
		}
		if matched {
			t.Fatal("expected matched=false for wrong code")
		}
		if remaining != 9 {
			t.Errorf("expected 9 remaining, got %d", remaining)
		}
	})

	t.Run("GenerateAndStore again invalidates old codes, CountUnused=10", func(t *testing.T) {
		_, err := s.GenerateAndStore(ctx, userID, 10, 4)
		if err != nil {
			t.Fatalf("GenerateAndStore: %v", err)
		}
		count, err := s.CountUnused(ctx, userID)
		if err != nil {
			t.Fatalf("CountUnused: %v", err)
		}
		if count != 10 {
			t.Errorf("expected 10 after regeneration, got %d", count)
		}
	})

	t.Run("InvalidateAll marks all codes used", func(t *testing.T) {
		err := s.InvalidateAll(ctx, userID)
		if err != nil {
			t.Fatalf("InvalidateAll: %v", err)
		}
		count, err := s.CountUnused(ctx, userID)
		if err != nil {
			t.Fatalf("CountUnused: %v", err)
		}
		if count != 0 {
			t.Errorf("expected 0 after InvalidateAll, got %d", count)
		}
	})
}

func cleanupUser(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM user_backup_codes WHERE user_id = $1`, userID)
	})
}
