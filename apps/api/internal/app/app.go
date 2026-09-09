// Package app wires every module into the API router. It owns the root chi
// mux, versioned route groups, and CORS, and is the single composition root
// for the API binary.
package app

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/health"
)

// App holds the composed application router.
type App struct {
	router chi.Router
}

// New composes modules into the root router with versioned groups.
func New(log *slog.Logger, healthMod *health.Module, authMod *auth.Module, allowedOrigins []string) *App {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Liveness endpoint mounted at the API root as well.
	r.Get("/api/v1/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ok","service":"goonj-api"}`))
	})

	r.Route("/api/v1", func(v1 chi.Router) {
		v1.Mount("/health", healthMod.Router())
		v1.Mount("/auth", authMod.Router())
	})

	return &App{router: r}
}

// Handler exposes the composed root handler for the HTTP server.
func (a *App) Handler() http.Handler { return a.router }
