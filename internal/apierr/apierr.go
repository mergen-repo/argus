package apierr

import (
	"encoding/json"
	"net/http"
)

type contextKey string

const (
	CorrelationIDKey contextKey = "correlation_id"
	TenantIDKey      contextKey = "tenant_id"
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
	CodeTokenExpired        = "TOKEN_EXPIRED"
	CodeInvalidRefreshToken = "INVALID_REFRESH_TOKEN"

	CodeForbidden        = "FORBIDDEN"
	CodeInsufficientRole = "INSUFFICIENT_ROLE"
	CodeScopeDenied      = "SCOPE_DENIED"

	CodeResourceLimitExceeded = "RESOURCE_LIMIT_EXCEEDED"
	CodeTenantSuspended       = "TENANT_SUSPENDED"

	CodeRateLimited = "RATE_LIMITED"

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
