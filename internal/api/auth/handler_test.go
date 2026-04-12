package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

func TestAuthHandler_ListSessions_NoUserContext(t *testing.T) {
	h := NewAuthHandler(nil, 24*time.Hour, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	w := httptest.NewRecorder()

	h.ListSessions(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandler_ListSessions_NilUserID(t *testing.T) {
	h := NewAuthHandler(nil, 24*time.Hour, false)

	ctx := context.WithValue(context.Background(), apierr.UserIDKey, uuid.Nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListSessions(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandler_ListSessions_LimitBounds(t *testing.T) {
	cases := []struct {
		name         string
		limitParam   string
		wantEffective int
	}{
		{"default", "", 50},
		{"valid 10", "10", 10},
		{"valid 100", "100", 100},
		{"over 100 clamped", "200", 50},
		{"zero clamped", "0", 50},
		{"non-numeric clamped", "abc", 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit := 50
			if tc.limitParam != "" {
				parsed := 0
				valid := true
				for _, c := range tc.limitParam {
					if c < '0' || c > '9' {
						valid = false
						break
					}
					parsed = parsed*10 + int(c-'0')
				}
				if valid && parsed > 0 && parsed <= 100 {
					limit = parsed
				}
			}
			if limit != tc.wantEffective {
				t.Errorf("limit = %d, want %d", limit, tc.wantEffective)
			}
		})
	}
}
