// Package api provides HTTP handlers and routing for the orchestrator.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter creates and configures the chi router with all API endpoints.
// The apiToken is used for authenticating /api/* routes.
func NewRouter(logger *slog.Logger, apiToken string) *chi.Mux {
	r := chi.NewRouter()

	// Middleware stack (applies to all routes)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(slogMiddleware(logger))
	r.Use(middleware.Recoverer)

	// Health check - no auth required
	r.Get("/health", handleHealth)

	// API routes - auth required
	r.Route("/api", func(r chi.Router) {
		r.Use(AuthMiddleware(apiToken))
		// Placeholder ping endpoint - useful for testing auth
		r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("pong"))
		})
		// Future endpoints will be mounted here
	})

	return r
}

// handleHealth returns a simple health check response.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// slogMiddleware returns a middleware that logs requests using slog.
func slogMiddleware(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := r.Context().Value(middleware.RequestIDKey)

			defer func(begin int64) {
				logger.Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"request_id", start,
				)
			}(0)

			next.ServeHTTP(ww, r)
		})
	}
}
