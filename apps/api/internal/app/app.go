// Package app wires every module into the API router. It owns the root chi
// mux, versioned route groups, CORS, and global middleware, and is the single
// composition root for the API binary.
package app

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/audio"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/health"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/live"
)

// Deps carries the modules the router mounts.
type Deps struct {
	Log            *slog.Logger
	AllowedOrigins []string
	Auth           *auth.Module
	Live           *live.Module
	Audio          *audio.Module // nil when object storage is unavailable
	Health         *health.Module
}

// New composes modules into the root router with versioned groups.
func New(d Deps) *App {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(d.Auth.Middleware.Authenticate)

	r.Get("/api/v1/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"status":"ok","service":"goonj-api"}`))
	})

	r.Route("/api/v1", func(v1 chi.Router) {
		v1.Mount("/health", d.Health.Router())
		v1.Mount("/auth", d.Auth.Router())
		v1.Mount("/live", d.Live.Router(d.Auth.Middleware))
		if d.Audio != nil {
			v1.Mount("/audio", d.Audio.Router(d.Auth.Middleware))
		}
	})

	return &App{router: r}
}

// App holds the composed application router.
type App struct {
	router chi.Router
}

// Handler exposes the composed root handler for the HTTP server.
func (a *App) Handler() http.Handler { return a.router }
