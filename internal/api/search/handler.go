package search

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

type Handler struct {
	simStore      *store.SIMStore
	apnStore      *store.APNStore
	operatorStore *store.OperatorStore
	policyStore   *store.PolicyStore
	userStore     *store.UserStore
	db            *pgxpool.Pool
	logger        zerolog.Logger
}

func NewHandler(
	simStore *store.SIMStore,
	apnStore *store.APNStore,
	operatorStore *store.OperatorStore,
	policyStore *store.PolicyStore,
	userStore *store.UserStore,
	db *pgxpool.Pool,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		simStore:      simStore,
		apnStore:      apnStore,
		operatorStore: operatorStore,
		policyStore:   policyStore,
		userStore:     userStore,
		db:            db,
		logger:        logger.With().Str("component", "search_handler").Logger(),
	}
}

// D-008: Per-type enriched search result DTOs.

type SIMResult struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	Label        string `json:"label"`
	Sub          string `json:"sub,omitempty"`
	State        string `json:"state,omitempty"`
	OperatorName string `json:"operator_name,omitempty"`
}

type APNResult struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	Label        string `json:"label"`
	Sub          string `json:"sub,omitempty"`
	MCC          string `json:"mcc,omitempty"`
	OperatorName string `json:"operator_name,omitempty"`
}

type OperatorResult struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	Label        string `json:"label"`
	Sub          string `json:"sub,omitempty"`
	MCC          string `json:"mcc,omitempty"`
	HealthStatus string `json:"health_status,omitempty"`
}

type PolicyResult struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
	State string `json:"state,omitempty"`
}

type UserResult struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
	Role  string `json:"role,omitempty"`
}

// SearchResult is kept for backward compatibility.
type SearchResult = SIMResult

const defaultSearchLimit = 5
const maxSearchLimit = 20

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "q is required")
		return
	}

	typesParam := r.URL.Query().Get("types")
	var typeFilter map[string]bool
	if typesParam != "" {
		typeFilter = make(map[string]bool)
		for _, t := range strings.Split(typesParam, ",") {
			typeFilter[strings.TrimSpace(t)] = true
		}
	}

	limit := defaultSearchLimit
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if v, err := strconv.Atoi(lStr); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
	defer cancel()

	if h.db == nil {
		apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{})
		return
	}

	pattern := "%" + q + "%"

	type entityResults struct {
		sims      []SIMResult
		apns      []APNResult
		operators []OperatorResult
		policies  []PolicyResult
		users     []UserResult
	}

	var results entityResults
	g, gctx := errgroup.WithContext(ctx)

	if typeFilter == nil || typeFilter["sim"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT s.id, s.iccid, s.imsi, s.state, COALESCE(o.name,'') as op_name
				 FROM sims s
				 LEFT JOIN operators o ON o.id = s.operator_id
				 WHERE s.tenant_id = $1 AND (s.iccid ILIKE $2 OR s.imsi ILIKE $2 OR s.msisdn ILIKE $2)
				 ORDER BY s.created_at DESC
				 LIMIT $3`,
				tenantID, pattern, limit,
			)
			if err != nil {
				h.logger.Warn().Err(err).Msg("search sims query failed")
				return nil
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				var iccid, imsi, state, opName string
				if err := rows.Scan(&id, &iccid, &imsi, &state, &opName); err != nil {
					continue
				}
				results.sims = append(results.sims, SIMResult{
					Type:         "sim",
					ID:           id.String(),
					Label:        iccid,
					Sub:          imsi,
					State:        state,
					OperatorName: opName,
				})
			}
			return nil
		})
	}

	if typeFilter == nil || typeFilter["apn"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT a.id, a.name, a.state, COALESCE(o.code,'') as mcc, COALESCE(o.name,'') as op_name
				 FROM apns a
				 LEFT JOIN operators o ON o.id = a.operator_id
				 WHERE a.tenant_id = $1 AND a.name ILIKE $2
				 ORDER BY a.created_at DESC
				 LIMIT $3`,
				tenantID, pattern, limit,
			)
			if err != nil {
				h.logger.Warn().Err(err).Msg("search apns query failed")
				return nil
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				var name, state, mcc, opName string
				if err := rows.Scan(&id, &name, &state, &mcc, &opName); err != nil {
					continue
				}
				results.apns = append(results.apns, APNResult{
					Type:         "apn",
					ID:           id.String(),
					Label:        name,
					Sub:          state,
					MCC:          mcc,
					OperatorName: opName,
				})
			}
			return nil
		})
	}

	if typeFilter == nil || typeFilter["operator"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT DISTINCT o.id, o.name, o.code, COALESCE(o.mcc,'') as mcc, COALESCE(oh.status,'unknown') as health_status
				 FROM operators o
				 JOIN operator_grants g ON g.operator_id = o.id AND g.tenant_id = $1
				 LEFT JOIN LATERAL (
				     SELECT status FROM operator_health_history
				     WHERE operator_id = o.id ORDER BY checked_at DESC LIMIT 1
				 ) oh ON TRUE
				 WHERE o.name ILIKE $2 OR o.code ILIKE $2
				 ORDER BY o.name
				 LIMIT $3`,
				tenantID, pattern, limit,
			)
			if err != nil {
				h.logger.Warn().Err(err).Msg("search operators query failed")
				return nil
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				var name, code, mcc, health string
				if err := rows.Scan(&id, &name, &code, &mcc, &health); err != nil {
					continue
				}
				results.operators = append(results.operators, OperatorResult{
					Type:         "operator",
					ID:           id.String(),
					Label:        name,
					Sub:          code,
					MCC:          mcc,
					HealthStatus: health,
				})
			}
			return nil
		})
	}

	if typeFilter == nil || typeFilter["policy"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT id, name, state FROM policies
				 WHERE tenant_id = $1 AND name ILIKE $2
				 ORDER BY updated_at DESC
				 LIMIT $3`,
				tenantID, pattern, limit,
			)
			if err != nil {
				h.logger.Warn().Err(err).Msg("search policies query failed")
				return nil
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				var name, state string
				if err := rows.Scan(&id, &name, &state); err != nil {
					continue
				}
				results.policies = append(results.policies, PolicyResult{
					Type:  "policy",
					ID:    id.String(),
					Label: name,
					Sub:   state,
					State: state,
				})
			}
			return nil
		})
	}

	if typeFilter == nil || typeFilter["user"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT id, email, COALESCE(name, ''), role FROM users
				 WHERE tenant_id = $1 AND (email ILIKE $2 OR name ILIKE $2)
				 ORDER BY created_at DESC
				 LIMIT $3`,
				tenantID, pattern, limit,
			)
			if err != nil {
				h.logger.Warn().Err(err).Msg("search users query failed")
				return nil
			}
			defer rows.Close()
			for rows.Next() {
				var id uuid.UUID
				var email, name, role string
				if err := rows.Scan(&id, &email, &name, &role); err != nil {
					continue
				}
				label := email
				if name != "" {
					label = name
				}
				results.users = append(results.users, UserResult{
					Type:  "user",
					ID:    id.String(),
					Label: label,
					Sub:   email,
					Role:  role,
				})
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		h.logger.Error().Err(err).Msg("search errgroup error")
	}

	response := map[string]interface{}{
		"sims":      orEmpty(simsToIface(results.sims)),
		"apns":      orEmpty(apnsToIface(results.apns)),
		"operators": orEmpty(opsToIface(results.operators)),
		"policies":  orEmpty(policiesToIface(results.policies)),
		"users":     orEmpty(usersToIface(results.users)),
	}

	apierr.WriteSuccess(w, http.StatusOK, response)
}

func orEmpty(v []interface{}) []interface{} {
	if v == nil {
		return []interface{}{}
	}
	return v
}

func simsToIface(s []SIMResult) []interface{} {
	r := make([]interface{}, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}

func apnsToIface(s []APNResult) []interface{} {
	r := make([]interface{}, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}

func opsToIface(s []OperatorResult) []interface{} {
	r := make([]interface{}, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}

func policiesToIface(s []PolicyResult) []interface{} {
	r := make([]interface{}, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}

func usersToIface(s []UserResult) []interface{} {
	r := make([]interface{}, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}
