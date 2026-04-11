package auth

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	authpkg "github.com/btopcu/argus/internal/auth"
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
}

type refreshResponse struct {
	Token string `json:"token"`
}

type setup2FAResponse struct {
	Secret string `json:"secret"`
	QRURI  string `json:"qr_uri"`
}

type verify2FARequest struct {
	Code string `json:"code"`
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

	apierr.WriteSuccess(w, http.StatusOK, loginResponse{
		User:        result.User,
		Token:       result.Token,
		Requires2FA: result.Requires2FA,
	})
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

	if req.Code == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "2FA code is required")
		return
	}

	ipAddr := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()

	result, err := h.svc.Verify2FA(r.Context(), userID, req.Code, ipAddr, userAgent)
	if err != nil {
		if errors.Is(err, authpkg.ErrInvalid2FACode) {
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalid2FACode,
				"Invalid or expired 2FA code")
			return
		}
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError,
			"An unexpected error occurred")
		return
	}

	h.setRefreshCookie(w, result.RefreshToken)

	apierr.WriteSuccess(w, http.StatusOK, verify2FAResponse{
		Token: result.Token,
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
