package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	authpkg "github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/bcrypt"
)

// dummyBcryptHash is a pre-computed bcrypt hash (cost=12) used exclusively for
// constant-time enumeration defense on the "email not found" path.
// It is never used for real password verification.
var dummyBcryptHash = []byte("$2a$12$QaxzKHvfEpKUsGYYK9fJFOAKmW300m10uDmyskRQJsUL06dmMaJqW")

func (h *AuthHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Invalid request body")
		return
	}
	if req.Email == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "email is required")
		return
	}
	rateKey := strings.ToLower(strings.TrimSpace(req.Email))

	if h.prStore == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Password reset is not configured")
		return
	}

	_ = h.prStore.PurgeExpired(ctx)

	n, err := h.prStore.CountRecentForEmail(ctx, rateKey, time.Hour)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if n >= h.prRateLimit {
		h.createAuditEntry(r, "auth.password_reset_requested", rateKey, nil, map[string]any{"rate_limited": true})
		apierr.WriteError(w, http.StatusTooManyRequests, apierr.CodeRateLimited, "Too many requests. Please try again later.")
		return
	}

	if h.userStore == nil {
		if h.dummyBcryptHook != nil {
			h.dummyBcryptHook()
		}
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte("dummy-password"))
		h.createAuditEntry(r, "auth.password_reset_requested", rateKey, nil, map[string]any{"found": false})
		writeGenericSuccess(w)
		return
	}

	user, uerr := h.userStore.GetByEmail(ctx, rateKey)
	found := uerr == nil && user != nil

	if !found {
		if h.dummyBcryptHook != nil {
			h.dummyBcryptHook()
		}
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte("dummy-password"))
		h.createAuditEntry(r, "auth.password_reset_requested", rateKey, nil, map[string]any{"found": false})
		writeGenericSuccess(w)
		return
	}

	var tokBytes [32]byte
	if _, err := rand.Read(tokBytes[:]); err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	tokB64 := base64.RawURLEncoding.EncodeToString(tokBytes[:])
	tokHash := sha256.Sum256(tokBytes[:])
	expires := time.Now().Add(h.prTokenTTL)

	if err := h.prStore.Create(ctx, user.ID, tokHash, rateKey, expires); err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.emailSender != nil {
		resetURL := strings.TrimRight(h.prPublicBaseURL, "/") + "/auth/reset?token=" + tokB64
		expiryHuman := fmt.Sprintf("%d dakika", int(h.prTokenTTL.Minutes()))
		subject, textBody, htmlBody, rerr := notification.RenderPasswordResetEmail(notification.PasswordResetEmailData{
			UserName:    user.Name,
			ResetURL:    resetURL,
			ExpiryHuman: expiryHuman,
		})
		if rerr != nil {
			log.Error().Err(rerr).Msg("password reset email render failed")
		} else if serr := h.emailSender.SendTo(ctx, user.Email, subject, textBody, htmlBody); serr != nil {
			log.Error().Err(serr).Str("email", user.Email).Msg("password reset email send failed")
		}
	}

	h.createAuditEntry(r, "auth.password_reset_requested", rateKey, nil, map[string]any{"found": true})
	writeGenericSuccess(w)
}

func (h *AuthHandler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid request body")
		return
	}
	if req.Token == "" || req.Password == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "token and password are required")
		return
	}

	tokBytes, err := base64.RawURLEncoding.DecodeString(req.Token)
	if err != nil || len(tokBytes) != 32 {
		apierr.WriteError(w, http.StatusBadRequest, "PASSWORD_RESET_INVALID_TOKEN", "Reset link is invalid or has expired")
		return
	}
	tokHash := sha256.Sum256(tokBytes)

	if h.prStore == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Password reset is not configured")
		return
	}

	prt, err := h.prStore.FindByHash(ctx, tokHash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apierr.WriteError(w, http.StatusBadRequest, "PASSWORD_RESET_INVALID_TOKEN", "Reset link is invalid or has expired")
			return
		}
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if err := h.svc.ResetPasswordForUser(ctx, prt.UserID, req.Password); err != nil {
		switch {
		case errors.Is(err, authpkg.ErrPasswordTooShort):
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodePasswordTooShort,
				"Password does not meet minimum length requirement")
		case errors.Is(err, authpkg.ErrPasswordMissingClass):
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodePasswordMissingClass,
				"Password must contain uppercase, lowercase, digit, and symbol characters")
		case errors.Is(err, authpkg.ErrPasswordRepeatingChars):
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodePasswordRepeatingChars,
				"Password contains too many repeating characters")
		case errors.Is(err, authpkg.ErrPasswordReused):
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodePasswordReused,
				"Password was recently used. Please choose a different password")
		default:
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
				"An unexpected error occurred")
		}
		return
	}

	_ = h.prStore.DeleteByHash(ctx, tokHash)
	_ = h.prStore.DeleteAllForUser(ctx, prt.UserID)

	h.createAuditEntry(r, "auth.password_reset_completed", prt.UserID.String(), nil, nil)

	apierr.WriteSuccess(w, http.StatusOK, map[string]any{"message": "Password reset successful"})
}

func writeGenericSuccess(w http.ResponseWriter) {
	apierr.WriteSuccess(w, http.StatusOK, map[string]any{
		"message": "If that email exists, a reset link has been sent.",
	})
}
