// Package auth contains the authentication module skeleton: routes and types
// for the /v1/auth group. Handlers stay thin; logic will live in the service
// layer as Phase 1 (login, register, refresh, RBAC) lands.
package auth

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// RegisterRequest is the payload for POST /v1/auth/register.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Module mounts auth routes.
type Module struct{}

// NewModule builds the auth module.
func NewModule() *Module { return &Module{} }

// Router mounts the /auth route group.
func (m *Module) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/register", m.handleRegister)
	r.Post("/login", m.notImplemented)
	return r
}

func (m *Module) handleRegister(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "auth module lands in Phase 1")
}

func (m *Module) notImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "auth module lands in Phase 1")
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":` + quote(msg) + `}`))
}

func quote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "\"\""
	}
	return string(b)
}
