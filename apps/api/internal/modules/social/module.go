// Package social implements the follower graph: follow/unfollow creators,
// public creator pages, the "you follow" list, and the subscriptions feed —
// newest published episodes across the creators the viewer follows.
package social

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	authmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/auth"
)

// Module bundles the social module's collaborators.
type Module struct {
	Service  *Service
	Handlers *Handlers
}

// NewModule wires the social module together.
func NewModule(pool *pgxpool.Pool, log *slog.Logger) *Module {
	store := NewStore(pool)
	service := NewService(store, log)
	return &Module{
		Service:  service,
		Handlers: NewHandlers(service, log),
	}
}

// Router returns the social routes.
func (m *Module) Router(mw *authmod.Middleware) http.Handler {
	return m.Handlers.Router(mw)
}

// FeedRouter returns the subscriptions-feed routes (mounted at /subscriptions).
func (m *Module) FeedRouter(mw *authmod.Middleware) http.Handler {
	return m.Handlers.FeedRouter(mw)
}
