package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestResolveIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		setupCtx   func(r *http.Request) *http.Request
		remoteAddr string
		wantPrefix string
	}{
		{
			name: "api key auth",
			setupCtx: func(r *http.Request) *http.Request {
				ctx := r.Context()
				ctx = context.WithValue(ctx, apierr.AuthTypeKey, "api_key")
				ctx = context.WithValue(ctx, apierr.APIKeyIDKey, "key-123")
				return r.WithContext(ctx)
			},
			wantPrefix: "apikey:",
		},
		{
			name: "jwt auth",
			setupCtx: func(r *http.Request) *http.Request {
				ctx := r.Context()
				ctx = context.WithValue(ctx, apierr.AuthTypeKey, "jwt")
				ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.MustParse("11111111-1111-1111-1111-111111111111"))
				ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.MustParse("22222222-2222-2222-2222-222222222222"))
				return r.WithContext(ctx)
			},
			wantPrefix: "user:",
		},
		{
			name: "unauthenticated falls back to IP",
			setupCtx: func(r *http.Request) *http.Request {
				return r
			},
			remoteAddr: "192.168.1.1:12345",
			wantPrefix: "ip:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr
			}
			req = tt.setupCtx(req)

			result := resolveIdentifier(req)
			if len(result) < len(tt.wantPrefix) || result[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("resolveIdentifier() = %q, want prefix %q", result, tt.wantPrefix)
			}
		})
	}
}

func TestResolveLimits(t *testing.T) {
	defaultMin := 1000
	defaultHour := 30000

	t.Run("defaults when no context values", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		perMin, perHour := resolveLimits(req, defaultMin, defaultHour)
		if perMin != defaultMin {
			t.Errorf("perMin = %d, want %d", perMin, defaultMin)
		}
		if perHour != defaultHour {
			t.Errorf("perHour = %d, want %d", perHour, defaultHour)
		}
	})

	t.Run("custom limits from context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := req.Context()
		ctx = context.WithValue(ctx, RateLimitPerMinuteKey, 500)
		ctx = context.WithValue(ctx, RateLimitPerHourKey, 10000)
		req = req.WithContext(ctx)

		perMin, perHour := resolveLimits(req, defaultMin, defaultHour)
		if perMin != 500 {
			t.Errorf("perMin = %d, want 500", perMin)
		}
		if perHour != 10000 {
			t.Errorf("perHour = %d, want 10000", perHour)
		}
	})
}

func TestRateLimiterSkipsHealth(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimiter(nil, 1000, 30000, logger)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called for /api/health (rate limiter skipped)")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRateLimiterNilRedisPassthrough(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimiter(nil, 1000, 30000, logger)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called when Redis is nil (fail-open)")
	}
}

func TestSetRateLimitHeaders(t *testing.T) {
	rr := httptest.NewRecorder()
	result := &RateLimitResult{
		Allowed:   true,
		Remaining: 742,
		ResetAt:   1710770520,
		Limit:     1000,
	}

	setRateLimitHeaders(rr, result)

	if got := rr.Header().Get("X-RateLimit-Limit"); got != "1000" {
		t.Errorf("X-RateLimit-Limit = %q, want %q", got, "1000")
	}
	if got := rr.Header().Get("X-RateLimit-Remaining"); got != "742" {
		t.Errorf("X-RateLimit-Remaining = %q, want %q", got, "742")
	}
	if got := rr.Header().Get("X-RateLimit-Reset"); got != "1710770520" {
		t.Errorf("X-RateLimit-Reset = %q, want %q", got, "1710770520")
	}
}

func TestWriteRateLimitResponse(t *testing.T) {
	rr := httptest.NewRecorder()
	result := &RateLimitResult{
		Allowed:   false,
		Remaining: 0,
		ResetAt:   9999999999,
		Limit:     1000,
	}

	writeRateLimitResponse(rr, result, "per_minute")

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}

	retryAfter := rr.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Retry-After header should be set")
	}

	if got := rr.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("X-RateLimit-Remaining = %q, want %q", got, "0")
	}

	var resp struct {
		Status string `json:"status"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := decodeJSON(rr, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("status = %q, want %q", resp.Status, "error")
	}
	if resp.Error.Code != "RATE_LIMITED" {
		t.Errorf("error code = %q, want %q", resp.Error.Code, "RATE_LIMITED")
	}
}

func TestIsRedisNil(t *testing.T) {
	if isRedisNil(nil) {
		t.Error("nil error should not be redis nil")
	}
}

func TestHasScopeAccess(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		required string
		want     bool
	}{
		{"wildcard allows all", []string{"*"}, "sims:read", true},
		{"exact match", []string{"sims:read"}, "sims:read", true},
		{"resource wildcard", []string{"sims:*"}, "sims:read", true},
		{"resource wildcard write", []string{"sims:*"}, "sims:write", true},
		{"no match", []string{"cdrs:read"}, "sims:read", false},
		{"empty scopes", []string{}, "sims:read", false},
		{"different resource", []string{"cdrs:*"}, "sims:read", false},
		{"multiple scopes match", []string{"cdrs:read", "sims:read"}, "sims:read", true},
		{"partial resource no match", []string{"sim:read"}, "sims:read", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasScopeAccess(tt.scopes, tt.required)
			if got != tt.want {
				t.Errorf("hasScopeAccess(%v, %q) = %v, want %v", tt.scopes, tt.required, got, tt.want)
			}
		})
	}
}

func decodeJSON(rr *httptest.ResponseRecorder, v interface{}) error {
	return json.NewDecoder(rr.Body).Decode(v)
}
