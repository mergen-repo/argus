package store

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const backupCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

type BackupCodeStore struct {
	db *pgxpool.Pool
}

func NewBackupCodeStore(pool *pgxpool.Pool) *BackupCodeStore {
	return &BackupCodeStore{db: pool}
}

func generateBackupCode() (string, error) {
	buf := make([]byte, 8)
	b := make([]byte, 1)
	for i := range buf {
		for {
			if _, err := rand.Read(b); err != nil {
				return "", err
			}
			if int(b[0]) < len(backupCodeAlphabet)*(256/len(backupCodeAlphabet)) {
				buf[i] = backupCodeAlphabet[int(b[0])%len(backupCodeAlphabet)]
				break
			}
		}
	}
	return string(buf[:4]) + "-" + string(buf[4:]), nil
}

func (s *BackupCodeStore) GenerateAndStore(ctx context.Context, userID uuid.UUID, count int, bcryptCost int) ([]string, error) {
	plaintexts := make([]string, count)
	hashes := make([]string, count)
	for i := 0; i < count; i++ {
		code, err := generateBackupCode()
		if err != nil {
			return nil, fmt.Errorf("store: generate backup code: %w", err)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(code), bcryptCost)
		if err != nil {
			return nil, fmt.Errorf("store: hash backup code: %w", err)
		}
		plaintexts[i] = code
		hashes[i] = string(hash)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM user_backup_codes WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: delete old backup codes: %w", err)
	}

	for _, h := range hashes {
		_, err = tx.Exec(ctx,
			`INSERT INTO user_backup_codes (user_id, code_hash) VALUES ($1, $2)`,
			userID, h,
		)
		if err != nil {
			return nil, fmt.Errorf("store: insert backup code: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit backup codes: %w", err)
	}

	return plaintexts, nil
}

func (s *BackupCodeStore) ConsumeIfMatch(ctx context.Context, userID uuid.UUID, rawCode string) (bool, int, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, code_hash FROM user_backup_codes WHERE user_id = $1 AND used_at IS NULL`,
		userID,
	)
	if err != nil {
		return false, 0, fmt.Errorf("store: query backup codes: %w", err)
	}
	defer rows.Close()

	type row struct {
		id       int64
		codeHash string
	}
	var candidates []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.codeHash); err != nil {
			return false, 0, fmt.Errorf("store: scan backup code: %w", err)
		}
		candidates = append(candidates, r)
	}
	if err := rows.Err(); err != nil {
		return false, 0, fmt.Errorf("store: iterate backup codes: %w", err)
	}

	for _, c := range candidates {
		if bcrypt.CompareHashAndPassword([]byte(c.codeHash), []byte(rawCode)) == nil {
			tag, err := s.db.Exec(ctx,
				`UPDATE user_backup_codes SET used_at = NOW() WHERE id = $1 AND used_at IS NULL`,
				c.id,
			)
			if err != nil {
				return false, 0, fmt.Errorf("store: consume backup code: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return false, 0, nil
			}
			remaining, err := s.CountUnused(ctx, userID)
			if err != nil {
				return true, 0, err
			}
			return true, remaining, nil
		}
	}

	return false, 0, nil
}

func (s *BackupCodeStore) CountUnused(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_backup_codes WHERE user_id = $1 AND used_at IS NULL`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count unused backup codes: %w", err)
	}
	return count, nil
}

func (s *BackupCodeStore) InvalidateAll(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE user_backup_codes SET used_at = NOW() WHERE user_id = $1 AND used_at IS NULL`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("store: invalidate backup codes: %w", err)
	}
	return nil
}
