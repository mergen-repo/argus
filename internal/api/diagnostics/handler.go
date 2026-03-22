package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	diag "github.com/btopcu/argus/internal/diagnostics"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const cacheKeyPrefix = "diag:"
const cacheTTL = 1 * time.Minute

type Handler struct {
	diagService *diag.Service
	redisClient *redis.Client
	logger      zerolog.Logger
}

func NewHandler(diagService *diag.Service, redisClient *redis.Client, logger zerolog.Logger) *Handler {
	return &Handler{
		diagService: diagService,
		redisClient: redisClient,
		logger:      logger.With().Str("component", "diagnostics_handler").Logger(),
	}
}

type diagnoseRequest struct {
	IncludeTestAuth *bool `json:"include_test_auth"`
}

func (h *Handler) Diagnose(w http.ResponseWriter, r *http.Request) {
	tenantIDVal := r.Context().Value(apierr.TenantIDKey)
	tenantID, ok := tenantIDVal.(uuid.UUID)
	if !ok {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid tenant context")
		return
	}

	simIDStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(simIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid sim id")
		return
	}

	var req diagnoseRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
			return
		}
	}

	includeTestAuth := false
	if req.IncludeTestAuth != nil {
		includeTestAuth = *req.IncludeTestAuth
	}

	cacheKey := fmt.Sprintf("%s%s:%s:%v", cacheKeyPrefix, tenantID.String(), simID.String(), includeTestAuth)

	if h.redisClient != nil {
		cached, cacheErr := h.redisClient.Get(r.Context(), cacheKey).Bytes()
		if cacheErr == nil && len(cached) > 0 {
			var result diag.DiagnosticResult
			if err := json.Unmarshal(cached, &result); err == nil {
				apierr.WriteSuccess(w, http.StatusOK, result)
				return
			}
		}
	}

	result, err := h.diagService.Diagnose(r.Context(), tenantID, simID, includeTestAuth)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simID.String()).Msg("diagnostics failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "diagnostics failed")
		return
	}

	if h.redisClient != nil {
		if encoded, err := json.Marshal(result); err == nil {
			if setErr := h.redisClient.Set(r.Context(), cacheKey, encoded, cacheTTL).Err(); setErr != nil {
				h.logger.Warn().Err(setErr).Msg("failed to cache diagnostic result")
			}
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, result)
}
