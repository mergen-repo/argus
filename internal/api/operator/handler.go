package operator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/analytics/aggregates"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/crypto"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/btopcu/argus/internal/operator/adapterschema"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// STORY-090 Wave 2 D2-B: the legacy `validAdapterTypes` map (and the
// `adapter_type` request/response field it gated) was REMOVED in this
// wave along with the column drop. Callers that previously sent a
// flat adapter_config with an adapter_type hint must now either:
//   - send a nested post-090 body (canonical), or
//   - send a flat body whose keys disambiguate the protocol via the
//     adapterschema.DetectShape heuristics (e.g. "shared_secret" →
//     RADIUS, "origin_host" → Diameter).
// The `adapter_type` hint is no longer accepted.

var validFailoverPolicies = map[string]bool{
	"reject":             true,
	"fallback_to_next":   true,
	"queue_with_timeout": true,
}

var validOperatorStates = map[string]bool{
	"active":   true,
	"disabled": true,
}

type Handler struct {
	operatorStore   *store.OperatorStore
	tenantStore     *store.TenantStore
	simStore        *store.SIMStore
	sessionStore    *store.RadiusSessionStore
	cdrStore        *store.CDRStore
	auditSvc        audit.Auditor
	encryptionKey   string
	adapterRegistry *adapter.Registry
	agg             aggregates.Aggregates
	logger          zerolog.Logger
}

type HandlerOption func(*Handler)

func WithSIMStore(s *store.SIMStore) HandlerOption {
	return func(h *Handler) { h.simStore = s }
}

func WithSessionStore(s *store.RadiusSessionStore) HandlerOption {
	return func(h *Handler) { h.sessionStore = s }
}

func WithCDRStore(cs *store.CDRStore) HandlerOption {
	return func(h *Handler) { h.cdrStore = cs }
}

func WithAggregates(a aggregates.Aggregates) HandlerOption {
	return func(h *Handler) { h.agg = a }
}

func NewHandler(
	operatorStore *store.OperatorStore,
	tenantStore *store.TenantStore,
	auditSvc audit.Auditor,
	encryptionKey string,
	adapterRegistry *adapter.Registry,
	logger zerolog.Logger,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		operatorStore:   operatorStore,
		tenantStore:     tenantStore,
		auditSvc:        auditSvc,
		encryptionKey:   encryptionKey,
		adapterRegistry: adapterRegistry,
		logger:          logger.With().Str("component", "operator_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type operatorResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
	MCC  string `json:"mcc"`
	MNC  string `json:"mnc"`
	// EnabledProtocols is derived from the nested adapter_config and
	// replaces the legacy `adapter_type` string. Callers that only
	// need the canonical primary protocol should pick the first
	// element (canonical order is diameter, radius, sba, http, mock).
	// An empty list signals "operator has no enabled protocols" —
	// visible in UI but non-routable.
	EnabledProtocols []string `json:"enabled_protocols"`
	// AdapterConfig carries the decrypted, nested, secrets-MASKED
	// adapter_config. STORY-090 Gate (F-A2): previously omitted from
	// the response; added so the Protocols tab can reflect stored
	// state on first render (AC-4 / AC-5). Only populated for
	// Create/Update/Detail responses — List responses leave it nil to
	// keep payloads slim per plan §API Specifications.
	AdapterConfig             json.RawMessage `json:"adapter_config,omitempty"`
	SupportedRATTypes         []string        `json:"supported_rat_types"`
	HealthStatus              string          `json:"health_status"`
	CircuitState              string          `json:"circuit_state"` // FIX-308: live CB state ('closed' / 'half_open' / 'open')
	HealthCheckIntervalSec    int             `json:"health_check_interval_sec"`
	FailoverPolicy            string          `json:"failover_policy"`
	FailoverTimeoutMs         int             `json:"failover_timeout_ms"`
	CircuitBreakerThreshold   int             `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverySec int             `json:"circuit_breaker_recovery_sec"`
	SLAUptimeTarget           *float64        `json:"sla_uptime_target"`
	SLALatencyThresholdMs     int             `json:"sla_latency_threshold_ms"`
	State                     string          `json:"state"`
	CreatedAt                 string          `json:"created_at"`
	UpdatedAt                 string          `json:"updated_at"`
	SimCount                  int             `json:"sim_count"`
	ActiveSessions            int64           `json:"active_sessions"`
	TotalTrafficBytes         int64           `json:"total_traffic_bytes"`
	LastHealthCheck           *string         `json:"last_health_check"`
}

type grantResponse struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	OperatorID string  `json:"operator_id"`
	Enabled    bool    `json:"enabled"`
	GrantedAt  string  `json:"granted_at"`
	GrantedBy  *string `json:"granted_by,omitempty"`
}

type healthResponse struct {
	HealthStatus string  `json:"health_status"`
	LatencyMs    *int    `json:"latency_ms"`
	CircuitState string  `json:"circuit_state"`
	LastCheck    *string `json:"last_check"`
	Uptime24h    float64 `json:"uptime_24h"`
	FailureCount int     `json:"failure_count"`
}

type testResponse struct {
	Success   bool   `json:"success"`
	LatencyMs int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type createOperatorRequest struct {
	Name string `json:"name"`
	Code string `json:"code"`
	MCC  string `json:"mcc"`
	MNC  string `json:"mnc"`
	// AdapterType removed in STORY-090 Wave 2 D2-B — callers must
	// supply a nested adapter_config (or a heuristic-classifiable
	// flat body). The adapter_type hint is no longer accepted.
	AdapterConfig             json.RawMessage `json:"adapter_config"`
	SupportedRATTypes         []string        `json:"supported_rat_types"`
	FailoverPolicy            *string         `json:"failover_policy"`
	FailoverTimeoutMs         *int            `json:"failover_timeout_ms"`
	CircuitBreakerThreshold   *int            `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverySec *int            `json:"circuit_breaker_recovery_sec"`
	HealthCheckIntervalSec    *int            `json:"health_check_interval_sec"`
	SLAUptimeTarget           *float64        `json:"sla_uptime_target"`
	SMDPPlusURL               *string         `json:"sm_dp_plus_url"`
	SMDPPlusConfig            json.RawMessage `json:"sm_dp_plus_config"`
}

type updateOperatorRequest struct {
	Name                      *string         `json:"name"`
	AdapterConfig             json.RawMessage `json:"adapter_config"`
	SupportedRATTypes         []string        `json:"supported_rat_types"`
	FailoverPolicy            *string         `json:"failover_policy"`
	FailoverTimeoutMs         *int            `json:"failover_timeout_ms"`
	CircuitBreakerThreshold   *int            `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverySec *int            `json:"circuit_breaker_recovery_sec"`
	HealthCheckIntervalSec    *int            `json:"health_check_interval_sec"`
	SLAUptimeTarget           *float64        `json:"sla_uptime_target,omitempty"`
	SLALatencyThresholdMs     *int            `json:"sla_latency_threshold_ms,omitempty"`
	SMDPPlusURL               *string         `json:"sm_dp_plus_url"`
	SMDPPlusConfig            json.RawMessage `json:"sm_dp_plus_config"`
	State                     *string         `json:"state"`
}

type createGrantRequest struct {
	TenantID          string   `json:"tenant_id"`
	OperatorID        string   `json:"operator_id"`
	SupportedRATTypes []string `json:"supported_rat_types"`
}

// normalizeIncomingAdapterConfig takes a caller-supplied plaintext
// adapter_config blob (either nested post-090 or legacy flat pre-090)
// together with the optional adapter_type hint and returns the
// canonical nested JSON ready for encryption+persist. Rejects
// malformed or unknown-shape inputs with ErrShapeInvalidJSON /
// ErrUpConvertMissingHint. Rejects validation failures with
// ErrValidation. The returned bytes are always the nested plaintext
// JSON (adapter_config never goes into the DB without this
// normalization step in Wave 1).
func normalizeIncomingAdapterConfig(raw json.RawMessage, hint string) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	n, err := adapterschema.UpConvert(raw, hint)
	if err != nil {
		return nil, err
	}
	if err := adapterschema.Validate(n); err != nil {
		return nil, err
	}
	return adapterschema.MarshalNested(n)
}

// decryptAndNormalize reads an operator row, decrypts its
// adapter_config, detects the shape, and lazily rewrites legacy flat
// rows to the nested shape (D1-A). Returns the plaintext nested JSON
// for downstream consumers (TestConnection, HealthChecker, detail DTO).
// The re-persist is best-effort: if the UPDATE fails, the read path
// still succeeds with the in-memory nested config — the next call
// will try again. Side-effect logs a structured line on up-convert.
func (h *Handler) decryptAndNormalize(ctx context.Context, op *store.Operator) (json.RawMessage, error) {
	raw := op.AdapterConfig
	if len(raw) == 0 {
		return raw, nil
	}
	plaintext := raw
	if h.encryptionKey != "" {
		decrypted, err := crypto.DecryptJSON(raw, h.encryptionKey)
		if err != nil {
			// Corrupted envelope / key mismatch — surface as typed
			// error so the caller can distinguish from validation.
			return nil, err
		}
		plaintext = decrypted
	}
	shape, detectErr := adapterschema.DetectShape(plaintext)
	if detectErr != nil && detectErr != adapterschema.ErrShapeUnknown {
		// ShapeUnknown with a non-empty hint is recoverable via
		// UpConvert below; ShapeInvalidJSON is terminal.
		return nil, detectErr
	}
	if shape == adapterschema.ShapeNested {
		// Already nested — parse to confirm structure, but no rewrite.
		return plaintext, nil
	}
	// Legacy flat or unknown-with-hint: up-convert and re-persist.
	// Hint is empty post-Wave-2 (D2-B removed the column) — UpConvert
	// tolerates empty hints for heuristic-classifiable shapes.
	n, err := adapterschema.UpConvert(plaintext, "")
	if err != nil {
		return nil, err
	}
	nested, err := adapterschema.MarshalNested(n)
	if err != nil {
		return nil, err
	}
	h.logger.Info().
		Str("op", "adapter_config_upconvert").
		Str("operator_id", op.ID.String()).
		Str("old_shape", shape.String()).
		Str("new_shape", "nested").
		Msg("upconverted legacy adapter_config to nested shape")
	// Best-effort re-persist: re-encrypt (if key set), issue a
	// scoped UPDATE. Failure is logged but does NOT propagate — the
	// in-memory nested config is still usable by the caller.
	toPersist := nested
	if h.encryptionKey != "" {
		enc, encErr := crypto.EncryptJSON(nested, h.encryptionKey)
		if encErr == nil {
			toPersist = enc
		} else {
			h.logger.Warn().Err(encErr).Str("operator_id", op.ID.String()).Msg("re-encrypt upconverted adapter_config failed; skipping re-persist")
			return nested, nil
		}
	}
	if _, upErr := h.operatorStore.Update(ctx, op.ID, store.UpdateOperatorParams{AdapterConfig: toPersist}); upErr != nil {
		h.logger.Warn().Err(upErr).Str("operator_id", op.ID.String()).Msg("re-persist upconverted adapter_config failed; continuing with in-memory value")
	}
	return nested, nil
}

// toOperatorResponse builds the JSON response shape for an Operator.
// STORY-090 Wave 2 D2-B: enabledProtocols replaces the legacy
// `adapter_type` string. Callers that have already decrypted +
// normalized the adapter_config pass the derived enabled-protocol
// list; callers operating on the raw DB row (e.g. List / ExportCSV
// paths that do NOT decrypt per row to minimise cost) pass nil and
// the field defaults to an empty array.
func toOperatorResponse(o *store.Operator, enabledProtocols []string) operatorResponse {
	rats := o.SupportedRATTypes
	if rats == nil {
		rats = []string{}
	}
	protos := enabledProtocols
	if protos == nil {
		protos = []string{}
	}
	return operatorResponse{
		ID:                        o.ID.String(),
		Name:                      o.Name,
		Code:                      o.Code,
		MCC:                       o.MCC,
		MNC:                       o.MNC,
		EnabledProtocols:          protos,
		SupportedRATTypes:         rats,
		HealthStatus:              o.HealthStatus,
		CircuitState:              o.CircuitState, // FIX-308 extension
		HealthCheckIntervalSec:    o.HealthCheckIntervalSec,
		FailoverPolicy:            o.FailoverPolicy,
		FailoverTimeoutMs:         o.FailoverTimeoutMs,
		CircuitBreakerThreshold:   o.CircuitBreakerThreshold,
		CircuitBreakerRecoverySec: o.CircuitBreakerRecoverySec,
		SLAUptimeTarget:           o.SLAUptimeTarget,
		SLALatencyThresholdMs:     o.SLALatencyThresholdMs,
		State:                     o.State,
		CreatedAt:                 o.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:                 o.UpdatedAt.Format(time.RFC3339Nano),
	}
}

// maskedSecretSentinel is the value emitted in GET responses for any
// recognised secret field in the nested adapter_config. Plan §API
// Specifications: "Secrets handling: shared_secret, auth_token,
// client_key, etc. are ALWAYS masked in GET responses (return "****"
// or null)".
//
// PATCH semantic: a client that fetches the masked config, edits one
// non-secret field, and PATCHes the whole body back would overwrite
// real secrets with "****" without special handling. The incoming-
// normalizer below detects the sentinel and restores the stored
// plaintext value — so the masked value round-trips safely. Setting
// a secret explicitly requires sending a DIFFERENT string.
const maskedSecretSentinel = "****"

// secretFieldNames lists every sub-blob key that the backend treats as
// a secret. Both the mask path (outbound) and the PATCH preserve path
// (inbound) branch on this set. Keep in sync with plan §API
// Specifications > Secrets handling and §Screen Mockups (form inputs
// marked "masked").
var secretFieldNames = map[string]struct{}{
	"shared_secret": {},
	"auth_token":    {},
	"bearer_token":  {},
	"basic_pass":    {},
	"password":      {},
	"client_key":    {},
	"client_secret": {},
	"api_key":       {},
}

// maskAdapterConfig returns a copy of the nested plaintext
// adapter_config with every recognised secret field replaced by
// `maskedSecretSentinel`. On any parse failure returns nil + the
// error — callers emit the omitempty-nil variant so the response
// stays JSON-valid. Does NOT mutate the input.
func maskAdapterConfig(nested json.RawMessage) (json.RawMessage, error) {
	if len(nested) == 0 {
		return nil, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(nested, &top); err != nil {
		return nil, err
	}
	for protocol, sub := range top {
		var subMap map[string]interface{}
		if err := json.Unmarshal(sub, &subMap); err != nil {
			continue
		}
		for field := range secretFieldNames {
			if v, ok := subMap[field]; ok {
				// Mask non-empty values; leave empty/null untouched so
				// "not configured yet" distinguishes from "redacted".
				if s, isStr := v.(string); isStr && s != "" {
					subMap[field] = maskedSecretSentinel
				} else if v != nil && !isStr {
					subMap[field] = maskedSecretSentinel
				}
			}
		}
		remarshaled, err := json.Marshal(subMap)
		if err != nil {
			return nil, err
		}
		top[protocol] = remarshaled
		_ = protocol
	}
	out, err := json.Marshal(top)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// containsMaskedSecretSentinel scans the incoming body for any secret
// field that equals the mask sentinel. Used to refuse a PATCH when
// the stored config cannot be decrypted (see Update handler).
func containsMaskedSecretSentinel(incoming json.RawMessage) bool {
	if len(incoming) == 0 {
		return false
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(incoming, &top); err != nil {
		return false
	}
	for _, sub := range top {
		var subMap map[string]interface{}
		if err := json.Unmarshal(sub, &subMap); err != nil {
			continue
		}
		for field := range secretFieldNames {
			if v, ok := subMap[field].(string); ok && v == maskedSecretSentinel {
				return true
			}
		}
	}
	return false
}

// restoreMaskedSecrets returns a copy of `incoming` with any masked
// secret field (value == maskedSecretSentinel) replaced by the
// stored decrypted value from `stored`. Used on PATCH to avoid
// wiping real secrets when a client sends back a previously-fetched
// masked body. Both arguments are nested plaintext.
//
// If `stored` lacks a secret the incoming masked value maps to, the
// sentinel stays — the server-side validator may accept "****" as a
// literal secret (adapter factory's concern). If both stored and
// incoming lack a field, no-op.
func restoreMaskedSecrets(incoming, stored json.RawMessage) (json.RawMessage, error) {
	if len(incoming) == 0 {
		return incoming, nil
	}
	var incomingTop map[string]json.RawMessage
	if err := json.Unmarshal(incoming, &incomingTop); err != nil {
		return nil, err
	}
	var storedTop map[string]json.RawMessage
	if len(stored) > 0 {
		_ = json.Unmarshal(stored, &storedTop)
	}
	for protocol, sub := range incomingTop {
		var subMap map[string]interface{}
		if err := json.Unmarshal(sub, &subMap); err != nil {
			continue
		}
		var storedSubMap map[string]interface{}
		if storedTop != nil {
			if storedSub, ok := storedTop[protocol]; ok {
				_ = json.Unmarshal(storedSub, &storedSubMap)
			}
		}
		mutated := false
		for field := range secretFieldNames {
			v, ok := subMap[field]
			if !ok {
				continue
			}
			s, isStr := v.(string)
			if !isStr || s != maskedSecretSentinel {
				continue
			}
			if storedSubMap == nil {
				continue
			}
			storedVal, storedHas := storedSubMap[field]
			if !storedHas {
				continue
			}
			subMap[field] = storedVal
			mutated = true
		}
		if mutated {
			remarshaled, err := json.Marshal(subMap)
			if err != nil {
				return nil, err
			}
			incomingTop[protocol] = remarshaled
		}
	}
	return json.Marshal(incomingTop)
}

// resolveNestedAdapterConfigForResponse decrypts + normalizes the
// stored adapter_config and returns the nested plaintext + derived
// enabled-protocol list. Returns nil + empty slice if the op has no
// stored config or decryption fails — callers emit an empty response.
// Used by Create/Update/Get responses.
func (h *Handler) resolveNestedAdapterConfigForResponse(ctx context.Context, op *store.Operator) (json.RawMessage, []string) {
	if len(op.AdapterConfig) == 0 {
		return nil, nil
	}
	nested, err := h.decryptAndNormalize(ctx, op)
	if err != nil {
		return nil, nil
	}
	parsed, err := adapterschema.ParseNested(nested)
	if err != nil {
		return nested, nil
	}
	return nested, adapterschema.DeriveEnabledProtocols(parsed)
}

// deriveEnabledProtocolsFromStored best-effort decrypts the op row's
// encrypted adapter_config and returns the enabled-protocol list. On
// any error (corrupted envelope / malformed JSON) returns an empty
// slice — the caller treats it as "no enabled protocols".
func (h *Handler) deriveEnabledProtocolsFromStored(o *store.Operator) []string {
	if len(o.AdapterConfig) == 0 {
		return nil
	}
	plaintext := o.AdapterConfig
	if h.encryptionKey != "" {
		if decrypted, err := crypto.DecryptJSON(plaintext, h.encryptionKey); err == nil {
			plaintext = decrypted
		} else {
			return nil
		}
	}
	n, err := adapterschema.ParseNested(plaintext)
	if err != nil {
		// Legacy flat — try heuristic up-convert; the up-converted
		// shape's single enabled protocol becomes the primary.
		up, upErr := adapterschema.UpConvert(plaintext, "")
		if upErr != nil {
			return nil
		}
		return adapterschema.DeriveEnabledProtocols(up)
	}
	return adapterschema.DeriveEnabledProtocols(n)
}

func toGrantResponse(g *store.OperatorGrant) grantResponse {
	resp := grantResponse{
		ID:         g.ID.String(),
		TenantID:   g.TenantID.String(),
		OperatorID: g.OperatorID.String(),
		Enabled:    g.Enabled,
		GrantedAt:  g.GrantedAt.Format(time.RFC3339Nano),
	}
	if g.GrantedBy != nil {
		s := g.GrantedBy.String()
		resp.GrantedBy = &s
	}
	return resp
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	stateFilter := r.URL.Query().Get("state")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	operators, nextCursor, err := h.operatorStore.List(r.Context(), cursor, limit, stateFilter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list operators")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	ctx := r.Context()
	tenantID, _ := ctx.Value(apierr.TenantIDKey).(uuid.UUID)
	role, _ := ctx.Value(apierr.RoleKey).(string)

	if role != "super_admin" && tenantID != uuid.Nil {
		grants, gErr := h.operatorStore.ListGrants(ctx, tenantID)
		if gErr != nil {
			h.logger.Warn().Err(gErr).Msg("list grants for operator filter")
		} else {
			allowed := make(map[uuid.UUID]bool, len(grants))
			for _, g := range grants {
				allowed[g.OperatorID] = true
			}
			filtered := operators[:0]
			for _, o := range operators {
				if allowed[o.ID] {
					filtered = append(filtered, o)
				}
			}
			operators = filtered
			nextCursor = ""
		}
	}

	var simCounts map[uuid.UUID]int
	if h.agg != nil && tenantID != uuid.Nil {
		simCounts, err = h.agg.SIMCountByOperator(ctx, tenantID)
		if err != nil {
			h.logger.Warn().Err(err).Msg("count sims by operator")
		}
	}

	var sessionCounts map[string]int64
	var trafficMap map[uuid.UUID]int64
	if h.agg != nil && tenantID != uuid.Nil {
		if stats, err2 := h.agg.ActiveSessionStats(ctx, tenantID); err2 == nil {
			sessionCounts = stats.ByOperator
		} else {
			h.logger.Warn().Err(err2).Msg("get session stats for operator list")
		}
		trafficMap, err = h.agg.TrafficByOperator(ctx, tenantID)
		if err != nil {
			h.logger.Warn().Err(err).Msg("get traffic by operator")
		}
	}

	healthTimes, err := h.operatorStore.LatestHealthByOperator(ctx)
	if err != nil {
		h.logger.Warn().Err(err).Msg("get latest health times by operator")
	}

	items := make([]operatorResponse, 0, len(operators))
	for _, o := range operators {
		resp := toOperatorResponse(&o, h.deriveEnabledProtocolsFromStored(&o))
		if simCounts != nil {
			resp.SimCount = simCounts[o.ID]
		}
		if sessionCounts != nil {
			resp.ActiveSessions = sessionCounts[o.ID.String()]
		}
		if trafficMap != nil {
			resp.TotalTrafficBytes = trafficMap[o.ID]
		}
		if healthTimes != nil {
			if t, ok := healthTimes[o.ID]; ok {
				ts := t.Format(time.RFC3339Nano)
				resp.LastHealthCheck = &ts
			}
		}
		items = append(items, resp)
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createOperatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name is required", "code": "required"})
	}
	if req.Code == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "code", "message": "Code is required", "code": "required"})
	}
	if req.MCC == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "mcc", "message": "MCC is required", "code": "required"})
	} else if len(req.MCC) != 3 {
		validationErrors = append(validationErrors, map[string]string{"field": "mcc", "message": "MCC must be 3 digits", "code": "format"})
	}
	if req.MNC == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "mnc", "message": "MNC is required", "code": "required"})
	} else if len(req.MNC) < 2 || len(req.MNC) > 3 {
		validationErrors = append(validationErrors, map[string]string{"field": "mnc", "message": "MNC must be 2-3 digits", "code": "format"})
	}
	// STORY-090 Wave 2 D2-B: adapter_type validation removed. Create
	// rejects a request that omits adapter_config entirely (there's
	// nothing to persist) but otherwise relies on the adapterschema
	// normalize path below to classify / validate the shape.
	if len(req.AdapterConfig) == 0 {
		validationErrors = append(validationErrors, map[string]string{"field": "adapter_config", "message": "adapter_config is required", "code": "required"})
	}
	if req.FailoverPolicy != nil && !validFailoverPolicies[*req.FailoverPolicy] {
		validationErrors = append(validationErrors, map[string]string{"field": "failover_policy", "message": "Invalid failover policy. Allowed: reject, fallback_to_next, queue_with_timeout", "code": "invalid_enum"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	// Normalize adapter_config to the canonical nested shape (D1-A +
	// D2-B: Wave 2 drops the adapter_type hint, relying on
	// heuristic-based classification for legacy flat bodies).
	// Accepts either a nested post-090 body OR a flat body whose
	// top-level keys match the adapterschema heuristic set
	// (shared_secret → RADIUS, origin_host → Diameter, etc.).
	adapterConfig := req.AdapterConfig
	if len(adapterConfig) > 0 {
		normalized, normErr := normalizeIncomingAdapterConfig(adapterConfig, "")
		if normErr != nil {
			var field string
			msg := "Invalid adapter_config shape"
			switch {
			case errors.Is(normErr, adapterschema.ErrShapeInvalidJSON):
				field = "adapter_config"
				msg = "adapter_config is not valid JSON"
			case errors.Is(normErr, adapterschema.ErrUpConvertMissingHint):
				field = "adapter_config"
				msg = "adapter_config shape is ambiguous — use nested post-090 keys (radius/diameter/sba/http/mock)"
			case errors.Is(normErr, adapterschema.ErrValidation):
				field = "adapter_config"
				msg = normErr.Error()
			default:
				field = "adapter_config"
				msg = "adapter_config could not be normalized: " + normErr.Error()
			}
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				"Request validation failed",
				[]map[string]string{{"field": field, "message": msg, "code": "invalid_shape"}})
			return
		}
		adapterConfig = normalized
	}
	if adapterConfig != nil && len(adapterConfig) > 0 && h.encryptionKey != "" {
		encrypted, err := crypto.EncryptJSON(adapterConfig, h.encryptionKey)
		if err != nil {
			h.logger.Error().Err(err).Msg("encrypt adapter config")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		adapterConfig = encrypted
	}

	smDPConfig := req.SMDPPlusConfig
	if smDPConfig != nil && len(smDPConfig) > 0 && h.encryptionKey != "" {
		encrypted, err := crypto.EncryptJSON(smDPConfig, h.encryptionKey)
		if err != nil {
			h.logger.Error().Err(err).Msg("encrypt sm-dp+ config")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		smDPConfig = encrypted
	}

	o, err := h.operatorStore.Create(r.Context(), store.CreateOperatorParams{
		Name:                      req.Name,
		Code:                      req.Code,
		MCC:                       req.MCC,
		MNC:                       req.MNC,
		AdapterConfig:             adapterConfig,
		SMDPPlusURL:               req.SMDPPlusURL,
		SMDPPlusConfig:            smDPConfig,
		SupportedRATTypes:         req.SupportedRATTypes,
		FailoverPolicy:            req.FailoverPolicy,
		FailoverTimeoutMs:         req.FailoverTimeoutMs,
		CircuitBreakerThreshold:   req.CircuitBreakerThreshold,
		CircuitBreakerRecoverySec: req.CircuitBreakerRecoverySec,
		HealthCheckIntervalSec:    req.HealthCheckIntervalSec,
		SLAUptimeTarget:           req.SLAUptimeTarget,
	})
	if err != nil {
		if errors.Is(err, store.ErrOperatorCodeExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"An operator with this code or name already exists",
				[]map[string]string{{"field": "code", "value": req.Code}})
			return
		}
		if errors.Is(err, store.ErrMCCMNCExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"An operator with this MCC+MNC combination already exists",
				[]map[string]string{{"field": "mcc", "value": req.MCC}, {"field": "mnc", "value": req.MNC}})
			return
		}
		h.logger.Error().Err(err).Msg("create operator")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "operator.create", o.ID.String(), nil, o)

	resp := toOperatorResponse(o, h.deriveEnabledProtocolsFromStored(o))
	if nested, _ := h.resolveNestedAdapterConfigForResponse(r.Context(), o); len(nested) > 0 {
		if masked, mErr := maskAdapterConfig(nested); mErr == nil {
			resp.AdapterConfig = masked
		}
	}
	apierr.WriteSuccess(w, http.StatusCreated, resp)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	var req updateOperatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	role, _ := r.Context().Value(apierr.RoleKey).(string)
	if req.SLAUptimeTarget != nil || req.SLALatencyThresholdMs != nil {
		if !apierr.HasRole(role, "operator_manager") {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
				"This action requires operator_manager role or higher")
			return
		}
	}

	var validationErrors []map[string]string
	if req.FailoverPolicy != nil && !validFailoverPolicies[*req.FailoverPolicy] {
		validationErrors = append(validationErrors, map[string]string{"field": "failover_policy", "message": "Invalid failover policy", "code": "invalid_enum"})
	}
	if req.State != nil && !validOperatorStates[*req.State] {
		validationErrors = append(validationErrors, map[string]string{"field": "state", "message": "Invalid state. Allowed: active, disabled", "code": "invalid_enum"})
	}
	if req.SLAUptimeTarget != nil && (*req.SLAUptimeTarget < 50.0 || *req.SLAUptimeTarget > 100.0) {
		apierr.WriteError(w, http.StatusBadRequest, "invalid_sla_target",
			"sla_uptime_target must be between 50.0 and 100.0 inclusive")
		return
	}
	if req.SLALatencyThresholdMs != nil && (*req.SLALatencyThresholdMs < 50 || *req.SLALatencyThresholdMs > 60000) {
		apierr.WriteError(w, http.StatusBadRequest, "invalid_latency_threshold",
			"sla_latency_threshold_ms must be between 50 and 60000 inclusive")
		return
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	existing, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	// Normalize incoming adapter_config the same way Create does.
	// STORY-090 Wave 2 D2-B: the adapter_type hint is no longer
	// available — callers must send either a nested body or a flat
	// body whose keys are heuristically classifiable.
	//
	// STORY-090 Gate (F-A2): PATCH round-trip safety. Clients fetch
	// the detail GET which returns masked secrets (sentinel "****").
	// If the client PATCHes the whole body back with that sentinel,
	// we must NOT overwrite the stored plaintext secret with "****".
	// restoreMaskedSecrets splices the stored plaintext back in for
	// any sentinel-valued secret field before validation.
	adapterConfig := req.AdapterConfig
	if len(adapterConfig) > 0 {
		// Decrypt stored config once so we can restore masked values.
		// If the incoming body contains a masked sentinel BUT the stored
		// config could not be decrypted, refuse the PATCH — otherwise
		// the literal "****" would be accepted by the schema validator
		// and silently become the operator's real secret.
		storedNested, dErr := h.decryptAndNormalize(r.Context(), existing)
		if dErr != nil || len(storedNested) == 0 {
			if containsMaskedSecretSentinel(adapterConfig) {
				apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
					"Cannot PATCH adapter_config with masked secrets when stored config is undecryptable — re-supply real values",
					[]map[string]string{{"field": "adapter_config", "code": "masked_sentinel_without_stored"}})
				return
			}
		} else {
			if restored, rErr := restoreMaskedSecrets(adapterConfig, storedNested); rErr == nil {
				adapterConfig = restored
			}
		}
		normalized, normErr := normalizeIncomingAdapterConfig(adapterConfig, "")
		if normErr != nil {
			var field string
			msg := "Invalid adapter_config shape"
			switch {
			case errors.Is(normErr, adapterschema.ErrShapeInvalidJSON):
				field = "adapter_config"
				msg = "adapter_config is not valid JSON"
			case errors.Is(normErr, adapterschema.ErrUpConvertMissingHint):
				field = "adapter_config"
				msg = "legacy flat adapter_config could not be up-converted without a protocol hint"
			case errors.Is(normErr, adapterschema.ErrValidation):
				field = "adapter_config"
				msg = normErr.Error()
			default:
				field = "adapter_config"
				msg = "adapter_config could not be normalized: " + normErr.Error()
			}
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				"Request validation failed",
				[]map[string]string{{"field": field, "message": msg, "code": "invalid_shape"}})
			return
		}
		adapterConfig = normalized
	}
	if adapterConfig != nil && len(adapterConfig) > 0 && h.encryptionKey != "" {
		encrypted, err := crypto.EncryptJSON(adapterConfig, h.encryptionKey)
		if err != nil {
			h.logger.Error().Err(err).Msg("encrypt adapter config on update")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		adapterConfig = encrypted
	}

	smDPConfig := req.SMDPPlusConfig
	if smDPConfig != nil && len(smDPConfig) > 0 && h.encryptionKey != "" {
		encrypted, err := crypto.EncryptJSON(smDPConfig, h.encryptionKey)
		if err != nil {
			h.logger.Error().Err(err).Msg("encrypt sm-dp+ config on update")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		smDPConfig = encrypted
	}

	if req.SLAUptimeTarget != nil || req.SLALatencyThresholdMs != nil {
		actorID := userIDFromContext(r)
		var actorUUID uuid.UUID
		if actorID != nil {
			actorUUID = *actorID
		}
		slaUptime := existing.SLAUptimeTarget
		var slaUptimeVal float64
		if slaUptime != nil {
			slaUptimeVal = *slaUptime
		} else {
			slaUptimeVal = 99.9
		}
		if req.SLAUptimeTarget != nil {
			slaUptimeVal = *req.SLAUptimeTarget
		}
		slaLatency := existing.SLALatencyThresholdMs
		if req.SLALatencyThresholdMs != nil {
			slaLatency = *req.SLALatencyThresholdMs
		}
		if slaErr := h.operatorStore.UpdateSLATargets(r.Context(), id, slaUptimeVal, slaLatency, actorUUID); slaErr != nil {
			if errors.Is(slaErr, store.ErrOperatorNotFound) {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
				return
			}
			h.logger.Error().Err(slaErr).Str("operator_id", idStr).Msg("update sla targets")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
	}

	// SLA fields are always routed through UpdateSLATargets (called above) when
	// either is present — pass nil here to avoid a double-write on sla_uptime_target.
	updated, err := h.operatorStore.Update(r.Context(), id, store.UpdateOperatorParams{
		Name:                      req.Name,
		AdapterConfig:             adapterConfig,
		SMDPPlusURL:               req.SMDPPlusURL,
		SMDPPlusConfig:            smDPConfig,
		SupportedRATTypes:         req.SupportedRATTypes,
		FailoverPolicy:            req.FailoverPolicy,
		FailoverTimeoutMs:         req.FailoverTimeoutMs,
		CircuitBreakerThreshold:   req.CircuitBreakerThreshold,
		CircuitBreakerRecoverySec: req.CircuitBreakerRecoverySec,
		HealthCheckIntervalSec:    req.HealthCheckIntervalSec,
		State:                     req.State,
	})
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("update operator")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if req.AdapterConfig != nil || req.State != nil {
		h.adapterRegistry.Remove(id)
	}

	h.createAuditEntry(r, "operator.update", id.String(), existing, updated)

	resp := toOperatorResponse(updated, h.deriveEnabledProtocolsFromStored(updated))
	if nested, _ := h.resolveNestedAdapterConfigForResponse(r.Context(), updated); len(nested) > 0 {
		if masked, mErr := maskAdapterConfig(nested); mErr == nil {
			resp.AdapterConfig = masked
		}
	}
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

// Get (Detail) returns a single operator with adapter_config included
// (secrets masked). STORY-090 Gate (F-A2): adds the missing detail
// endpoint so the Protocols tab can reflect stored state on first
// render. Plan §API Specifications > GET detail.
// Route: GET /api/v1/operators/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator detail")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	// Tenant-scope enforcement: non-super_admin users must have a
	// grant for this operator, matching the List filter behaviour at
	// handler.go List().
	ctx := r.Context()
	tenantID, _ := ctx.Value(apierr.TenantIDKey).(uuid.UUID)
	role, _ := ctx.Value(apierr.RoleKey).(string)
	if role != "super_admin" && tenantID != uuid.Nil {
		grants, gErr := h.operatorStore.ListGrants(ctx, tenantID)
		if gErr == nil {
			allowed := false
			for _, g := range grants {
				if g.OperatorID == op.ID {
					allowed = true
					break
				}
			}
			if !allowed {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
				return
			}
		}
	}

	nested, enabled := h.resolveNestedAdapterConfigForResponse(ctx, op)
	resp := toOperatorResponse(op, enabled)
	if len(nested) > 0 {
		if masked, mErr := maskAdapterConfig(nested); mErr == nil {
			resp.AdapterConfig = masked
		}
	}

	// Populate SimCount / ActiveSessions / TotalTrafficBytes / health
	// timestamp the same way the List handler does.
	if h.agg != nil && tenantID != uuid.Nil {
		if simCounts, err := h.agg.SIMCountByOperator(ctx, tenantID); err == nil {
			resp.SimCount = simCounts[op.ID]
		}
		if stats, err := h.agg.ActiveSessionStats(ctx, tenantID); err == nil {
			resp.ActiveSessions = stats.ByOperator[op.ID.String()]
		}
		if trafficMap, err := h.agg.TrafficByOperator(ctx, tenantID); err == nil {
			resp.TotalTrafficBytes = trafficMap[op.ID]
		}
	}
	if healthTimes, err := h.operatorStore.LatestHealthByOperator(ctx); err == nil {
		if t, ok := healthTimes[op.ID]; ok {
			ts := t.Format(time.RFC3339Nano)
			resp.LastHealthCheck = &ts
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for health")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	latestLog, err := h.operatorStore.GetLatestHealth(r.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get latest health")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	total, failures, err := h.operatorStore.CountFailures24h(r.Context(), id)
	if err != nil {
		h.logger.Warn().Err(err).Str("operator_id", idStr).Msg("count failures 24h")
	}

	resp := healthResponse{
		HealthStatus: op.HealthStatus,
		CircuitState: "closed",
		Uptime24h:    100.0,
		FailureCount: failures,
	}

	if latestLog != nil {
		resp.LatencyMs = latestLog.LatencyMs
		resp.CircuitState = latestLog.CircuitState
		ts := latestLog.CheckedAt.Format(time.RFC3339Nano)
		resp.LastCheck = &ts
	}

	if total > 0 {
		resp.Uptime24h = float64(total-failures) / float64(total) * 100.0
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

// testConnectionForProtocol runs the per-protocol HealthCheck against
// the decrypted+normalized nested adapter_config. Returns the response
// body, HTTP status code, and an error (the error is non-nil only when
// the caller should short-circuit without an envelope — adapter
// creation failures). For validation-style failures (protocol not
// enabled, no enabled protocols at all) the returned status is a 4xx
// and `err` is nil; the caller still emits an error envelope via
// apierr.WriteError.
//
// STORY-090 Wave 3 Task 7a: extracted from the legacy per-operator
// TestConnection handler so both the legacy path and the new per-
// protocol path share the same adapter resolution + HealthCheck
// invocation.
func (h *Handler) testConnectionForProtocol(ctx context.Context, op *store.Operator, protocol string, nestedPlaintext json.RawMessage) (testResponse, int, error) {
	if !adapterschema.IsValidProtocol(protocol) {
		return testResponse{}, http.StatusBadRequest, nil
	}

	parsed, pErr := adapterschema.ParseNested(nestedPlaintext)
	if pErr != nil {
		return testResponse{}, http.StatusUnprocessableEntity, nil
	}

	enabled := adapterschema.DeriveEnabledProtocols(parsed)
	isEnabled := false
	for _, p := range enabled {
		if p == protocol {
			isEnabled = true
			break
		}
	}
	if !isEnabled {
		return testResponse{}, http.StatusUnprocessableEntity, nil
	}

	sub := adapterschema.SubConfigRaw(parsed, protocol)
	if sub == nil {
		return testResponse{}, http.StatusUnprocessableEntity, nil
	}

	a, err := h.adapterRegistry.GetOrCreate(op.ID, protocol, sub)
	if err != nil {
		if errors.Is(err, adapter.ErrUnsupportedProtocol) {
			return testResponse{}, http.StatusBadRequest, err
		}
		return testResponse{}, http.StatusUnprocessableEntity, err
	}

	result := a.HealthCheck(ctx)
	return testResponse{
		Success:   result.Success,
		LatencyMs: result.LatencyMs,
		Error:     result.Error,
	}, http.StatusOK, nil
}

// resolveNestedAdapterConfig decrypts + up-converts the stored
// adapter_config into its nested plaintext form, falling back to a raw
// decrypt if normalization fails. Used by both TestConnection variants.
func (h *Handler) resolveNestedAdapterConfig(ctx context.Context, op *store.Operator) json.RawMessage {
	nestedPlaintext, err := h.decryptAndNormalize(ctx, op)
	if err != nil {
		h.logger.Warn().Err(err).Str("operator_id", op.ID.String()).Msg("normalize adapter_config for test — falling back to raw decrypt")
		nestedPlaintext = op.AdapterConfig
		if h.encryptionKey != "" {
			if decrypted, decErr := crypto.DecryptJSON(nestedPlaintext, h.encryptionKey); decErr == nil {
				nestedPlaintext = decrypted
			}
		}
	}
	return nestedPlaintext
}

// TestConnection (legacy path) exercises the primary enabled protocol
// derived from the nested adapter_config in canonical order. The frontend
// Overview-tab "Test Connection" button still calls this endpoint; the
// per-protocol variant lives at /operators/{id}/test/{protocol}.
func (h *Handler) TestConnection(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for test")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	nestedPlaintext := h.resolveNestedAdapterConfig(r.Context(), op)

	primary := ""
	if parsed, pErr := adapterschema.ParseNested(nestedPlaintext); pErr == nil {
		primary = adapterschema.DerivePrimaryProtocol(parsed)
	}
	if primary == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeProtocolNotConfigured,
			"Operator has no enabled protocol — cannot run TestConnection")
		return
	}

	resp, status, tcErr := h.testConnectionForProtocol(r.Context(), op, primary, nestedPlaintext)
	if tcErr != nil {
		h.logger.Warn().Err(tcErr).Str("protocol", primary).Msg("adapter config invalid for test")
		apierr.WriteError(w, status, apierr.CodeAdapterConfigInvalid, tcErr.Error())
		return
	}
	if status != http.StatusOK {
		apierr.WriteError(w, status, apierr.CodeValidationError, "TestConnection failed for primary protocol")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

// TestConnectionForProtocol (STORY-090 Wave 3 Task 7a) runs the
// HealthCheck against a caller-specified protocol read from the URL
// path. 400 for an unknown protocol name, 422 when the protocol is not
// enabled in the operator's adapter_config, 404 for unknown operator.
// Route: POST /api/v1/operators/{id}/test/{protocol}
func (h *Handler) TestConnectionForProtocol(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	protocol := chi.URLParam(r, "protocol")
	if !adapterschema.IsValidProtocol(protocol) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Invalid protocol name — must be one of: mock, radius, diameter, sba, http")
		return
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for test")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	nestedPlaintext := h.resolveNestedAdapterConfig(r.Context(), op)

	resp, status, tcErr := h.testConnectionForProtocol(r.Context(), op, protocol, nestedPlaintext)
	if tcErr != nil {
		h.logger.Warn().Err(tcErr).Str("protocol", protocol).Msg("adapter config invalid for per-protocol test")
		apierr.WriteError(w, status, apierr.CodeAdapterConfigInvalid, tcErr.Error())
		return
	}
	switch status {
	case http.StatusOK:
		apierr.WriteSuccess(w, http.StatusOK, resp)
	case http.StatusBadRequest:
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Invalid protocol name — must be one of: mock, radius, diameter, sba, http")
	case http.StatusUnprocessableEntity:
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeProtocolNotConfigured,
			"Protocol is not enabled in this operator's adapter_config")
	default:
		apierr.WriteError(w, status, apierr.CodeInternalError, "TestConnection failed")
	}
}

func (h *Handler) CreateGrant(w http.ResponseWriter, r *http.Request) {
	var req createGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.TenantID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "tenant_id", "message": "Tenant ID is required", "code": "required"})
	}
	if req.OperatorID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "operator_id", "message": "Operator ID is required", "code": "required"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid tenant_id format")
		return
	}
	operatorID, err := uuid.Parse(req.OperatorID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
		return
	}

	if _, err := h.tenantStore.GetByID(r.Context(), tenantID); err != nil {
		if errors.Is(err, store.ErrTenantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Tenant not found")
			return
		}
		h.logger.Error().Err(err).Msg("get tenant for grant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if _, err := h.operatorStore.GetByID(r.Context(), operatorID); err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Msg("get operator for grant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromContext(r)

	g, err := h.operatorStore.CreateGrant(r.Context(), tenantID, operatorID, userID, req.SupportedRATTypes)
	if err != nil {
		if errors.Is(err, store.ErrGrantExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"This operator is already granted to this tenant")
			return
		}
		h.logger.Error().Err(err).Msg("create operator grant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "operator_grant.create", g.ID.String(), nil, g)

	apierr.WriteSuccess(w, http.StatusCreated, toGrantResponse(g))
}

func (h *Handler) ListGrants(w http.ResponseWriter, r *http.Request) {
	role, _ := r.Context().Value(apierr.RoleKey).(string)
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	targetTenantID := tenantID
	if role == "super_admin" {
		if tid := r.URL.Query().Get("tenant_id"); tid != "" {
			parsed, err := uuid.Parse(tid)
			if err != nil {
				apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid tenant_id format")
				return
			}
			targetTenantID = parsed
		}
	}

	if !apierr.HasRole(role, "tenant_admin") && role != "super_admin" {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
			"This action requires tenant_admin role or higher")
		return
	}

	grants, err := h.operatorStore.ListGrants(r.Context(), targetTenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list operator grants")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]grantResponse, 0, len(grants))
	for _, g := range grants {
		items = append(items, toGrantResponse(&g))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Limit: len(items),
	})
}

func (h *Handler) DeleteGrant(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid grant ID format")
		return
	}

	existing, err := h.operatorStore.GetGrantByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrGrantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator grant not found")
			return
		}
		h.logger.Error().Err(err).Str("grant_id", idStr).Msg("get grant for delete")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if err := h.operatorStore.DeleteGrant(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrGrantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator grant not found")
			return
		}
		h.logger.Error().Err(err).Str("grant_id", idStr).Msg("delete grant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "operator_grant.delete", id.String(), existing, nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

// GetSessions returns currently-active RADIUS sessions for a single operator.
// Tenant-scoped via the middleware-populated TenantIDKey. Cursor paginated.
//
// Route: GET /api/v1/operators/:id/sessions?limit=N&cursor=UUID
// Role:  api_user+ (read)
func (h *Handler) GetSessions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	operatorID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	// Tenant scoping: verify the caller's tenant has a grant on this operator.
	if _, err := h.operatorStore.GetByID(r.Context(), operatorID); err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for sessions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.sessionStore == nil {
		apierr.WriteList(w, http.StatusOK, []interface{}{}, apierr.ListMeta{HasMore: false})
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	cursor := r.URL.Query().Get("cursor")

	params := store.ListActiveSessionsParams{
		TenantID:   &tenantID,
		OperatorID: &operatorID,
		Limit:      limit,
		Cursor:     cursor,
	}
	sessions, nextCursor, err := h.sessionStore.ListActiveFiltered(r.Context(), params)
	if err != nil {
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("list operator sessions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to list sessions")
		return
	}

	// Enrich with ICCID / IMSI / MSISDN by joining SIM data.
	// Per-session SIM fetch — bounded by limit (≤100).
	items := make([]operatorSessionDTO, 0, len(sessions))
	now := time.Now()
	for i := range sessions {
		s := &sessions[i]
		dto := operatorSessionDTO{
			ID:           s.ID.String(),
			SimID:        s.SimID.String(),
			OperatorID:   s.OperatorID.String(),
			NASIP:        stripCIDRSuffix(deref(s.NASIP)),
			FramedIP:     stripCIDRSuffix(deref(s.FramedIP)),
			SessionState: s.SessionState,
			StartedAt:    s.StartedAt.Format(time.RFC3339),
			BytesIn:      s.BytesIn,
			BytesOut:     s.BytesOut,
			DurationSec:  int64(now.Sub(s.StartedAt).Seconds()),
		}
		if s.APNID != nil {
			apnID := s.APNID.String()
			dto.APNID = &apnID
		}
		if h.simStore != nil {
			if sim, err := h.simStore.GetByID(r.Context(), tenantID, s.SimID); err == nil {
				dto.IMSI = sim.IMSI
				dto.ICCID = sim.ICCID
				if sim.MSISDN != nil {
					dto.MSISDN = *sim.MSISDN
				}
			}
		}
		items = append(items, dto)
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

// operatorSessionDTO is the enriched response shape for operator sessions:
// joins RadiusSession + SIM (for IMSI / ICCID / MSISDN). Duration is
// computed server-side so UI doesn't need clock sync.
type operatorSessionDTO struct {
	ID           string  `json:"id"`
	SimID        string  `json:"sim_id"`
	OperatorID   string  `json:"operator_id"`
	APNID        *string `json:"apn_id,omitempty"`
	NASIP        string  `json:"nas_ip"`
	FramedIP     string  `json:"framed_ip,omitempty"`
	IMSI         string  `json:"imsi,omitempty"`
	ICCID        string  `json:"iccid,omitempty"`
	MSISDN       string  `json:"msisdn,omitempty"`
	SessionState string  `json:"session_state"`
	StartedAt    string  `json:"started_at"`
	DurationSec  int64   `json:"duration_sec"`
	BytesIn      int64   `json:"bytes_in"`
	BytesOut     int64   `json:"bytes_out"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// stripCIDRSuffix removes an optional "/N" (e.g. "10.0.0.1/32" → "10.0.0.1").
// PostgreSQL INET columns serialise with the suffix even for host addresses.
func stripCIDRSuffix(ip string) string {
	for i, c := range ip {
		if c == '/' {
			return ip[:i]
		}
	}
	return ip
}

// GetTraffic returns bytes-in/out time-series bucketed over the requested
// period for a single operator. Same validPeriods surface as APN traffic
// (15m / 1h / 6h / 24h / 7d / 30d). Uses cdrs_hourly for short periods,
// raw cdrs for 7d/30d.
//
// Route: GET /api/v1/operators/:id/traffic?period=24h
// Role:  api_user+
func (h *Handler) GetTraffic(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	operatorID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	validPeriods := map[string]bool{"15m": true, "1h": true, "6h": true, "24h": true, "7d": true, "30d": true}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}
	if !validPeriods[period] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "period must be one of: 15m, 1h, 6h, 24h, 7d, 30d")
		return
	}

	if _, err := h.operatorStore.GetByID(r.Context(), operatorID); err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for traffic")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.cdrStore == nil {
		apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"period": period,
			"series": []store.APNTrafficBucket{},
		})
		return
	}

	series, err := h.cdrStore.GetOperatorTraffic(r.Context(), tenantID, operatorID, period)
	if err != nil {
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator traffic")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to retrieve operator traffic")
		return
	}
	if series == nil {
		series = []store.APNTrafficBucket{}
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"period": period,
		"series": series,
	})
}

func (h *Handler) createAuditEntry(r *http.Request, action, entityID string, before, after interface{}) {
	if h.auditSvc == nil {
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	userID := userIDFromContext(r)
	ip := r.RemoteAddr
	ua := r.UserAgent()

	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	var beforeData, afterData json.RawMessage
	if before != nil {
		beforeData, _ = json.Marshal(before)
	}
	if after != nil {
		afterData, _ = json.Marshal(after)
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		EntityType:    "operator",
		EntityID:      entityID,
		BeforeData:    beforeData,
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Str("action", action).Msg("audit entry failed")
	}
}

func (h *Handler) GetHealthHistory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	q := r.URL.Query()
	hours := 24
	if v := q.Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 168 {
			hours = n
		} else if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "hours must be between 1 and 168")
			return
		}
	}
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		} else if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "limit must be between 1 and 500")
			return
		}
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for health history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	_ = op

	logs, err := h.operatorStore.GetHealthLogs(r.Context(), id, limit)
	if err != nil {
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get health logs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to retrieve health history")
		return
	}

	window := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	filtered := make([]store.OperatorHealthLog, 0, len(logs))
	for _, l := range logs {
		if l.CheckedAt.After(window) {
			filtered = append(filtered, l)
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, filtered)
}

func (h *Handler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	window := r.URL.Query().Get("window")
	validWindows := map[string]bool{"15m": true, "1h": true, "6h": true, "24h": true}
	if window == "" {
		window = "1h"
	}
	if !validWindows[window] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "window must be one of: 15m, 1h, 6h, 24h")
		return
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for metrics")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	_ = op

	if h.cdrStore == nil {
		apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"window":  window,
			"buckets": []store.OperatorMetricBucket{},
		})
		return
	}

	buckets, err := h.cdrStore.GetOperatorMetrics(r.Context(), tenantID, id, window)
	if err != nil {
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator metrics")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to retrieve operator metrics")
		return
	}

	if buckets == nil {
		buckets = []store.OperatorMetricBucket{}
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"window":  window,
		"buckets": buckets,
	})
}

func userIDFromContext(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
