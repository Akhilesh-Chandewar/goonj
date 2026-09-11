// Package playlists implements user playlists: CRUD, ordered items, and
// owner-only editing. Positions are rewritten in a single transaction on
// every removal/reorder so the sequence can never develop holes.
package playlists

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Module bundles the playlists module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
}

// NewModule wires the playlists module together.
func NewModule(pool *pgxpool.Pool, log *slog.Logger) *Module {
	store := NewStore(pool)
	service := NewService(store, log)
	return &Module{
		Service:  service,
		Handlers: NewHandlers(service, log),
	}
}

// Router returns the playlist routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}
