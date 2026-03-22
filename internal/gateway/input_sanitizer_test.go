package gateway

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestSanitizeStringScriptTag(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "script tag removal",
			input: `{"name": "<script>alert('xss')</script>"}`,
			want:  `{"name": ""}`,
		},
		{
			name:  "javascript protocol removal",
			input: `{"url": "javascript:alert(1)"}`,
			want:  `{"url": "alert(1)"}`,
		},
		{
			name:  "html tag removal",
			input: `{"desc": "<b>bold</b> <i>italic</i>"}`,
			want:  `{"desc": "bold italic"}`,
		},
		{
			name:  "clean input unchanged",
			input: `{"name": "John Doe", "age": 30}`,
			want:  `{"name": "John Doe", "age": 30}`,
		},
		{
			name:  "nested script",
			input: `{"val": "<SCRIPT SRC=http://evil.com/xss.js></SCRIPT>"}`,
			want:  `{"val": ""}`,
		},
		{
			name:  "on event handler with actual quotes",
			input: `<div onclick="alert(1)">test</div>`,
			want:  `test`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeString(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInputSanitizerMiddleware(t *testing.T) {
	logger := zerolog.Nop()

	var receivedBody string
	handler := InputSanitizer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))

	body := `{"name": "<script>alert('xss')</script>test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if receivedBody != `{"name": "test"}` {
		t.Errorf("sanitized body = %q, expected script tags removed", receivedBody)
	}
}

func TestInputSanitizerSkipsNonJSON(t *testing.T) {
	logger := zerolog.Nop()

	var receivedBody string
	handler := InputSanitizer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))

	body := `<script>alert('xss')</script>`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "multipart/form-data")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if receivedBody != body {
		t.Errorf("non-JSON body should not be sanitized, got %q", receivedBody)
	}
}

func TestInputSanitizerGETPassthrough(t *testing.T) {
	logger := zerolog.Nop()

	called := false
	handler := InputSanitizer(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("GET request should pass through sanitizer")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestSanitizeValue(t *testing.T) {
	got := SanitizeValue("<script>bad</script>clean")
	want := "clean"
	if got != want {
		t.Errorf("SanitizeValue() = %q, want %q", got, want)
	}
}
