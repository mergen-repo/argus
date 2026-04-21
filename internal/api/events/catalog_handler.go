package events

import (
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/rs/zerolog"
)

// Handler serves the static event catalog (FIX-212 AC-5). Read-only,
// tenant-agnostic, zero DB queries.
type Handler struct {
	logger zerolog.Logger
}

// NewHandler constructs an events.Handler.
func NewHandler(logger zerolog.Logger) *Handler {
	return &Handler{logger: logger.With().Str("component", "events_catalog").Logger()}
}

// List returns the full Catalog table under the standard success envelope.
// GET /api/v1/events/catalog.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"events": Catalog,
	})
}
