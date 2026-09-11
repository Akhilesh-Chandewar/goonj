// Package engagement implements Phase 4 social interactions on audio:
// likes, comments with one level of replies, and comment likes. Counts are
// derived in queries (never denormalized counters) — volumes are well within
// a counted index scan at this stage, and correctness is free.
package engagement

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Module bundles the engagement module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
}

// NewModule wires the engagement module together.
func NewModule(pool *pgxpool.Pool, log *slog.Logger) *Module {
	store := NewStore(pool)
	service := NewService(store, log)
	return &Module{
		Service:  service,
		Handlers: NewHandlers(service, log),
	}
}

// Router returns the engagement routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}
