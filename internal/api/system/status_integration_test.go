package system

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/auth"
	"github.com/google/uuid"
)

// injectRole is a minimal auth middleware for integration tests that validates
// a bearer JWT and injects role/tenant/user into the request context.
// It mirrors gateway.JWTAuth + gateway.RequireRole without importing gateway
// (which would create an import cycle: gateway → api/system → gateway).
func injectRole(secret, requiredRole string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(hdr) <= len(prefix) {
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
				"Missing or invalid authorization header")
			return
		}
		tokenStr := hdr[len(prefix):]

		claims, err := auth.ValidateToken(tokenStr, secret)
		if err != nil {
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
				"Invalid authentication token")
			return
		}

		if !apierr.HasRole(claims.Role, requiredRole) {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
				"This action requires "+requiredRole+" role or higher")
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, apierr.TenantIDKey, claims.TenantID)
		ctx = context.WithValue(ctx, apierr.UserIDKey, claims.UserID)
		ctx = context.WithValue(ctx, apierr.RoleKey, claims.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// buildAdminJWT generates a super_admin JWT signed with the given secret.
func buildAdminJWT(t *testing.T, secret string) string {
	t.Helper()
	tok, err := auth.GenerateToken(secret, uuid.New(), uuid.New(), "super_admin", 15*time.Minute, false)
	if err != nil {
		t.Fatalf("buildAdminJWT: %v", err)
	}
	return "Bearer " + tok
}

// buildStatusRouter creates an httptest.Server that mounts:
//   - GET /api/v1/status         → StatusHandler.Serve (no auth)
//   - GET /api/v1/status/details → StatusHandler.ServeDetails (requires super_admin)
func buildStatusRouter(secret string, h *StatusHandler) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		h.Serve(w, r)
	})
	mux.Handle("/api/v1/status/details",
		injectRole(secret, "super_admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeDetails(w, r)
		})),
	)
	return httptest.NewServer(mux)
}

const integTestSecret = "integration-test-secret-must-be-at-least-32ch"

func TestStatusIntegration_PublicEndpointNoAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}

	h := NewStatusHandler(
		&mockHealthStatusChecker{state: "healthy", httpStatus: http.StatusOK},
		&mockTenantCounter{count: 3},
		"1.0.0", "abc1234", "2026-04-12T00:00:00Z",
	)
	srv := buildStatusRouter(integTestSecret, h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("GET /api/v1/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
		Data   struct {
			Service       string `json:"service"`
			Overall       string `json:"overall"`
			Version       string `json:"version"`
			Uptime        string `json:"uptime"`
			ActiveTenants int64  `json:"active_tenants"`
		} `json:"data"`
		Meta *json.RawMessage `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Status != "success" {
		t.Errorf("status = %q, want %q", body.Status, "success")
	}
	if body.Data.Service != "argus" {
		t.Errorf("service = %q, want %q", body.Data.Service, "argus")
	}
	if body.Data.Overall != "healthy" {
		t.Errorf("overall = %q, want %q", body.Data.Overall, "healthy")
	}
	if body.Data.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", body.Data.Version, "1.0.0")
	}
	if body.Data.ActiveTenants != 3 {
		t.Errorf("active_tenants = %d, want 3", body.Data.ActiveTenants)
	}
	if body.Meta != nil {
		t.Errorf("meta should be absent on public /api/v1/status, got: %s", *body.Meta)
	}
}

func TestStatusIntegration_DetailsWithAdminJWT(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}

	details := map[string]string{"db": "ok", "redis": "ok"}
	h := NewStatusHandler(
		&mockHealthStatusChecker{state: "healthy", httpStatus: http.StatusOK, details: details},
		&mockTenantCounter{count: 7},
		"2.0.0", "deadbeef", "2026-04-12T00:00:00Z",
	)
	srv := buildStatusRouter(integTestSecret, h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/status/details", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", buildAdminJWT(t, integTestSecret))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/status/details: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
		Data   struct {
			Overall       string `json:"overall"`
			ActiveTenants int64  `json:"active_tenants"`
		} `json:"data"`
		Meta struct {
			Details interface{} `json:"details"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Status != "success" {
		t.Errorf("status = %q, want %q", body.Status, "success")
	}
	if body.Data.Overall != "healthy" {
		t.Errorf("overall = %q, want %q", body.Data.Overall, "healthy")
	}
	if body.Data.ActiveTenants != 7 {
		t.Errorf("active_tenants = %d, want 7", body.Data.ActiveTenants)
	}
	if body.Meta.Details == nil {
		t.Error("meta.details should be present for admin /api/v1/status/details")
	}
}

func TestStatusIntegration_DetailsWithoutToken_401(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}

	h := NewStatusHandler(
		&mockHealthStatusChecker{state: "healthy", httpStatus: http.StatusOK},
		nil,
		"1.0.0", "sha", "2026-04-12T00:00:00Z",
	)
	srv := buildStatusRouter(integTestSecret, h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/status/details")
	if err != nil {
		t.Fatalf("GET /api/v1/status/details: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated details: status = %d, want 401", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode 401 body: %v", err)
	}
	if body.Status != "error" {
		t.Errorf("status = %q, want %q", body.Status, "error")
	}
}
