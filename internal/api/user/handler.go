package user

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

var validRoles = map[string]bool{
	"api_user":         true,
	"analyst":          true,
	"policy_editor":    true,
	"sim_manager":      true,
	"operator_manager": true,
	"tenant_admin":     true,
}

type userStoreI interface {
	GetByID(ctx context.Context, id uuid.UUID) (*store.User, error)
	ListByTenant(ctx context.Context, cursor string, limit int, roleFilter string, stateFilter string) ([]store.User, string, error)
	CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error)
	CreateUser(ctx context.Context, p store.CreateUserParams) (*store.User, error)
	UpdateUser(ctx context.Context, id uuid.UUID, p store.UpdateUserParams) (*store.User, error)
	DeletePII(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) (*store.PurgeResult, error)
	ClearLockout(ctx context.Context, userID uuid.UUID) error
	SetPasswordHash(ctx context.Context, userID uuid.UUID, hash string) error
	SetPasswordChangeRequired(ctx context.Context, userID uuid.UUID, required bool) error
	UpdateLocale(ctx context.Context, userID uuid.UUID, locale string) error
}

type sessionRevoker interface {
	RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error
	GetActiveByUserID(ctx context.Context, userID uuid.UUID) ([]store.UserSession, error)
}

type apiKeyRevoker interface {
	RevokeAllByUser(ctx context.Context, userID uuid.UUID) (int64, error)
}

type wsDropper interface {
	DropUser(userID uuid.UUID)
}

type Handler struct {
	userStore      userStoreI
	tenantStore    *store.TenantStore
	auditSvc       audit.Auditor
	auditStore     *store.AuditStore
	logger         zerolog.Logger
	sessionStore   sessionRevoker
	apiKeyStore    apiKeyRevoker
	wsHub          wsDropper
	passwordPolicy auth.PasswordPolicy
	bcryptCost     int
	viewStore      *store.UserViewStore
	columnPrefStore *store.UserColumnPrefStore
}

type HandlerOption func(*Handler)

func WithSessionStore(s sessionRevoker) HandlerOption {
	return func(h *Handler) { h.sessionStore = s }
}

func WithAPIKeyStore(s apiKeyRevoker) HandlerOption {
	return func(h *Handler) { h.apiKeyStore = s }
}

func WithWSHub(hub wsDropper) HandlerOption {
	return func(h *Handler) { h.wsHub = hub }
}

func WithAuditStore(s *store.AuditStore) HandlerOption {
	return func(h *Handler) { h.auditStore = s }
}

func WithPasswordPolicy(policy auth.PasswordPolicy, bcryptCost int) HandlerOption {
	return func(h *Handler) {
		h.passwordPolicy = policy
		h.bcryptCost = bcryptCost
	}
}

func WithViewStore(s *store.UserViewStore) HandlerOption {
	return func(h *Handler) { h.viewStore = s }
}

func WithColumnPrefStore(s *store.UserColumnPrefStore) HandlerOption {
	return func(h *Handler) { h.columnPrefStore = s }
}

func NewHandler(userStore *store.UserStore, tenantStore *store.TenantStore, auditSvc audit.Auditor, logger zerolog.Logger, opts ...HandlerOption) *Handler {
	h := &Handler{
		userStore:   userStore,
		tenantStore: tenantStore,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("component", "user_handler").Logger(),
		bcryptCost:  12,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

type userResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	State       string  `json:"state"`
	LastLoginAt *string `json:"last_login_at,omitempty"`
	LockedUntil *string `json:"locked_until,omitempty"`
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
	if u.LockedUntil != nil {
		s := u.LockedUntil.Format("2006-01-02T15:04:05Z07:00")
		resp.LockedUntil = &s
	}
	return resp
}

type userDetailResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	State       string  `json:"state"`
	TOTPEnabled bool    `json:"totp_enabled"`
	LastLoginAt *string `json:"last_login_at,omitempty"`
	LockedUntil *string `json:"locked_until,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func toUserDetailResponse(u *store.User) userDetailResponse {
	resp := userDetailResponse{
		ID:          u.ID.String(),
		Email:       u.Email,
		Name:        u.Name,
		Role:        u.Role,
		State:       u.State,
		TOTPEnabled: u.TOTPEnabled,
		CreatedAt:   u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if u.LastLoginAt != nil {
		s := u.LastLoginAt.Format("2006-01-02T15:04:05Z07:00")
		resp.LastLoginAt = &s
	}
	if u.LockedUntil != nil {
		s := u.LockedUntil.Format("2006-01-02T15:04:05Z07:00")
		resp.LockedUntil = &s
	}
	return resp
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid user ID format")
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	existing, err := h.userStore.GetByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("get user detail")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	callerRole, _ := r.Context().Value(apierr.RoleKey).(string)
	if callerRole != "super_admin" && existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toUserDetailResponse(existing))
}

func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid user ID format")
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	existing, err := h.userStore.GetByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("get user for activity")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	callerRole, _ := r.Context().Value(apierr.RoleKey).(string)
	if callerRole != "super_admin" && existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
		return
	}

	if h.auditStore == nil {
		apierr.WriteSuccess(w, http.StatusOK, []interface{}{})
		return
	}

	q := r.URL.Query()
	cursor := q.Get("cursor")
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	entries, nextCursor, err := h.auditStore.List(r.Context(), tenantID, store.ListAuditParams{
		Cursor: cursor,
		Limit:  limit,
		UserID: &targetID,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("list user activity")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteList(w, http.StatusOK, entries, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
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
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeTenantLimitExceeded,
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

func (h *Handler) Unlock(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid user ID format")
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	existing, err := h.userStore.GetByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("get user for unlock")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
		return
	}

	if err := h.userStore.ClearLockout(r.Context(), targetID); err != nil {
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("clear lockout")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "user.unlock", targetID.String(), nil, nil)

	apierr.WriteSuccess(w, http.StatusOK, map[string]string{"status": "success"})
}

func (h *Handler) RevokeSessions(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid user ID format")
		return
	}

	callerID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	callerRole, _ := r.Context().Value(apierr.RoleKey).(string)
	isSelf := callerID == targetID

	if !isSelf && !apierr.HasRole(callerRole, "tenant_admin") {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
			"This action requires tenant_admin role or higher or must be performed on your own account",
			[]map[string]string{{"required_role": "tenant_admin", "current_role": callerRole}})
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	existing, err := h.userStore.GetByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("get user for revoke sessions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
		return
	}

	includeAPIKeys := r.URL.Query().Get("include_api_keys") == "true"

	var sessionsCount int
	if h.sessionStore != nil {
		active, aErr := h.sessionStore.GetActiveByUserID(r.Context(), targetID)
		if aErr != nil {
			h.logger.Error().Err(aErr).Str("user_id", idStr).Msg("count active sessions")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		sessionsCount = len(active)
		if rErr := h.sessionStore.RevokeAllUserSessions(r.Context(), targetID); rErr != nil {
			h.logger.Error().Err(rErr).Str("user_id", idStr).Msg("revoke user sessions")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
	}

	var apiKeysCount int64
	if includeAPIKeys && h.apiKeyStore != nil {
		n, kErr := h.apiKeyStore.RevokeAllByUser(r.Context(), targetID)
		if kErr != nil {
			h.logger.Error().Err(kErr).Str("user_id", idStr).Msg("revoke user api keys")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		apiKeysCount = n
	}

	if h.wsHub != nil {
		h.wsHub.DropUser(targetID)
	}

	h.createAuditEntry(r, "user.sessions_revoked", targetID.String(), nil, map[string]interface{}{
		"include_api_keys": includeAPIKeys,
		"sessions_count":   sessionsCount,
		"apikeys_count":    apiKeysCount,
	})

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"sessions_revoked": sessionsCount,
		"apikeys_revoked":  apiKeysCount,
	})
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid user ID format")
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	existing, err := h.userStore.GetByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
			return
		}
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("get user for password reset")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "User not found")
		return
	}

	tempPassword, err := auth.GenerateRandomPolicyCompliant(h.passwordPolicy)
	if err != nil {
		h.logger.Error().Err(err).Msg("generate temp password")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	cost := h.bcryptCost
	if cost <= 0 {
		cost = 12
	}
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(tempPassword), cost)
	if err != nil {
		h.logger.Error().Err(err).Msg("hash temp password")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if err := h.userStore.SetPasswordHash(r.Context(), targetID, string(hashBytes)); err != nil {
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("set password hash")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if err := h.userStore.SetPasswordChangeRequired(r.Context(), targetID, true); err != nil {
		h.logger.Error().Err(err).Str("user_id", idStr).Msg("set password change required")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.sessionStore != nil {
		if err := h.sessionStore.RevokeAllUserSessions(r.Context(), targetID); err != nil {
			h.logger.Error().Err(err).Str("user_id", idStr).Msg("revoke sessions after password reset")
		}
	}

	if h.wsHub != nil {
		h.wsHub.DropUser(targetID)
	}

	h.createAuditEntry(r, "user.password_reset", targetID.String(), nil, nil)

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"temp_password": tempPassword,
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
