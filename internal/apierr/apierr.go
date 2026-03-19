package apierr

import (
	"encoding/json"
	"net/http"
)

type contextKey string

const (
	TenantIDKey contextKey = "tenant_id"
	UserIDKey   contextKey = "user_id"
	RoleKey     contextKey = "role"
)

const (
	CodeInternalError    = "INTERNAL_ERROR"
	CodeInvalidFormat    = "INVALID_FORMAT"
	CodeValidationError  = "VALIDATION_ERROR"
	CodeNotFound         = "NOT_FOUND"
	CodeConflict         = "CONFLICT"
	CodeAlreadyExists    = "ALREADY_EXISTS"
	CodeMSISDNNotFound   = "MSISDN_NOT_FOUND"
	CodeMSISDNNotAvailable = "MSISDN_NOT_AVAILABLE"
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
	Total      int64  `json:"total"`
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
