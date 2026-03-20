package msisdn

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	msisdns *store.MSISDNStore
	logger  zerolog.Logger
}

func NewHandler(msisdns *store.MSISDNStore, logger zerolog.Logger) *Handler {
	return &Handler{
		msisdns: msisdns,
		logger:  logger,
	}
}

type msisdnDTO struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"tenant_id"`
	OperatorID    uuid.UUID  `json:"operator_id"`
	MSISDN        string     `json:"msisdn"`
	State         string     `json:"state"`
	SimID         *uuid.UUID `json:"sim_id,omitempty"`
	ReservedUntil *string    `json:"reserved_until,omitempty"`
	CreatedAt     string     `json:"created_at"`
}

type importRequest struct {
	OperatorID uuid.UUID `json:"operator_id"`
	MSISDNs    []struct {
		MSISDN       string `json:"msisdn"`
		OperatorCode string `json:"operator_code"`
	} `json:"msisdns"`
}

type assignRequest struct {
	SimID uuid.UUID `json:"sim_id"`
}

const timeFmt = "2006-01-02T15:04:05Z07:00"

func toDTO(m *store.MSISDN) msisdnDTO {
	dto := msisdnDTO{
		ID:         m.ID,
		TenantID:   m.TenantID,
		OperatorID: m.OperatorID,
		MSISDN:     m.MSISDN,
		State:      m.State,
		SimID:      m.SimID,
		CreatedAt:  m.CreatedAt.Format(timeFmt),
	}
	if m.ReservedUntil != nil {
		v := m.ReservedUntil.Format(timeFmt)
		dto.ReservedUntil = &v
	}
	return dto
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	state := r.URL.Query().Get("state")
	operatorIDStr := r.URL.Query().Get("operator_id")

	limit := 20
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	var operatorID *uuid.UUID
	if operatorIDStr != "" {
		if id, err := uuid.Parse(operatorIDStr); err == nil {
			operatorID = &id
		}
	}

	if state != "" {
		validStates := map[string]bool{"available": true, "assigned": true, "reserved": true}
		if !validStates[state] {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
				[]map[string]interface{}{{"field": "state", "message": "State must be one of: available, assigned, reserved", "code": "invalid_enum"}})
			return
		}
	}

	results, nextCursor, err := h.msisdns.List(r.Context(), cursor, limit, state, operatorID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list msisdn pool")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]msisdnDTO, 0, len(results))
	for i := range results {
		items = append(items, toDTO(&results[i]))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		h.importCSV(w, r)
		return
	}

	h.importJSON(w, r)
}

func (h *Handler) importJSON(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]interface{}
	if req.OperatorID == uuid.Nil {
		validationErrors = append(validationErrors, map[string]interface{}{"field": "operator_id", "message": "Operator ID is required", "code": "required"})
	}
	if len(req.MSISDNs) == 0 {
		validationErrors = append(validationErrors, map[string]interface{}{"field": "msisdns", "message": "At least one MSISDN is required", "code": "required"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	rows := make([]store.MSISDNImportRow, len(req.MSISDNs))
	for i, m := range req.MSISDNs {
		rows[i] = store.MSISDNImportRow{
			MSISDN:       m.MSISDN,
			OperatorCode: m.OperatorCode,
		}
	}

	result, err := h.msisdns.BulkImport(r.Context(), req.OperatorID, rows)
	if err != nil {
		h.logger.Error().Err(err).Msg("import msisdns")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, result)
}

func (h *Handler) importCSV(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Failed to parse multipart form")
		return
	}

	operatorIDStr := r.FormValue("operator_id")
	operatorID, err := uuid.Parse(operatorIDStr)
	if err != nil || operatorID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "operator_id", "message": "Valid operator ID is required", "code": "required"}})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "CSV file is required")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)

	header, err := reader.Read()
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Failed to read CSV header")
		return
	}

	msisdnCol := -1
	operatorCodeCol := -1
	for i, col := range header {
		switch strings.TrimSpace(strings.ToLower(col)) {
		case "msisdn":
			msisdnCol = i
		case "operator_code":
			operatorCodeCol = i
		}
	}

	if msisdnCol == -1 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "CSV must contain a 'msisdn' column")
		return
	}

	var rows []store.MSISDNImportRow
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			continue
		}

		row := store.MSISDNImportRow{
			MSISDN: strings.TrimSpace(record[msisdnCol]),
		}
		if operatorCodeCol >= 0 && operatorCodeCol < len(record) {
			row.OperatorCode = strings.TrimSpace(record[operatorCodeCol])
		}
		if row.MSISDN != "" {
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "file", "message": "CSV contains no valid MSISDN rows", "code": "required"}})
		return
	}

	result, importErr := h.msisdns.BulkImport(r.Context(), operatorID, rows)
	if importErr != nil {
		h.logger.Error().Err(importErr).Msg("import msisdns csv")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, result)
}

func (h *Handler) Assign(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid MSISDN pool entry ID")
		return
	}

	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.SimID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "sim_id", "message": "SIM ID is required", "code": "required"}})
		return
	}

	m, err := h.msisdns.Assign(r.Context(), id, req.SimID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeMSISDNNotFound, "MSISDN not found")
			return
		}
		if errors.Is(err, store.ErrMSISDNNotAvailable) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeMSISDNNotAvailable, "MSISDN is not available for assignment")
			return
		}
		h.logger.Error().Err(err).Msg("assign msisdn")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toDTO(m))
}
