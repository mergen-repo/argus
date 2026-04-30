package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/operatorsim/config"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

type metricsRegistry struct {
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	reg             *prometheus.Registry
}

func newMetricsRegistry() *metricsRegistry {
	reg := prometheus.NewRegistry()
	requestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "operator_sim_requests_total",
		Help: "Total number of requests handled by operator-sim, partitioned by operator, path, method, and status code.",
	}, []string{"operator", "path", "method", "status_code"})
	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "operator_sim_request_duration_seconds",
		Help:    "Duration of requests handled by operator-sim.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operator", "path", "method"})
	reg.MustRegister(requestsTotal, requestDuration)
	return &metricsRegistry{
		requestsTotal:   requestsTotal,
		requestDuration: requestDuration,
		reg:             reg,
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (sr *statusRecorder) WriteHeader(code int) {
	if !sr.wrote {
		sr.status = code
		sr.wrote = true
		sr.ResponseWriter.WriteHeader(code)
	}
}

func (sr *statusRecorder) statusCode() int {
	if !sr.wrote {
		return http.StatusOK
	}
	return sr.status
}

type Server struct {
	cfg         *config.Config
	logger      zerolog.Logger
	router      *chi.Mux
	metrics     *metricsRegistry
	httpServer  *http.Server
	muxServer   *http.Server
	operatorSet map[string]struct{}
}

func New(cfg *config.Config, logger zerolog.Logger) *Server {
	s := &Server{
		cfg:     cfg,
		logger:  logger,
		metrics: newMetricsRegistry(),
	}

	s.operatorSet = make(map[string]struct{}, len(cfg.Operators))
	for _, op := range cfg.Operators {
		s.operatorSet[op.Code] = struct{}{}
	}

	s.router = s.buildRouter()

	s.httpServer = &http.Server{
		Addr:         cfg.Server.Listen,
		Handler:      s.router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	s.muxServer = &http.Server{
		Addr:    cfg.Server.MetricsListen,
		Handler: s.buildMetricsMux(),
	}

	return s
}

func (s *Server) buildMetricsMux() *chi.Mux {
	metricsMux := chi.NewRouter()
	metricsMux.Get("/-/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	metricsMux.Handle("/-/metrics", promhttp.HandlerFor(s.metrics.reg, promhttp.HandlerOpts{}))
	return metricsMux
}

func (s *Server) buildRouter() *chi.Mux {
	r := chi.NewRouter()

	r.Route("/{operator}", func(r chi.Router) {
		r.Use(s.validateOperator)
		r.Use(s.instrumentMiddleware)
		r.Get("/health", s.healthHandler)
		r.Get("/subscribers/{imsi}", s.subscriberHandler)
		r.Post("/cdr", s.cdrHandler)
	})

	return r
}

func (s *Server) validateOperator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op := chi.URLParam(r, "operator")
		if _, ok := s.operatorSet[op]; !ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			resp := struct {
				Error    string `json:"error"`
				Operator string `json:"operator"`
			}{"unknown operator", op}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) instrumentMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(sr, r)

		op := chi.URLParam(r, "operator")
		routePattern := chi.RouteContext(r.Context()).RoutePattern()
		statusCode := strconv.Itoa(sr.statusCode())

		s.metrics.requestsTotal.WithLabelValues(op, routePattern, r.Method, statusCode).Inc()
		s.metrics.requestDuration.WithLabelValues(op, routePattern, r.Method).Observe(time.Since(start).Seconds())
	})
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	go func() {
		s.logger.Info().Str("addr", s.cfg.Server.Listen).Msg("operator-sim HTTP server listening")
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	go func() {
		s.logger.Info().Str("addr", s.cfg.Server.MetricsListen).Msg("operator-sim metrics server listening")
		if err := s.muxServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Error().Err(err).Msg("HTTP server shutdown error")
	}
	if err := s.muxServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Error().Err(err).Msg("metrics server shutdown error")
	}

	return nil
}
