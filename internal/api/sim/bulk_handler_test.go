package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestBulkImportMissingFile(t *testing.T) {
	h := NewBulkHandler(nil, nil, zerolog.Nop())

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.Import(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestBulkImportNonCSVFile(t *testing.T) {
	h := NewBulkHandler(nil, nil, zerolog.Nop())

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "data.txt")
	part.Write([]byte("some data"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.Import(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkImportMissingColumns(t *testing.T) {
	h := NewBulkHandler(nil, nil, zerolog.Nop())

	csvContent := "iccid,imsi\n8990111234567890123,286010123456789\n"

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "import.csv")
	part.Write([]byte(csvContent))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.Import(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkImportEmptyCSV(t *testing.T) {
	h := NewBulkHandler(nil, nil, zerolog.Nop())

	csvContent := "iccid,imsi,msisdn,operator_code,apn_name\n"

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "import.csv")
	part.Write([]byte(csvContent))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.Import(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}
