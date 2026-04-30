package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

// switchTenantRequest is the body for POST /api/v1/auth/switch-tenant.
type switchTenantRequest struct {
	TenantID string `json:"tenant_id"`
}

// switchTenantResponse returns the new JWT plus context flags the frontend
// needs to refresh its auth store and tenant chip.
type switchTenantResponse struct {
	JWT            string  `json:"jwt"`
	UserID         string  `json:"user_id"`
	HomeTenantID   string  `json:"home_tenant_id"`
	ActiveTenantID *string `json:"active_tenant_id,omitempty"`
	Role           string  `json:"role"`
}

// SwitchTenant mints a new JWT for the calling super_admin with an
// ActiveTenantID override. Every subsequent request using the new token
// sees the selected tenant as its effective tenant scope (except for the
// admin's home tenant, which remains in Claims.TenantID for audit use).
//
// Route: POST /api/v1/auth/switch-tenant
// Role:  super_admin (enforced at router layer)
// Audit: action=tenant.context_switched, entity_type=tenant, entity_id=<target>
func (h *Handler) SwitchTenant(w http.ResponseWriter, r *http.Request) {
	if h.userStore == nil || h.tenantStore == nil || h.jwtSecret == "" {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "tenant switch not configured")
		return
	}

	adminID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || adminID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "auth context required")
		return
	}
	homeTenantID, ok := r.Context().Value(apierr.HomeTenantIDKey).(uuid.UUID)
	if !ok || homeTenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "home tenant missing from auth context")
		return
	}
	role, _ := r.Context().Value(apierr.RoleKey).(string)
	if role != "super_admin" {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole, "tenant context switch requires super_admin")
		return
	}

	var req switchTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}
	targetID, err := uuid.Parse(req.TenantID)
	if err != nil || targetID == uuid.Nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "tenant_id is not a valid UUID")
		return
	}

	target, err := h.tenantStore.GetByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrTenantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "tenant not found")
			return
		}
		h.logger.Error().Err(err).Str("tenant_id", targetID.String()).Msg("switch_tenant: get tenant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to fetch tenant")
		return
	}
	if target.State != "active" {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeTenantSuspended, "target tenant is not active")
		return
	}

	// Keep the switched-token lifetime consistent with impersonate: 1 hour.
	// The frontend refreshes via standard /auth/refresh when the token nears
	// expiry, at which point a new access token WITHOUT an active tenant is
	// issued — this is intentional: the active tenant lives only on the
	// access token, not the refresh token, so a page reload or idle expiry
	// naturally returns the admin to System View.
	targetID2 := target.ID
	jwtStr, err := auth.GenerateSwitchedToken(h.jwtSecret, adminID, homeTenantID, &targetID2, role, time.Hour)
	if err != nil {
		h.logger.Error().Err(err).Msg("switch_tenant: generate token")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to issue token")
		return
	}

	if h.auditSvc != nil {
		afterData, _ := json.Marshal(map[string]string{
			"from_tenant_id":  homeTenantID.String(),
			"to_tenant_id":    target.ID.String(),
			"admin_user_id":   adminID.String(),
		})
		_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
			TenantID:   target.ID,
			UserID:     &adminID,
			Action:     "tenant.context_switched",
			EntityType: "tenant",
			EntityID:   target.ID.String(),
			AfterData:  afterData,
		})
	}

	activeID := target.ID.String()
	apierr.WriteSuccess(w, http.StatusOK, switchTenantResponse{
		JWT:            jwtStr,
		UserID:         adminID.String(),
		HomeTenantID:   homeTenantID.String(),
		ActiveTenantID: &activeID,
		Role:           role,
	})
}

// ExitTenantContext mints a new JWT with ActiveTenantID cleared so the
// admin returns to their home tenant / System View.
//
// Route: POST /api/v1/auth/exit-tenant-context
// Role:  super_admin
// Audit: action=tenant.context_exited, entity_type=tenant, entity_id=<from>
//
// Idempotent: safe to call even when no active tenant is set.
func (h *Handler) ExitTenantContext(w http.ResponseWriter, r *http.Request) {
	if h.userStore == nil || h.jwtSecret == "" {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "tenant switch not configured")
		return
	}

	adminID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || adminID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "auth context required")
		return
	}
	homeTenantID, ok := r.Context().Value(apierr.HomeTenantIDKey).(uuid.UUID)
	if !ok || homeTenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "home tenant missing from auth context")
		return
	}
	role, _ := r.Context().Value(apierr.RoleKey).(string)
	if role != "super_admin" {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole, "tenant context switch requires super_admin")
		return
	}

	// Capture the previously-active tenant (if any) for audit — reads from
	// the middleware-populated ActiveTenantIDKey rather than re-decoding JWT.
	var fromTenant string
	if active, ok := r.Context().Value(apierr.ActiveTenantIDKey).(uuid.UUID); ok && active != uuid.Nil {
		fromTenant = active.String()
	}

	jwtStr, err := auth.GenerateSwitchedToken(h.jwtSecret, adminID, homeTenantID, nil, role, time.Hour)
	if err != nil {
		h.logger.Error().Err(err).Msg("exit_tenant_context: generate token")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to issue token")
		return
	}

	if h.auditSvc != nil && fromTenant != "" {
		// Audit the exit under the tenant we were viewing — that's where the
		// action took place from the admin's perspective.
		fromUUID, _ := uuid.Parse(fromTenant)
		afterData, _ := json.Marshal(map[string]string{
			"from_tenant_id": fromTenant,
			"to_tenant_id":   homeTenantID.String(),
			"admin_user_id":  adminID.String(),
		})
		_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
			TenantID:   fromUUID,
			UserID:     &adminID,
			Action:     "tenant.context_exited",
			EntityType: "tenant",
			EntityID:   fromTenant,
			AfterData:  afterData,
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, switchTenantResponse{
		JWT:          jwtStr,
		UserID:       adminID.String(),
		HomeTenantID: homeTenantID.String(),
		Role:         role,
	})
}
