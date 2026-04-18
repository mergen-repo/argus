package apierr

import (
	"encoding/json"
	"net/http"
)

type contextKey string

const (
	CorrelationIDKey contextKey = "correlation_id"
	// TenantIDKey holds the EFFECTIVE tenant for the current request. For a
	// super_admin with an active tenant-context switch, this is the selected
	// tenant; for all other users it equals the user's home tenant.
	TenantIDKey      contextKey = "tenant_id"
	// HomeTenantIDKey holds the user's original/home tenant from the JWT.
	// Audit consumers that need the admin's real tenant (not the active
	// scope) should read this key.
	HomeTenantIDKey  contextKey = "home_tenant_id"
	// ActiveTenantIDKey is set only when a super_admin has switched into a
	// tenant context. Absent otherwise. Presence of this key is the
	// authoritative signal that "viewing-as-tenant" is active.
	ActiveTenantIDKey contextKey = "active_tenant_id"
	UserIDKey        contextKey = "user_id"
	RoleKey          contextKey = "role"
	AuthTypeKey      contextKey = "auth_type"
	ScopesKey        contextKey = "scopes"
	APIKeyIDKey      contextKey = "api_key_id"
)

const (
	CodeInternalError      = "INTERNAL_ERROR"
	CodeInvalidFormat      = "INVALID_FORMAT"
	CodeValidationError    = "VALIDATION_ERROR"
	CodeNotFound           = "NOT_FOUND"
	CodeConflict           = "CONFLICT"
	CodeAlreadyExists      = "ALREADY_EXISTS"
	CodeMSISDNNotFound     = "MSISDN_NOT_FOUND"
	CodeMSISDNNotAvailable = "MSISDN_NOT_AVAILABLE"

	CodeInvalidCredentials  = "INVALID_CREDENTIALS"
	CodeAccountLocked       = "ACCOUNT_LOCKED"
	CodeAccountDisabled     = "ACCOUNT_DISABLED"
	CodeInvalid2FACode      = "INVALID_2FA_CODE"
	CodeInvalidBackupCode   = "INVALID_BACKUP_CODE"
	CodeTOTPNotEnabled      = "TOTP_NOT_ENABLED"
	CodeTokenExpired        = "TOKEN_EXPIRED"
	CodeInvalidRefreshToken = "INVALID_REFRESH_TOKEN"

	CodeForbidden              = "FORBIDDEN"
	CodeInsufficientRole       = "INSUFFICIENT_ROLE"
	CodeScopeDenied            = "SCOPE_DENIED"
	CodeAPIKeyIPNotAllowed     = "API_KEY_IP_NOT_ALLOWED"

	CodeResourceLimitExceeded = "RESOURCE_LIMIT_EXCEEDED"
	CodeTenantLimitExceeded   = "TENANT_LIMIT_EXCEEDED"
	CodeTenantSuspended       = "TENANT_SUSPENDED"

	CodeRateLimited = "RATE_LIMITED"

	CodeRequestBodyTooLarge = "REQUEST_BODY_TOO_LARGE"

	CodeAPNHasActiveSIMs       = "APN_HAS_ACTIVE_SIMS"
	CodePoolExhausted          = "POOL_EXHAUSTED"
	CodeIPAlreadyAllocated     = "IP_ALREADY_ALLOCATED"
	CodeICCIDExists            = "ICCID_EXISTS"
	CodeIMSIExists             = "IMSI_EXISTS"
	CodeInvalidStateTransition = "INVALID_STATE_TRANSITION"

	CodeProfileAlreadyEnabled   = "PROFILE_ALREADY_ENABLED"
	CodeNotESIM                 = "NOT_ESIM"
	CodeInvalidProfileState     = "INVALID_PROFILE_STATE"
	CodeSameProfile             = "SAME_PROFILE"
	CodeDifferentSIM            = "DIFFERENT_SIM"
	CodeSessionDisconnectFailed = "SESSION_DISCONNECT_FAILED"
	CodeProfileLimitExceeded    = "PROFILE_LIMIT_EXCEEDED"
	CodeCannotDeleteEnabled     = "CANNOT_DELETE_ENABLED_PROFILE"
	CodeDuplicateProfile        = "DUPLICATE_PROFILE"
	CodeProfileNotAvailable     = "PROFILE_NOT_AVAILABLE"
	CodeIPReleaseFailed         = "IP_RELEASE_FAILED"

	CodeOperatorUnavailable    = "OPERATOR_UNAVAILABLE"
	CodeProtocolNotConfigured  = "PROTOCOL_NOT_CONFIGURED"

	CodePasswordTooShort         = "PASSWORD_TOO_SHORT"
	CodePasswordMissingClass     = "PASSWORD_MISSING_CLASS"
	CodePasswordRepeatingChars   = "PASSWORD_REPEATING_CHARS"
	CodePasswordReused           = "PASSWORD_REUSED"
	CodePasswordChangeRequired   = "PASSWORD_CHANGE_REQUIRED"

	CodeInvalidCIDR = "INVALID_CIDR"

	CodeRoamingAgreementNotFound          = "ROAMING_AGREEMENT_NOT_FOUND"
	CodeRoamingAgreementOverlap           = "ROAMING_AGREEMENT_OVERLAP"
	CodeRoamingAgreementInvalidDates      = "ROAMING_AGREEMENT_INVALID_DATES"
	CodeRoamingAgreementOperatorNotGranted = "ROAMING_AGREEMENT_OPERATOR_NOT_GRANTED"

	CodeInvalidDateRange = "INVALID_DATE_RANGE"
)

type SuccessResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Meta   interface{} `json:"meta,omitempty"`
}

type ErrorResponse struct {
	Status string     `json:"status"`
	Error  ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

type ListMeta struct {
	Total      int64  `json:"total,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
	Limit      int    `json:"limit,omitempty"`
}

type ListResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data"`
	Meta   ListMeta    `json:"meta"`
}

func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func WriteSuccess(w http.ResponseWriter, status int, data interface{}) {
	WriteJSON(w, status, SuccessResponse{
		Status: "success",
		Data:   data,
	})
}

func WriteList(w http.ResponseWriter, status int, data interface{}, meta ListMeta) {
	WriteJSON(w, status, ListResponse{
		Status: "success",
		Data:   data,
		Meta:   meta,
	})
}

func WriteError(w http.ResponseWriter, status int, code, message string, details ...interface{}) {
	resp := ErrorResponse{
		Status: "error",
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	}
	if len(details) > 0 {
		resp.Error.Details = details[0]
	}
	WriteJSON(w, status, resp)
}

var roleLevels = map[string]int{
	"api_user":         1,
	"analyst":          2,
	"policy_editor":    3,
	"sim_manager":      4,
	"operator_manager": 5,
	"tenant_admin":     6,
	"super_admin":      7,
}

func RoleLevel(role string) int {
	return roleLevels[role]
}

func HasRole(userRole, requiredRole string) bool {
	return RoleLevel(userRole) >= RoleLevel(requiredRole)
}
