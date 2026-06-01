package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/myorg/docker-cleanup-agent/internal/cleanup"
	"github.com/myorg/docker-cleanup-agent/internal/config"
	"github.com/myorg/docker-cleanup-agent/internal/docker"
	"github.com/myorg/docker-cleanup-agent/internal/metrics"
	"github.com/myorg/docker-cleanup-agent/internal/storage"
)

// Server is the HTTP server with all routes mounted.
type Server struct {
	httpServer *http.Server
	cfg        config.ServerConfig
	logger     *slog.Logger
}

// New creates and configures the HTTP server with all routes mounted.
func New(
	cfg config.ServerConfig,
	db *storage.DB,
	reg *metrics.Registry,
	dockerClient *docker.Client,
	cleaner cleanup.Cleaner,
	serverID string,
	logger *slog.Logger,
) *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))

	// /health is always open — used by orchestrators and uptime monitors
	r.Get("/health", HealthHandler(dockerClient, serverID))

	// /metrics and /api/* are protected by Basic Auth if credentials are configured
	r.Group(func(r chi.Router) {
		if cfg.Auth.Username != "" {
			r.Use(basicAuth(cfg.Auth.Username, cfg.Auth.Password))
		}
		r.Get("/metrics", reg.Handler().ServeHTTP)
		r.Route("/api", func(r chi.Router) {
			r.Get("/history", historyHandler(db, serverID))
			r.Get("/stats", statsHandler(db, serverID))
			r.Get("/containers", containersHandler(db, serverID))
		})
	})

	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.Listen,
			Handler:      r,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		cfg:    cfg,
		logger: logger,
	}
}

// Start begins listening on cfg.Listen. Non-blocking.
func (s *Server) Start() error {
	s.logger.Info("http server listening", "addr", s.cfg.Listen)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("http server error", "error", err)
		}
	}()
	return nil
}

// Shutdown gracefully drains connections.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// GET /api/history?limit=20
func historyHandler(db *storage.DB, serverID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := queryInt(r, "limit", 20, 1, 100)
		sid := r.URL.Query().Get("server_id")
		if sid == "" {
			sid = serverID
		}

		runs, err := db.ListRuns(r.Context(), sid, limit)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if runs == nil {
			runs = []storage.RunRecord{}
		}
		jsonOK(w, map[string]any{"runs": runs, "total": len(runs)})
	}
}

// GET /api/stats?server_id=<id>
func statsHandler(db *storage.DB, serverID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := r.URL.Query().Get("server_id")
		if sid == "" {
			sid = serverID
		}

		stats, err := db.GetAggregateStats(r.Context(), sid)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, stats)
	}
}

// GET /api/containers?limit=10&window_days=30
func containersHandler(db *storage.DB, serverID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := queryInt(r, "limit", 10, 1, 100)
		windowDays := queryInt(r, "window_days", 30, 1, 365)

		containers, err := db.GetTopContainersByCleanupCount(r.Context(), serverID, limit, windowDays)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if containers == nil {
			containers = []storage.ContainerFrequency{}
		}
		jsonOK(w, map[string]any{"containers": containers})
	}
}

func queryInt(r *http.Request, key string, defaultVal, min, max int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}

// basicAuth returns a middleware that requires HTTP Basic Auth.
// Uses constant-time comparison to prevent timing attacks.
func basicAuth(username, password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(u), []byte(username)) != 1 ||
				subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="docker-cleanup-agent"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"latency_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
