package system

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockHealthStatusChecker struct {
	state      string
	httpStatus int
	details    interface{}
}

func (m *mockHealthStatusChecker) CheckStatus(_ context.Context) (string, int, interface{}) {
	return m.state, m.httpStatus, m.details
}

type mockTenantCounter struct {
	count int64
	err   error
}

func (m *mockTenantCounter) CountActive(_ context.Context) (int64, error) {
	return m.count, m.err
}

func TestStatusHandler_Serve(t *testing.T) {
	tests := []struct {
		name           string
		healthState    string
		healthHTTP     int
		details        interface{}
		tenantCount    int64
		wantHTTP       int
		wantOverall    string
		wantTenants    int64
		wantVersion    string
		wantUptimeGt0  bool
	}{
		{
			name:        "all ok returns healthy 200",
			healthState: "healthy",
			healthHTTP:  http.StatusOK,
			tenantCount: 5,
			wantHTTP:    http.StatusOK,
			wantOverall: "healthy",
			wantTenants: 5,
			wantVersion: "1.2.3",
		},
		{
			name:        "degraded returns degraded 200",
			healthState: "degraded",
			healthHTTP:  http.StatusOK,
			tenantCount: 10,
			wantHTTP:    http.StatusOK,
			wantOverall: "degraded",
			wantTenants: 10,
			wantVersion: "1.2.3",
		},
		{
			name:        "db down returns unhealthy 503",
			healthState: "unhealthy",
			healthHTTP:  http.StatusServiceUnavailable,
			tenantCount: 0,
			wantHTTP:    http.StatusServiceUnavailable,
			wantOverall: "unhealthy",
			wantTenants: 0,
			wantVersion: "1.2.3",
		},
		{
			name:          "uptime is non-empty",
			healthState:   "healthy",
			healthHTTP:    http.StatusOK,
			tenantCount:   42,
			wantHTTP:      http.StatusOK,
			wantOverall:   "healthy",
			wantTenants:   42,
			wantVersion:   "1.2.3",
			wantUptimeGt0: true,
		},
		{
			name:        "active tenants reflected in body",
			healthState: "healthy",
			healthHTTP:  http.StatusOK,
			tenantCount: 99,
			wantHTTP:    http.StatusOK,
			wantOverall: "healthy",
			wantTenants: 99,
			wantVersion: "1.2.3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewStatusHandler(
				&mockHealthStatusChecker{
					state:      tc.healthState,
					httpStatus: tc.healthHTTP,
					details:    tc.details,
				},
				&mockTenantCounter{count: tc.tenantCount},
				nil,
				"1.2.3", "abc1234", "2026-04-12T10:00:00Z",
			)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
			rec := httptest.NewRecorder()
			h.Serve(rec, req)

			if rec.Code != tc.wantHTTP {
				t.Errorf("HTTP status = %d, want %d", rec.Code, tc.wantHTTP)
			}

			var resp struct {
				Status string `json:"status"`
				Data   struct {
					Service       string `json:"service"`
					Overall       string `json:"overall"`
					Version       string `json:"version"`
					Uptime        string `json:"uptime"`
					ActiveTenants int64  `json:"active_tenants"`
					RecentError5m int64  `json:"recent_error_5m"`
				} `json:"data"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if resp.Status != "success" {
				t.Errorf("status = %q, want %q", resp.Status, "success")
			}
			if resp.Data.Service != "argus" {
				t.Errorf("service = %q, want %q", resp.Data.Service, "argus")
			}
			if resp.Data.Overall != tc.wantOverall {
				t.Errorf("overall = %q, want %q", resp.Data.Overall, tc.wantOverall)
			}
			if resp.Data.Version != tc.wantVersion {
				t.Errorf("version = %q, want %q", resp.Data.Version, tc.wantVersion)
			}
			if resp.Data.ActiveTenants != tc.wantTenants {
				t.Errorf("active_tenants = %d, want %d", resp.Data.ActiveTenants, tc.wantTenants)
			}
			if resp.Data.RecentError5m != 0 {
				t.Errorf("recent_error_5m = %d, want 0", resp.Data.RecentError5m)
			}
			if tc.wantUptimeGt0 && resp.Data.Uptime == "" {
				t.Error("uptime should be non-empty")
			}
		})
	}
}

type mockErrSrc struct{ count int64 }

func (m *mockErrSrc) Recent5xxCount() int64 { return m.count }

func TestStatusHandler_RecentError5m(t *testing.T) {
	t.Run("no error source wired — returns 0", func(t *testing.T) {
		h := NewStatusHandler(
			&mockHealthStatusChecker{state: "healthy", httpStatus: http.StatusOK},
			&mockTenantCounter{count: 1},
			nil,
			"1.0.0", "sha", "2026-04-12T00:00:00Z",
		)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		rec := httptest.NewRecorder()
		h.Serve(rec, req)

		var resp struct {
			Data struct {
				RecentError5m int64 `json:"recent_error_5m"`
			} `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Data.RecentError5m != 0 {
			t.Errorf("recent_error_5m = %d, want 0", resp.Data.RecentError5m)
		}
	})

	t.Run("with error source — returns live count on /api/v1/status", func(t *testing.T) {
		h := NewStatusHandler(
			&mockHealthStatusChecker{state: "healthy", httpStatus: http.StatusOK},
			&mockTenantCounter{count: 1},
			&mockErrSrc{count: 42},
			"1.0.0", "sha", "2026-04-12T00:00:00Z",
		)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		rec := httptest.NewRecorder()
		h.Serve(rec, req)

		var resp struct {
			Data struct {
				RecentError5m int64 `json:"recent_error_5m"`
			} `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Data.RecentError5m != 42 {
			t.Errorf("recent_error_5m = %d, want 42", resp.Data.RecentError5m)
		}
	})

	t.Run("with error source — returns live count on /api/v1/status/details", func(t *testing.T) {
		h := NewStatusHandler(
			&mockHealthStatusChecker{state: "healthy", httpStatus: http.StatusOK, details: map[string]string{"db": "ok"}},
			&mockTenantCounter{count: 1},
			&mockErrSrc{count: 7},
			"1.0.0", "sha", "2026-04-12T00:00:00Z",
		)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status/details", nil)
		rec := httptest.NewRecorder()
		h.ServeDetails(rec, req)

		var resp struct {
			Data struct {
				RecentError5m int64 `json:"recent_error_5m"`
			} `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Data.RecentError5m != 7 {
			t.Errorf("recent_error_5m = %d, want 7", resp.Data.RecentError5m)
		}
	})
}

func TestStatusHandler_ServeDetails(t *testing.T) {
	type detailsPayload struct {
		State string `json:"state"`
	}

	h := NewStatusHandler(
		&mockHealthStatusChecker{
			state:      "healthy",
			httpStatus: http.StatusOK,
			details:    detailsPayload{State: "healthy"},
		},
		&mockTenantCounter{count: 7},
		nil,
		"2.0.0", "deadbeef", "2026-04-12T00:00:00Z",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status/details", nil)
	rec := httptest.NewRecorder()
	h.ServeDetails(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Status string `json:"status"`
		Data   struct {
			Overall       string `json:"overall"`
			ActiveTenants int64  `json:"active_tenants"`
		} `json:"data"`
		Meta struct {
			Details interface{} `json:"details"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("status = %q, want %q", resp.Status, "success")
	}
	if resp.Data.Overall != "healthy" {
		t.Errorf("overall = %q, want %q", resp.Data.Overall, "healthy")
	}
	if resp.Data.ActiveTenants != 7 {
		t.Errorf("active_tenants = %d, want 7", resp.Data.ActiveTenants)
	}
	if resp.Meta.Details == nil {
		t.Error("meta.details should be present in admin route")
	}
}

func TestStatusHandler_ServeDetails_Unhealthy503(t *testing.T) {
	h := NewStatusHandler(
		&mockHealthStatusChecker{
			state:      "unhealthy",
			httpStatus: http.StatusServiceUnavailable,
			details:    map[string]string{"db": "error"},
		},
		&mockTenantCounter{count: 0},
		nil,
		"1.0.0", "sha123", "2026-04-12T00:00:00Z",
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status/details", nil)
	rec := httptest.NewRecorder()
	h.ServeDetails(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("HTTP status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var resp struct {
		Data struct {
			Overall string `json:"overall"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Overall != "unhealthy" {
		t.Errorf("overall = %q, want unhealthy", resp.Data.Overall)
	}
}
