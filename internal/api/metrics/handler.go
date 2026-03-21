package metrics

import (
	"net/http"

	analyticmetrics "github.com/btopcu/argus/internal/analytics/metrics"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/rs/zerolog"
)

type Handler struct {
	collector *analyticmetrics.Collector
	logger    zerolog.Logger
}

func NewHandler(collector *analyticmetrics.Collector, logger zerolog.Logger) *Handler {
	return &Handler{
		collector: collector,
		logger:    logger.With().Str("component", "metrics_handler").Logger(),
	}
}

func (h *Handler) GetSystemMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	m, err := h.collector.GetMetrics(ctx)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to get system metrics")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to retrieve metrics")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, m)
}
