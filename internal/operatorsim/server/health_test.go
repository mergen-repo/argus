package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/operatorsim/config"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

func TestHealthHandler(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())

	tests := []struct {
		name     string
		operator string
		wantCode int
	}{
		{"turkcell health", "turkcell", http.StatusOK},
		{"vodafone_tr health", "vodafone_tr", http.StatusOK},
		{"vodafone health", "vodafone", http.StatusOK},
		{"turk_telekom health", "turk_telekom", http.StatusOK},
		{"mock health", "mock", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/"+tt.operator+"/health", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("operator", tt.operator)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			w := httptest.NewRecorder()
			srv.healthHandler(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", w.Code, tt.wantCode)
			}

			var body struct {
				Operator  string    `json:"operator"`
				Status    string    `json:"status"`
				Timestamp time.Time `json:"timestamp"`
			}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Operator != tt.operator {
				t.Errorf("operator = %q, want %q", body.Operator, tt.operator)
			}
			if body.Status != "ok" {
				t.Errorf("status = %q, want ok", body.Status)
			}
			if time.Since(body.Timestamp) > 2*time.Second {
				t.Errorf("timestamp %v is not recent (>2s ago)", body.Timestamp)
			}
		})
	}
}

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Listen:        ":9595",
			MetricsListen: ":9596",
			ReadTimeout:   config.DefaultReadTimeout,
			WriteTimeout:  config.DefaultWriteTimeout,
		},
		Operators: []config.OperatorEntry{
			{Code: "turkcell", DisplayName: "Turkcell"},
			{Code: "vodafone_tr", DisplayName: "Vodafone TR"},
			{Code: "vodafone", DisplayName: "Vodafone TR"},
			{Code: "turk_telekom", DisplayName: "Turk Telekom"},
			{Code: "mock", DisplayName: "Mock Operator"},
		},
		Log: config.LogConfig{Level: "info", Format: "console"},
		Stubs: config.StubsConfig{
			SubscriberStatus: "active",
			SubscriberPlan:   "default",
			CDREcho:          true,
		},
	}
}
