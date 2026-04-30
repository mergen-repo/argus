package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/config"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func KeyFingerprint(secret string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(secret)))[:19]
}

func CheckAndAuditRotation(ctx context.Context, cfg *config.Config, auditor audit.Auditor, bootID string, logger zerolog.Logger) error {
	if cfg.JWTSecretPrevious == "" {
		return nil
	}

	corrID, err := uuid.Parse(bootID)
	if err != nil {
		corrID = uuid.New()
	}

	afterData, err := json.Marshal(map[string]string{
		"current_fingerprint":  KeyFingerprint(cfg.JWTSecret),
		"previous_fingerprint": KeyFingerprint(cfg.JWTSecretPrevious),
	})
	if err != nil {
		return fmt.Errorf("key_rotation: marshal after_data: %w", err)
	}

	tenantID := uuid.Nil
	_, err = auditor.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:      tenantID,
		Action:        "jwt_key_rotation_detected",
		EntityType:    "security",
		EntityID:      "jwt_signing_key",
		AfterData:     afterData,
		CorrelationID: &corrID,
	})
	if err != nil {
		logger.Warn().Err(err).Msg("key_rotation: audit entry failed")
		return err
	}

	logger.Info().
		Str("current_fingerprint", KeyFingerprint(cfg.JWTSecret)).
		Str("previous_fingerprint", KeyFingerprint(cfg.JWTSecretPrevious)).
		Msg("jwt key rotation detected and audited")

	return nil
}
