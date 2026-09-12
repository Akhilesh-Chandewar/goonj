// Package auth provides registration, login, refresh rotation, logout,
// identity, and RBAC middleware for Goonj. Handlers stay thin; business
// logic lives in the service, token mechanics in TokenService.
package auth

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Module bundles the auth module's collaborators.
type Module struct {
	Service    *Service
	Middleware *Middleware
	Handlers   *Handlers
}

// NewModule wires the auth module together. priorSecrets are superseded
// JWT signing keys (newest previous first) accepted for validation only —
// see TokenService for the rotation protocol.
func NewModule(pool *pgxpool.Pool, rdb *redis.Client, jwtSecret string, priorSecrets []string, log *slog.Logger) *Module {
	tokens := NewTokenService(jwtSecret, rdb)
	for i := len(priorSecrets) - 1; i >= 0; i-- {
		tokens.AddPriorSecret(priorSecrets[i])
	}
	service := NewService(pool, tokens, log)
	middleware := NewMiddleware(tokens)
	handlers := NewHandlers(service)

	return &Module{
		Service:    service,
		Middleware: middleware,
		Handlers:   handlers,
	}
}

// Router returns the auth routes with middleware applied.
func (m *Module) Router() http.Handler {
	return m.Handlers.Router(m.Middleware)
}
