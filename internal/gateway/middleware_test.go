package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/rs/zerolog"
)

func TestZerologRequestLogger_StructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Str("service", "argus").Logger()

	handler := CorrelationID()(
		ZerologRequestLogger(logger)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status":"success"}`))
			}),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	requiredFields := []string{"level", "correlation_id", "method", "path", "status", "duration_ms", "bytes", "service"}
	for _, field := range requiredFields {
		if _, ok := logEntry[field]; !ok {
			t.Errorf("missing required field %q in log entry: %s", field, buf.String())
		}
	}

	if logEntry["method"] != "GET" {
		t.Errorf("expected method GET, got %v", logEntry["method"])
	}
	if logEntry["path"] != "/api/health" {
		t.Errorf("expected path /api/health, got %v", logEntry["path"])
	}
	if logEntry["level"] != "info" {
		t.Errorf("expected level info for 200, got %v", logEntry["level"])
	}
}

func TestZerologRequestLogger_WarnOn4xx(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	handler := CorrelationID()(
		ZerologRequestLogger(logger)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("log output is not valid JSON: %v", err)
	}
	if logEntry["level"] != "warn" {
		t.Errorf("expected level warn for 404, got %v", logEntry["level"])
	}
}

func TestZerologRequestLogger_ErrorOn5xx(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	handler := CorrelationID()(
		ZerologRequestLogger(logger)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("log output is not valid JSON: %v", err)
	}
	if logEntry["level"] != "error" {
		t.Errorf("expected level error for 500, got %v", logEntry["level"])
	}
}

func TestRecoveryWithZerolog_CatchesPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	handler := CorrelationID()(
		RecoveryWithZerolog(logger)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("test panic")
			}),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}

	var resp apierr.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp.Error.Code != apierr.CodeInternalError {
		t.Errorf("expected error code %s, got %s", apierr.CodeInternalError, resp.Error.Code)
	}

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("log output is not valid JSON: %v", err)
	}
	if logEntry["panic"] != "test panic" {
		t.Errorf("expected panic field in log, got %v", logEntry["panic"])
	}
	if _, ok := logEntry["stack"]; !ok {
		t.Error("expected stack field in log")
	}
}

func TestCorrelationID_PropagatesFromMiddlewareToHandler(t *testing.T) {
	var handlerID string

	handler := CorrelationID()(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerID = GetCorrelationID(r.Context())
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	headerID := rec.Header().Get("X-Request-ID")
	if handlerID == "" || headerID == "" {
		t.Fatal("expected both context and header correlation IDs")
	}
	if handlerID != headerID {
		t.Errorf("handler ID %q != header ID %q", handlerID, headerID)
	}
}

func TestCorrelationID_AppearsInLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	var handlerID string
	handler := CorrelationID()(
		ZerologRequestLogger(logger)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerID = GetCorrelationID(r.Context())
				w.WriteHeader(http.StatusOK)
			}),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("log output is not valid JSON: %v", err)
	}

	logID, _ := logEntry["correlation_id"].(string)
	if logID != handlerID {
		t.Errorf("log correlation_id %q != handler correlation_id %q", logID, handlerID)
	}
}
