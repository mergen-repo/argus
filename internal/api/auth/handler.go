package auth

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	authpkg "github.com/btopcu/argus/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

type AuthHandler struct {
	svc           *authpkg.Service
	refreshMaxAge int
	secureCookie  bool
}

func NewAuthHandler(svc *authpkg.Service, refreshExpiry time.Duration, secureCookie bool) *AuthHandler {
	return &AuthHandler{
		svc:           svc,
		refreshMaxAge: int(refreshExpiry.Seconds()),
		secureCookie:  secureCookie,
	}
}

type loginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

type loginResponse struct {
	User        authpkg.UserInfo `json:"user"`
	Token       string           `json:"token"`
	Requires2FA bool             `json:"requires_2fa"`
	Partial     bool             `json:"partial,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	SessionID   string           `json:"session_id,omitempty"`
}

type refreshResponse struct {
	Token string `json:"token"`
}

type setup2FAResponse struct {
	Secret string `json:"secret"`
	QRURI  string `json:"qr_uri"`
}

type verify2FARequest struct {
	Code       string `json:"code"`
	BackupCode string `json:"backup_code"`
}

type generateBackupCodesResponse struct {
	Codes []string `json:"codes"`
}

type verify2FAResponse struct {
	Token string `json:"token"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.Email == "" || req.Password == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Email and password are required")
		return
	}

	ipAddr := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()

	result, lockInfo, err := h.svc.Login(r.Context(), req.Email, req.Password, ipAddr, userAgent, req.RememberMe)
	if err != nil {
		switch {
		case errors.Is(err, authpkg.ErrAccountLocked):
			details := []map[string]interface{}{
				{"retry_after_seconds": lockInfo.RetryAfterSeconds, "failed_attempts": lockInfo.FailedAttempts},
			}
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeAccountLocked,
				"Account locked due to too many failed attempts.", details)
		case errors.Is(err, authpkg.ErrAccountDisabled):
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeAccountDisabled,
				"Your account has been disabled. Contact your administrator.")
		case errors.Is(err, authpkg.ErrInvalidCredentials):
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
				"Invalid email or password")
		default:
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
				"An unexpected error occurred")
		}
		return
	}

	if !result.Requires2FA && result.RefreshToken != "" {
		h.setRefreshCookie(w, result.RefreshToken)
	}

	resp := loginResponse{
		User:        result.User,
		Token:       result.Token,
		Requires2FA: result.Requires2FA,
		Reason:      result.Reason,
	}
	if result.Reason != "" || result.Requires2FA {
		resp.Partial = true
	}
	if result.SessionID != (uuid.UUID{}) {
		resp.SessionID = result.SessionID.String()
	}
	if result.Reason == "password_change_required" {
		apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
			Status: "success",
			Data:   resp,
			Meta: map[string]string{
				"code": apierr.CodePasswordChangeRequired,
			},
		})
		return
	}
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("refresh_token")
	if err != nil || cookie.Value == "" {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidRefreshToken,
			"Refresh token is invalid or has been revoked")
		return
	}

	ipAddr := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()

	result, err := h.svc.Refresh(r.Context(), cookie.Value, ipAddr, userAgent)
	if err != nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidRefreshToken,
			"Refresh token is invalid or has been revoked")
		return
	}

	h.setRefreshCookie(w, result.RefreshToken)

	apierr.WriteSuccess(w, http.StatusOK, refreshResponse{
		Token: result.Token,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
			"Authentication required")
		return
	}

	cookie, err := r.Cookie("refresh_token")
	refreshToken := ""
	if err == nil {
		refreshToken = cookie.Value
	}

	_ = h.svc.Logout(r.Context(), userID, refreshToken)

	h.clearRefreshCookie(w)

	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) Setup2FA(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
			"Authentication required")
		return
	}

	result, err := h.svc.Setup2FA(r.Context(), userID)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"Failed to setup 2FA")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, setup2FAResponse{
		Secret: result.Secret,
		QRURI:  result.QRURI,
	})
}

func (h *AuthHandler) Verify2FA(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
			"Authentication required")
		return
	}

	var req verify2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.Code == "" && req.BackupCode == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "code or backup_code is required")
		return
	}

	ipAddr := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()

	result, err := h.svc.Verify2FAWithInput(r.Context(), userID, authpkg.Verify2FAInput{
		Code:       req.Code,
		BackupCode: req.BackupCode,
		IPAddress:  ipAddr,
		UserAgent:  userAgent,
	})
	if err != nil {
		switch {
		case errors.Is(err, authpkg.ErrInvalidBackupCode):
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidBackupCode,
				"Invalid backup code")
		case errors.Is(err, authpkg.ErrInvalid2FACode):
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalid2FACode,
				"Invalid or expired 2FA code")
		default:
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
				"An unexpected error occurred")
		}
		return
	}

	h.setRefreshCookie(w, result.RefreshToken)

	data := verify2FAResponse{Token: result.Token}
	if result.UsedBackupCode && result.BackupCodesRemaining <= 3 {
		type backupMeta struct {
			BackupCodesRemaining int `json:"backup_codes_remaining"`
		}
		apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
			Status: "success",
			Data:   data,
			Meta:   backupMeta{BackupCodesRemaining: result.BackupCodesRemaining},
		})
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, data)
}

func (h *AuthHandler) GenerateBackupCodes(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
			"Authentication required")
		return
	}

	codes, err := h.svc.GenerateBackupCodes(r.Context(), userID)
	if err != nil {
		if errors.Is(err, authpkg.ErrTOTPNotEnabled) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeTOTPNotEnabled,
				"TOTP must be enabled before generating backup codes")
			return
		}
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, generateBackupCodesResponse{Codes: codes})
}

func (h *AuthHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
			"Authentication required")
		return
	}

	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	sessions, nextCursor, err := h.svc.ListSessions(r.Context(), userID, q.Get("cursor"), limit)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"An unexpected error occurred")
		return
	}

	type sessionDTO struct {
		ID        string  `json:"id"`
		IPAddress *string `json:"ip_address"`
		UserAgent *string `json:"user_agent"`
		CreatedAt string  `json:"created_at"`
		ExpiresAt string  `json:"expires_at"`
	}

	dtos := make([]sessionDTO, 0, len(sessions))
	for _, s := range sessions {
		dtos = append(dtos, sessionDTO{
			ID:        s.ID.String(),
			IPAddress: s.IPAddress,
			UserAgent: s.UserAgent,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
			ExpiresAt: s.ExpiresAt.Format(time.RFC3339),
		})
	}

	apierr.WriteList(w, http.StatusOK, dtos, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

func (h *AuthHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
			"Authentication required")
		return
	}

	sessionIDStr := chi.URLParam(r, "id")
	if sessionIDStr == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Session ID is required")
		return
	}
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid session ID format")
		return
	}

	if err := h.svc.RevokeSessionForUser(r.Context(), userID, sessionID); err != nil {
		if errors.Is(err, authpkg.ErrSessionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Session not found")
			return
		}
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]bool{"revoked": true})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type changePasswordResponse struct {
	AccessToken  string           `json:"access_token"`
	RefreshToken string           `json:"refresh_token"`
	User         authpkg.UserInfo `json:"user"`
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
			"Authentication required")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"current_password and new_password are required")
		return
	}

	if err := h.svc.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, authpkg.ErrInvalidCredentials):
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
				"Current password is incorrect")
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

	ipAddr := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()

	result, err := h.svc.CreateSessionForUser(r.Context(), userID, ipAddr, userAgent)
	if err != nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"An unexpected error occurred")
		return
	}

	h.setRefreshCookie(w, result.RefreshToken)

	apierr.WriteSuccess(w, http.StatusOK, changePasswordResponse{
		AccessToken:  result.Token,
		RefreshToken: result.RefreshToken,
		User:         result.User,
	})
}

func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    token,
		Path:     "/api/v1/auth",
		MaxAge:   h.refreshMaxAge,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *AuthHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteStrictMode,
	})
}
