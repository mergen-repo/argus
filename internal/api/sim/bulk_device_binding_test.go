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
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func makeDeviceBindingsRequest(t *testing.T, filename, csvBody string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if filename != "" {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		part.Write([]byte(csvBody))
	}
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/device-bindings", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, apierr.RoleKey, "sim_manager")
	req = req.WithContext(ctx)

	return httptest.NewRecorder(), req
}

func TestDeviceBindingsCSVMissingFile(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/device-bindings", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New().String())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.DeviceBindingsCSV(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestDeviceBindingsCSVNonCSVFile(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())
	w, req := makeDeviceBindingsRequest(t, "data.txt", "iccid,bound_imei,binding_mode\n")
	h.DeviceBindingsCSV(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestDeviceBindingsCSVMissingColumns(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())
	w, req := makeDeviceBindingsRequest(t, "bindings.csv", "iccid,bound_imei\n8990111234567890123,490154203237518\n")
	h.DeviceBindingsCSV(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestDeviceBindingsCSVEmptyRows(t *testing.T) {
	h := NewBulkHandler(nil, nil, nil, zerolog.Nop())
	w, req := makeDeviceBindingsRequest(t, "bindings.csv", "iccid,bound_imei,binding_mode\n")
	h.DeviceBindingsCSV(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestDeviceBindingsCSVEnqueuesJob(t *testing.T) {
	tenantID := uuid.New()
	jobs := &fakeJobCreator{created: &store.Job{ID: uuid.New(), TenantID: tenantID}}
	publisher := &fakeEventPublisher{}
	h := newBulkHandlerWithFakes(jobs, nil, nil, publisher)

	csvBody := "iccid,bound_imei,binding_mode\n8990111234567890123,490154203237518,strict\n8990111234567890124,,soft\n"
	w, req := makeDeviceBindingsRequest(t, "bindings.csv", csvBody)
	req = req.WithContext(context.WithValue(req.Context(), apierr.TenantIDKey, tenantID))
	h.DeviceBindingsCSV(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d; body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	if jobs.lastParams.Type != job.JobTypeBulkDeviceBindings {
		t.Errorf("job type = %q, want %q", jobs.lastParams.Type, job.JobTypeBulkDeviceBindings)
	}
	if jobs.lastParams.TotalItems != 2 {
		t.Errorf("total items = %d, want 2", jobs.lastParams.TotalItems)
	}

	var payload job.BulkDeviceBindingsPayload
	if err := json.Unmarshal(jobs.lastParams.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(payload.Rows))
	}
	if payload.Rows[0].ICCID != "8990111234567890123" {
		t.Errorf("row[0].ICCID = %q, want 8990111234567890123", payload.Rows[0].ICCID)
	}
	if payload.Rows[0].BindingMode != "strict" {
		t.Errorf("row[0].BindingMode = %q, want strict", payload.Rows[0].BindingMode)
	}
	if payload.Rows[1].BoundIMEI != "" {
		t.Errorf("row[1].BoundIMEI = %q, want empty", payload.Rows[1].BoundIMEI)
	}

	var respBody apierr.SuccessResponse
	json.NewDecoder(w.Body).Decode(&respBody)
	if respBody.Status != "success" {
		t.Errorf("response status = %q, want success", respBody.Status)
	}
}
