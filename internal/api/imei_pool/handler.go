// Package imeipool implements the HTTP handlers for the three IMEI pool
// management endpoints (API-331 List, API-332 Add, API-333 Delete) and the
// bulk import and cross-pool lookup endpoints (API-334, API-335).
//
// bound_sims_count (API-331 include_bound_count): currently returns 0 for
// every entry. A per-entry COUNT of sims.bound_imei matching the entry's
// imei_or_tac requires either a join or N extra queries; performance
// optimisation is deferred. Reviewer: route as tech debt (D-NNN).
//
// API-335 Lookup — tech debt:
//   - bound_sims: simStore.ListByBoundIMEI is not implemented; returns empty [].
//     Route as D-NNN for SIMStore extension.
//   - history: imeiHistoryStore.ListByObservedIMEI is not implemented; returns empty [].
//     Route as D-NNN for IMEIHistoryStore extension.
package imeipool

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const maxImportSize = 10 << 20 // 10 MB

// maxImportRows is the per-request row cap for bulk IMEI pool imports.
// Declared as var (not const) so tests can shadow it to a small value.
var maxImportRows = 100_000

// jobCreator is the minimal interface for creating async jobs.
type jobCreator interface {
	Create(ctx context.Context, p store.CreateJobParams) (*store.Job, error)
}

// eventPublisher is the minimal interface for publishing to the event bus.
type eventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

var (
	fullIMEIRegexp = regexp.MustCompile(`^[0-9]{15}$`)
	tacRegexp      = regexp.MustCompile(`^[0-9]{8}$`)
)

// hasCSVInjectionPrefix returns true if s starts with a character that some
// spreadsheet apps interpret as a formula. Mirrors the worker-side guard in
// internal/job/imei_pool_import_worker.go (STORY-095 plan §Risks R4) so the
// single-entry Add path enforces the same rule as bulk import.
func hasCSVInjectionPrefix(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t':
		return true
	}
	return false
}

// Handler serves API-331, API-332, API-333, API-334, and API-335.
type Handler struct {
	poolStore *store.IMEIPoolStore
	simStore  interface{} // reserved for future bound_sims_count implementation
	jobStore  jobCreator
	eventBus  eventPublisher
	auditSvc  audit.Auditor
	logger    zerolog.Logger
}

// NewHandler constructs a Handler.
// simStore is accepted for future bound_sims_count wiring; it is not used today.
// jobStore and eventBus are required for API-334 (BulkImport).
func NewHandler(
	poolStore *store.IMEIPoolStore,
	simStore interface{},
	jobStore *store.JobStore,
	eventBus *bus.EventBus,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *Handler {
	h := &Handler{
		poolStore: poolStore,
		simStore:  simStore,
		auditSvc:  auditSvc,
		logger:    logger.With().Str("component", "imei_pool_handler").Logger(),
	}
	if jobStore != nil {
		h.jobStore = jobStore
	}
	if eventBus != nil {
		h.eventBus = eventBus
	}
	return h
}

// ── DTOs ──────────────────────────────────────────────────────────────────────

type poolEntryResponse struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	Pool             string  `json:"pool"`
	Kind             string  `json:"kind"`
	IMEIOrTAC        string  `json:"imei_or_tac"`
	DeviceModel      *string `json:"device_model"`
	Description      *string `json:"description"`
	QuarantineReason *string `json:"quarantine_reason,omitempty"`
	BlockReason      *string `json:"block_reason,omitempty"`
	ImportedFrom     *string `json:"imported_from,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	BoundSIMsCount   int     `json:"bound_sims_count"`
}

func toPoolEntryResponse(e store.PoolEntry) poolEntryResponse {
	return poolEntryResponse{
		ID:               e.ID.String(),
		TenantID:         e.TenantID.String(),
		Pool:             string(e.Pool),
		Kind:             e.Kind,
		IMEIOrTAC:        e.IMEIOrTAC,
		DeviceModel:      e.DeviceModel,
		Description:      e.Description,
		QuarantineReason: e.QuarantineReason,
		BlockReason:      e.BlockReason,
		ImportedFrom:     e.ImportedFrom,
		CreatedAt:        e.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:        e.UpdatedAt.Format(time.RFC3339Nano),
		BoundSIMsCount:   0, // deferred — see package doc
	}
}

// ── API-331: GET /api/v1/imei-pools/{kind} ────────────────────────────────────

// List handles API-331.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	kindStr := chi.URLParam(r, "kind")
	pool := store.PoolKind(kindStr)
	if !store.IsValidPoolKind(pool) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidPoolKind, "pool kind must be one of: whitelist, greylist, blacklist")
		return
	}

	q := r.URL.Query()

	limit := 50
	if ls := q.Get("limit"); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidParam, "limit must be a positive integer")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}

	params := store.ListParams{
		Cursor: q.Get("cursor"),
		Limit:  limit,
	}

	if tac := q.Get("tac"); tac != "" {
		if !tacRegexp.MatchString(tac) {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidTAC, "tac must be exactly 8 digits")
			return
		}
		params.TAC = &tac
	}

	if imei := q.Get("imei"); imei != "" {
		if !fullIMEIRegexp.MatchString(imei) {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidIMEI, "imei must be exactly 15 digits")
			return
		}
		params.IMEI = &imei
	}

	if dm := q.Get("device_model"); dm != "" {
		params.DeviceModel = &dm
	}

	entries, nextCursor, err := h.poolStore.List(r.Context(), tenantID, pool, params)
	if err != nil {
		h.logger.Error().Err(err).Str("pool", kindStr).Msg("list imei pool entries failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	items := make([]poolEntryResponse, len(entries))
	for i, e := range entries {
		items[i] = toPoolEntryResponse(e)
	}

	type listMeta struct {
		NextCursor string `json:"next_cursor"`
	}
	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
		Status: "success",
		Data:   items,
		Meta:   listMeta{NextCursor: nextCursor},
	})
}

// ── API-332: POST /api/v1/imei-pools/{kind} ───────────────────────────────────

// addEntryRequest is the POST body. Optional string fields use non-pointer
// json.RawMessage to distinguish "absent" from "explicit null" (PAT-031).
type addEntryRequest struct {
	Kind             string          `json:"kind"`
	IMEIOrTAC        string          `json:"imei_or_tac"`
	DeviceModel      json.RawMessage `json:"device_model"`
	Description      json.RawMessage `json:"description"`
	QuarantineReason json.RawMessage `json:"quarantine_reason"`
	BlockReason      json.RawMessage `json:"block_reason"`
	ImportedFrom     json.RawMessage `json:"imported_from"`
}

// decodeOptionalStringField mirrors device_binding_handler.go (PAT-031).
// len(raw)==0 → absent → (nil, false, nil)
// "null" → explicit null → (nil, true, nil)
// `"value"` → (&value, true, nil)
func decodeOptionalStringField(raw json.RawMessage) (*string, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false, err
	}
	return &s, true, nil
}

// Add handles API-332.
func (h *Handler) Add(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	kindStr := chi.URLParam(r, "kind")
	pool := store.PoolKind(kindStr)
	if !store.IsValidPoolKind(pool) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidPoolKind, "pool kind must be one of: whitelist, greylist, blacklist")
		return
	}

	var req addEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if !store.IsValidEntryKind(req.Kind) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidEntryKind, "kind must be one of: full_imei, tac_range")
		return
	}

	switch req.Kind {
	case store.EntryKindFullIMEI:
		if !fullIMEIRegexp.MatchString(req.IMEIOrTAC) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidIMEI, "imei_or_tac must be exactly 15 digits for full_imei entries")
			return
		}
	case store.EntryKindTACRange:
		if !tacRegexp.MatchString(req.IMEIOrTAC) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidTAC, "imei_or_tac must be exactly 8 digits for tac_range entries")
			return
		}
	}

	deviceModel, _, err := decodeOptionalStringField(req.DeviceModel)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid device_model value")
		return
	}

	description, _, err := decodeOptionalStringField(req.Description)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid description value")
		return
	}

	quarantineReason, _, err := decodeOptionalStringField(req.QuarantineReason)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid quarantine_reason value")
		return
	}

	blockReason, _, err := decodeOptionalStringField(req.BlockReason)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid block_reason value")
		return
	}

	importedFromVal, _, err := decodeOptionalStringField(req.ImportedFrom)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid imported_from value")
		return
	}

	// CSV-injection guard (parity with bulk worker — STORY-095 plan §Risks R4).
	// Reject any operator-supplied string that begins with =, +, -, @, or tab so
	// re-exported CSVs cannot smuggle spreadsheet formulas. Field-level rejection
	// returns 422 with CSV_INJECTION_REJECTED.
	for _, candidate := range []struct {
		name  string
		value *string
	}{
		{"device_model", deviceModel},
		{"description", description},
		{"quarantine_reason", quarantineReason},
		{"block_reason", blockReason},
		{"imported_from", importedFromVal},
	} {
		if candidate.value != nil && hasCSVInjectionPrefix(*candidate.value) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeCSVInjectionRejected,
				candidate.name+" cannot start with =, +, -, @, or tab")
			return
		}
	}

	// Pool-specific required-field validation (handler-level — returns typed
	// error codes before hitting the store's generic validation).
	switch pool {
	case store.PoolGreylist:
		if quarantineReason == nil || *quarantineReason == "" {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeMissingQuarantineReason, "quarantine_reason is required for greylist entries")
			return
		}
	case store.PoolBlacklist:
		if blockReason == nil || *blockReason == "" {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeMissingBlockReason, "block_reason is required for blacklist entries")
			return
		}
		if importedFromVal == nil || !store.IsValidImportedFrom(*importedFromVal) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidImportedFrom, "imported_from is required for blacklist entries and must be one of: manual, gsma_ceir, operator_eir")
			return
		}
	}

	userID := userIDFromCtx(r)

	entry, err := h.poolStore.Add(r.Context(), tenantID, pool, store.AddEntryParams{
		Kind:             req.Kind,
		IMEIOrTAC:        req.IMEIOrTAC,
		DeviceModel:      deviceModel,
		Description:      description,
		QuarantineReason: quarantineReason,
		BlockReason:      blockReason,
		ImportedFrom:     importedFromVal,
		CreatedBy:        userID,
	})
	if err != nil {
		if errors.Is(err, store.ErrPoolEntryDuplicate) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeIMEIPoolDuplicate, "An entry for this IMEI or TAC already exists in the pool")
			return
		}
		h.logger.Error().Err(err).Str("pool", kindStr).Msg("add imei pool entry failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	h.emitAddAudit(r, tenantID, entry, userID)

	apierr.WriteSuccess(w, http.StatusCreated, toPoolEntryResponse(*entry))
}

// ── API-333: DELETE /api/v1/imei-pools/{kind}/{id} ───────────────────────────

// Delete handles API-333.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	kindStr := chi.URLParam(r, "kind")
	pool := store.PoolKind(kindStr)
	if !store.IsValidPoolKind(pool) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidPoolKind, "pool kind must be one of: whitelist, greylist, blacklist")
		return
	}

	idStr := chi.URLParam(r, "id")
	entryID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid entry ID format")
		return
	}

	if err := h.poolStore.Delete(r.Context(), tenantID, pool, entryID); err != nil {
		if errors.Is(err, store.ErrPoolEntryNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodePoolEntryNotFound, "Pool entry not found")
			return
		}
		h.logger.Error().Err(err).Str("pool", kindStr).Str("entry_id", idStr).Msg("delete imei pool entry failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	userID := userIDFromCtx(r)
	h.emitDeleteAudit(r, tenantID, pool, entryID.String(), userID)

	w.WriteHeader(http.StatusNoContent)
}

// ── API-334: POST /api/v1/imei-pools/{kind}/import ────────────────────────────

// expectedImportHeaders lists the required CSV columns in order.
var expectedImportHeaders = []string{
	"imei_or_tac", "kind", "device_model", "description",
	"quarantine_reason", "block_reason", "imported_from",
}

// BulkImport handles API-334.
func (h *Handler) BulkImport(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	kindStr := chi.URLParam(r, "kind")
	pool := store.PoolKind(kindStr)
	if !store.IsValidPoolKind(pool) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidPoolKind, "pool kind must be one of: whitelist, greylist, blacklist")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)
	if err := r.ParseMultipartForm(maxImportSize); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"File too large or invalid multipart form. Max size: 10MB")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"Missing 'file' field in multipart form")
		return
	}
	defer file.Close()

	csvData, err := io.ReadAll(file)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"Failed to read uploaded file")
		return
	}

	if len(csvData) == 0 {
		apierr.WriteError(w, http.StatusBadRequest, "EMPTY_FILE", "CSV file is empty")
		return
	}

	reader := csv.NewReader(strings.NewReader(string(csvData)))
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
			"Invalid CSV: cannot read header row")
		return
	}

	normalized := make([]string, len(headers))
	for i, h := range headers {
		normalized[i] = strings.ToLower(strings.TrimSpace(h))
	}

	if len(normalized) != len(expectedImportHeaders) {
		apierr.WriteError(w, http.StatusBadRequest, "INVALID_CSV",
			"CSV header must contain exactly: imei_or_tac,kind,device_model,description,quarantine_reason,block_reason,imported_from")
		return
	}
	for i, col := range expectedImportHeaders {
		if normalized[i] != col {
			apierr.WriteError(w, http.StatusBadRequest, "INVALID_CSV",
				"CSV header column order is incorrect; expected: imei_or_tac,kind,device_model,description,quarantine_reason,block_reason,imported_from")
			return
		}
	}

	var rows []job.IMEIPoolImportRowSpec
	for {
		rec, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat,
				"CSV parse error: "+readErr.Error())
			return
		}
		if len(rows) >= maxImportRows {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "TOO_MANY_ROWS",
				"CSV exceeds maximum row limit of 100000")
			return
		}
		rows = append(rows, job.IMEIPoolImportRowSpec{
			IMEIOrTAC:        strings.TrimSpace(rec[0]),
			Kind:             strings.TrimSpace(rec[1]),
			DeviceModel:      strings.TrimSpace(rec[2]),
			Description:      strings.TrimSpace(rec[3]),
			QuarantineReason: strings.TrimSpace(rec[4]),
			BlockReason:      strings.TrimSpace(rec[5]),
			ImportedFrom:     strings.TrimSpace(rec[6]),
		})
	}

	if len(rows) == 0 {
		apierr.WriteError(w, http.StatusBadRequest, "EMPTY_FILE", "CSV file contains no data rows")
		return
	}

	if h.jobStore == nil {
		h.logger.Error().Msg("jobStore not configured on imei_pool Handler")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	userID := userIDFromCtx(r)
	userIDStr := ""
	if userID != nil {
		userIDStr = userID.String()
	}

	payloadJSON, _ := json.Marshal(job.BulkIMEIPoolImportPayload{
		TenantID: tenantID.String(),
		UserID:   userIDStr,
		Pool:     string(pool),
		Rows:     rows,
	})

	j, err := h.jobStore.Create(r.Context(), store.CreateJobParams{
		Type:       job.JobTypeBulkIMEIPoolImport,
		Priority:   5,
		Payload:    payloadJSON,
		TotalItems: len(rows),
		CreatedBy:  userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("pool", kindStr).Msg("create bulk imei pool import job failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to create import job")
		return
	}

	if h.eventBus != nil {
		_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
			JobID:    j.ID,
			TenantID: j.TenantID,
			Type:     job.JobTypeBulkIMEIPoolImport,
		})
	}

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data:   map[string]string{"job_id": j.ID.String()},
	})
}

// ── API-335: GET /api/v1/imei-pools/lookup ────────────────────────────────────

// lookupListEntry is one entry in the flattened "lists" array of the Lookup response.
type lookupListEntry struct {
	Kind       string `json:"kind"`
	EntryID    string `json:"entry_id"`
	MatchedVia string `json:"matched_via"`
}

// lookupResponse is the response body for API-335.
type lookupResponse struct {
	Lists     []lookupListEntry        `json:"lists"`
	BoundSIMs []map[string]interface{} `json:"bound_sims"`
	History   []map[string]interface{} `json:"history"`
}

// Lookup handles API-335.
func (h *Handler) Lookup(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	imei := r.URL.Query().Get("imei")
	if !fullIMEIRegexp.MatchString(imei) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidIMEI, "imei query param must be exactly 15 digits")
		return
	}

	result, err := h.poolStore.Lookup(r.Context(), tenantID, imei)
	if err != nil {
		h.logger.Error().Err(err).Str("imei", imei).Msg("imei pool lookup failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	lists := make([]lookupListEntry, 0)
	if result != nil {
		for _, m := range result.Whitelist {
			lists = append(lists, lookupListEntry{Kind: "whitelist", EntryID: m.EntryID.String(), MatchedVia: m.MatchedVia})
		}
		for _, m := range result.Greylist {
			lists = append(lists, lookupListEntry{Kind: "greylist", EntryID: m.EntryID.String(), MatchedVia: m.MatchedVia})
		}
		for _, m := range result.Blacklist {
			lists = append(lists, lookupListEntry{Kind: "blacklist", EntryID: m.EntryID.String(), MatchedVia: m.MatchedVia})
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, lookupResponse{
		Lists:     lists,
		BoundSIMs: []map[string]interface{}{},
		History:   []map[string]interface{}{},
	})
}

// ── audit helpers ─────────────────────────────────────────────────────────────

func (h *Handler) emitAddAudit(r *http.Request, tenantID uuid.UUID, entry *store.PoolEntry, userID *uuid.UUID) {
	if h.auditSvc == nil {
		return
	}
	type addPayload struct {
		Pool             string  `json:"pool"`
		Kind             string  `json:"kind"`
		IMEIOrTAC        string  `json:"imei_or_tac"`
		DeviceModel      *string `json:"device_model,omitempty"`
		Description      *string `json:"description,omitempty"`
		QuarantineReason *string `json:"quarantine_reason,omitempty"`
		BlockReason      *string `json:"block_reason,omitempty"`
		ImportedFrom     *string `json:"imported_from,omitempty"`
	}
	afterData, _ := json.Marshal(addPayload{
		Pool:             string(entry.Pool),
		Kind:             entry.Kind,
		IMEIOrTAC:        entry.IMEIOrTAC,
		DeviceModel:      entry.DeviceModel,
		Description:      entry.Description,
		QuarantineReason: entry.QuarantineReason,
		BlockReason:      entry.BlockReason,
		ImportedFrom:     entry.ImportedFrom,
	})

	ip := r.RemoteAddr
	ua := r.UserAgent()
	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        "imei_pool.entry_added",
		EntityType:    "imei_pool_entry",
		EntityID:      entry.ID.String(),
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Msg("imei pool add audit entry failed")
	}
}

func (h *Handler) emitDeleteAudit(r *http.Request, tenantID uuid.UUID, pool store.PoolKind, entryID string, userID *uuid.UUID) {
	if h.auditSvc == nil {
		return
	}
	// Before-payload is empty: the entry is deleted before we can fetch it.
	// The audit chain records the entry ID and pool kind in entity fields.
	// Tech debt: add GetByID to IMEIPoolStore to capture full before-state.
	type deletePayload struct {
		Pool string `json:"pool"`
	}
	beforeData, _ := json.Marshal(deletePayload{Pool: string(pool)})

	ip := r.RemoteAddr
	ua := r.UserAgent()
	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        "imei_pool.entry_removed",
		EntityType:    "imei_pool_entry",
		EntityID:      entryID,
		BeforeData:    beforeData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Msg("imei pool delete audit entry failed")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func userIDFromCtx(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
