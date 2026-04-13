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

type SearchResult struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
}

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
		apierr.WriteSuccess(w, http.StatusOK, map[string][]SearchResult{})
		return
	}

	pattern := "%" + q + "%"

	type entityResults struct {
		sims      []SearchResult
		apns      []SearchResult
		operators []SearchResult
		policies  []SearchResult
		users     []SearchResult
	}

	var results entityResults
	g, gctx := errgroup.WithContext(ctx)

	if typeFilter == nil || typeFilter["sim"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT id, iccid, imsi FROM sims
				 WHERE tenant_id = $1 AND (iccid ILIKE $2 OR imsi ILIKE $2 OR msisdn ILIKE $2)
				 ORDER BY created_at DESC
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
				var iccid, imsi string
				if err := rows.Scan(&id, &iccid, &imsi); err != nil {
					continue
				}
				results.sims = append(results.sims, SearchResult{
					Type:  "sim",
					ID:    id.String(),
					Label: iccid,
					Sub:   imsi,
				})
			}
			return nil
		})
	}

	if typeFilter == nil || typeFilter["apn"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT id, name, state FROM apns
				 WHERE tenant_id = $1 AND name ILIKE $2
				 ORDER BY created_at DESC
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
				var name, state string
				if err := rows.Scan(&id, &name, &state); err != nil {
					continue
				}
				results.apns = append(results.apns, SearchResult{
					Type:  "apn",
					ID:    id.String(),
					Label: name,
					Sub:   state,
				})
			}
			return nil
		})
	}

	if typeFilter == nil || typeFilter["operator"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT DISTINCT o.id, o.name, o.code
				 FROM operators o
				 JOIN operator_grants g ON g.operator_id = o.id AND g.tenant_id = $1
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
				var name, code string
				if err := rows.Scan(&id, &name, &code); err != nil {
					continue
				}
				results.operators = append(results.operators, SearchResult{
					Type:  "operator",
					ID:    id.String(),
					Label: name,
					Sub:   code,
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
				results.policies = append(results.policies, SearchResult{
					Type:  "policy",
					ID:    id.String(),
					Label: name,
					Sub:   state,
				})
			}
			return nil
		})
	}

	if typeFilter == nil || typeFilter["user"] {
		g.Go(func() error {
			rows, err := h.db.Query(gctx,
				`SELECT id, email, COALESCE(name, '') FROM users
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
				var email, name string
				if err := rows.Scan(&id, &email, &name); err != nil {
					continue
				}
				label := email
				if name != "" {
					label = name
				}
				results.users = append(results.users, SearchResult{
					Type:  "user",
					ID:    id.String(),
					Label: label,
					Sub:   email,
				})
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		h.logger.Error().Err(err).Msg("search errgroup error")
	}

	grouped := map[string][]SearchResult{}
	if len(results.sims) > 0 {
		grouped["sim"] = results.sims
	}
	if len(results.apns) > 0 {
		grouped["apn"] = results.apns
	}
	if len(results.operators) > 0 {
		grouped["operator"] = results.operators
	}
	if len(results.policies) > 0 {
		grouped["policy"] = results.policies
	}
	if len(results.users) > 0 {
		grouped["user"] = results.users
	}

	apierr.WriteSuccess(w, http.StatusOK, grouped)
}
