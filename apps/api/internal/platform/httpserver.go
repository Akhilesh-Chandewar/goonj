package platform

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Status/value keys shared by all health payloads.
const (
	statusKey = "status"
	valueKey  = "values"
	errorKey  = "error"
)

// Checker reports component health for readiness probes.
type Checker interface {
	CheckHealth(ctx context.Context) HealthStatus
}

// CheckerFunc adapts a function to Checker.
type CheckerFunc func(ctx context.Context) HealthStatus

// CheckHealth implements Checker.
func (f CheckerFunc) CheckHealth(ctx context.Context) HealthStatus { return f(ctx) }

// HealthStatus is the result of a single component check.
type HealthStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// HTTPServer wraps the router with server-level concerns: request logging,
// panic recovery, timeouts, and liveness/readiness endpoints.
type HTTPServer struct {
	http    *http.Server
	log     *slog.Logger
	readers []Checker
}

// NewHTTPServer builds the server around the given root handler.
func NewHTTPServer(cfg Config, log *slog.Logger, root http.Handler, checkers ...Checker) *HTTPServer {
	mux := http.NewServeMux()
	mux.Handle("/healthz", liveness(log))
	mux.Handle("/readyz", readiness(log, checkers))
	mux.Handle("/", root)

	handler := recoverAndLog(mux, log)

	return &HTTPServer{
		http: &http.Server{
			Addr:         ":" + cfg.HTTPPort,
			Handler:      handler,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			IdleTimeout:  120 * time.Second,
		},
		log: log,
	}
}

// Start runs the server; it returns only on error (which is always non-nil,
// http.ErrServerClosed aside).
func (s *HTTPServer) Start() error {
	s.log.Info("http server listening", slog.String("addr", s.http.Addr))
	return s.http.ListenAndServe()
}

// Shutdown gracefully drains connections within the configured grace period.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.log.Info("http server shutting down")
	return s.http.Shutdown(ctx)
}

func liveness(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		log.Debug("liveness checked")
		writeJSON(w, http.StatusOK, map[string]string{statusKey: "alive"})
	}
}

func readiness(log *slog.Logger, checkers []Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		results := make(map[string]HealthStatus, len(checkers))
		ready := true
		for _, c := range checkers {
			res := c.CheckHealth(ctx)
			if res.Status != "ok" {
				ready = false
			}
			name := componentName(c)
			results[name] = res
		}

		body := map[string]any{statusKey: statusValue(ready), valueKey: results}
		if !ready {
			writeJSON(w, http.StatusServiceUnavailable, body)
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

func componentName(c Checker) string {
	switch v := c.(type) {
	case PostgresHealth:
		return "postgres"
	case RedisHealth:
		return "redis"
	case CheckerFunc:
		return "checker"
	default:
		_ = v
		return "component"
	}
}

func statusValue(ready bool) string {
	if ready {
		return "ready"
	}
	return "unavailable"
}

func recoverAndLog(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path))
				writeJSON(w, http.StatusInternalServerError, map[string]string{errorKey: "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
		log.Debug("request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Duration("elapsed", time.Since(start)))
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Header already written; nothing more we can do.
		return
	}
}
