// Package search implements Phase 4 discovery: PostgreSQL full-text search
// over audio titles/descriptions plus trigram fuzzy matching on titles and
// creator names. v1 is pure SQL; pgvector semantic search lands in Phase 5.
package search

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Module bundles the search module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
}

// NewModule wires the search module together.
func NewModule(pool *pgxpool.Pool, log *slog.Logger) *Module {
	store := NewStore(pool)
	service := NewService(store, log)
	return &Module{
		Service:  service,
		Handlers: NewHandlers(service, log),
	}
}

// Router returns the search routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}
