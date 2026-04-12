package user

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var validRoles = map[string]bool{
	"api_user":         true,
	"analyst":          true,
	"policy_editor":    true,
	"sim_manager":      true,
	"operator_manager": true,
	"tenant_admin":     true,
}

type Handler struct {
	userStore   *store.UserStore
	tenantStore *store.TenantStore
	auditSvc    audit.Auditor
	logger      zerolog.Logger
}

func NewHandler(userStore *store.UserStore, tenantStore *store.TenantStore, auditSvc audit.Auditor, logger zerolog.Logger) *Handler {
	return &Handler{
		userStore:   userStore,
		tenantStore: tenantStore,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("component", "user_handler").Logger(),
	}
}

type userResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	State       string  `json:"state"`
	LastLoginAt *string `json:"last_login_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type createUserRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

type updateUserRequest struct {
	Name  *string `json:"name"`
	Role  *string `json:"role"`
	State *string `json:"state"`
}

func toUserResponse(u *store.User) userResponse {
	resp := userResponse{
		ID:        u.ID.String(),
		Email:     u.Email,
		Name:      u.Name,
		Role:      u.Role,
		State:     u.State,
		CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if u.LastLoginAt != nil {
		s := u.LastLoginAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastLoginAt = &s
	}
	return resp
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	roleFilter := r.URL.Query().Get("role")
	stateFilter := r.URL.Query().Get("state")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	users, nextCursor, err := h.userStore.ListByTenant(r.Context(), cursor, limit, roleFilter, stateFilter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list users")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]userResponse, 0, len(users))
	for _, u := range users {
		items = append(items, toUserResponse(&u))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Email == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "email", "message": "Email is required", "code": "required"})
	} else if !isValidEmail(req.Email) {
		validationErrors = append(validationErrors, map[string]string{"field": "email", "message": "Invalid email format", "code": "format"})
	}
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name is required", "code": "required"})
	}
	if req.Role == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "role", "message": "Role is required", "code": "required"})
	} else if !validRoles[req.Role] {
		callerRole, _ := r.Context().Value(apierr.RoleKey).(string)
		if req.Role == "super_admin" && callerRole != "super_admin" {
			validationErrors = append(validationErrors, map[string]string{"field": "role", "message": "Only super_admin can assign super_admin role", "code": "invalid_enum"})
		} else if req.Role != "super_admin" {
			validationErrors = append(validationErrors, map[string]string{"field": "role", "message": "Invalid role value", "code": "invalid_enum"})
		}
	}

	if req.Role != "" && validRoles[req.Role] {
		callerRole, _ := r.Context().Value(apierr.RoleKey).(string)
		if callerRole != "super_admin" && apierr.RoleLevel(req.Role) >= apierr.RoleLevel(callerRole) {
			validationErrors = append(validationErrors, map[string]string{"field": "role", "message": "Cannot assign a role equal to or higher than your own", "code": "invalid_enum"})
		}
	}

	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	tenant, err := h.tenantStore.GetByID(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("get tenant for resource limit check")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	currentCount, err := h.userStore.CountByTenant(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("count users for resource limit check")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if currentCount >= tenant.MaxUsers {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeResourceLimitExceeded,
			"Tenant resource limit exceeded",
			[]map[string]interface{}{
				{"resource": "users", "current": currentCount, "limit": tenant.MaxUsers},
			})
		return
	}

	u, err := h.userStore.CreateUser(r.Context(), store.CreateUserParams{
		Email: req.Email,
		Name:  req.Name,
		Role:  req.Role,
	})
	if err != nil {
		if errors.Is(err, store.ErrEmailExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"A user with this email already exists in this tenant",
				[]map[string]string{{"field": "email", "value": req.Email}})
			return
		}
		h.logger.Error().Err(err).Msg("create user")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "user.create", u.ID.String(), nil, u)

	apierr.WriteSuccess(w, http.StatusCreated, toUserResponse(u))
}

var validUserStates = map[string]bool{
	"active":   true,
	"disabled": true,
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid user ID format")
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	callerRole, _ := r.Context().Value(apierr.RoleKey).(string)
	callerID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	isSelf := callerID == targetID

	if isSelf && !apierr.HasRole(callerRole, "tenant_admin") {
		if req.Role != nil || req.State != nil {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden,
				"You can only update your own name. Role and state changes require tenant_admin or higher.")
			return
		}
	}

	if !isSelf && !apierr.HasRole(callerRole, "tenant_admin") {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
			"This action requires tenant_admin role or higher",
			[]map[string]string{{"required_role": "tenant_admin", "current_role": callerRole}})
		return
	}

	var validationErrors []map[string]string
	if req.Role != nil {
		if !validRoles[*req.Role] && *req.Role != "super_admin" {
			validationErrors = append(validationErrors, map[string]string{"field": "role", "message": "Invalid role value", "code": "invalid_enum"})
		}
		if *req.Role == "super_admin" && callerRole != "super_admin" {
			validationErrors = append(validationErrors, map[string]string{"field": "role", "message": "Only super_admin can assign super_admin role", "code": "invalid_enum"})
		}
		if callerRole != "super_admin" && apierr.RoleLevel(*req.Role) >= apierr.RoleLevel(callerRole) {
			validationErrors = append(validationErrors, map[string]string{"field": "role", "message": "Cannot assign a role equal to or higher than your own", "code": "invalid_enum"})
		}
	}
	if req.State != nil && !validUserStates[*req.State] {
		validationErrors = append(validationErrors, map[string]string{"field": "state", "message": "Invalid state value. Allowed: active, disabled", "code": "invalid_enum"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	existing, err := h.userStore.GetByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("get user for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
		return
	}

	updated, err := h.userStore.UpdateUser(r.Context(), targetID, store.UpdateUserParams{
		Name:  req.Name,
		Role:  req.Role,
		State: req.State,
	})
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("update user")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "user.update", targetID.String(), existing, updated)

	apierr.WriteSuccess(w, http.StatusOK, toUserResponse(updated))
}

// Delete handles GDPR erasure of a user's PII. It is gated by the `gdpr=1`
// query parameter to prevent accidental invocation. Only tenant_admin or
// higher may call it (router group enforces the role). Scope is the caller's
// own tenant; cross-tenant deletion is rejected as NOT_FOUND.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid user ID format")
		return
	}

	if r.URL.Query().Get("gdpr") != "1" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"GDPR erasure requires the `gdpr=1` query parameter",
			[]map[string]string{{"field": "gdpr", "message": "must be 1", "code": "required"}})
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	callerID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if callerID == targetID {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden,
			"You cannot GDPR-purge your own account; another tenant_admin must do so")
		return
	}

	existing, err := h.userStore.GetByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("get user for purge")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
		return
	}
	if existing.State == "purged" {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeConflict, "User is already purged")
		return
	}

	result, err := h.userStore.DeletePII(r.Context(), targetID, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("purge user pii")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "user.purge", targetID.String(), existing, map[string]interface{}{
		"state":            "purged",
		"sessions_revoked": result.SessionsRevoked,
		"purged_at":        result.PurgedAt,
	})

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"user_id":          result.UserID.String(),
		"sessions_revoked": result.SessionsRevoked,
		"purged_at":        result.PurgedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

func (h *Handler) createAuditEntry(r *http.Request, action, entityID string, before, after interface{}) {
	if h.auditSvc == nil {
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	ip := r.RemoteAddr
	ua := r.UserAgent()

	var userID *uuid.UUID
	if ok && uid != uuid.Nil {
		userID = &uid
	}

	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	var beforeData, afterData json.RawMessage
	if before != nil {
		beforeData, _ = json.Marshal(before)
	}
	if after != nil {
		afterData, _ = json.Marshal(after)
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		EntityType:    "user",
		EntityID:      entityID,
		BeforeData:    beforeData,
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Str("action", action).Msg("audit entry failed")
	}
}

func isValidEmail(email string) bool {
	at := strings.Index(email, "@")
	if at <= 0 {
		return false
	}
	domain := email[at+1:]
	if domain == "" {
		return false
	}
	dot := strings.Index(domain, ".")
	return dot > 0 && dot < len(domain)-1
}
