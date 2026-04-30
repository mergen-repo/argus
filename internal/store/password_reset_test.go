package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPasswordResetPool(t *testing.T) *pgxpool.Pool {
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

func cleanupPasswordResetUser(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM password_reset_tokens WHERE user_id = $1`, userID)
	})
}

// seedPRUser inserts a tenant + a user (FK target for password_reset_tokens.user_id)
// and returns the user UUID. Cleanup is registered automatically.
// Required because password_reset_tokens.user_id has a NOT NULL FK to users(id);
// without seeding a real user row, INSERTs in these tests fail with FK violations.
func seedPRUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tenantID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, contact_email) VALUES ($1, $2, $3)`,
		tenantID, "pr-store-"+tenantID.String()[:8], "pr-store-"+tenantID.String()[:8]+"@test.invalid",
	); err != nil {
		t.Fatalf("seedPRUser: insert tenant: %v", err)
	}
	userID := uuid.New()
	email := "pr-store-" + userID.String()[:8] + "@test.invalid"
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, password_hash, name, role, state)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID, tenantID, email, "$2a$04$xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "PR Store Test", "tenant_admin", "active",
	); err != nil {
		t.Fatalf("seedPRUser: insert user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id = $1`, userID)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
		pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})
	return userID
}

func hashToken(s string) [32]byte {
	return sha256.Sum256([]byte(s))
}

func TestPasswordReset_CreateAndFindByHash(t *testing.T) {
	pool := testPasswordResetPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	store := NewPasswordResetStore(pool)
	userID := seedPRUser(t, pool)

	hash := hashToken("happy-path-token")
	expiresAt := time.Now().Add(15 * time.Minute)

	err := store.Create(ctx, userID, hash, "user@example.com", expiresAt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tok, err := store.FindByHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if tok.UserID != userID {
		t.Errorf("UserID = %v, want %v", tok.UserID, userID)
	}
	if tok.TokenHash != hash {
		t.Errorf("TokenHash mismatch")
	}
	if tok.EmailRateKey != "user@example.com" {
		t.Errorf("EmailRateKey = %q, want %q", tok.EmailRateKey, "user@example.com")
	}
}

func TestPasswordReset_FindByHash_Expired_ReturnsErrNotFound(t *testing.T) {
	pool := testPasswordResetPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	store := NewPasswordResetStore(pool)
	userID := seedPRUser(t, pool)

	hash := hashToken("expired-token")
	expiresAt := time.Now().Add(-1 * time.Minute)

	err := store.Create(ctx, userID, hash, "user@example.com", expiresAt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = store.FindByHash(ctx, hash)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestPasswordReset_DeleteByHash_Idempotent(t *testing.T) {
	pool := testPasswordResetPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	store := NewPasswordResetStore(pool)
	userID := seedPRUser(t, pool)

	hash := hashToken("delete-idempotent-token")
	expiresAt := time.Now().Add(15 * time.Minute)

	if err := store.Create(ctx, userID, hash, "user@example.com", expiresAt); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.DeleteByHash(ctx, hash); err != nil {
		t.Fatalf("DeleteByHash (first): %v", err)
	}

	if err := store.DeleteByHash(ctx, hash); err != nil {
		t.Errorf("DeleteByHash (second, idempotent) returned error: %v", err)
	}
}

func TestPasswordReset_DeleteAllForUser(t *testing.T) {
	pool := testPasswordResetPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	store := NewPasswordResetStore(pool)
	userID := seedPRUser(t, pool)

	expiresAt := time.Now().Add(15 * time.Minute)
	hashes := [3][32]byte{
		hashToken("user-token-1"),
		hashToken("user-token-2"),
		hashToken("user-token-3"),
	}
	for _, h := range hashes {
		if err := store.Create(ctx, userID, h, "user@example.com", expiresAt); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	if err := store.DeleteAllForUser(ctx, userID); err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}

	for _, h := range hashes {
		_, err := store.FindByHash(ctx, h)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound after DeleteAllForUser, got %v", err)
		}
	}
}

func TestPasswordReset_CountRecentForEmail_WindowBoundary(t *testing.T) {
	pool := testPasswordResetPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	store := NewPasswordResetStore(pool)
	userID := seedPRUser(t, pool)

	emailKey := "rate-limit@example.com"
	expiresAt := time.Now().Add(15 * time.Minute)
	now := time.Now().UTC()

	tokens := []struct {
		raw       string
		createdAt time.Time
	}{
		{"recent-token", now},
		{"half-hour-token", now.Add(-30 * time.Minute)},
		{"two-hour-token", now.Add(-2 * time.Hour)},
	}

	for _, tok := range tokens {
		h := hashToken(tok.raw)
		_, err := pool.Exec(ctx,
			`INSERT INTO password_reset_tokens (user_id, token_hash, email_rate_key, expires_at, created_at)
			 VALUES ($1, $2, $3, $4, $5)`,
			userID, h[:], emailKey, expiresAt, tok.createdAt,
		)
		if err != nil {
			t.Fatalf("insert token %q: %v", tok.raw, err)
		}
	}

	count, err := store.CountRecentForEmail(ctx, emailKey, 1*time.Hour)
	if err != nil {
		t.Fatalf("CountRecentForEmail: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 tokens within 1h window, got %d", count)
	}
}

func TestPasswordReset_Create_DuplicateHash_ReturnsError(t *testing.T) {
	pool := testPasswordResetPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	store := NewPasswordResetStore(pool)
	userID := seedPRUser(t, pool)

	hash := hashToken("duplicate-hash-token")
	expiresAt := time.Now().Add(15 * time.Minute)

	if err := store.Create(ctx, userID, hash, "user@example.com", expiresAt); err != nil {
		t.Fatalf("Create (first): %v", err)
	}

	err := store.Create(ctx, userID, hash, "user@example.com", expiresAt)
	if err == nil {
		t.Fatal("expected error on duplicate token_hash, got nil")
	}
}

func TestPasswordReset_PurgeExpired_RemovesOnlyExpired(t *testing.T) {
	pool := testPasswordResetPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	store := NewPasswordResetStore(pool)
	userID := seedPRUser(t, pool)

	future := time.Now().Add(15 * time.Minute)
	past := time.Now().Add(-15 * time.Minute)

	active := [2][32]byte{hashToken("active-1"), hashToken("active-2")}
	expired := [2][32]byte{hashToken("expired-1"), hashToken("expired-2")}

	for _, h := range active {
		if err := store.Create(ctx, userID, h, "purge@example.com", future); err != nil {
			t.Fatalf("Create active: %v", err)
		}
	}
	for _, h := range expired {
		_, err := pool.Exec(ctx,
			`INSERT INTO password_reset_tokens (user_id, token_hash, email_rate_key, expires_at)
			 VALUES ($1, $2, $3, $4)`,
			userID, h[:], "purge@example.com", past,
		)
		if err != nil {
			t.Fatalf("Insert expired: %v", err)
		}
	}

	if err := store.PurgeExpired(ctx); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}

	for _, h := range active {
		tok, err := store.FindByHash(ctx, h)
		if err != nil {
			t.Errorf("active token should remain, got error: %v", err)
		}
		if tok == nil {
			t.Error("active token should remain, got nil")
		}
	}

	var remaining int
	pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = $1`,
		userID,
	).Scan(&remaining)
	if remaining != 2 {
		t.Errorf("expected 2 rows after PurgeExpired, got %d", remaining)
	}
}
