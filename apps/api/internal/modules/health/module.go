// Package health provides the liveness/readiness contract for the API. It is
// the reference implementation of a Goonj module: router → service →
// repository, with no cross-module reach-ins.
package health

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Service reports the status of a named dependency.
type Service struct {
	Name    string
	Status  func(ctx context.Context) string
	Details map[string]string
}

// Module bundles the health routes and their backing checks.
type Module struct {
	services []Service
}

// NewModule builds the health module from component checks.
func NewModule(services ...Service) *Module {
	return &Module{services: services}
}

// Router mounts the health endpoints. Mounted at /health by the app root:
// GET /api/v1/health and GET /api/v1/health/services.
func (m *Module) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/", m.handleHealth)
	r.Get("/services", m.handleServices)
	return r
}

func (m *Module) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeHealth(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (m *Module) handleServices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := make(map[string]string, len(m.services))
	for _, s := range m.services {
		out[s.Name] = s.Status(ctx)
	}
	writeHealth(w, http.StatusOK, out)
}

func writeHealth(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
