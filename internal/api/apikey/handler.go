package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
)

var scopePattern = regexp.MustCompile(`^(\*|[a-z_]+:\*|[a-z_]+:[a-z_]+)$`)

type Handler struct {
	apiKeyStore *store.APIKeyStore
	tenantStore *store.TenantStore
	auditSvc    audit.Auditor
	maxKeys     int
	logger      zerolog.Logger
}

func NewHandler(apiKeyStore *store.APIKeyStore, tenantStore *store.TenantStore, auditSvc audit.Auditor, maxKeys int, logger zerolog.Logger) *Handler {
	return &Handler{
		apiKeyStore: apiKeyStore,
		tenantStore: tenantStore,
		auditSvc:    auditSvc,
		maxKeys:     maxKeys,
		logger:      logger.With().Str("component", "apikey_handler").Logger(),
	}
}

type createRequest struct {
	Name               string   `json:"name"`
	Scopes             []string `json:"scopes"`
	RateLimitPerMinute *int     `json:"rate_limit_per_minute"`
	RateLimitPerHour   *int     `json:"rate_limit_per_hour"`
	ExpiresAt          *string  `json:"expires_at"`
}

type updateRequest struct {
	Name               *string  `json:"name"`
	Scopes             *[]string `json:"scopes"`
	RateLimitPerMinute *int     `json:"rate_limit_per_minute"`
	RateLimitPerHour   *int     `json:"rate_limit_per_hour"`
}

type apiKeyResponse struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Prefix     string         `json:"prefix"`
	Key        string         `json:"key,omitempty"`
	Scopes     []string       `json:"scopes"`
	RateLimits rateLimitsResp `json:"rate_limits"`
	UsageCount int64          `json:"usage_count,omitempty"`
	LastUsedAt *string        `json:"last_used_at,omitempty"`
	ExpiresAt  *string        `json:"expires_at,omitempty"`
	RevokedAt  *string        `json:"revoked_at,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

type rateLimitsResp struct {
	PerMinute int `json:"per_minute"`
	PerHour   int `json:"per_hour"`
}

type rotateResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Prefix          string `json:"prefix"`
	Key             string `json:"key"`
	GracePeriodEnds string `json:"grace_period_ends"`
}

func toAPIKeyResponse(k *store.APIKey) apiKeyResponse {
	resp := apiKeyResponse{
		ID:     k.ID.String(),
		Name:   k.Name,
		Prefix: k.KeyPrefix,
		Scopes: k.Scopes,
		RateLimits: rateLimitsResp{
			PerMinute: k.RateLimitPerMinute,
			PerHour:   k.RateLimitPerHour,
		},
		UsageCount: k.UsageCount,
		CreatedAt:  k.CreatedAt.Format(time.RFC3339),
	}
	if k.LastUsedAt != nil {
		s := k.LastUsedAt.Format(time.RFC3339)
		resp.LastUsedAt = &s
	}
	if k.ExpiresAt != nil {
		s := k.ExpiresAt.Format(time.RFC3339)
		resp.ExpiresAt = &s
	}
	if k.RevokedAt != nil {
		s := k.RevokedAt.Format(time.RFC3339)
		resp.RevokedAt = &s
	}
	return resp
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name is required", "code": "required"})
	} else if len(req.Name) > 100 {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name must be at most 100 characters", "code": "max_length"})
	}

	if len(req.Scopes) == 0 {
		validationErrors = append(validationErrors, map[string]string{"field": "scopes", "message": "Scopes is required and must be non-empty", "code": "required"})
	} else {
		for _, scope := range req.Scopes {
			if !scopePattern.MatchString(scope) {
				validationErrors = append(validationErrors, map[string]string{
					"field":   "scopes",
					"message": fmt.Sprintf("Invalid scope format: %s. Expected pattern: resource:action, resource:*, or *", scope),
					"code":    "format",
				})
				break
			}
		}
	}

	if req.RateLimitPerMinute != nil && *req.RateLimitPerMinute <= 0 {
		validationErrors = append(validationErrors, map[string]string{"field": "rate_limit_per_minute", "message": "Rate limit per minute must be positive", "code": "min_value"})
	}
	if req.RateLimitPerHour != nil && *req.RateLimitPerHour <= 0 {
		validationErrors = append(validationErrors, map[string]string{"field": "rate_limit_per_hour", "message": "Rate limit per hour must be positive", "code": "min_value"})
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, pErr := time.Parse(time.RFC3339, *req.ExpiresAt)
		if pErr != nil {
			validationErrors = append(validationErrors, map[string]string{"field": "expires_at", "message": "Invalid date format. Expected ISO8601/RFC3339", "code": "format"})
		} else if t.Before(time.Now()) {
			validationErrors = append(validationErrors, map[string]string{"field": "expires_at", "message": "Expiry date must be in the future", "code": "min_value"})
		} else {
			expiresAt = &t
		}
	}

	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	currentCount, err := h.apiKeyStore.CountByTenant(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("count api keys for resource limit check")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if currentCount >= h.maxKeys {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeResourceLimitExceeded,
			"Tenant resource limit exceeded",
			[]map[string]interface{}{
				{"resource": "api_keys", "current": currentCount, "limit": h.maxKeys},
			})
		return
	}

	prefix, fullKey, keyHash, err := generateAPIKey()
	if err != nil {
		h.logger.Error().Err(err).Msg("generate api key")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	perMinute := 1000
	if req.RateLimitPerMinute != nil {
		perMinute = *req.RateLimitPerMinute
	}
	perHour := 30000
	if req.RateLimitPerHour != nil {
		perHour = *req.RateLimitPerHour
	}

	var createdBy *uuid.UUID
	if uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID); ok && uid != uuid.Nil {
		createdBy = &uid
	}

	k, err := h.apiKeyStore.Create(r.Context(), store.CreateAPIKeyParams{
		Name:               req.Name,
		KeyPrefix:          prefix,
		KeyHash:            keyHash,
		Scopes:             req.Scopes,
		RateLimitPerMinute: perMinute,
		RateLimitPerHour:   perHour,
		ExpiresAt:          expiresAt,
		CreatedBy:          createdBy,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create api key")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := toAPIKeyResponse(k)
	resp.Key = fullKey

	h.createAuditEntry(r, "apikey.create", k.ID.String(), nil, k)

	apierr.WriteSuccess(w, http.StatusCreated, resp)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	keys, nextCursor, err := h.apiKeyStore.ListByTenant(r.Context(), cursor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list api keys")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]apiKeyResponse, 0, len(keys))
	for _, k := range keys {
		items = append(items, toAPIKeyResponse(&k))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid API key ID format")
		return
	}

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Name != nil && *req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name cannot be empty", "code": "required"})
	}
	if req.Name != nil && len(*req.Name) > 100 {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name must be at most 100 characters", "code": "max_length"})
	}

	if req.Scopes != nil {
		if len(*req.Scopes) == 0 {
			validationErrors = append(validationErrors, map[string]string{"field": "scopes", "message": "Scopes must be non-empty", "code": "required"})
		} else {
			for _, scope := range *req.Scopes {
				if !scopePattern.MatchString(scope) {
					validationErrors = append(validationErrors, map[string]string{
						"field":   "scopes",
						"message": fmt.Sprintf("Invalid scope format: %s", scope),
						"code":    "format",
					})
					break
				}
			}
		}
	}

	if req.RateLimitPerMinute != nil && *req.RateLimitPerMinute <= 0 {
		validationErrors = append(validationErrors, map[string]string{"field": "rate_limit_per_minute", "message": "Rate limit per minute must be positive", "code": "min_value"})
	}
	if req.RateLimitPerHour != nil && *req.RateLimitPerHour <= 0 {
		validationErrors = append(validationErrors, map[string]string{"field": "rate_limit_per_hour", "message": "Rate limit per hour must be positive", "code": "min_value"})
	}

	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	existing, err := h.apiKeyStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "API key not found")
			return
		}
		h.logger.Error().Err(err).Str("api_key_id", idStr).Msg("get api key for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	updated, err := h.apiKeyStore.Update(r.Context(), id, store.UpdateAPIKeyParams{
		Name:               req.Name,
		Scopes:             req.Scopes,
		RateLimitPerMinute: req.RateLimitPerMinute,
		RateLimitPerHour:   req.RateLimitPerHour,
	})
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "API key not found")
			return
		}
		h.logger.Error().Err(err).Str("api_key_id", idStr).Msg("update api key")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "apikey.update", id.String(), existing, updated)

	apierr.WriteSuccess(w, http.StatusOK, toAPIKeyResponse(updated))
}

func (h *Handler) Rotate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid API key ID format")
		return
	}

	existing, err := h.apiKeyStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "API key not found")
			return
		}
		h.logger.Error().Err(err).Str("api_key_id", idStr).Msg("get api key for rotation")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	prefix, fullKey, keyHash, err := generateAPIKey()
	if err != nil {
		h.logger.Error().Err(err).Msg("generate api key for rotation")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	rotated, err := h.apiKeyStore.Rotate(r.Context(), id, prefix, keyHash)
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "API key not found")
			return
		}
		h.logger.Error().Err(err).Str("api_key_id", idStr).Msg("rotate api key")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	gracePeriodEnds := time.Now().Add(24 * time.Hour)

	h.createAuditEntry(r, "apikey.rotate", id.String(), existing, rotated)

	apierr.WriteSuccess(w, http.StatusOK, rotateResponse{
		ID:              rotated.ID.String(),
		Name:            rotated.Name,
		Prefix:          prefix,
		Key:             fullKey,
		GracePeriodEnds: gracePeriodEnds.Format(time.RFC3339),
	})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid API key ID format")
		return
	}

	existing, _ := h.apiKeyStore.GetByID(r.Context(), id)

	err = h.apiKeyStore.Revoke(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrAPIKeyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "API key not found")
			return
		}
		h.logger.Error().Err(err).Str("api_key_id", idStr).Msg("revoke api key")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "apikey.revoke", id.String(), existing, nil)

	w.WriteHeader(http.StatusNoContent)
}

func generateAPIKey() (prefix, fullKey, keyHash string, err error) {
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return "", "", "", fmt.Errorf("generate random bytes: %w", err)
	}

	prefix = hex.EncodeToString(secret[:2])[:2]
	secretHex := hex.EncodeToString(secret)
	fullKey = fmt.Sprintf("argus_%s_%s", prefix, secretHex)

	hash := sha256.Sum256([]byte(fullKey))
	keyHash = hex.EncodeToString(hash[:])

	return prefix, fullKey, keyHash, nil
}

func HashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

func ParseAPIKey(key string) (prefix, secret string, ok bool) {
	parts := strings.SplitN(key, "_", 3)
	if len(parts) != 3 || parts[0] != "argus" || len(parts[1]) < 2 || len(parts[2]) == 0 {
		return "", "", false
	}
	return parts[1], parts[2], true
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
		EntityType:    "api_key",
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
