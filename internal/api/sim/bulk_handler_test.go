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
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

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
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

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
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

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

func TestBulkStateChangeMissingSegmentID(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"target_state": "suspended",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkStateChangeInvalidState(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"segment_id":   uuid.New().String(),
		"target_state": "invalid_state",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkPolicyAssignMissingPolicyVersionID(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"segment_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/policy-assign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "policy_editor")
	req = req.WithContext(ctx)

	h.PolicyAssign(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkOperatorSwitchMissingFields(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body, _ := json.Marshal(map[string]interface{}{
		"segment_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/operator-switch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)

	h.OperatorSwitch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestBulkStateChangeInvalidJSON(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/state-change", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	h.StateChange(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestBulkImportEmptyCSV(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

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
