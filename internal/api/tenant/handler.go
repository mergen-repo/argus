package tenant

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	tenantStore *store.TenantStore
	auditSvc    audit.Auditor
	logger      zerolog.Logger
}

func NewHandler(tenantStore *store.TenantStore, auditSvc audit.Auditor, logger zerolog.Logger) *Handler {
	return &Handler{
		tenantStore: tenantStore,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("component", "tenant_handler").Logger(),
	}
}

type tenantResponse struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Slug             string          `json:"slug"`
	Domain           *string         `json:"domain"`
	ContactEmail     string          `json:"contact_email"`
	ContactPhone     *string         `json:"contact_phone,omitempty"`
	MaxSims          int             `json:"max_sims"`
	MaxApns          int             `json:"max_apns"`
	MaxUsers         int             `json:"max_users"`
	Settings         json.RawMessage `json:"settings,omitempty"`
	State            string          `json:"state"`
	SimCount         int             `json:"sim_count"`
	UserCount        int             `json:"user_count"`
	APNCount         *int            `json:"apn_count,omitempty"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
}

type createTenantRequest struct {
	Name                 string  `json:"name"`
	Domain               *string `json:"domain"`
	ContactEmail         string  `json:"contact_email"`
	ContactPhone         *string `json:"contact_phone"`
	MaxSims              *int    `json:"max_sims"`
	MaxApns              *int    `json:"max_apns"`
	MaxUsers             *int    `json:"max_users"`
	AdminName            string  `json:"admin_name"`
	AdminEmail           string  `json:"admin_email"`
	AdminInitialPassword string  `json:"admin_initial_password"`
}

type updateTenantRequest struct {
	Name         *string          `json:"name"`
	ContactEmail *string          `json:"contact_email"`
	ContactPhone *string          `json:"contact_phone"`
	MaxSims      *int             `json:"max_sims"`
	MaxApns      *int             `json:"max_apns"`
	MaxUsers     *int             `json:"max_users"`
	State        *string          `json:"state"`
	Settings     *json.RawMessage `json:"settings"`
}

type tenantStatsResponse struct {
	SimCount       int `json:"sim_count"`
	UserCount      int `json:"user_count"`
	APNCount       int `json:"apn_count"`
	ActiveSessions int `json:"active_sessions"`
	StorageBytes   int `json:"storage_bytes"`
}

var slugNonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(name)
	s = slugNonAlnum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func toTenantResponse(t *store.Tenant) tenantResponse {
	return tenantResponse{
		ID:           t.ID.String(),
		Name:         t.Name,
		Slug:         slugify(t.Name),
		Domain:       t.Domain,
		ContactEmail: t.ContactEmail,
		ContactPhone: t.ContactPhone,
		MaxSims:      t.MaxSims,
		MaxApns:      t.MaxApns,
		MaxUsers:     t.MaxUsers,
		Settings:     t.Settings,
		State:        t.State,
		CreatedAt:    t.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:    t.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func toTenantWithCountsResponse(twc *store.TenantWithCounts) tenantResponse {
	resp := toTenantResponse(&twc.Tenant)
	resp.SimCount = twc.SimCount
	resp.UserCount = twc.UserCount
	return resp
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	stateFilter := r.URL.Query().Get("state")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	tenants, nextCursor, err := h.tenantStore.ListWithCounts(r.Context(), cursor, limit, stateFilter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list tenants")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]tenantResponse, 0, len(tenants))
	for i := range tenants {
		items = append(items, toTenantWithCountsResponse(&tenants[i]))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name is required", "code": "required"})
	}
	if req.ContactEmail == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "contact_email", "message": "Contact email is required", "code": "required"})
	}
	if req.AdminName == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "admin_name", "message": "Admin name is required", "code": "required"})
	}
	if req.AdminEmail == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "admin_email", "message": "Admin email is required", "code": "required"})
	} else if !isValidAdminEmail(req.AdminEmail) {
		validationErrors = append(validationErrors, map[string]string{"field": "admin_email", "message": "Invalid email format", "code": "format"})
	}
	if len(req.AdminInitialPassword) < 8 {
		validationErrors = append(validationErrors, map[string]string{"field": "admin_initial_password", "message": "Admin password must be at least 8 characters", "code": "min_length"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	hashBytes, err := bcrypt.GenerateFromPassword([]byte(req.AdminInitialPassword), 12)
	if err != nil {
		h.logger.Error().Err(err).Msg("hash admin password")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromContext(r)

	t, adminUser, err := h.tenantStore.CreateTenantWithAdmin(r.Context(), store.CreateTenantWithAdminParams{
		CreateTenantParams: store.CreateTenantParams{
			Name:         req.Name,
			Domain:       req.Domain,
			ContactEmail: req.ContactEmail,
			ContactPhone: req.ContactPhone,
			MaxSims:      req.MaxSims,
			MaxApns:      req.MaxApns,
			MaxUsers:     req.MaxUsers,
			CreatedBy:    userID,
		},
		AdminName:         req.AdminName,
		AdminEmail:        req.AdminEmail,
		AdminPasswordHash: string(hashBytes),
	})
	if err != nil {
		if errors.Is(err, store.ErrDomainExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"A tenant with this domain already exists",
				[]map[string]string{{"field": "domain", "value": ptrStr(req.Domain)}})
			return
		}
		if errors.Is(err, store.ErrEmailExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"A user with this admin email already exists",
				[]map[string]string{{"field": "admin_email", "value": req.AdminEmail}})
			return
		}
		h.logger.Error().Err(err).Msg("create tenant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.emitAuditForTenant(r, t.ID, "tenant.create", "tenant", t.ID.String(), nil, t)
	h.emitAuditForTenant(r, t.ID, "user.create", "user", adminUser.ID.String(), nil, map[string]interface{}{
		"email": adminUser.Email, "name": adminUser.Name, "role": adminUser.Role, "tenant_id": t.ID.String(),
	})

	resp := toTenantResponse(t)
	apierr.WriteSuccess(w, http.StatusCreated, map[string]interface{}{
		"tenant":        resp,
		"admin_user_id": adminUser.ID.String(),
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid tenant ID format")
		return
	}

	role, _ := r.Context().Value(apierr.RoleKey).(string)
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	if role != "super_admin" && id != tenantID {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "You can only view your own tenant")
		return
	}

	t, err := h.tenantStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrTenantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Tenant not found")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", idStr).Msg("get tenant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	stats, err := h.tenantStore.GetStats(r.Context(), id)
	if err != nil {
		h.logger.Warn().Err(err).Str("tenant_id", idStr).Msg("get tenant stats")
	}

	resp := toTenantResponse(t)
	if stats != nil {
		resp.SimCount = stats.SimCount
		resp.UserCount = stats.UserCount
		resp.APNCount = &stats.APNCount
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

var validTenantTransitions = map[string][]string{
	"active":    {"suspended"},
	"suspended": {"active", "terminated"},
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid tenant ID format")
		return
	}

	role, _ := r.Context().Value(apierr.RoleKey).(string)
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	if role != "super_admin" && id != tenantID {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "You can only update your own tenant")
		return
	}

	var req updateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if role != "super_admin" {
		if req.MaxSims != nil || req.MaxApns != nil || req.MaxUsers != nil || req.State != nil {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden,
				"Only super_admin can modify resource limits and tenant state")
			return
		}
	}

	existing, err := h.tenantStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrTenantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Tenant not found")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", idStr).Msg("get tenant for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if req.State != nil && *req.State != existing.State {
		allowed := validTenantTransitions[existing.State]
		valid := false
		for _, s := range allowed {
			if s == *req.State {
				valid = true
				break
			}
		}
		if !valid {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				"Invalid tenant state transition",
				[]map[string]interface{}{
					{"from_state": existing.State, "to_state": *req.State, "allowed_transitions": allowed},
				})
			return
		}
	}

	userID := userIDFromContext(r)

	updated, err := h.tenantStore.Update(r.Context(), id, store.UpdateTenantParams{
		Name:         req.Name,
		ContactEmail: req.ContactEmail,
		ContactPhone: req.ContactPhone,
		MaxSims:      req.MaxSims,
		MaxApns:      req.MaxApns,
		MaxUsers:     req.MaxUsers,
		State:        req.State,
		Settings:     req.Settings,
		UpdatedBy:    userID,
	})
	if err != nil {
		if errors.Is(err, store.ErrTenantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Tenant not found")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", idStr).Msg("update tenant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "tenant.update", id.String(), existing, updated)

	resp := toTenantResponse(updated)
	if stats, err := h.tenantStore.GetStats(r.Context(), id); err == nil && stats != nil {
		resp.SimCount = stats.SimCount
		resp.UserCount = stats.UserCount
		resp.APNCount = &stats.APNCount
	}
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid tenant ID format")
		return
	}

	role, _ := r.Context().Value(apierr.RoleKey).(string)
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	if role != "super_admin" && id != tenantID {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "You can only view your own tenant stats")
		return
	}

	_, err = h.tenantStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrTenantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Tenant not found")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", idStr).Msg("get tenant for stats")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	stats, err := h.tenantStore.GetStats(r.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", idStr).Msg("get tenant stats")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, tenantStatsResponse{
		SimCount:       stats.SimCount,
		UserCount:      stats.UserCount,
		APNCount:       stats.APNCount,
		ActiveSessions: stats.ActiveSessions,
		StorageBytes:   stats.StorageBytes,
	})
}

func (h *Handler) createAuditEntry(r *http.Request, action, entityID string, before, after interface{}) {
	if h.auditSvc == nil {
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	userID := userIDFromContext(r)
	ip := r.RemoteAddr
	ua := r.UserAgent()

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
		EntityType:    "tenant",
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

func userIDFromContext(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *Handler) emitAuditForTenant(r *http.Request, tenantID uuid.UUID, action, entityType, entityID string, before, after interface{}) {
	if h.auditSvc == nil {
		return
	}

	userID := userIDFromContext(r)
	ip := r.RemoteAddr
	ua := r.UserAgent()

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
		EntityType:    entityType,
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

func isValidAdminEmail(email string) bool {
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
