// Package history implements listening history and continue-listening: an
// upserted per (user, audio) progress row — one write per user per item, not
// per event — plus a resume endpoint the player consults before starting.
package history

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Module bundles the history module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
}

// NewModule wires the history module together.
func NewModule(pool *pgxpool.Pool, log *slog.Logger) *Module {
	store := NewStore(pool)
	service := NewService(store, log)
	return &Module{
		Service:  service,
		Handlers: NewHandlers(service, log),
	}
}

// Router returns the history routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}
