package system

import (
	"errors"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/config"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type ReliabilityHandler struct {
	backupStore *store.BackupStore
	auditStore  *store.AuditStore
	cfg         *config.Config
	logger      zerolog.Logger
}

func NewReliabilityHandler(
	backupStore *store.BackupStore,
	auditStore *store.AuditStore,
	cfg *config.Config,
	logger zerolog.Logger,
) *ReliabilityHandler {
	return &ReliabilityHandler{
		backupStore: backupStore,
		auditStore:  auditStore,
		cfg:         cfg,
		logger:      logger.With().Str("component", "reliability_handler").Logger(),
	}
}

type backupRunResp struct {
	Status     string     `json:"status"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	SizeMB     float64    `json:"size_mb"`
	S3Key      string     `json:"s3_key"`
	SHA256     string     `json:"sha256"`
	Kind       string     `json:"kind"`
	StartedAt  time.Time  `json:"started_at"`
}

type verifyResp struct {
	Status       string    `json:"status"`
	VerifiedAt   time.Time `json:"verified_at"`
	TenantsCount int64     `json:"tenants_count"`
	SimsCount    int64     `json:"sims_count"`
}

type backupStatusResp struct {
	LastDaily   *backupRunResp  `json:"last_daily"`
	LastWeekly  *backupRunResp  `json:"last_weekly"`
	LastMonthly *backupRunResp  `json:"last_monthly"`
	LastVerify  *verifyResp     `json:"last_verify"`
	History     []backupRunResp `json:"history"`
}

func toBackupRunResp(r *store.BackupRun) *backupRunResp {
	if r == nil {
		return nil
	}
	return &backupRunResp{
		Status:     r.State,
		FinishedAt: r.FinishedAt,
		SizeMB:     float64(r.SizeBytes) / (1024 * 1024),
		S3Key:      r.S3Key,
		SHA256:     r.SHA256,
		Kind:       r.Kind,
		StartedAt:  r.StartedAt,
	}
}

func (h *ReliabilityHandler) BackupStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	daily, err := h.backupStore.Latest(ctx, "daily")
	if err != nil && !errors.Is(err, store.ErrBackupRunNotFound) {
		h.logger.Error().Err(err).Msg("failed to get daily backup")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to retrieve backup status")
		return
	}

	weekly, err := h.backupStore.Latest(ctx, "weekly")
	if err != nil && !errors.Is(err, store.ErrBackupRunNotFound) {
		h.logger.Error().Err(err).Msg("failed to get weekly backup")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to retrieve backup status")
		return
	}

	monthly, err := h.backupStore.Latest(ctx, "monthly")
	if err != nil && !errors.Is(err, store.ErrBackupRunNotFound) {
		h.logger.Error().Err(err).Msg("failed to get monthly backup")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to retrieve backup status")
		return
	}

	history, err := h.backupStore.ListRecent(ctx, "daily", 30)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list backup history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to retrieve backup history")
		return
	}

	verification, err := h.backupStore.LatestVerification(ctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to get latest verification")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to retrieve verification status")
		return
	}

	histResp := make([]backupRunResp, 0, len(history))
	for _, run := range history {
		r := run
		if v := toBackupRunResp(&r); v != nil {
			histResp = append(histResp, *v)
		}
	}

	resp := backupStatusResp{
		LastDaily:   toBackupRunResp(daily),
		LastWeekly:  toBackupRunResp(weekly),
		LastMonthly: toBackupRunResp(monthly),
		History:     histResp,
	}

	if verification != nil {
		vr := &verifyResp{
			Status:     verification.State,
			VerifiedAt: verification.VerifiedAt,
		}
		if verification.TenantsCount != nil {
			vr.TenantsCount = *verification.TenantsCount
		}
		if verification.SimsCount != nil {
			vr.SimsCount = *verification.SimsCount
		}
		resp.LastVerify = vr
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

type jwtRotationEntry struct {
	When          time.Time `json:"when"`
	Actor         string    `json:"actor"`
	CorrelationID string    `json:"correlation_id"`
}

type jwtRotationHistoryResp struct {
	CurrentFingerprint  string             `json:"current_fingerprint"`
	PreviousFingerprint string             `json:"previous_fingerprint"`
	History             []jwtRotationEntry `json:"history"`
}

func (h *ReliabilityHandler) JWTRotationHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	entries, _, err := h.auditStore.List(ctx, uuid.Nil, store.ListAuditParams{
		Action: "jwt_key_rotation_detected",
		Limit:  10,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list jwt rotation audit logs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to retrieve rotation history")
		return
	}

	history := make([]jwtRotationEntry, 0, len(entries))
	for _, e := range entries {
		actor := "system"
		if e.UserID != nil {
			actor = e.UserID.String()
		}

		corrID := ""
		if e.CorrelationID != nil {
			corrID = e.CorrelationID.String()
		}

		history = append(history, jwtRotationEntry{
			When:          e.CreatedAt,
			Actor:         actor,
			CorrelationID: corrID,
		})
	}

	prevFP := ""
	if h.cfg.JWTSecretPrevious != "" {
		prevFP = auth.KeyFingerprint(h.cfg.JWTSecretPrevious)
	}

	resp := jwtRotationHistoryResp{
		CurrentFingerprint:  auth.KeyFingerprint(h.cfg.JWTSecret),
		PreviousFingerprint: prevFP,
		History:             history,
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

